package mgmt

import (
	"testing"
	"time"
)

func TestAppendAndReadUsageEvents(t *testing.T) {
	dir := t.TempDir()

	t0 := time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 4, 15, 11, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)

	events := []UsageEvent{
		{Timestamp: t0, Actor: "Alice@example.com", AssetName: "my-skill", AssetVersion: "1.0.0", AssetType: "skill"},
		{Timestamp: t1, Actor: "bob@example.com", AssetName: "my-skill", AssetVersion: "1.0.0", AssetType: "skill"},
		{Timestamp: t2, Actor: "alice@example.com", AssetName: "other", AssetVersion: "2.0.0", AssetType: "rule"},
	}

	if err := AppendUsageEvents(dir, events); err != nil {
		t.Fatalf("AppendUsageEvents failed: %v", err)
	}

	got, err := ReadUsageEvents(dir, UsageFilter{})
	if err != nil {
		t.Fatalf("ReadUsageEvents failed: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 events, got %d", len(got))
	}

	// Actor should be normalized
	for _, ev := range got {
		if ev.Actor == "Alice@example.com" {
			t.Errorf("expected normalized actor, got %s", ev.Actor)
		}
	}

	// Filter by asset
	got, err = ReadUsageEvents(dir, UsageFilter{AssetName: "my-skill"})
	if err != nil {
		t.Fatalf("ReadUsageEvents failed: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 my-skill events, got %d", len(got))
	}

	// Filter by time
	got, err = ReadUsageEvents(dir, UsageFilter{Since: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("ReadUsageEvents failed: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 May event, got %d", len(got))
	}
}

func TestSummarizeUsage(t *testing.T) {
	dir := t.TempDir()

	t0 := time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC)
	events := []UsageEvent{
		{Timestamp: t0, Actor: "alice@example.com", AssetName: "my-skill", AssetType: "skill"},
		{Timestamp: t0.Add(time.Hour), Actor: "alice@example.com", AssetName: "my-skill", AssetType: "skill"},
		{Timestamp: t0.Add(2 * time.Hour), Actor: "bob@example.com", AssetName: "my-skill", AssetType: "skill"},
		{Timestamp: t0.Add(3 * time.Hour), Actor: "carol@example.com", AssetName: "other", AssetType: "rule"},
	}

	if err := AppendUsageEvents(dir, events); err != nil {
		t.Fatalf("AppendUsageEvents failed: %v", err)
	}

	summary, err := SummarizeUsage(dir, UsageFilter{})
	if err != nil {
		t.Fatalf("SummarizeUsage failed: %v", err)
	}

	if summary.TotalEvents != 4 {
		t.Errorf("expected 4 total events, got %d", summary.TotalEvents)
	}
	if len(summary.PerAsset) != 2 {
		t.Fatalf("expected 2 assets, got %d", len(summary.PerAsset))
	}

	// my-skill should be first (3 uses vs 1)
	if summary.PerAsset[0].AssetName != "my-skill" {
		t.Errorf("expected my-skill first, got %s", summary.PerAsset[0].AssetName)
	}
	if summary.PerAsset[0].TotalUses != 3 {
		t.Errorf("expected 3 uses for my-skill, got %d", summary.PerAsset[0].TotalUses)
	}
	if summary.PerAsset[0].UniqueActors != 2 {
		t.Errorf("expected 2 unique actors, got %d", summary.PerAsset[0].UniqueActors)
	}
	lastExpected := t0.Add(2 * time.Hour)
	if !summary.PerAsset[0].LastUsed.Equal(lastExpected) {
		t.Errorf("expected LastUsed=%v, got %v", lastExpected, summary.PerAsset[0].LastUsed)
	}

	// Alice should be top actor (2 uses)
	if len(summary.PerActor) != 3 {
		t.Errorf("expected 3 actors, got %d", len(summary.PerActor))
	}
	if summary.PerActor[0].Actor != "alice@example.com" || summary.PerActor[0].TotalUses != 2 {
		t.Errorf("expected alice@2, got %v", summary.PerActor[0])
	}
}

func TestReadUsageEventsMissingDir(t *testing.T) {
	dir := t.TempDir()
	got, err := ReadUsageEvents(dir, UsageFilter{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty events, got %d", len(got))
	}
}
