package main

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
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/publish"
	"github.com/sleuth-io/sx/internal/utils"
)

// Drafts are the app's local, unpublished working copies. A draft lives in
// the sx config directory (app-drafts/{id}/ with a draft.json plus the
// asset's files), survives restarts, and touches the vault only when the
// user hits Publish. The word "version" never reaches the UI: publishes are
// "revisions" and their numbering is automatic.

// Draft is the bridge's view of one unpublished asset.
type Draft struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	TypeLabel   string `json:"typeLabel"`
	Description string `json:"description"`
	// TargetAsset is the existing asset this draft updates, or "" when the
	// draft would create a new asset.
	TargetAsset string      `json:"targetAsset"`
	Files       []AssetFile `json:"files"`
}

// draftMeta is the persisted sidecar (everything but file contents).
type draftMeta struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	TargetAsset string `json:"targetAsset"`
}

func draftsRoot() (string, error) {
	dir, err := utils.GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "app-drafts"), nil
}

var draftIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

func draftDir(id string) (string, error) {
	if !draftIDPattern.MatchString(id) {
		return "", errors.New("invalid draft id")
	}
	root, err := draftsRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, id), nil
}

// slugify turns a filename or title into an asset-name-shaped slug.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSuffix(s, filepath.Ext(s))
	var b strings.Builder
	lastDash := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// inferDescription pulls a short description out of markdown content:
// frontmatter description first, else the first non-heading paragraph line.
func inferDescription(content string) string {
	lines := strings.Split(content, "\n")
	inFrontmatter := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if i == 0 && trimmed == "---" {
			inFrontmatter = true
			continue
		}
		if inFrontmatter {
			if trimmed == "---" {
				inFrontmatter = false
				continue
			}
			if desc, ok := strings.CutPrefix(trimmed, "description:"); ok {
				return strings.Trim(strings.TrimSpace(desc), `"'`)
			}
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if len(trimmed) > 200 {
			trimmed = trimmed[:197] + "…"
		}
		return trimmed
	}
	return ""
}

// promptFileNameFor returns the canonical prompt filename for a type, so a
// dropped loose markdown file lands with the name every AI client expects.
func promptFileNameFor(t asset.Type) string {
	switch t.Key {
	case asset.TypeAgent.Key:
		return "AGENT.md"
	case asset.TypeCommand.Key:
		return "COMMAND.md"
	case asset.TypeRule.Key:
		return "RULE.md"
	default:
		return "SKILL.md"
	}
}

// CreateDraftFromPaths builds a draft from files dropped onto the window.
// One markdown file becomes a single-file asset; a directory is taken whole.
func (a *App) CreateDraftFromPaths(paths []string) (Draft, error) {
	if len(paths) == 0 {
		return Draft{}, errors.New("nothing was dropped")
	}
	// One directory: take its contents as the asset.
	// Otherwise: collect the dropped files flat.
	var files []AssetFile
	base := paths[0]
	if info, err := os.Stat(base); err == nil && info.IsDir() && len(paths) == 1 {
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if strings.HasPrefix(d.Name(), ".") {
				return nil
			}
			rel, err := filepath.Rel(base, path)
			if err != nil {
				return err
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files = append(files, AssetFile{Path: filepath.ToSlash(rel), Content: string(content)})
			return nil
		})
		if err != nil {
			return Draft{}, err
		}
	} else {
		for _, p := range paths {
			info, err := os.Stat(p)
			if err != nil || info.IsDir() {
				continue
			}
			content, err := os.ReadFile(p)
			if err != nil {
				return Draft{}, err
			}
			files = append(files, AssetFile{Path: filepath.Base(p), Content: string(content)})
		}
	}
	if len(files) == 0 {
		return Draft{}, errors.New("no readable files were dropped")
	}

	name := slugify(filepath.Base(base))
	return a.assembleDraft(name, files)
}

// CreateDraftFromAsset starts an edit: a draft seeded with the latest
// published files of an existing asset.
func (a *App) CreateDraftFromAsset(name string) (Draft, error) {
	detail, err := a.GetAsset(name, "")
	if err != nil {
		return Draft{}, err
	}
	files := make([]AssetFile, 0, len(detail.Files))
	for _, f := range detail.Files {
		if f.Path == "metadata.toml" {
			continue // regenerated on publish
		}
		files = append(files, f)
	}
	draft := Draft{
		ID:          name,
		Name:        name,
		Type:        detail.Type,
		TypeLabel:   detail.TypeLabel,
		Description: detail.Description,
		TargetAsset: name,
		Files:       files,
	}
	return draft, a.saveDraft(draft)
}

// assembleDraft detects what the dropped files are, matches them against
// existing assets, and persists the draft.
func (a *App) assembleDraft(name string, files []AssetFile) (Draft, error) {
	zipData, err := zipFromFiles(files)
	if err != nil {
		return Draft{}, err
	}
	detectedName, assetType, _, err := publish.DetectNameAndType(zipData, name)
	if err != nil {
		return Draft{}, err
	}
	if detectedName != "" {
		name = slugify(detectedName)
	}
	if name == "" {
		name = "new-asset"
	}

	// A single loose markdown file gets the canonical prompt filename.
	if len(files) == 1 && strings.EqualFold(filepath.Ext(files[0].Path), ".md") {
		files[0].Path = promptFileNameFor(assetType)
	}

	description := ""
	for _, f := range files {
		if strings.HasSuffix(strings.ToLower(f.Path), ".md") {
			if description = inferDescription(f.Content); description != "" {
				break
			}
		}
	}

	draft := Draft{
		ID:          name,
		Name:        name,
		Type:        assetType.Key,
		TypeLabel:   assetType.Label,
		Description: description,
		Files:       files,
	}

	// Does this draft update an existing asset?
	if v, err := a.currentVault(); err == nil {
		if versions, err := v.GetVersionList(a.ctx, name); err == nil && len(versions) > 0 {
			draft.TargetAsset = name
		}
	}

	return draft, a.saveDraft(draft)
}

func (a *App) saveDraft(d Draft) error {
	dir, err := draftDir(d.ID)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	for _, f := range d.Files {
		target := filepath.Join(dir, filepath.FromSlash(f.Path))
		rel, err := filepath.Rel(dir, target)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("invalid file path in draft: %s", f.Path)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(target, []byte(f.Content), 0644); err != nil {
			return err
		}
	}
	meta := draftMeta{Name: d.Name, Type: d.Type, Description: d.Description, TargetAsset: d.TargetAsset}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "draft.json"), data, 0644)
}

// ListDrafts returns every saved draft.
func (a *App) ListDrafts() ([]Draft, error) {
	root, err := draftsRoot()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return []Draft{}, nil
		}
		return nil, err
	}
	drafts := make([]Draft, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if d, err := a.loadDraft(entry.Name()); err == nil {
			drafts = append(drafts, d)
		}
	}
	sort.Slice(drafts, func(i, j int) bool { return drafts[i].Name < drafts[j].Name })
	return drafts, nil
}

// GetDraft loads one draft with file contents.
func (a *App) GetDraft(id string) (Draft, error) {
	return a.loadDraft(id)
}

func (a *App) loadDraft(id string) (Draft, error) {
	dir, err := draftDir(id)
	if err != nil {
		return Draft{}, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "draft.json"))
	if err != nil {
		return Draft{}, err
	}
	var meta draftMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return Draft{}, err
	}
	t := asset.FromString(meta.Type)
	draft := Draft{
		ID: id, Name: meta.Name, Type: meta.Type, TypeLabel: t.Label,
		Description: meta.Description, TargetAsset: meta.TargetAsset,
	}
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() == "draft.json" {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		draft.Files = append(draft.Files, AssetFile{Path: filepath.ToSlash(rel), Content: string(content)})
		return nil
	})
	if err != nil {
		return Draft{}, err
	}
	sort.Slice(draft.Files, func(i, j int) bool { return draft.Files[i].Path < draft.Files[j].Path })
	return draft, nil
}

// UpdateDraft persists edited fields and file contents.
func (a *App) UpdateDraft(d Draft) (Draft, error) {
	if !draftIDPattern.MatchString(d.ID) {
		return Draft{}, errors.New("invalid draft")
	}
	if d.Name = slugify(d.Name); d.Name == "" {
		return Draft{}, errors.New("give the asset a name")
	}
	t := asset.FromString(d.Type)
	if !t.IsValid() {
		return Draft{}, fmt.Errorf("unknown asset type %q", d.Type)
	}
	d.TypeLabel = t.Label
	if err := a.saveDraft(d); err != nil {
		return Draft{}, err
	}
	return d, nil
}

// DiscardDraft deletes a draft.
func (a *App) DiscardDraft(id string) error {
	dir, err := draftDir(id)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// PublishDraft publishes a draft to the vault as the next revision of its
// asset and removes the draft. Returns the published asset's card.
func (a *App) PublishDraft(id string) (AssetCard, error) {
	draft, err := a.loadDraft(id)
	if err != nil {
		return AssetCard{}, err
	}
	v, err := a.currentVault()
	if err != nil {
		return AssetCard{}, err
	}

	zipData, err := zipFromFiles(draft.Files)
	if err != nil {
		return AssetCard{}, err
	}
	assetType := asset.FromString(draft.Type)
	if !assetType.IsValid() {
		return AssetCard{}, fmt.Errorf("unknown asset type %q", draft.Type)
	}

	version, identical, err := publish.SuggestVersion(a.ctx, v, draft.Name, zipData)
	if err != nil {
		return AssetCard{}, friendlyVaultError(err)
	}
	if identical {
		// Nothing changed relative to the latest revision; just drop the draft.
		_ = a.DiscardDraft(id)
		return AssetCard{}, fmt.Errorf("%s already matches the latest revision — nothing to publish", draft.Name)
	}

	meta := publish.BuildMetadata(draft.Name, version, assetType, zipData)
	if strings.TrimSpace(draft.Description) != "" {
		meta.Asset.Description = strings.TrimSpace(draft.Description)
	}
	hasMetadata := false
	for _, f := range draft.Files {
		if f.Path == "metadata.toml" {
			hasMetadata = true
		}
	}
	zipData, err = publish.ApplyMetadata(meta, zipData, hasMetadata)
	if err != nil {
		return AssetCard{}, err
	}
	if err := metadata.ValidateZip(zipData, &assetType); err != nil {
		return AssetCard{}, err
	}

	lockAsset := &lockfile.Asset{
		Name:    meta.Asset.Name,
		Version: meta.Asset.Version,
		Type:    meta.Asset.Type,
		Clients: append([]string(nil), meta.Asset.Clients...),
	}
	if err := v.AddAsset(a.ctx, lockAsset, zipData); err != nil {
		return AssetCard{}, friendlyVaultError(err)
	}

	// Register the publish in the manifest. Updates inherit the asset's
	// existing sharing; new assets default to the whole library (everyone
	// who can see this vault) — sharing IS the vault in the app's model.
	if draft.TargetAsset != "" {
		err = v.InheritInstallations(a.ctx, lockAsset)
	} else {
		err = v.SetInstallations(a.ctx, lockAsset, "")
	}
	if err != nil {
		return AssetCard{}, friendlyVaultError(err)
	}

	_ = a.DiscardDraft(id)
	return AssetCard{
		Name:        meta.Asset.Name,
		Description: meta.Asset.Description,
		Type:        meta.Asset.Type.Key,
		TypeLabel:   meta.Asset.Type.Label,
		Version:     meta.Asset.Version,
	}, nil
}

// RestoreRevision republishes an older revision's contents as the newest
// revision — the undo story for published edits.
func (a *App) RestoreRevision(name, version string) error {
	v, err := a.currentVault()
	if err != nil {
		return err
	}
	zipData, err := v.GetAssetByVersion(a.ctx, name, version)
	if err != nil {
		return friendlyVaultError(err)
	}
	next, identical, err := publish.SuggestVersion(a.ctx, v, name, zipData)
	if err != nil {
		return friendlyVaultError(err)
	}
	if identical {
		return errors.New("that revision already matches the current one")
	}
	meta := publish.BuildMetadata(name, next, assetTypeOf(a, name, version), zipData)
	zipData, err = publish.ApplyMetadata(meta, zipData, true)
	if err != nil {
		return err
	}
	lockAsset := &lockfile.Asset{
		Name:    name,
		Version: next,
		Type:    meta.Asset.Type,
		Clients: append([]string(nil), meta.Asset.Clients...),
	}
	if err := v.AddAsset(a.ctx, lockAsset, zipData); err != nil {
		return friendlyVaultError(err)
	}
	if err := v.InheritInstallations(a.ctx, lockAsset); err != nil {
		return friendlyVaultError(err)
	}
	return nil
}

func assetTypeOf(a *App, name, version string) asset.Type {
	if v, err := a.currentVault(); err == nil {
		if meta, err := v.GetMetadata(a.ctx, name, version); err == nil {
			return meta.Asset.Type
		}
	}
	return asset.TypeSkill
}

// zipFromFiles builds an in-memory zip from draft files.
func zipFromFiles(files []AssetFile) ([]byte, error) {
	tmp, err := os.MkdirTemp("", "sx-app-draft-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	for _, f := range files {
		target := filepath.Join(tmp, filepath.FromSlash(f.Path))
		rel, err := filepath.Rel(tmp, target)
		if err != nil || strings.HasPrefix(rel, "..") {
			return nil, fmt.Errorf("invalid file path: %s", f.Path)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(target, []byte(f.Content), 0644); err != nil {
			return nil, err
		}
	}
	return utils.CreateZip(tmp)
}
