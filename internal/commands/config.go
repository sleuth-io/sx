package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/skills/internal/asset"
	"github.com/sleuth-io/skills/internal/assets"
	"github.com/sleuth-io/skills/internal/buildinfo"
	"github.com/sleuth-io/skills/internal/cache"
	"github.com/sleuth-io/skills/internal/clients"
	"github.com/sleuth-io/skills/internal/config"
	"github.com/sleuth-io/skills/internal/gitutil"
	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/scope"
	"github.com/sleuth-io/skills/internal/utils"
)

// ConfigOutput represents the full config output for JSON serialization
type ConfigOutput struct {
	Version      VersionInfo      `json:"version"`
	Platform     PlatformInfo     `json:"platform"`
	Config       ConfigInfo       `json:"config"`
	Directories  DirectoryInfo    `json:"directories"`
	Clients      []ClientInfo     `json:"clients"`
	CurrentScope *scope.Scope     `json:"currentScope,omitempty"`
	Artifacts    []ScopeArtifacts `json:"artifacts"`
	RecentLogs   []string         `json:"recentLogs"`
}

type VersionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

type PlatformInfo struct {
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	WorkingDir string `json:"workingDir"`
}

type ConfigInfo struct {
	Path          string `json:"path"`
	Exists        bool   `json:"exists"`
	Type          string `json:"type,omitempty"`
	RepositoryURL string `json:"repositoryUrl,omitempty"`
	ServerURL     string `json:"serverUrl,omitempty"`
}

type DirectoryInfo struct {
	Config         string `json:"config"`
	Cache          string `json:"cache"`
	Artifacts      string `json:"artifacts"`
	GitRepos       string `json:"gitRepos"`
	LockFiles      string `json:"lockFiles"`
	InstalledState string `json:"installedState"`
	LogFile        string `json:"logFile"`
}

type ClientInfo struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Installed      bool     `json:"installed"`
	Version        string   `json:"version,omitempty"`
	Directory      string   `json:"directory"`
	HooksInstalled bool     `json:"hooksInstalled"`
	Supports       []string `json:"supports"`
}

type ScopeArtifacts struct {
	Scope           string         `json:"scope"`
	TrackerPath     string         `json:"trackerPath"`
	LockFileVersion string         `json:"lockFileVersion,omitempty"`
	InstalledAt     string         `json:"installedAt,omitempty"`
	Artifacts       []ArtifactInfo `json:"artifacts"`
}

// ArtifactStatus represents the installation status of an artifact
type ArtifactStatus string

const (
	StatusInstalled    ArtifactStatus = "installed"     // Installed and matches lock file
	StatusOutdated     ArtifactStatus = "outdated"      // Installed but different version
	StatusNotInstalled ArtifactStatus = "not_installed" // In lock file but not installed
	StatusOrphaned     ArtifactStatus = "orphaned"      // Installed but not in lock file
)

type ArtifactInfo struct {
	Name             string         `json:"name"`
	Version          string         `json:"version"`
	InstalledVersion string         `json:"installedVersion,omitempty"` // If different from Version
	Type             string         `json:"type"`
	Clients          []string       `json:"clients"`
	Status           ArtifactStatus `json:"status"`
}

// NewConfigCommand creates the config command
func NewConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Display configuration and installation status",
		Long:  "Shows current configuration, detected clients, installed assets, and paths for debugging and remote support.",
		RunE:  runConfig,
	}
	cmd.Flags().Bool("json", false, "Output in JSON format")
	cmd.Flags().Bool("all", false, "Show all assets from lock file, not just those for current repo context")
	return cmd
}

func runConfig(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	showAll, _ := cmd.Flags().GetBool("all")

	output := gatherConfigInfo(showAll)

	if jsonOutput {
		return printJSON(output)
	}
	return printText(output, showAll)
}

func gatherConfigInfo(showAll bool) ConfigOutput {
	output := ConfigOutput{}

	// Version info
	output.Version = VersionInfo{
		Version: buildinfo.Version,
		Commit:  buildinfo.Commit,
		Date:    buildinfo.Date,
	}

	// Platform info
	cwd, _ := os.Getwd()
	output.Platform = PlatformInfo{
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		WorkingDir: cwd,
	}

	// Detect current scope
	var currentScope *scope.Scope
	gitContext, err := gitutil.DetectContext(context.Background())
	if err == nil && gitContext.IsRepo && gitContext.RepoURL != "" {
		if gitContext.RelativePath == "." {
			currentScope = &scope.Scope{
				Type:    scope.TypeRepo,
				RepoURL: gitContext.RepoURL,
			}
		} else {
			currentScope = &scope.Scope{
				Type:     scope.TypePath,
				RepoURL:  gitContext.RepoURL,
				RepoPath: gitContext.RelativePath,
			}
		}
		output.CurrentScope = currentScope
	}

	// Config info
	output.Config = gatherConfigDetails()

	// Directory info
	output.Directories = gatherDirectoryInfo()

	// Client info
	output.Clients = gatherClientInfo()

	// Unified artifact list with status
	output.Artifacts = gatherUnifiedArtifacts(currentScope, showAll)

	// Recent logs
	output.RecentLogs = gatherRecentLogs(5)

	return output
}

func gatherConfigDetails() ConfigInfo {
	configPath, _ := utils.GetConfigFile()
	info := ConfigInfo{
		Path:   configPath,
		Exists: utils.FileExists(configPath),
	}

	if cfg, err := config.Load(); err == nil {
		info.Type = string(cfg.Type)
		info.RepositoryURL = cfg.RepositoryURL
		if cfg.Type == config.RepositoryTypeSleuth {
			info.ServerURL = cfg.GetServerURL()
		}
	}

	return info
}

func gatherDirectoryInfo() DirectoryInfo {
	configDir, _ := utils.GetConfigDir()
	cacheDir, _ := cache.GetCacheDir()
	artifactsDir, _ := cache.GetArtifactCacheDir()
	gitReposDir, _ := cache.GetGitReposCacheDir()
	lockFilesDir, _ := cache.GetLockFileCacheDir()
	trackerDir, _ := cache.GetTrackerCacheDir()

	logFile := ""
	if cacheDir != "" {
		logFile = filepath.Join(cacheDir, "sx.log")
	}

	return DirectoryInfo{
		Config:         configDir,
		Cache:          cacheDir,
		Artifacts:      artifactsDir,
		GitRepos:       gitReposDir,
		LockFiles:      lockFilesDir,
		InstalledState: trackerDir,
		LogFile:        logFile,
	}
}

func gatherClientInfo() []ClientInfo {
	var clientInfos []ClientInfo

	allClients := clients.Global().GetAll()
	for _, client := range allClients {
		info := ClientInfo{
			ID:        client.ID(),
			Name:      client.DisplayName(),
			Installed: client.IsInstalled(),
			Version:   strings.TrimSpace(client.GetVersion()),
			Directory: getClientDirectory(client.ID()),
			Supports:  getClientSupportedTypes(client),
		}
		info.HooksInstalled = checkHooksInstalled(client.ID(), info.Directory)
		clientInfos = append(clientInfos, info)
	}

	return clientInfos
}

func getClientDirectory(clientID string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	switch clientID {
	case "claude-code":
		return filepath.Join(home, ".claude")
	case "cursor":
		return filepath.Join(home, ".cursor")
	default:
		return ""
	}
}

func getClientSupportedTypes(client clients.Client) []string {
	var supported []string
	for _, t := range asset.AllTypes() {
		if client.SupportsArtifactType(t) {
			supported = append(supported, t.Key)
		}
	}
	return supported
}

func checkHooksInstalled(clientID, clientDir string) bool {
	if clientDir == "" {
		return false
	}

	switch clientID {
	case "claude-code":
		// Check settings.json for sx hooks (or legacy skills hooks)
		settingsPath := filepath.Join(clientDir, "settings.json")
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			return false
		}
		content := string(data)
		return strings.Contains(content, "sx install") || strings.Contains(content, "skills install")

	case "cursor":
		// Check hooks.json for sx hooks (or legacy skills hooks)
		hooksPath := filepath.Join(clientDir, "hooks.json")
		data, err := os.ReadFile(hooksPath)
		if err != nil {
			return false
		}
		content := string(data)
		return strings.Contains(content, "sx install") || strings.Contains(content, "skills install")
	}

	return false
}

// groupArtifactsByScope groups lock file artifacts by scope (repo URL or "Global")
// An artifact may appear in multiple scopes if it has multiple repositories
func groupArtifactsByScope(lf *lockfile.LockFile, currentScope *scope.Scope, showAll bool) map[string][]*lockfile.Artifact {
	var matcher *scope.Matcher
	if currentScope != nil {
		matcher = scope.NewMatcher(currentScope)
	}

	grouped := make(map[string][]*lockfile.Artifact)
	for i := range lf.Artifacts {
		art := &lf.Artifacts[i]

		// Filter by scope if not showing all
		if !showAll && matcher != nil && !matcher.MatchesArtifact(art) {
			continue
		}

		if art.IsGlobal() {
			grouped["Global"] = append(grouped["Global"], art)
		} else {
			// Add to each repository scope
			for _, repo := range art.Repositories {
				grouped[repo.Repo] = append(grouped[repo.Repo], art)
			}
		}
	}
	return grouped
}

// getLatestVersion finds the latest version for a given artifact name in a list
func getLatestVersion(artifacts []*lockfile.Artifact) *lockfile.Artifact {
	var latest *lockfile.Artifact
	for _, art := range artifacts {
		if latest == nil || art.Version > latest.Version {
			latest = art
		}
	}
	return latest
}

// determineArtifactStatus determines the installation status of an artifact
func determineArtifactStatus(art *lockfile.Artifact, scopeName string, tracker *assets.Tracker) (ArtifactStatus, string, []string) {
	if tracker == nil {
		return StatusNotInstalled, "", art.Clients
	}

	var installed *assets.InstalledArtifact
	if art.IsGlobal() {
		// Global artifacts: check with empty repo/path
		installed = tracker.FindArtifactWithMatcher(art.Name, "", "", scope.MatchRepoURLs)
	} else {
		// Scoped artifacts: check using the artifact's own repo scope
		// For the current scope we're displaying, find the matching repo entry
		for _, repo := range art.Repositories {
			if scope.MatchRepoURLs(repo.Repo, scopeName) {
				// Check repo-scoped installation
				installed = tracker.FindArtifactWithMatcher(art.Name, repo.Repo, "", scope.MatchRepoURLs)
				if installed != nil {
					break
				}
				// Also check path-scoped installations
				for _, path := range repo.Paths {
					installed = tracker.FindArtifactWithMatcher(art.Name, repo.Repo, path, scope.MatchRepoURLs)
					if installed != nil {
						break
					}
				}
				if installed != nil {
					break
				}
			}
		}
	}

	if installed != nil {
		if installed.Version == art.Version {
			return StatusInstalled, "", installed.Clients
		}
		return StatusOutdated, installed.Version, installed.Clients
	}
	return StatusNotInstalled, "", art.Clients
}

// gatherUnifiedArtifacts builds a unified list of artifacts from the lock file with installation status
func gatherUnifiedArtifacts(currentScope *scope.Scope, showAll bool) []ScopeArtifacts {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}

	// Load lock file
	lockFileData, err := cache.LoadLockFile(cfg.RepositoryURL)
	if err != nil || len(lockFileData) == 0 {
		return nil
	}

	lf, err := lockfile.Parse(lockFileData)
	if err != nil {
		return nil
	}

	// Load tracker for installation status
	tracker, _ := assets.LoadTracker()

	// Group artifacts by scope
	grouped := groupArtifactsByScope(lf, currentScope, showAll)

	// Build result with installation status
	var scopes []ScopeArtifacts
	for scopeName, arts := range grouped {
		s := ScopeArtifacts{
			Scope:     scopeName,
			Artifacts: []ArtifactInfo{},
		}

		// Group by artifact name to find latest version per artifact
		byName := make(map[string][]*lockfile.Artifact)
		for _, art := range arts {
			byName[art.Name] = append(byName[art.Name], art)
		}

		// Process each artifact (using latest version only)
		for _, versions := range byName {
			latest := getLatestVersion(versions)
			if latest == nil {
				continue
			}

			status, installedVersion, clients := determineArtifactStatus(latest, scopeName, tracker)

			info := ArtifactInfo{
				Name:             latest.Name,
				Version:          latest.Version,
				Type:             latest.Type.Key,
				Status:           status,
				Clients:          clients,
				InstalledVersion: installedVersion,
			}

			s.Artifacts = append(s.Artifacts, info)
		}

		scopes = append(scopes, s)
	}

	// Also check for orphaned artifacts (installed but not in lock file)
	if tracker != nil {
		// Get installed artifacts for current scope
		var installedArts []assets.InstalledArtifact
		if showAll || currentScope == nil {
			installedArts = tracker.Artifacts
		} else {
			installedArts = tracker.FindForScope(currentScope.RepoURL, currentScope.RepoPath, scope.MatchRepoURLs)
		}

		for _, installed := range installedArts {
			// Check if this artifact is in the lock file
			found := false
			for i := range lf.Artifacts {
				if lf.Artifacts[i].Name == installed.Name {
					found = true
					break
				}
			}

			if !found {
				// Orphaned artifact - add to appropriate scope
				scopeName := installed.ScopeDescription()

				// Find or create the scope
				var targetScope *ScopeArtifacts
				for i := range scopes {
					if scopes[i].Scope == scopeName {
						targetScope = &scopes[i]
						break
					}
				}
				if targetScope == nil {
					scopes = append(scopes, ScopeArtifacts{
						Scope:     scopeName,
						Artifacts: []ArtifactInfo{},
					})
					targetScope = &scopes[len(scopes)-1]
				}

				targetScope.Artifacts = append(targetScope.Artifacts, ArtifactInfo{
					Name:    installed.Name,
					Version: installed.Version,
					Type:    installed.Type,
					Clients: installed.Clients,
					Status:  StatusOrphaned,
				})
			}
		}
	}

	return scopes
}

func gatherRecentLogs(lines int) []string {
	cacheDir, err := cache.GetCacheDir()
	if err != nil {
		return nil
	}

	logPath := filepath.Join(cacheDir, "sx.log")
	return readLastLines(logPath, lines)
}

func readLastLines(path string, n int) []string {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	var allLines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		allLines = append(allLines, scanner.Text())
	}

	if len(allLines) <= n {
		return allLines
	}
	return allLines[len(allLines)-n:]
}

func printJSON(output ConfigOutput) error {
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func printText(output ConfigOutput, showAll bool) error {
	fmt.Println("sx Configuration")
	fmt.Println("================")
	fmt.Println()

	// Version
	fmt.Printf("Version: %s (commit: %s, built: %s)\n", output.Version.Version, output.Version.Commit, output.Version.Date)
	fmt.Printf("Platform: %s/%s\n", output.Platform.OS, output.Platform.Arch)
	fmt.Printf("Working Directory: %s\n", output.Platform.WorkingDir)
	fmt.Println()

	// Configuration
	fmt.Println("Configuration")
	fmt.Println("-------------")
	existsStr := "exists"
	if !output.Config.Exists {
		existsStr = "not found"
	}
	fmt.Printf("Config File: %s (%s)\n", output.Config.Path, existsStr)
	if output.Config.Type != "" {
		fmt.Printf("Type: %s\n", output.Config.Type)
	}
	if output.Config.RepositoryURL != "" {
		fmt.Printf("Repository URL: %s\n", output.Config.RepositoryURL)
	}
	if output.Config.ServerURL != "" {
		fmt.Printf("Server URL: %s\n", output.Config.ServerURL)
	}
	fmt.Println()

	// Directories
	fmt.Println("Directories")
	fmt.Println("-----------")
	fmt.Printf("Config: %s\n", output.Directories.Config)
	fmt.Printf("Cache: %s\n", output.Directories.Cache)
	fmt.Printf("  └─ artifacts/\n")
	fmt.Printf("  └─ git-repos/\n")
	fmt.Printf("  └─ lockfiles/\n")
	fmt.Printf("  └─ installed-state/\n")
	fmt.Printf("Log File: %s\n", output.Directories.LogFile)
	fmt.Println()

	// Clients
	fmt.Println("Detected Clients")
	fmt.Println("----------------")
	for _, client := range output.Clients {
		fmt.Printf("%s:\n", client.Name)
		installedStr := "no"
		if client.Installed {
			installedStr = "yes"
		}
		fmt.Printf("  Installed: %s\n", installedStr)
		if client.Version != "" {
			fmt.Printf("  Version: %s\n", client.Version)
		}
		fmt.Printf("  Directory: %s\n", client.Directory)
		hooksStr := "no"
		if client.HooksInstalled {
			hooksStr = "yes"
		}
		fmt.Printf("  Hooks: %s\n", hooksStr)
		fmt.Printf("  Supports: %s\n", strings.Join(client.Supports, ", "))
		fmt.Println()
	}

	// Recent logs
	if len(output.RecentLogs) > 0 {
		fmt.Println("Recent Logs (last 5 lines)")
		fmt.Println("--------------------------")
		for _, line := range output.RecentLogs {
			fmt.Println(line)
		}
		fmt.Println()
	}

	// Artifacts with status
	if len(output.Artifacts) > 0 {
		fmt.Println("Artifacts")
		fmt.Println("---------")
		for _, s := range output.Artifacts {
			fmt.Printf("%s:\n", s.Scope)
			for _, art := range s.Artifacts {
				clientsStr := ""
				if len(art.Clients) > 0 {
					clientsStr = fmt.Sprintf(" → %s", strings.Join(art.Clients, ", "))
				}

				// Format status indicator
				statusStr := ""
				switch art.Status {
				case StatusInstalled:
					statusStr = " (installed)"
				case StatusOutdated:
					statusStr = fmt.Sprintf(" (outdated: %s)", art.InstalledVersion)
				case StatusNotInstalled:
					statusStr = " (not installed)"
				case StatusOrphaned:
					statusStr = " (removed from lock file)"
				}

				fmt.Printf("  - %s (%s) [%s]%s%s\n", art.Name, art.Version, art.Type, statusStr, clientsStr)
			}
			fmt.Println()
		}
	} else {
		fmt.Println("Artifacts")
		fmt.Println("---------")
		fmt.Println("No artifacts found.")
		fmt.Println()
	}

	return nil
}
