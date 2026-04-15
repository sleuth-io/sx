package vault

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// decodeJSONRPCRequest reads the request body and returns the decoded
// JSON-RPC request. Test helper to keep individual tests terse.
func decodeJSONRPCRequest(t *testing.T, r *http.Request) jsonrpcRequest {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	var req jsonrpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	return req
}

// writeJSONRPCResult writes a JSON-RPC success response with the given raw
// result payload. Test helper.
func writeJSONRPCResult(t *testing.T, w http.ResponseWriter, id int, result any) {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	resp := jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  raw,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func TestSleuthMCPProxyPostSendsBearerAndDecodesResult(t *testing.T) {
	var gotAuth string
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/mcp/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		req := decodeJSONRPCRequest(t, r)
		gotMethod = req.Method
		writeJSONRPCResult(t, w, req.ID, map[string]string{"ok": "yes"})
	}))
	defer srv.Close()

	proxy := newSleuthMCPProxy(srv.URL, "test-token")
	resp, err := proxy.post(context.Background(), jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      reqIDListTools,
		Method:  "tools/list",
	})
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer test-token")
	}
	if gotMethod != "tools/list" {
		t.Errorf("method = %q, want tools/list", gotMethod)
	}

	var payload map[string]string
	if err := json.Unmarshal(resp.Result, &payload); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if payload["ok"] != "yes" {
		t.Errorf("result[ok] = %q, want yes", payload["ok"])
	}
}

func TestSleuthMCPProxyPostSurfacesHTTPErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	proxy := newSleuthMCPProxy(srv.URL, "")
	_, err := proxy.post(context.Background(), jsonrpcRequest{JSONRPC: "2.0", ID: 1, Method: "x"})
	if err == nil {
		t.Fatal("expected error on HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("error should mention status and body: %v", err)
	}
}

func TestSleuthMCPProxyPostSurfacesJSONRPCError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not found"}}`))
	}))
	defer srv.Close()

	proxy := newSleuthMCPProxy(srv.URL, "")
	_, err := proxy.post(context.Background(), jsonrpcRequest{JSONRPC: "2.0", ID: 1, Method: "x"})
	if err == nil {
		t.Fatal("expected error on JSON-RPC error response")
	}
	if !strings.Contains(err.Error(), "method not found") {
		t.Errorf("error should contain upstream message: %v", err)
	}
}

func TestSleuthMCPProxyPostMissingBaseURL(t *testing.T) {
	proxy := newSleuthMCPProxy("", "token")
	_, err := proxy.post(context.Background(), jsonrpcRequest{JSONRPC: "2.0", ID: 1, Method: "x"})
	if err == nil {
		t.Fatal("expected error when baseURL is empty")
	}
}

func TestListUpstreamToolsFiltersToMgmtNamespace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeJSONRPCRequest(t, r)
		if req.Method != "tools/list" {
			t.Errorf("unexpected method: %s", req.Method)
		}
		writeJSONRPCResult(t, w, req.ID, map[string]any{
			"tools": []map[string]any{
				{"name": "mgmt__create_team", "description": "create"},
				{"name": "mgmt__list_bots", "description": "list"},
				{"name": "vault__read_asset", "description": "read"},
				{"name": "unrelated_tool", "description": "nope"},
			},
		})
	}))
	defer srv.Close()

	proxy := newSleuthMCPProxy(srv.URL, "token")
	tools, err := proxy.ListUpstreamTools(context.Background())
	if err != nil {
		t.Fatalf("ListUpstreamTools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("want 2 mgmt tools, got %d: %+v", len(tools), tools)
	}
	for _, tool := range tools {
		if !strings.HasPrefix(tool.Name, mgmtNamespace) {
			t.Errorf("returned tool %q lost its mgmt__ prefix (should be kept by ListUpstreamTools)", tool.Name)
		}
	}
}

func TestListUpstreamToolsPropagatesUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer srv.Close()

	proxy := newSleuthMCPProxy(srv.URL, "")
	if _, err := proxy.ListUpstreamTools(context.Background()); err == nil {
		t.Fatal("expected error when upstream returns 502")
	}
}

func TestCallForwardsUpstreamNameAndArguments(t *testing.T) {
	var gotName string
	var gotArgs map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeJSONRPCRequest(t, r)
		if req.Method != "tools/call" {
			t.Errorf("method = %q, want tools/call", req.Method)
		}
		params, ok := req.Params.(map[string]any)
		if !ok {
			t.Fatalf("params not a map: %T", req.Params)
		}
		gotName, _ = params["name"].(string)
		gotArgs, _ = params["arguments"].(map[string]any)

		result := map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "hello from upstream"},
			},
		}
		writeJSONRPCResult(t, w, req.ID, result)
	}))
	defer srv.Close()

	proxy := newSleuthMCPProxy(srv.URL, "token")
	result, err := proxy.Call(
		context.Background(),
		"mgmt__create_team",
		json.RawMessage(`{"name":"Platform","size":7}`),
	)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if gotName != "mgmt__create_team" {
		t.Errorf("upstream name = %q, want mgmt__create_team", gotName)
	}
	if gotArgs["name"] != "Platform" {
		t.Errorf("arg name = %v, want Platform", gotArgs["name"])
	}
	// JSON numbers decode as float64 through any.
	if gotArgs["size"] != float64(7) {
		t.Errorf("arg size = %v, want 7", gotArgs["size"])
	}

	if len(result.Content) != 1 {
		t.Fatalf("want 1 content block, got %d", len(result.Content))
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T, want *mcp.TextContent", result.Content[0])
	}
	if text.Text != "hello from upstream" {
		t.Errorf("content text = %q, want hello from upstream", text.Text)
	}
}

func TestCallAcceptsEmptyArguments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := decodeJSONRPCRequest(t, r)
		params, _ := req.Params.(map[string]any)
		// With no arguments, the field should marshal as nil (JSON null) on
		// the wire, which decodes back to nil here.
		if params["arguments"] != nil {
			t.Errorf("arguments = %v, want nil for empty input", params["arguments"])
		}
		writeJSONRPCResult(t, w, req.ID, map[string]any{"content": []any{}})
	}))
	defer srv.Close()

	proxy := newSleuthMCPProxy(srv.URL, "")
	if _, err := proxy.Call(context.Background(), "mgmt__ping", nil); err != nil {
		t.Fatalf("Call with nil args: %v", err)
	}
}

func TestCallRejectsMalformedArguments(t *testing.T) {
	proxy := newSleuthMCPProxy("http://unused", "")
	_, err := proxy.Call(context.Background(), "mgmt__ping", json.RawMessage(`{not json`))
	if err == nil {
		t.Fatal("expected error for malformed arguments")
	}
}
