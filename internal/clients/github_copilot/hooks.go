package github_copilot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/sx/internal/bootstrap"
	"github.com/sleuth-io/sx/internal/clients/github_copilot/handlers"
	"github.com/sleuth-io/sx/internal/logger"
)

// installBootstrap installs GitHub Copilot CLI infrastructure (hooks and MCP servers).
// This sets up hooks for auto-update/usage tracking and registers MCP servers.
// Only installs options that are present in the opts slice.
//
// Copilot CLI hooks go in .github/hooks/sx.json (workspace level).
func installBootstrap(opts []bootstrap.Option) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	log := logger.Get()

	// Install hooks to workspace .github/hooks/ (Copilot CLI only supports workspace-level hooks)
	installHooks := bootstrap.ContainsKey(opts, bootstrap.SessionHookKey) ||
		bootstrap.ContainsKey(opts, bootstrap.AnalyticsHookKey)

	if installHooks {
		repoRoot := findGitRoot()
		if repoRoot == "" {
			log.Warn("skipping Copilot hooks: not in a git repository (Copilot CLI only supports workspace-level hooks in .github/hooks/)")
		} else {
			if err := installCopilotHooks(repoRoot, opts); err != nil {
				return err
			}
		}
	}

	// Install MCP servers from options that have MCPConfig
	for _, opt := range opts {
		if opt.MCPConfig != nil {
			if err := installMCPServerFromConfig(home, opt.MCPConfig); err != nil {
				log.Error("failed to install MCP server", "server", opt.MCPConfig.Name, "error", err)
				return fmt.Errorf("failed to install MCP server %s: %w", opt.MCPConfig.Name, err)
			}
		}
	}

	return nil
}

// installCopilotHooks installs hooks to .github/hooks/sx.json
func installCopilotHooks(repoRoot string, opts []bootstrap.Option) error {
	hooksDir := filepath.Join(repoRoot, ".github", handlers.DirHooks)
	hookFilePath := filepath.Join(hooksDir, handlers.FileHooks)
	log := logger.Get()

	// Ensure hooks directory exists
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("failed to create hooks directory: %w", err)
	}

	// Read existing sx.json or create new
	var config CopilotHooksConfig
	if data, err := os.ReadFile(hookFilePath); err == nil {
		if err := json.Unmarshal(data, &config); err != nil {
			log.Error("failed to parse sx.json", "error", err)
			return fmt.Errorf("failed to parse sx.json: %w", err)
		}
	} else {
		config.Version = 1
		config.Hooks = make(map[string][]CopilotHookEntry)
	}

	// Ensure version is set
	if config.Version == 0 {
		config.Version = 1
	}

	// Install sessionStart hook (if enabled)
	if bootstrap.ContainsKey(opts, bootstrap.SessionHookKey) {
		installHook := "sx install --hook-mode --client=github-copilot"
		if !hasHookWithCommand(config.Hooks["sessionStart"], installHook) {
			// Remove old sx/skills install hooks first
			config.Hooks["sessionStart"] = removeHooksWithPrefix(config.Hooks["sessionStart"], "sx install", "skills install")
			config.Hooks["sessionStart"] = append(config.Hooks["sessionStart"], CopilotHookEntry{
				Type:       "command",
				Bash:       installHook,
				TimeoutSec: 30,
			})
			log.Info("hook installed", "hook", "sessionStart", "command", installHook)
		}
	}

	// Install postToolUse hook (if enabled)
	if bootstrap.ContainsKey(opts, bootstrap.AnalyticsHookKey) {
		reportHook := "sx report-usage --client=github-copilot"
		if !hasHookWithCommand(config.Hooks["postToolUse"], reportHook) {
			// Remove old sx/skills report-usage hooks first
			config.Hooks["postToolUse"] = removeHooksWithPrefix(config.Hooks["postToolUse"], "sx report-usage", "skills report-usage")
			config.Hooks["postToolUse"] = append(config.Hooks["postToolUse"], CopilotHookEntry{
				Type:       "command",
				Bash:       reportHook,
				TimeoutSec: 30,
			})
			log.Info("hook installed", "hook", "postToolUse", "command", reportHook)
		}
	}

	// Write back to file
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal sx.json: %w", err)
	}

	if err := os.WriteFile(hookFilePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write sx.json: %w", err)
	}

	return nil
}

// findGitRoot finds the root of the git repository from the current working directory.
// Returns empty string if not in a git repository.
func findGitRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	dir := cwd
	for {
		gitDir := filepath.Join(dir, ".git")
		if stat, err := os.Stat(gitDir); err == nil && stat.IsDir() {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached root without finding .git
			return ""
		}
		dir = parent
	}
}

// installMCPServerFromConfig installs an MCP server from a bootstrap.MCPServerConfig
// to ~/.copilot/mcp-config.json (Copilot CLI) and ~/.vscode/mcp.json (VS Code Copilot, if exists)
func installMCPServerFromConfig(homeDir string, config *bootstrap.MCPServerConfig) error {
	log := logger.Get()

	serverConfig := map[string]any{
		"type":    "stdio",
		"command": config.Command,
		"args":    config.Args,
	}

	// Add env if present
	if len(config.Env) > 0 {
		envMap := make(map[string]any)
		for k, v := range config.Env {
			envMap[k] = v
		}
		serverConfig["env"] = envMap
	}

	// Always install to ~/.copilot/mcp-config.json (Copilot CLI)
	copilotDir := filepath.Join(homeDir, ".copilot")
	if err := handlers.AddCopilotCLIMCPServer(copilotDir, config.Name, serverConfig); err != nil {
		return err
	}
	log.Info("MCP server installed", "server", config.Name, "location", "~/.copilot/mcp-config.json")

	// Also install to ~/.vscode/mcp.json if .vscode/ exists (VS Code Copilot)
	vscodeDir := filepath.Join(homeDir, ".vscode")
	if stat, err := os.Stat(vscodeDir); err == nil && stat.IsDir() {
		if err := handlers.AddMCPServer(vscodeDir, config.Name, serverConfig); err != nil {
			log.Warn("failed to install MCP server to .vscode", "error", err)
		} else {
			log.Info("MCP server installed", "server", config.Name, "location", "~/.vscode/mcp.json")
		}
	}

	return nil
}

// uninstallBootstrap removes GitHub Copilot CLI infrastructure (hooks and MCP servers).
// Only uninstalls options that are present in the opts slice.
func uninstallBootstrap(opts []bootstrap.Option) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	log := logger.Get()

	// Build list of what to uninstall
	var mcpToUninstall []string
	uninstallSession := false
	uninstallAnalytics := false

	for _, opt := range opts {
		switch opt.Key {
		case bootstrap.SessionHookKey:
			uninstallSession = true
		case bootstrap.AnalyticsHookKey:
			uninstallAnalytics = true
		default:
			if opt.MCPConfig != nil {
				mcpToUninstall = append(mcpToUninstall, opt.MCPConfig.Name)
			}
		}
	}

	// Remove hooks if requested (from workspace .github/hooks/)
	if uninstallSession || uninstallAnalytics {
		repoRoot := findGitRoot()
		if repoRoot != "" {
			hookFilePath := filepath.Join(repoRoot, ".github", handlers.DirHooks, handlers.FileHooks)
			if err := uninstallCopilotHooks(hookFilePath, uninstallSession, uninstallAnalytics); err != nil {
				log.Error("failed to uninstall hooks", "error", err)
			}
		}
	}

	// Remove MCP servers
	for _, name := range mcpToUninstall {
		if err := uninstallMCPServerByName(home, name); err != nil {
			log.Error("failed to uninstall MCP server", "server", name, "error", err)
			return fmt.Errorf("failed to uninstall MCP server %s: %w", name, err)
		}
	}

	return nil
}

// uninstallCopilotHooks removes sx hooks from sx.json
func uninstallCopilotHooks(hookFilePath string, uninstallSession, uninstallAnalytics bool) error {
	log := logger.Get()

	data, err := os.ReadFile(hookFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var config CopilotHooksConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}

	modified := false

	if uninstallSession {
		original := len(config.Hooks["sessionStart"])
		config.Hooks["sessionStart"] = removeHooksWithPrefix(config.Hooks["sessionStart"], "sx install", "skills install")
		if len(config.Hooks["sessionStart"]) != original {
			modified = true
			log.Info("hook removed", "hook", "sessionStart")
		}
		if len(config.Hooks["sessionStart"]) == 0 {
			delete(config.Hooks, "sessionStart")
		}
	}

	if uninstallAnalytics {
		original := len(config.Hooks["postToolUse"])
		config.Hooks["postToolUse"] = removeHooksWithPrefix(config.Hooks["postToolUse"], "sx report-usage", "skills report-usage")
		if len(config.Hooks["postToolUse"]) != original {
			modified = true
			log.Info("hook removed", "hook", "postToolUse")
		}
		if len(config.Hooks["postToolUse"]) == 0 {
			delete(config.Hooks, "postToolUse")
		}
	}

	if !modified {
		return nil
	}

	// If no hooks remain, we could delete the file, but let's keep it with version
	data, err = json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(hookFilePath, data, 0644)
}

// hasHookWithCommand checks if a hook with the exact command already exists
func hasHookWithCommand(hooks []CopilotHookEntry, command string) bool {
	for _, hook := range hooks {
		if hook.Bash == command {
			return true
		}
	}
	return false
}

// removeHooksWithPrefix removes hooks whose bash command starts with any of the given prefixes
func removeHooksWithPrefix(hooks []CopilotHookEntry, prefixes ...string) []CopilotHookEntry {
	var filtered []CopilotHookEntry
	for _, hook := range hooks {
		shouldRemove := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(hook.Bash, prefix) {
				shouldRemove = true
				break
			}
		}
		if !shouldRemove {
			filtered = append(filtered, hook)
		}
	}
	return filtered
}

// uninstallMCPServerByName removes an MCP server by name from both
// ~/.copilot/mcp-config.json and ~/.vscode/mcp.json (if exists)
func uninstallMCPServerByName(homeDir, name string) error {
	log := logger.Get()

	// Remove from ~/.copilot/mcp-config.json (Copilot CLI)
	copilotDir := filepath.Join(homeDir, ".copilot")
	if err := handlers.RemoveCopilotCLIMCPServer(copilotDir, name); err != nil {
		return err
	}
	log.Info("MCP server uninstalled", "server", name, "location", "~/.copilot/mcp-config.json")

	// Also remove from ~/.vscode/mcp.json if it exists (VS Code Copilot)
	vscodeDir := filepath.Join(homeDir, ".vscode")
	if stat, err := os.Stat(vscodeDir); err == nil && stat.IsDir() {
		if err := handlers.RemoveMCPServer(vscodeDir, name); err != nil {
			log.Warn("failed to remove MCP server from .vscode", "error", err)
		} else {
			log.Info("MCP server uninstalled", "server", name, "location", "~/.vscode/mcp.json")
		}
	}

	return nil
}

// CopilotHooksConfig represents the structure of Copilot CLI sx.json
type CopilotHooksConfig struct {
	Version int                           `json:"version"`
	Hooks   map[string][]CopilotHookEntry `json:"hooks"`
}

// CopilotHookEntry represents a single hook entry for Copilot CLI
type CopilotHookEntry struct {
	Type       string `json:"type"`
	Bash       string `json:"bash,omitempty"`
	Powershell string `json:"powershell,omitempty"`
	Cwd        string `json:"cwd,omitempty"`
	TimeoutSec int    `json:"timeoutSec,omitempty"`
}
