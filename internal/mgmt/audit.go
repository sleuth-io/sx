package mgmt

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// AuditDirName is the directory under the vault root that holds monthly
// audit JSONL files.
const AuditDirName = ".sx/audit"

// Audit event names. Most mirror the set skills.new emits so dashboards and
// downstream consumers can treat the two streams interchangeably; local-only
// vault maintenance events are explicit where they do not have a server
// equivalent.
const (
	EventTeamCreated       = "team.created"
	EventTeamUpdated       = "team.updated"
	EventTeamDeleted       = "team.deleted"
	EventTeamMemberAdded   = "team.member_added"
	EventTeamMemberRemoved = "team.member_removed"
	EventTeamAdminSet      = "team.admin_set"
	EventTeamAdminUnset    = "team.admin_unset"
	EventTeamRepoAdded     = "team.repo_added"
	EventTeamRepoRemoved   = "team.repo_removed"
	EventBotCreated        = "bot.created"
	EventBotUpdated        = "bot.updated"
	EventBotDeleted        = "bot.deleted"
	EventBotTeamAdded      = "bot.team_added"
	EventBotTeamRemoved    = "bot.team_removed"
	// Bot API key lifecycle events live on the Sleuth server's audit
	// stream, not the local .sx/audit JSONL log: file-based vaults
	// reject bot key operations entirely (BotApiKeyManager is
	// Sleuth-only), and Sleuth's audit is captured server-side. No
	// local constants are defined for those events to avoid implying
	// audit coverage that the local log doesn't have. See docs/bots.md.
	EventAssetCreated   = "asset.created"
	EventAssetRecovered = "asset.recovered"
	EventAssetUpdated   = "asset.updated"
	EventAssetRemoved   = "asset.removed"
	EventAssetRenamed   = "asset.renamed"
	EventInstallSet     = "install.set"
	EventInstallCleared = "install.cleared"
	EventInstallRemoved = "install.removed"
)

// Audit target type constants.
const (
	TargetTypeTeam         = "team"
	TargetTypeBot          = "bot"
	TargetTypeAsset        = "asset"
	TargetTypeInstallation = "installation"
)

// AuditEvent is a single row in .sx/audit/YYYY-MM.jsonl.
type AuditEvent struct {
	Timestamp  time.Time `json:"ts"`
	Actor      string    `json:"actor"`
	Event      string    `json:"event"`
	TargetType string    `json:"target_type"`
	Target     string    `json:"target"`
	// Profile records which sx profile produced the event. Optional —
	// older sx versions did not populate it. Useful for cross-vault
	// correlation when a user runs multiple profiles against the same
	// audit stream.
	Profile string         `json:"profile,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
}

// AuditFilter narrows an audit query. Zero values mean "don't filter on
// that field". EventPrefix matches if the event string starts with the
// prefix (e.g. "team." matches every team event).
type AuditFilter struct {
	Actor       string
	EventPrefix string
	Target      string
	Since       time.Time
	Until       time.Time
	Limit       int
}

// auditProfileTag is the active profile name reported on every emitted
// audit event. Populated by SetAuditProfileTag at command boot so call
// sites don't have to thread the profile through every AuditEvent
// literal. Guarded by auditProfileTagMu — vault mutation paths may emit
// events from background goroutines.
var (
	auditProfileTagMu sync.RWMutex
	auditProfileTag   string
)

// SetAuditProfileTag records the profile name to stamp onto subsequent
// AppendAuditEvent calls. Empty disables tagging.
func SetAuditProfileTag(name string) {
	auditProfileTagMu.Lock()
	auditProfileTag = strings.TrimSpace(name)
	auditProfileTagMu.Unlock()
}

// getAuditProfileTag reads the tag under the package mutex.
func getAuditProfileTag() string {
	auditProfileTagMu.RLock()
	defer auditProfileTagMu.RUnlock()
	return auditProfileTag
}

// AppendAuditEvent appends an event to the monthly audit file for the
// event's timestamp. The parent directory is created if needed. Stamps
// the active profile (when set via SetAuditProfileTag) so consumers can
// correlate cross-profile activity.
func AppendAuditEvent(vaultRoot string, event AuditEvent) error {
	return AppendAuditEvents(vaultRoot, []AuditEvent{event})
}

// AppendAuditEvents appends a batch of events to the monthly audit files.
// Events without timestamps share a single timestamp so related audit rows
// stay together in the log.
func AppendAuditEvents(vaultRoot string, events []AuditEvent) error {
	if len(events) == 0 {
		return nil
	}
	now := time.Now().UTC()
	profile := getAuditProfileTag()
	byPath := make(map[string][]AuditEvent)
	for _, event := range events {
		if event.Timestamp.IsZero() {
			event.Timestamp = now
		} else {
			event.Timestamp = event.Timestamp.UTC()
		}
		if event.Profile == "" {
			event.Profile = profile
		}
		path := filepath.Join(vaultRoot, AuditDirName, monthFile(event.Timestamp))
		byPath[path] = append(byPath[path], event)
	}

	dir := filepath.Join(vaultRoot, AuditDirName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create audit directory: %w", err)
	}
	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := appendJSONL(path, byPath[path]); err != nil {
			return err
		}
	}
	return nil
}

// QueryAuditEvents reads all monthly files under .sx/audit and returns the
// events that match the filter, sorted newest first. A non-existent audit
// directory returns an empty slice, not an error.
func QueryAuditEvents(vaultRoot string, filter AuditFilter) ([]AuditEvent, error) {
	events, err := readMonthlyJSONLDir[AuditEvent](filepath.Join(vaultRoot, AuditDirName))
	if err != nil {
		return nil, err
	}

	var matched []AuditEvent
	for _, ev := range events {
		if matchesAuditFilter(ev, filter) {
			matched = append(matched, ev)
		}
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].Timestamp.After(matched[j].Timestamp)
	})

	if filter.Limit > 0 && len(matched) > filter.Limit {
		matched = matched[:filter.Limit]
	}
	return matched, nil
}

func matchesAuditFilter(ev AuditEvent, f AuditFilter) bool {
	if f.Actor != "" && !strings.EqualFold(ev.Actor, f.Actor) {
		return false
	}
	if f.EventPrefix != "" && !strings.HasPrefix(ev.Event, f.EventPrefix) {
		return false
	}
	if f.Target != "" && ev.Target != f.Target {
		return false
	}
	if !f.Since.IsZero() && ev.Timestamp.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && ev.Timestamp.After(f.Until) {
		return false
	}
	return true
}

// monthFile returns the YYYY-MM.jsonl file name for the given timestamp.
func monthFile(t time.Time) string {
	return t.UTC().Format("2006-01") + ".jsonl"
}
