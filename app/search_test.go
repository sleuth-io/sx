package main

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

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

// Excerpts must stay rune-safe: multi-byte characters near the hit (an
// em-dash, an accented word, a length-changing İ) must never misalign
// the highlight or leave a split rune at a window boundary.
func TestSearchExcerptRuneSafety(t *testing.T) {
	a := searchTestApp(t)
	body := "---\nname: unicode-notes\n---\n# Notes\n\n" +
		strings.Repeat("é", 80) + " — İstanbul café rules — the flaky selector fix lives here — " +
		strings.Repeat("—", 80)
	draft, err := a.CreateDraftFromFiles("unicode-notes", []AssetFile{{Path: "SKILL.md", Content: body}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.PublishDraft(draft.ID); err != nil {
		t.Fatal(err)
	}

	matches, err := a.SearchAssetContent("flaky")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var m *ContentMatch
	for i := range matches {
		if matches[i].Name == "unicode-notes" {
			m = &matches[i]
		}
	}
	if m == nil {
		t.Fatalf("unicode-notes not matched: %+v", matches)
	}
	if m.Match != "flaky" {
		t.Fatalf("highlight misaligned: Match = %q", m.Match)
	}
	for _, part := range []string{m.Before, m.Match, m.After} {
		if !utf8.ValidString(part) || strings.ContainsRune(part, '�') {
			t.Fatalf("excerpt part not rune-safe: %q", part)
		}
	}
}

// The cache must track the CURRENT asset set: republishing evicts the
// superseded revision's entry, deleting purges the asset's entry.
func TestSearchCacheEviction(t *testing.T) {
	a := searchTestApp(t)

	if _, err := a.SearchAssetContent("selectors"); err != nil {
		t.Fatal(err)
	}
	keyV1, ok := a.searchCacheKeys.Load("playwright-guide")
	if !ok {
		t.Fatalf("no cache key after first search")
	}
	if _, ok := a.searchCache.Load(keyV1.(string)); !ok {
		t.Fatalf("no cache entry after first search")
	}

	// Republish → new revision → the old entry must go.
	draft, err := a.CreateDraftFromFiles("playwright-guide", []AssetFile{{
		Path: "SKILL.md", Content: "---\nname: playwright-guide\n---\n# Playwright\n\nRewritten selectors guide.",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.PublishDraft(draft.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SearchAssetContent("selectors"); err != nil {
		t.Fatal(err)
	}
	keyV2, _ := a.searchCacheKeys.Load("playwright-guide")
	if keyV2 == keyV1 {
		t.Fatalf("cache key did not advance after republish")
	}
	if _, ok := a.searchCache.Load(keyV1.(string)); ok {
		t.Fatalf("superseded revision still cached")
	}

	// Delete → both maps purge.
	if err := a.DeleteAssets([]string{"playwright-guide"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.searchCacheKeys.Load("playwright-guide"); ok {
		t.Fatalf("deleted asset still has a cache key")
	}
	if _, ok := a.searchCache.Load(keyV2.(string)); ok {
		t.Fatalf("deleted asset's markdown still cached")
	}
}
