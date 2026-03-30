package kiro

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/sx/internal/bootstrap"
	"github.com/sleuth-io/sx/internal/clients/kiro/handlers"
	"github.com/sleuth-io/sx/internal/logger"
)

const (
	// CLI agent config file name - use default.json so skills still work
	cliAgentFile = "default.json"
	// IDE hook file name
	ideHookReportUsage = "sx-report-usage.kiro.hook"
)

// CLIHookCommand represents a single hook command in CLI format
type CLIHookCommand struct {
	Command string `json:"command"`
	Matcher string `json:"matcher,omitempty"`
}

// CLIAgentConfig represents the Kiro CLI agent config with hooks
type CLIAgentConfig struct {
	Name        string                      `json:"name"`
	Description string                      `json:"description,omitempty"`
	Hooks       map[string][]CLIHookCommand `json:"hooks,omitempty"`
}

// IDEHookFile represents the JSON structure of a Kiro IDE .kiro.hook file
type IDEHookFile struct {
	Name        string      `json:"name"`
	Version     string      `json:"version"`
	Description string      `json:"description,omitempty"`
	When        IDEHookWhen `json:"when"`
	Then        IDEHookThen `json:"then"`
}

// IDEHookWhen specifies when the hook triggers
type IDEHookWhen struct {
	Type      string   `json:"type"`
	ToolTypes []string `json:"toolTypes,omitempty"`
}

// IDEHookThen specifies what action to take
type IDEHookThen struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// installKiroHooks installs hooks to both CLI (.kiro/agents/) and IDE (.kiro/hooks/) locations
func installKiroHooks(repoRoot string, opts []bootstrap.Option) error {
	if err := installKiroCLIHooks(repoRoot, opts); err != nil {
		return err
	}
	if err := installKiroIDEHooks(repoRoot, opts); err != nil {
		return err
	}
	return nil
}

// installKiroCLIHooks installs hooks to {repoRoot}/.kiro/agents/ for CLI
func installKiroCLIHooks(repoRoot string, opts []bootstrap.Option) error {
	agentsDir := filepath.Join(repoRoot, handlers.ConfigDir, handlers.DirAgents)
	log := logger.Get()

	// Ensure agents directory exists
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		return fmt.Errorf("failed to create agents directory: %w", err)
	}

	agentPath := filepath.Join(agentsDir, cliAgentFile)

	// Load existing config or create new one
	config := loadOrCreateAgentConfig(agentPath)

	modified := false

	// Install agentSpawn hook for auto-update (if enabled)
	if bootstrap.ContainsKey(opts, bootstrap.SessionHookKey) {
		if !hasHookCommand(config, "agentSpawn", "sx install") {
			addHook(config, "agentSpawn", CLIHookCommand{
				Command: "sx install --hook-mode --client=kiro",
			})
			log.Info("CLI hook installed", "hook", "agentSpawn", "file", cliAgentFile)
			modified = true
		}
	}

	// Install postToolUse hook for usage tracking (if enabled)
	if bootstrap.ContainsKey(opts, bootstrap.AnalyticsHookKey) {
		if !hasHookCommand(config, "postToolUse", "sx report-usage") {
			addHook(config, "postToolUse", CLIHookCommand{
				Command: "sx report-usage --client=kiro",
			})
			log.Info("CLI hook installed", "hook", "postToolUse", "file", cliAgentFile)
			modified = true
		}
	}

	if modified {
		if err := writeAgentConfig(agentPath, config); err != nil {
			return err
		}
		fmt.Printf("  Installed Kiro CLI hooks to %s\n", agentsDir)
	}

	return nil
}

// installKiroIDEHooks installs hooks to {repoRoot}/.kiro/hooks/ for IDE
func installKiroIDEHooks(repoRoot string, opts []bootstrap.Option) error {
	hooksDir := filepath.Join(repoRoot, handlers.ConfigDir, handlers.DirHooks)
	log := logger.Get()

	// Ensure hooks directory exists
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("failed to create hooks directory: %w", err)
	}

	modified := false

	// Note: IDE doesn't support a session-start hook, so we only install usage tracking

	// Install postToolUse hook for usage tracking (if enabled)
	if bootstrap.ContainsKey(opts, bootstrap.AnalyticsHookKey) {
		hookPath := filepath.Join(hooksDir, ideHookReportUsage)
		if !ideHookFileHasCommand(hookPath, "sx report-usage") {
			hook := IDEHookFile{
				Name:        "sx report-usage",
				Version:     "1.0.0",
				Description: "Track skill usage for analytics",
				When: IDEHookWhen{
					Type:      "postToolUse",
					ToolTypes: []string{"*"},
				},
				Then: IDEHookThen{
					Type:    "runCommand",
					Command: "sx report-usage --client=kiro",
				},
			}
			if err := writeIDEHookFile(hookPath, hook); err != nil {
				return err
			}
			log.Info("IDE hook installed", "hook", "postToolUse", "file", ideHookReportUsage)
			modified = true
		}
	}

	if modified {
		fmt.Printf("  Installed Kiro IDE hooks to %s\n", hooksDir)
	}

	return nil
}

// uninstallKiroHooks removes sx hooks from both CLI and IDE locations
func uninstallKiroHooks(repoRoot string, uninstallSession, uninstallAnalytics bool) error {
	if err := uninstallKiroCLIHooks(repoRoot, uninstallSession, uninstallAnalytics); err != nil {
		return err
	}
	if err := uninstallKiroIDEHooks(repoRoot, uninstallSession, uninstallAnalytics); err != nil {
		return err
	}
	return nil
}

// uninstallKiroCLIHooks removes sx hooks from {repoRoot}/.kiro/agents/
func uninstallKiroCLIHooks(repoRoot string, uninstallSession, uninstallAnalytics bool) error {
	agentsDir := filepath.Join(repoRoot, handlers.ConfigDir, handlers.DirAgents)
	agentPath := filepath.Join(agentsDir, cliAgentFile)
	log := logger.Get()

	// Load existing config
	config := loadOrCreateAgentConfig(agentPath)
	modified := false

	if uninstallSession {
		if removeHook(config, "agentSpawn", "sx install") {
			log.Info("CLI hook removed", "hook", "agentSpawn")
			modified = true
		}
	}

	if uninstallAnalytics {
		if removeHook(config, "postToolUse", "sx report-usage") {
			log.Info("CLI hook removed", "hook", "postToolUse")
			modified = true
		}
	}

	if modified {
		// Always write back the config (don't delete default.json - user may have other settings)
		if err := writeAgentConfig(agentPath, config); err != nil {
			return err
		}
	}

	return nil
}

// uninstallKiroIDEHooks removes sx hooks from {repoRoot}/.kiro/hooks/
func uninstallKiroIDEHooks(repoRoot string, uninstallSession, uninstallAnalytics bool) error {
	hooksDir := filepath.Join(repoRoot, handlers.ConfigDir, handlers.DirHooks)
	log := logger.Get()

	// Clean up legacy sx-install hook if it exists
	if uninstallSession {
		hookPath := filepath.Join(hooksDir, "sx-install.kiro.hook")
		if err := os.Remove(hookPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove sx-install.kiro.hook: %w", err)
		}
		log.Info("IDE hook removed", "file", "sx-install.kiro.hook")
	}

	if uninstallAnalytics {
		hookPath := filepath.Join(hooksDir, ideHookReportUsage)
		if err := os.Remove(hookPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove %s: %w", ideHookReportUsage, err)
		}
		log.Info("IDE hook removed", "file", ideHookReportUsage)
	}

	return nil
}

// loadOrCreateAgentConfig loads an existing agent config or creates a new one
// If the file exists, we preserve all existing fields and just add our hooks
func loadOrCreateAgentConfig(path string) *CLIAgentConfig {
	data, err := os.ReadFile(path)
	if err != nil {
		// No existing config - create minimal one
		return &CLIAgentConfig{
			Name:  "default",
			Hooks: make(map[string][]CLIHookCommand),
		}
	}

	var config CLIAgentConfig
	if err := json.Unmarshal(data, &config); err != nil {
		log := logger.Get()
		log.Warn("corrupt agent config, will recreate", "path", path, "error", err)
		return &CLIAgentConfig{
			Name:  "default",
			Hooks: make(map[string][]CLIHookCommand),
		}
	}

	if config.Hooks == nil {
		config.Hooks = make(map[string][]CLIHookCommand)
	}

	return &config
}

// writeAgentConfig writes the CLI agent config to a JSON file
func writeAgentConfig(path string, config *CLIAgentConfig) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal agent config: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write agent config %s: %w", filepath.Base(path), err)
	}
	return nil
}

// hasHookCommand checks if an agent config has a hook with the given command prefix
// The prefix must be followed by whitespace or end of string (to avoid "sx install" matching "sx install-old")
func hasHookCommand(config *CLIAgentConfig, eventType, commandPrefix string) bool {
	hooks, ok := config.Hooks[eventType]
	if !ok {
		return false
	}
	for _, h := range hooks {
		if matchesCommandPrefix(h.Command, commandPrefix) {
			return true
		}
	}
	return false
}

// matchesCommandPrefix checks if a command starts with the given prefix,
// ensuring the prefix is followed by whitespace or end of string
func matchesCommandPrefix(command, prefix string) bool {
	if !strings.HasPrefix(command, prefix) {
		return false
	}
	// Must be followed by space, tab, or end of string
	if len(command) == len(prefix) {
		return true
	}
	next := command[len(prefix)]
	return next == ' ' || next == '\t'
}

// addHook adds a hook command to the agent config
func addHook(config *CLIAgentConfig, eventType string, hook CLIHookCommand) {
	config.Hooks[eventType] = append(config.Hooks[eventType], hook)
}

// removeHook removes a hook with the given command prefix from the agent config
// Returns true if a hook was removed
// The prefix must be followed by whitespace or end of string (to avoid "sx install" matching "sx install-old")
func removeHook(config *CLIAgentConfig, eventType, commandPrefix string) bool {
	hooks, ok := config.Hooks[eventType]
	if !ok {
		return false
	}

	newHooks := make([]CLIHookCommand, 0, len(hooks))
	removed := false
	for _, h := range hooks {
		if matchesCommandPrefix(h.Command, commandPrefix) {
			removed = true
		} else {
			newHooks = append(newHooks, h)
		}
	}

	if len(newHooks) == 0 {
		delete(config.Hooks, eventType)
	} else {
		config.Hooks[eventType] = newHooks
	}

	return removed
}

// writeIDEHookFile writes an IDE .kiro.hook JSON file
func writeIDEHookFile(path string, hook IDEHookFile) error {
	data, err := json.MarshalIndent(hook, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal IDE hook file: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write IDE hook file %s: %w", filepath.Base(path), err)
	}
	return nil
}

// ideHookFileHasCommand checks if an IDE hook file exists and contains the expected command prefix
func ideHookFileHasCommand(path string, commandPrefix string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	var hook IDEHookFile
	if err := json.Unmarshal(data, &hook); err != nil {
		log := logger.Get()
		log.Warn("corrupt IDE hook file, will overwrite", "path", path, "error", err)
		return false
	}

	return strings.HasPrefix(hook.Then.Command, commandPrefix)
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
		if _, err := os.Stat(gitDir); err == nil {
			// .git can be a directory (normal repo) or a file (worktree with gitdir: pointer)
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
