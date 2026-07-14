package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/Khan/genqlient/graphql"

	vaultgql "github.com/sleuth-io/sx/internal/vault/graphql"
)

// Sleuth-vault quality storage (docs/quality-spec.md): the server twin of
// .sx/quality/<asset>.json. skills.new evaluates assets itself and keeps
// one evaluation document per asset (Asset.evaluation_result), so reads
// normalize that document into the interchange shape and writes are
// refused — ReevaluateQuality asks the server to run a fresh evaluation
// instead.

// GetQuality returns the asset's quality wrapper doc, normalized from the
// server's evaluation document. Assets the server hasn't evaluated (and
// git-sourced assets, which skills.new doesn't evaluate) yield an empty
// records list.
func (s *SleuthVault) GetQuality(ctx context.Context, asset string) (string, error) {
	node, err := s.qualityAssetLookup(ctx, asset)
	if errors.Is(err, errQualityAssetNotFound) {
		return marshalQualityWrapper(false, nil)
	}
	if err != nil {
		return "", err
	}
	record := normalizeServerEvaluation(node)
	var records []json.RawMessage
	if record != nil {
		records = []json.RawMessage{record}
	}
	return marshalQualityWrapper(node.GetEvaluating(), records)
}

// AddQuality is refused: skills.new evaluates assets itself and its
// document is the source of truth. Use ReevaluateQuality.
func (s *SleuthVault) AddQuality(ctx context.Context, asset, record string) error {
	return ErrQualityReadOnly
}

// LatestQuality returns the newest record per asset, paging the vault's
// asset list.
func (s *SleuthVault) LatestQuality(ctx context.Context) (string, error) {
	latest := map[string]json.RawMessage{}
	pageSize := 50
	var after *string
	for {
		resp, err := vaultgql.GetAssetQuality(ctx, s.gqlClient(), nil, &pageSize, after)
		if err != nil {
			if isAppPluginSchemaUnknownErr(err) {
				return "", ErrQualityUnsupported
			}
			return "", err
		}
		for _, node := range resp.Vault.Assets.Nodes {
			if record := normalizeServerEvaluation(node); record != nil {
				latest[node.GetSlug()] = record
			}
		}
		page := resp.Vault.Assets.PageInfo
		if !page.HasNextPage || page.EndCursor == nil {
			break
		}
		after = page.EndCursor
	}
	if len(latest) == 0 {
		return "", nil
	}
	out, err := json.Marshal(latest)
	return string(out), err
}

// ReevaluateQuality asks the server to evaluate the asset and reports
// "server": the caller polls GetQuality until evaluating flips false.
func (s *SleuthVault) ReevaluateQuality(ctx context.Context, asset string) (string, error) {
	node, err := s.qualityAssetLookup(ctx, asset)
	if errors.Is(err, errQualityAssetNotFound) {
		return "", fmt.Errorf("asset %q not found in this library", asset)
	}
	if err != nil {
		return "", err
	}
	// The mutation runs the evaluation inline server-side, so it outlives
	// the default 30s HTTP budget — use the long-request client.
	client := graphql.NewClient(s.serverURL+"/graphql", &authDoer{
		client:    s.streamingClient,
		authToken: s.authToken,
	})
	resp, err := vaultgql.EvaluateAsset(ctx, client, node.GetId())
	if err != nil {
		if isAppPluginSchemaUnknownErr(err) {
			return "", ErrQualityUnsupported
		}
		return "", err
	}
	for _, e := range resp.EvaluateAsset.Errors {
		if len(e.Messages) > 0 {
			return "", &mutationError{message: e.Messages[0]}
		}
	}
	return QualityEvalServer, nil
}

// errQualityAssetNotFound signals a slug that doesn't resolve; callers
// decide whether that's an empty read or a hard error.
var errQualityAssetNotFound = errors.New("asset not found")

// qualityAssetLookup fetches the asset's quality-bearing node by slug.
func (s *SleuthVault) qualityAssetLookup(
	ctx context.Context, asset string,
) (vaultgql.GetAssetQualityVaultAssetsVaultAssetsConnectionNodesVaultAsset, error) {
	first := 1
	resp, err := vaultgql.GetAssetQuality(ctx, s.gqlClient(), &asset, &first, nil)
	if err != nil {
		if isAppPluginSchemaUnknownErr(err) {
			return nil, ErrQualityUnsupported
		}
		return nil, err
	}
	if len(resp.Vault.Assets.Nodes) == 0 {
		return nil, errQualityAssetNotFound
	}
	return resp.Vault.Assets.Nodes[0], nil
}

// serverEvaluation is the shape pulse stores in Asset.evaluation_result
// (sleuth/apps/issues/skills/asset_evaluation_tool.py). Only the fields
// the interchange record needs are decoded; factors pass through raw.
type serverEvaluation struct {
	OverallConfidence  float64                    `json:"overall_confidence"`
	CategoryScores     map[string]json.RawMessage `json:"category_scores"`
	Factors            json.RawMessage            `json:"factors"`
	Reasoning          string                     `json:"reasoning"`
	LLMStrengths       []string                   `json:"llm_strengths"`
	LLMWeaknesses      []string                   `json:"llm_weaknesses"`
	LLMRecommendations []string                   `json:"llm_recommendations"`
	FileCount          *int                       `json:"file_count"`
	WordCount          *int                       `json:"word_count"`
}

// interchange category names, keyed by pulse's category_scores names.
var serverCategoryNames = map[string]string{
	"structure":       "structure",
	"actionability":   "actionability",
	"content_quality": "content",
	"completeness":    "completeness",
}

// normalizeServerEvaluation converts one asset node's server evaluation
// document into an interchange record (nil when the asset has none —
// never evaluated, or not a managed source).
func normalizeServerEvaluation(
	node vaultgql.GetAssetQualityVaultAssetsVaultAssetsConnectionNodesVaultAsset,
) json.RawMessage {
	managed, ok := node.GetSource().(*vaultgql.GetAssetQualityVaultAssetsVaultAssetsConnectionNodesVaultAssetSourceAssetManagedSource)
	if !ok || managed.EvaluationResult == nil || *managed.EvaluationResult == "" {
		return nil
	}
	var eval serverEvaluation
	if err := json.Unmarshal([]byte(*managed.EvaluationResult), &eval); err != nil {
		return nil
	}

	categories := map[string]int{}
	for serverName, name := range serverCategoryNames {
		raw, ok := eval.CategoryScores[serverName]
		if !ok {
			continue
		}
		// Current servers store {tier, score, weight} objects; older
		// documents stored bare numbers. Accept both.
		var scored struct {
			Score *float64 `json:"score"`
		}
		var bare float64
		switch {
		case json.Unmarshal(raw, &scored) == nil && scored.Score != nil:
			categories[name] = int(math.Round(*scored.Score))
		case json.Unmarshal(raw, &bare) == nil:
			categories[name] = int(math.Round(bare))
		}
	}

	record := map[string]any{
		"source":     "server",
		"overall":    int(math.Round(eval.OverallConfidence * 100)),
		"categories": categories,
		"summary":    eval.Reasoning,
		"insights": map[string]any{
			"strengths":       emptyIfNil(eval.LLMStrengths),
			"improvements":    emptyIfNil(eval.LLMWeaknesses),
			"recommendations": emptyIfNil(eval.LLMRecommendations),
		},
	}
	// Pulse stores no evaluation timestamp; the asset's updatedAt is the
	// closest signal (an evaluation always touches the asset).
	if at := node.GetUpdatedAt(); !at.IsZero() {
		record["at"] = at.UTC().Format(time.RFC3339)
	}
	if len(eval.Factors) > 0 && string(eval.Factors) != "null" {
		record["factors"] = eval.Factors
	}
	if eval.FileCount != nil || eval.WordCount != nil {
		stats := map[string]any{}
		if eval.FileCount != nil {
			stats["file_count"] = *eval.FileCount
		}
		if eval.WordCount != nil {
			stats["word_count"] = *eval.WordCount
		}
		record["stats"] = stats
	}

	out, err := json.Marshal(record)
	if err != nil {
		return nil
	}
	return out
}

func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
