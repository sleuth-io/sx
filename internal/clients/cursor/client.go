package cursor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sleuth-io/skills/internal/artifact"
	"github.com/sleuth-io/skills/internal/clients"
	"github.com/sleuth-io/skills/internal/clients/cursor/handlers"
	"github.com/sleuth-io/skills/internal/handlers/dirartifact"
	"github.com/sleuth-io/skills/internal/metadata"
)

var skillOps = dirartifact.NewOperations("skills", &artifact.TypeSkill)

// Client implements the clients.Client interface for Cursor
type Client struct {
	clients.BaseClient
}

// NewClient creates a new Cursor client
func NewClient() *Client {
	return &Client{
		BaseClient: clients.NewBaseClient(
			"cursor",
			"Cursor",
			[]artifact.Type{
				artifact.TypeMCP,
				artifact.TypeMCPRemote,
				artifact.TypeSkill, // Transform to commands
				artifact.TypeCommand,
				artifact.TypeHook, // Supported via hooks.json
			},
		),
	}
}

// IsInstalled checks if Cursor is installed by checking for .cursor directory
func (c *Client) IsInstalled() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	// Check for .cursor directory (primary indicator)
	configDir := filepath.Join(home, ".cursor")
	if stat, err := os.Stat(configDir); err == nil {
		return stat.IsDir()
	}

	return false
}

// GetVersion returns the Cursor version
func (c *Client) GetVersion() string {
	// Cursor doesn't have a standard --version command
	// Could check package.json in extension directory if needed
	return ""
}

// InstallArtifacts installs artifacts to Cursor using client-specific handlers
func (c *Client) InstallArtifacts(ctx context.Context, req clients.InstallRequest) (clients.InstallResponse, error) {
	resp := clients.InstallResponse{
		Results: make([]clients.ArtifactResult, 0, len(req.Artifacts)),
	}

	// Determine target directory based on scope
	targetBase := c.determineTargetBase(req.Scope)

	// Ensure target directory exists
	if err := os.MkdirAll(targetBase, 0755); err != nil {
		return resp, fmt.Errorf("failed to create target directory: %w", err)
	}

	// Install each artifact using appropriate handler
	for _, bundle := range req.Artifacts {
		result := clients.ArtifactResult{
			ArtifactName: bundle.Artifact.Name,
		}

		var err error
		switch bundle.Metadata.Artifact.Type {
		case artifact.TypeMCP:
			handler := handlers.NewMCPHandler(bundle.Metadata)
			err = handler.Install(ctx, bundle.ZipData, targetBase)
		case artifact.TypeMCPRemote:
			handler := handlers.NewMCPRemoteHandler(bundle.Metadata)
			err = handler.Install(ctx, bundle.ZipData, targetBase)
		case artifact.TypeSkill:
			// Install skill to .cursor/skills/ (not transformed to command)
			handler := handlers.NewSkillHandler(bundle.Metadata)
			err = handler.Install(ctx, bundle.ZipData, targetBase)
		case artifact.TypeCommand:
			handler := handlers.NewCommandHandler(bundle.Metadata)
			err = handler.Install(ctx, bundle.ZipData, targetBase)
		case artifact.TypeHook:
			handler := handlers.NewHookHandler(bundle.Metadata)
			err = handler.Install(ctx, bundle.ZipData, targetBase)
		default:
			result.Status = clients.StatusSkipped
			result.Message = fmt.Sprintf("Unsupported artifact type: %s", bundle.Metadata.Artifact.Type.Key)
			resp.Results = append(resp.Results, result)
			continue
		}

		if err != nil {
			result.Status = clients.StatusFailed
			result.Error = err
			result.Message = fmt.Sprintf("Installation failed: %v", err)
		} else {
			result.Status = clients.StatusSuccess
			result.Message = fmt.Sprintf("Installed to %s", targetBase)
		}

		resp.Results = append(resp.Results, result)
	}

	// Note: Skills support (rules file, MCP server) is configured by EnsureSkillsSupport
	// which is called by the install command after all artifacts are installed

	return resp, nil
}

// UninstallArtifacts removes artifacts from Cursor
func (c *Client) UninstallArtifacts(ctx context.Context, req clients.UninstallRequest) (clients.UninstallResponse, error) {
	resp := clients.UninstallResponse{
		Results: make([]clients.ArtifactResult, 0, len(req.Artifacts)),
	}

	targetBase := c.determineTargetBase(req.Scope)

	for _, art := range req.Artifacts {
		result := clients.ArtifactResult{
			ArtifactName: art.Name,
		}

		// Create minimal metadata for removal
		meta := &metadata.Metadata{
			Artifact: metadata.Artifact{
				Name: art.Name,
				Type: art.Type,
			},
		}

		var err error
		switch art.Type {
		case artifact.TypeMCP, artifact.TypeMCPRemote:
			handler := handlers.NewMCPHandler(meta)
			err = handler.Remove(ctx, targetBase)
		case artifact.TypeSkill:
			handler := handlers.NewSkillHandler(meta)
			err = handler.Remove(ctx, targetBase)
		case artifact.TypeCommand:
			handler := handlers.NewCommandHandler(meta)
			err = handler.Remove(ctx, targetBase)
		case artifact.TypeHook:
			handler := handlers.NewHookHandler(meta)
			err = handler.Remove(ctx, targetBase)
		default:
			result.Status = clients.StatusSkipped
			result.Message = fmt.Sprintf("Unsupported artifact type: %s", art.Type.Key)
			resp.Results = append(resp.Results, result)
			continue
		}

		if err != nil {
			result.Status = clients.StatusFailed
			result.Error = err
		} else {
			result.Status = clients.StatusSuccess
			result.Message = "Uninstalled successfully"
		}

		resp.Results = append(resp.Results, result)
	}

	return resp, nil
}

// determineTargetBase returns the installation directory based on scope
func (c *Client) determineTargetBase(scope *clients.InstallScope) string {
	home, _ := os.UserHomeDir()

	switch scope.Type {
	case clients.ScopeGlobal:
		return filepath.Join(home, ".cursor")
	case clients.ScopeRepository:
		return filepath.Join(scope.RepoRoot, ".cursor")
	case clients.ScopePath:
		return filepath.Join(scope.RepoRoot, scope.Path, ".cursor")
	default:
		return filepath.Join(home, ".cursor")
	}
}

// EnsureSkillsSupport ensures skills infrastructure is set up for the current context.
// This scans skills from all applicable scopes (global, repo, path) and creates
// a local .cursor/rules/skills.md file listing all available skills.
// This must be called even when no new skills are installed, to ensure the
// local rules file exists (Cursor doesn't load global rules).
func (c *Client) EnsureSkillsSupport(ctx context.Context, scope *clients.InstallScope) error {
	// 1. Register skills MCP server globally (idempotent)
	if err := c.registerSkillsMCPServer(); err != nil {
		return fmt.Errorf("failed to register MCP server: %w", err)
	}

	// 2. Collect skills from all applicable scopes
	allSkills := c.collectAllScopeSkills(scope)

	// 3. Determine local target (current working directory context)
	localTarget := c.determineLocalTarget(scope)
	if localTarget == "" {
		// No local context (shouldn't happen if scope is properly set)
		return nil
	}

	// 4. Generate rules file with all skills
	return c.generateSkillsRulesFileFromSkills(allSkills, localTarget)
}

// collectAllScopeSkills gathers skills from global, repo, and path scopes
func (c *Client) collectAllScopeSkills(scope *clients.InstallScope) []clients.InstalledSkill {
	var allSkills []clients.InstalledSkill
	seen := make(map[string]bool)

	// Helper to add skills without duplicates (path > repo > global precedence)
	addSkills := func(skills []clients.InstalledSkill) {
		for _, skill := range skills {
			if !seen[skill.Name] {
				seen[skill.Name] = true
				allSkills = append(allSkills, skill)
			}
		}
	}

	// 1. Path-scoped skills (highest precedence)
	if scope.Type == clients.ScopePath && scope.RepoRoot != "" && scope.Path != "" {
		pathBase := filepath.Join(scope.RepoRoot, scope.Path, ".cursor")
		if skills, err := skillOps.ScanInstalled(pathBase); err == nil {
			for _, s := range skills {
				addSkills([]clients.InstalledSkill{{Name: s.Name, Description: s.Description, Version: s.Version}})
			}
		}
	}

	// 2. Repo-scoped skills
	if scope.RepoRoot != "" {
		repoBase := filepath.Join(scope.RepoRoot, ".cursor")
		if skills, err := skillOps.ScanInstalled(repoBase); err == nil {
			for _, s := range skills {
				addSkills([]clients.InstalledSkill{{Name: s.Name, Description: s.Description, Version: s.Version}})
			}
		}
	}

	// 3. Global skills (lowest precedence)
	home, _ := os.UserHomeDir()
	globalBase := filepath.Join(home, ".cursor")
	if skills, err := skillOps.ScanInstalled(globalBase); err == nil {
		for _, s := range skills {
			addSkills([]clients.InstalledSkill{{Name: s.Name, Description: s.Description, Version: s.Version}})
		}
	}

	return allSkills
}

// determineLocalTarget returns the local .cursor directory for rules file
// This is where the rules file will be created so Cursor can load it
func (c *Client) determineLocalTarget(scope *clients.InstallScope) string {
	switch scope.Type {
	case clients.ScopePath:
		if scope.RepoRoot != "" && scope.Path != "" {
			return filepath.Join(scope.RepoRoot, scope.Path, ".cursor")
		}
		fallthrough
	case clients.ScopeRepository:
		if scope.RepoRoot != "" {
			return filepath.Join(scope.RepoRoot, ".cursor")
		}
		fallthrough
	default:
		// For global scope, use current working directory if in a repo
		// Otherwise, no local target (global rules don't work in Cursor)
		cwd, err := os.Getwd()
		if err != nil {
			return ""
		}
		return filepath.Join(cwd, ".cursor")
	}
}

// generateSkillsRulesFileFromSkills creates the rules file from a list of skills
func (c *Client) generateSkillsRulesFileFromSkills(skills []clients.InstalledSkill, targetBase string) error {
	rulesDir := filepath.Join(targetBase, "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		return err
	}

	rulePath := filepath.Join(rulesDir, "skills.md")

	// If no skills, remove the rules file if it exists
	if len(skills) == 0 {
		if _, err := os.Stat(rulePath); err == nil {
			return os.Remove(rulePath)
		}
		return nil
	}

	// Build skill list
	var skillsList string
	for _, skill := range skills {
		skillsList += fmt.Sprintf("\n<skill>\n<name>%s</name>\n<description>%s</description>\n</skill>\n",
			skill.Name, skill.Description)
	}

	// Generate complete skills.md with frontmatter
	content := fmt.Sprintf(`---
description: "Available skills for AI assistance"
alwaysApply: true
---

<!-- AUTO-GENERATED by Sleuth Skills - Do not edit manually -->
<!-- Run 'skills install' to regenerate this file -->

## Available Skills

You have access to the following skills. When a user's task matches a skill, use the %sread_skill%s MCP tool to load full instructions.

<available_skills>
%s
</available_skills>

## Usage

Invoke %sread_skill(name: "skill-name")%s via the MCP tool when needed.

The tool returns the skill content as markdown. Any %s@filename%s references in the content are automatically resolved to absolute paths.
`, "`", "`", skillsList, "`", "`", "`", "`")

	return os.WriteFile(rulePath, []byte(content), 0644)
}

// registerSkillsMCPServer adds skills MCP server to ~/.cursor/mcp.json
func (c *Client) registerSkillsMCPServer() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	mcpConfigPath := filepath.Join(home, ".cursor", "mcp.json")

	// Read existing mcp.json
	config, err := handlers.ReadMCPConfig(mcpConfigPath)
	if err != nil {
		return err
	}

	// Only add if missing (don't overwrite existing entry)
	if config.MCPServers == nil {
		config.MCPServers = make(map[string]interface{})
	}

	if _, exists := config.MCPServers["skills"]; exists {
		// Already configured, don't overwrite
		return nil
	}

	// Get path to skills binary
	skillsBinary, err := os.Executable()
	if err != nil {
		return err
	}

	// Add skills MCP server entry
	config.MCPServers["skills"] = map[string]interface{}{
		"command": skillsBinary,
		"args":    []string{"serve"},
	}

	return handlers.WriteMCPConfig(mcpConfigPath, config)
}

// ListSkills returns all installed skills for a given scope
func (c *Client) ListSkills(ctx context.Context, scope *clients.InstallScope) ([]clients.InstalledSkill, error) {
	targetBase := c.determineTargetBase(scope)

	installed, err := skillOps.ScanInstalled(targetBase)
	if err != nil {
		return nil, fmt.Errorf("failed to scan installed skills: %w", err)
	}

	skills := make([]clients.InstalledSkill, 0, len(installed))
	for _, info := range installed {
		skills = append(skills, clients.InstalledSkill{
			Name:        info.Name,
			Description: info.Description,
			Version:     info.Version,
		})
	}

	return skills, nil
}

// ReadSkill reads the content of a specific skill by name
func (c *Client) ReadSkill(ctx context.Context, name string, scope *clients.InstallScope) (*clients.SkillContent, error) {
	targetBase := c.determineTargetBase(scope)

	content, baseDir, description, err := skillOps.ReadPromptContent(targetBase, name, "SKILL.md", func(m *metadata.Metadata) string { return m.Skill.PromptFile })
	if err != nil {
		return nil, err
	}

	return &clients.SkillContent{
		Name:        name,
		Description: description,
		Content:     content,
		BaseDir:     baseDir,
	}, nil
}

// InstallHooks is a no-op for Cursor since it doesn't need system hooks.
// This method exists to satisfy the Client interface.
func (c *Client) InstallHooks(ctx context.Context) error {
	// Cursor doesn't need system hooks
	return nil
}

func init() {
	// Auto-register on package import
	clients.Register(NewClient())
}
