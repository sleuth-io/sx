package main

import (
	"encoding/json"
	"testing"

	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

func qualityTestApp(t *testing.T) *App {
	t.Helper()
	vdir := t.TempDir()
	runGitCmd(t, vdir, "init")
	runGitCmd(t, vdir, "config", "user.email", "alice@example.com")
	runGitCmd(t, vdir, "config", "user.name", "Alice")
	v, err := vaultpkg.NewPathVault("file://" + vdir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	return newTestAppWithVault(t, v)
}

// The bridge is a thin pass-through to the vault's QualityStore: records
// round-trip, the wrapper shape holds, and reevaluate dispatches "local"
// on a file vault.
func TestPluginQualityBridge(t *testing.T) {
	a := qualityTestApp(t)

	raw, err := a.PluginQualityGet("commit-msgs")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var doc struct {
		Evaluating bool             `json:"evaluating"`
		Records    []map[string]any `json:"records"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("wrapper not JSON: %v\n%s", err, raw)
	}
	if doc.Evaluating || len(doc.Records) != 0 {
		t.Fatalf("initial doc = %q", raw)
	}

	record := `{"at":"2026-07-14T18:00:00Z","source":"app","overall":84,` +
		`"categories":{"structure":90,"actionability":80,"content":83,"completeness":85}}`
	if err := a.PluginQualityAdd("commit-msgs", record); err != nil {
		t.Fatalf("add: %v", err)
	}
	raw, err = a.PluginQualityGet("commit-msgs")
	if err != nil {
		t.Fatalf("get after add: %v", err)
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("wrapper not JSON: %v", err)
	}
	if len(doc.Records) != 1 || doc.Records[0]["overall"].(float64) != 84 {
		t.Fatalf("doc after add = %q", raw)
	}

	latest, err := a.PluginQualityLatest()
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	var m map[string]map[string]any
	if err := json.Unmarshal([]byte(latest), &m); err != nil || m["commit-msgs"] == nil {
		t.Fatalf("latest = %q, %v", latest, err)
	}

	mode, err := a.PluginQualityReevaluate("commit-msgs")
	if err != nil || mode != vaultpkg.QualityEvalLocal {
		t.Fatalf("reevaluate = %q, %v; want local", mode, err)
	}
}
