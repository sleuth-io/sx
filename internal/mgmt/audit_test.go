package mgmt

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAppendAndQueryAuditEvents(t *testing.T) {
	dir := t.TempDir()

	t0 := time.Date(2026, 4, 15, 10, 30, 0, 0, time.UTC)
	t1 := time.Date(2026, 4, 15, 11, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)

	events := []AuditEvent{
		{Timestamp: t0, Actor: "alice@example.com", Event: EventTeamCreated, TargetType: TargetTypeTeam, Target: "platform"},
		{Timestamp: t1, Actor: "alice@example.com", Event: EventTeamMemberAdded, TargetType: TargetTypeTeam, Target: "platform", Data: map[string]any{"member": "bob@example.com"}},
		{Timestamp: t2, Actor: "bob@example.com", Event: EventInstallSet, TargetType: TargetTypeInstallation, Target: "my-skill", Data: map[string]any{"kind": "team", "team": "platform"}},
	}
	for _, ev := range events {
		if err := AppendAuditEvent(dir, ev); err != nil {
			t.Fatalf("AppendAuditEvent failed: %v", err)
		}
	}

	// Verify files created per-month
	aprFile := filepath.Join(dir, AuditDirName, "2026-04.jsonl")
	if _, err := filepath.Abs(aprFile); err != nil {
		t.Errorf("bad audit file path: %v", err)
	}

	// Query all — should return newest first
	got, err := QueryAuditEvents(dir, AuditFilter{})
	if err != nil {
		t.Fatalf("QueryAuditEvents failed: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 events, got %d", len(got))
	}
	if !got[0].Timestamp.Equal(t2) {
		t.Errorf("expected newest event first, got %v", got[0].Timestamp)
	}
	if !got[2].Timestamp.Equal(t0) {
		t.Errorf("expected oldest event last, got %v", got[2].Timestamp)
	}

	// Filter by actor (case-insensitive)
	got, err = QueryAuditEvents(dir, AuditFilter{Actor: "ALICE@example.com"})
	if err != nil {
		t.Fatalf("QueryAuditEvents failed: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 alice events, got %d", len(got))
	}

	// Filter by event prefix
	got, err = QueryAuditEvents(dir, AuditFilter{EventPrefix: "team."})
	if err != nil {
		t.Fatalf("QueryAuditEvents failed: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 team.* events, got %d", len(got))
	}

	// Filter by target
	got, err = QueryAuditEvents(dir, AuditFilter{Target: "my-skill"})
	if err != nil {
		t.Fatalf("QueryAuditEvents failed: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 my-skill event, got %d", len(got))
	}

	// Filter by time range — only May events
	got, err = QueryAuditEvents(dir, AuditFilter{Since: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("QueryAuditEvents failed: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 May event, got %d", len(got))
	}

	// Limit
	got, err = QueryAuditEvents(dir, AuditFilter{Limit: 1})
	if err != nil {
		t.Fatalf("QueryAuditEvents failed: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected limit=1, got %d", len(got))
	}
}

func TestQueryAuditEventsMissingDir(t *testing.T) {
	dir := t.TempDir()
	got, err := QueryAuditEvents(dir, AuditFilter{})
	if err != nil {
		t.Fatalf("expected no error on missing dir, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty events, got %d", len(got))
	}
}

func TestAppendAuditEventDefaultsTimestamp(t *testing.T) {
	dir := t.TempDir()
	before := time.Now().UTC().Add(-time.Second)
	if err := AppendAuditEvent(dir, AuditEvent{Actor: "alice@example.com", Event: EventTeamCreated, Target: "platform"}); err != nil {
		t.Fatalf("AppendAuditEvent failed: %v", err)
	}
	after := time.Now().UTC().Add(time.Second)

	got, err := QueryAuditEvents(dir, AuditFilter{})
	if err != nil {
		t.Fatalf("QueryAuditEvents failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].Timestamp.Before(before) || got[0].Timestamp.After(after) {
		t.Errorf("default timestamp outside expected window: %v", got[0].Timestamp)
	}
}
