package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/llm"
)

func TestLLMConfigRoundTrip(t *testing.T) {
	a := pluginTestApp(t)
	store := &memSecretStore{values: map[string]string{}}
	defer setPluginSecretStore(store)()

	status, err := a.LLMStatus()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Config.Provider != "" {
		t.Fatalf("unconfigured provider = %q", status.Config.Provider)
	}
	if len(status.Providers) < 6 {
		t.Fatalf("expected all provider options, got %d", len(status.Providers))
	}

	cfg := llm.Config{Provider: llm.ProviderOpenAI, Model: "some-model", BaseURL: "https://example.test"}
	if err := a.LLMSetConfig(cfg); err != nil {
		t.Fatalf("set config: %v", err)
	}
	status, err = a.LLMStatus()
	if err != nil || status.Config != cfg {
		t.Fatalf("config round trip = %+v, %v", status.Config, err)
	}

	// Clearing removes the file entirely.
	if err := a.LLMSetConfig(llm.Config{}); err != nil {
		t.Fatalf("clear config: %v", err)
	}
	path, _ := llmConfigPath()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("clearing the config should remove llm.json")
	}

	if err := a.LLMSetConfig(llm.Config{Provider: "hal9000"}); err == nil {
		t.Fatal("unknown provider should be rejected")
	}
}

func TestLLMSetAPIKey(t *testing.T) {
	a := pluginTestApp(t)
	store := &memSecretStore{values: map[string]string{}}
	defer setPluginSecretStore(store)()

	if err := a.LLMSetAPIKey(llm.ProviderAnthropic, "sk-ant-test"); err != nil {
		t.Fatalf("set key: %v", err)
	}
	if store.values["llm-provider/anthropic"] != "sk-ant-test" {
		t.Fatalf("keyring account wrong: %v", store.values)
	}
	status, err := a.LLMStatus()
	if err != nil || !status.KeySet[llm.ProviderAnthropic] || status.KeySet[llm.ProviderOpenAI] {
		t.Fatalf("keySet = %+v, %v", status.KeySet, err)
	}

	if err := a.LLMSetAPIKey(llm.ProviderAnthropic, ""); err != nil {
		t.Fatalf("delete key: %v", err)
	}
	if _, ok := store.values["llm-provider/anthropic"]; ok {
		t.Fatal("empty value should delete the key")
	}

	if err := a.LLMSetAPIKey("hal9000", "k"); err == nil {
		t.Fatal("unknown provider should be rejected")
	}
	if err := a.LLMSetAPIKey(llm.ProviderOpenAI, strings.Repeat("x", maxPluginSecretBytes+1)); err == nil {
		t.Fatal("oversize key should be rejected")
	}
}

// llmTestServer mimics an OpenAI-compatible endpoint.
func llmTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-live" {
			http.Error(w, "bad key", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"pong"}}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`))
	}))
}

func TestLLMComplete(t *testing.T) {
	a := pluginTestApp(t)
	store := &memSecretStore{values: map[string]string{}}
	defer setPluginSecretStore(store)()

	// Nothing configured yet: a clear, actionable error.
	if _, err := a.LLMComplete("skill-doctor", `{"messages":[{"role":"user","content":"ping"}]}`); err == nil ||
		!strings.Contains(err.Error(), "no AI provider configured") {
		t.Fatalf("unconfigured error = %v", err)
	}

	srv := llmTestServer(t)
	defer srv.Close()
	if err := a.LLMSetConfig(llm.Config{Provider: llm.ProviderOpenAI, Model: "m", BaseURL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	if err := a.LLMSetAPIKey(llm.ProviderOpenAI, "sk-live"); err != nil {
		t.Fatal(err)
	}

	out, err := a.LLMComplete("skill-doctor", `{"messages":[{"role":"user","content":"ping"}]}`)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	var resp llm.Response
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if resp.Text != "pong" || resp.Provider != llm.ProviderOpenAI || resp.Usage.InputTokens != 3 {
		t.Fatalf("resp = %+v", resp)
	}

	if _, err := a.LLMComplete("Bad ID!", `{}`); err == nil {
		t.Fatal("invalid plugin id should be rejected")
	}
	if _, err := a.LLMComplete("skill-doctor", `{not json`); err == nil {
		t.Fatal("invalid request JSON should be rejected")
	}
}

func TestLLMTestButton(t *testing.T) {
	a := pluginTestApp(t)
	store := &memSecretStore{values: map[string]string{}}
	defer setPluginSecretStore(store)()

	srv := llmTestServer(t)
	defer srv.Close()
	if err := a.LLMSetConfig(llm.Config{Provider: llm.ProviderOpenAI, Model: "m", BaseURL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	if err := a.LLMSetAPIKey(llm.ProviderOpenAI, "sk-live"); err != nil {
		t.Fatal(err)
	}
	got, err := a.LLMTest()
	if err != nil || got != "pong" {
		t.Fatalf("LLMTest = %q, %v", got, err)
	}
}
