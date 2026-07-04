package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/sleuth-io/sx/internal/config"
	gitpkg "github.com/sleuth-io/sx/internal/git"
	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/utils"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// Settings exposes the shared sx configuration (the same one the CLI uses)
// so the app is never a mystery: which vault, which profile, which file.

// ProfileInfo describes one configured profile for the settings view.
type ProfileInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`     // "git" | "path" | "sleuth"
	Location string `json:"location"` // URL or path, display form
	Identity string `json:"identity"`
	Default  bool   `json:"default"`
}

// Settings is the app's view of the sx configuration.
type Settings struct {
	ConfigPath string        `json:"configPath"`
	Profiles   []ProfileInfo `json:"profiles"`
}

// GetSettings returns every configured profile and where the config lives.
func (a *App) GetSettings() (Settings, error) {
	configFile, err := utils.GetConfigFile()
	if err != nil {
		return Settings{}, err
	}
	out := Settings{ConfigPath: configFile}

	mpc, err := config.LoadMultiProfile()
	if err != nil {
		// Not configured yet — settings still show where config will live.
		return out, nil
	}
	active := config.GetActiveProfileName(mpc)
	for name, p := range mpc.Profiles {
		cfg := p.ToConfig(nil, nil)
		info := ProfileInfo{
			Name:     name,
			Type:     string(cfg.Type),
			Identity: cfg.Identity,
			Default:  name == active,
		}
		switch cfg.Type {
		case config.RepositoryTypeSleuth:
			info.Location = cfg.ServerURL
		case config.RepositoryTypeGit, config.RepositoryTypePath:
			info.Location = strings.TrimPrefix(cfg.RepositoryURL, "file://")
		default:
			info.Location = strings.TrimPrefix(cfg.RepositoryURL, "file://")
		}
		out.Profiles = append(out.Profiles, info)
	}
	sort.Slice(out.Profiles, func(i, j int) bool {
		if out.Profiles[i].Default != out.Profiles[j].Default {
			return out.Profiles[i].Default
		}
		return out.Profiles[i].Name < out.Profiles[j].Name
	})
	return out, nil
}

// SwitchProfile makes the named profile the default — for the app AND the
// CLI, since they share one configuration.
func (a *App) SwitchProfile(name string) (VaultInfo, error) {
	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return VaultInfo{}, err
	}
	if _, ok := mpc.GetProfile(name); !ok {
		return VaultInfo{}, fmt.Errorf("profile %q not found", name)
	}
	mpc.DefaultProfile = name
	if err := config.SaveMultiProfile(mpc); err != nil {
		return VaultInfo{}, err
	}
	// Clear any session override so the new default takes effect now.
	config.SetActiveProfile("")
	a.resetVault()
	return a.GetVaultInfo(), nil
}

// AddLibrary connects a new library (profile) of type "path" or "git" and
// switches to it. skills.new libraries go through StartSleuthLogin instead.
func (a *App) AddLibrary(name, kind, location, identity string) (VaultInfo, error) {
	name = slugify(name)
	if name == "" {
		return VaultInfo{}, errors.New("give the library a name")
	}
	if mpc, err := config.LoadMultiProfile(); err == nil {
		if _, exists := mpc.GetProfile(name); exists {
			return VaultInfo{}, fmt.Errorf("a library named %q already exists", name)
		}
	}

	cfg := &config.Config{Identity: manifest.NormalizeEmail(identity)}
	switch kind {
	case "path":
		location = strings.TrimSpace(strings.TrimPrefix(location, "file://"))
		if location == "" {
			return VaultInfo{}, errors.New("choose a folder for the library")
		}
		if err := os.MkdirAll(location, 0755); err != nil {
			return VaultInfo{}, err
		}
		cfg.Type = config.RepositoryTypePath
		cfg.RepositoryURL = "file://" + location
	case "git":
		location = strings.TrimSpace(location)
		if location == "" {
			return VaultInfo{}, errors.New("enter the git repository URL")
		}
		cfg.Type = config.RepositoryTypeGit
		cfg.RepositoryURL = location
		v, err := vaultpkg.NewFromConfig(cfg)
		if err != nil {
			return VaultInfo{}, friendlyVaultError(err)
		}
		if _, err := v.ListAssets(a.ctx, vaultpkg.ListAssetsOptions{Limit: 1}); err != nil {
			return VaultInfo{}, friendlyVaultError(err)
		}
	default:
		return VaultInfo{}, fmt.Errorf("unsupported library type %q", kind)
	}

	if err := config.SaveToProfile(cfg, name); err != nil {
		return VaultInfo{}, err
	}
	return a.SwitchProfile(name)
}

// SleuthLoginStart carries what the user needs to authorize a skills.new
// sign-in: the code to confirm and the URL (opened automatically).
type SleuthLoginStart struct {
	VerificationURI string `json:"verificationUri"`
	UserCode        string `json:"userCode"`
	DeviceCode      string `json:"deviceCode"`
	// BrowserOpened is false when the system browser could not be opened
	// (the frontend should emphasize the manual URL + code).
	BrowserOpened bool `json:"browserOpened"`
}

// StartSleuthLogin begins the skills.new device sign-in and opens the
// browser. Follow with CompleteSleuthLogin to wait for authorization.
func (a *App) StartSleuthLogin(serverURL string) (SleuthLoginStart, error) {
	serverURL = strings.TrimSpace(serverURL)
	if serverURL == "" {
		serverURL = "https://app.skills.new"
	}
	oauthClient := config.NewOAuthClient(serverURL)
	resp, err := oauthClient.StartDeviceFlow(a.ctx)
	if err != nil {
		return SleuthLoginStart{}, friendlyVaultError(err)
	}
	browserURL := resp.VerificationURIComplete
	if browserURL == "" {
		browserURL = fmt.Sprintf("%s?user_code=%s", resp.VerificationURI, resp.UserCode)
	}
	// A failed (or refused, e.g. non-http scheme) open is fine: the
	// returned URI + code let the frontend show manual instructions.
	browserOpened := config.OpenBrowser(browserURL) == nil
	return SleuthLoginStart{
		VerificationURI: resp.VerificationURI,
		UserCode:        resp.UserCode,
		DeviceCode:      resp.DeviceCode,
		BrowserOpened:   browserOpened,
	}, nil
}

// CompleteSleuthLogin waits for the browser authorization, then saves the
// library and switches to it. The wait is abortable via CancelSleuthLogin —
// a user who changes their mind must not be stuck until the device code
// expires.
func (a *App) CompleteSleuthLogin(serverURL, deviceCode, name string) (VaultInfo, error) {
	serverURL = strings.TrimSpace(serverURL)
	if serverURL == "" {
		serverURL = "https://app.skills.new"
	}
	name = slugify(name)
	if name == "" {
		name = "skills-new"
	}

	ctx, cancel := context.WithCancel(a.ctx)
	a.loginMu.Lock()
	if a.loginCancel != nil {
		// A new attempt supersedes any previous still-polling one.
		a.loginCancel()
	}
	a.loginCancel = cancel
	a.loginMu.Unlock()
	defer func() {
		a.loginMu.Lock()
		if a.loginCancel != nil {
			a.loginCancel()
			a.loginCancel = nil
		}
		a.loginMu.Unlock()
	}()

	oauthClient := config.NewOAuthClient(serverURL)
	token, err := oauthClient.PollForToken(ctx, deviceCode)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return VaultInfo{}, errors.New("sign-in cancelled")
		}
		return VaultInfo{}, fmt.Errorf("sign-in was not completed: %w", err)
	}
	cfg := &config.Config{
		Type:          config.RepositoryTypeSleuth,
		ServerURL:     serverURL,
		RepositoryURL: serverURL,
		AuthToken:     token.AccessToken,
	}
	if err := config.SaveToProfile(cfg, name); err != nil {
		return VaultInfo{}, err
	}
	return a.SwitchProfile(name)
}

// CancelSleuthLogin aborts an in-flight skills.new sign-in wait, if any.
// The pending CompleteSleuthLogin call returns a "sign-in cancelled" error.
func (a *App) CancelSleuthLogin() {
	a.loginMu.Lock()
	defer a.loginMu.Unlock()
	if a.loginCancel != nil {
		a.loginCancel()
		a.loginCancel = nil
	}
}

// GitStatusInfo tells the frontend whether git operations can work on
// this machine at all, so git-backed choices can explain themselves
// instead of failing (or, on a Mac without developer tools, triggering
// Apple's install dialog).
type GitStatusInfo struct {
	Available bool   `json:"available"`
	Version   string `json:"version"`
	Reason    string `json:"reason"`
}

// GitStatus probes for a usable git binary without side effects.
func (a *App) GitStatus() GitStatusInfo {
	av := gitpkg.CheckAvailability(a.ctx)
	return GitStatusInfo{Available: av.Available, Version: av.Version, Reason: av.Reason}
}

// GitRepoOption is one repository the user can pick instead of typing a
// URL.
type GitRepoOption struct {
	Name string `json:"name"` // "owner/repo"
	URL  string `json:"url"`  // clone URL matching the user's git protocol
}

// ListGitRepos returns the repositories the signed-in GitHub CLI account
// can access, most recently pushed first, so git-vault forms can offer a
// searchable picker. Returns nil (not an error) when `gh` is missing or
// unauthenticated — the form falls back to manual URL entry.
func (a *App) ListGitRepos() []GitRepoOption {
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, ghPath, "api",
		"user/repos?per_page=100&sort=pushed&affiliation=owner,collaborator,organization_member",
		"--jq", `[.[] | {name: .full_name, https: .clone_url, ssh: .ssh_url}]`,
	).Output()
	if err != nil {
		return nil
	}
	var repos []struct {
		Name  string `json:"name"`
		HTTPS string `json:"https"`
		SSH   string `json:"ssh"`
	}
	if err := json.Unmarshal(out, &repos); err != nil {
		return nil
	}

	// Honor the protocol gh is configured to clone with — that's the one
	// the user's credentials are set up for. The setting is per-host
	// (github.com) with a global fallback.
	useSSH := false
	if proto, err := exec.CommandContext(ctx, ghPath, "config", "get", "-h", "github.com", "git_protocol").Output(); err == nil && strings.TrimSpace(string(proto)) != "" {
		useSSH = strings.TrimSpace(string(proto)) == "ssh"
	} else if proto, err := exec.CommandContext(ctx, ghPath, "config", "get", "git_protocol").Output(); err == nil {
		useSSH = strings.TrimSpace(string(proto)) == "ssh"
	}

	options := make([]GitRepoOption, 0, len(repos))
	for _, r := range repos {
		url := r.HTTPS
		if useSSH {
			url = r.SSH
		}
		options = append(options, GitRepoOption{Name: r.Name, URL: url})
	}
	return options
}

// PickDirectory opens the native folder picker (for new local libraries).
func (a *App) PickDirectory() (string, error) {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose a folder for the library",
	})
	if err != nil {
		return "", err
	}
	return dir, nil
}

// PickFilesForDraft opens the native file picker and turns the selection
// into a draft — the click-driven twin of drag-and-drop.
func (a *App) PickFilesForDraft() (Draft, error) {
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Add to your library",
		Filters: []runtime.FileFilter{
			{DisplayName: "Assets (*.md, *.zip)", Pattern: "*.md;*.zip;*.markdown"},
			{DisplayName: "All files", Pattern: "*"},
		},
	})
	if err != nil {
		return Draft{}, err
	}
	if len(paths) == 0 {
		return Draft{}, errCancelled
	}
	return a.CreateDraftFromPaths(paths)
}

// PickFolderForDraft opens the native directory picker for multi-file assets.
func (a *App) PickFolderForDraft() (Draft, error) {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Add a folder to your library",
	})
	if err != nil {
		return Draft{}, err
	}
	if dir == "" {
		return Draft{}, errCancelled
	}
	return a.CreateDraftFromPaths([]string{dir})
}
