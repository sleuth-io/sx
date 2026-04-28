package commands

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/manifest"
)

// TestEnv provides an isolated test environment with common setup utilities.
type TestEnv struct {
	t       *testing.T
	TempDir string // Root temp directory
	HomeDir string // Simulated home directory
	origDir string // Original working directory for cleanup
}

// NewTestEnv creates a new isolated test environment.
// It sets HOME and XDG environment variables and creates the necessary
// directories so that Claude Code, GitHub Copilot, and Gemini clients are detected.
func NewTestEnv(t *testing.T) *TestEnv {
	t.Helper()

	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	claudeDir := filepath.Join(homeDir, ".claude")
	copilotDir := filepath.Join(homeDir, ".copilot")
	geminiDir := filepath.Join(homeDir, ".gemini")
	kiroDir := filepath.Join(homeDir, ".kiro")

	// Set environment for sandboxing.
	// XDG_* only works on Linux; macOS os.UserConfigDir/UserCacheDir ignore them
	// and return ~/Library/..., so we also set the SX-specific overrides which
	// GetConfigDir/GetCacheDir check first on every platform.
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))
	t.Setenv("SX_CONFIG_DIR", filepath.Join(homeDir, ".config", "sx"))
	t.Setenv("SX_CACHE_DIR", filepath.Join(homeDir, ".cache", "sx"))

	// Create directories for Claude Code, GitHub Copilot, Gemini, and Kiro
	for _, dir := range []string{homeDir, claudeDir, copilotDir, geminiDir, kiroDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// Create settings.json so Claude Code client is detected
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("Failed to create settings.json: %v", err)
	}

	origDir, _ := os.Getwd()

	return &TestEnv{
		t:       t,
		TempDir: tempDir,
		HomeDir: homeDir,
		origDir: origDir,
	}
}

// GlobalClaudeDir returns the path to ~/.claude
func (e *TestEnv) GlobalClaudeDir() string {
	return filepath.Join(e.HomeDir, ".claude")
}

// GlobalGeminiDir returns the path to ~/.gemini
func (e *TestEnv) GlobalGeminiDir() string {
	return filepath.Join(e.HomeDir, ".gemini")
}

// Chdir changes to the specified directory and registers cleanup to restore.
func (e *TestEnv) Chdir(dir string) {
	e.t.Helper()
	if err := os.Chdir(dir); err != nil {
		e.t.Fatalf("Failed to chdir to %s: %v", dir, err)
	}
	e.t.Cleanup(func() {
		_ = os.Chdir(e.origDir)
	})
}

// MkdirAll creates a directory and all parents.
func (e *TestEnv) MkdirAll(path string) string {
	e.t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		e.t.Fatalf("Failed to create directory %s: %v", path, err)
	}
	return path
}

// WriteFile writes content to a file, creating parent directories as needed.
func (e *TestEnv) WriteFile(path, content string) {
	e.t.Helper()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		e.t.Fatalf("Failed to create directory %s: %v", dir, err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		e.t.Fatalf("Failed to write file %s: %v", path, err)
	}
}

// SetupPathVault creates a path-type vault with config pointing to it.
// Returns the vault directory path.
func (e *TestEnv) SetupPathVault() string {
	e.t.Helper()

	vaultDir := e.MkdirAll(filepath.Join(e.TempDir, "vault"))

	// Create config
	configDir := e.MkdirAll(filepath.Join(e.HomeDir, ".config", "sx"))
	configContent := fmt.Sprintf(`{"type":"path","repositoryUrl":"file://%s"}`, vaultDir)
	e.WriteFile(filepath.Join(configDir, "config.json"), configContent)

	return vaultDir
}

// AddSkillToVault adds a skill to the vault directory.
// Returns the skill source directory path.
func (e *TestEnv) AddSkillToVault(vaultDir, name, version string) string {
	e.t.Helper()

	skillDir := e.MkdirAll(filepath.Join(vaultDir, "assets", name, version))

	metadata := fmt.Sprintf(`[asset]
name = "%s"
type = "skill"
version = "%s"
description = "Test skill %s"

[skill]
readme = "README.md"
prompt-file = "SKILL.md"
`, name, version, name)

	e.WriteFile(filepath.Join(skillDir, "metadata.toml"), metadata)
	e.WriteFile(filepath.Join(skillDir, "README.md"), "# "+name)
	e.WriteFile(filepath.Join(skillDir, "SKILL.md"), "You are "+name)

	return skillDir
}

// WriteLockFile seeds the vault's asset list from legacy lockfile-style
// TOML content. It parses the content via the lockfile package and
// persists the resulting assets directly into sx.toml so subsequent
// vault reads see them — including later calls to WriteLockFile that
// rewrite the seed between sub-tests. Tests continue to write in the
// legacy lockfile shape they were originally written against.
func (e *TestEnv) WriteLockFile(vaultDir, content string) {
	e.t.Helper()
	lf, err := lockfile.Parse([]byte(content))
	if err != nil {
		e.t.Fatalf("parse seed lockfile: %v", err)
	}
	m := &manifest.Manifest{
		SchemaVersion: manifest.CurrentSchemaVersion,
		CreatedBy:     "test",
	}
	if len(lf.Assets) > 0 {
		m.Assets = make([]manifest.Asset, 0, len(lf.Assets))
		for _, a := range lf.Assets {
			dst := manifest.Asset{
				Name:         a.Name,
				Version:      a.Version,
				Type:         a.Type,
				Clients:      append([]string(nil), a.Clients...),
				Dependencies: copyDependenciesLockfileToManifest(a.Dependencies),
			}
			if a.SourceHTTP != nil {
				dst.SourceHTTP = &manifest.SourceHTTP{URL: a.SourceHTTP.URL, Size: a.SourceHTTP.Size}
				if a.SourceHTTP.Hashes != nil {
					dst.SourceHTTP.Hashes = make(map[string]string, len(a.SourceHTTP.Hashes))
					maps.Copy(dst.SourceHTTP.Hashes, a.SourceHTTP.Hashes)
				}
			}
			if a.SourcePath != nil {
				dst.SourcePath = &manifest.SourcePath{Path: a.SourcePath.Path}
			}
			if a.SourceGit != nil {
				dst.SourceGit = &manifest.SourceGit{URL: a.SourceGit.URL, Ref: a.SourceGit.Ref, Subdirectory: a.SourceGit.Subdirectory}
			}
			for _, s := range a.Scopes {
				if len(s.Paths) == 0 {
					dst.Scopes = append(dst.Scopes, manifest.Scope{Kind: manifest.ScopeKindRepo, Repo: s.Repo})
				} else {
					dst.Scopes = append(dst.Scopes, manifest.Scope{
						Kind:  manifest.ScopeKindPath,
						Repo:  s.Repo,
						Paths: append([]string(nil), s.Paths...),
					})
				}
			}
			m.Assets = append(m.Assets, dst)
		}
	}
	if err := manifest.Save(vaultDir, m); err != nil {
		e.t.Fatalf("save seeded manifest: %v", err)
	}
}

// ReadVaultAssets returns the vault's current state as a
// lockfile.LockFile. Tests that previously read $vaultDir/sx.lock and
// parsed it should call this instead — it reads sx.toml (the source of
// truth) and converts to the lockfile shape. Identity-dependent scopes
// (team, user) are projected as-is into lockfile.Scope entries so
// tests that assert on scope counts still work.
func (e *TestEnv) ReadVaultAssets(vaultDir string) (*lockfile.LockFile, bool) {
	e.t.Helper()
	m, ok, err := manifest.Load(vaultDir)
	if err != nil {
		e.t.Fatalf("read vault manifest at %s: %v", vaultDir, err)
	}
	if !ok || m == nil {
		return nil, false
	}
	lf := &lockfile.LockFile{}
	for _, a := range m.Assets {
		dst := lockfile.Asset{
			Name:         a.Name,
			Version:      a.Version,
			Type:         a.Type,
			Clients:      append([]string(nil), a.Clients...),
			Dependencies: copyDependenciesManifestToLockfile(a.Dependencies),
		}
		if a.SourceHTTP != nil {
			dst.SourceHTTP = &lockfile.SourceHTTP{URL: a.SourceHTTP.URL, Size: a.SourceHTTP.Size}
			if a.SourceHTTP.Hashes != nil {
				dst.SourceHTTP.Hashes = make(map[string]string, len(a.SourceHTTP.Hashes))
				maps.Copy(dst.SourceHTTP.Hashes, a.SourceHTTP.Hashes)
			}
		}
		if a.SourcePath != nil {
			dst.SourcePath = &lockfile.SourcePath{Path: a.SourcePath.Path}
		}
		if a.SourceGit != nil {
			dst.SourceGit = &lockfile.SourceGit{URL: a.SourceGit.URL, Ref: a.SourceGit.Ref, Subdirectory: a.SourceGit.Subdirectory}
		}
		for _, s := range a.Scopes {
			switch s.Kind {
			case manifest.ScopeKindRepo:
				dst.Scopes = append(dst.Scopes, lockfile.Scope{Repo: s.Repo})
			case manifest.ScopeKindPath:
				dst.Scopes = append(dst.Scopes, lockfile.Scope{Repo: s.Repo, Paths: append([]string(nil), s.Paths...)})
			case manifest.ScopeKindOrg, manifest.ScopeKindTeam, manifest.ScopeKindUser, manifest.ScopeKindBot:
				// Identity-dependent scopes do not translate to the
				// lockfile.Scope shape. Tests that need to assert on
				// them should read the manifest directly.
			}
		}
		lf.Assets = append(lf.Assets, dst)
	}
	return lf, true
}

// ResetVaultAssets removes the vault's manifest so the next operation
// starts from an empty state. Replaces the old pattern of
// `os.Remove(filepath.Join(vaultDir, "sx.lock"))` between subtests.
func (e *TestEnv) ResetVaultAssets(vaultDir string) {
	e.t.Helper()
	for _, name := range []string{"sx.toml", "sx.lock", "sx.lock.migrated"} {
		_ = os.Remove(filepath.Join(vaultDir, name))
	}
}

func copyDependenciesLockfileToManifest(in []lockfile.Dependency) []manifest.Dependency {
	if len(in) == 0 {
		return nil
	}
	out := make([]manifest.Dependency, len(in))
	for i, d := range in {
		out[i] = manifest.Dependency{Name: d.Name, Version: d.Version}
	}
	return out
}

func copyDependenciesManifestToLockfile(in []manifest.Dependency) []lockfile.Dependency {
	if len(in) == 0 {
		return nil
	}
	out := make([]lockfile.Dependency, len(in))
	for i, d := range in {
		out[i] = lockfile.Dependency{Name: d.Name, Version: d.Version}
	}
	return out
}

// SetupGitRepo initializes a git repo with a remote URL.
// Returns the repo directory path.
func (e *TestEnv) SetupGitRepo(name, remoteURL string) string {
	e.t.Helper()

	repoDir := e.MkdirAll(filepath.Join(e.TempDir, name))

	// git init
	e.runGit(repoDir, "init")

	// git config
	e.runGit(repoDir, "config", "user.email", "test@test.com")
	e.runGit(repoDir, "config", "user.name", "Test")

	// Create initial commit (needed for some git operations)
	e.WriteFile(filepath.Join(repoDir, ".gitkeep"), "")
	e.runGit(repoDir, "add", ".")
	e.runGit(repoDir, "commit", "-m", "init")

	// Add remote
	e.runGit(repoDir, "remote", "add", "origin", remoteURL)

	return repoDir
}

func (e *TestEnv) runGit(dir string, args ...string) {
	e.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		e.t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

// AssertFileExists fails the test if the file does not exist.
func (e *TestEnv) AssertFileExists(path string) {
	e.t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		e.t.Errorf("Expected file to exist: %s", path)
	}
}

// AssertFileNotExists fails the test if the file exists.
func (e *TestEnv) AssertFileNotExists(path string) {
	e.t.Helper()
	if _, err := os.Stat(path); err == nil {
		e.t.Errorf("Expected file to NOT exist: %s", path)
	}
}

// AddRuleToVault adds a rule asset to the vault directory.
// Returns the rule source directory path.
func (e *TestEnv) AddRuleToVault(vaultDir, name, version, content string) string {
	e.t.Helper()

	ruleDir := e.MkdirAll(filepath.Join(vaultDir, "assets", name, version))

	metadata := fmt.Sprintf(`[asset]
name = "%s"
type = "rule"
version = "%s"
description = "Test rule %s"

[rule]
title = "%s"
prompt-file = "RULE.md"
`, name, version, name, name)

	e.WriteFile(filepath.Join(ruleDir, "metadata.toml"), metadata)
	e.WriteFile(filepath.Join(ruleDir, "RULE.md"), content)

	return ruleDir
}

// AddRuleToVaultWithGlobs adds a rule asset with globs to the vault directory.
// Returns the rule source directory path.
func (e *TestEnv) AddRuleToVaultWithGlobs(vaultDir, name, version, content string, globs []string) string {
	e.t.Helper()

	ruleDir := e.MkdirAll(filepath.Join(vaultDir, "assets", name, version))

	// Build globs array string
	globsStr := ""
	if len(globs) > 0 {
		var sb strings.Builder
		sb.WriteString("globs = [")
		for i, g := range globs {
			if i > 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(&sb, "%q", g)
		}
		sb.WriteString("]\n")
		globsStr = sb.String()
	}

	metadata := fmt.Sprintf(`[asset]
name = "%s"
type = "rule"
version = "%s"
description = "Test rule %s"

[rule]
title = "%s"
prompt-file = "RULE.md"
%s`, name, version, name, name, globsStr)

	e.WriteFile(filepath.Join(ruleDir, "metadata.toml"), metadata)
	e.WriteFile(filepath.Join(ruleDir, "RULE.md"), content)

	return ruleDir
}

// AddPluginToVault adds a Claude Code plugin to the vault directory.
// Returns the plugin source directory path.
func (e *TestEnv) AddPluginToVault(vaultDir, name, version string) string {
	e.t.Helper()

	pluginDir := e.MkdirAll(filepath.Join(vaultDir, "assets", name, version))

	metadata := fmt.Sprintf(`[asset]
name = "%s"
type = "claude-code-plugin"
version = "%s"
description = "Test plugin %s"

[claude-code-plugin]
manifest-file = ".claude-plugin/plugin.json"
`, name, version, name)

	pluginJSON := fmt.Sprintf(`{
  "name": "%s",
  "description": "Test plugin %s",
  "version": "%s",
  "author": { "name": "Test Author" }
}`, name, name, version)

	e.WriteFile(filepath.Join(pluginDir, "metadata.toml"), metadata)
	e.MkdirAll(filepath.Join(pluginDir, ".claude-plugin"))
	e.WriteFile(filepath.Join(pluginDir, ".claude-plugin", "plugin.json"), pluginJSON)
	e.WriteFile(filepath.Join(pluginDir, "README.md"), "# "+name)

	return pluginDir
}

// AddCommandToVault adds a command asset to the vault directory.
// Returns the command source directory path.
func (e *TestEnv) AddCommandToVault(vaultDir, name, version, content string) string {
	e.t.Helper()

	commandDir := e.MkdirAll(filepath.Join(vaultDir, "assets", name, version))

	metadata := fmt.Sprintf(`[asset]
name = "%s"
type = "command"
version = "%s"
description = "Test command %s"

[command]
prompt-file = "COMMAND.md"
`, name, version, name)

	e.WriteFile(filepath.Join(commandDir, "metadata.toml"), metadata)
	e.WriteFile(filepath.Join(commandDir, "COMMAND.md"), content)

	return commandDir
}

// AddAgentToVault adds an agent asset to the vault directory.
// Returns the agent source directory path.
func (e *TestEnv) AddAgentToVault(vaultDir, name, version, content string) string {
	e.t.Helper()

	agentDir := e.MkdirAll(filepath.Join(vaultDir, "assets", name, version))

	metadata := fmt.Sprintf(`[asset]
name = "%s"
type = "agent"
version = "%s"
description = "Test agent %s"

[agent]
prompt-file = "AGENT.md"
`, name, version, name)

	e.WriteFile(filepath.Join(agentDir, "metadata.toml"), metadata)
	e.WriteFile(filepath.Join(agentDir, "AGENT.md"), content)

	return agentDir
}

// formatArgsArray formats a slice of strings as a TOML array string.
func formatArgsArray(args []string) string {
	var sb strings.Builder
	sb.WriteString("[")
	for i, arg := range args {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%q", arg)
	}
	sb.WriteString("]")
	return sb.String()
}

// AddMCPToVault adds an MCP server asset to the vault directory.
// Returns the MCP source directory path.
func (e *TestEnv) AddMCPToVault(vaultDir, name, version, command string, args []string) string {
	e.t.Helper()

	mcpDir := e.MkdirAll(filepath.Join(vaultDir, "assets", name, version))

	// Build args array string
	argsStr := formatArgsArray(args)

	metadata := fmt.Sprintf(`[asset]
name = "%s"
type = "mcp"
version = "%s"
description = "Test MCP server %s"

[mcp]
command = "%s"
args = %s
`, name, version, name, command, argsStr)

	e.WriteFile(filepath.Join(mcpDir, "metadata.toml"), metadata)
	// MCP servers typically have a main script or binary
	e.WriteFile(filepath.Join(mcpDir, "server.js"), "// MCP server placeholder")

	return mcpDir
}

// AddMCPRemoteToVault adds an MCP-Remote asset to the vault directory.
// MCP-Remote has no server files, just configuration for a remote server.
// Returns the MCP-Remote source directory path.
func (e *TestEnv) AddMCPRemoteToVault(vaultDir, name, version, command string, args []string) string {
	e.t.Helper()

	mcpDir := e.MkdirAll(filepath.Join(vaultDir, "assets", name, version))

	// Build args array string
	argsStr := formatArgsArray(args)

	metadata := fmt.Sprintf(`[asset]
name = "%s"
type = "mcp-remote"
version = "%s"
description = "Test MCP-Remote server %s"

[mcp]
command = "%s"
args = %s
`, name, version, name, command, argsStr)

	e.WriteFile(filepath.Join(mcpDir, "metadata.toml"), metadata)
	// MCP-Remote has no server files, just metadata

	return mcpDir
}
