package clients

import (
	"context"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/bootstrap"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/metadata"
)

// mockClient implements the Client interface for testing
type mockClient struct {
	BaseClient
	ruleCaps *RuleCapabilities
}

func (m *mockClient) RuleCapabilities() *RuleCapabilities {
	return m.ruleCaps
}

// Stub implementations of Client interface
func (m *mockClient) IsInstalled() bool  { return true }
func (m *mockClient) GetVersion() string { return "1.0" }
func (m *mockClient) InstallAssets(context.Context, InstallRequest) (InstallResponse, error) {
	return InstallResponse{}, nil
}
func (m *mockClient) UninstallAssets(context.Context, UninstallRequest) (UninstallResponse, error) {
	return UninstallResponse{}, nil
}
func (m *mockClient) ListAssets(context.Context, *InstallScope) ([]InstalledSkill, error) {
	return nil, nil
}
func (m *mockClient) ReadSkill(context.Context, string, *InstallScope) (*SkillContent, error) {
	return &SkillContent{}, nil
}
func (m *mockClient) EnsureAssetSupport(context.Context, *InstallScope) error { return nil }
func (m *mockClient) GetBootstrapOptions(context.Context) []bootstrap.Option  { return nil }
func (m *mockClient) InstallBootstrap(context.Context, []bootstrap.Option) error {
	return nil
}
func (m *mockClient) UninstallBootstrap(context.Context, []bootstrap.Option) error { return nil }
func (m *mockClient) ShouldInstall(context.Context) (bool, error)                  { return true, nil }
func (m *mockClient) VerifyAssets(context.Context, []*lockfile.Asset, *InstallScope) []VerifyResult {
	return nil
}
func (m *mockClient) ScanInstalledAssets(context.Context, *InstallScope) ([]InstalledAsset, error) {
	return nil, nil
}
func (m *mockClient) GetAssetPath(context.Context, string, asset.Type, *InstallScope) (string, error) {
	return "", nil
}

func newMockClient(id string, caps *RuleCapabilities) *mockClient {
	return &mockClient{
		BaseClient: NewBaseClient(id, id, nil),
		ruleCaps:   caps,
	}
}

func TestDetectClientFromPath(t *testing.T) {
	reg := NewRegistry()

	// Register a mock Claude Code client
	claudeCaps := &RuleCapabilities{
		ClientName:     "claude-code",
		RulesDirectory: ".claude/rules",
		FileExtension:  ".md",
		MatchesPath: func(path string) bool {
			return path == ".claude/rules/test.md"
		},
	}
	reg.Register(newMockClient("claude-code", claudeCaps))

	// Register a mock Cursor client
	cursorCaps := &RuleCapabilities{
		ClientName:     "cursor",
		RulesDirectory: ".cursor/rules",
		FileExtension:  ".mdc",
		MatchesPath: func(path string) bool {
			return path == ".cursor/rules/test.mdc"
		},
	}
	reg.Register(newMockClient("cursor", cursorCaps))

	tests := []struct {
		name       string
		path       string
		wantClient string
	}{
		{
			name:       "detects claude code rule",
			path:       ".claude/rules/test.md",
			wantClient: "claude-code",
		},
		{
			name:       "detects cursor rule",
			path:       ".cursor/rules/test.mdc",
			wantClient: "cursor",
		},
		{
			name:       "no match for unknown path",
			path:       "unknown/path.txt",
			wantClient: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, caps := reg.DetectClientFromPath(tt.path)
			if tt.wantClient == "" {
				if client != nil {
					t.Errorf("expected no client, got %s", client.ID())
				}
			} else {
				if client == nil {
					t.Errorf("expected client %s, got nil", tt.wantClient)
				} else if client.ID() != tt.wantClient {
					t.Errorf("expected client %s, got %s", tt.wantClient, client.ID())
				}
				if caps == nil {
					t.Errorf("expected capabilities, got nil")
				}
			}
		})
	}
}

func TestIsInstructionFile(t *testing.T) {
	reg := NewRegistry()

	// Register a mock client with instruction files
	caps := &RuleCapabilities{
		ClientName:       "claude-code",
		InstructionFiles: []string{"CLAUDE.md", "AGENTS.md"},
	}
	reg.Register(newMockClient("claude-code", caps))

	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "CLAUDE.md is instruction file",
			path: "CLAUDE.md",
			want: true,
		},
		{
			name: "AGENTS.md is instruction file",
			path: "AGENTS.md",
			want: true,
		},
		{
			name: "nested CLAUDE.md is instruction file",
			path: "subdir/CLAUDE.md",
			want: true,
		},
		{
			name: "case insensitive match",
			path: "claude.md",
			want: true,
		},
		{
			name: "README.md is not instruction file",
			path: "README.md",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reg.IsInstructionFile(tt.path)
			if got != tt.want {
				t.Errorf("IsInstructionFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestParseRuleFile(t *testing.T) {
	reg := NewRegistry()

	// Register a mock client with a parser
	caps := &RuleCapabilities{
		ClientName: "test-client",
		MatchesPath: func(path string) bool {
			return path == "test.md"
		},
		ParseRuleFile: func(content []byte) (*ParsedRule, error) {
			return &ParsedRule{
				ClientName: "test-client",
				Globs:      []string{"**/*.go"},
				Content:    "parsed content",
			}, nil
		},
	}
	reg.Register(newMockClient("test-client", caps))

	t.Run("parses with matching client", func(t *testing.T) {
		result, err := reg.ParseRuleFile("test.md", []byte("raw content"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.ClientName != "test-client" {
			t.Errorf("ClientName = %q, want %q", result.ClientName, "test-client")
		}
		if result.Content != "parsed content" {
			t.Errorf("Content = %q, want %q", result.Content, "parsed content")
		}
	})

	t.Run("returns raw content for unknown path", func(t *testing.T) {
		result, err := reg.ParseRuleFile("unknown.md", []byte("raw content"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Content != "raw content" {
			t.Errorf("Content = %q, want %q", result.Content, "raw content")
		}
	})
}

func TestGetRulesDirectories(t *testing.T) {
	reg := NewRegistry()

	reg.Register(newMockClient("claude-code", &RuleCapabilities{
		RulesDirectory: ".claude/rules",
	}))
	reg.Register(newMockClient("cursor", &RuleCapabilities{
		RulesDirectory: ".cursor/rules",
	}))
	reg.Register(newMockClient("no-rules", nil))

	dirs := reg.GetRulesDirectories()

	if len(dirs) != 2 {
		t.Errorf("expected 2 directories, got %d", len(dirs))
	}

	// Check both directories are present (order may vary)
	found := make(map[string]bool)
	for _, d := range dirs {
		found[d] = true
	}
	if !found[".claude/rules"] {
		t.Error("missing .claude/rules")
	}
	if !found[".cursor/rules"] {
		t.Error("missing .cursor/rules")
	}
}

func TestBaseClientRuleCapabilities(t *testing.T) {
	base := NewBaseClient("test", "Test", nil)
	if base.RuleCapabilities() != nil {
		t.Error("BaseClient.RuleCapabilities() should return nil")
	}
}

func TestRuleCapabilitiesGenerateRuleFile(t *testing.T) {
	// Test that the type signature matches
	caps := &RuleCapabilities{
		GenerateRuleFile: func(cfg *metadata.RuleConfig, body string) []byte {
			return []byte(body)
		},
	}

	result := caps.GenerateRuleFile(&metadata.RuleConfig{}, "test")
	if string(result) != "test" {
		t.Errorf("expected 'test', got %q", string(result))
	}
}
