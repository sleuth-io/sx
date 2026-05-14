package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/assets"
	"github.com/sleuth-io/sx/internal/bootstrap"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/gitutil"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/ui"
)

// stubClient is a minimal clients.Client implementation for testing
type stubClient struct {
	id string
}

func (s *stubClient) ID() string          { return s.id }
func (s *stubClient) DisplayName() string { return s.id }
func (s *stubClient) IsInstalled() bool   { return true }
func (s *stubClient) GetVersion() string  { return "" }
func (s *stubClient) SupportsAssetType(asset.Type) bool {
	return true
}
func (s *stubClient) InstallAssets(context.Context, clients.InstallRequest) (clients.InstallResponse, error) {
	return clients.InstallResponse{}, nil
}
func (s *stubClient) UninstallAssets(context.Context, clients.UninstallRequest) (clients.UninstallResponse, error) {
	return clients.UninstallResponse{}, nil
}
func (s *stubClient) ListAssets(context.Context, *clients.InstallScope) ([]clients.InstalledSkill, error) {
	return nil, nil
}
func (s *stubClient) ReadSkill(context.Context, string, *clients.InstallScope) (*clients.SkillContent, error) {
	return &clients.SkillContent{}, nil
}
func (s *stubClient) EnsureAssetSupport(context.Context, *clients.InstallScope) error { return nil }
func (s *stubClient) GetBootstrapOptions(context.Context) []bootstrap.Option          { return nil }
func (s *stubClient) GetBootstrapPath() string                                        { return "" }
func (s *stubClient) InstallBootstrap(context.Context, []bootstrap.Option) error      { return nil }
func (s *stubClient) UninstallBootstrap(context.Context, []bootstrap.Option) error    { return nil }
func (s *stubClient) ShouldInstall(context.Context) (bool, error)                     { return true, nil }
func (s *stubClient) VerifyAssets(context.Context, []*lockfile.Asset, *clients.InstallScope) []clients.VerifyResult {
	return nil
}
func (s *stubClient) ScanInstalledAssets(context.Context, *clients.InstallScope) ([]clients.InstalledAsset, error) {
	return nil, nil
}
func (s *stubClient) GetAssetPath(context.Context, string, asset.Type, *clients.InstallScope) (string, error) {
	return "", nil
}
func (s *stubClient) RuleCapabilities() *clients.RuleCapabilities { return nil }

// recordingClient is a stubClient that captures the bootstrap options it
// receives via UninstallBootstrap so tests can assert what was passed in.
type recordingClient struct {
	stubClient
	options       []bootstrap.Option // returned from GetBootstrapOptions
	uninstallSeen []bootstrap.Option // captured by UninstallBootstrap
}

func (r *recordingClient) GetBootstrapOptions(context.Context) []bootstrap.Option {
	return r.options
}

func (r *recordingClient) UninstallBootstrap(_ context.Context, opts []bootstrap.Option) error {
	r.uninstallSeen = append([]bootstrap.Option(nil), opts...)
	return nil
}

// TestUninstallHooksFromClients_PassesEveryOption pins the contract that
// uninstallSystemHooks (via this helper) hands UninstallBootstrap every
// option the client returns from GetBootstrapOptions, regardless of which
// options are enabled in the user's MultiProfileConfig. If a future change
// re-introduces an enabled-filter, this test fails.
func TestUninstallHooksFromClients_PassesEveryOption(t *testing.T) {
	rec := &recordingClient{
		stubClient: stubClient{id: "stub"},
		options: []bootstrap.Option{
			{Key: bootstrap.SessionHookKey, Description: "session"},
			{Key: bootstrap.AnalyticsHookKey, Description: "analytics"},
			{Key: "custom-disabled-in-config", Description: "user disabled this in config"},
		},
	}

	out := ui.NewOutput(&bytes.Buffer{}, &bytes.Buffer{})
	uninstallHooksFromClients(context.Background(), []clients.Client{rec}, nil, out)

	if len(rec.uninstallSeen) != len(rec.options) {
		t.Fatalf("expected %d options passed to UninstallBootstrap, got %d (%+v)",
			len(rec.options), len(rec.uninstallSeen), rec.uninstallSeen)
	}
	seen := make(map[string]bool, len(rec.uninstallSeen))
	for _, o := range rec.uninstallSeen {
		seen[o.Key] = true
	}
	for _, want := range rec.options {
		if !seen[want.Key] {
			t.Errorf("option %q was not passed to UninstallBootstrap — would be left as an orphan hook", want.Key)
		}
	}
}

// keys returns sorted keys from a map for readable error messages
func keys(m map[string]bool) []string {
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	slices.Sort(result)
	return result
}

func TestFilterUninstallPlanByScope(t *testing.T) {
	// Shared tracker data: 2 global, 2 repo-A (one path-scoped), 1 repo-B
	allAssets := []AssetUninstallPlan{
		{Name: "global-skill-1", Type: asset.TypeSkill, IsGlobal: true, Clients: []string{"claude-code"}},
		{Name: "global-skill-2", Type: asset.TypeSkill, IsGlobal: true, Clients: []string{"claude-code", "cursor"}},
		{Name: "repo-a-skill", Type: asset.TypeSkill, IsGlobal: false, Repository: "https://github.com/org/repo-a", Clients: []string{"claude-code"}},
		{Name: "repo-a-path-skill", Type: asset.TypeSkill, IsGlobal: false, Repository: "https://github.com/org/repo-a", Path: "services/api", Clients: []string{"claude-code", "cursor"}},
		{Name: "repo-b-skill", Type: asset.TypeSkill, IsGlobal: false, Repository: "https://github.com/org/repo-b", Clients: []string{"cursor"}},
	}

	tests := []struct {
		name       string
		gitContext *gitutil.GitContext
		all        bool
		wantNames  []string
	}{
		{
			name:       "inside repo without --all: only that repo's assets",
			gitContext: &gitutil.GitContext{IsRepo: true, RepoURL: "https://github.com/org/repo-a", RepoRoot: "/work/repo-a", RelativePath: "."},
			all:        false,
			wantNames:  []string{"repo-a-skill", "repo-a-path-skill"},
		},
		{
			name:       "inside repo with --all: all assets",
			gitContext: &gitutil.GitContext{IsRepo: true, RepoURL: "https://github.com/org/repo-a", RepoRoot: "/work/repo-a", RelativePath: "."},
			all:        true,
			wantNames:  []string{"global-skill-1", "global-skill-2", "repo-a-skill", "repo-a-path-skill", "repo-b-skill"},
		},
		{
			name:       "outside repo without --all: no-op",
			gitContext: &gitutil.GitContext{IsRepo: false},
			all:        false,
			wantNames:  []string{},
		},
		{
			name:       "outside repo with --all: all assets",
			gitContext: &gitutil.GitContext{IsRepo: false},
			all:        true,
			wantNames:  []string{"global-skill-1", "global-skill-2", "repo-a-skill", "repo-a-path-skill", "repo-b-skill"},
		},
		{
			name:       "inside repo-b without --all: only repo-b assets",
			gitContext: &gitutil.GitContext{IsRepo: true, RepoURL: "https://github.com/org/repo-b", RepoRoot: "/work/repo-b", RelativePath: "."},
			all:        false,
			wantNames:  []string{"repo-b-skill"},
		},
		{
			name:       "inside unknown repo without --all: no matching assets",
			gitContext: &gitutil.GitContext{IsRepo: true, RepoURL: "https://github.com/org/repo-c", RepoRoot: "/work/repo-c", RelativePath: "."},
			all:        false,
			wantNames:  []string{},
		},
		{
			name:       "inside repo-a subdirectory without --all: still matches repo-a assets",
			gitContext: &gitutil.GitContext{IsRepo: true, RepoURL: "https://github.com/org/repo-a", RepoRoot: "/work/repo-a", RelativePath: "services/api"},
			all:        false,
			wantNames:  []string{"repo-a-skill", "repo-a-path-skill"},
		},
		{
			name:       "inside unknown repo with --all: all assets",
			gitContext: &gitutil.GitContext{IsRepo: true, RepoURL: "https://github.com/org/repo-c", RepoRoot: "/work/repo-c", RelativePath: "."},
			all:        true,
			wantNames:  []string{"global-skill-1", "global-skill-2", "repo-a-skill", "repo-a-path-skill", "repo-b-skill"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := UninstallPlan{
				Assets:     append([]AssetUninstallPlan{}, allAssets...),
				GitContext: tt.gitContext,
			}

			result := filterUninstallPlanByScope(plan, tt.all)

			gotNames := make(map[string]bool)
			for _, a := range result.Assets {
				gotNames[a.Name] = true
			}

			wantNames := make(map[string]bool)
			for _, name := range tt.wantNames {
				wantNames[name] = true
			}

			if len(gotNames) != len(wantNames) {
				t.Errorf("got %d assets %v, want %d assets %v", len(gotNames), keys(gotNames), len(wantNames), keys(wantNames))
				return
			}

			for name := range wantNames {
				if !gotNames[name] {
					t.Errorf("missing expected asset %q", name)
				}
			}
		})
	}
}

func TestFilterUninstallPlanByScopeThenClients(t *testing.T) {
	allAssets := []AssetUninstallPlan{
		{Name: "global-skill", Type: asset.TypeSkill, IsGlobal: true, Clients: []string{"claude-code", "cursor"}},
		{Name: "repo-skill", Type: asset.TypeSkill, IsGlobal: false, Repository: "https://github.com/org/repo-a", Clients: []string{"claude-code", "cursor"}},
	}

	tests := []struct {
		name        string
		gitContext  *gitutil.GitContext
		all         bool
		clientsFlag string
		wantNames   []string
		wantClients map[string][]string
	}{
		{
			name:        "inside repo, no --all, --clients cursor: only repo assets for cursor",
			gitContext:  &gitutil.GitContext{IsRepo: true, RepoURL: "https://github.com/org/repo-a", RepoRoot: "/work/repo-a", RelativePath: "."},
			all:         false,
			clientsFlag: "cursor",
			wantNames:   []string{"repo-skill"},
			wantClients: map[string][]string{"repo-skill": {"cursor"}},
		},
		{
			name:        "inside repo, --all, --clients cursor: all assets for cursor",
			gitContext:  &gitutil.GitContext{IsRepo: true, RepoURL: "https://github.com/org/repo-a", RepoRoot: "/work/repo-a", RelativePath: "."},
			all:         true,
			clientsFlag: "cursor",
			wantNames:   []string{"global-skill", "repo-skill"},
			wantClients: map[string][]string{"global-skill": {"cursor"}, "repo-skill": {"cursor"}},
		},
		{
			name:        "outside repo, no --all, --clients cursor: no-op",
			gitContext:  &gitutil.GitContext{IsRepo: false},
			all:         false,
			clientsFlag: "cursor",
			wantNames:   []string{},
			wantClients: map[string][]string{},
		},
		{
			name:        "outside repo, --all, --clients cursor: all assets for cursor",
			gitContext:  &gitutil.GitContext{IsRepo: false},
			all:         true,
			clientsFlag: "cursor",
			wantNames:   []string{"global-skill", "repo-skill"},
			wantClients: map[string][]string{"global-skill": {"cursor"}, "repo-skill": {"cursor"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := UninstallPlan{
				Assets:     append([]AssetUninstallPlan{}, allAssets...),
				GitContext: tt.gitContext,
			}

			// Apply scope filter first, then clients filter (same order as runUninstall)
			result := filterUninstallPlanByScope(plan, tt.all)
			if tt.clientsFlag != "" {
				result = filterUninstallPlanByClients(result, tt.clientsFlag)
			}

			gotNames := make(map[string]bool)
			for _, a := range result.Assets {
				gotNames[a.Name] = true
			}

			wantNames := make(map[string]bool)
			for _, name := range tt.wantNames {
				wantNames[name] = true
			}

			if len(gotNames) != len(wantNames) {
				t.Errorf("got %d assets %v, want %d assets %v", len(gotNames), keys(gotNames), len(wantNames), keys(wantNames))
				return
			}

			for name := range wantNames {
				if !gotNames[name] {
					t.Errorf("missing expected asset %q", name)
				}
			}

			for _, a := range result.Assets {
				expected, ok := tt.wantClients[a.Name]
				if !ok {
					t.Errorf("unexpected asset %q", a.Name)
					continue
				}
				if len(a.Clients) != len(expected) {
					t.Errorf("asset %q: got clients %v, want %v", a.Name, a.Clients, expected)
				}
			}
		})
	}
}

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

func TestFilterClientsByFlag(t *testing.T) {
	allClients := []clients.Client{
		&stubClient{id: "claude-code"},
		&stubClient{id: "cursor"},
		&stubClient{id: "gemini"},
	}

	tests := []struct {
		name    string
		flag    string
		wantIDs []string
	}{
		{
			name:    "single client",
			flag:    "cursor",
			wantIDs: []string{"cursor"},
		},
		{
			name:    "multiple clients",
			flag:    "claude-code,gemini",
			wantIDs: []string{"claude-code", "gemini"},
		},
		{
			name:    "with spaces",
			flag:    "claude-code , cursor",
			wantIDs: []string{"claude-code", "cursor"},
		},
		{
			name:    "no match",
			flag:    "unknown",
			wantIDs: []string{},
		},
		{
			name:    "all clients",
			flag:    "claude-code,cursor,gemini",
			wantIDs: []string{"claude-code", "cursor", "gemini"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterClientsByFlag(allClients, tt.flag)

			gotIDs := make(map[string]bool)
			for _, c := range result {
				gotIDs[c.ID()] = true
			}

			wantIDs := make(map[string]bool)
			for _, id := range tt.wantIDs {
				wantIDs[id] = true
			}

			if len(gotIDs) != len(wantIDs) {
				t.Errorf("got %d clients %v, want %d clients %v", len(gotIDs), keys(gotIDs), len(wantIDs), keys(wantIDs))
				return
			}

			for id := range wantIDs {
				if !gotIDs[id] {
					t.Errorf("missing expected client %q", id)
				}
			}
		})
	}
}
