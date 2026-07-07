package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/mgmt"
	"github.com/sleuth-io/sx/internal/utils"
)

// App-extension support (docs/app-plugins-spec.md). The webview has no
// filesystem: everything an extension persists or reads flows through
// these bridge methods, which is what makes the API surface the blast
// radius. Storage is per plugin, per profile, app-side (never in the
// vault).

var pluginIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,63}$`)

// defaultEnabledBuiltins ship enabled; the frontend host registers them.
// Kept in Go so first-toggle materialization matches the frontend's boot
// defaults.
var defaultEnabledBuiltins = []string{"library-dashboard", "publish-doctor"}

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

// PluginEnabledState distinguishes "never configured" (built-ins default
// on) from "user disabled everything" (respect it).
type PluginEnabledState struct {
	Configured bool     `json:"configured"`
	Enabled    []string `json:"enabled"`
}

// EnabledPlugins returns the per-profile enabled extension ids.
func (a *App) EnabledPlugins() (PluginEnabledState, error) {
	state := PluginEnabledState{Enabled: []string{}}
	dir, err := a.pluginDataDir()
	if err != nil {
		return state, nil // no profile yet — defaults apply
	}
	data, err := os.ReadFile(filepath.Join(dir, "enabled.json"))
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		return state, err
	}
	sort.Strings(ids)
	state.Configured = true
	state.Enabled = ids
	return state, nil
}

// SetPluginEnabled persists an extension's enabled state.
func (a *App) SetPluginEnabled(id string, enabled bool) error {
	if err := validatePluginID(id); err != nil {
		return err
	}
	dir, err := a.pluginDataDir()
	if err != nil {
		return err
	}
	current, err := a.EnabledPlugins()
	if err != nil {
		return err
	}
	set := map[string]bool{}
	for _, existing := range current.Enabled {
		set[existing] = true
	}
	if !current.Configured {
		// First toggle ever: materialize the defaults (all built-ins on)
		// so disabling one doesn't silently disable the others.
		for _, builtin := range defaultEnabledBuiltins {
			set[builtin] = true
		}
	}
	set[id] = enabled
	if !enabled {
		delete(set, id)
	}
	ids := make([]string, 0, len(set))
	for existing := range set {
		ids = append(ids, existing)
	}
	sort.Strings(ids)
	data, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	target := filepath.Join(dir, "enabled.json")
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, target)
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
