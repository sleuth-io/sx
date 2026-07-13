package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/config"
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

// draftsRoot is the draft directory for the CURRENT library. Drafts are
// unpublished work destined for a specific vault — switching libraries
// must not carry another library's drafts along, so each library gets its
// own subdirectory keyed by its location.
func draftsRoot() (string, error) {
	dir, err := utils.GetConfigDir()
	if err != nil {
		return "", err
	}
	base := filepath.Join(dir, "app-drafts")
	cfg, err := config.Load()
	if err != nil {
		// No configured library: a bare root keeps onboarding flows working.
		return base, nil
	}
	location := cfg.RepositoryURL
	if cfg.Type == config.RepositoryTypeSleuth && cfg.ServerURL != "" {
		location = cfg.ServerURL
	}
	sum := sha256.Sum256([]byte(string(cfg.Type) + "|" + location))
	root := filepath.Join(base, "lib-"+hex.EncodeToString(sum[:8]))

	// One-time adoption of pre-namespacing drafts: loose draft dirs at the
	// top level belong to whichever library the user had open — move them
	// into the current library's space the first time it's used.
	if entries, err := os.ReadDir(base); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), "lib-") {
				continue
			}
			if _, err := os.Stat(filepath.Join(base, entry.Name(), "draft.json")); err != nil {
				continue
			}
			if err := os.MkdirAll(root, 0755); err != nil {
				break
			}
			_ = os.Rename(filepath.Join(base, entry.Name()), filepath.Join(root, entry.Name()))
		}
	}
	return root, nil
}

var draftIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// draftsMu serializes every draft mutation. Wails invokes bound methods
// concurrently, and autosave (UpdateDraft) can otherwise interleave with
// publish/discard and destroy work.
var draftsMu sync.Mutex

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

// inferDescription pulls a short description out of markdown content;
// shared with the publish pipeline and vault listings.
func inferDescription(content string) string {
	return asset.InferDescription(content)
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

// errCancelled marks a user-cancelled picker; the frontend ignores it.
var errCancelled = errors.New("cancelled")

// junkFile reports paths that should never become asset files (macOS zip
// artifacts, dotfiles).
func junkFile(path string) bool {
	if strings.HasPrefix(path, "__MACOSX/") || strings.Contains(path, "/__MACOSX/") {
		return true
	}
	return strings.HasPrefix(filepath.Base(path), ".")
}

// filesFromZip expands a zipped asset into draft files, stripping a single
// shared top-level folder (the common "skill-name/SKILL.md" shape) and
// skipping junk and binary entries.
func filesFromZip(zipData []byte) ([]AssetFile, error) {
	entries, err := utils.ListZipFiles(zipData)
	if err != nil {
		return nil, errors.New("that zip file couldn't be read")
	}
	prefix := ""
	for i, entry := range entries {
		top := ""
		if slash := strings.IndexByte(entry, '/'); slash >= 0 {
			top = entry[:slash+1]
		}
		if i == 0 {
			prefix = top
		} else if top != prefix {
			prefix = ""
			break
		}
	}

	var files []AssetFile
	for _, entry := range entries {
		if strings.HasSuffix(entry, "/") || junkFile(entry) {
			continue
		}
		content, err := utils.ReadZipFile(zipData, entry)
		if err != nil {
			continue
		}
		if !utf8.Valid(content) {
			continue // binary payloads (images etc.) can't be edited here
		}
		files = append(files, AssetFile{
			Path:    strings.TrimPrefix(entry, prefix),
			Content: string(content),
		})
	}
	if len(files) == 0 {
		return nil, errors.New("that zip has no text files a draft can use")
	}
	return files, nil
}

func isZip(path string, content []byte) bool {
	if strings.EqualFold(filepath.Ext(path), ".zip") {
		return true
	}
	return len(content) >= 4 && string(content[:4]) == "PK\x03\x04"
}

// CreateDraftFromPaths builds a draft from files dropped onto the window (or
// chosen in the picker). One markdown file becomes a single-file asset, a
// zip is unpacked, and a directory is taken whole.
func (a *App) CreateDraftFromPaths(paths []string) (Draft, error) {
	draftsMu.Lock()
	defer draftsMu.Unlock()
	if len(paths) == 0 {
		return Draft{}, errors.New("nothing was dropped")
	}
	// One directory: take its contents as the asset.
	// Otherwise: collect the dropped files flat, unpacking zips.
	var files []AssetFile
	base := paths[0]
	if info, err := os.Stat(base); err == nil && info.IsDir() && len(paths) == 1 {
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				// Never descend into dot-directories (.git, .venv, …) —
				// their internals must not ship to the team vault.
				if strings.HasPrefix(d.Name(), ".") && path != base {
					return filepath.SkipDir
				}
				return nil
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
			if !utf8.Valid(content) {
				return nil
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
			if isZip(p, content) {
				zipFiles, err := filesFromZip(content)
				if err != nil {
					return Draft{}, err
				}
				files = append(files, zipFiles...)
				continue
			}
			if !utf8.Valid(content) {
				continue
			}
			files = append(files, AssetFile{Path: filepath.Base(p), Content: string(content)})
		}
	}
	if len(files) == 0 {
		return Draft{}, errors.New("nothing usable was dropped — markdown files, folders, and zips work")
	}

	name := slugify(filepath.Base(base))
	return a.assembleDraft(name, files)
}

// CreateBlankDraft scaffolds an empty asset of the given kind, ready to
// write in the editor.
func (a *App) CreateBlankDraft(kind string) (Draft, error) {
	draftsMu.Lock()
	defer draftsMu.Unlock()
	t := asset.FromString(kind)
	if !t.IsValid() {
		t = asset.TypeSkill
	}
	// Re-open an in-progress blank draft of this kind instead of silently
	// overwriting it.
	if existing, err := a.loadDraft("new-" + t.Key); err == nil {
		return existing, nil
	}
	content := "---\nname: my-" + t.Key + "\ndescription: \n---\n\n# My " + t.Label + "\n\nDescribe what your AI tools should know or do.\n"
	draft := Draft{
		ID:        "new-" + t.Key,
		Name:      "new-" + t.Key,
		Type:      t.Key,
		TypeLabel: t.Label,
		Files:     []AssetFile{{Path: promptFileNameFor(t), Content: content}},
	}
	return draft, a.saveDraft(draft)
}

// CreateDraftFromAsset starts an edit: a draft seeded with the latest
// published files of an existing asset.
func (a *App) CreateDraftFromAsset(name string) (Draft, error) {
	if err := validateAssetRef(name, ""); err != nil {
		return Draft{}, err
	}
	draftsMu.Lock()
	defer draftsMu.Unlock()
	// Resume unpublished edits instead of silently replacing them with a
	// fresh copy of the published files.
	if existing, err := a.loadDraft(name); err == nil {
		return existing, nil
	}
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

// targetAssetFor reports whether a draft named name updates an existing
// asset: the name itself when the vault has versions for it, "" otherwise.
// Publish uses this to inherit the asset's installations instead of
// resetting sharing to everyone. A vault read failure yields "" — same as
// today's behavior for new-asset drafts; the publish sheet shows what the
// draft targets, so a wrong "" is visible before it can do harm.
func (a *App) targetAssetFor(name string) string {
	if v, err := a.currentVault(); err == nil {
		if versions, err := v.GetVersionList(a.ctx, name); err == nil && len(versions) > 0 {
			return name
		}
	}
	return ""
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

	draft.TargetAsset = a.targetAssetFor(name)

	return draft, a.saveDraft(draft)
}

// CreateDraftFromFiles creates a draft from in-memory files — the
// extension API's drafts.create path. The id derives from the name and
// is uniquified against existing drafts, so repeated creates (multiple
// quick-captures, the same template twice) never clobber each other.
func (a *App) CreateDraftFromFiles(name string, files []AssetFile) (Draft, error) {
	draftsMu.Lock()
	defer draftsMu.Unlock()
	if len(files) == 0 {
		return Draft{}, errors.New("a draft needs at least one file")
	}
	base := slugify(name)
	if base == "" {
		base = "extension-draft"
	}
	id := base
	for i := 2; ; i++ {
		if _, err := a.loadDraft(id); err != nil {
			break // free slot
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
	t := asset.TypeSkill
	if zipData, err := zipFromFiles(files); err == nil {
		if _, detected, _, derr := publish.DetectNameAndType(zipData, id); derr == nil && detected.Key != "" {
			t = detected
		}
	}
	draft := Draft{
		ID:        id,
		Name:      id,
		Type:      t.Key,
		TypeLabel: t.Label,
		Files:     files,
	}

	// Without this, publishing an extension-created draft over an existing
	// asset would take the new-asset branch and reset its sharing to everyone.
	draft.TargetAsset = a.targetAssetFor(draft.Name)

	return draft, a.saveDraft(draft)
}

// saveDraft persists a draft atomically: the new content is staged in a
// sibling temp directory and swapped in, so a crash or write error never
// destroys the previous copy. Callers must hold draftsMu.
func (a *App) saveDraft(d Draft) error {
	dir, err := draftDir(d.ID)
	if err != nil {
		return err
	}
	tmp := dir + ".~tmp"
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	for _, f := range d.Files {
		target := filepath.Join(tmp, filepath.FromSlash(f.Path))
		rel, err := filepath.Rel(tmp, target)
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
	if err := os.MkdirAll(tmp, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tmp, "draft.json"), data, 0644); err != nil {
		return err
	}

	// Swap: keep the old copy until the new one is fully in place.
	old := dir + ".~old"
	if err := os.RemoveAll(old); err != nil {
		return err
	}
	if _, err := os.Stat(dir); err == nil {
		if err := os.Rename(dir, old); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, dir); err != nil {
		// Restore the previous copy rather than leaving nothing.
		_ = os.Rename(old, dir)
		return err
	}
	return os.RemoveAll(old)
}

// ListDrafts returns every saved draft.
func (a *App) ListDrafts() ([]Draft, error) {
	draftsMu.Lock()
	defer draftsMu.Unlock()
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
	draftsMu.Lock()
	defer draftsMu.Unlock()
	return a.loadDraft(id)
}

// loadDraft reads a draft, recovering the previous copy if a crash during
// saveDraft's rename swap left it at dir.~old. Callers must hold draftsMu.
func (a *App) loadDraft(id string) (Draft, error) {
	dir, err := draftDir(id)
	if err != nil {
		return Draft{}, err
	}
	if _, statErr := os.Stat(filepath.Join(dir, "draft.json")); statErr != nil {
		old := dir + ".~old"
		if _, oldErr := os.Stat(filepath.Join(old, "draft.json")); oldErr == nil {
			_ = os.RemoveAll(dir)
			if renameErr := os.Rename(old, dir); renameErr != nil {
				return Draft{}, renameErr
			}
		}
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
	// Repair drafts persisted without a type (a vault whose metadata read
	// failed used to produce them) — otherwise they can never publish
	// ("unknown asset type"). Detect from the files, defaulting to skill.
	if draft.Type == "" {
		repaired := asset.TypeSkill
		if zipData, zerr := zipFromFiles(draft.Files); zerr == nil {
			if _, detected, _, derr := publish.DetectNameAndType(zipData, draft.Name); derr == nil && detected.Key != "" {
				repaired = detected
			}
		}
		draft.Type = repaired.Key
		draft.TypeLabel = repaired.Label
	}
	return draft, nil
}

// UpdateDraft persists edited fields and file contents. It refuses to
// resurrect a draft that was published or discarded while the save was
// queued.
func (a *App) UpdateDraft(d Draft) (Draft, error) {
	draftsMu.Lock()
	defer draftsMu.Unlock()
	if !draftIDPattern.MatchString(d.ID) {
		return Draft{}, errors.New("invalid draft")
	}
	dir, err := draftDir(d.ID)
	if err != nil {
		return Draft{}, err
	}
	if _, err := os.Stat(filepath.Join(dir, "draft.json")); err != nil {
		return Draft{}, errors.New("draft no longer exists")
	}
	if d.Name = slugify(d.Name); d.Name == "" {
		return Draft{}, errors.New("give the asset a name")
	}
	t := asset.FromString(d.Type)
	if !t.IsValid() {
		return Draft{}, fmt.Errorf("unknown asset type %q", d.Type)
	}
	d.TypeLabel = t.Label
	// A rename changes what the draft publishes onto: recompute whether the
	// (new) name updates an existing asset, exactly as draft creation does.
	// A stale TargetAsset would either reset an existing asset's sharing or
	// inherit scopes that don't exist.
	if d.TargetAsset != d.Name {
		d.TargetAsset = a.targetAssetFor(d.Name)
	}
	if err := a.saveDraft(d); err != nil {
		return Draft{}, err
	}
	return d, nil
}

// DiscardDraft deletes a draft.
func (a *App) DiscardDraft(id string) error {
	draftsMu.Lock()
	defer draftsMu.Unlock()
	return discardDraftLocked(id)
}

func discardDraftLocked(id string) error {
	dir, err := draftDir(id)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// PublishDraft publishes a draft to the vault as the next revision of its
// asset and removes the draft. Returns the published asset's card.
func (a *App) PublishDraft(id string) (AssetCard, error) {
	draftsMu.Lock()
	defer draftsMu.Unlock()
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
		_ = discardDraftLocked(id)
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

	_ = discardDraftLocked(id)
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
	if err := validateAssetRef(name, version); err != nil {
		return err
	}
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
