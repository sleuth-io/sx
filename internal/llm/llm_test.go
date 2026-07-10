package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateRequest(t *testing.T) {
	valid := Request{Messages: []Message{{Role: "user", Content: "hi"}}}
	if err := validateRequest(valid); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	cases := []struct {
		name string
		req  Request
	}{
		{"no messages", Request{}},
		{"bad role", Request{Messages: []Message{{Role: "robot", Content: "x"}}}},
		{"bad schema", Request{
			Messages: []Message{{Role: "user", Content: "x"}},
			Schema:   json.RawMessage("{nope"),
		}},
	}
	for _, tc := range cases {
		if err := validateRequest(tc.req); err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
	}
}

func TestSplitSystemAndFlatten(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "be terse"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
		{Role: "user", Content: "bye"},
	}
	system, rest := splitSystem(msgs)
	if system != "be terse" {
		t.Fatalf("system = %q", system)
	}
	if len(rest) != 3 || rest[0].Role != "user" {
		t.Fatalf("rest = %+v", rest)
	}
	flat := flattenPrompt(msgs)
	for _, want := range []string{"be terse", "hello", "Assistant: hi", "bye"} {
		if !strings.Contains(flat, want) {
			t.Errorf("flattenPrompt missing %q in %q", want, flat)
		}
	}
}

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		name, in, want string
		wantErr        bool
	}{
		{"bare object", `{"a":1}`, `{"a":1}`, false},
		{"bare array", ` [1,2] `, `[1,2]`, false},
		{"fenced", "```json\n{\"a\":1}\n```", `{"a":1}`, false},
		{"preamble", `Here you go: {"a":{"b":2}} hope that helps`, `{"a":{"b":2}}`, false},
		{"no json", "sorry, I cannot", "", true},
		{"truncated", `{"a":`, "", true},
	}
	for _, tc := range cases {
		got, err := extractJSON(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("%s: expected error, got %q", tc.name, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%s: got %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
}

func TestNewConfigErrors(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		key  string
	}{
		{"empty provider", Config{}, ""},
		{"unknown provider", Config{Provider: "hal9000"}, ""},
		{"anthropic without key", Config{Provider: ProviderAnthropic}, ""},
		{"openai without model", Config{Provider: ProviderOpenAI}, "sk-x"},
		{"google without key", Config{Provider: ProviderGoogle, Model: "gemini-x"}, ""},
		{"ollama without model", Config{Provider: ProviderOllama}, ""},
	}
	for _, tc := range cases {
		if _, err := New(tc.cfg, tc.key); err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
	}
	if p, err := New(Config{Provider: ProviderAnthropic}, "sk-ant"); err != nil || p.ID() != ProviderAnthropic {
		t.Fatalf("anthropic with key: %v", err)
	}
	if p, err := New(Config{Provider: ProviderOllama, Model: "llama3"}, ""); err != nil || p.ID() != ProviderOllama {
		t.Fatalf("ollama with model: %v", err)
	}
}

// fakeBin writes an executable shell script into dir and returns its path.
func fakeBin(t *testing.T, dir, name, script string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil { // #nosec G306 -- test fixture must be executable
		t.Fatal(err)
	}
	return path
}

func stubGUIDirs(t *testing.T, dirs ...string) {
	t.Helper()
	prev := guiExtraDirs
	guiExtraDirs = func() []string { return dirs }
	t.Cleanup(func() { guiExtraDirs = prev })
}

func TestFindCLI(t *testing.T) {
	pathDir := t.TempDir()
	guiDir := t.TempDir()
	fakeBin(t, pathDir, "claude", "echo hi")
	fakeBin(t, guiDir, "codex", "echo hi")
	t.Setenv("PATH", pathDir)
	stubGUIDirs(t, guiDir)

	if p, ok := findCLI(ProviderClaudeCLI); !ok || p != filepath.Join(pathDir, "claude") {
		t.Fatalf("claude via PATH: %q %v", p, ok)
	}
	if p, ok := findCLI(ProviderCodexCLI); !ok || p != filepath.Join(guiDir, "codex") {
		t.Fatalf("codex via GUI dirs: %q %v", p, ok)
	}
	if _, ok := findCLI(ProviderGeminiCLI); ok {
		t.Fatal("gemini should not be found")
	}
}

func TestDetectProviders(t *testing.T) {
	pathDir := t.TempDir()
	fakeBin(t, pathDir, "claude", "echo hi")
	t.Setenv("PATH", pathDir)
	stubGUIDirs(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"llama3:8b"},{"name":"qwen3"}]}`))
	}))
	defer srv.Close()

	infos := DetectProviders(context.Background(), srv.URL)
	byID := map[string]ProviderInfo{}
	for _, i := range infos {
		byID[i.ID] = i
	}
	if !byID[ProviderClaudeCLI].Available {
		t.Error("claude-cli should be available")
	}
	if byID[ProviderCodexCLI].Available {
		t.Error("codex-cli should be unavailable")
	}
	ollama := byID[ProviderOllama]
	if !ollama.Available || len(ollama.Models) != 2 || ollama.Models[0] != "llama3:8b" {
		t.Errorf("ollama detection = %+v", ollama)
	}
	for _, id := range []string{ProviderAnthropic, ProviderOpenAI, ProviderGoogle} {
		if !byID[id].Available || !byID[id].NeedsAPIKey {
			t.Errorf("%s should be available and need a key", id)
		}
	}
}

func TestDetectProvidersNoOllama(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	stubGUIDirs(t)
	// Point at a server that immediately refuses.
	srv := httptest.NewServer(http.HandlerFunc(http.NotFound))
	srv.Close()
	infos := DetectProviders(context.Background(), srv.URL)
	for _, i := range infos {
		if i.ID == ProviderOllama && i.Available {
			t.Fatal("ollama should be unavailable")
		}
	}
}
