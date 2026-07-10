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
			(*captured)["_query"] = r.URL.RawQuery
			(*captured)["_auth"] = r.Header.Get("Authorization")
			(*captured)["_apikey"] = r.Header.Get("x-api-key")
			(*captured)["_googkey"] = r.Header.Get("x-goog-api-key")
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

func TestNormalizeOpenAIBase(t *testing.T) {
	cases := []struct {
		in       string
		wantBase string
		wantOAI  bool
	}{
		{"", "https://api.openai.com", true},
		{"https://api.openai.com/v1", "https://api.openai.com", true},
		{"https://api.openai.com/v1/", "https://api.openai.com", true},
		{"https://openrouter.ai/api/v1", "https://openrouter.ai/api", false},
		{"http://localhost:8000", "http://localhost:8000", false},
	}
	for _, tc := range cases {
		base, isOAI := normalizeOpenAIBase(tc.in)
		if base != tc.wantBase || isOAI != tc.wantOAI {
			t.Errorf("normalizeOpenAIBase(%q) = %q, %v; want %q, %v",
				tc.in, base, isOAI, tc.wantBase, tc.wantOAI)
		}
	}
}

func TestOpenAIProviderV1SuffixNotDoubled(t *testing.T) {
	var got map[string]any
	srv := captureServer(t, `{"choices":[{"message":{"content":"ok"}}]}`, &got)
	defer srv.Close()

	p := &openAIProvider{apiKey: "k", model: "m", baseURL: srv.URL + "/v1"}
	if _, err := p.Complete(context.Background(), basicReq); err != nil {
		t.Fatal(err)
	}
	if got["_path"] != "/v1/chat/completions" {
		t.Fatalf("path = %v (v1 suffix should not double)", got["_path"])
	}
}

func TestSchemaValidationRejectsNonConforming(t *testing.T) {
	// Reply is well-formed JSON but misses the required field — the
	// sx.llm contract promises a VALIDATED document, so this must fail.
	srv := captureServer(t, `{"content":[{"type":"text","text":"{\"nope\":1}"}]}`, nil)
	defer srv.Close()

	req := Request{
		Messages: []Message{{Role: "user", Content: "hello"}},
		Schema:   json.RawMessage(`{"type":"object","required":["ok"],"properties":{"ok":{"type":"boolean"}}}`),
	}
	p := &anthropicProvider{apiKey: "k", baseURL: srv.URL}
	_, err := p.Complete(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatalf("non-conforming reply should fail validation, got %v", err)
	}

	// The same schema with a conforming reply passes.
	srv2 := captureServer(t, `{"content":[{"type":"text","text":"{\"ok\":true}"}]}`, nil)
	defer srv2.Close()
	p2 := &anthropicProvider{apiKey: "k", baseURL: srv2.URL}
	resp, err := p2.Complete(context.Background(), req)
	if err != nil || resp.Text != `{"ok":true}` {
		t.Fatalf("conforming reply = %q, %v", resp.Text, err)
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
	// The key must travel as a header, never in the URL (proxy logs).
	if got["_googkey"] != "g-key" {
		t.Fatalf("x-goog-api-key = %v", got["_googkey"])
	}
	if q, _ := got["_query"].(string); strings.Contains(q, "key=") {
		t.Fatalf("API key leaked into the query string: %q", q)
	}
	if _, hasSys := got["systemInstruction"]; !hasSys {
		t.Fatal("system instruction missing")
	}
}

func TestCLIProviderClaude(t *testing.T) {
	dir := t.TempDir()
	// Echo the stdin prompt back inside a claude-style JSON envelope so
	// the test proves both stdin plumbing and envelope parsing.
	// The fake asserts the no-tools flag is on the command line — the
	// sandbox hardening must fail LOUDLY if the flag is ever dropped or
	// renamed, not silently re-enable the agent's tools.
	bin := fakeBin(t, dir, "claude",
		`ok=0; prev=""
for a in "$@"; do
  if [ "$prev" = "--tools" ] && [ -z "$a" ]; then ok=1; fi
  prev="$a"
done
[ "$ok" = 1 ] || { echo 'missing --tools ""' >&2; exit 9; }
prompt=$(cat | tr '\n' ' ')
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
	// final answer there, like codex exec does — and assert the
	// read-only sandbox flag is present (hardening must fail loudly).
	bin := fakeBin(t, dir, "codex",
		`out=""; sandbox=""
while [ $# -gt 0 ]; do
  if [ "$1" = "--output-last-message" ]; then out="$2"; shift; fi
  if [ "$1" = "--sandbox" ]; then sandbox="$2"; shift; fi
  shift
done
[ "$sandbox" = "read-only" ] || { echo 'missing --sandbox read-only' >&2; exit 9; }
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
	// Assert approval-mode is pinned (never yolo) before replying.
	bin := fakeBin(t, dir, "gemini",
		`mode=""; prev=""
for a in "$@"; do
  if [ "$prev" = "--approval-mode" ]; then mode="$a"; fi
  prev="$a"
done
[ "$mode" = "default" ] || { echo 'missing --approval-mode default' >&2; exit 9; }
cat > /dev/null; printf '{"response":"salut"}'`)
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
