package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/utils"
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
