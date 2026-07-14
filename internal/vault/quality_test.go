package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func qualityRecord(overall int) string {
	return fmt.Sprintf(
		`{"at":"2026-07-14T18:00:00Z","source":"app","overall":%d,`+
			`"categories":{"structure":90,"actionability":80,"content":83,"completeness":85},`+
			`"summary":"solid skill","insights":{"strengths":["a"],"improvements":["b"],"recommendations":["c"]}}`,
		overall,
	)
}

func decodeQualityWrapper(t *testing.T, raw string) (bool, []map[string]any) {
	t.Helper()
	var doc struct {
		Evaluating bool             `json:"evaluating"`
		Records    []map[string]any `json:"records"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("wrapper is not a JSON object: %v\n%s", err, raw)
	}
	if doc.Records == nil {
		t.Fatalf("records must be an array, never null:\n%s", raw)
	}
	return doc.Evaluating, doc.Records
}

// Records round-trip through .sx/quality/<asset>.json, newest first,
// wrapped in {evaluating, records}; junk names and malformed records
// are refused.
func TestQualityRoundTrip(t *testing.T) {
	v := appPluginTestVault(t, nil)
	ctx := context.Background()

	raw, err := v.GetQuality(ctx, "commit-msgs")
	if err != nil {
		t.Fatalf("initial get: %v", err)
	}
	if evaluating, records := decodeQualityWrapper(t, raw); evaluating || len(records) != 0 {
		t.Fatalf("initial wrapper = %q; want evaluating=false, no records", raw)
	}

	if err := v.AddQuality(ctx, "commit-msgs", qualityRecord(60)); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := v.AddQuality(ctx, "commit-msgs", qualityRecord(84)); err != nil {
		t.Fatalf("add second: %v", err)
	}

	raw, err = v.GetQuality(ctx, "commit-msgs")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	_, records := decodeQualityWrapper(t, raw)
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
	if newest := records[0]["overall"].(float64); newest != 84 {
		t.Fatalf("newest first violated: overall = %v", newest)
	}

	// Another asset's history is invisible.
	raw, _ = v.GetQuality(ctx, "other-skill")
	if _, records := decodeQualityWrapper(t, raw); len(records) != 0 {
		t.Fatalf("cross-asset read = %q", raw)
	}

	if err := v.AddQuality(ctx, "../evil", qualityRecord(50)); err == nil {
		t.Fatalf("path-shaped asset name accepted")
	}
	if err := v.AddQuality(ctx, "commit-msgs", "not json"); err == nil {
		t.Fatalf("non-JSON record accepted")
	}
	if err := v.AddQuality(ctx, "commit-msgs", `{"categories":{}}`); err == nil {
		t.Fatalf("overall-less record accepted")
	}
	if err := v.AddQuality(ctx, "commit-msgs", `{"overall":"high","categories":{}}`); err == nil {
		t.Fatalf("non-numeric overall accepted")
	}
	if err := v.AddQuality(ctx, "commit-msgs", `{"overall":140,"categories":{}}`); err == nil {
		t.Fatalf("out-of-range overall accepted")
	}
	if err := v.AddQuality(ctx, "commit-msgs", `{"overall":84}`); err == nil {
		t.Fatalf("categories-less record accepted")
	}
	if err := v.AddQuality(ctx, "commit-msgs", `{"overall":84,"categories":null}`); err == nil {
		t.Fatalf("null categories accepted")
	}
	fat := `{"overall":84,"categories":{},"pad":"` + strings.Repeat("x", maxQualityRecordBytes) + `"}`
	if err := v.AddQuality(ctx, "commit-msgs", fat); err == nil {
		t.Fatalf("oversized record accepted")
	}
}

// History is capped at maxQualityRecords, dropping the oldest.
func TestQualityCapRetention(t *testing.T) {
	v := appPluginTestVault(t, nil)
	ctx := context.Background()

	for i := range maxQualityRecords + 5 {
		if err := v.AddQuality(ctx, "busy-skill", qualityRecord(i)); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}
	raw, err := v.GetQuality(ctx, "busy-skill")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	_, records := decodeQualityWrapper(t, raw)
	if len(records) != maxQualityRecords {
		t.Fatalf("got %d records, want cap %d", len(records), maxQualityRecords)
	}
	if newest := records[0]["overall"].(float64); newest != float64(maxQualityRecords+4) {
		t.Fatalf("newest record wrong after cap: %v", newest)
	}
}

// LatestQuality maps every evaluated asset to its newest record only.
func TestLatestQuality(t *testing.T) {
	v := appPluginTestVault(t, nil)
	ctx := context.Background()

	if got, err := v.LatestQuality(ctx); err != nil || got != "" {
		t.Fatalf("initial latest = %q, %v; want empty", got, err)
	}
	for _, add := range []struct {
		asset   string
		overall int
	}{{"skill-a", 40}, {"skill-a", 70}, {"skill-b", 55}} {
		if err := v.AddQuality(ctx, add.asset, qualityRecord(add.overall)); err != nil {
			t.Fatalf("add: %v", err)
		}
	}

	raw, err := v.LatestQuality(ctx)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	var latest map[string]map[string]any
	if err := json.Unmarshal([]byte(raw), &latest); err != nil {
		t.Fatalf("latest is not an object: %v", err)
	}
	if len(latest) != 2 {
		t.Fatalf("got %d assets, want 2", len(latest))
	}
	if got := latest["skill-a"]["overall"].(float64); got != 70 {
		t.Fatalf("skill-a latest overall = %v, want 70", got)
	}
}

// File vaults never evaluate themselves: reevaluate always dispatches to
// the caller ("local").
func TestQualityReevaluateIsLocal(t *testing.T) {
	v := appPluginTestVault(t, nil)
	mode, err := v.ReevaluateQuality(context.Background(), "commit-msgs")
	if err != nil || mode != QualityEvalLocal {
		t.Fatalf("reevaluate = %q, %v; want %q", mode, err, QualityEvalLocal)
	}
}

// A corrupt quality file starts a fresh list instead of bricking the
// asset, and the write path preserves the corrupt bytes as .bad first —
// plain and synced folders have no git history to fall back on.
func TestQualityCorruptFileRecovers(t *testing.T) {
	v := appPluginTestVault(t, nil)
	ctx := context.Background()
	if err := v.AddQuality(ctx, "flaky", qualityRecord(30)); err != nil {
		t.Fatalf("add: %v", err)
	}
	path, err := qualityPath(v.repoPath, "flaky")
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	if err := os.WriteFile(path, []byte("{{corrupt"), 0o644); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	// Reads are non-destructive: empty records, corrupt bytes untouched.
	raw, err := v.GetQuality(ctx, "flaky")
	if err != nil {
		t.Fatalf("corrupt get: %v", err)
	}
	if _, records := decodeQualityWrapper(t, raw); len(records) != 0 {
		t.Fatalf("corrupt get = %q; want empty records", raw)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("read destroyed the corrupt file: %v", err)
	}

	if err := v.AddQuality(ctx, "flaky", qualityRecord(65)); err != nil {
		t.Fatalf("add after corrupt: %v", err)
	}
	raw, err = v.GetQuality(ctx, "flaky")
	if err != nil {
		t.Fatalf("get after recovery: %v", err)
	}
	if _, records := decodeQualityWrapper(t, raw); len(records) != 1 {
		t.Fatalf("got %d records after recovery, want 1", len(records))
	}
	bad, err := os.ReadFile(path + ".bad")
	if err != nil {
		t.Fatalf("corrupt bytes not preserved as .bad: %v", err)
	}
	if string(bad) != "{{corrupt" {
		t.Fatalf(".bad content = %q", bad)
	}
	// The .bad file is invisible to the latest-per-asset scan.
	raw, err = v.LatestQuality(ctx)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	var latest map[string]map[string]any
	if err := json.Unmarshal([]byte(raw), &latest); err != nil {
		t.Fatalf("latest is not an object: %v", err)
	}
	if len(latest) != 1 || latest["flaky"] == nil {
		t.Fatalf("latest after recovery = %v", latest)
	}
}
