package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/sleuth-io/sx/internal/cache"
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
	// Default: the library the app is viewing and writes go to.
	Default bool `json:"default"`
	// Active: part of the active set — Sync installs this library's assets
	// too. More than one library can be active at a time (sx multi-profile).
	Active bool `json:"active"`
	// TrackRepos: repository views are enabled for this library.
	TrackRepos bool `json:"trackRepos"`
	// Icon is the library's uploaded icon as a data URL ("" = none).
	Icon string `json:"icon"`
}

// Settings is the app's view of the sx configuration.
type Settings struct {
	Profiles []ProfileInfo `json:"profiles"`
}

// GetSettings returns every configured profile.
func (a *App) GetSettings() (Settings, error) {
	out := Settings{}

	mpc, err := config.LoadMultiProfile()
	if err != nil {
		// Not configured yet — nothing to list.
		return out, nil
	}
	active := config.GetActiveProfileName(mpc)
	for name, p := range mpc.Profiles {
		cfg := p.ToConfig(nil, nil)
		info := ProfileInfo{
			Name:       name,
			Type:       string(cfg.Type),
			Identity:   cfg.Identity,
			Default:    name == active,
			Active:     mpc.IsProfileActive(name) || name == active,
			TrackRepos: cfg.TrackRepos,
			Icon:       libraryIconDataURL(name),
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
	// SetDefaultProfile keeps the invariant that the default library is in
	// the active set; other active libraries stay active (multi-vault sync).
	if err := mpc.SetDefaultProfile(name); err != nil {
		return VaultInfo{}, err
	}
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
	a.loginGen++
	gen := a.loginGen
	a.loginCancel = cancel
	a.loginMu.Unlock()
	defer func() {
		a.loginMu.Lock()
		// Only clean up our own registration — if a newer attempt has
		// taken over loginCancel, cancelling it would abort a live sign-in.
		if a.loginGen == gen && a.loginCancel != nil {
			a.loginCancel()
			a.loginCancel = nil
		}
		a.loginMu.Unlock()
		cancel()
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
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()

	// Page through recently-pushed-first results, bounded so an account
	// with thousands of repos can't stall the form; the field still
	// accepts a pasted URL for anything beyond the cap.
	type repoRow struct {
		Name  string `json:"name"`
		HTTPS string `json:"https"`
		SSH   string `json:"ssh"`
	}
	var repos []repoRow
	const perPage, maxPages = 100, 5
	for page := 1; page <= maxPages; page++ {
		out, err := exec.CommandContext(ctx, ghPath, "api",
			fmt.Sprintf("user/repos?per_page=%d&page=%d&sort=pushed&affiliation=owner,collaborator,organization_member", perPage, page),
			"--jq", `[.[] | {name: .full_name, https: .clone_url, ssh: .ssh_url}]`,
		).Output()
		if err != nil {
			if page == 1 {
				return nil
			}
			break // keep what we have
		}
		var batch []repoRow
		if err := json.Unmarshal(out, &batch); err != nil {
			return nil
		}
		repos = append(repos, batch...)
		if len(batch) < perPage {
			break
		}
	}

	useSSH := ghPrefersSSH(ctx, ghPath)

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

// ghPrefersSSH reports the protocol gh is configured to clone with — that's
// the one the user's credentials are set up for. The setting is per-host
// (github.com) with a global fallback.
func ghPrefersSSH(ctx context.Context, ghPath string) bool {
	if proto, err := exec.CommandContext(ctx, ghPath, "config", "get", "-h", "github.com", "git_protocol").Output(); err == nil && strings.TrimSpace(string(proto)) != "" {
		return strings.TrimSpace(string(proto)) == "ssh"
	}
	if proto, err := exec.CommandContext(ctx, ghPath, "config", "get", "git_protocol").Output(); err == nil {
		return strings.TrimSpace(string(proto)) == "ssh"
	}
	return false
}

// GitHubAccount returns the signed-in GitHub CLI login, or "" when gh is
// missing or unauthenticated. Lets git-vault forms offer to create a
// repository under the user's account.
func (a *App) GitHubAccount() string {
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, ghPath, "api", "user", "--jq", ".login").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// CreateGitRepo creates a new private GitHub repository under the signed-in
// account and returns it as a pickable option. auto_init gives the repo an
// initial commit, which a git vault needs before it can push.
func (a *App) CreateGitRepo(repoName string) (GitRepoOption, error) {
	repoName = strings.TrimSpace(repoName)
	if !gitRepoNamePattern.MatchString(repoName) {
		return GitRepoOption{}, errors.New("repository names can only contain letters, numbers, dots, dashes and underscores")
	}
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		return GitRepoOption{}, errors.New("the GitHub CLI (gh) is required to create repositories")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, ghPath, "api", "user/repos",
		"-f", "name="+repoName,
		"-F", "private=true",
		"-F", "auto_init=true",
	).Output()
	if err != nil {
		return GitRepoOption{}, fmt.Errorf("couldn't create the repository: %s", ghErrorMessage(out, err))
	}
	var created struct {
		FullName string `json:"full_name"`
		HTTPS    string `json:"clone_url"`
		SSH      string `json:"ssh_url"`
	}
	if err := json.Unmarshal(out, &created); err != nil {
		return GitRepoOption{}, fmt.Errorf("unexpected GitHub response: %w", err)
	}
	url := created.HTTPS
	if ghPrefersSSH(ctx, ghPath) {
		url = created.SSH
	}
	return GitRepoOption{Name: created.FullName, URL: url}, nil
}

// AvailableRepoName returns the first of base, base-2 … base-9 not already
// taken on the signed-in GitHub account, so create-a-repo offers never
// promise a name that is guaranteed to collide (e.g. a second machine
// re-running onboarding). Falls back to base when gh can't answer.
func (a *App) AvailableRepoName(base string) string {
	if !gitRepoNamePattern.MatchString(base) {
		return base
	}
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		return base
	}
	login := a.GitHubAccount()
	if login == "" {
		return base
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	for i := 1; i <= 9; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		// A failing lookup means "not found" (free) — or a network error,
		// in which case creating will surface the real problem anyway.
		if exec.CommandContext(ctx, ghPath, "api", "repos/"+login+"/"+name, "--silent").Run() != nil {
			return name
		}
	}
	return base
}

var gitRepoNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)

// githubRepoPattern extracts owner/repo from the two URL shapes gh hands
// out (and users paste): https://github.com/o/r(.git) and git@github.com:o/r(.git).
var githubRepoPattern = regexp.MustCompile(`^(?:https://github\.com/|git@github\.com:)([\w.-]+)/([\w.-]+?)(?:\.git)?/?$`)

// ghErrorMessage digs the API error message out of a failed `gh api` call.
// gh writes "gh: <summary> (HTTP nnn)" to stderr and the raw JSON error
// body to STDOUT; the body's errors[].message carries the specific reason
// ("name already exists on this account") — prefer that.
func ghErrorMessage(stdout []byte, err error) string {
	var body struct {
		Message string `json:"message"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(stdout, &body) == nil {
		for _, e := range body.Errors {
			if e.Message != "" {
				return e.Message
			}
		}
		if body.Message != "" {
			return body.Message
		}
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		summary, _, _ := strings.Cut(strings.TrimSpace(string(exitErr.Stderr)), "\n")
		return strings.TrimPrefix(summary, "gh: ")
	}
	return err.Error()
}

// LibraryRemoval describes what removing a library would do, so the
// confirmation dialog offers exactly what's possible — and nothing that
// isn't.
type LibraryRemoval struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Location string `json:"location"`
	// LastLibrary blocks removal entirely (same rule as `sx profile remove`).
	LastLibrary bool `json:"lastLibrary"`
	// Active: removing this library switches the app to another one.
	Active bool `json:"active"`
	// CanDeleteSource: the underlying storage can also be deleted from here
	// (a local folder, or a GitHub repository the user administers).
	CanDeleteSource bool   `json:"canDeleteSource"`
	SourceLabel     string `json:"sourceLabel"`
	// SharedSource: deleting the source affects other people too — a GitHub
	// repository, or a folder inside a sync service (SourceProvider says
	// which) where the deletion propagates to every teammate.
	SharedSource   bool   `json:"sharedSource"`
	SourceProvider string `json:"sourceProvider"`
}

// DescribeLibraryRemoval reports what RemoveLibrary(name) would be able to
// do, including whether the underlying source is deletable from this app.
func (a *App) DescribeLibraryRemoval(name string) (LibraryRemoval, error) {
	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return LibraryRemoval{}, err
	}
	profile, ok := mpc.GetProfile(name)
	if !ok {
		return LibraryRemoval{}, fmt.Errorf("library %q not found", name)
	}
	cfg := profile.ToConfig(nil, nil)

	out := LibraryRemoval{
		Name:        name,
		Type:        string(cfg.Type),
		LastLibrary: len(mpc.Profiles) <= 1,
		Active:      name == config.GetActiveProfileName(mpc),
	}
	switch cfg.Type {
	case config.RepositoryTypeSleuth:
		out.Location = cfg.ServerURL
	case config.RepositoryTypePath:
		out.Location = strings.TrimPrefix(cfg.RepositoryURL, "file://")
		if info, err := os.Stat(out.Location); err == nil && info.IsDir() {
			out.CanDeleteSource = true
			out.SourceLabel = out.Location
			// A folder inside Dropbox/Drive/OneDrive/iCloud is a shared
			// library: deleting it here deletes it for every teammate the
			// sync service shares it with.
			if provider := utils.ProviderForPath(out.Location, utils.DetectSyncFolders()); provider != "" {
				out.SharedSource = true
				out.SourceProvider = provider
			}
		}
	case config.RepositoryTypeGit:
		out.Location = cfg.RepositoryURL
		if m := githubRepoPattern.FindStringSubmatch(cfg.RepositoryURL); m != nil {
			if ghPath, err := exec.LookPath("gh"); err == nil {
				ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
				defer cancel()
				// Only offer deletion the user can actually perform: repo
				// admins. (The delete itself may still need the delete_repo
				// scope; RemoveLibrary explains that if it comes up.)
				admin, err := exec.CommandContext(ctx, ghPath, "api",
					"repos/"+m[1]+"/"+m[2], "--jq", ".permissions.admin").Output()
				if err == nil && strings.TrimSpace(string(admin)) == "true" {
					out.CanDeleteSource = true
					out.SourceLabel = m[1] + "/" + m[2] + " on GitHub"
					out.SharedSource = true
					out.SourceProvider = "GitHub"
				}
			}
		}
	}
	return out, nil
}

// RemoveLibrary disconnects a library from the shared sx configuration —
// the same operation as `sx profile remove`, including its refusal to
// remove the last one. With deleteSource, the underlying storage goes too:
// the vault folder for path libraries, the GitHub repository for git ones.
// Source deletion happens first — if it fails, the library stays configured
// so the user can retry.
func (a *App) RemoveLibrary(name string, deleteSource bool) (VaultInfo, error) {
	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return VaultInfo{}, err
	}
	profile, ok := mpc.GetProfile(name)
	if !ok {
		return VaultInfo{}, fmt.Errorf("library %q not found", name)
	}
	if len(mpc.Profiles) <= 1 {
		return VaultInfo{}, errors.New("can't remove the last library — add another one first")
	}
	cfg := profile.ToConfig(nil, nil)

	if deleteSource {
		switch cfg.Type {
		case config.RepositoryTypePath:
			if err := deleteVaultFolder(strings.TrimPrefix(cfg.RepositoryURL, "file://")); err != nil {
				return VaultInfo{}, err
			}
		case config.RepositoryTypeGit:
			if err := a.deleteGitHubRepo(cfg.RepositoryURL); err != nil {
				return VaultInfo{}, err
			}
		case config.RepositoryTypeSleuth:
			// Server-side data; never deletable from here.
			return VaultInfo{}, errors.New("this library type has no deletable source")
		default:
			return VaultInfo{}, errors.New("this library type has no deletable source")
		}
	}

	// A git library leaves a working clone in the cache; clear it either way
	// so a future re-add starts fresh.
	if cfg.Type == config.RepositoryTypeGit {
		if clonePath, err := cache.GetGitRepoCachePath(cfg.RepositoryURL); err == nil {
			_ = os.RemoveAll(clonePath)
		}
	}

	if err := mpc.DeleteProfile(name); err != nil {
		return VaultInfo{}, err
	}
	if err := config.SaveMultiProfile(mpc); err != nil {
		return VaultInfo{}, err
	}
	// The icon belongs to the library; no library, no icon.
	removeIconFiles(name)
	// Clear any session override and re-resolve — the default may have moved.
	config.SetActiveProfile("")
	a.resetVault()
	return a.GetVaultInfo(), nil
}

// SetLibraryActive adds or removes a library from the active set — the
// libraries Sync (and `sx install`) merge assets from. The current library
// is always active; the last active library can't be deactivated. Mirrors
// `sx profile activate` / `sx profile deactivate`.
func (a *App) SetLibraryActive(name string, active bool) error {
	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return err
	}
	if _, ok := mpc.GetProfile(name); !ok {
		return fmt.Errorf("library %q not found", name)
	}
	if active {
		if err := mpc.Activate(name); err != nil {
			return err
		}
	} else {
		if name == config.GetActiveProfileName(mpc) {
			return errors.New("the current library is always synced — switch to another library first")
		}
		if err := mpc.Deactivate(name); err != nil {
			return err
		}
	}
	return config.SaveMultiProfile(mpc)
}

// deleteVaultFolder removes a path library's folder, but only when it looks
// like one: empty, or containing an sx.toml manifest. A mispointed config
// must never be able to erase somebody's documents folder.
func deleteVaultFolder(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" || dir == "/" || !filepath.IsAbs(dir) {
		return fmt.Errorf("refusing to delete %q", dir)
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil // already gone
	}
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		if _, err := os.Stat(filepath.Join(dir, "sx.toml")); err != nil {
			return fmt.Errorf("%s doesn't look like a library folder (no sx.toml) — not deleting it", dir)
		}
	}
	return os.RemoveAll(dir)
}

// deleteGitHubRepo deletes the repository behind a git library via the
// GitHub CLI. Deletion needs the delete_repo scope, which gh doesn't hold
// by default — the error explains how to grant it.
func (a *App) deleteGitHubRepo(repoURL string) error {
	m := githubRepoPattern.FindStringSubmatch(repoURL)
	if m == nil {
		return errors.New("only GitHub repositories can be deleted from here")
	}
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		return errors.New("the GitHub CLI (gh) is required to delete repositories")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, ghPath, "api", "-X", "DELETE", "repos/"+m[1]+"/"+m[2]).Output(); err != nil {
		msg := ghErrorMessage(out, err)
		if strings.Contains(msg, "delete_repo") || strings.Contains(msg, "403") {
			return fmt.Errorf("GitHub refused to delete %s/%s: your gh token lacks the delete_repo scope. Run 'gh auth refresh -h github.com -s delete_repo', then try again", m[1], m[2])
		}
		return fmt.Errorf("couldn't delete %s/%s on GitHub: %s", m[1], m[2], msg)
	}
	return nil
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
