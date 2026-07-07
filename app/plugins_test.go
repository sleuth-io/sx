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

func TestPluginEnabledStateTriState(t *testing.T) {
	a := pluginTestApp(t)

	// Never configured: defaults apply (built-ins on) and Configured=false.
	state, err := a.EnabledPlugins()
	if err != nil || state.Configured {
		t.Fatalf("initial state = %+v, %v; want unconfigured", state, err)
	}

	// First toggle materializes the built-in defaults so disabling one
	// doesn't silently disable the rest.
	if err := a.SetPluginEnabled("publish-doctor", false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	state, err = a.EnabledPlugins()
	if err != nil || !state.Configured {
		t.Fatalf("state after toggle = %+v, %v; want configured", state, err)
	}
	if len(state.Enabled) != 1 || state.Enabled[0] != "library-dashboard" {
		t.Fatalf("enabled = %v, want [library-dashboard]", state.Enabled)
	}

	// Re-enable adds it back.
	if err := a.SetPluginEnabled("publish-doctor", true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	state, _ = a.EnabledPlugins()
	if len(state.Enabled) != 2 {
		t.Fatalf("enabled = %v, want both built-ins", state.Enabled)
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
