package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func pluginTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SX_CONFIG_DIR", dir)
	// pluginDataDir needs a loadable config; seed a minimal path profile.
	seedConfig(t, dir)
	return &App{}
}

func seedConfig(t *testing.T, dir string) {
	t.Helper()
	cfg := `{"type":"path","repositoryUrl":"file:///tmp/x","defaultProfile":"default","activeProfiles":["default"],"profiles":{"default":{"type":"path","repositoryUrl":"file:///tmp/x"}}}`
	if err := writeFile(dir+"/config.json", cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func TestPluginStorageRoundTrip(t *testing.T) {
	a := pluginTestApp(t)

	// Nothing saved yet.
	if got, err := a.PluginLoadData("library-dashboard"); err != nil || got != "" {
		t.Fatalf("load empty = %q, %v", got, err)
	}
	if err := a.PluginSaveData("library-dashboard", `{"pins":["a"]}`); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got, err := a.PluginLoadData("library-dashboard"); err != nil || got != `{"pins":["a"]}` {
		t.Fatalf("load = %q, %v", got, err)
	}

	// Ids are validated on every entry point — a path-shaped id must not
	// become a path.
	if _, err := a.PluginLoadData("../evil"); err == nil {
		t.Fatalf("path-shaped id accepted on load")
	}
	if err := a.PluginSaveData("Evil_ID", "{}"); err == nil {
		t.Fatalf("invalid id accepted on save")
	}
	if err := a.PluginSaveData("big", strings.Repeat("x", maxPluginDataBytes+1)); err == nil {
		t.Fatalf("oversized data accepted")
	}
}

func TestPluginDecisions(t *testing.T) {
	a := pluginTestApp(t)

	// No decisions yet: unknown ids fall back to their default frontend-
	// side (built-ins on), so the map starts empty rather than seeded.
	decisions, err := a.PluginDecisions()
	if err != nil || len(decisions) != 0 {
		t.Fatalf("initial decisions = %v, %v; want empty", decisions, err)
	}

	// Intent persists per id; other ids stay undecided (their defaults
	// keep applying — how a future built-in auto-enables for existing
	// users).
	if err := a.SetPluginDecision("publish-doctor", false); err != nil {
		t.Fatalf("set: %v", err)
	}
	decisions, err = a.PluginDecisions()
	if err != nil || len(decisions) != 1 || decisions["publish-doctor"] != false {
		t.Fatalf("decisions = %v, %v; want publish-doctor:false only", decisions, err)
	}
	if err := a.SetPluginDecision("../evil", true); err == nil {
		t.Fatalf("invalid id accepted")
	}
}

func TestPluginConsents(t *testing.T) {
	a := pluginTestApp(t)
	consents, err := a.PluginConsents()
	if err != nil || len(consents) != 0 {
		t.Fatalf("initial consents = %v, %v", consents, err)
	}
	if err := a.SetPluginConsent("publish-doctor", []string{"events", "assets:read"}); err != nil {
		t.Fatalf("consent: %v", err)
	}
	consents, _ = a.PluginConsents()
	got := consents["publish-doctor"]
	if len(got) != 2 || got[0] != "assets:read" {
		t.Fatalf("consents = %v, want sorted permission set", consents)
	}
}

func TestAppVersionNonEmpty(t *testing.T) {
	a := &App{}
	if a.AppVersion() == "" {
		t.Fatalf("AppVersion must never be empty (sx.app.version contract)")
	}
}

func TestUsageCutoffCaps(t *testing.T) {
	now := time.Now()
	if c := usageCutoff(0); now.Sub(c) < 29*24*time.Hour || now.Sub(c) > 31*24*time.Hour {
		t.Fatalf("default cutoff = %v, want ~30d", now.Sub(c))
	}
	if c := usageCutoff(10_000); now.Sub(c) > 366*24*time.Hour {
		t.Fatalf("cutoff uncapped: %v", now.Sub(c))
	}
}
