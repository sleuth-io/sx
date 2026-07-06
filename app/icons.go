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

// Library icons personalize the switcher. For git and path libraries the
// user uploads one, stored under the sx config dir (icons/<library>.<ext>).
// skills.new libraries belong to an organization, so their icon IS the org
// icon — pulled from the API and cached locally (icons/<library>.org).

var iconExts = []string{".png", ".jpg", ".jpeg", ".webp", ".gif"}

// maxIconBytes bounds uploads: the icon renders at ~28px, so anything
// beyond this is waste that would bloat every GetSettings payload.
const maxIconBytes = 1 << 20

func iconsDir() (string, error) {
	dir, err := utils.GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "icons"), nil
}

// libraryIconFile finds the stored icon for a library, "" when none.
func libraryIconFile(name string) string {
	if !safePathComponent(name) {
		return ""
	}
	dir, err := iconsDir()
	if err != nil {
		return ""
	}
	for _, ext := range iconExts {
		p := filepath.Join(dir, name+ext)
		if info, err := os.Stat(p); err == nil && info.Size() <= maxIconBytes {
			return p
		}
	}
	return ""
}

// libraryIconDataURL loads a library's icon as a data URL for the frontend.
func libraryIconDataURL(name string) string {
	p := libraryIconFile(name)
	if p == "" {
		return ""
	}
	data, err := os.ReadFile(p)
	if err != nil || len(data) == 0 {
		return ""
	}
	return "data:" + iconMime(p) + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func iconMime(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return "image/png"
	}
}

func removeIconFiles(name string) {
	dir, err := iconsDir()
	if err != nil {
		return
	}
	for _, ext := range iconExts {
		_ = os.Remove(filepath.Join(dir, name+ext))
	}
	_ = os.Remove(filepath.Join(dir, name+orgIconExt))
}

// --- Org icons (skills.new libraries) ---

// orgIconExt marks a cached organization icon; the mime is sniffed from
// the bytes since the org can change image formats server-side.
const orgIconExt = ".org"

// orgIconRefreshed tracks which libraries already refreshed their org icon
// this session, so GetSettings polls don't hammer the API.
var orgIconRefreshed sync.Map

// libraryIcon resolves a library's icon by type: skills.new libraries show
// their organization's icon; everything else shows the local upload.
func (a *App) libraryIcon(cfgType config.RepositoryType, name string) string {
	if cfgType == config.RepositoryTypeSleuth {
		return a.orgIconDataURL(name)
	}
	return libraryIconDataURL(name)
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
		return "data:" + http.DetectContentType(data) + ";base64," + base64.StdEncoding.EncodeToString(data)
	}
	if _, loaded := orgIconRefreshed.LoadOrStore(name, true); !loaded {
		a.refreshOrgIcon(name, cache)
		if data, err := os.ReadFile(cache); err == nil && len(data) > 0 {
			return "data:" + http.DetectContentType(data) + ";base64," + base64.StdEncoding.EncodeToString(data)
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

// resolveLibraryName turns "" into the active library, validates the name
// is usable as a filename component, and rejects skills.new libraries —
// their icon is the org's, managed on the server.
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

// ChooseLibraryIcon opens the native image picker and stores the choice
// as the library's icon. Returns the new icon as a data URL — empty when
// the user cancelled the picker.
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
	dir, err := iconsDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	// One icon per library: drop any previous file with a different ext.
	removeIconFiles(name)
	if err := os.WriteFile(filepath.Join(dir, name+ext), data, 0644); err != nil {
		return "", err
	}
	return libraryIconDataURL(name), nil
}

// ClearLibraryIcon removes a library's icon (back to the default mark).
func (a *App) ClearLibraryIcon(name string) error {
	name, err := resolveLibraryName(name)
	if err != nil {
		return err
	}
	removeIconFiles(name)
	return nil
}
