package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func benchmarkRecord(delta float64) string {
	return fmt.Sprintf(
		`{"at":"2026-07-12T18:00:00Z","source":"app","executor":{"provider":"claude-cli","model":"m"},`+
			`"runs_per_config":1,"summary":{"with_skill":{},"without_skill":{},"delta":{"pass_rate":%g}}}`,
		delta,
	)
}

func decodeList(t *testing.T, raw string) []map[string]any {
	t.Helper()
	if raw == "" {
		return nil
	}
	var out []map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("list is not a JSON array: %v\n%s", err, raw)
	}
	return out
}

// Records round-trip through .sx/benchmarks/<asset>.json, newest first,
// capped; junk names and malformed records are refused.
func TestBenchmarksRoundTrip(t *testing.T) {
	v := appPluginTestVault(t, nil)
	ctx := context.Background()

	if got, err := v.ListBenchmarks(ctx, "commit-msgs"); err != nil || got != "" {
		t.Fatalf("initial list = %q, %v; want empty", got, err)
	}

	if err := v.AddBenchmark(ctx, "commit-msgs", benchmarkRecord(0.1)); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := v.AddBenchmark(ctx, "commit-msgs", benchmarkRecord(0.3)); err != nil {
		t.Fatalf("add second: %v", err)
	}

	raw, err := v.ListBenchmarks(ctx, "commit-msgs")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	records := decodeList(t, raw)
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
	newestDelta := records[0]["summary"].(map[string]any)["delta"].(map[string]any)["pass_rate"].(float64)
	if newestDelta != 0.3 {
		t.Fatalf("newest first violated: delta = %v", newestDelta)
	}

	// Another asset's history is invisible.
	if got, _ := v.ListBenchmarks(ctx, "other-skill"); got != "" {
		t.Fatalf("cross-asset read = %q", got)
	}

	if err := v.AddBenchmark(ctx, "../evil", benchmarkRecord(0.1)); err == nil {
		t.Fatalf("path-shaped asset name accepted")
	}
	if err := v.AddBenchmark(ctx, "commit-msgs", "not json"); err == nil {
		t.Fatalf("non-JSON record accepted")
	}
	if err := v.AddBenchmark(ctx, "commit-msgs", `{"no_summary":true}`); err == nil {
		t.Fatalf("summary-less record accepted")
	}
	fat := `{"summary":{},"pad":"` + strings.Repeat("x", maxBenchmarkRecordBytes) + `"}`
	if err := v.AddBenchmark(ctx, "commit-msgs", fat); err == nil {
		t.Fatalf("oversized record accepted")
	}
}

// History is capped at maxBenchmarkRecords, dropping the oldest.
func TestBenchmarksCapRetention(t *testing.T) {
	v := appPluginTestVault(t, nil)
	ctx := context.Background()

	for i := range maxBenchmarkRecords + 5 {
		if err := v.AddBenchmark(ctx, "busy-skill", benchmarkRecord(float64(i)/100)); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}
	records := decodeList(t, mustList(t, v, "busy-skill"))
	if len(records) != maxBenchmarkRecords {
		t.Fatalf("got %d records, want cap %d", len(records), maxBenchmarkRecords)
	}
	newest := records[0]["summary"].(map[string]any)["delta"].(map[string]any)["pass_rate"].(float64)
	if newest != float64(maxBenchmarkRecords+4)/100 {
		t.Fatalf("newest record wrong after cap: %v", newest)
	}
}

// LatestBenchmarks maps every benchmarked asset to its newest record only.
func TestLatestBenchmarks(t *testing.T) {
	v := appPluginTestVault(t, nil)
	ctx := context.Background()

	if got, err := v.LatestBenchmarks(ctx); err != nil || got != "" {
		t.Fatalf("initial latest = %q, %v; want empty", got, err)
	}
	for _, add := range []struct {
		asset string
		delta float64
	}{{"skill-a", 0.1}, {"skill-a", 0.4}, {"skill-b", 0.05}} {
		if err := v.AddBenchmark(ctx, add.asset, benchmarkRecord(add.delta)); err != nil {
			t.Fatalf("add: %v", err)
		}
	}

	raw, err := v.LatestBenchmarks(ctx)
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
	aDelta := latest["skill-a"]["summary"].(map[string]any)["delta"].(map[string]any)["pass_rate"].(float64)
	if aDelta != 0.4 {
		t.Fatalf("skill-a latest delta = %v, want 0.4", aDelta)
	}
}

// A corrupt benchmarks file starts a fresh list instead of bricking the asset.
func TestBenchmarksCorruptFileRecovers(t *testing.T) {
	v := appPluginTestVault(t, nil)
	ctx := context.Background()
	if err := v.AddBenchmark(ctx, "flaky", benchmarkRecord(0.2)); err != nil {
		t.Fatalf("add: %v", err)
	}
	path, err := benchmarksPath(v.repoPath, "flaky")
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	if err := os.WriteFile(path, []byte("{{corrupt"), 0o644); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	if got, err := v.ListBenchmarks(ctx, "flaky"); err != nil || got != "" {
		t.Fatalf("corrupt list = %q, %v; want empty, nil", got, err)
	}
	if err := v.AddBenchmark(ctx, "flaky", benchmarkRecord(0.5)); err != nil {
		t.Fatalf("add after corrupt: %v", err)
	}
	if got := decodeList(t, mustList(t, v, "flaky")); len(got) != 1 {
		t.Fatalf("got %d records after recovery, want 1", len(got))
	}
}

func mustList(t *testing.T, v *PathVault, asset string) string {
	t.Helper()
	raw, err := v.ListBenchmarks(context.Background(), asset)
	if err != nil {
		t.Fatalf("list %s: %v", asset, err)
	}
	return raw
}
