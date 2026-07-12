package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sleuth-io/sx/internal/mgmt"
	"github.com/sleuth-io/sx/internal/utils"
)

// ---- Benchmark storage (API 1.10.0, docs/benchmarks-spec.md) ----
// One capped, newest-first list of benchmark records per asset:
// .sx/benchmarks/<asset>.json on file-backed vaults; real EvalBenchmark
// rows behind the interchange API on skills.new. Records are the
// interchange shape shared with pulse — the same document a skills.new
// server-run benchmark exports.

// BenchmarkStore is implemented by vaults that can hold benchmark records.
type BenchmarkStore interface {
	// ListBenchmarks returns the asset's records as a JSON array string,
	// newest first ("" when none exist).
	ListBenchmarks(ctx context.Context, asset string) (string, error)

	// AddBenchmark prepends one interchange record (a JSON object string).
	AddBenchmark(ctx context.Context, asset, record string) error

	// LatestBenchmarks returns a JSON object mapping asset name to its
	// newest record, for every asset that has one — the dashboard's
	// single bulk read.
	LatestBenchmarks(ctx context.Context) (string, error)
}

// ErrBenchmarksUnsupported is returned when the vault backend cannot
// store benchmark records (a server predating the surface).
var ErrBenchmarksUnsupported = errors.New(
	"this library's server doesn't support benchmark storage yet")

const (
	// maxBenchmarkRecordBytes bounds one record (mirrored server-side).
	maxBenchmarkRecordBytes = 256 << 10
	// maxBenchmarkRecords caps each asset's retained history; git
	// history keeps the rest on repo-backed vaults.
	maxBenchmarkRecords = 20
)

// Benchmark files are keyed by asset name, so the name must be a safe
// path segment — the same shape asset directories already use.
var benchmarkAssetPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

func benchmarksPath(vaultRoot, asset string) (string, error) {
	if !benchmarkAssetPattern.MatchString(asset) || strings.Contains(asset, "..") {
		return "", fmt.Errorf("invalid asset name %q", asset)
	}
	return filepath.Join(vaultRoot, ".sx", "benchmarks", asset+".json"), nil
}

// validateBenchmarkRecord is the one bounded-valid-JSON-object contract,
// enforced identically by every backend before anything is written.
func validateBenchmarkRecord(record string) error {
	if len(record) > maxBenchmarkRecordBytes {
		return fmt.Errorf("benchmark record exceeds %d bytes", maxBenchmarkRecordBytes)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(record), &obj); err != nil {
		return errors.New("benchmark record must be a JSON object")
	}
	// Downstream readers (the dashboard, the BenchmarkRecord TS type)
	// assume summary is a stat-block object; on file vaults this is the
	// only validation gate, so a scalar must be refused here.
	// summary == nil catches JSON null, which unmarshals without error.
	var summary map[string]json.RawMessage
	if raw, ok := obj["summary"]; !ok || json.Unmarshal(raw, &summary) != nil || summary == nil {
		return errors.New("benchmark record needs a summary object")
	}
	return nil
}

type benchmarksDoc struct {
	Benchmarks []json.RawMessage `json:"benchmarks"`
}

func readBenchmarksDoc(path string) (*benchmarksDoc, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is under the vault root with a validated name
	if os.IsNotExist(err) {
		return &benchmarksDoc{}, nil
	}
	if err != nil {
		return nil, err
	}
	doc := &benchmarksDoc{}
	if err := json.Unmarshal(data, doc); err != nil {
		// A corrupt file must not brick the asset's benchmarks forever —
		// treat it as an empty list. Reads never mutate; the write path
		// preserves the corrupt bytes as <asset>.json.bad first, because
		// only git vaults have history to fall back on — plain and
		// synced folders would otherwise lose the records outright.
		return &benchmarksDoc{}, nil
	}
	return doc, nil
}

// preserveCorruptBenchmarks sets aside an unparseable benchmarks file as
// <asset>.json.bad (best-effort) so a fresh write can't destroy the only
// copy on vaults without git history.
func preserveCorruptBenchmarks(path string) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is under the vault root with a validated name
	if err != nil {
		return
	}
	if json.Unmarshal(data, &benchmarksDoc{}) == nil {
		return // parseable — nothing to preserve
	}
	_ = os.Rename(path, path+".bad")
}

func commonListBenchmarks(vaultRoot, asset string) (string, error) {
	path, err := benchmarksPath(vaultRoot, asset)
	if err != nil {
		return "", err
	}
	doc, err := readBenchmarksDoc(path)
	if err != nil {
		return "", err
	}
	if len(doc.Benchmarks) == 0 {
		return "", nil
	}
	out, err := json.Marshal(doc.Benchmarks)
	return string(out), err
}

func commonAddBenchmark(vaultRoot, asset, record string) error {
	if err := validateBenchmarkRecord(record); err != nil {
		return err
	}
	path, err := benchmarksPath(vaultRoot, asset)
	if err != nil {
		return err
	}
	preserveCorruptBenchmarks(path)
	doc, err := readBenchmarksDoc(path)
	if err != nil {
		return err
	}
	doc.Benchmarks = append([]json.RawMessage{json.RawMessage(record)}, doc.Benchmarks...)
	if len(doc.Benchmarks) > maxBenchmarkRecords {
		doc.Benchmarks = doc.Benchmarks[:maxBenchmarkRecords]
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Atomic write for the same reason as app-plugin shared data: readers
	// take no lock and synced folders can replicate mid-write.
	return utils.WriteFileAtomic(path, data, 0o644)
}

func commonLatestBenchmarks(vaultRoot string) (string, error) {
	dir := filepath.Join(vaultRoot, ".sx", "benchmarks")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	latest := map[string]json.RawMessage{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		asset := strings.TrimSuffix(name, ".json")
		if !benchmarkAssetPattern.MatchString(asset) {
			continue
		}
		doc, err := readBenchmarksDoc(filepath.Join(dir, name))
		if err != nil || len(doc.Benchmarks) == 0 {
			continue
		}
		latest[asset] = doc.Benchmarks[0]
	}
	if len(latest) == 0 {
		return "", nil
	}
	out, err := json.Marshal(latest)
	return string(out), err
}

// ListBenchmarks returns the asset's benchmark records.
func (p *PathVault) ListBenchmarks(ctx context.Context, asset string) (string, error) {
	return commonListBenchmarks(p.repoPath, asset)
}

// AddBenchmark records one benchmark result.
func (p *PathVault) AddBenchmark(ctx context.Context, asset, record string) error {
	return p.withLock(ctx, func(actor mgmt.Actor) error {
		return commonAddBenchmark(p.repoPath, asset, record)
	})
}

// LatestBenchmarks returns the newest record per asset.
func (p *PathVault) LatestBenchmarks(ctx context.Context) (string, error) {
	return commonLatestBenchmarks(p.repoPath)
}

// ListBenchmarks returns the asset's benchmark records from the clone.
func (g *GitVault) ListBenchmarks(ctx context.Context, asset string) (string, error) {
	if err := g.cloneOrUpdate(ctx); err != nil {
		return "", err
	}
	return commonListBenchmarks(g.repoPath, asset)
}

// AddBenchmark records one benchmark result and pushes.
func (g *GitVault) AddBenchmark(ctx context.Context, asset, record string) error {
	return g.runInVaultTx(ctx, "Record benchmark for "+asset, func(root string, actor mgmt.Actor) error {
		return commonAddBenchmark(root, asset, record)
	})
}

// LatestBenchmarks returns the newest record per asset from the clone.
func (g *GitVault) LatestBenchmarks(ctx context.Context) (string, error) {
	if err := g.cloneOrUpdate(ctx); err != nil {
		return "", err
	}
	return commonLatestBenchmarks(g.repoPath)
}
