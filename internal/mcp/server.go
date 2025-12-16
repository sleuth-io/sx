package mcpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sleuth-io/skills/internal/clients"
	"github.com/sleuth-io/skills/internal/config"
	"github.com/sleuth-io/skills/internal/gitutil"
	"github.com/sleuth-io/skills/internal/logger"
	"github.com/sleuth-io/skills/internal/stats"
	vaultpkg "github.com/sleuth-io/skills/internal/vault"
)

// UsageReporter handles reporting skill usage
type UsageReporter interface {
	ReportSkillUsage(skillName, skillVersion string)
}

// Server provides an MCP server that exposes skill operations
type Server struct {
	registry      *clients.Registry
	usageReporter UsageReporter
}

// NewServer creates a new MCP server
func NewServer(registry *clients.Registry) *Server {
	s := &Server{
		registry: registry,
	}
	s.usageReporter = s // Server implements UsageReporter by default
	return s
}

// SetUsageReporter sets a custom usage reporter (for testing)
func (s *Server) SetUsageReporter(reporter UsageReporter) {
	s.usageReporter = reporter
}

// ReadSkillInput is the input type for read_skill tool
type ReadSkillInput struct {
	Name string `json:"name" jsonschema:"name of the skill to read"`
}

// fileRefPattern matches @filename or @path/to/file patterns in skill content
var fileRefPattern = regexp.MustCompile(`@([a-zA-Z0-9_\-./]+\.[a-zA-Z0-9]+)`)

// Run starts the MCP server over stdio
func (s *Server) Run(ctx context.Context) error {
	impl := &mcp.Implementation{
		Name:    "skills",
		Version: "1.0.0",
	}

	mcpServer := mcp.NewServer(impl, nil)

	// Register the read_skill tool - returns plain markdown text
	mcp.AddTool(mcpServer, &mcp.Tool{
		Name:        "read_skill",
		Description: "Read a skill's full instructions and content. Returns the skill content as markdown with @file references resolved to absolute paths.",
	}, s.handleReadSkill)

	// Run over stdio
	return mcpServer.Run(ctx, &mcp.StdioTransport{})
}

// handleReadSkill handles the read_skill tool invocation
// Returns plain markdown text with @file references resolved to absolute paths
func (s *Server) handleReadSkill(ctx context.Context, req *mcp.CallToolRequest, input ReadSkillInput) (*mcp.CallToolResult, any, error) {
	if input.Name == "" {
		return nil, nil, fmt.Errorf("skill name is required")
	}

	// Determine scope from current working directory
	scope, err := s.detectScope(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to detect scope: %w", err)
	}

	// Try each installed client until we find the skill
	installedClients := s.registry.DetectInstalled()
	for _, client := range installedClients {
		content, err := client.ReadSkill(ctx, input.Name, scope)
		if err == nil {
			// Report usage (best-effort, won't fail the MCP call)
			go s.usageReporter.ReportSkillUsage(content.Name, content.Version)

			// Resolve @file references to absolute paths
			resolvedContent := resolveFileReferences(content.Content, content.BaseDir)

			// Return plain markdown text
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: resolvedContent},
				},
			}, nil, nil
		}
	}

	return nil, nil, fmt.Errorf("skill not found: %s", input.Name)
}

// resolveFileReferences replaces @file references with absolute paths
// Only replaces if the file actually exists at the resolved path
func resolveFileReferences(content string, baseDir string) string {
	return fileRefPattern.ReplaceAllStringFunc(content, func(match string) string {
		// Extract the relative path (everything after @)
		relativePath := match[1:] // Remove the @ prefix

		// Build absolute path
		absolutePath := filepath.Join(baseDir, relativePath)

		// Only replace if the file exists
		if _, err := os.Stat(absolutePath); err == nil {
			return "@" + absolutePath
		}

		// File doesn't exist, leave the reference unchanged
		return match
	})
}

// detectScope determines the current scope using gitutil
func (s *Server) detectScope(ctx context.Context) (*clients.InstallScope, error) {
	gitContext, err := gitutil.DetectContext(ctx)
	if err != nil {
		return nil, err
	}

	if !gitContext.IsRepo {
		return &clients.InstallScope{Type: clients.ScopeGlobal}, nil
	}

	if gitContext.RelativePath == "." {
		return &clients.InstallScope{
			Type:     clients.ScopeRepository,
			RepoRoot: gitContext.RepoRoot,
			RepoURL:  gitContext.RepoURL,
		}, nil
	}

	return &clients.InstallScope{
		Type:     clients.ScopePath,
		RepoRoot: gitContext.RepoRoot,
		RepoURL:  gitContext.RepoURL,
		Path:     gitContext.RelativePath,
	}, nil
}

// ReportSkillUsage reports usage of a skill to the vault.
// This function runs in a goroutine and is best-effort - it will not block the MCP call.
func (s *Server) ReportSkillUsage(skillName, skillVersion string) {
	log := logger.Get()

	// Create usage event
	usageEvent := stats.UsageEvent{
		ArtifactName:    skillName,
		ArtifactVersion: skillVersion,
		ArtifactType:    "skill",
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
	}

	// Enqueue event (fast, local file write)
	if err := stats.EnqueueEvent(usageEvent); err != nil {
		log.Warn("failed to enqueue usage event", "skill", skillName, "error", err)
		return
	}

	log.Debug("skill usage enqueued", "name", skillName, "version", skillVersion)

	// Try to flush queue with timeout (network call, but we're already in a goroutine)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Load config to get vault
	cfg, err := config.Load()
	if err != nil {
		return
	}

	// Create vault instance
	vault, err := vaultpkg.NewFromConfig(cfg)
	if err != nil {
		return
	}

	// Try to flush queue
	_ = stats.FlushQueue(ctx, vault)
}
