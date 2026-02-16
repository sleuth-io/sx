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

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/assets"
	"github.com/sleuth-io/sx/internal/buildinfo"
	"github.com/sleuth-io/sx/internal/cache"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/gitutil"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/scope"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/utils"
)

// ConfigOutput represents the full config output for JSON serialization
type ConfigOutput struct {
	Version      VersionInfo   `json:"version"`
	Platform     PlatformInfo  `json:"platform"`
	Config       ConfigInfo    `json:"config"`
	Directories  DirectoryInfo `json:"directories"`
	Clients      []ClientInfo  `json:"clients"`
	CurrentScope *scope.Scope  `json:"currentScope,omitempty"`
	Assets       []ScopeAssets `json:"assets"`
	RecentLogs   []string      `json:"recentLogs"`
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
	Profile       string `json:"profile,omitempty"`
	Type          string `json:"type,omitempty"`
	RepositoryURL string `json:"repositoryUrl,omitempty"`
	ServerURL     string `json:"serverUrl,omitempty"`
}

type DirectoryInfo struct {
	Config         string `json:"config"`
	Cache          string `json:"cache"`
	Assets         string `json:"assets"`
	GitRepos       string `json:"gitRepos"`
	LockFiles      string `json:"lockFiles"`
	InstalledState string `json:"installedState"`
	LogFile        string `json:"logFile"`
}

type ClientInfo struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Installed            bool     `json:"installed"`
	Enabled              bool     `json:"enabled"`
	ExplicitlyConfigured bool     `json:"explicitlyConfigured"` // true if enabledClients is set in config
	Version              string   `json:"version,omitempty"`
	Directory            string   `json:"directory"`
	HooksInstalled       bool     `json:"hooksInstalled"`
	Supports             []string `json:"supports"`
}

type ScopeAssets struct {
	Scope           string      `json:"scope"`
	TrackerPath     string      `json:"trackerPath"`
	LockFileVersion string      `json:"lockFileVersion,omitempty"`
	InstalledAt     string      `json:"installedAt,omitempty"`
	Assets          []AssetInfo `json:"assets"`
}

// AssetStatus represents the installation status of an asset
type AssetStatus string

const (
	StatusInstalled    AssetStatus = "installed"     // Installed and matches lock file
	StatusOutdated     AssetStatus = "outdated"      // Installed but different version
	StatusNotInstalled AssetStatus = "not_installed" // In lock file but not installed
	StatusOrphaned     AssetStatus = "orphaned"      // Installed but not in lock file
)

type AssetInfo struct {
	Name             string      `json:"name"`
	Version          string      `json:"version"`
	InstalledVersion string      `json:"installedVersion,omitempty"` // If different from Version
	Type             string      `json:"type"`
	Clients          []string    `json:"clients"`
	Status           AssetStatus `json:"status"`
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

	// Unified asset list with status
	output.Assets = gatherUnifiedAssets(currentScope, showAll)

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

	if mpc, err := config.LoadMultiProfile(); err == nil {
		info.Profile = config.GetActiveProfileName(mpc)
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
	assetsDir, _ := cache.GetAssetCacheDir()
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
		Assets:         assetsDir,
		GitRepos:       gitReposDir,
		LockFiles:      lockFilesDir,
		InstalledState: trackerDir,
		LogFile:        logFile,
	}
}

func gatherClientInfo() []ClientInfo {
	var clientInfos []ClientInfo

	// Load config to check enabled clients
	cfg, _ := config.Load()
	explicitlyConfigured := cfg != nil && len(cfg.EnabledClients) > 0

	allClients := clients.Global().GetAll()
	for _, client := range allClients {
		installed := client.IsInstalled()
		// Only call GetVersion() if installed. Some CLIs (e.g., `copilot version`)
		// create config directories as a side effect, which we want to avoid.
		var version string
		if installed {
			version = strings.TrimSpace(client.GetVersion())
		}
		info := ClientInfo{
			ID:                   client.ID(),
			Name:                 client.DisplayName(),
			Installed:            installed,
			Enabled:              cfg == nil || cfg.IsClientEnabled(client.ID()),
			ExplicitlyConfigured: explicitlyConfigured,
			Version:              version,
			Directory:            getClientDirectory(client.ID()),
			Supports:             getClientSupportedTypes(client),
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
	case "github-copilot":
		return filepath.Join(home, ".copilot")
	default:
		return ""
	}
}

func getClientSupportedTypes(client clients.Client) []string {
	var supported []string
	for _, t := range asset.AllTypes() {
		if client.SupportsAssetType(t) {
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

// groupAssetsByScope groups lock file assets by scope (repo URL or "Global")
// An asset may appear in multiple scopes if it has multiple repositories
func groupAssetsByScope(lf *lockfile.LockFile, currentScope *scope.Scope, showAll bool) map[string][]*lockfile.Asset {
	var matcher *scope.Matcher
	if currentScope != nil {
		matcher = scope.NewMatcher(currentScope)
	}

	grouped := make(map[string][]*lockfile.Asset)
	for i := range lf.Assets {
		asset := &lf.Assets[i]

		// Filter by scope if not showing all
		if !showAll && matcher != nil && !matcher.MatchesAsset(asset) {
			continue
		}

		if asset.IsGlobal() {
			grouped["Global"] = append(grouped["Global"], asset)
		} else {
			// Add to each repository scope
			for _, repo := range asset.Scopes {
				grouped[repo.Repo] = append(grouped[repo.Repo], asset)
			}
		}
	}
	return grouped
}

// getLatestVersion finds the latest version for a given asset name in a list
func getLatestVersion(assets []*lockfile.Asset) *lockfile.Asset {
	var latest *lockfile.Asset
	for _, asset := range assets {
		if latest == nil || asset.Version > latest.Version {
			latest = asset
		}
	}
	return latest
}

// determineAssetStatus determines the installation status of an asset
func determineAssetStatus(asset *lockfile.Asset, scopeName string, tracker *assets.Tracker) (AssetStatus, string, []string) {
	if tracker == nil {
		return StatusNotInstalled, "", asset.Clients
	}

	var installed *assets.InstalledAsset
	if asset.IsGlobal() {
		// Global assets: check with empty repo/path
		installed = tracker.FindAssetWithMatcher(asset.Name, "", "", scope.MatchRepoURLs)
	} else {
		// Scoped assets: check using the asset's own repo scope
		// For the current scope we're displaying, find the matching repo entry
		for _, repo := range asset.Scopes {
			if scope.MatchRepoURLs(repo.Repo, scopeName) {
				// Check repo-scoped installation
				installed = tracker.FindAssetWithMatcher(asset.Name, repo.Repo, "", scope.MatchRepoURLs)
				if installed != nil {
					break
				}
				// Also check path-scoped installations
				for _, path := range repo.Paths {
					installed = tracker.FindAssetWithMatcher(asset.Name, repo.Repo, path, scope.MatchRepoURLs)
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
		if installed.Version == asset.Version {
			return StatusInstalled, "", installed.Clients
		}
		return StatusOutdated, installed.Version, installed.Clients
	}
	return StatusNotInstalled, "", asset.Clients
}

// gatherUnifiedAssets builds a unified list of assets from the lock file with installation status
func gatherUnifiedAssets(currentScope *scope.Scope, showAll bool) []ScopeAssets {
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

	// Group assets by scope
	grouped := groupAssetsByScope(lf, currentScope, showAll)

	// Build result with installation status
	var scopes []ScopeAssets
	for scopeName, scopeAssets := range grouped {
		s := ScopeAssets{
			Scope:  scopeName,
			Assets: []AssetInfo{},
		}

		// Group by asset name to find latest version per asset
		byName := make(map[string][]*lockfile.Asset)
		for _, asset := range scopeAssets {
			byName[asset.Name] = append(byName[asset.Name], asset)
		}

		// Process each asset (using latest version only)
		for _, versions := range byName {
			latest := getLatestVersion(versions)
			if latest == nil {
				continue
			}

			status, installedVersion, clients := determineAssetStatus(latest, scopeName, tracker)

			info := AssetInfo{
				Name:             latest.Name,
				Version:          latest.Version,
				Type:             latest.Type.Key,
				Status:           status,
				Clients:          clients,
				InstalledVersion: installedVersion,
			}

			s.Assets = append(s.Assets, info)
		}

		scopes = append(scopes, s)
	}

	// Also check for orphaned assets (installed but not in lock file)
	if tracker != nil {
		// Get installed assets for current scope
		var installedAssets []assets.InstalledAsset
		if showAll || currentScope == nil {
			installedAssets = tracker.Assets
		} else {
			installedAssets = tracker.FindForScope(currentScope.RepoURL, currentScope.RepoPath, scope.MatchRepoURLs)
		}

		for _, installed := range installedAssets {
			// Check if this asset is in the lock file
			found := false
			for i := range lf.Assets {
				if lf.Assets[i].Name == installed.Name {
					found = true
					break
				}
			}

			if !found {
				// Orphaned asset - add to appropriate scope
				scopeName := installed.ScopeDescription()

				// Find or create the scope
				var targetScope *ScopeAssets
				for i := range scopes {
					if scopes[i].Scope == scopeName {
						targetScope = &scopes[i]
						break
					}
				}
				if targetScope == nil {
					scopes = append(scopes, ScopeAssets{
						Scope:  scopeName,
						Assets: []AssetInfo{},
					})
					targetScope = &scopes[len(scopes)-1]
				}

				targetScope.Assets = append(targetScope.Assets, AssetInfo{
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
	out := ui.NewOutput(os.Stdout, os.Stderr)

	out.Header("sx Configuration")
	out.Newline()

	// Version
	out.Printf("Version: %s\n", out.EmphasisText(fmt.Sprintf("%s (commit: %s, built: %s)", output.Version.Version, output.Version.Commit, output.Version.Date)))
	out.Printf("Platform: %s\n", out.MutedText(fmt.Sprintf("%s/%s", output.Platform.OS, output.Platform.Arch)))
	out.Printf("Working Directory: %s\n", out.MutedText(output.Platform.WorkingDir))
	out.Newline()

	// Configuration
	out.Section("Configuration")
	if output.Config.Exists {
		out.Printf("Config File: %s %s\n", out.MutedText(output.Config.Path), out.SuccessText("(exists)"))
	} else {
		out.Printf("Config File: %s %s\n", out.MutedText(output.Config.Path), out.ErrorText("(not found)"))
	}
	if output.Config.Profile != "" {
		out.KeyValue("Profile", output.Config.Profile)
	}
	if output.Config.Type != "" {
		out.KeyValue("Type", output.Config.Type)
	}
	if output.Config.RepositoryURL != "" {
		out.KeyValue("Repository URL", output.Config.RepositoryURL)
	}
	if output.Config.ServerURL != "" {
		out.KeyValue("Server URL", output.Config.ServerURL)
	}
	out.Newline()

	// Directories
	out.Section("Directories")
	out.KeyValue("Config", output.Directories.Config)
	out.Printf("Cache: %s\n", out.MutedText(output.Directories.Cache))
	out.Muted("  └─ assets/")
	out.Muted("  └─ git-repos/")
	out.Muted("  └─ lockfiles/")
	out.Muted("  └─ installed-state/")
	out.KeyValue("Log File", output.Directories.LogFile)
	out.Newline()

	// Clients (using shared styled output)
	out.Section("Detected Clients")
	PrintClientsSection(out, output.Clients)

	// Recent logs
	if len(output.RecentLogs) > 0 {
		out.Section("Recent Logs (last 5 lines)")
		for _, line := range output.RecentLogs {
			out.Muted(line)
		}
		out.Newline()
	}

	// Assets with status
	out.Section("Assets")
	if len(output.Assets) > 0 {
		for _, s := range output.Assets {
			out.Bold(s.Scope + ":")
			for _, asset := range s.Assets {
				clientsStr := ""
				if len(asset.Clients) > 0 {
					clientsStr = " → " + out.MutedText(strings.Join(asset.Clients, ", "))
				}

				// Format status indicator with color
				var statusStr string
				switch asset.Status {
				case StatusInstalled:
					statusStr = out.SuccessText(" (installed)")
				case StatusOutdated:
					statusStr = out.ErrorText(fmt.Sprintf(" (outdated: %s)", asset.InstalledVersion))
				case StatusNotInstalled:
					statusStr = out.MutedText(" (not installed)")
				case StatusOrphaned:
					statusStr = out.ErrorText(" (removed from lock file)")
				}

				out.Printf("  - %s %s [%s]%s%s\n",
					out.BoldText(asset.Name),
					out.MutedText("("+asset.Version+")"),
					out.EmphasisText(asset.Type),
					statusStr,
					clientsStr)
			}
			out.Newline()
		}
	} else {
		out.Muted("No assets found.")
		out.Newline()
	}

	return nil
}
