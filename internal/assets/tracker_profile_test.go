package assets

import (
	"os"
	"path/filepath"
	"testing"
)

// An installed asset must remember which profile installed it, so
// `vault list --installed --profile X` can show only X's assets.
// Empty profile means the default profile.

func TestInstalledAsset_ProfileRoundTrips(t *testing.T) {
	t.Setenv("SX_CACHE_DIR", t.TempDir())

	tracker := &Tracker{
		Version: TrackerFormatVersion,
		Assets: []InstalledAsset{
			{Name: "chat", Version: "1.0", Type: "skill", Clients: []string{"claude-code"}, Profile: "gh"},
			{Name: "coding-standards", Version: "2.0", Type: "skill", Clients: []string{"claude-code"}}, // default profile
		},
	}

	if err := SaveTracker(tracker); err != nil {
		t.Fatalf("SaveTracker() error = %v", err)
	}

	loaded, err := LoadTracker()
	if err != nil {
		t.Fatalf("LoadTracker() error = %v", err)
	}

	got := map[string]string{}
	for _, a := range loaded.Assets {
		got[a.Name] = a.Profile
	}

	if got["chat"] != "gh" {
		t.Errorf("chat profile = %q, want %q", got["chat"], "gh")
	}
	if got["coding-standards"] != "" {
		t.Errorf("coding-standards profile = %q, want empty (default)", got["coding-standards"])
	}
}

// Trackers written before this field existed have no "profile" key. They
// must load with an empty profile (meaning default), not fail.
func TestInstalledAsset_OldTrackerLoadsWithEmptyProfile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SX_CACHE_DIR", dir)

	old := `{
  "version": "3",
  "assets": [
    {"name": "frontend-design", "version": "1.0", "type": "skill", "clients": ["claude-code"]}
  ]
}`
	if err := os.WriteFile(filepath.Join(dir, "installed.json"), []byte(old), 0644); err != nil {
		t.Fatalf("write old tracker: %v", err)
	}

	loaded, err := LoadTracker()
	if err != nil {
		t.Fatalf("LoadTracker() error = %v", err)
	}
	if len(loaded.Assets) != 1 {
		t.Fatalf("loaded %d assets, want 1", len(loaded.Assets))
	}
	if loaded.Assets[0].Profile != "" {
		t.Errorf("old asset profile = %q, want empty", loaded.Assets[0].Profile)
	}
}
