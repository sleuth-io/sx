package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/sleuth-io/sx/internal/buildinfo"
	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/version"
)

// UpdateInfo tells the frontend the outcome of the startup update check.
// Zero-valued when the check is inconclusive — the app never nags on
// network failures or dev builds.
type UpdateInfo struct {
	// Available: a newer release exists and could NOT be installed
	// automatically; the banner links to the download.
	Available bool   `json:"available"`
	Version   string `json:"version"`
	URL       string `json:"url"`
	// Installed: the update was downloaded and swapped in automatically
	// (like the CLI's self-update); it takes effect on the next launch.
	Installed bool `json:"installed"`
}

const releasesURL = "https://api.github.com/repos/sleuth-io/sx/releases/latest"

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name        string `json:"name"`
		DownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// isDevBuild mirrors the CLI's autoupdate guard: local builds (untagged,
// dirty, or ahead of a tag via git-describe) never self-update or nag.
func isDevBuild(v string) bool {
	if v == "" || v == "dev" || strings.Contains(v, "-dirty") {
		return true
	}
	// git-describe suffix: 1.8.1-31-gabcdef — ahead of the tag, not older.
	return strings.Contains(v, "-g")
}

// updateOutcome is the full result of an update check; the startup banner
// only cares about two of its states, the menu-driven check reports all.
type updateOutcome struct {
	status  string // "installed" | "available" | "uptodate" | "devbuild" | "error"
	version string
	url     string
}

func (a *App) checkForUpdate() updateOutcome {
	current := strings.TrimPrefix(buildinfo.Version, "v")
	if isDevBuild(current) {
		return updateOutcome{status: "devbuild", version: current}
	}

	release, ok := a.fetchLatestRelease()
	if !ok {
		return updateOutcome{status: "error"}
	}
	latest := strings.TrimPrefix(release.TagName, "v")
	if latest == "" || !baseVersionNewer(latest, current) {
		return updateOutcome{status: "uptodate", version: current}
	}

	if a.tryAutoUpdate(release) {
		return updateOutcome{status: "installed", version: latest}
	}
	return updateOutcome{status: "available", version: latest, url: release.HTMLURL}
}

// CheckForUpdate compares this build against the latest GitHub release and,
// like `sx` itself, applies the update automatically when it can (macOS,
// running from an installed .app bundle). When automatic install isn't
// possible it falls back to a notify-only banner.
func (a *App) CheckForUpdate() UpdateInfo {
	switch out := a.checkForUpdate(); out.status {
	case "installed":
		return UpdateInfo{Installed: true, Version: out.version}
	case "available":
		return UpdateInfo{Available: true, Version: out.version, URL: out.url}
	default:
		// Up to date, dev build, or inconclusive — never nag on startup.
		return UpdateInfo{}
	}
}

// CheckForUpdatesInteractively runs an update check from the native menu
// and reports the outcome in a dialog — a user-initiated check must always
// answer, including "you're up to date".
func (a *App) CheckForUpdatesInteractively() {
	go func() {
		out := a.checkForUpdate()
		switch out.status {
		case "installed":
			a.updateDialog("Update installed",
				fmt.Sprintf("sx was updated to version %s. It takes effect the next time you open the app.", out.version))
		case "available":
			choice, err := wailsruntime.MessageDialog(a.ctx, wailsruntime.MessageDialogOptions{
				Type:          wailsruntime.QuestionDialog,
				Title:         "Update available",
				Message:       fmt.Sprintf("Version %s is available, but couldn't be installed automatically.", out.version),
				Buttons:       []string{"Open Download Page", "Later"},
				DefaultButton: "Open Download Page",
			})
			if err == nil && choice == "Open Download Page" {
				_ = config.OpenBrowser(out.url)
			}
		case "devbuild":
			a.updateDialog("Development build",
				fmt.Sprintf("This is a development build (%s) — automatic updates are disabled.", out.version))
		case "error":
			a.updateDialog("Couldn't check for updates",
				"The update service couldn't be reached. Check your connection and try again.")
		default:
			a.updateDialog("You're up to date",
				fmt.Sprintf("sx %s is the latest version.", out.version))
		}
	}()
}

func (a *App) updateDialog(title, message string) {
	_, _ = wailsruntime.MessageDialog(a.ctx, wailsruntime.MessageDialogOptions{
		Type:    wailsruntime.InfoDialog,
		Title:   title,
		Message: message,
	})
}

// ShowAbout is the native menu's About item.
func (a *App) ShowAbout() {
	a.updateDialog("sx", "Your team's library for AI assets\n\nVersion "+buildinfo.Version)
}

func (a *App) fetchLatestRelease() (githubRelease, bool) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(a.ctx, http.MethodGet, releasesURL, nil)
	if err != nil {
		return githubRelease{}, false
	}
	req.Header.Set("User-Agent", buildinfo.GetUserAgent())
	resp, err := client.Do(req)
	if err != nil {
		return githubRelease{}, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return githubRelease{}, false
	}
	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return githubRelease{}, false
	}
	return release, true
}

// baseVersionNewer reports whether latest's release version is strictly
// newer than current's, comparing base versions only. Pre-release/describe
// suffixes are ignored: a build tagged 1.8.1 (or ahead of it) is up to
// date with release 1.8.1.
func baseVersionNewer(latest, current string) bool {
	latestVer, err := version.Parse(latest)
	if err != nil {
		return false
	}
	currentVer, err := version.Parse(current)
	if err != nil {
		return false
	}
	latestBase := version.Version{Major: latestVer.Major, Minor: latestVer.Minor, Patch: latestVer.Patch}
	currentBase := version.Version{Major: currentVer.Major, Minor: currentVer.Minor, Patch: currentVer.Patch}
	return latestBase.Compare(&currentBase) > 0
}

// tryAutoUpdate downloads the release's app artifact and swaps the
// installed .app bundle in place — the app-shaped equivalent of the CLI's
// go-selfupdate flow. Only on macOS, and only when running from a real
// bundle; any failure quietly falls back to the notify banner.
func (a *App) tryAutoUpdate(release githubRelease) bool {
	if goruntime.GOOS != "darwin" {
		return false
	}
	bundle, ok := currentAppBundle()
	if !ok {
		return false
	}
	wantPrefix := "sx-app-macos-" + goruntime.GOARCH
	var assetURL string
	for _, asset := range release.Assets {
		if strings.HasPrefix(asset.Name, wantPrefix) && strings.HasSuffix(asset.Name, ".zip") {
			assetURL = asset.DownloadURL
			break
		}
	}
	if assetURL == "" {
		return false
	}

	log := logger.Get()
	staging, err := os.MkdirTemp(filepath.Dir(bundle), ".sx-app-update-")
	if err != nil {
		// Fall back to the system temp dir (may be a different volume).
		staging, err = os.MkdirTemp("", "sx-app-update-")
		if err != nil {
			return false
		}
	}
	defer func() { _ = os.RemoveAll(staging) }()

	zipPath := filepath.Join(staging, "update.zip")
	if err := downloadFile(a.ctx, assetURL, zipPath); err != nil {
		log.Warn("app update download failed", "error", err)
		return false
	}
	// ditto preserves resource forks and the bundle layout.
	if out, err := exec.CommandContext(a.ctx, "ditto", "-x", "-k", zipPath, staging).CombinedOutput(); err != nil {
		log.Warn("app update extract failed", "error", err, "output", string(out))
		return false
	}
	newBundle := filepath.Join(staging, filepath.Base(bundle))
	if _, err := os.Stat(filepath.Join(newBundle, "Contents", "MacOS")); err != nil {
		return false
	}

	backup := bundle + ".update-backup"
	_ = os.RemoveAll(backup)
	if err := os.Rename(bundle, backup); err != nil {
		log.Warn("app update swap failed", "error", err)
		return false
	}
	if err := moveBundle(newBundle, bundle); err != nil {
		// Put the old bundle back rather than leaving nothing installed.
		_ = os.Rename(backup, bundle)
		log.Warn("app update install failed", "error", err)
		return false
	}
	_ = os.RemoveAll(backup)
	log.Info("app auto-updated", "version", release.TagName)
	return true
}

// currentAppBundle resolves the .app bundle this process runs from.
func currentAppBundle() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", false
	}
	// …/sx.app/Contents/MacOS/sx
	macosDir := filepath.Dir(exe)
	contents := filepath.Dir(macosDir)
	bundle := filepath.Dir(contents)
	if filepath.Base(macosDir) != "MacOS" || filepath.Base(contents) != "Contents" ||
		!strings.HasSuffix(bundle, ".app") {
		return "", false
	}
	return bundle, true
}

// moveBundle renames when possible (same volume) and falls back to ditto.
func moveBundle(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if out, err := exec.Command("ditto", src, dst).CombinedOutput(); err != nil {
		return fmt.Errorf("ditto: %w (%s)", err, string(out))
	}
	return nil
}

func downloadFile(ctx context.Context, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", buildinfo.GetUserAgent())
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.ReadFrom(resp.Body); err != nil {
		return err
	}
	return f.Close()
}
