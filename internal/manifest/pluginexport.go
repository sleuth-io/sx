package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/utils"
)

// Derived plugin-marketplace manifests, regenerated on every manifest save
// so Claude Code and Codex can install the vault directly:
//
//	/plugin marketplace add <vault repo>        (Claude Code)
//	codex plugin marketplace add <vault repo>   (Codex)
//
// The files are pure functions of sx.toml plus the vault's origin URL and
// are always overwritten. They require the v2 storage format, where
// assets/{name}/ holds the directly-usable latest version.
const (
	claudeMarketplaceFile = ".claude-plugin/marketplace.json"
	codexPluginFile       = ".codex-plugin/plugin.json"
	codexMarketplaceFile  = ".agents/plugins/marketplace.json"
)

type claudeMarketplace struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Owner       marketplaceOwner    `json:"owner"`
	Plugins     []claudePluginEntry `json:"plugins"`
}

type marketplaceOwner struct {
	Name string `json:"name"`
}

type claudePluginEntry struct {
	Name        string   `json:"name"`
	Source      string   `json:"source"`
	Description string   `json:"description,omitempty"`
	Strict      bool     `json:"strict"`
	Skills      []string `json:"skills"`
}

type codexPlugin struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Skills      string `json:"skills"`
}

type codexMarketplace struct {
	Name    string             `json:"name"`
	Plugins []codexMarketEntry `json:"plugins"`
}

type codexMarketEntry struct {
	Name   string            `json:"name"`
	Source codexMarketSource `json:"source"`
}

type codexMarketSource struct {
	Source string `json:"source"`
	Path   string `json:"path"`
}

// writePluginManifests regenerates the derived marketplace files, or
// removes them when the vault has nothing to offer (no skill assets, or a
// pre-v2 storage layout where assets/ is not directly usable).
func writePluginManifests(vaultRoot string, m *Manifest) error {
	skills := latestSkillNames(m)
	if m.SchemaVersion < 2 || len(skills) == 0 {
		return removePluginManifests(vaultRoot)
	}

	slug := vaultSlug(vaultRoot)
	description := fmt.Sprintf("Skills from the %s sx library", slug)

	claude := claudeMarketplace{
		Name:        slug,
		Description: description,
		Owner:       marketplaceOwner{Name: slug},
		Plugins: []claudePluginEntry{{
			Name:        slug,
			Source:      "./",
			Description: "Every skill in this library",
			Strict:      false,
			Skills:      []string{"./assets"},
		}},
	}
	seenPlugins := map[string]bool{slug: true}
	for _, c := range m.Collections {
		entry, ok := collectionPluginEntry(c, skills, slug)
		if !ok {
			continue
		}
		// Two collection names can slugify to the same plugin name; a
		// duplicate would make the whole marketplace invalid.
		if seenPlugins[entry.Name] {
			continue
		}
		seenPlugins[entry.Name] = true
		claude.Plugins = append(claude.Plugins, entry)
	}

	codexP := codexPlugin{
		Name:        slug,
		Version:     "0.1.0",
		Description: description,
		Skills:      "./assets",
	}
	codexM := codexMarketplace{
		Name: slug,
		Plugins: []codexMarketEntry{{
			Name:   slug,
			Source: codexMarketSource{Source: "local", Path: "./"},
		}},
	}

	for file, payload := range map[string]any{
		claudeMarketplaceFile: claude,
		codexPluginFile:       codexP,
		codexMarketplaceFile:  codexM,
	} {
		if err := writeJSON(filepath.Join(vaultRoot, filepath.FromSlash(file)), payload); err != nil {
			return err
		}
	}
	return nil
}

// collectionPluginEntry maps one collection to a Claude plugin listing its
// skill assets. Collections without skills, or colliding with the library
// plugin's name, produce no entry.
func collectionPluginEntry(c Collection, skills map[string]bool, librarySlug string) (claudePluginEntry, bool) {
	name := utils.Slugify(c.Name)
	if name == "" || name == librarySlug {
		return claudePluginEntry{}, false
	}
	var paths []string
	for _, assetName := range c.Assets {
		if skills[assetName] {
			paths = append(paths, "./assets/"+assetName)
		}
	}
	if len(paths) == 0 {
		return claudePluginEntry{}, false
	}
	sort.Strings(paths)
	description := c.Description
	if description == "" {
		description = fmt.Sprintf("Skills in the %s collection", c.Name)
	}
	return claudePluginEntry{
		Name:        name,
		Source:      "./",
		Description: description,
		Strict:      false,
		Skills:      paths,
	}, true
}

// latestSkillNames returns the set of skill asset names in the manifest.
// The manifest holds one row per published version; names dedupe them.
func latestSkillNames(m *Manifest) map[string]bool {
	out := map[string]bool{}
	for _, a := range m.Assets {
		if a.Type.Key == asset.TypeSkill.Key {
			out[a.Name] = true
		}
	}
	return out
}

func removePluginManifests(vaultRoot string) error {
	var errs []error
	for _, file := range []string{claudeMarketplaceFile, codexPluginFile, codexMarketplaceFile} {
		path := filepath.Join(vaultRoot, filepath.FromSlash(file))
		if err := os.Remove(path); err != nil {
			if !os.IsNotExist(err) {
				// A stale marketplace we failed to retract is a real
				// error, not "already gone".
				errs = append(errs, err)
			}
			continue
		}
		// Drop the containing directory too when we emptied it.
		_ = os.Remove(filepath.Dir(path))
	}
	return errors.Join(errs...)
}

func writeJSON(path string, payload any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode %s: %w", path, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}

// vaultSlug names the marketplace: the git origin URL's repo name when the
// vault root is a clone (clone directories are cache hashes), otherwise
// the directory name.
func vaultSlug(vaultRoot string) string {
	name := filepath.Base(vaultRoot)
	if url := gitOriginURL(vaultRoot); url != "" {
		base := strings.TrimSuffix(filepath.Base(strings.ReplaceAll(url, ":", "/")), ".git")
		if base != "" {
			name = base
		}
	}
	if slug := utils.Slugify(name); slug != "" {
		return slug
	}
	return "sx-library"
}

// Bounded to the origin section ([^[]* cannot cross into the next
// [section] header), so another remote's url is never captured.
var gitOriginURLPattern = regexp.MustCompile(`\[remote "origin"\][^\[]*url\s*=\s*(\S+)`)

// gitOriginURL reads the origin remote from .git/config without invoking
// git. Returns "" for non-clones.
func gitOriginURL(vaultRoot string) string {
	data, err := os.ReadFile(filepath.Join(vaultRoot, ".git", "config"))
	if err != nil {
		return ""
	}
	match := gitOriginURLPattern.FindSubmatch(data)
	if match == nil {
		return ""
	}
	return string(match[1])
}
