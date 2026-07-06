package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/utils"
)

// Library icons personalize the switcher: one optional image per library,
// stored under the sx config dir (icons/<library>.<ext>) so each machine
// keeps its own copy regardless of vault type.

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
}

// resolveLibraryName turns "" into the active library and validates the
// name is usable as a filename component.
func resolveLibraryName(name string) (string, error) {
	if name == "" {
		cfg, err := config.Load()
		if err != nil {
			return "", err
		}
		name = cfg.ProfileName
	}
	if !safePathComponent(name) {
		return "", fmt.Errorf("library %q not found", name)
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
