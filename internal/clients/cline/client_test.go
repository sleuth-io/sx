package cline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/clients"
)

func TestNewClient(t *testing.T) {
	client := NewClient()

	if client.ID() != clients.ClientIDCline {
		t.Errorf("Expected ID %s, got %s", clients.ClientIDCline, client.ID())
	}

	if client.DisplayName() != "Cline" {
		t.Errorf("Expected DisplayName 'Cline', got %s", client.DisplayName())
	}
}

func TestClient_SupportedAssetTypes(t *testing.T) {
	client := NewClient()

	// Should support skill, rule, and mcp
	supportedTypes := []asset.Type{
		asset.TypeSkill,
		asset.TypeRule,
		asset.TypeMCP,
	}

	for _, at := range supportedTypes {
		if !client.SupportsAssetType(at) {
			t.Errorf("Expected client to support %s", at.Key)
		}
	}

	// Should NOT support hook (Cline has no hook system)
	if client.SupportsAssetType(asset.TypeHook) {
		t.Error("Cline should not support hooks")
	}
}

func TestClient_IsInstalled_GlobalClineDir(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	client := NewClient()

	// Initially not installed
	if client.IsInstalled() {
		t.Error("Should not be installed without .cline directory")
	}

	// Create global .cline directory
	clineDir := filepath.Join(tmpDir, ".cline")
	if err := os.MkdirAll(clineDir, 0755); err != nil {
		t.Fatalf("Failed to create .cline dir: %v", err)
	}

	// Now should be installed
	if !client.IsInstalled() {
		t.Error("Should be installed with .cline directory")
	}
}

func TestClient_IsInstalled_LocalClineDir(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	workDir := filepath.Join(tmpDir, "work")

	t.Setenv("HOME", homeDir)

	// Create work directory without .cline
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("Failed to create work dir: %v", err)
	}

	// Change to work directory
	originalDir, _ := os.Getwd()
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("Failed to chdir: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalDir)
	}()

	client := NewClient()

	// Initially not installed
	if client.IsInstalled() {
		t.Error("Should not be installed without any .cline directory")
	}

	// Create local .cline directory
	localClineDir := filepath.Join(workDir, ".cline")
	if err := os.MkdirAll(localClineDir, 0755); err != nil {
		t.Fatalf("Failed to create local .cline dir: %v", err)
	}

	// Now should be installed
	if !client.IsInstalled() {
		t.Error("Should be installed with local .cline directory")
	}
}

func TestClient_GetVersion(t *testing.T) {
	client := NewClient()

	// GetVersion returns the Cline CLI version if installed, empty otherwise
	// We can't assert the exact value since it depends on whether cline CLI is installed
	version := client.GetVersion()
	// Just verify it doesn't panic - version may be empty if CLI not installed
	_ = version
}

func TestClient_DetermineTargetBase(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	t.Setenv("HOME", homeDir)

	client := NewClient()

	tests := []struct {
		name     string
		scope    *clients.InstallScope
		expected string
		wantErr  bool
	}{
		{
			name:     "global scope",
			scope:    &clients.InstallScope{Type: clients.ScopeGlobal},
			expected: filepath.Join(homeDir, ".cline"),
			wantErr:  false,
		},
		{
			name: "repo scope",
			scope: &clients.InstallScope{
				Type:     clients.ScopeRepository,
				RepoRoot: "/path/to/repo",
			},
			expected: "/path/to/repo/.cline",
			wantErr:  false,
		},
		{
			name: "path scope",
			scope: &clients.InstallScope{
				Type:     clients.ScopePath,
				RepoRoot: "/path/to/repo",
				Path:     "services/api",
			},
			expected: "/path/to/repo/services/api/.cline",
			wantErr:  false,
		},
		{
			name: "repo scope without root",
			scope: &clients.InstallScope{
				Type: clients.ScopeRepository,
			},
			wantErr: true,
		},
		{
			name: "path scope without root",
			scope: &clients.InstallScope{
				Type: clients.ScopePath,
				Path: "services/api",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := client.determineTargetBase(tt.scope)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if got != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, got)
			}
		})
	}
}

func TestClient_RuleCapabilities(t *testing.T) {
	client := NewClient()
	caps := client.RuleCapabilities()

	if caps == nil {
		t.Fatal("RuleCapabilities should not be nil")
	}

	if caps.ClientName != "cline" {
		t.Errorf("Expected ClientName 'cline', got %s", caps.ClientName)
	}

	if caps.RulesDirectory != ".clinerules" {
		t.Errorf("Expected RulesDirectory '.clinerules', got %s", caps.RulesDirectory)
	}
}

func TestClient_GetBootstrapOptions(t *testing.T) {
	client := NewClient()
	opts := client.GetBootstrapOptions(nil)

	// Cline doesn't have hooks, so should only have MCP options
	if len(opts) == 0 {
		t.Error("Expected at least one bootstrap option")
	}

	// Should have the Sleuth AI Query MCP option
	hasMCPOption := false
	for _, opt := range opts {
		if opt.MCPConfig != nil {
			hasMCPOption = true
			break
		}
	}

	if !hasMCPOption {
		t.Error("Expected at least one MCP bootstrap option")
	}

	// Should NOT have session or analytics hooks
	for _, opt := range opts {
		if opt.Key == "session-hook" || opt.Key == "analytics-hook" {
			t.Errorf("Cline should not have hook options, found: %s", opt.Key)
		}
	}
}

func TestClient_ShouldInstall(t *testing.T) {
	client := NewClient()

	// Cline doesn't have hooks, so ShouldInstall always returns true
	should, err := client.ShouldInstall(nil)
	if err != nil {
		t.Errorf("ShouldInstall returned error: %v", err)
	}
	if !should {
		t.Error("ShouldInstall should always return true for Cline")
	}
}

func TestClient_GetBootstrapPath(t *testing.T) {
	client := NewClient()
	path := client.GetBootstrapPath()

	// Should return the MCP config path
	if path == "" {
		t.Error("GetBootstrapPath should return non-empty path")
	}

	if !filepath.IsAbs(path) {
		t.Errorf("Expected absolute path, got %s", path)
	}

	if !contains(path, "cline_mcp_settings.json") {
		t.Errorf("Expected path to end with cline_mcp_settings.json, got %s", path)
	}
}

// Helper function
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
