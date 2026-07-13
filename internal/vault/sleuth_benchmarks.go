package vault

import (
	"context"
	"encoding/json"

	vaultgql "github.com/sleuth-io/sx/internal/vault/graphql"
)

// Sleuth-vault benchmark storage (docs/benchmarks-spec.md): the server
// twin of .sx/benchmarks/<asset>.json. Records land as real EvalBenchmark
// rows on skills.new (triggered_by=external), and reads return the full
// history — server-run benchmarks included — in the interchange shape.
// The server enforces validation, the size cap, and org-member auth, so
// the client stays a thin pass-through.

// ListBenchmarks returns the asset's benchmark records, newest first.
func (s *SleuthVault) ListBenchmarks(ctx context.Context, asset string) (string, error) {
	first := maxBenchmarkRecords
	resp, err := vaultgql.GetAssetBenchmarks(ctx, s.gqlClient(), asset, &first)
	if err != nil {
		if isAppPluginSchemaUnknownErr(err) {
			return "", ErrBenchmarksUnsupported
		}
		return "", err
	}
	return decodeJSONStringScalar(resp.Vault.AssetBenchmarks)
}

// AddBenchmark records one client-run benchmark result on the server.
func (s *SleuthVault) AddBenchmark(ctx context.Context, asset, record string) error {
	if err := validateBenchmarkRecord(record); err != nil {
		return err
	}
	quoted, err := json.Marshal(record) // JSONString: record as a string value
	if err != nil {
		return err
	}
	input := vaultgql.ImportAssetBenchmarkInput{
		AssetName: asset,
		Benchmark: json.RawMessage(quoted),
	}
	resp, err := vaultgql.ImportAssetBenchmark(ctx, s.gqlClient(), input)
	if err != nil {
		if isAppPluginSchemaUnknownErr(err) {
			return ErrBenchmarksUnsupported
		}
		return err
	}
	for _, e := range resp.ImportAssetBenchmark.Errors {
		if len(e.Messages) > 0 {
			return &mutationError{message: e.Messages[0]}
		}
	}
	return nil
}

// LatestBenchmarks returns the newest record per asset in one call.
func (s *SleuthVault) LatestBenchmarks(ctx context.Context) (string, error) {
	resp, err := vaultgql.GetLatestAssetBenchmarks(ctx, s.gqlClient())
	if err != nil {
		if isAppPluginSchemaUnknownErr(err) {
			return "", ErrBenchmarksUnsupported
		}
		return "", err
	}
	return decodeJSONStringScalar(resp.Vault.LatestAssetBenchmarks)
}

// decodeJSONStringScalar unwraps graphene's JSONString scalar: the
// document arrives as a quoted JSON string value, not inline JSON.
func decodeJSONStringScalar(raw *json.RawMessage) (string, error) {
	if raw == nil {
		return "", nil
	}
	var doc string
	if err := json.Unmarshal(*raw, &doc); err != nil {
		return "", err
	}
	if doc == "null" || doc == "{}" || doc == "[]" {
		return "", nil
	}
	return doc, nil
}
