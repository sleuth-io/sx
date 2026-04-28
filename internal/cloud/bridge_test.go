package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestServe_EndToEnd stands up a pulse-side WebSocket test server, runs
// “Serve“ against it, sends an MCP JSON-RPC request wrapped in the
// relay envelope, and verifies the response body carries the
// “initialize“ reply from the in-memory MCP server.
//
// This exercises the subscribe-before-publish invariant (the test would
// hang if “Serve“ tried to publish before dialing), the envelope
// round-trip, and the id translation (our internal monotonic id should
// not leak; the chat client's “jsonrpc_id“ should be echoed).
func TestServe_EndToEnd(t *testing.T) {
	type outbound struct {
		Type      string         `json:"type"`
		RequestID string         `json:"request_id"`
		Body      map[string]any `json:"body"`
	}

	respCh := make(chan outbound, 1)
	reqID := "test-req-1"
	jsonrpcID := 42 // chat-client-supplied id; must be echoed verbatim

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer machine-tok-abc" {
			t.Errorf("missing/bad Authorization header: %q", auth)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		// Handler lives for the life of the test; ``Serve`` cancels
		// ``ctx`` when the test ends.
		defer func() { _ = ws.Close(websocket.StatusNormalClosure, "") }()

		// Send one MCP request envelope to the sx side.
		envBytes, err := json.Marshal(map[string]any{
			"type":       "mcp-request",
			"request_id": reqID,
			"jsonrpc_id": jsonrpcID,
			"method":     "initialize",
			"params": map[string]any{
				"protocolVersion": "2025-06-18",
				"capabilities":    map[string]any{},
				"clientInfo": map[string]any{
					"name":    "test-client",
					"version": "0",
				},
			},
		})
		if err != nil {
			t.Errorf("marshal envelope: %v", err)
			return
		}
		writeCtx, writeCancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer writeCancel()
		if err := ws.Write(writeCtx, websocket.MessageText, envBytes); err != nil {
			t.Errorf("write req: %v", err)
			return
		}

		// Read the reply.
		readCtx, readCancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer readCancel()
		_, data, err := ws.Read(readCtx)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				t.Errorf("read resp: %v", err)
			}
			return
		}
		var out outbound
		if err := json.Unmarshal(data, &out); err != nil {
			t.Errorf("unmarshal resp: %v", err)
			return
		}
		respCh <- out
	}))
	defer srv.Close()

	cred := &Credential{
		RelayBaseURL: strings.TrimSuffix(srv.URL, "/") + "/relay/SRtest/",
		RelayGID:     "SRtest",
		MachineToken: "machine-tok-abc",
	}

	// MCP server factory: empty server is fine for ``initialize`` —
	// the SDK responds with server info + capabilities regardless of
	// registered tools.
	factory := func() (*mcp.Server, error) {
		return mcp.NewServer(&mcp.Implementation{Name: "test-sx", Version: "0.1"}, nil), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- Serve(ctx, ServeOptions{
			Credential:       cred,
			MCPServerFactory: factory,
		})
	}()

	select {
	case out := <-respCh:
		if out.Type != "mcp-response" {
			t.Errorf("wrong envelope type: %q", out.Type)
		}
		if out.RequestID != reqID {
			t.Errorf("request_id not echoed: got %q want %q", out.RequestID, reqID)
		}
		// The body must carry the chat-client's original jsonrpc id.
		// JSON unmarshals numeric ids into float64, so compare against
		// the number we sent.
		gotID, ok := out.Body["id"].(float64)
		if !ok {
			t.Fatalf("response missing id: %+v", out.Body)
		}
		if int(gotID) != jsonrpcID {
			t.Errorf("id not echoed: got %v want %v", gotID, jsonrpcID)
		}
		if _, ok := out.Body["result"]; !ok {
			t.Errorf("response missing result: %+v", out.Body)
		}
		if errVal, has := out.Body["error"]; has {
			t.Errorf("unexpected error in response: %v", errVal)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("timed out waiting for MCP response envelope")
	}

	cancel()
	if err := <-serveErr; err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("Serve returned non-cancel error: %v", err)
	}
}

// TestServe_UnknownMethodReturnsJSONRPCError verifies that when the
// in-process MCP server returns a method-not-found error, the envelope
// carries a JSON-RPC error body (not a transport-level failure).
func TestServe_UnknownMethodReturnsJSONRPCError(t *testing.T) {
	type outbound struct {
		Type      string         `json:"type"`
		RequestID string         `json:"request_id"`
		Body      map[string]any `json:"body"`
	}
	respCh := make(chan outbound, 1)
	reqID := "unknown-1"
	jsonrpcID := "client-id-7"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		defer func() { _ = ws.Close(websocket.StatusNormalClosure, "") }()

		envBytes, _ := json.Marshal(map[string]any{
			"type":       "mcp-request",
			"request_id": reqID,
			"jsonrpc_id": jsonrpcID,
			"method":     "this/does/not/exist",
			"params":     map[string]any{},
		})
		writeCtx, writeCancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer writeCancel()
		if err := ws.Write(writeCtx, websocket.MessageText, envBytes); err != nil {
			return
		}

		readCtx, readCancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer readCancel()
		_, data, err := ws.Read(readCtx)
		if err != nil {
			return
		}
		var out outbound
		_ = json.Unmarshal(data, &out)
		respCh <- out
	}))
	defer srv.Close()

	cred := &Credential{
		RelayBaseURL: strings.TrimSuffix(srv.URL, "/") + "/relay/SRtest/",
		RelayGID:     "SRtest",
		MachineToken: "tok",
	}
	factory := func() (*mcp.Server, error) {
		return mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() { _ = Serve(ctx, ServeOptions{Credential: cred, MCPServerFactory: factory}) }()

	select {
	case out := <-respCh:
		if _, has := out.Body["error"]; !has {
			t.Errorf("expected error in body, got %+v", out.Body)
		}
		if got, _ := out.Body["id"].(string); got != jsonrpcID {
			t.Errorf("string id not echoed: got %v want %q", out.Body["id"], jsonrpcID)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("timed out waiting for error envelope")
	}
}

// TestParseErrorCode ensures we import jsonrpc correctly (compile-time
// check). Acts as a smoke test for the error-code constants we rely on.
func TestJSONRPCErrorCodesAreStable(t *testing.T) {
	if jsonrpc.CodeInternalError != -32603 {
		t.Errorf("CodeInternalError changed: %d", jsonrpc.CodeInternalError)
	}
}
