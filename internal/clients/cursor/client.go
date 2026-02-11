package cursor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/bootstrap"
	"github.com/sleuth-io/sx/internal/cache"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/clients/cursor/handlers"
	"github.com/sleuth-io/sx/internal/handlers/dirasset"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/metadata"
)

var skillOps = dirasset.NewOperations("skills", &asset.TypeSkill)

// Client implements the clients.Client interface for Cursor
type Client struct {
	clients.BaseClient
}

// NewClient creates a new Cursor client
func NewClient() *Client {
	return &Client{
		BaseClient: clients.NewBaseClient(
			clients.ClientIDCursor,
			"Cursor",
			[]asset.Type{
				asset.TypeMCP,
				asset.TypeSkill, // Transform to commands
				asset.TypeCommand,
				asset.TypeHook, // Supported via hooks.json
				asset.TypeRule, // .cursor/rules/{name}.mdc
			},
		),
	}
}

// RuleCapabilities returns Cursor's rule capabilities
func (c *Client) RuleCapabilities() *clients.RuleCapabilities {
	return RuleCapabilities()
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

// InstallAssets installs assets to Cursor using client-specific handlers
func (c *Client) InstallAssets(ctx context.Context, req clients.InstallRequest) (clients.InstallResponse, error) {
	resp := clients.InstallResponse{
		Results: make([]clients.AssetResult, 0, len(req.Assets)),
	}

	// Determine target directory based on scope
	targetBase, err := c.determineTargetBase(req.Scope)
	if err != nil {
		return resp, fmt.Errorf("cannot determine installation directory: %w", err)
	}

	// Ensure target directory exists
	if err := os.MkdirAll(targetBase, 0755); err != nil {
		return resp, fmt.Errorf("failed to create target directory: %w", err)
	}

	// Install each asset using appropriate handler
	for _, bundle := range req.Assets {
		result := clients.AssetResult{
			AssetName: bundle.Asset.Name,
		}

		var err error
		switch bundle.Metadata.Asset.Type {
		case asset.TypeMCP:
			handler := handlers.NewMCPHandler(bundle.Metadata)
			err = handler.Install(ctx, bundle.ZipData, targetBase)
		case asset.TypeSkill:
			// Install skill to .cursor/skills/ (not transformed to command)
			handler := handlers.NewSkillHandler(bundle.Metadata)
			err = handler.Install(ctx, bundle.ZipData, targetBase)
		case asset.TypeCommand:
			handler := handlers.NewCommandHandler(bundle.Metadata)
			err = handler.Install(ctx, bundle.ZipData, targetBase)
		case asset.TypeHook:
			handler := handlers.NewHookHandler(bundle.Metadata)
			err = handler.Install(ctx, bundle.ZipData, targetBase)
		case asset.TypeRule:
			handler := handlers.NewRuleHandler(bundle.Metadata, "")
			err = handler.Install(ctx, bundle.ZipData, targetBase)
		default:
			result.Status = clients.StatusSkipped
			result.Message = "Unsupported asset type: " + bundle.Metadata.Asset.Type.Key
			resp.Results = append(resp.Results, result)
			continue
		}

		if err != nil {
			result.Status = clients.StatusFailed
			result.Error = err
			result.Message = fmt.Sprintf("Installation failed: %v", err)
		} else {
			result.Status = clients.StatusSuccess
			result.Message = "Installed to " + targetBase
		}

		resp.Results = append(resp.Results, result)
	}

	// Note: Skills support (rules file, MCP server) is configured by EnsureSkillsSupport
	// which is called by the install command after all assets are installed

	return resp, nil
}

// UninstallAssets removes assets from Cursor
func (c *Client) UninstallAssets(ctx context.Context, req clients.UninstallRequest) (clients.UninstallResponse, error) {
	resp := clients.UninstallResponse{
		Results: make([]clients.AssetResult, 0, len(req.Assets)),
	}

	targetBase, err := c.determineTargetBase(req.Scope)
	if err != nil {
		return resp, fmt.Errorf("cannot determine uninstall directory: %w", err)
	}

	for _, a := range req.Assets {
		result := clients.AssetResult{
			AssetName: a.Name,
		}

		// Create minimal metadata for removal
		meta := &metadata.Metadata{
			Asset: metadata.Asset{
				Name: a.Name,
				Type: a.Type,
			},
		}

		var err error
		switch a.Type {
		case asset.TypeMCP:
			handler := handlers.NewMCPHandler(meta)
			err = handler.Remove(ctx, targetBase)
		case asset.TypeSkill:
			handler := handlers.NewSkillHandler(meta)
			err = handler.Remove(ctx, targetBase)
		case asset.TypeCommand:
			handler := handlers.NewCommandHandler(meta)
			err = handler.Remove(ctx, targetBase)
		case asset.TypeHook:
			handler := handlers.NewHookHandler(meta)
			err = handler.Remove(ctx, targetBase)
		case asset.TypeRule:
			handler := handlers.NewRuleHandler(meta, "")
			err = handler.Remove(ctx, targetBase)
		default:
			result.Status = clients.StatusSkipped
			result.Message = "Unsupported asset type: " + a.Type.Key
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
// Returns an error if a repo/path-scoped install is requested without a valid RepoRoot
func (c *Client) determineTargetBase(scope *clients.InstallScope) (string, error) {
	home, _ := os.UserHomeDir()

	switch scope.Type {
	case clients.ScopeGlobal:
		return filepath.Join(home, ".cursor"), nil
	case clients.ScopeRepository:
		if scope.RepoRoot == "" {
			return "", errors.New("repo-scoped install requires RepoRoot but none provided (not in a git repository?)")
		}
		return filepath.Join(scope.RepoRoot, ".cursor"), nil
	case clients.ScopePath:
		if scope.RepoRoot == "" {
			return "", errors.New("path-scoped install requires RepoRoot but none provided (not in a git repository?)")
		}
		return filepath.Join(scope.RepoRoot, scope.Path, ".cursor"), nil
	default:
		return filepath.Join(home, ".cursor"), nil
	}
}

// EnsureAssetSupport ensures asset infrastructure is set up for the current context.
// This scans skills from all applicable scopes (global, repo, path) and creates
// a local .cursor/rules/skills.md file listing all available skills.
// This must be called even when no new assets are installed, to ensure the
// local rules file exists (Cursor doesn't load global rules).
func (c *Client) EnsureAssetSupport(ctx context.Context, scope *clients.InstallScope) error {
	log := logger.Get()

	// 1. Register skills MCP server globally (idempotent)
	if err := c.registerSkillsMCPServer(); err != nil {
		return fmt.Errorf("failed to register MCP server: %w", err)
	}

	// 2. Collect skills from all applicable scopes
	allSkills := c.collectAllScopeSkills(scope)
	log.Debug("collected skills for rules file", "count", len(allSkills), "scope_type", scope.Type, "repo_root", scope.RepoRoot)

	// 3. Determine local target (current working directory context)
	localTarget := c.determineLocalTarget(scope)
	if localTarget == "" {
		// No local context (shouldn't happen if scope is properly set)
		log.Warn("no local target for rules file", "scope_type", scope.Type, "repo_root", scope.RepoRoot)
		return nil
	}

	log.Debug("generating rules file", "target", localTarget, "skill_count", len(allSkills))

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
	case clients.ScopeGlobal:
		// For global scope, use current working directory if in a repo
		// Otherwise, no local target (global rules don't work in Cursor)
		cwd, err := os.Getwd()
		if err != nil {
			return ""
		}
		return filepath.Join(cwd, ".cursor")
	case clients.ScopePath:
		if scope.RepoRoot != "" && scope.Path != "" {
			return filepath.Join(scope.RepoRoot, scope.Path, ".cursor")
		}
		fallthrough
	case clients.ScopeRepository:
		if scope.RepoRoot != "" {
			return filepath.Join(scope.RepoRoot, ".cursor")
		}
	}
	// Fallback: use current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return filepath.Join(cwd, ".cursor")
}

// generateSkillsRulesFileFromSkills cleans up the legacy skills.md rules file.
// Skills are now discovered natively by Cursor from .cursor/skills/ directory,
// so the rules file is no longer needed.
func (c *Client) generateSkillsRulesFileFromSkills(skills []clients.InstalledSkill, targetBase string) error {
	rulePath := filepath.Join(targetBase, "rules", "skills.md")

	// Remove legacy rules file if it exists (no longer needed with native skill discovery)
	if _, err := os.Stat(rulePath); err == nil {
		return os.Remove(rulePath)
	}
	return nil
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
		config.MCPServers = make(map[string]any)
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
	config.MCPServers["skills"] = map[string]any{
		"command": skillsBinary,
		"args":    []string{"serve"},
	}

	return handlers.WriteMCPConfig(mcpConfigPath, config)
}

// ListAssets returns all installed skills for a given scope
func (c *Client) ListAssets(ctx context.Context, scope *clients.InstallScope) ([]clients.InstalledSkill, error) {
	targetBase, err := c.determineTargetBase(scope)
	if err != nil {
		return nil, fmt.Errorf("cannot determine target directory: %w", err)
	}

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
	targetBase, err := c.determineTargetBase(scope)
	if err != nil {
		return nil, fmt.Errorf("cannot determine target directory: %w", err)
	}

	result, err := skillOps.ReadPromptContent(targetBase, name, "SKILL.md", func(m *metadata.Metadata) string { return m.Skill.PromptFile })
	if err != nil {
		return nil, err
	}

	return &clients.SkillContent{
		Name:        name,
		Description: result.Description,
		Version:     result.Version,
		Content:     result.Content,
		BaseDir:     result.BaseDir,
	}, nil
}

// GetBootstrapOptions returns bootstrap options for Cursor.
// This includes the beforeSubmitPrompt hook for auto-update.
func (c *Client) GetBootstrapOptions(ctx context.Context) []bootstrap.Option {
	return []bootstrap.Option{bootstrap.CursorBeforeSubmitHook}
}

// InstallBootstrap installs Cursor infrastructure (hooks and MCP servers).
// Only installs options that are present in the opts slice.
func (c *Client) InstallBootstrap(ctx context.Context, opts []bootstrap.Option) error {
	// Install beforeSubmitPrompt hook (if enabled)
	if bootstrap.ContainsKey(opts, bootstrap.CursorSessionHookKey) {
		if err := c.installBeforeSubmitPromptHook(); err != nil {
			return err
		}
	}

	// Install MCP servers from options that have MCPConfig
	for _, opt := range opts {
		if opt.MCPConfig != nil {
			if err := c.installMCPServerFromConfig(opt.MCPConfig); err != nil {
				return fmt.Errorf("failed to install MCP server %s: %w", opt.MCPConfig.Name, err)
			}
		}
	}

	return nil
}

// installMCPServerFromConfig installs an MCP server from a bootstrap.MCPServerConfig
func (c *Client) installMCPServerFromConfig(config *bootstrap.MCPServerConfig) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	cursorDir := filepath.Join(home, ".cursor")
	log := logger.Get()

	serverConfig := map[string]any{
		"command": config.Command,
		"args":    config.Args,
	}

	// Add env if present
	if len(config.Env) > 0 {
		serverConfig["env"] = config.Env
	}

	if err := handlers.AddMCPServer(cursorDir, config.Name, serverConfig); err != nil {
		return err
	}

	log.Info("MCP server installed", "server", config.Name, "command", config.Command)
	return nil
}

// UninstallBootstrap removes Cursor infrastructure (hooks and MCP servers).
// Only uninstalls options that are present in the opts slice.
func (c *Client) UninstallBootstrap(ctx context.Context, opts []bootstrap.Option) error {
	for _, opt := range opts {
		switch opt.Key {
		case bootstrap.CursorSessionHookKey:
			if err := c.uninstallBeforeSubmitPromptHook(); err != nil {
				return err
			}
		default:
			// Handle MCP options generically
			if opt.MCPConfig != nil {
				if err := c.uninstallMCPServerByName(opt.MCPConfig.Name); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// ShouldInstall checks if installation should proceed based on conversation tracking.
// Cursor fires beforeSubmitPrompt on every prompt, so we track conversation IDs
// to only run install once per conversation.
func (c *Client) ShouldInstall(ctx context.Context) (bool, error) {
	log := logger.Get()

	// Check if stdin has data (hook mode passes JSON via stdin)
	if !hasStdinData() {
		// Not a hook call (e.g., manual invocation), proceed with install
		return true, nil
	}

	// Try to parse Cursor hook input
	input, err := parseCursorHookInput()
	if err != nil {
		// Can't parse input, proceed with install anyway
		log.Debug("failed to parse cursor hook input, proceeding with install", "error", err)
		return true, nil
	}

	// No conversation ID means we can't track, proceed with install
	if input.ConversationID == "" {
		return true, nil
	}

	// Check session cache
	sessionCache, err := cache.NewSessionCache(c.ID())
	if err != nil {
		log.Error("failed to create session cache", "error", err)
		return true, nil // Proceed on error
	}

	// Already seen this conversation? Skip install
	if sessionCache.HasSession(input.ConversationID) {
		log.Debug("conversation already seen, skipping install", "conversation_id", input.ConversationID)
		return false, nil
	}

	// Record optimistically (before install starts)
	if err := sessionCache.RecordSession(input.ConversationID); err != nil {
		log.Error("failed to record session", "error", err)
		// Continue anyway - worst case we install again next time
	}

	// Cull old entries periodically (5 days)
	if err := sessionCache.CullOldEntries(5 * 24 * time.Hour); err != nil {
		log.Debug("failed to cull old session entries", "error", err)
		// Non-fatal, continue
	}

	return true, nil
}

// cursorHookInput represents the JSON structure passed by Cursor hooks via stdin
type cursorHookInput struct {
	ConversationID string   `json:"conversation_id"`
	GenerationID   string   `json:"generation_id"`
	Prompt         string   `json:"prompt"`
	HookEventName  string   `json:"hook_event_name"`
	WorkspaceRoots []string `json:"workspace_roots"` // Workspace directory paths
	Model          string   `json:"model"`
	CursorVersion  string   `json:"cursor_version"`
}

// hasStdinData checks if there's data available on stdin without blocking.
// Returns false if stdin is a terminal or has no data.
func hasStdinData() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	// Check if stdin is a pipe or has data
	return (stat.Mode() & os.ModeCharDevice) == 0
}

// parseCursorHookInput reads and parses the Cursor hook JSON from stdin.
// If stdin was already read by ParseWorkspaceDir, it uses the cached version.
func parseCursorHookInput() (*cursorHookInput, error) {
	var input cursorHookInput

	// Try to use cached stdin first (if ParseWorkspaceDir was called)
	if cachedReader := GetCachedStdin(); cachedReader != nil {
		decoder := json.NewDecoder(cachedReader)
		if err := decoder.Decode(&input); err != nil {
			return nil, fmt.Errorf("failed to decode hook input from cache: %w", err)
		}
		return &input, nil
	}

	// Fallback: read from stdin directly
	decoder := json.NewDecoder(os.Stdin)
	if err := decoder.Decode(&input); err != nil {
		return nil, fmt.Errorf("failed to decode hook input: %w", err)
	}
	return &input, nil
}

// uninstallBeforeSubmitPromptHook removes the beforeSubmitPrompt hook
func (c *Client) uninstallBeforeSubmitPromptHook() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	hooksJSONPath := filepath.Join(home, ".cursor", "hooks.json")
	log := logger.Get()

	// Read existing hooks.json
	config, err := handlers.ReadHooksJSON(hooksJSONPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No hooks.json, nothing to uninstall
			return nil
		}
		return fmt.Errorf("failed to read hooks.json: %w", err)
	}

	// Filter out our hook from beforeSubmitPrompt
	hooks, ok := config.Hooks["beforeSubmitPrompt"]
	if !ok || len(hooks) == 0 {
		// No beforeSubmitPrompt hooks, nothing to remove
		return nil
	}

	filtered := []map[string]any{}
	for _, hook := range hooks {
		cmd, ok := hook["command"].(string)
		if !ok {
			filtered = append(filtered, hook)
			continue
		}
		if !strings.HasPrefix(cmd, "sx install") && !strings.HasPrefix(cmd, "skills install") {
			filtered = append(filtered, hook)
		}
	}

	// Only modify if something was filtered
	if len(filtered) == len(hooks) {
		return nil
	}

	if len(filtered) == 0 {
		delete(config.Hooks, "beforeSubmitPrompt")
	} else {
		config.Hooks["beforeSubmitPrompt"] = filtered
	}

	log.Info("hook removed", "hook", "beforeSubmitPrompt")

	if err := handlers.WriteHooksJSON(hooksJSONPath, config); err != nil {
		return fmt.Errorf("failed to write hooks.json: %w", err)
	}

	return nil
}

// installBeforeSubmitPromptHook installs the beforeSubmitPrompt hook for auto-install
func (c *Client) installBeforeSubmitPromptHook() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	hooksJSONPath := filepath.Join(home, ".cursor", "hooks.json")
	log := logger.Get()

	// Read existing hooks.json
	config, err := handlers.ReadHooksJSON(hooksJSONPath)
	if err != nil {
		return fmt.Errorf("failed to read hooks.json: %w", err)
	}

	hookCommand := "sx install --hook-mode --client=cursor"

	// First, check if exact hook command already exists
	exactMatch := false
	var oldHookRef map[string]any
	if hooks, ok := config.Hooks["beforeSubmitPrompt"]; ok {
		for _, hook := range hooks {
			if cmd, ok := hook["command"].(string); ok {
				if cmd == hookCommand {
					exactMatch = true
					break
				}
				if strings.HasPrefix(cmd, "sx install") || strings.HasPrefix(cmd, "skills install") {
					oldHookRef = hook // Remember for updating
				}
			}
		}
	}

	// Already have exact match, nothing to do
	if exactMatch {
		return nil
	}

	// Get current working directory for context logging
	cwd, _ := os.Getwd()

	// Update old hook if found, otherwise add new
	if oldHookRef != nil {
		oldHookRef["command"] = hookCommand
		log.Info("hook updated", "hook", "beforeSubmitPrompt", "command", hookCommand, "cwd", cwd)
	} else {
		if config.Hooks["beforeSubmitPrompt"] == nil {
			config.Hooks["beforeSubmitPrompt"] = []map[string]any{}
		}
		config.Hooks["beforeSubmitPrompt"] = append(config.Hooks["beforeSubmitPrompt"], map[string]any{
			"command": hookCommand,
		})
		log.Info("hook installed", "hook", "beforeSubmitPrompt", "command", hookCommand, "cwd", cwd)
	}

	if err := handlers.WriteHooksJSON(hooksJSONPath, config); err != nil {
		return fmt.Errorf("failed to write hooks.json: %w", err)
	}

	return nil
}

// VerifyAssets checks if assets are actually installed on the filesystem
func (c *Client) VerifyAssets(ctx context.Context, assets []*lockfile.Asset, scope *clients.InstallScope) []clients.VerifyResult {
	results := make([]clients.VerifyResult, 0, len(assets))

	targetBase, err := c.determineTargetBase(scope)
	if err != nil {
		// Can't determine target - mark all assets as not installed
		for _, a := range assets {
			results = append(results, clients.VerifyResult{
				Asset:     a,
				Installed: false,
				Message:   fmt.Sprintf("cannot determine target directory: %v", err),
			})
		}
		return results
	}

	for _, a := range assets {
		result := clients.VerifyResult{
			Asset: a,
		}

		handler, err := handlers.NewHandler(a.Type, &metadata.Metadata{
			Asset: metadata.Asset{
				Name:    a.Name,
				Version: a.Version,
				Type:    a.Type,
			},
		})
		if err != nil {
			result.Message = err.Error()
		} else {
			result.Installed, result.Message = handler.VerifyInstalled(targetBase)
		}

		results = append(results, result)
	}

	return results
}

// ScanInstalledAssets returns an empty list for Cursor (not yet supported)
func (c *Client) ScanInstalledAssets(ctx context.Context, scope *clients.InstallScope) ([]clients.InstalledAsset, error) {
	// Cursor asset import not yet supported
	return []clients.InstalledAsset{}, nil
}

// GetAssetPath returns an error for Cursor (not yet supported)
func (c *Client) GetAssetPath(ctx context.Context, name string, assetType asset.Type, scope *clients.InstallScope) (string, error) {
	return "", errors.New("asset import not supported for Cursor")
}

// uninstallMCPServerByName removes an MCP server by its name
func (c *Client) uninstallMCPServerByName(name string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	cursorDir := filepath.Join(home, ".cursor")
	log := logger.Get()

	if err := handlers.RemoveMCPServer(cursorDir, name); err != nil {
		return err
	}

	log.Info("MCP server uninstalled", "server", name)
	return nil
}

func init() {
	// Auto-register on package import
	clients.Register(NewClient())
}
