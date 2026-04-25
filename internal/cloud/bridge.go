package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sleuth-io/sx/internal/logger"
)

// Backoff bounds for automatic WebSocket reconnection. A disconnected
// serve loop retries with exponential backoff so a flaky network doesn't
// require manual restarts. If a connection ran longer than
// “reconnectResetThreshold“ we treat it as healthy and reset the
// backoff on the next disconnect — otherwise a long-lived session that
// drops once would reconnect at the max delay forever.
const (
	initialReconnectDelay   = 1 * time.Second
	maxReconnectDelay       = 30 * time.Second
	reconnectResetThreshold = 60 * time.Second

	// readFrameTimeout bounds how long we'll wait for an inbound frame
	// before sending a client-side ping to keep the WebSocket alive.
	// Pulse's nginx + pushpin have their own timeouts (typically 60s);
	// pinging well under that prevents idle disconnects.
	readFrameTimeout = 45 * time.Second

	// mcpCallTimeout bounds how long we'll wait for the in-process MCP
	// server to answer a single tools/call. Must be shorter than
	// pulse's 30s caller-side timeout so we can surface a useful error
	// instead of the chat client hanging.
	mcpCallTimeout = 25 * time.Second

	// writeEnvelopeTimeout bounds one outbound WebSocket write. Kept
	// short so a stuck peer can't wedge a handler forever.
	writeEnvelopeTimeout = 10 * time.Second
)

// ServeOptions collects the knobs for “sx cloud serve“. The zero
// value is NOT usable — Credential + MCPServerFactory must be set.
type ServeOptions struct {
	// Credential is the persisted relay credential (URL + machine
	// token). Required.
	Credential *Credential

	// MCPServerFactory builds a freshly-initialized MCP server that
	// exposes the local vault's tools. Each reconnect creates a new
	// server + in-memory transport pair so stale state from a prior
	// session can't leak across reconnects. Returning an error aborts
	// the reconnect and surfaces it to the operator; a silent empty
	// server would be indistinguishable from a healthy vault with no
	// tools.
	MCPServerFactory func() (*mcp.Server, error)

	// HTTPClient, if set, overrides the HTTP client used for the
	// WebSocket handshake. Tests inject a stub; production passes nil
	// to use the default client.
	HTTPClient *http.Client
}

// Serve runs the WebSocket dispatcher loop, returning only when “ctx“
// is cancelled or a non-recoverable error occurs. Reconnect attempts are
// made with exponential backoff; “ctx“ cancellation is observed at
// each backoff step.
func Serve(ctx context.Context, opts ServeOptions) error {
	if opts.Credential == nil {
		return errors.New("serve: credential is nil")
	}
	if opts.MCPServerFactory == nil {
		return errors.New("serve: MCPServerFactory is nil")
	}
	if err := opts.Credential.Validate(); err != nil {
		return fmt.Errorf("serve: invalid credential: %w", err)
	}
	wsURL, err := opts.Credential.WebSocketURL()
	if err != nil {
		return fmt.Errorf("serve: bad WebSocket URL: %w", err)
	}

	log := logger.Get()
	delay := initialReconnectDelay
	for {
		start := time.Now()
		connErr := runOneConnection(ctx, opts, wsURL)
		duration := time.Since(start)

		if connErr != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		if connErr != nil {
			log.Warn("sx cloud serve connection ended; will retry",
				"error", connErr, "duration", duration, "delay", delay)
		}

		// If the session was healthy enough to live past the reset
		// threshold, start the next backoff from scratch. Prevents the
		// "maxed out forever after a long happy run" case.
		if duration > reconnectResetThreshold {
			delay = initialReconnectDelay
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		// Exponential backoff, capped.
		delay *= 2
		if delay > maxReconnectDelay {
			delay = maxReconnectDelay
		}
	}
}

// runOneConnection dials the WebSocket once, sets up the in-memory MCP
// bridge, and runs until the WebSocket is closed or “ctx“ is cancelled.
// Reconnect decisions live in “Serve“.
func runOneConnection(ctx context.Context, opts ServeOptions, wsURL string) error {
	log := logger.Get()

	server, err := opts.MCPServerFactory()
	if err != nil {
		return fmt.Errorf("failed to build MCP server: %w", err)
	}

	dialOpts := &websocket.DialOptions{
		HTTPClient: opts.HTTPClient,
		HTTPHeader: http.Header{
			"Authorization": []string{"Bearer " + opts.Credential.MachineToken},
		},
	}
	conn, resp, err := websocket.Dial(ctx, wsURL, dialOpts)
	if err != nil {
		return fmt.Errorf("websocket dial %s: %w", wsURL, err)
	}
	// The HTTP response body isn't used after the handshake completes,
	// but closing it satisfies the bodyclose linter and releases any
	// HTTP/1.1 conn reader state.
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer func() {
		_ = conn.Close(websocket.StatusNormalClosure, "")
	}()

	// Generous message cap — MCP payloads fit in well under a MB in
	// practice, but vault tool outputs can be large.
	conn.SetReadLimit(4 << 20)

	log.Info("sx cloud serve connected", "url", wsURL, "relay_gid", opts.Credential.RelayGID)

	// Bring up an in-process MCP client + server pair via an in-memory
	// transport. All JSON-RPC plumbing (ids, error envelopes, schema
	// validation) lives in the SDK's Server; we just shuttle envelopes
	// in and out.
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	serverCtx, serverCancel := context.WithCancel(ctx)
	defer serverCancel()
	go func() {
		if err := server.Run(serverCtx, serverTransport); err != nil && !errors.Is(err, context.Canceled) {
			log.Warn("in-process MCP server exited", "error", err)
		}
	}()

	memConn, err := clientTransport.Connect(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect in-memory MCP transport: %w", err)
	}
	defer func() { _ = memConn.Close() }()

	mux := newResponseMux(memConn)
	muxCtx, muxCancel := context.WithCancel(ctx)
	defer muxCancel()
	go mux.readLoop(muxCtx)

	return dispatchLoop(ctx, conn, mux)
}

// dispatchLoop runs until the WebSocket closes. Inbound envelopes are
// handled concurrently so one slow MCP call can't block others. Ping
// liveness happens in the same loop: if “readFrameTimeout“ elapses
// with no frame we send a ping and keep going.
func dispatchLoop(ctx context.Context, ws *websocket.Conn, mux *responseMux) error {
	log := logger.Get()

	// Wait for spawned handlers on exit so we don't leak goroutines
	// across reconnects — each reconnect spawns a fresh mux + dispatch
	// loop, so previous handlers must fully drain.
	var handlers sync.WaitGroup
	defer handlers.Wait()

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		readCtx, cancelRead := context.WithTimeout(ctx, readFrameTimeout)
		kind, data, err := ws.Read(readCtx)
		cancelRead()
		if err != nil {
			// Read deadline: send a ping and keep going.
			if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
				pingCtx, cancelPing := context.WithTimeout(ctx, 5*time.Second)
				if pingErr := ws.Ping(pingCtx); pingErr != nil {
					cancelPing()
					return fmt.Errorf("ping failed: %w", pingErr)
				}
				cancelPing()
				continue
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("ws read: %w", err)
		}
		if kind != websocket.MessageText {
			log.Debug("ignoring non-text WebSocket frame", "kind", kind, "bytes", len(data))
			continue
		}

		var env inboundEnvelope
		if err := json.Unmarshal(data, &env); err != nil {
			log.Warn("malformed envelope from pulse", "error", err)
			continue
		}
		if env.Type != "mcp-request" {
			log.Debug("ignoring non-request envelope", "type", env.Type)
			continue
		}

		handlers.Add(1)
		go func(env inboundEnvelope) {
			defer handlers.Done()
			if err := handleOneRequest(ctx, ws, mux, env); err != nil {
				log.Warn("request dispatch failed", "error", err, "request_id", env.RequestID)
			}
		}(env)
	}
}

// inboundEnvelope is the wire shape pulse publishes on
// “sx-relay:<gid>:in“. Matches “SxRequestEnvelope.to_json“ in
// “sleuth/apps/sx_relay/service/relay_bus.py“.
type inboundEnvelope struct {
	Type      string          `json:"type"`
	RequestID string          `json:"request_id"`
	JSONRPCID json.RawMessage `json:"jsonrpc_id"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params"`
}

// outboundEnvelope is the wire shape we send back on each inbound
// request. Must match the parser in
// “sleuth/apps/sx_relay/views/websocket.py“.
type outboundEnvelope struct {
	Type      string         `json:"type"`
	RequestID string         `json:"request_id"`
	Body      map[string]any `json:"body"`
}

// responseMux owns the single reader on the in-memory MCP connection
// and fans responses out by JSON-RPC id. Multiple concurrent handlers
// can write requests into “memConn“; each registers a pending channel
// under its id, waits for the reader to deliver, then unregisters.
type responseMux struct {
	memConn mcp.Connection
	seq     atomic.Uint64
	writeMu sync.Mutex

	pendingMu sync.Mutex
	pending   map[string]chan *jsonrpc.Response
}

func newResponseMux(memConn mcp.Connection) *responseMux {
	return &responseMux{
		memConn: memConn,
		pending: make(map[string]chan *jsonrpc.Response),
	}
}

// readLoop reads from “memConn“ until the context is cancelled or the
// connection closes. Response frames are delivered to the waiter
// registered under their id; server-originated requests (sampling /
// elicitation) are dropped — the relay shape doesn't support them.
func (m *responseMux) readLoop(ctx context.Context) {
	log := logger.Get()
	for {
		if ctx.Err() != nil {
			return
		}
		msg, err := m.memConn.Read(ctx)
		if err != nil {
			if ctx.Err() == nil && !errors.Is(err, context.Canceled) {
				log.Debug("response mux reader exited", "error", err)
			}
			m.shutdownPending()
			return
		}
		resp, ok := msg.(*jsonrpc.Response)
		if !ok {
			log.Debug("response mux dropped non-response message")
			continue
		}
		key := idKey(resp.ID)
		m.pendingMu.Lock()
		ch, present := m.pending[key]
		if present {
			delete(m.pending, key)
		}
		m.pendingMu.Unlock()
		if present {
			// Non-blocking send: the waiter always reads exactly once
			// before unregistering, and the channel is buffered to 1.
			ch <- resp
		}
	}
}

// shutdownPending unblocks every pending waiter when the reader exits.
// Waiters see a nil response and surface an internal error upstream.
func (m *responseMux) shutdownPending() {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()
	for k, ch := range m.pending {
		close(ch)
		delete(m.pending, k)
	}
}

// dispatch writes a request to “memConn“ and returns the matched
// response (or “nil“ with a non-nil error). Handlers call this once
// per inbound envelope.
func (m *responseMux) dispatch(ctx context.Context, method string, params json.RawMessage) (*jsonrpc.Response, error) {
	id, err := jsonrpc.MakeID(float64(m.seq.Add(1)))
	if err != nil {
		return nil, fmt.Errorf("make internal id: %w", err)
	}
	key := idKey(id)
	ch := make(chan *jsonrpc.Response, 1)

	m.pendingMu.Lock()
	m.pending[key] = ch
	m.pendingMu.Unlock()

	// Clean up the pending entry on any exit path — timeout, cancel, or
	// successful receipt. The writer lock is held only around the
	// single ``Write`` so concurrent handlers don't interleave frames.
	cleanup := func() {
		m.pendingMu.Lock()
		if existing, ok := m.pending[key]; ok && existing == ch {
			delete(m.pending, key)
		}
		m.pendingMu.Unlock()
	}

	p := params
	if len(p) == 0 {
		p = nil
	}
	req := &jsonrpc.Request{ID: id, Method: method, Params: p}

	m.writeMu.Lock()
	writeErr := m.memConn.Write(ctx, req)
	m.writeMu.Unlock()
	if writeErr != nil {
		cleanup()
		return nil, fmt.Errorf("mcp write: %w", writeErr)
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			// Channel closed by ``shutdownPending``: the transport
			// went away mid-flight. Treat as an internal error so the
			// caller produces a JSON-RPC -32603 for pulse.
			cleanup()
			return nil, errors.New("mcp transport closed before response")
		}
		return resp, nil
	case <-ctx.Done():
		cleanup()
		return nil, ctx.Err()
	}
}

// idKey turns an arbitrary jsonrpc.ID into a stable string we can use
// as a map key. “jsonrpc.ID“ is an interface alias for jsonrpc2.ID
// (not directly comparable), so we hash it via its wire form.
func idKey(id jsonrpc.ID) string {
	return fmt.Sprintf("%v", id)
}

// handleOneRequest forwards one inbound envelope to the in-memory MCP
// server via the mux, then writes the response back on the WebSocket. A
// response is always written (even on error) so pulse's await can
// complete without hitting its 30s timeout.
func handleOneRequest(ctx context.Context, ws *websocket.Conn, mux *responseMux, env inboundEnvelope) error {
	callCtx, cancel := context.WithTimeout(ctx, mcpCallTimeout)
	defer cancel()

	resp, err := mux.dispatch(callCtx, env.Method, env.Params)
	if err != nil {
		return writeError(ctx, ws, env, jsonrpc.CodeInternalError, err.Error())
	}
	return writeResponse(ctx, ws, env, resp)
}

// writeResponse wraps a JSON-RPC response in the outbound envelope and
// writes it on the WebSocket. Always carries exactly one of “result“
// or “error“ so the chat client can't reject us for an incomplete
// JSON-RPC reply.
func writeResponse(ctx context.Context, ws *websocket.Conn, env inboundEnvelope, resp *jsonrpc.Response) error {
	body := map[string]any{"jsonrpc": "2.0"}
	if len(env.JSONRPCID) > 0 {
		body["id"] = env.JSONRPCID
	}
	switch {
	case resp.Error != nil:
		body["error"] = resp.Error
	case len(resp.Result) > 0:
		body["result"] = json.RawMessage(resp.Result)
	default:
		// JSON-RPC §5: a successful response MUST contain "result". An
		// empty/absent SDK-side result (void-returning handler) maps to
		// ``null`` on the wire.
		body["result"] = json.RawMessage("null")
	}
	return sendEnvelope(ctx, ws, outboundEnvelope{
		Type:      "mcp-response",
		RequestID: env.RequestID,
		Body:      body,
	})
}

// writeError produces a JSON-RPC error response and sends it. Used for
// internal plumbing failures (mcp read/write, timeout) that the
// in-process server didn't get a chance to report on its own.
func writeError(ctx context.Context, ws *websocket.Conn, env inboundEnvelope, code int64, message string) error {
	body := map[string]any{
		"jsonrpc": "2.0",
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	}
	if len(env.JSONRPCID) > 0 {
		body["id"] = env.JSONRPCID
	}
	return sendEnvelope(ctx, ws, outboundEnvelope{
		Type:      "mcp-response",
		RequestID: env.RequestID,
		Body:      body,
	})
}

// sendEnvelope writes an envelope on the WebSocket with a fresh timeout.
//
// The outer context is intentionally discarded (named ``_outerCtx`` to make
// that obvious at the call site) and replaced with ``context.Background``.
// The reason: this function is the last write that tells pulse a request
// failed or completed, and the most common reason ``_outerCtx`` is cancelled
// at this point is that we're tearing down the connection or just received
// an interrupt — exactly the moments when we still need to flush a final
// frame to the server. A short ``writeEnvelopeTimeout`` is plenty to bound
// the write itself, so we don't risk hanging shutdown.
func sendEnvelope(_outerCtx context.Context, ws *websocket.Conn, env outboundEnvelope) error {
	_ = _outerCtx
	buf, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}
	writeCtx, cancel := context.WithTimeout(context.Background(), writeEnvelopeTimeout)
	defer cancel()
	return ws.Write(writeCtx, websocket.MessageText, buf)
}
