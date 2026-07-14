package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/sx/internal/mgmt"
	"github.com/sleuth-io/sx/internal/utils"
)

// ---- Quality storage (API 1.12.0, docs/quality-spec.md) ----
// One capped, newest-first list of quality evaluation records per asset:
// .sx/quality/<asset>.json on file-backed vaults; the server's own
// Asset.evaluation_result on skills.new. Records are the interchange
// shape normalized from pulse's evaluation document, so a record read
// from any vault type renders identically.

// QualityStore is implemented by vaults that can hold quality records.
type QualityStore interface {
	// GetQuality returns a wrapper doc for the asset:
	// {"evaluating": bool, "records": [...]} with records newest first.
	// evaluating is only ever true on server-evaluated vaults; records
	// is empty (never null) when the asset has no evaluations.
	GetQuality(ctx context.Context, asset string) (string, error)

	// AddQuality prepends one interchange record (a JSON object string).
	// Server-evaluated vaults refuse this with ErrQualityReadOnly — the
	// server's own evaluation is the source of truth there.
	AddQuality(ctx context.Context, asset, record string) error

	// LatestQuality returns a JSON object mapping asset name to its
	// newest record, for every asset that has one.
	LatestQuality(ctx context.Context) (string, error)

	// ReevaluateQuality requests a fresh evaluation and reports who runs
	// it: "server" when the vault backend evaluates (poll GetQuality
	// until evaluating flips false), "local" when the caller must run
	// the evaluation itself and store the result via AddQuality.
	ReevaluateQuality(ctx context.Context, asset string) (string, error)
}

// Reevaluate dispatch results: who runs the evaluation.
const (
	QualityEvalServer = "server"
	QualityEvalLocal  = "local"
)

// ErrQualityUnsupported is returned when the vault backend cannot store
// quality records (a server predating the surface).
var ErrQualityUnsupported = errors.New(
	"this library's server doesn't support quality storage yet")

// ErrQualityReadOnly is returned when a vault only surfaces its own
// server-side evaluations and refuses client-written records.
var ErrQualityReadOnly = errors.New(
	"this library's quality scores are server-evaluated and read-only")

const (
	// maxQualityRecordBytes bounds one record (same cap as benchmarks).
	maxQualityRecordBytes = 256 << 10
	// maxQualityRecords caps each asset's retained history — enough for
	// the trend chip; git history keeps the rest on repo-backed vaults.
	maxQualityRecords = 10
)

func qualityPath(vaultRoot, asset string) (string, error) {
	// Same safe-path-segment shape as benchmark files.
	if !benchmarkAssetPattern.MatchString(asset) || strings.Contains(asset, "..") {
		return "", fmt.Errorf("invalid asset name %q", asset)
	}
	return filepath.Join(vaultRoot, ".sx", "quality", asset+".json"), nil
}

// validateQualityRecord is the bounded-valid-JSON-object contract,
// enforced identically by every backend before anything is written.
func validateQualityRecord(record string) error {
	if len(record) > maxQualityRecordBytes {
		return fmt.Errorf("quality record exceeds %d bytes", maxQualityRecordBytes)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(record), &obj); err != nil {
		return errors.New("quality record must be a JSON object")
	}
	// Downstream readers (the QualityRecord TS type) key everything off
	// overall and categories; on file vaults this is the only validation
	// gate, so a malformed score must be refused here.
	var overall float64
	if raw, ok := obj["overall"]; !ok || json.Unmarshal(raw, &overall) != nil ||
		overall < 0 || overall > 100 {
		return errors.New("quality record needs a numeric overall score in [0,100]")
	}
	var categories map[string]json.RawMessage
	if raw, ok := obj["categories"]; !ok || json.Unmarshal(raw, &categories) != nil ||
		categories == nil {
		return errors.New("quality record needs a categories object")
	}
	return nil
}

type qualityFileDoc struct {
	Quality []json.RawMessage `json:"quality"`
}

// qualityWrapperDoc is what GetQuality returns on every vault type.
type qualityWrapperDoc struct {
	Evaluating bool              `json:"evaluating"`
	Records    []json.RawMessage `json:"records"`
}

func marshalQualityWrapper(evaluating bool, records []json.RawMessage) (string, error) {
	if records == nil {
		records = []json.RawMessage{}
	}
	out, err := json.Marshal(qualityWrapperDoc{Evaluating: evaluating, Records: records})
	return string(out), err
}

func readQualityDoc(path string) (*qualityFileDoc, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is under the vault root with a validated name
	if os.IsNotExist(err) {
		return &qualityFileDoc{}, nil
	}
	if err != nil {
		return nil, err
	}
	doc := &qualityFileDoc{}
	if err := json.Unmarshal(data, doc); err != nil {
		// A corrupt file must not brick the asset's quality history —
		// treat it as an empty list. Reads never mutate; the write path
		// preserves the corrupt bytes as <asset>.json.bad first.
		return &qualityFileDoc{}, nil
	}
	return doc, nil
}

// preserveCorruptQuality sets aside an unparseable quality file as
// <asset>.json.bad (best-effort) so a fresh write can't destroy the only
// copy on vaults without git history.
func preserveCorruptQuality(path string) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is under the vault root with a validated name
	if err != nil {
		return
	}
	if json.Unmarshal(data, &qualityFileDoc{}) == nil {
		return // parseable — nothing to preserve
	}
	_ = os.Rename(path, path+".bad")
}

func commonGetQuality(vaultRoot, asset string) (string, error) {
	path, err := qualityPath(vaultRoot, asset)
	if err != nil {
		return "", err
	}
	doc, err := readQualityDoc(path)
	if err != nil {
		return "", err
	}
	return marshalQualityWrapper(false, doc.Quality)
}

func commonAddQuality(vaultRoot, asset, record string) error {
	if err := validateQualityRecord(record); err != nil {
		return err
	}
	path, err := qualityPath(vaultRoot, asset)
	if err != nil {
		return err
	}
	preserveCorruptQuality(path)
	doc, err := readQualityDoc(path)
	if err != nil {
		return err
	}
	doc.Quality = append([]json.RawMessage{json.RawMessage(record)}, doc.Quality...)
	if len(doc.Quality) > maxQualityRecords {
		doc.Quality = doc.Quality[:maxQualityRecords]
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Atomic write for the same reason as benchmarks: readers take no
	// lock and synced folders can replicate mid-write.
	return utils.WriteFileAtomic(path, data, 0o644)
}

func commonLatestQuality(vaultRoot string) (string, error) {
	dir := filepath.Join(vaultRoot, ".sx", "quality")
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
		doc, err := readQualityDoc(filepath.Join(dir, name))
		if err != nil || len(doc.Quality) == 0 {
			continue
		}
		latest[asset] = doc.Quality[0]
	}
	if len(latest) == 0 {
		return "", nil
	}
	out, err := json.Marshal(latest)
	return string(out), err
}

// GetQuality returns the asset's quality wrapper doc.
func (p *PathVault) GetQuality(ctx context.Context, asset string) (string, error) {
	return commonGetQuality(p.repoPath, asset)
}

// AddQuality records one quality evaluation.
func (p *PathVault) AddQuality(ctx context.Context, asset, record string) error {
	return p.withLock(ctx, func(actor mgmt.Actor) error {
		return commonAddQuality(p.repoPath, asset, record)
	})
}

// LatestQuality returns the newest record per asset.
func (p *PathVault) LatestQuality(ctx context.Context) (string, error) {
	return commonLatestQuality(p.repoPath)
}

// ReevaluateQuality on a path vault is the caller's job: there is no
// backend that could run the evaluation.
func (p *PathVault) ReevaluateQuality(ctx context.Context, asset string) (string, error) {
	return QualityEvalLocal, nil
}

// GetQuality returns the asset's quality wrapper doc from the clone.
func (g *GitVault) GetQuality(ctx context.Context, asset string) (string, error) {
	if err := g.cloneOrUpdate(ctx); err != nil {
		return "", err
	}
	return commonGetQuality(g.repoPath, asset)
}

// AddQuality records one quality evaluation and pushes.
func (g *GitVault) AddQuality(ctx context.Context, asset, record string) error {
	return g.runInVaultTx(ctx, "Record quality for "+asset, func(root string, actor mgmt.Actor) error {
		return commonAddQuality(root, asset, record)
	})
}

// LatestQuality returns the newest record per asset from the clone.
func (g *GitVault) LatestQuality(ctx context.Context) (string, error) {
	if err := g.cloneOrUpdate(ctx); err != nil {
		return "", err
	}
	return commonLatestQuality(g.repoPath)
}

// ReevaluateQuality on a git vault is the caller's job, same as path.
func (g *GitVault) ReevaluateQuality(ctx context.Context, asset string) (string, error) {
	return QualityEvalLocal, nil
}
