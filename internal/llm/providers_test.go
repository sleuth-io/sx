package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var basicReq = Request{Messages: []Message{
	{Role: "system", Content: "be terse"},
	{Role: "user", Content: "hello"},
}}

var schemaReq = Request{
	Messages: []Message{{Role: "user", Content: "hello"}},
	Schema:   json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean"}}}`),
}

func captureServer(t *testing.T, reply string, captured *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if captured != nil {
			*captured = map[string]any{}
			_ = json.Unmarshal(body, captured)
			(*captured)["_path"] = r.URL.Path
			(*captured)["_auth"] = r.Header.Get("Authorization")
			(*captured)["_apikey"] = r.Header.Get("x-api-key")
		}
		_, _ = w.Write([]byte(reply))
	}))
}

func TestOllamaProvider(t *testing.T) {
	var got map[string]any
	srv := captureServer(t, `{"message":{"content":"{\"ok\":true}"},"prompt_eval_count":10,"eval_count":5}`, &got)
	defer srv.Close()

	p := &ollamaProvider{baseURL: srv.URL, model: "llama3"}
	resp, err := p.Complete(context.Background(), schemaReq)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != `{"ok":true}` || resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 5 {
		t.Fatalf("resp = %+v", resp)
	}
	if got["_path"] != "/api/chat" || got["model"] != "llama3" || got["stream"] != false {
		t.Fatalf("request = %+v", got)
	}
	if _, hasFormat := got["format"]; !hasFormat {
		t.Fatal("schema should be sent as native format")
	}
}

func TestAnthropicProvider(t *testing.T) {
	var got map[string]any
	srv := captureServer(t, `{"content":[{"type":"text","text":"hi there"}],"usage":{"input_tokens":7,"output_tokens":3}}`, &got)
	defer srv.Close()

	p := &anthropicProvider{apiKey: "sk-ant-test", baseURL: srv.URL}
	resp, err := p.Complete(context.Background(), basicReq)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "hi there" || resp.Model != anthropicDefaultModel {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.Usage.InputTokens != 7 || resp.Usage.OutputTokens != 3 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
	if got["_path"] != "/v1/messages" || got["_apikey"] != "sk-ant-test" {
		t.Fatalf("request = %+v", got)
	}
	if got["system"] != "be terse" {
		t.Fatalf("system = %v", got["system"])
	}
	msgs := got["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("system message should not remain in messages: %v", msgs)
	}
}

func TestAnthropicProviderSchemaInSystem(t *testing.T) {
	var got map[string]any
	srv := captureServer(t, `{"content":[{"type":"text","text":"{\"ok\":true}"}]}`, &got)
	defer srv.Close()

	p := &anthropicProvider{apiKey: "k", baseURL: srv.URL}
	resp, err := p.Complete(context.Background(), schemaReq)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != `{"ok":true}` {
		t.Fatalf("text = %q", resp.Text)
	}
	system, _ := got["system"].(string)
	if !strings.Contains(system, "JSON Schema") {
		t.Fatalf("schema instruction missing from system: %q", system)
	}
}

func TestAnthropicProviderAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"invalid x-api-key"}}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	p := &anthropicProvider{apiKey: "bad", baseURL: srv.URL}
	_, err := p.Complete(context.Background(), basicReq)
	if err == nil || !strings.Contains(err.Error(), "invalid x-api-key") {
		t.Fatalf("error should carry provider detail, got %v", err)
	}
}

func TestOpenAIProvider(t *testing.T) {
	var got map[string]any
	srv := captureServer(t, `{"choices":[{"message":{"content":"howdy"}}],"usage":{"prompt_tokens":4,"completion_tokens":2}}`, &got)
	defer srv.Close()

	req := basicReq
	req.MaxTokens = 100
	p := &openAIProvider{apiKey: "sk-x", model: "some-model", baseURL: srv.URL}
	resp, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "howdy" || resp.Usage.InputTokens != 4 {
		t.Fatalf("resp = %+v", resp)
	}
	if got["_path"] != "/v1/chat/completions" || got["_auth"] != "Bearer sk-x" {
		t.Fatalf("request = %+v", got)
	}
	// Custom base URL → the widely understood max_tokens spelling.
	if got["max_tokens"] != float64(100) {
		t.Fatalf("max_tokens = %v", got["max_tokens"])
	}
}

func TestOpenAIProviderSchemaPrependsSystem(t *testing.T) {
	var got map[string]any
	srv := captureServer(t, `{"choices":[{"message":{"content":"{\"ok\":false}"}}]}`, &got)
	defer srv.Close()

	p := &openAIProvider{apiKey: "k", model: "m", baseURL: srv.URL}
	resp, err := p.Complete(context.Background(), schemaReq)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != `{"ok":false}` {
		t.Fatalf("text = %q", resp.Text)
	}
	msgs := got["messages"].([]any)
	first := msgs[0].(map[string]any)
	if first["role"] != "system" || !strings.Contains(first["content"].(string), "JSON Schema") {
		t.Fatalf("first message should be the schema instruction: %v", first)
	}
}

func TestGoogleProvider(t *testing.T) {
	var got map[string]any
	srv := captureServer(t, `{"candidates":[{"content":{"parts":[{"text":"bonjour"}]}}],"usageMetadata":{"promptTokenCount":6,"candidatesTokenCount":2}}`, &got)
	defer srv.Close()

	p := &googleProvider{apiKey: "g-key", model: "gemini-test", baseURL: srv.URL}
	resp, err := p.Complete(context.Background(), basicReq)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "bonjour" || resp.Usage.InputTokens != 6 {
		t.Fatalf("resp = %+v", resp)
	}
	path, _ := got["_path"].(string)
	if !strings.Contains(path, "models/gemini-test:generateContent") {
		t.Fatalf("path = %q", path)
	}
	if _, hasSys := got["systemInstruction"]; !hasSys {
		t.Fatal("system instruction missing")
	}
}

func TestCLIProviderClaude(t *testing.T) {
	dir := t.TempDir()
	// Echo the stdin prompt back inside a claude-style JSON envelope so
	// the test proves both stdin plumbing and envelope parsing.
	bin := fakeBin(t, dir, "claude",
		`prompt=$(cat | tr '\n' ' ')
printf '{"result":"got: %s","is_error":false,"usage":{"input_tokens":9,"output_tokens":4}}' "$prompt"`)
	p := &cliProvider{id: ProviderClaudeCLI, binPath: bin}
	resp, err := p.Complete(context.Background(), basicReq)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Text, "got: ") || !strings.Contains(resp.Text, "hello") {
		t.Fatalf("text = %q", resp.Text)
	}
	if resp.Usage.InputTokens != 9 || resp.Usage.OutputTokens != 4 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
	if resp.Provider != ProviderClaudeCLI {
		t.Fatalf("provider = %q", resp.Provider)
	}
}

func TestCLIProviderClaudeError(t *testing.T) {
	dir := t.TempDir()
	bin := fakeBin(t, dir, "claude", `echo "Invalid API key" >&2; exit 1`)
	p := &cliProvider{id: ProviderClaudeCLI, binPath: bin}
	_, err := p.Complete(context.Background(), basicReq)
	if err == nil || !strings.Contains(err.Error(), "Invalid API key") {
		t.Fatalf("error should surface stderr, got %v", err)
	}
}

func TestCLIProviderCodexLastMessage(t *testing.T) {
	dir := t.TempDir()
	// Find the --output-last-message path among the args and write the
	// final answer there, like codex exec does.
	bin := fakeBin(t, dir, "codex",
		`out=""
while [ $# -gt 0 ]; do
  if [ "$1" = "--output-last-message" ]; then out="$2"; shift; fi
  shift
done
cat > /dev/null
echo "session log noise"
printf 'final answer' > "$out"`)
	p := &cliProvider{id: ProviderCodexCLI, binPath: bin}
	resp, err := p.Complete(context.Background(), basicReq)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "final answer" {
		t.Fatalf("text = %q", resp.Text)
	}
}

func TestCLIProviderGeminiJSONAndFallback(t *testing.T) {
	dir := t.TempDir()
	bin := fakeBin(t, dir, "gemini", `cat > /dev/null; printf '{"response":"salut"}'`)
	p := &cliProvider{id: ProviderGeminiCLI, binPath: bin}
	resp, err := p.Complete(context.Background(), basicReq)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "salut" {
		t.Fatalf("text = %q", resp.Text)
	}

	raw := fakeBin(t, dir, "gemini-old", `cat > /dev/null; printf 'plain text answer'`)
	p2 := &cliProvider{id: ProviderGeminiCLI, binPath: raw}
	resp2, err := p2.Complete(context.Background(), basicReq)
	if err != nil {
		t.Fatal(err)
	}
	if resp2.Text != "plain text answer" {
		t.Fatalf("fallback text = %q", resp2.Text)
	}
}

func TestCLIProviderSchemaExtraction(t *testing.T) {
	dir := t.TempDir()
	bin := fakeBin(t, dir, "claude",
		`cat > /dev/null
printf '{"result":"Sure! \\n{\\"ok\\":true}\\n done","is_error":false}'`)
	p := &cliProvider{id: ProviderClaudeCLI, binPath: bin}
	resp, err := p.Complete(context.Background(), schemaReq)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != `{"ok":true}` {
		t.Fatalf("text = %q", resp.Text)
	}
}
