package mgmt

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// AuditDirName is the directory under the vault root that holds monthly
// audit JSONL files.
const AuditDirName = ".sx/audit"

// Audit event names. These mirror the set skills.new emits so dashboards and
// downstream consumers can treat the two streams interchangeably.
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
	EventAssetCreated      = "asset.created"
	EventAssetUpdated      = "asset.updated"
	EventAssetRemoved      = "asset.removed"
	EventAssetRenamed      = "asset.renamed"
	EventInstallSet        = "install.set"
	EventInstallCleared    = "install.cleared"
)

// Audit target type constants.
const (
	TargetTypeTeam         = "team"
	TargetTypeAsset        = "asset"
	TargetTypeInstallation = "installation"
)

// AuditEvent is a single row in .sx/audit/YYYY-MM.jsonl.
type AuditEvent struct {
	Timestamp  time.Time      `json:"ts"`
	Actor      string         `json:"actor"`
	Event      string         `json:"event"`
	TargetType string         `json:"target_type"`
	Target     string         `json:"target"`
	Data       map[string]any `json:"data,omitempty"`
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

// AppendAuditEvent appends an event to the monthly audit file for the
// event's timestamp. The parent directory is created if needed.
func AppendAuditEvent(vaultRoot string, event AuditEvent) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	} else {
		event.Timestamp = event.Timestamp.UTC()
	}

	dir := filepath.Join(vaultRoot, AuditDirName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create audit directory: %w", err)
	}
	path := filepath.Join(dir, monthFile(event.Timestamp))
	return appendJSONL(path, []AuditEvent{event})
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
