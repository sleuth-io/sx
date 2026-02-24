package gemini

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

func TestNewClient(t *testing.T) {
	client := NewClient()

	if client.ID() != clients.ClientIDGemini {
		t.Errorf("ID() = %q, want %q", client.ID(), clients.ClientIDGemini)
	}

	if client.DisplayName() != "Gemini Code Assist" {
		t.Errorf("DisplayName() = %q, want %q", client.DisplayName(), "Gemini Code Assist")
	}
}

func TestSupportsAssetType(t *testing.T) {
	client := NewClient()

	tests := []struct {
		assetType asset.Type
		want      bool
	}{
		{asset.TypeMCP, true},
		{asset.TypeRule, true},
		{asset.TypeHook, true},    // Supported via settings.json hooks (Gemini CLI)
		{asset.TypeSkill, true},   // Supported via .gemini/commands/*.toml (Gemini CLI)
		{asset.TypeCommand, true}, // Supported via .gemini/commands/*.toml (same as skill)
		{asset.TypeAgent, false},
	}

	for _, tt := range tests {
		t.Run(tt.assetType.Key, func(t *testing.T) {
			got := client.SupportsAssetType(tt.assetType)
			if got != tt.want {
				t.Errorf("SupportsAssetType(%v) = %v, want %v", tt.assetType, got, tt.want)
			}
		})
	}
}

func TestIsInstalled(t *testing.T) {
	// Save current home
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	// Create temp home
	tempDir := t.TempDir()
	os.Setenv("HOME", tempDir)

	client := NewClient()

	// Initially not installed
	if client.IsInstalled() {
		t.Error("IsInstalled() = true when ~/.gemini doesn't exist")
	}

	// Create .gemini directory
	geminiDir := filepath.Join(tempDir, ".gemini")
	if err := os.MkdirAll(geminiDir, 0755); err != nil {
		t.Fatalf("Failed to create .gemini dir: %v", err)
	}

	// Now should be installed
	if !client.IsInstalled() {
		t.Error("IsInstalled() = false when ~/.gemini exists")
	}
}

func TestDetermineTargetBase(t *testing.T) {
	// Save current home
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tempDir := t.TempDir()
	os.Setenv("HOME", tempDir)

	client := NewClient()

	tests := []struct {
		name    string
		scope   *clients.InstallScope
		want    string
		wantErr bool
	}{
		{
			name:  "global scope",
			scope: &clients.InstallScope{Type: clients.ScopeGlobal},
			want:  filepath.Join(tempDir, ".gemini"),
		},
		{
			name: "repo scope",
			scope: &clients.InstallScope{
				Type:     clients.ScopeRepository,
				RepoRoot: "/path/to/repo",
			},
			want: "/path/to/repo",
		},
		{
			name: "path scope",
			scope: &clients.InstallScope{
				Type:     clients.ScopePath,
				RepoRoot: "/path/to/repo",
				Path:     "subdir",
			},
			want: "/path/to/repo/subdir",
		},
		{
			name: "repo scope without RepoRoot",
			scope: &clients.InstallScope{
				Type: clients.ScopeRepository,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := client.determineTargetBase(tt.scope)
			if tt.wantErr {
				if err == nil {
					t.Errorf("determineTargetBase() error = nil, wantErr = true")
				}
				return
			}
			if err != nil {
				t.Errorf("determineTargetBase() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("determineTargetBase() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRuleCapabilities(t *testing.T) {
	client := NewClient()
	caps := client.RuleCapabilities()

	if caps == nil {
		t.Fatal("RuleCapabilities() returned nil")
		return // Make staticcheck happy
	}

	if caps.ClientName != "gemini" {
		t.Errorf("ClientName = %q, want %q", caps.ClientName, "gemini")
	}

	if caps.FileExtension != ".md" {
		t.Errorf("FileExtension = %q, want %q", caps.FileExtension, ".md")
	}

	// Check instruction files
	expectedFiles := []string{"GEMINI.md", "AGENT.md", "AGENTS.md"}
	for _, expected := range expectedFiles {
		if !slices.Contains(caps.InstructionFiles, expected) {
			t.Errorf("InstructionFiles missing %q", expected)
		}
	}
}

func TestRuleCapabilitiesMatchesPath(t *testing.T) {
	caps := RuleCapabilities()

	tests := []struct {
		path string
		want bool
	}{
		{"GEMINI.md", true},
		{"gemini.md", true},
		{"AGENT.md", true},
		{"agent.md", true},
		{"AGENTS.md", true},
		{"agents.md", true},
		{"/some/path/GEMINI.md", true},
		{"/some/path/AGENT.md", true},
		{"my-agent.md", false}, // Should NOT match - not an exact filename
		{"agent.txt", false},
		{"README.md", false},
		{"rules.md", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := caps.MatchesPath(tt.path); got != tt.want {
				t.Errorf("MatchesPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestInstallAndUninstallRule(t *testing.T) {
	tempDir := t.TempDir()

	client := NewClient()

	// Create test rule content
	ruleContent := "This is a test rule for code standards."

	// Create a zip with the rule content
	zipData, err := utils.CreateZipFromContent("RULE.md", []byte(ruleContent))
	if err != nil {
		t.Fatalf("Failed to create zip: %v", err)
	}

	// Add metadata to the zip
	metadataContent := `[asset]
name = "test-rule"
version = "1.0.0"
type = "rule"
`
	zipData, err = utils.AddFileToZip(zipData, "metadata.toml", []byte(metadataContent))
	if err != nil {
		t.Fatalf("Failed to add metadata to zip: %v", err)
	}

	ctx := context.Background()
	scope := &clients.InstallScope{
		Type:     clients.ScopeRepository,
		RepoRoot: tempDir,
	}

	// Install the rule
	req := clients.InstallRequest{
		Assets: []*clients.AssetBundle{
			{
				Asset: &lockfile.Asset{
					Name:    "test-rule",
					Version: "1.0.0",
					Type:    asset.TypeRule,
				},
				Metadata: &metadata.Metadata{
					Asset: metadata.Asset{
						Name:    "test-rule",
						Version: "1.0.0",
						Type:    asset.TypeRule,
					},
				},
				ZipData: zipData,
			},
		},
		Scope: scope,
	}

	resp, err := client.InstallAssets(ctx, req)
	if err != nil {
		t.Fatalf("InstallAssets() error = %v", err)
	}

	if len(resp.Results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(resp.Results))
	}

	if resp.Results[0].Status != clients.StatusSuccess {
		t.Errorf("Install status = %v, want %v", resp.Results[0].Status, clients.StatusSuccess)
	}

	// Verify GEMINI.md was created
	geminiPath := filepath.Join(tempDir, "GEMINI.md")
	content, err := os.ReadFile(geminiPath)
	if err != nil {
		t.Fatalf("Failed to read GEMINI.md: %v", err)
	}

	if len(content) == 0 {
		t.Error("GEMINI.md is empty")
	}

	// Verify the rule is installed
	verifyResults := client.VerifyAssets(ctx, []*lockfile.Asset{req.Assets[0].Asset}, scope)
	if len(verifyResults) != 1 {
		t.Fatalf("Expected 1 verify result, got %d", len(verifyResults))
	}

	if !verifyResults[0].Installed {
		t.Errorf("VerifyAssets() returned Installed = false, want true")
	}

	// Uninstall the rule
	uninstallReq := clients.UninstallRequest{
		Assets: []asset.Asset{
			{
				Name: "test-rule",
				Type: asset.TypeRule,
			},
		},
		Scope: scope,
	}

	uninstallResp, err := client.UninstallAssets(ctx, uninstallReq)
	if err != nil {
		t.Fatalf("UninstallAssets() error = %v", err)
	}

	if len(uninstallResp.Results) != 1 {
		t.Fatalf("Expected 1 uninstall result, got %d", len(uninstallResp.Results))
	}

	if uninstallResp.Results[0].Status != clients.StatusSuccess {
		t.Errorf("Uninstall status = %v, want %v", uninstallResp.Results[0].Status, clients.StatusSuccess)
	}
}
