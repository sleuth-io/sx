package main

import (
	"context"
	"testing"

	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

func searchTestApp(t *testing.T) *App {
	t.Helper()
	a := pluginTestApp(t)
	a.ctx = context.Background()
	vdir := t.TempDir()
	runGitCmd(t, vdir, "init")
	runGitCmd(t, vdir, "config", "user.email", "alice@example.com")
	runGitCmd(t, vdir, "config", "user.name", "Alice")
	v, err := vaultpkg.NewPathVault("file://" + vdir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	a.vault = v

	publish := func(name, body string) {
		draft, err := a.CreateDraftFromFiles(name, []AssetFile{{Path: "SKILL.md", Content: body}})
		if err != nil {
			t.Fatalf("draft %s: %v", name, err)
		}
		if _, err := a.PublishDraft(draft.ID); err != nil {
			t.Fatalf("publish %s: %v", name, err)
		}
	}
	publish("playwright-guide", "---\nname: playwright-guide\n---\n# Playwright\n\nUse role selectors. Debug flaky selector timing with the trace viewer.")
	publish("sql-notes", "---\nname: sql-notes\n---\n# Migrations\n\nExpand and contract. Never lock a large table.")
	return a
}

func TestSearchAssetContent(t *testing.T) {
	a := searchTestApp(t)

	// Body term match with excerpt + highlight parts.
	matches, err := a.SearchAssetContent("flaky selector")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(matches) != 1 || matches[0].Name != "playwright-guide" {
		t.Fatalf("matches = %+v", matches)
	}
	if matches[0].Match == "" || matches[0].Matches == 0 {
		t.Fatalf("excerpt not populated: %+v", matches[0])
	}

	// AND semantics: all terms must appear.
	if m, _ := a.SearchAssetContent("flaky migrations"); len(m) != 0 {
		t.Fatalf("AND semantics violated: %+v", m)
	}

	// Quoted phrase must match exactly.
	if m, _ := a.SearchAssetContent(`"never lock a large table"`); len(m) != 1 || m[0].Name != "sql-notes" {
		t.Fatalf("phrase search = %+v", m)
	}
	if m, _ := a.SearchAssetContent(`"never lock a small table"`); len(m) != 0 {
		t.Fatalf("phrase should not match")
	}

	// Empty / too-short queries return nothing rather than everything.
	if m, _ := a.SearchAssetContent("  a "); len(m) != 0 {
		t.Fatalf("short query matched: %+v", m)
	}
}
