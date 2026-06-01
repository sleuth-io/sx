package vault

import (
	"context"
	"testing"
	"time"

	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
)

// TestPathVault_ImportAuditEventsRoundTrip verifies that audit events imported
// into a file-backed vault are persisted verbatim — original timestamp, actor,
// event, target, and data all survive a write/read round-trip.
func TestPathVault_ImportAuditEventsRoundTrip(t *testing.T) {
	mgmt.ResetActorCache()
	dir := t.TempDir()

	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "alice@example.com")
	runGit(t, dir, "config", "user.name", "Alice Admin")

	if err := manifest.Save(dir, &manifest.Manifest{SchemaVersion: manifest.CurrentSchemaVersion}); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}

	v, err := NewPathVault("file://" + dir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	ctx := context.Background()

	ts := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	events := []mgmt.AuditEvent{
		{
			Timestamp:  ts,
			Actor:      "bob@example.com",
			Event:      "team.created",
			TargetType: "team",
			Target:     "platform",
			Data:       map[string]any{"description": "core"},
		},
		{
			Timestamp:  ts.Add(time.Hour),
			Actor:      "carol@example.com",
			Event:      "install.set",
			TargetType: "installation",
			Target:     "my-skill",
		},
	}

	if err := v.ImportAuditEvents(ctx, events); err != nil {
		t.Fatalf("ImportAuditEvents: %v", err)
	}

	got, err := v.QueryAuditEvents(ctx, mgmt.AuditFilter{})
	if err != nil {
		t.Fatalf("QueryAuditEvents: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2", len(got))
	}

	// Newest first.
	if got[0].Event != "install.set" || got[0].Actor != "carol@example.com" {
		t.Errorf("event[0] = %q by %q, want install.set by carol@example.com", got[0].Event, got[0].Actor)
	}
	if !got[1].Timestamp.Equal(ts) {
		t.Errorf("event[1] timestamp = %v, want %v", got[1].Timestamp, ts)
	}
	if got[1].Event != "team.created" || got[1].TargetType != "team" || got[1].Target != "platform" {
		t.Errorf("event[1] = %+v, want team.created/team/platform", got[1])
	}
	if got[1].Data["description"] != "core" {
		t.Errorf("event[1] data = %v, want description=core", got[1].Data)
	}

	// Empty import is a no-op.
	if err := v.ImportAuditEvents(ctx, nil); err != nil {
		t.Fatalf("ImportAuditEvents(nil): %v", err)
	}
}
