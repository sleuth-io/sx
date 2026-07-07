package main

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
)

// memSecretStore is an in-memory keyring; failing=true simulates a
// headless machine with no secret service.
type memSecretStore struct {
	values  map[string]string
	failing bool
}

func (m *memSecretStore) Set(account, value string) error {
	if m.failing {
		return errors.New("no keyring backend")
	}
	m.values[account] = value
	return nil
}

func (m *memSecretStore) Get(account string) (string, bool, error) {
	if m.failing {
		return "", false, errors.New("no keyring backend")
	}
	v, ok := m.values[account]
	return v, ok, nil
}

func (m *memSecretStore) Delete(account string) error {
	if m.failing {
		return errors.New("no keyring backend")
	}
	delete(m.values, account)
	return nil
}

func TestPluginSecretsKeyring(t *testing.T) {
	a := pluginTestApp(t)
	store := &memSecretStore{values: map[string]string{}}
	defer setPluginSecretStore(store)()

	// Unset reads as empty, not an error.
	if got, err := a.PluginSecretGet("claude-assist", "api-key"); err != nil || got != "" {
		t.Fatalf("get unset = %q, %v", got, err)
	}
	if err := a.PluginSecretSet("claude-assist", "api-key", "sk-test-123"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got, err := a.PluginSecretGet("claude-assist", "api-key"); err != nil || got != "sk-test-123" {
		t.Fatalf("get = %q, %v", got, err)
	}
	// Accounts are namespaced profile/extension/name.
	if _, ok := store.values["default/claude-assist/api-key"]; !ok {
		t.Fatalf("keyring account not namespaced: %v", store.values)
	}
	// Another extension can't read it through the bridge (different
	// account key), and the value never lands on disk.
	if got, _ := a.PluginSecretGet("other-ext", "api-key"); got != "" {
		t.Fatalf("cross-extension read = %q", got)
	}
	if path, _ := a.secretFallbackPath("claude-assist"); fileExists(path) {
		t.Fatalf("fallback file written while keyring healthy")
	}

	// Empty value deletes.
	if err := a.PluginSecretSet("claude-assist", "api-key", ""); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got, _ := a.PluginSecretGet("claude-assist", "api-key"); got != "" {
		t.Fatalf("get after delete = %q", got)
	}
}

func TestPluginSecretsValidation(t *testing.T) {
	a := pluginTestApp(t)
	store := &memSecretStore{values: map[string]string{}}
	defer setPluginSecretStore(store)()

	if _, err := a.PluginSecretGet("../evil", "k"); err == nil {
		t.Fatalf("path-shaped id accepted")
	}
	if err := a.PluginSecretSet("claude-assist", "Bad Name", "v"); err == nil {
		t.Fatalf("invalid secret name accepted")
	}
	if err := a.PluginSecretSet("claude-assist", "k", strings.Repeat("x", maxPluginSecretBytes+1)); err == nil {
		t.Fatalf("oversized secret accepted")
	}
}

func TestPluginSecretsFileFallback(t *testing.T) {
	a := pluginTestApp(t)
	store := &memSecretStore{values: map[string]string{}, failing: true}
	defer setPluginSecretStore(store)()

	if err := a.PluginSecretSet("claude-assist", "api-key", "sk-fallback"); err != nil {
		t.Fatalf("set with dead keyring: %v", err)
	}
	if got, err := a.PluginSecretGet("claude-assist", "api-key"); err != nil || got != "sk-fallback" {
		t.Fatalf("get from fallback = %q, %v", got, err)
	}
	path, _ := a.secretFallbackPath("claude-assist")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("fallback file missing: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("fallback perms = %v, want 0600", info.Mode().Perm())
	}

	// Deleting the last secret removes the file entirely.
	if err := a.PluginSecretSet("claude-assist", "api-key", ""); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if fileExists(path) {
		t.Fatalf("fallback file survives after last delete")
	}

	// A recovered keyring wins over (and clears) the stale fallback.
	if err := a.PluginSecretSet("claude-assist", "api-key", "sk-old"); err != nil {
		t.Fatalf("re-set with dead keyring: %v", err)
	}
	store.failing = false
	if err := a.PluginSecretSet("claude-assist", "api-key", "sk-new"); err != nil {
		t.Fatalf("set with healthy keyring: %v", err)
	}
	if fileExists(path) {
		t.Fatalf("stale fallback copy kept after keyring write")
	}
	if got, _ := a.PluginSecretGet("claude-assist", "api-key"); got != "sk-new" {
		t.Fatalf("get = %q, want sk-new", got)
	}
}

func TestNetPermissionValidation(t *testing.T) {
	valid := []string{"net:api.anthropic.com", "net:localhost", "net:my-host.example.co"}
	for _, p := range valid {
		if !isKnownPluginPermission(p) {
			t.Errorf("%q rejected", p)
		}
	}
	invalid := []string{
		"net:", "net:HTTPS.COM", "net:https://api.anthropic.com",
		"net:api.anthropic.com:443", "net:api.anthropic.com/v1",
		"net:.leading.dot", "net:trailing.dot.", "network:foo",
	}
	for _, p := range invalid {
		if isKnownPluginPermission(p) {
			t.Errorf("%q accepted", p)
		}
	}
	if !isKnownPluginPermission("secrets") {
		t.Errorf("secrets rejected")
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
