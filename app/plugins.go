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

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/buildinfo"
	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/metadata"
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

// knownPluginPermissions mirrors the loader's list (host.ts); publish
// validation and load validation must accept the same universe.
var knownPluginPermissions = map[string]bool{
	"assets:read": true, "usage:read": true, "drafts:write": true,
	"views:sidebar": true, "views:asset-tab": true, "views:dashboard": true,
	"commands": true, "events": true,
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
	return atomicWriteFile(filepath.Join(dir, id+".data.json"), []byte(data))
}

// atomicWriteFile writes via a UNIQUE temp file + rename. A fixed temp
// name races when two callers save concurrently (two mounted views of
// the same extension, say): both write the one temp file and the loser's
// rename fails with ENOENT. CreateTemp gives each writer its own file;
// last rename wins, nobody errors.
func atomicWriteFile(target string, data []byte) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, filepath.Base(target)+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
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
	if err := atomicWriteFile(filepath.Join(dir, "decisions.json"), data); err != nil {
		return err
	}
	// Fire-and-forget: on a git vault the audit append is a full
	// pull+commit+push, and a per-user preference toggle must not block
	// the UI on network I/O. The decision file above is the durable
	// state. The event (incl. timestamp) is built NOW so rapid toggles
	// log in decision order even if the goroutines run out of order.
	event := mgmt.AuditEvent{
		Timestamp:  time.Now(),
		Actor:      strings.TrimSpace(a.GetVaultInfo().Identity),
		Event:      mgmt.EventPluginEnabled,
		TargetType: mgmt.TargetTypePlugin, Target: id,
	}
	if !enabled {
		event.Event = mgmt.EventPluginDisabled
	}
	go a.appendPluginAudit(event)
	return nil
}

func (a *App) appendPluginAudit(event mgmt.AuditEvent) {
	v, err := a.currentVault()
	if err != nil {
		return
	}
	if err := v.ImportAuditEvents(a.ctx, []mgmt.AuditEvent{event}); err != nil {
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
	return atomicWriteFile(filepath.Join(dir, "consents.json"), data)
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
	cutoff := usageCutoff(sinceDays)
	// Since bounds the read server-side/file-side so a dashboard widget
	// never walks an org's whole history.
	events, err := v.ReadUsageEvents(a.ctx, mgmt.UsageFilter{Since: cutoff})
	if err != nil {
		return nil, friendlyVaultError(err)
	}
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
	cutoff := usageCutoff(sinceDays)
	// Both bounds matter: Since scopes the window, Limit caps the walk —
	// unbounded, the sleuth backend pages an org's ENTIRE audit log,
	// which is slow at best and 502s at worst.
	events, err := v.QueryAuditEvents(a.ctx, mgmt.AuditFilter{Since: cutoff, Limit: 500})
	if err != nil {
		return nil, friendlyVaultError(err)
	}
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

	// Draft ids are slugified names, and same-name creates overwrite —
	// so two imported items declaring the same name would silently
	// collapse. Track ids produced by THIS batch and report the second
	// occurrence as a skip instead of double-counting a clobber.
	seen := map[string]bool{}
	for _, c := range candidates {
		draft, err := a.CreateDraftFromPaths([]string{c})
		if err != nil {
			res.Skipped++
			continue
		}
		if seen[draft.ID] {
			res.Skipped++
			logger.Get().Warn("import name collision; later item overwrote the draft", "draft", draft.ID)
			continue
		}
		seen[draft.ID] = true
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

// AddExtensionFromFolder publishes an extension folder (plugin.json +
// main.js, per docs/app-plugin-authoring.md) into the current vault as an
// app-plugin asset — the Extensions screen's "Add extension" path. A
// missing metadata.toml is tolerated: type and description are forced
// from plugin.json so authors only maintain one manifest. Returns the
// published name, "" on cancel.
func (a *App) AddExtensionFromFolder() (string, error) {
	dir, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Choose an extension folder (plugin.json + main.js)",
	})
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", nil // cancelled
	}
	return a.addExtensionFrom(dir)
}

// addExtensionFrom is the dialog-free publish core, split out for tests.
func (a *App) addExtensionFrom(dir string) (string, error) {
	if !a.VaultSupportsExtensions() {
		return "", errExtensionsUnsupported
	}
	manifestBytes, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return "", errors.New("that folder has no plugin.json — see docs/app-plugin-authoring.md")
	}
	var pm struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Version     string   `json:"version"`
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal(manifestBytes, &pm); err != nil {
		return "", errors.New("plugin.json is not valid JSON")
	}
	// Publish-time validation mirrors the loader (host.ts
	// parseVaultManifest) so nothing can publish successfully and then
	// silently fail to load.
	if err := validatePluginID(pm.ID); err != nil {
		return "", err
	}
	if pm.ID == "sx" || strings.HasPrefix(pm.ID, "sx-") {
		return "", errors.New(`extension ids may not claim the "sx" prefix`)
	}
	if pm.Name == "" || pm.Version == "" {
		return "", errors.New("plugin.json needs name and version")
	}
	if pm.Permissions == nil {
		return "", errors.New("plugin.json needs a permissions array (may be empty)")
	}
	for _, perm := range pm.Permissions {
		if !knownPluginPermissions[perm] {
			return "", fmt.Errorf("plugin.json declares unknown permission %q", perm)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "main.js")); err != nil {
		return "", errors.New("that folder has no main.js — bundle your extension to a single ES module")
	}

	draft, err := a.CreateDraftFromPaths([]string{dir})
	if err != nil {
		return "", err
	}
	draft.Name = pm.ID
	draft.Type = asset.TypeAppPlugin.Key
	draft.TypeLabel = asset.TypeAppPlugin.Label
	if pm.Description != "" {
		draft.Description = pm.Description
	}
	updated, err := a.UpdateDraft(draft)
	if err != nil {
		return "", err
	}
	card, err := a.PublishDraft(updated.ID)
	if err != nil {
		return "", err
	}
	return card.Name, nil
}

// VaultPlugin is a vault-installed extension as the frontend consumes it:
// the plugin.json manifest (runtime source of truth) plus its code.
type VaultPlugin struct {
	AssetName string `json:"assetName"`
	Manifest  string `json:"manifest"` // raw plugin.json
	Source    string `json:"source"`   // bundled ES module (entry file)
}

// maxPluginSourceBytes bounds a bundle so a hostile vault entry can't
// balloon the webview; 5 MB is generous for a bundled extension.
const maxPluginSourceBytes = 5 << 20

// ListVaultPlugins returns every app-plugin asset in the current vault
// with its manifest and code, ready for the host's Blob loader. Assets
// missing plugin.json or their entry file are skipped with a log — a
// malformed extension must not break the Extensions screen.
func (a *App) ListVaultPlugins() ([]VaultPlugin, error) {
	out := []VaultPlugin{}
	v, err := a.currentVault()
	if err != nil {
		return out, nil
	}
	res, err := v.ListAssets(a.ctx, vaultpkg.ListAssetsOptions{Type: asset.TypeAppPlugin.Key, Limit: 200})
	if err != nil {
		// Backends without the type (skills.new until P5) list nothing.
		return out, nil
	}
	for _, summary := range res.Assets {
		plugin, err := a.loadVaultPlugin(summary.Name)
		if err != nil {
			logger.Get().Warn("skipping malformed extension asset", "asset", summary.Name, "error", err)
			continue
		}
		out = append(out, plugin)
	}
	return out, nil
}

func (a *App) loadVaultPlugin(name string) (VaultPlugin, error) {
	zipData, err := a.latestAssetZip(name)
	if err != nil {
		return VaultPlugin{}, err
	}
	manifestBytes, err := utils.ReadZipFile(zipData, "plugin.json")
	if err != nil {
		return VaultPlugin{}, errors.New("no plugin.json in the bundle")
	}
	entry := "main.js"
	if metaBytes, err := utils.ReadZipFile(zipData, "metadata.toml"); err == nil {
		if meta, err := metadata.Parse(metaBytes); err == nil && meta.AppPlugin != nil && meta.AppPlugin.Entry != "" {
			entry = meta.AppPlugin.Entry
		}
	}
	source, err := utils.ReadZipFile(zipData, entry)
	if err != nil {
		return VaultPlugin{}, fmt.Errorf("entry file %s missing from the bundle", entry)
	}
	if len(source) > maxPluginSourceBytes {
		return VaultPlugin{}, fmt.Errorf("bundle exceeds %d bytes", maxPluginSourceBytes)
	}
	return VaultPlugin{
		AssetName: name,
		Manifest:  string(manifestBytes),
		Source:    string(source),
	}, nil
}

// PluginUserActivity is one user's usage inside the window.
type PluginUserActivity struct {
	Actor          string `json:"actor"`
	Events         int    `json:"events"`
	DistinctAssets int    `json:"distinctAssets"`
}

// PluginUserStatsResult feeds adoption/leaderboard widgets: everyone the
// vault knows about (team members ∪ usage actors ∪ the caller) plus
// per-user activity within the window.
type PluginUserStatsResult struct {
	KnownUsers []string             `json:"knownUsers"`
	Active     []PluginUserActivity `json:"active"`
}

// PluginUserStats aggregates usage by user — the usage:read capability.
func (a *App) PluginUserStats(sinceDays int) (PluginUserStatsResult, error) {
	res := PluginUserStatsResult{KnownUsers: []string{}, Active: []PluginUserActivity{}}
	v, err := a.currentVault()
	if err != nil {
		return res, err
	}
	// Identities normalize through the same rule everywhere (manifest
	// email normalization) so one person's team entry and usage-actor
	// string can't double-count in the adoption denominator. A recorder
	// that logs display names instead of emails still counts separately —
	// there is nothing to join on.
	known := map[string]bool{}
	if self := manifest.NormalizeEmail(a.GetVaultInfo().Identity); self != "" {
		known[self] = true
	}
	if teams, err := a.ListTeams(); err == nil {
		for _, team := range teams {
			for _, m := range team.Members {
				if n := manifest.NormalizeEmail(m); n != "" {
					known[n] = true
				}
			}
		}
	}
	cutoff := usageCutoff(sinceDays)
	events, err := v.ReadUsageEvents(a.ctx, mgmt.UsageFilter{Since: cutoff})
	if err != nil {
		return res, friendlyVaultError(err)
	}
	type agg struct {
		events int
		assets map[string]bool
	}
	byActor := map[string]*agg{}
	for _, e := range events {
		if e.Timestamp.Before(cutoff) {
			continue
		}
		actor := manifest.NormalizeEmail(e.Actor)
		if actor == "" {
			continue
		}
		known[actor] = true
		entry := byActor[actor]
		if entry == nil {
			entry = &agg{assets: map[string]bool{}}
			byActor[actor] = entry
		}
		entry.events++
		entry.assets[e.AssetName] = true
	}
	for actor := range known {
		res.KnownUsers = append(res.KnownUsers, actor)
	}
	sort.Strings(res.KnownUsers)
	for actor, entry := range byActor {
		res.Active = append(res.Active, PluginUserActivity{
			Actor: actor, Events: entry.events, DistinctAssets: len(entry.assets),
		})
	}
	sort.Slice(res.Active, func(i, j int) bool {
		if res.Active[i].DistinctAssets != res.Active[j].DistinctAssets {
			return res.Active[i].DistinctAssets > res.Active[j].DistinctAssets
		}
		return res.Active[i].Events > res.Active[j].Events
	})
	return res, nil
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
