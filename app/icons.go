package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/utils"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// Library icons personalize the switcher. Git and path libraries store the
// icon IN the vault (.sx/icon), so one user setting it applies to everyone
// sharing that vault. skills.new libraries belong to an organization, so
// their icon IS the org icon — pulled from the API and cached locally
// (config dir icons/<library>.org).

var iconExts = []string{".png", ".jpg", ".jpeg", ".webp", ".gif"}

// maxIconBytes bounds icons: they render at ~28px, so anything beyond
// this is waste that would bloat every GetSettings payload (and, for
// shared vaults, every clone).
const maxIconBytes = 1 << 20

func iconDataURL(data []byte) string {
	if len(data) == 0 || len(data) > maxIconBytes {
		return ""
	}
	return "data:" + http.DetectContentType(data) + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// libraryIcon resolves a library's icon by type: skills.new libraries show
// their organization's icon; git and path libraries show the icon stored
// in the vault.
func (a *App) libraryIcon(cfgType config.RepositoryType, name string) string {
	if cfgType == config.RepositoryTypeSleuth {
		return a.orgIconDataURL(name)
	}
	return a.vaultIconDataURL(name)
}

// vaultIconDataURL reads the shared icon out of a git/path library's vault.
func (a *App) vaultIconDataURL(name string) string {
	store, err := a.vaultIconStore(name)
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Second)
	defer cancel()
	data, err := store.GetVaultIcon(ctx)
	if err != nil {
		return ""
	}
	return iconDataURL(data)
}

// vaultIconStore opens the named library's vault as an icon store.
func (a *App) vaultIconStore(name string) (vaultpkg.VaultIconStore, error) {
	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return nil, err
	}
	profile, ok := mpc.GetProfile(name)
	if !ok {
		return nil, fmt.Errorf("library %q not found", name)
	}
	v, err := vaultpkg.NewFromConfig(profile.ToConfig(nil, nil))
	if err != nil {
		return nil, err
	}
	store, ok := v.(vaultpkg.VaultIconStore)
	if !ok {
		return nil, errors.New("this library type has no stored icon")
	}
	return store, nil
}

// resolveLibraryName turns "" into the active library, validates the name,
// and rejects skills.new libraries — their icon is the org's, managed on
// the server.
func resolveLibraryName(name string) (string, error) {
	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return "", err
	}
	if name == "" {
		name = config.GetActiveProfileName(mpc)
	}
	if !safePathComponent(name) {
		return "", fmt.Errorf("library %q not found", name)
	}
	if profile, ok := mpc.GetProfile(name); ok && profile.Type == config.RepositoryTypeSleuth {
		return "", errors.New("this library's icon comes from your skills.new organization — change it there")
	}
	return name, nil
}

// ChooseLibraryIcon opens the native image picker and stores the choice in
// the library's vault, where everyone sharing it will see it. Returns the
// new icon as a data URL — empty when the user cancelled the picker.
func (a *App) ChooseLibraryIcon(name string) (string, error) {
	name, err := resolveLibraryName(name)
	if err != nil {
		return "", err
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose a library icon",
		Filters: []runtime.FileFilter{
			{DisplayName: "Images (*.png, *.jpg, *.webp, *.gif)", Pattern: "*.png;*.jpg;*.jpeg;*.webp;*.gif"},
		},
	})
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil // cancelled
	}
	ext := strings.ToLower(filepath.Ext(path))
	if !slices.Contains(iconExts, ext) {
		return "", errors.New("choose a PNG, JPEG, WebP, or GIF image")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(data) > maxIconBytes {
		return "", errors.New("that image is over 1 MB — icons render small, pick a smaller file")
	}
	store, err := a.vaultIconStore(name)
	if err != nil {
		return "", err
	}
	// Git vaults commit and push the icon; give the sync room to run.
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
	defer cancel()
	if err := store.SetVaultIcon(ctx, data); err != nil {
		return "", friendlyVaultError(err)
	}
	return iconDataURL(data), nil
}

// ClearLibraryIcon removes a library's shared icon (back to the default
// mark, for everyone using the vault).
func (a *App) ClearLibraryIcon(name string) error {
	name, err := resolveLibraryName(name)
	if err != nil {
		return err
	}
	store, err := a.vaultIconStore(name)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
	defer cancel()
	if err := store.SetVaultIcon(ctx, nil); err != nil {
		return friendlyVaultError(err)
	}
	return nil
}

// --- Org icons (skills.new libraries) ---

// orgIconExt marks a cached organization icon; the mime is sniffed from
// the bytes since the org can change image formats server-side.
const orgIconExt = ".org"

// orgIconRefreshed tracks which libraries already refreshed their org icon
// this session, so GetSettings polls don't hammer the API.
var orgIconRefreshed sync.Map

func iconsDir() (string, error) {
	dir, err := utils.GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "icons"), nil
}

func removeIconFiles(name string) {
	dir, err := iconsDir()
	if err != nil {
		return
	}
	_ = os.Remove(filepath.Join(dir, name+orgIconExt))
}

// orgIconDataURL serves the cached org icon and keeps it fresh: a cached
// copy is returned immediately with a once-per-session background refresh;
// with no cache yet, one synchronous (bounded) fetch fills it.
func (a *App) orgIconDataURL(name string) string {
	if !safePathComponent(name) {
		return ""
	}
	dir, err := iconsDir()
	if err != nil {
		return ""
	}
	cache := filepath.Join(dir, name+orgIconExt)
	if data, err := os.ReadFile(cache); err == nil && len(data) > 0 {
		if _, loaded := orgIconRefreshed.LoadOrStore(name, true); !loaded {
			go a.refreshOrgIcon(name, cache)
		}
		return iconDataURL(data)
	}
	if _, loaded := orgIconRefreshed.LoadOrStore(name, true); !loaded {
		a.refreshOrgIcon(name, cache)
		if data, err := os.ReadFile(cache); err == nil && len(data) > 0 {
			return iconDataURL(data)
		}
	}
	return ""
}

// refreshOrgIcon asks the org's vault for its icon URL and re-caches the
// image. Best-effort: failures keep whatever cache exists.
func (a *App) refreshOrgIcon(name, cache string) {
	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return
	}
	profile, ok := mpc.GetProfile(name)
	if !ok {
		return
	}
	cfg := profile.ToConfig(nil, nil)
	v, err := vaultpkg.NewFromConfig(cfg)
	if err != nil {
		return
	}
	provider, ok := v.(interface {
		OrgInfo(ctx context.Context) (string, string, error)
	})
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, iconURL, err := provider.OrgInfo(ctx)
	if err != nil {
		return
	}
	if iconURL == "" {
		// The org has no icon (anymore) — drop any stale cache.
		_ = os.Remove(cache)
		return
	}
	data, err := fetchOrgIconImage(ctx, cfg, iconURL)
	if err != nil || len(data) == 0 {
		return
	}
	if err := os.MkdirAll(filepath.Dir(cache), 0755); err != nil {
		return
	}
	tmp := cache + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	_ = os.Rename(tmp, cache)
}

// fetchOrgIconImage downloads the icon, resolving relative URLs against
// the server and sending auth only to the vault's own host.
func fetchOrgIconImage(ctx context.Context, cfg *config.Config, iconURL string) ([]byte, error) {
	resolved := iconURL
	server, serverErr := url.Parse(cfg.ServerURL)
	if u, err := url.Parse(iconURL); err == nil && !u.IsAbs() && serverErr == nil {
		resolved = server.ResolveReference(u).String()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resolved, nil)
	if err != nil {
		return nil, err
	}
	if target, err := url.Parse(resolved); err == nil && serverErr == nil && target.Host == server.Host && cfg.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.AuthToken)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("icon download failed: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxIconBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxIconBytes {
		return nil, errors.New("org icon exceeds the size cap")
	}
	return data, nil
}
