// Management MCP tools for the Sleuth vault — proxy edition.
//
// Previously this package shipped hand-written Go implementations of every
// management tool (list_assets, create_team, add_bot_api_key, ...), each
// wrapping a specific GraphQL query against skills.new. That setup meant
// every backend change had to be mirrored in Go and drifted easily.
//
// This file replaces those per-tool handlers with a thin MCP proxy. At sx
// startup we:
//
//  1. Open an MCP session against skills.new's /mcp/ endpoint (Bearer PAT),
//  2. Call tools/list and keep only the "mgmt__*" namespace (asset content
//     tools live under "vault__*" and are not the management surface), and
//  3. Register each returned tool with the local mcp.Server, stripping the
//     mgmt__ prefix so the tool names sx clients see match what they've been
//     consuming (create_team, list_bots, run_pql_query, ...).
//
// Each local tool handler is a single shared closure that reposts the call
// to skills.new's /mcp/ with the original namespaced name, captures the
// upstream CallToolResult, and returns it verbatim. Adding a new tool on
// skills.new shows up in sx on the next restart with zero Go changes —
// the skills.new Python registry is the single source of truth.
//
// Failure modes:
//   - Upstream unreachable at startup → we log a clear warning and register
//     no management tools. sx's native query tool keeps working. The next
//     sx restart will retry.
//   - Permission denied / tool error → the upstream envelope
//     ({"error": "permission_denied", ...}) is passed through verbatim in
//     the MCP text content block so the calling LLM sees the same shape
//     the web ManagementOrchestrator produces.

package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sleuth-io/sx/internal/buildinfo"
	"github.com/sleuth-io/sx/internal/logger"
)

// mgmtNamespace is the prefix every management tool carries in the skills.new
// /mcp/ catalog. It's stripped before re-registration so local tool names
// stay stable across the migration.
const mgmtNamespace = "mgmt__"

// upstreamMCPTimeout is the per-call timeout for the /mcp/ HTTP round-trip.
// Management tools are synchronous and fast (most are DB reads or a single
// mutation), so we keep this tight to fail loudly on upstream stalls.
const upstreamMCPTimeout = 30 * time.Second

// sleuthMCPProxy talks to skills.new's /mcp/ endpoint on behalf of sx.
type sleuthMCPProxy struct {
	baseURL    string
	authToken  string
	httpClient *http.Client
}

func newSleuthMCPProxy(baseURL, authToken string) *sleuthMCPProxy {
	return &sleuthMCPProxy{
		baseURL:    strings.TrimRight(baseURL, "/"),
		authToken:  authToken,
		httpClient: &http.Client{Timeout: upstreamMCPTimeout},
	}
}

// jsonrpcRequest is the subset of JSON-RPC 2.0 we emit. The `result`/`error`
// fields live in a separate response struct below.
type jsonrpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

// post sends a single JSON-RPC request and returns the decoded response.
// It's used for both the initialize handshake and the actual tools/list /
// tools/call calls. skills.new's /mcp/ gateway is stateless per request
// (see “mcp_gateway_view“ — the DELETE handler acks without tearing down
// anything) so a single POST round-trip is all we need once auth is set.
func (p *sleuthMCPProxy) post(ctx context.Context, body jsonrpcRequest) (*jsonrpcResponse, error) {
	if p.baseURL == "" {
		return nil, errors.New("sleuth vault base URL is not set; cannot reach /mcp/")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON-RPC request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/mcp/", bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("failed to create /mcp/ request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("User-Agent", buildinfo.GetUserAgent())
	if p.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.authToken)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to POST /mcp/: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read /mcp/ response body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("/mcp/ returned HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// The gateway may 202 with an empty body on notifications/initialized —
	// callers of that method aren't expected to inspect the response.
	if len(bytes.TrimSpace(bodyBytes)) == 0 {
		return &jsonrpcResponse{}, nil
	}

	var out jsonrpcResponse
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		return nil, fmt.Errorf("failed to parse /mcp/ JSON-RPC response: %w", err)
	}
	if out.Error != nil {
		return nil, fmt.Errorf("/mcp/ JSON-RPC error %d: %s", out.Error.Code, out.Error.Message)
	}
	return &out, nil
}

// ListUpstreamTools fetches the catalog from skills.new's /mcp/ endpoint and
// returns only the tools under the mgmt__ namespace with the prefix stripped.
// The vault__ tools (asset content read helpers) are intentionally excluded —
// sx has its own native asset-content commands and proxying those would be
// double-exposure.
func (p *sleuthMCPProxy) ListUpstreamTools(ctx context.Context) ([]*mcp.Tool, error) {
	resp, err := p.post(ctx, jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	})
	if err != nil {
		return nil, err
	}

	var payload struct {
		Tools []*mcp.Tool `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &payload); err != nil {
		return nil, fmt.Errorf("failed to parse tools/list payload: %w", err)
	}

	out := make([]*mcp.Tool, 0, len(payload.Tools))
	for _, t := range payload.Tools {
		if !strings.HasPrefix(t.Name, mgmtNamespace) {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// Call forwards a tools/call request for the given upstream (mgmt__-prefixed)
// tool name and returns the upstream CallToolResult as-is. The mcp package's
// CallToolResult has an UnmarshalJSON that handles the Content polymorphism,
// so a direct json.Unmarshal is sufficient.
func (p *sleuthMCPProxy) Call(ctx context.Context, upstreamName string, arguments json.RawMessage) (*mcp.CallToolResult, error) {
	var args any
	if len(arguments) > 0 {
		// Round-trip the raw bytes through json.Unmarshal so the final request
		// carries a proper JSON object (not an escaped string).
		if err := json.Unmarshal(arguments, &args); err != nil {
			return nil, fmt.Errorf("failed to parse tool arguments: %w", err)
		}
	}
	resp, err := p.post(ctx, jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/call",
		Params: map[string]any{
			"name":      upstreamName,
			"arguments": args,
		},
	})
	if err != nil {
		return nil, err
	}

	result := &mcp.CallToolResult{}
	if err := json.Unmarshal(resp.Result, result); err != nil {
		return nil, fmt.Errorf("failed to parse tools/call result for %s: %w", upstreamName, err)
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Registration — called from the top-level sx MCP server.
// ---------------------------------------------------------------------------

// RegisterManagementTools proxies every management tool exposed by skills.new
// into sx's local MCP server. It lists the upstream catalog once at startup
// and registers each tool via a shared forwarder closure. If the upstream is
// unreachable the local server simply doesn't get management tools — sx's
// native query tool and all non-MCP commands keep working.
func (s *SleuthVault) RegisterManagementTools(mcpServer *mcp.Server) {
	log := logger.Get()
	log.Debug("registering management MCP tools via /mcp/ proxy")

	proxy := newSleuthMCPProxy(s.serverURL, s.authToken)

	// Use a short-lived context just for the startup list. Each individual
	// tool call uses the request's own context.
	ctx, cancel := context.WithTimeout(context.Background(), upstreamMCPTimeout)
	defer cancel()

	upstreamTools, err := proxy.ListUpstreamTools(ctx)
	if err != nil {
		log.Warn(
			"failed to list management tools from skills.new /mcp/ — "+
				"management surface will be unavailable until the next sx restart",
			"error", err,
		)
		return
	}

	for _, upstream := range upstreamTools {
		upstreamName := upstream.Name
		localName := strings.TrimPrefix(upstreamName, mgmtNamespace)
		local := &mcp.Tool{
			Name:        localName,
			Description: upstream.Description,
			InputSchema: upstream.InputSchema,
		}
		mcpServer.AddTool(local, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return proxy.Call(ctx, upstreamName, req.Params.Arguments)
		})
		log.Debug("registered management tool proxy", "local", localName, "upstream", upstreamName)
	}
	log.Debug("management tool proxy registration complete", "count", len(upstreamTools))
}
