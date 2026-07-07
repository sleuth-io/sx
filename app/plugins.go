package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/sleuth-io/sx/internal/buildinfo"
	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/mgmt"
	"github.com/sleuth-io/sx/internal/utils"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// App-extension support (docs/app-plugins-spec.md). The webview has no
// filesystem: everything an extension persists or reads flows through
// these bridge methods, which is what makes the API surface the blast
// radius. Storage is per plugin, per profile, app-side (never in the
// vault).

var pluginIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)

// AppVersion exposes the build version to extensions (sx.app.version)
// and to the host's minAppVersion gate.
func (a *App) AppVersion() string {
	return strings.TrimPrefix(buildinfo.Version, "v")
}

func validatePluginID(id string) error {
	if !pluginIDPattern.MatchString(id) {
		return fmt.Errorf("invalid extension id %q", id)
	}
	return nil
}

// pluginDataDir is where extension state lives:
// <config>/app-plugins/<profile>/ with data files per extension id.
func (a *App) pluginDataDir() (string, error) {
	base, err := utils.GetConfigDir()
	if err != nil {
		return "", err
	}
	cfg, err := config.Load()
	if err != nil {
		return "", errors.New("no library configured")
	}
	profile := cfg.ProfileName
	if profile == "" {
		profile = "default"
	}
	dir := filepath.Join(base, "app-plugins", profile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// PluginLoadData returns the extension's saved JSON blob ("" when none).
func (a *App) PluginLoadData(id string) (string, error) {
	if err := validatePluginID(id); err != nil {
		return "", err
	}
	dir, err := a.pluginDataDir()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".data.json"))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// maxPluginDataBytes bounds a single extension's stored state; the API is
// for settings and small caches, not a database.
const maxPluginDataBytes = 1 << 20

// PluginSaveData persists the extension's JSON blob atomically.
func (a *App) PluginSaveData(id, data string) error {
	if err := validatePluginID(id); err != nil {
		return err
	}
	if len(data) > maxPluginDataBytes {
		return fmt.Errorf("extension data exceeds %d bytes", maxPluginDataBytes)
	}
	dir, err := a.pluginDataDir()
	if err != nil {
		return err
	}
	target := filepath.Join(dir, id+".data.json")
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, []byte(data), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

// PluginDecisions returns the per-profile INTENDED enablement per
// extension id. Intent — not live-loaded state — is what persists, so a
// transient load failure can never demote an extension the user wanted
// on, and ids with no recorded decision fall back to their default
// (built-ins on, vault-installed extensions off).
func (a *App) PluginDecisions() (map[string]bool, error) {
	out := map[string]bool{}
	dir, err := a.pluginDataDir()
	if err != nil {
		return out, nil // no profile yet — defaults apply
	}
	data, err := os.ReadFile(filepath.Join(dir, "decisions.json"))
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetPluginDecision records the user's intent for one extension and
// appends the matching audit event on vaults that record history
// (best-effort; the local decision file is the durable state).
func (a *App) SetPluginDecision(id string, enabled bool) error {
	if err := validatePluginID(id); err != nil {
		return err
	}
	decisions, err := a.PluginDecisions()
	if err != nil {
		return err
	}
	decisions[id] = enabled
	dir, err := a.pluginDataDir()
	if err != nil {
		return err
	}
	data, err := json.Marshal(decisions)
	if err != nil {
		return err
	}
	target := filepath.Join(dir, "decisions.json")
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		return err
	}
	// Fire-and-forget: on a git vault the audit append is a full
	// pull+commit+push, and a per-user preference toggle must not block
	// the UI on network I/O. The decision file above is the durable state.
	go a.auditPluginDecision(id, enabled)
	return nil
}

func (a *App) auditPluginDecision(id string, enabled bool) {
	v, err := a.currentVault()
	if err != nil {
		return
	}
	event := mgmt.EventPluginEnabled
	if !enabled {
		event = mgmt.EventPluginDisabled
	}
	err = v.ImportAuditEvents(a.ctx, []mgmt.AuditEvent{{
		Timestamp: time.Now(),
		Actor:     strings.TrimSpace(a.GetVaultInfo().Identity),
		Event:     event, TargetType: mgmt.TargetTypePlugin, Target: id,
	}})
	if err != nil {
		logger.Get().Warn("extension audit append failed", "error", err)
	}
}

// PluginConsents returns the per-profile record of consented permission
// sets, keyed by extension id. The frontend re-prompts when an
// extension's declared permissions differ from what was consented.
func (a *App) PluginConsents() (map[string][]string, error) {
	out := map[string][]string{}
	dir, err := a.pluginDataDir()
	if err != nil {
		return out, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, "consents.json"))
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetPluginConsent records that the user consented to an extension's
// permission set.
func (a *App) SetPluginConsent(id string, permissions []string) error {
	if err := validatePluginID(id); err != nil {
		return err
	}
	consents, err := a.PluginConsents()
	if err != nil {
		return err
	}
	dir, err := a.pluginDataDir()
	if err != nil {
		return err
	}
	sorted := append([]string(nil), permissions...)
	sort.Strings(sorted)
	consents[id] = sorted
	data, err := json.Marshal(consents)
	if err != nil {
		return err
	}
	target := filepath.Join(dir, "consents.json")
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

// PluginPolicy is the extension policy as the frontend consumes it.
type PluginPolicy struct {
	Mode    string   `json:"mode"`
	Allowed []string `json:"allowed"`
}

// GetPluginPolicy reads the vault's [app-plugins] policy. Vaults without
// policy support (or no policy set) report open. A read FAILURE must not
// fail open — a transient git error would silently lift an org's
// allowlist — so successful reads are cached per profile and an error
// serves the cache.
func (a *App) GetPluginPolicy() (PluginPolicy, error) {
	open := PluginPolicy{Mode: vaultpkg.AppPluginModeOpen, Allowed: []string{}}
	v, err := a.currentVault()
	if err != nil {
		return a.cachedPluginPolicy(open), nil
	}
	store, ok := v.(vaultpkg.AppPluginPolicyStore)
	if !ok {
		return open, nil // backend has no policy concept — genuinely open
	}
	policy, err := store.AppPluginPolicy(a.ctx)
	if err != nil {
		return a.cachedPluginPolicy(open), nil
	}
	out := open
	if policy != nil && policy.Mode != "" {
		allowed := policy.Allowed
		if allowed == nil {
			allowed = []string{}
		}
		out = PluginPolicy{Mode: policy.Mode, Allowed: allowed}
	}
	a.cachePluginPolicy(out)
	return out, nil
}

func (a *App) policyCachePath() (string, error) {
	dir, err := a.pluginDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "policy-cache.json"), nil
}

func (a *App) cachePluginPolicy(p PluginPolicy) {
	path, err := a.policyCachePath()
	if err != nil {
		return
	}
	if data, err := json.Marshal(p); err == nil {
		_ = os.WriteFile(path, data, 0o644)
	}
}

func (a *App) cachedPluginPolicy(fallback PluginPolicy) PluginPolicy {
	path, err := a.policyCachePath()
	if err != nil {
		return fallback
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	var p PluginPolicy
	if err := json.Unmarshal(data, &p); err != nil || p.Mode == "" {
		return fallback
	}
	if p.Allowed == nil {
		p.Allowed = []string{}
	}
	return p
}

// PluginUsageEventRecord is the extension-facing usage event shape.
type PluginUsageEventRecord struct {
	Timestamp    string `json:"timestamp"`
	Actor        string `json:"actor"`
	AssetName    string `json:"assetName"`
	AssetVersion string `json:"assetVersion"`
	AssetType    string `json:"assetType"`
}

// PluginUsageEvents returns the vault's usage events from the last
// sinceDays days (capped), newest first — the usage:read capability.
func (a *App) PluginUsageEvents(sinceDays int) ([]PluginUsageEventRecord, error) {
	v, err := a.currentVault()
	if err != nil {
		return nil, err
	}
	events, err := v.ReadUsageEvents(a.ctx, mgmt.UsageFilter{})
	if err != nil {
		return nil, friendlyVaultError(err)
	}
	cutoff := usageCutoff(sinceDays)
	out := make([]PluginUsageEventRecord, 0, len(events))
	for _, e := range events {
		if e.Timestamp.Before(cutoff) {
			continue
		}
		out = append(out, PluginUsageEventRecord{
			Timestamp:    e.Timestamp.Format(time.RFC3339),
			Actor:        e.Actor,
			AssetName:    e.AssetName,
			AssetVersion: e.AssetVersion,
			AssetType:    e.AssetType,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp > out[j].Timestamp })
	return out, nil
}

// PluginAuditEventRecord is the extension-facing audit event shape.
type PluginAuditEventRecord struct {
	Timestamp  string `json:"timestamp"`
	Actor      string `json:"actor"`
	Event      string `json:"event"`
	TargetType string `json:"targetType"`
	Target     string `json:"target"`
}

// PluginAuditEvents returns the vault's audit events from the last
// sinceDays days (capped), newest first — the usage:read capability.
func (a *App) PluginAuditEvents(sinceDays int) ([]PluginAuditEventRecord, error) {
	v, err := a.currentVault()
	if err != nil {
		return nil, err
	}
	events, err := v.QueryAuditEvents(a.ctx, mgmt.AuditFilter{})
	if err != nil {
		return nil, friendlyVaultError(err)
	}
	cutoff := usageCutoff(sinceDays)
	out := make([]PluginAuditEventRecord, 0, len(events))
	for _, e := range events {
		if e.Timestamp.Before(cutoff) {
			continue
		}
		out = append(out, PluginAuditEventRecord{
			Timestamp:  e.Timestamp.Format(time.RFC3339),
			Actor:      e.Actor,
			Event:      e.Event,
			TargetType: e.TargetType,
			Target:     e.Target,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp > out[j].Timestamp })
	return out, nil
}

// ImportResult reports what a folder import produced.
type ImportResult struct {
	Created []string `json:"created"`
	Skipped int      `json:"skipped"`
}

// ImportDraftsFromFolder opens a directory picker and batch-creates one
// draft per skill-shaped entry found: subdirectories containing a
// SKILL.md (a .claude/skills layout or an sx vault assets dir), plus
// loose top-level markdown files (a folder of prompts, an Obsidian
// folder). Everything lands as DRAFTS — the human reviews and publishes.
// Serves the Importer built-in through the drafts:write capability.
func (a *App) ImportDraftsFromFolder() (ImportResult, error) {
	dir, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Choose a folder to import (e.g. .claude/skills, or a folder of prompts)",
	})
	if err != nil {
		return ImportResult{Created: []string{}}, err
	}
	if dir == "" {
		return ImportResult{Created: []string{}}, nil // cancelled
	}
	return a.importDraftsFrom(dir)
}

// importDraftsFrom is the dialog-free scan+create core, split out so the
// import shape is testable without a native picker.
func (a *App) importDraftsFrom(dir string) (ImportResult, error) {
	res := ImportResult{Created: []string{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return res, err
	}
	var candidates []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(dir, name)
		if e.IsDir() {
			// A skill folder: any dir carrying markdown (SKILL.md or
			// otherwise). Empty or markdown-less dirs are skipped.
			if dirHasMarkdown(full) {
				candidates = append(candidates, full)
			} else {
				res.Skipped++
			}
			continue
		}
		if strings.EqualFold(filepath.Ext(name), ".md") {
			candidates = append(candidates, full)
		} else {
			res.Skipped++
		}
	}
	if len(candidates) == 0 {
		return res, errors.New("no markdown files or skill folders found in that folder")
	}

	for _, c := range candidates {
		draft, err := a.CreateDraftFromPaths([]string{c})
		if err != nil {
			res.Skipped++
			continue
		}
		res.Created = append(res.Created, draft.Name)
	}
	sort.Strings(res.Created)
	return res, nil
}

func dirHasMarkdown(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			return true
		}
	}
	return false
}

// usageCutoff caps history reads at a year so an extension can't force an
// unbounded scan; zero/negative means the default 30 days.
func usageCutoff(sinceDays int) time.Time {
	if sinceDays <= 0 {
		sinceDays = 30
	}
	if sinceDays > 365 {
		sinceDays = 365
	}
	return time.Now().AddDate(0, 0, -sinceDays)
}
