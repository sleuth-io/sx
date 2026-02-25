package commands

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/assets"
)

func TestFilterUninstallPlanByClients(t *testing.T) {
	tests := []struct {
		name        string
		plan        UninstallPlan
		clientsFlag string
		wantAssets  int
		wantClients map[string][]string // asset name -> expected clients
	}{
		{
			name: "filter to single client",
			plan: UninstallPlan{
				Assets: []AssetUninstallPlan{
					{
						Name:    "skill-1",
						Type:    asset.TypeSkill,
						Clients: []string{"claude-code", "gemini", "cursor"},
					},
				},
			},
			clientsFlag: "gemini",
			wantAssets:  1,
			wantClients: map[string][]string{
				"skill-1": {"gemini"},
			},
		},
		{
			name: "filter to multiple clients",
			plan: UninstallPlan{
				Assets: []AssetUninstallPlan{
					{
						Name:    "skill-1",
						Type:    asset.TypeSkill,
						Clients: []string{"claude-code", "gemini", "cursor"},
					},
				},
			},
			clientsFlag: "gemini,cursor",
			wantAssets:  1,
			wantClients: map[string][]string{
				"skill-1": {"gemini", "cursor"},
			},
		},
		{
			name: "filter removes asset with no matching clients",
			plan: UninstallPlan{
				Assets: []AssetUninstallPlan{
					{
						Name:    "skill-1",
						Type:    asset.TypeSkill,
						Clients: []string{"claude-code"},
					},
					{
						Name:    "skill-2",
						Type:    asset.TypeSkill,
						Clients: []string{"gemini"},
					},
				},
			},
			clientsFlag: "gemini",
			wantAssets:  1,
			wantClients: map[string][]string{
				"skill-2": {"gemini"},
			},
		},
		{
			name: "filter with spaces in flag",
			plan: UninstallPlan{
				Assets: []AssetUninstallPlan{
					{
						Name:    "skill-1",
						Type:    asset.TypeSkill,
						Clients: []string{"claude-code", "gemini"},
					},
				},
			},
			clientsFlag: "claude-code , gemini",
			wantAssets:  1,
			wantClients: map[string][]string{
				"skill-1": {"claude-code", "gemini"},
			},
		},
		{
			name: "no matching clients returns empty",
			plan: UninstallPlan{
				Assets: []AssetUninstallPlan{
					{
						Name:    "skill-1",
						Type:    asset.TypeSkill,
						Clients: []string{"claude-code"},
					},
				},
			},
			clientsFlag: "gemini",
			wantAssets:  0,
			wantClients: map[string][]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterUninstallPlanByClients(tt.plan, tt.clientsFlag)

			if len(result.Assets) != tt.wantAssets {
				t.Errorf("got %d assets, want %d", len(result.Assets), tt.wantAssets)
			}

			for _, asset := range result.Assets {
				expectedClients, ok := tt.wantClients[asset.Name]
				if !ok {
					t.Errorf("unexpected asset %q in result", asset.Name)
					continue
				}

				if len(asset.Clients) != len(expectedClients) {
					t.Errorf("asset %q: got %d clients, want %d", asset.Name, len(asset.Clients), len(expectedClients))
					continue
				}

				for i, client := range asset.Clients {
					if client != expectedClients[i] {
						t.Errorf("asset %q: client[%d] = %q, want %q", asset.Name, i, client, expectedClients[i])
					}
				}
			}
		})
	}
}

func TestUpdateTrackerPartialClientRemoval(t *testing.T) {
	// Set up temp cache directory for tracker
	tempDir := t.TempDir()
	originalXDG := os.Getenv("XDG_CACHE_HOME")
	os.Setenv("XDG_CACHE_HOME", tempDir)
	defer os.Setenv("XDG_CACHE_HOME", originalXDG)

	// Ensure the sx subdirectory exists
	sxCacheDir := filepath.Join(tempDir, "sx")
	if err := os.MkdirAll(sxCacheDir, 0755); err != nil {
		t.Fatalf("failed to create cache dir: %v", err)
	}

	tests := []struct {
		name            string
		initialTracker  *assets.Tracker
		results         []UninstallResult
		expectedClients map[string][]string // assetKey -> expected remaining clients
		expectedRemoved []string            // asset keys that should be fully removed
	}{
		{
			name: "partial removal keeps other clients",
			initialTracker: &assets.Tracker{
				Version: "3",
				Assets: []assets.InstalledAsset{
					{
						Name:    "skill-1",
						Version: "1.0.0",
						Clients: []string{"claude-code", "github-copilot", "gemini"},
					},
				},
			},
			results: []UninstallResult{
				{AssetName: "skill-1", ClientID: "github-copilot", Success: true},
			},
			expectedClients: map[string][]string{
				"skill-1||": {"claude-code", "gemini"},
			},
		},
		{
			name: "removing all clients removes asset",
			initialTracker: &assets.Tracker{
				Version: "3",
				Assets: []assets.InstalledAsset{
					{
						Name:    "skill-1",
						Version: "1.0.0",
						Clients: []string{"github-copilot"},
					},
				},
			},
			results: []UninstallResult{
				{AssetName: "skill-1", ClientID: "github-copilot", Success: true},
			},
			expectedClients: map[string][]string{},
			expectedRemoved: []string{"skill-1||"},
		},
		{
			name: "failed removal does not affect tracker",
			initialTracker: &assets.Tracker{
				Version: "3",
				Assets: []assets.InstalledAsset{
					{
						Name:    "skill-1",
						Version: "1.0.0",
						Clients: []string{"claude-code", "github-copilot"},
					},
				},
			},
			results: []UninstallResult{
				{AssetName: "skill-1", ClientID: "github-copilot", Success: false},
			},
			expectedClients: map[string][]string{
				"skill-1||": {"claude-code", "github-copilot"},
			},
		},
		{
			name: "scoped assets match by full key",
			initialTracker: &assets.Tracker{
				Version: "3",
				Assets: []assets.InstalledAsset{
					{
						Name:       "skill-1",
						Version:    "1.0.0",
						Repository: "https://github.com/org/repo",
						Clients:    []string{"claude-code", "github-copilot"},
					},
					{
						Name:    "skill-1", // Same name, different scope (global)
						Version: "1.0.0",
						Clients: []string{"claude-code", "github-copilot"},
					},
				},
			},
			results: []UninstallResult{
				// Only remove from the repo-scoped asset
				{AssetName: "skill-1", Repository: "https://github.com/org/repo", ClientID: "github-copilot", Success: true},
			},
			expectedClients: map[string][]string{
				"skill-1|https://github.com/org/repo|": {"claude-code"},                   // repo-scoped: copilot removed
				"skill-1||":                            {"claude-code", "github-copilot"}, // global: unchanged
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save initial tracker
			if err := assets.SaveTracker(tt.initialTracker); err != nil {
				t.Fatalf("failed to save initial tracker: %v", err)
			}

			// Build removedClients map (simulating what updateTracker does internally)
			removedClients := make(map[string]map[string]bool)
			for _, result := range tt.results {
				if result.Success {
					key := result.AssetName + "|" + result.Repository + "|" + result.Path
					if removedClients[key] == nil {
						removedClients[key] = make(map[string]bool)
					}
					removedClients[key][result.ClientID] = true
				}
			}

			// Load and update tracker (simulating updateTracker logic)
			tracker, err := assets.LoadTracker()
			if err != nil {
				t.Fatalf("failed to load tracker: %v", err)
			}

			for i := range tracker.Assets {
				key := tracker.Assets[i].Name + "|" + tracker.Assets[i].Repository + "|" + tracker.Assets[i].Path
				clientsRemoved, found := removedClients[key]
				if !found {
					continue
				}

				var remainingClients []string
				for _, c := range tracker.Assets[i].Clients {
					if !clientsRemoved[c] {
						remainingClients = append(remainingClients, c)
					}
				}

				if len(remainingClients) == 0 {
					tracker.RemoveAsset(tracker.Assets[i].Key())
				} else {
					tracker.Assets[i].Clients = remainingClients
				}
			}

			if err := assets.SaveTracker(tracker); err != nil {
				t.Fatalf("failed to save updated tracker: %v", err)
			}

			// Verify results
			tracker, err = assets.LoadTracker()
			if err != nil {
				t.Fatalf("failed to reload tracker: %v", err)
			}

			// Check expected clients
			for _, a := range tracker.Assets {
				key := a.Name + "|" + a.Repository + "|" + a.Path
				expected, ok := tt.expectedClients[key]
				if !ok {
					t.Errorf("unexpected asset %q in tracker", key)
					continue
				}

				if len(a.Clients) != len(expected) {
					t.Errorf("asset %q: got %d clients %v, want %d clients %v",
						key, len(a.Clients), a.Clients, len(expected), expected)
					continue
				}

				for i, c := range a.Clients {
					if c != expected[i] {
						t.Errorf("asset %q: client[%d] = %q, want %q", key, i, c, expected[i])
					}
				}
				delete(tt.expectedClients, key)
			}

			// Check no expected assets are missing (unless they should be removed)
			for key := range tt.expectedClients {
				if !slices.Contains(tt.expectedRemoved, key) {
					t.Errorf("expected asset %q not found in tracker", key)
				}
			}
		})
	}
}
