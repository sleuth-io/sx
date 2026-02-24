package commands

import (
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
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
