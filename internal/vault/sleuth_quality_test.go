package vault

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// realistic pulse evaluation_result document (asset_evaluation_tool.py
// output), category scores in the current {tier, score, weight} shape.
const serverEvalDoc = `{
	"overall_confidence": 0.8246,
	"category_scores": {
		"structure": {"tier": "Good", "score": 78.4, "weight": 0.24},
		"actionability": {"tier": "Excellent", "score": 90.0, "weight": 0.36},
		"content_quality": {"tier": "Good", "score": 80.2, "weight": 0.27},
		"completeness": {"tier": "Needs Work", "score": 0.0, "weight": 0.13}
	},
	"factors": {"specificity": {"score": 90, "tier": "Excellent", "justification": "clear"}},
	"reasoning": "A solid, well-structured skill.",
	"llm_strengths": ["concrete examples"],
	"llm_weaknesses": ["no troubleshooting section"],
	"llm_recommendations": ["add a setup section"],
	"file_count": 2,
	"word_count": 609
}`

func qualityNode(slug string, evaluating bool, evalResult *string) map[string]any {
	source := map[string]any{"__typename": "AssetManagedSource"}
	if evalResult != nil {
		source["confidenceScore"] = 82.46
		source["evaluationResult"] = *evalResult
	}
	return map[string]any{
		"__typename": "Skill",
		"id":         "gid://" + slug,
		"slug":       slug,
		"updatedAt":  "2026-07-14T10:00:00Z",
		"evaluating": evaluating,
		"source":     source,
	}
}

func qualityAssetsResponse(nodes ...map[string]any) map[string]any {
	if nodes == nil {
		nodes = []map[string]any{}
	}
	return map[string]any{"vault": map[string]any{"assets": map[string]any{
		"pageInfo": map[string]any{"hasNextPage": false, "endCursor": nil},
		"nodes":    nodes,
	}}}
}

// The server evaluation document normalizes into the interchange record:
// confidence 0-1 becomes overall 0-100, content_quality becomes content,
// weaknesses become improvements, and zero scores survive.
func TestSleuthQualityGetNormalizes(t *testing.T) {
	doc := serverEvalDoc
	srv, _ := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"GetAssetQuality": func(vars map[string]any) any {
			if vars["slug"] != "commit-msgs" {
				t.Errorf("slug var = %v", vars["slug"])
			}
			return qualityAssetsResponse(qualityNode("commit-msgs", false, &doc))
		},
	})
	v := NewSleuthVault(srv.URL, "test-token")

	raw, err := v.GetQuality(context.Background(), "commit-msgs")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	evaluating, records := decodeQualityWrapper(t, raw)
	if evaluating || len(records) != 1 {
		t.Fatalf("wrapper = %q", raw)
	}
	rec := records[0]
	if rec["overall"].(float64) != 82 {
		t.Fatalf("overall = %v, want 82", rec["overall"])
	}
	if rec["source"] != "server" {
		t.Fatalf("source = %v", rec["source"])
	}
	cats := rec["categories"].(map[string]any)
	if cats["content"].(float64) != 80 || cats["actionability"].(float64) != 90 {
		t.Fatalf("categories = %v", cats)
	}
	if _, renamed := cats["content_quality"]; renamed {
		t.Fatalf("content_quality not renamed: %v", cats)
	}
	if cats["completeness"].(float64) != 0 {
		t.Fatalf("zero category score dropped: %v", cats)
	}
	if rec["summary"] != "A solid, well-structured skill." {
		t.Fatalf("summary = %v", rec["summary"])
	}
	insights := rec["insights"].(map[string]any)
	if got := insights["improvements"].([]any); len(got) != 1 || got[0] != "no troubleshooting section" {
		t.Fatalf("improvements = %v", got)
	}
	if rec["at"] != "2026-07-14T10:00:00Z" {
		t.Fatalf("at = %v", rec["at"])
	}
	stats := rec["stats"].(map[string]any)
	if stats["file_count"].(float64) != 2 || stats["word_count"].(float64) != 609 {
		t.Fatalf("stats = %v", stats)
	}
	if _, ok := rec["factors"].(map[string]any)["specificity"]; !ok {
		t.Fatalf("factors not passed through: %v", rec["factors"])
	}
}

// Old evaluation documents stored bare numbers for category scores.
func TestSleuthQualityGetBareCategoryScores(t *testing.T) {
	doc := `{"overall_confidence": 0.5, "category_scores": {"structure": 55, "content_quality": 41.6}, "reasoning": "ok"}`
	srv, _ := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"GetAssetQuality": func(map[string]any) any {
			return qualityAssetsResponse(qualityNode("old-skill", false, &doc))
		},
	})
	v := NewSleuthVault(srv.URL, "test-token")
	raw, err := v.GetQuality(context.Background(), "old-skill")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	_, records := decodeQualityWrapper(t, raw)
	cats := records[0]["categories"].(map[string]any)
	if cats["structure"].(float64) != 55 || cats["content"].(float64) != 42 {
		t.Fatalf("categories = %v", cats)
	}
}

// While the server evaluates, the flag surfaces so callers can poll; a
// never-evaluated asset yields empty records, not an error.
func TestSleuthQualityGetEvaluatingAndEmpty(t *testing.T) {
	srv, _ := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"GetAssetQuality": func(map[string]any) any {
			return qualityAssetsResponse(qualityNode("fresh-skill", true, nil))
		},
	})
	v := NewSleuthVault(srv.URL, "test-token")
	raw, err := v.GetQuality(context.Background(), "fresh-skill")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	evaluating, records := decodeQualityWrapper(t, raw)
	if !evaluating || len(records) != 0 {
		t.Fatalf("wrapper = %q; want evaluating with no records", raw)
	}

	// Unknown slug: empty wrapper, no error.
	srv2, _ := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"GetAssetQuality": func(map[string]any) any { return qualityAssetsResponse() },
	})
	v2 := NewSleuthVault(srv2.URL, "test-token")
	raw, err = v2.GetQuality(context.Background(), "missing")
	if err != nil {
		t.Fatalf("get missing: %v", err)
	}
	if evaluating, records := decodeQualityWrapper(t, raw); evaluating || len(records) != 0 {
		t.Fatalf("missing wrapper = %q", raw)
	}
}

// skills.new evaluates assets itself: client-written records are refused.
func TestSleuthQualityAddIsReadOnly(t *testing.T) {
	srv, _ := mockSleuthGraphQL(t, map[string]func(map[string]any) any{})
	v := NewSleuthVault(srv.URL, "test-token")
	err := v.AddQuality(context.Background(), "commit-msgs", qualityRecord(80))
	if !errors.Is(err, ErrQualityReadOnly) {
		t.Fatalf("add = %v; want ErrQualityReadOnly", err)
	}
}

// Reevaluate resolves the slug to a GID, fires the mutation, and reports
// server-mode dispatch; mutation errors surface as Go errors.
func TestSleuthQualityReevaluate(t *testing.T) {
	doc := serverEvalDoc
	srv, records := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"GetAssetQuality": func(map[string]any) any {
			return qualityAssetsResponse(qualityNode("commit-msgs", false, &doc))
		},
		"EvaluateAsset": func(vars map[string]any) any {
			if vars["id"] != "gid://commit-msgs" {
				t.Errorf("mutation id = %v", vars["id"])
			}
			return map[string]any{"evaluateAsset": map[string]any{"errors": []any{}}}
		},
	})
	v := NewSleuthVault(srv.URL, "test-token")
	mode, err := v.ReevaluateQuality(context.Background(), "commit-msgs")
	if err != nil || mode != QualityEvalServer {
		t.Fatalf("reevaluate = %q, %v; want %q", mode, err, QualityEvalServer)
	}
	var sawMutation bool
	for _, r := range *records {
		if r.OperationName == "EvaluateAsset" {
			sawMutation = true
		}
	}
	if !sawMutation {
		t.Fatalf("EvaluateAsset mutation never issued: %+v", *records)
	}
}

func TestSleuthQualityReevaluateErrors(t *testing.T) {
	doc := serverEvalDoc
	srv, _ := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"GetAssetQuality": func(map[string]any) any {
			return qualityAssetsResponse(qualityNode("commit-msgs", false, &doc))
		},
		"EvaluateAsset": func(map[string]any) any {
			return map[string]any{"evaluateAsset": map[string]any{"errors": []any{
				map[string]any{"field": "id", "messages": []string{"permission denied"}},
			}}}
		},
	})
	v := NewSleuthVault(srv.URL, "test-token")
	if _, err := v.ReevaluateQuality(context.Background(), "commit-msgs"); err == nil {
		t.Fatalf("mutation error not surfaced")
	}

	// Unknown slug: clear error before any mutation fires.
	srv2, records := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"GetAssetQuality": func(map[string]any) any { return qualityAssetsResponse() },
	})
	v2 := NewSleuthVault(srv2.URL, "test-token")
	if _, err := v2.ReevaluateQuality(context.Background(), "missing"); err == nil {
		t.Fatalf("missing asset accepted")
	}
	for _, r := range *records {
		if r.OperationName == "EvaluateAsset" {
			t.Fatalf("mutation fired for missing asset")
		}
	}
}

// LatestQuality pages the asset list and maps slug to normalized record,
// skipping never-evaluated assets.
func TestSleuthLatestQuality(t *testing.T) {
	doc := serverEvalDoc
	page := 0
	srv, _ := mockSleuthGraphQL(t, map[string]func(map[string]any) any{
		"GetAssetQuality": func(vars map[string]any) any {
			page++
			if page == 1 {
				if vars["slug"] != nil {
					t.Errorf("bulk read sent slug = %v", vars["slug"])
				}
				return map[string]any{"vault": map[string]any{"assets": map[string]any{
					"pageInfo": map[string]any{"hasNextPage": true, "endCursor": "c1"},
					"nodes": []map[string]any{
						qualityNode("skill-a", false, &doc),
						qualityNode("no-eval", false, nil),
					},
				}}}
			}
			if vars["after"] != "c1" {
				t.Errorf("page 2 after = %v", vars["after"])
			}
			return qualityAssetsResponse(qualityNode("skill-b", false, &doc))
		},
	})
	v := NewSleuthVault(srv.URL, "test-token")
	raw, err := v.LatestQuality(context.Background())
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	var latest map[string]map[string]any
	if err := json.Unmarshal([]byte(raw), &latest); err != nil {
		t.Fatalf("latest is not an object: %v\n%s", err, raw)
	}
	if len(latest) != 2 || latest["skill-a"] == nil || latest["skill-b"] == nil {
		t.Fatalf("latest = %v", latest)
	}
	if latest["no-eval"] != nil {
		t.Fatalf("never-evaluated asset included: %v", latest)
	}
}

// A server predating the quality surface classifies as unsupported.
func TestSleuthQualityUnsupportedServer(t *testing.T) {
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errors":[{"message":"Cannot query field \"evaluating\" on type \"Skill\""}]}`))
	}))
	t.Cleanup(old.Close)
	v := NewSleuthVault(old.URL, "test-token")
	if _, err := v.GetQuality(context.Background(), "x"); !errors.Is(err, ErrQualityUnsupported) {
		t.Fatalf("get on old server = %v; want ErrQualityUnsupported", err)
	}
}
