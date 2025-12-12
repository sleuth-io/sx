package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sleuth-io/skills/internal/clients"
)

// mockClient implements clients.Client for testing
type mockClient struct {
	clients.BaseClient
	skills map[string]*clients.SkillContent
}

func newMockClient() *mockClient {
	return &mockClient{
		BaseClient: clients.NewBaseClient("mock", "Mock Client", nil),
		skills:     make(map[string]*clients.SkillContent),
	}
}

func (m *mockClient) IsInstalled() bool  { return true }
func (m *mockClient) GetVersion() string { return "1.0.0" }

func (m *mockClient) InstallArtifacts(ctx context.Context, req clients.InstallRequest) (clients.InstallResponse, error) {
	return clients.InstallResponse{}, nil
}

func (m *mockClient) UninstallArtifacts(ctx context.Context, req clients.UninstallRequest) (clients.UninstallResponse, error) {
	return clients.UninstallResponse{}, nil
}

func (m *mockClient) ListSkills(ctx context.Context, scope *clients.InstallScope) ([]clients.InstalledSkill, error) {
	skills := make([]clients.InstalledSkill, 0, len(m.skills))
	for _, s := range m.skills {
		skills = append(skills, clients.InstalledSkill{
			Name:        s.Name,
			Description: s.Description,
		})
	}
	return skills, nil
}

func (m *mockClient) ReadSkill(ctx context.Context, name string, scope *clients.InstallScope) (*clients.SkillContent, error) {
	if skill, ok := m.skills[name]; ok {
		return skill, nil
	}
	return nil, os.ErrNotExist
}

func (m *mockClient) EnsureSkillsSupport(ctx context.Context, scope *clients.InstallScope) error {
	return nil
}

func (m *mockClient) InstallHooks(ctx context.Context) error {
	return nil
}

func (m *mockClient) UninstallHooks(ctx context.Context) error {
	return nil
}

func (m *mockClient) ShouldInstall(ctx context.Context) (bool, error) {
	return true, nil
}

func (m *mockClient) addSkill(name, description, version, content, baseDir string) {
	m.skills[name] = &clients.SkillContent{
		Name:        name,
		Description: description,
		Version:     version,
		Content:     content,
		BaseDir:     baseDir,
	}
}

func TestServer_ReadSkill(t *testing.T) {
	// Create a mock client with test skills
	mock := newMockClient()
	mock.addSkill("test-skill", "A test skill", "1.0.0", "# Test Skill\n\nThis is the skill content.", "/tmp/skills/test-skill")

	// Create a registry with just our mock client
	registry := clients.NewRegistry()
	registry.Register(mock)

	// Create the MCP server
	server := NewServer(registry)

	// Create the internal MCP server for testing
	impl := &mcp.Implementation{
		Name:    "skills",
		Version: "1.0.0",
	}
	mcpServer := mcp.NewServer(impl, nil)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "read_skill",
		Description: "Read a skill's full instructions and content.",
	}, server.handleReadSkill)

	// Connect using in-memory transport
	ctx := context.Background()
	t1, t2 := mcp.NewInMemoryTransports()

	_, err := mcpServer.Connect(ctx, t1, nil)
	if err != nil {
		t.Fatalf("Failed to connect server: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v1.0.0"}, nil)
	session, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("Failed to connect client: %v", err)
	}
	defer session.Close()

	// Test reading an existing skill
	t.Run("read existing skill", func(t *testing.T) {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "read_skill",
			Arguments: map[string]any{"name": "test-skill"},
		})
		if err != nil {
			t.Fatalf("CallTool failed: %v", err)
		}

		if result.IsError {
			t.Fatalf("Tool returned error: %v", result.Content)
		}

		// Check the text content contains skill markdown
		if len(result.Content) == 0 {
			t.Fatal("Expected content")
		}
		textContent, ok := result.Content[0].(*mcp.TextContent)
		if !ok {
			t.Fatalf("Expected TextContent, got %T", result.Content[0])
		}
		if textContent.Text == "" {
			t.Error("Expected non-empty text content")
		}
		// Should contain the skill content
		if textContent.Text != "# Test Skill\n\nThis is the skill content." {
			t.Errorf("Expected skill content, got: %s", textContent.Text)
		}
	})

	// Test reading a non-existent skill
	t.Run("read non-existent skill", func(t *testing.T) {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "read_skill",
			Arguments: map[string]any{"name": "non-existent"},
		})
		if err != nil {
			t.Fatalf("CallTool failed: %v", err)
		}

		// Should return an error in the result
		if !result.IsError {
			t.Error("Expected IsError to be true for non-existent skill")
		}
	})

	// Test reading with empty name
	t.Run("read with empty name", func(t *testing.T) {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "read_skill",
			Arguments: map[string]any{"name": ""},
		})
		if err != nil {
			t.Fatalf("CallTool failed: %v", err)
		}

		// Should return an error in the result
		if !result.IsError {
			t.Error("Expected IsError to be true for empty name")
		}
	})
}

func TestServer_Integration(t *testing.T) {
	// Create a temp directory with an actual skill
	tempDir := t.TempDir()
	skillDir := filepath.Join(tempDir, ".claude", "skills", "integration-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("Failed to create skill dir: %v", err)
	}

	// Create metadata.toml
	metadata := `[artifact]
name = "integration-skill"
type = "skill"
description = "An integration test skill"

[skill]
prompt-file = "SKILL.md"
`
	if err := os.WriteFile(filepath.Join(skillDir, "metadata.toml"), []byte(metadata), 0644); err != nil {
		t.Fatalf("Failed to write metadata: %v", err)
	}

	// Create SKILL.md
	skillContent := `# Integration Skill

This skill helps with @example.txt integration testing.

## Instructions
1. Do the thing
2. Check the other thing
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatalf("Failed to write SKILL.md: %v", err)
	}

	// Create example.txt
	if err := os.WriteFile(filepath.Join(skillDir, "example.txt"), []byte("example content"), 0644); err != nil {
		t.Fatalf("Failed to write example.txt: %v", err)
	}

	// Use a mock client that reads from our temp directory
	mock := newMockClient()
	mock.addSkill("integration-skill", "An integration test skill", "1.0.0", skillContent, skillDir)

	registry := clients.NewRegistry()
	registry.Register(mock)

	server := NewServer(registry)

	// Create and connect
	impl := &mcp.Implementation{Name: "skills", Version: "1.0.0"}
	mcpServer := mcp.NewServer(impl, nil)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "read_skill",
		Description: "Read a skill's full instructions and content.",
	}, server.handleReadSkill)

	ctx := context.Background()
	t1, t2 := mcp.NewInMemoryTransports()

	_, err := mcpServer.Connect(ctx, t1, nil)
	if err != nil {
		t.Fatalf("Failed to connect server: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v1.0.0"}, nil)
	session, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("Failed to connect client: %v", err)
	}
	defer session.Close()

	// Read the skill
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "read_skill",
		Arguments: map[string]any{"name": "integration-skill"},
	})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	if result.IsError {
		t.Fatalf("Tool returned error: %v", result.Content)
	}

	// Verify content contains expected data
	textContent, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Expected TextContent, got %T", result.Content[0])
	}

	// The response should be plain markdown
	if textContent.Text == "" {
		t.Error("Expected content to be non-empty")
	}

	// The @example.txt reference should be resolved to absolute path since file exists
	expectedAbsPath := "@" + filepath.Join(skillDir, "example.txt")
	if !strings.Contains(textContent.Text, expectedAbsPath) {
		t.Errorf("Expected @example.txt to be resolved to %s, got: %s", expectedAbsPath, textContent.Text)
	}
}

func TestResolveFileReferences(t *testing.T) {
	// Create a temp directory with a test file
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "exists.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	tests := []struct {
		name     string
		content  string
		baseDir  string
		expected string
	}{
		{
			name:     "resolves existing file",
			content:  "See @exists.txt for details",
			baseDir:  tempDir,
			expected: "See @" + testFile + " for details",
		},
		{
			name:     "leaves non-existent file unchanged",
			content:  "See @nonexistent.txt for details",
			baseDir:  tempDir,
			expected: "See @nonexistent.txt for details",
		},
		{
			name:     "handles multiple references",
			content:  "See @exists.txt and @missing.md",
			baseDir:  tempDir,
			expected: "See @" + testFile + " and @missing.md",
		},
		{
			name:     "ignores non-file @ references",
			content:  "Contact @username on Twitter",
			baseDir:  tempDir,
			expected: "Contact @username on Twitter",
		},
		{
			name:     "handles nested paths",
			content:  "See @subdir/file.txt",
			baseDir:  tempDir,
			expected: "See @subdir/file.txt", // doesn't exist, unchanged
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveFileReferences(tt.content, tt.baseDir)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// mockUsageReporter captures usage reports for testing
type mockUsageReporter struct {
	mu      sync.Mutex
	reports []usageReport
	called  chan struct{}
}

type usageReport struct {
	skillName    string
	skillVersion string
}

func newMockUsageReporter() *mockUsageReporter {
	return &mockUsageReporter{
		called: make(chan struct{}, 1),
	}
}

func (m *mockUsageReporter) ReportSkillUsage(skillName, skillVersion string) {
	m.mu.Lock()
	m.reports = append(m.reports, usageReport{skillName, skillVersion})
	m.mu.Unlock()
	select {
	case m.called <- struct{}{}:
	default:
	}
}

func (m *mockUsageReporter) getReports() []usageReport {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]usageReport{}, m.reports...)
}

func TestServer_ReadSkill_ReportsUsage(t *testing.T) {
	// Create a mock client with a test skill
	mock := newMockClient()
	mock.addSkill("usage-test-skill", "A skill for testing usage", "2.0.0", "# Usage Test\n\nContent here.", "/tmp/skills/usage-test")

	registry := clients.NewRegistry()
	registry.Register(mock)

	server := NewServer(registry)

	// Inject mock usage reporter
	mockReporter := newMockUsageReporter()
	server.SetUsageReporter(mockReporter)

	// Create and connect MCP server
	impl := &mcp.Implementation{Name: "skills", Version: "1.0.0"}
	mcpServer := mcp.NewServer(impl, nil)
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "read_skill",
		Description: "Read a skill's full instructions and content.",
	}, server.handleReadSkill)

	ctx := context.Background()
	t1, t2 := mcp.NewInMemoryTransports()

	_, err := mcpServer.Connect(ctx, t1, nil)
	if err != nil {
		t.Fatalf("Failed to connect server: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v1.0.0"}, nil)
	session, err := client.Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("Failed to connect client: %v", err)
	}
	defer session.Close()

	// Read the skill
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "read_skill",
		Arguments: map[string]any{"name": "usage-test-skill"},
	})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if result.IsError {
		t.Fatalf("Tool returned error: %v", result.Content)
	}

	// Wait for the goroutine to call the reporter
	<-mockReporter.called

	// Verify the usage was reported
	reports := mockReporter.getReports()
	if len(reports) != 1 {
		t.Fatalf("Expected 1 usage report, got %d", len(reports))
	}

	if reports[0].skillName != "usage-test-skill" {
		t.Errorf("Expected skill name 'usage-test-skill', got %q", reports[0].skillName)
	}
	if reports[0].skillVersion != "2.0.0" {
		t.Errorf("Expected skill version '2.0.0', got %q", reports[0].skillVersion)
	}
}
