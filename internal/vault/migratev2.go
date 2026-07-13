package vault

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sleuth-io/sx/internal/buildinfo"
	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
	"github.com/sleuth-io/sx/internal/utils"
	"github.com/sleuth-io/sx/internal/vault/layout"
	"github.com/sleuth-io/sx/internal/version"
)

// This file implements the v1 → v2 storage-format migration for file-backed
// vaults. See internal/vault/layout for the two formats and docs/v2-spec.md
// for the design. Migration is:
//
//   - Automatic: every direct write to a v1 vault migrates it first, as its
//     own commit for git vaults, so users of a new sx never manage formats.
//   - Explicit: `sx vault migrate` runs the same conversion on demand.
//   - Skipped for PR-branch writes: a contributor without direct write
//     access keeps producing v1-shaped changes so their PR stays mergeable;
//     the vault migrates on the next direct write by a publisher.
//   - Resumable: an interrupted migration is completed by the next run.
//     Per asset, an existing archive directory means "already moved" — the
//     root view is refreshed and the move is skipped.
//
// Reads never migrate. A v2 build reads v1 vaults through layout detection
// indefinitely.

// ErrStorageUpToDate is returned by migration entry points when there is
// nothing to migrate: the vault is already on the current storage format, or
// is uninitialized (no manifest — the format is stamped when the manifest is
// first created, not here).
var ErrStorageUpToDate = errors.New("vault storage is already on the current format")

// MigrationResult describes a completed storage migration.
type MigrationResult struct {
	// Assets is the number of asset directories moved into the archive.
	Assets int
}

// MigrationPlan is the dry-run description of a pending migration.
type MigrationPlan struct {
	// FromVersion and ToVersion are the storage format versions involved.
	FromVersion int
	ToVersion   int
	// Assets are the asset names whose storage would move.
	Assets []string
}

// planStorageMigration inspects a vault and reports what migrateStorageToV2
// would do. Returns ErrStorageUpToDate when the vault needs no migration.
func planStorageMigration(vaultRoot string) (*MigrationPlan, error) {
	m, ok, err := manifest.Load(vaultRoot)
	if err != nil {
		return nil, err
	}
	if !ok || m.SchemaVersion >= 2 {
		return nil, ErrStorageUpToDate
	}
	names, err := v1AssetDirNames(vaultRoot)
	if err != nil {
		return nil, err
	}
	return &MigrationPlan{FromVersion: m.SchemaVersion, ToVersion: 2, Assets: names}, nil
}

// v1AssetDirNames lists the asset directories under assets/ that hold v1
// version storage (i.e. have not been converted to a v2 root view yet).
// Namespaced asset names ("opsx/apply") live in nested directories: each
// directory is classified as an asset or a namespace segment, and namespaces
// are recursed into rather than migrated as assets themselves — treating a
// namespace directory as an asset would archive its child assets as phantom
// versions.
func v1AssetDirNames(vaultRoot string) ([]string, error) {
	manifestNames, err := manifestAssetNameSet(vaultRoot)
	if err != nil {
		return nil, err
	}
	// A name that is simultaneously an asset and a namespace prefix
	// ("opsx" alongside "opsx/apply") has no correct migration: moving
	// opsx as an asset archives apply/ as a phantom version, and treating
	// it as a namespace strands opsx's own versions. Refuse rather than
	// silently pick one interpretation.
	if conflict := assetNamespaceConflict(manifestNames); conflict != "" {
		return nil, fmt.Errorf("manifest declares %q both as an asset and as a namespace prefix of other assets; rename one of them before migrating", conflict)
	}
	var names []string
	if err := collectV1AssetDirNames(vaultRoot, "", manifestNames, &names); err != nil {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}

// assetNamespaceConflict returns a declared asset name that is also a
// namespace prefix of another declared name (e.g. "opsx" when both "opsx"
// and "opsx/apply" exist), or "" when there is no conflict.
func assetNamespaceConflict(names map[string]bool) string {
	for name := range names {
		for i := len(name) - 1; i > 0; i-- {
			if name[i] == '/' && names[name[:i]] {
				return name[:i]
			}
		}
	}
	return ""
}

// manifestAssetNameSet returns the set of asset names declared in the
// vault's manifest, or an empty set when there is no manifest.
func manifestAssetNameSet(vaultRoot string) (map[string]bool, error) {
	m, ok, err := manifest.Load(vaultRoot)
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool)
	if ok {
		for i := range m.Assets {
			names[m.Assets[i].Name] = true
		}
	}
	return names, nil
}

// collectV1AssetDirNames scans one level of the assets/ tree, appending the
// slash-separated names of migratable v1 asset directories and recursing
// into namespace directories. prefix is the slash-separated path scanned so
// far ("" for the assets/ root).
func collectV1AssetDirNames(vaultRoot, prefix string, manifestNames map[string]bool, names *[]string) error {
	entries, err := os.ReadDir(filepath.Join(vaultRoot, "assets", filepath.FromSlash(prefix)))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read assets directory: %w", err)
	}
	for _, entry := range filterScanEntries(entries) {
		if !entry.IsDir() {
			continue
		}
		rel := path.Join(prefix, entry.Name())
		// Classify before the archived check: a partially migrated
		// namespace has an archive directory of its own (holding the
		// children already moved), which must not hide the children
		// still waiting under assets/{rel}.
		if isNamespaceDir(filepath.Join(vaultRoot, "assets"), rel, manifestNames) {
			if err := collectV1AssetDirNames(vaultRoot, rel, manifestNames, names); err != nil {
				return err
			}
			continue
		}
		// An existing archive for this asset means a prior (interrupted)
		// migration already moved it; assets/{rel} is its root view now.
		archived := filepath.Join(vaultRoot, ".sx", "versions", filepath.FromSlash(rel))
		if _, err := os.Stat(archived); err == nil {
			continue
		}
		// Only directories shaped like v1 assets (version subdirectories)
		// are migratable. Anything else — an empty folder, a hand-made
		// directory of loose files — is left in place untouched rather
		// than moved somewhere it can never be completed from.
		hasVersions, err := dirHasSubdirectories(filepath.Join(vaultRoot, "assets", filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		if !hasVersions {
			continue
		}
		*names = append(*names, rel)
	}
	return nil
}

// isNamespaceDir reports whether {baseDir}/{rel} is a namespace directory —
// a path segment of namespaced asset names (e.g. "opsx" for "opsx/apply") —
// rather than an asset directory. baseDir is the tree being classified:
// the v1 assets root or the v2 archive root. The manifest is the primary
// signal: a declared asset name is always an asset, and declared names
// nested under rel make it a namespace (a name that is both is rejected
// up front by assetNamespaceConflict). On-disk shape settles undeclared
// directories:
//
//   - rel holding its own list.txt → asset.
//   - a child holding list.txt BESIDE subdirectories (the v1 asset-dir
//     signature) → the children are assets → namespace. A lone list.txt
//     with no sibling subdirectories is treated as version-dir payload,
//     not a signal — though an undeclared version dir that bundles both
//     a payload list.txt and payload subdirectories can still trip this.
//   - no list.txt anywhere: a version directory holds the asset's files
//     directly, while an asset directory holds only version
//     subdirectories. So a child with regular files at its root marks
//     rel an asset, and file-less children with subdirectories mark it
//     a namespace. (An asset whose EVERY version dir has no root files
//     — no metadata.toml, no prompt file — would be misread as a
//     namespace, but such an asset has nothing recognizable to migrate
//     anyway.)
//   - anything else defaults to asset, matching pre-namespace behavior.
func isNamespaceDir(baseDir, rel string, manifestNames map[string]bool) bool {
	if manifestNames[rel] {
		return false
	}
	for name := range manifestNames {
		if strings.HasPrefix(name, rel+"/") {
			return true
		}
	}
	dir := filepath.Join(baseDir, filepath.FromSlash(rel))
	if _, err := os.Stat(filepath.Join(dir, "list.txt")); err == nil {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	anyChildFile := false
	anyChildSubdirOnly := false
	for _, entry := range filterScanEntries(entries) {
		if !entry.IsDir() {
			continue
		}
		childEntries, err := os.ReadDir(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		hasFile, hasSubdir, hasList := false, false, false
		for _, ce := range filterScanEntries(childEntries) {
			if ce.IsDir() {
				hasSubdir = true
				continue
			}
			if ce.Name() == "list.txt" {
				hasList = true
				continue
			}
			hasFile = true
		}
		// A list.txt BESIDE version subdirectories is the v1 asset-dir
		// signature — the child is an asset, so rel is a namespace. A
		// list.txt with no subdirectories next to it is just a payload
		// file inside a version directory and counts as a regular file.
		if hasList && hasSubdir {
			return true
		}
		if hasFile || hasList {
			anyChildFile = true
		} else if hasSubdir {
			anyChildSubdirOnly = true
		}
	}
	if anyChildFile {
		return false
	}
	return anyChildSubdirOnly
}

// dirHasSubdirectories reports whether dir contains at least one
// subdirectory (a v1 asset's version directories).
func dirHasSubdirectories(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return true, nil
		}
	}
	return false, nil
}

// migrateStorageToV2 converts a v1 vault's storage layout to v2 in place and
// stamps schema_version = 2 on the manifest. The caller must hold the
// vault's write lock. Returns ErrStorageUpToDate when there is nothing to
// migrate.
//
// Steps per asset: move assets/{name} (all version dirs + list.txt) to
// .sx/versions/{name}, then materialize the latest version back at
// assets/{name} as the root view. Then rewrite manifest source paths that
// pointed at v1 storage locations, set schema_version = 2, save, and append
// a vault.migrated audit event.
func migrateStorageToV2(vaultRoot string, actorEmail string) (*MigrationResult, error) {
	m, ok, err := manifest.Load(vaultRoot)
	if err != nil {
		return nil, err
	}
	if !ok || m.SchemaVersion >= 2 {
		return nil, ErrStorageUpToDate
	}

	v1, err := layout.ForVersion(layout.V1)
	if err != nil {
		return nil, err
	}
	v2, err := layout.ForVersion(layout.V2)
	if err != nil {
		return nil, err
	}

	names, err := v1AssetDirNames(vaultRoot)
	if err != nil {
		return nil, err
	}

	migrated := 0
	for _, name := range names {
		if err := migrateAssetStorage(vaultRoot, v2, name); err != nil {
			return nil, fmt.Errorf("failed to migrate asset %s: %w", name, err)
		}
		migrated++
	}
	// Complete any half-migrated assets from an interrupted earlier run:
	// their archive exists but the root view may be missing or stale.
	if err := refreshAllRootViews(vaultRoot, v2); err != nil {
		return nil, err
	}

	// Rewrite source paths that referenced v1 storage locations. The lock
	// file is resolved from the manifest, so this is also what repoints
	// every consumer's installs at the archive.
	for i := range m.Assets {
		a := &m.Assets[i]
		if a.SourcePath != nil && sourcePathsEqual(a.SourcePath.Path, v1.SourcePathRel(a.Name, a.Version)) {
			a.SourcePath.Path = v2.SourcePathRel(a.Name, a.Version)
		}
	}
	m.SchemaVersion = 2
	// Stamp created_by if the manifest never had one (hand-authored
	// vaults): lock resolution requires it, and this build is now the
	// last writer either way.
	if strings.TrimSpace(m.CreatedBy) == "" {
		m.CreatedBy = buildinfo.GetCreatedBy()
	}
	if err := manifest.Save(vaultRoot, m); err != nil {
		return nil, err
	}

	event := mgmt.AuditEvent{
		Actor:      actorEmail,
		Event:      mgmt.EventVaultMigrated,
		TargetType: mgmt.TargetTypeVault,
		Target:     "storage-format",
		Data: map[string]any{
			"from":   1,
			"to":     2,
			"assets": migrated,
		},
	}
	if err := mgmt.AppendAuditEvent(vaultRoot, event); err != nil {
		return nil, err
	}
	return &MigrationResult{Assets: migrated}, nil
}

// migrateAssetStorage moves one asset's v1 storage into the v2 archive and
// materializes its root view.
func migrateAssetStorage(vaultRoot string, v2 layout.Layout, name string) error {
	srcDir := filepath.Join(vaultRoot, "assets", name)
	dstDir := filepath.Join(vaultRoot, v2.VersionsDir(name))
	if err := os.MkdirAll(filepath.Dir(dstDir), 0755); err != nil {
		return err
	}
	if err := os.Rename(srcDir, dstDir); err != nil {
		return err
	}
	if err := ensureVersionList(vaultRoot, v2, name); err != nil {
		return err
	}
	return refreshRootView(vaultRoot, v2, name)
}

// ensureVersionList synthesizes a list.txt from the archived version
// directories when the v1 asset was missing one. Without it, the archive's
// versions would be undiscoverable after migration.
func ensureVersionList(vaultRoot string, v2 layout.Layout, name string) error {
	listPath := filepath.Join(vaultRoot, v2.VersionListPath(name))
	if _, err := os.Stat(listPath); err == nil {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(vaultRoot, v2.VersionsDir(name)))
	if err != nil {
		return err
	}
	var versions []string
	for _, entry := range entries {
		// A conflicted-copy directory dropped by a sync client must not
		// become a phantom version.
		if entry.IsDir() && !utils.IsSyncArtifact(entry.Name()) {
			versions = append(versions, entry.Name())
		}
	}
	if len(versions) == 0 {
		return nil
	}
	content := strings.Join(version.Sort(versions), "\n") + "\n"
	return os.WriteFile(listPath, []byte(content), 0644)
}

// refreshAllRootViews re-materializes the root view of every archived asset.
// Used to complete interrupted migrations and repair root-view drift.
func refreshAllRootViews(vaultRoot string, v2 layout.Layout) error {
	manifestNames, err := manifestAssetNameSet(vaultRoot)
	if err != nil {
		return err
	}
	return refreshRootViewsUnder(vaultRoot, v2, "", manifestNames)
}

// refreshRootViewsUnder refreshes the root views of the archived assets one
// level below .sx/versions/{prefix}, recursing into namespace directories.
// A namespace directory (a segment of namespaced asset names like "opsx"
// for "opsx/apply") must never be refreshed as an asset itself: it has no
// list.txt, so refreshRootView would read an empty version list and delete
// every child root view under assets/{prefix}.
func refreshRootViewsUnder(vaultRoot string, v2 layout.Layout, prefix string, manifestNames map[string]bool) error {
	entries, err := os.ReadDir(filepath.Join(vaultRoot, ".sx", "versions", filepath.FromSlash(prefix)))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	// filterScanEntries, not just an IsSyncArtifact check: the scan side
	// uses it too, and it additionally skips numbered sync-conflict copies
	// ("apply 2") — refreshing one of those would materialize a phantom
	// root view for the conflict copy.
	for _, entry := range filterScanEntries(entries) {
		if !entry.IsDir() {
			continue
		}
		rel := path.Join(prefix, entry.Name())
		if isNamespaceDir(filepath.Join(vaultRoot, ".sx", "versions"), rel, manifestNames) {
			if err := refreshRootViewsUnder(vaultRoot, v2, rel, manifestNames); err != nil {
				return err
			}
			continue
		}
		if err := refreshRootView(vaultRoot, v2, rel); err != nil {
			return err
		}
	}
	return nil
}

// ensureMigrated runs the storage migration on a path vault before a
// write, taking the vault's file lock for the duration so two processes
// sharing the path vault can't migrate concurrently. Callers already
// holding the lock use ensureMigratedHeld.
func (p *PathVault) ensureMigrated(ctx context.Context) error {
	if _, err := planStorageMigration(p.repoPath); err != nil {
		if errors.Is(err, ErrStorageUpToDate) {
			return nil
		}
		return err
	}
	fl, err := p.acquirePathLock(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = fl.Unlock() }()
	return p.ensureMigratedHeld(ctx)
}

// ensureMigratedHeld is ensureMigrated for callers that already hold the
// vault's write lock.
func (p *PathVault) ensureMigratedHeld(ctx context.Context) error {
	if _, err := planStorageMigration(p.repoPath); err != nil {
		if errors.Is(err, ErrStorageUpToDate) {
			return nil
		}
		return err
	}
	actor, err := p.CurrentActor(ctx)
	if err != nil {
		return err
	}
	if _, err := migrateStorageToV2(p.repoPath, actor.Email); err != nil && !errors.Is(err, ErrStorageUpToDate) {
		return err
	}
	return nil
}

// MigrateStorage runs the v1 → v2 storage migration explicitly
// (`sx vault migrate`). Returns ErrStorageUpToDate when the vault is
// already current. It takes the write lock directly rather than going
// through withLock, which would auto-migrate first and make this call
// always report "already migrated".
func (p *PathVault) MigrateStorage(ctx context.Context) (*MigrationResult, error) {
	fl, err := p.acquirePathLock(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = fl.Unlock() }()
	actor, err := p.CurrentActor(ctx)
	if err != nil {
		return nil, err
	}
	if err := actor.RequireRealIdentity(); err != nil {
		return nil, err
	}
	return migrateStorageToV2(p.repoPath, actor.Email)
}

// PlanStorageMigration reports what MigrateStorage would do (dry run).
func (p *PathVault) PlanStorageMigration(ctx context.Context) (*MigrationPlan, error) {
	return planStorageMigration(p.repoPath)
}

// ensureMigratedLocked runs the storage migration on a git vault before a
// direct write, committing and pushing it as its own commit so the format
// change lands atomically and independently of the write that triggered it.
// The caller must hold the vault flock and have called cloneOrUpdate.
//
// PR-branch mode skips migration entirely: a v2 migration commit inside a PR
// against a v1 default branch would be unmergeable, and PR contributors may
// lack permission to push the migration directly. Their writes stay in the
// vault's current format.
func (g *GitVault) ensureMigratedLocked(ctx context.Context) error {
	if g.prBranch != "" {
		return nil
	}
	if _, err := planStorageMigration(g.repoPath); err != nil {
		if errors.Is(err, ErrStorageUpToDate) {
			return nil
		}
		return err
	}
	actor, err := mgmt.CurrentGitActor(ctx, g.repoPath)
	if err != nil {
		return err
	}
	if _, err := migrateStorageToV2(g.repoPath, actor.Email); err != nil {
		if errors.Is(err, ErrStorageUpToDate) {
			return nil
		}
		return err
	}

	if err := g.gitClient.Add(ctx, g.repoPath, "."); err != nil {
		return err
	}
	hasChanges, err := g.gitClient.HasStagedChanges(ctx, g.repoPath)
	if err != nil {
		return err
	}
	if !hasChanges {
		return nil
	}
	if err := g.gitClient.Commit(ctx, g.repoPath, "Migrate vault storage to format v2"); err != nil {
		return err
	}
	if err := g.pushWithRebaseRetry(ctx); err != nil {
		// A concurrent client may have pushed its own migration commit,
		// which a rebase cannot reconcile (both moved the same files).
		// Discard our local migration and adopt the remote state; if the
		// remote turns out to be migrated, the caller can proceed.
		if recoverErr := g.discardLocalAndResync(ctx); recoverErr != nil {
			return fmt.Errorf("failed to push migration commit: %w (recovery also failed: %w)", err, recoverErr)
		}
		l, detectErr := detectLayout(g.repoPath)
		if detectErr != nil {
			return detectErr
		}
		if l.Version() >= layout.V2 {
			return nil
		}
		return fmt.Errorf("failed to push migration commit: %w", err)
	}
	return nil
}

// discardLocalAndResync hard-anchors the working clone back to the remote
// tip, discarding local commits. Used to recover from a lost migration race.
// The caller must hold the vault flock.
func (g *GitVault) discardLocalAndResync(ctx context.Context) error {
	// The failed push retry may have left the clone mid-rebase (the rebase
	// of our migration commit onto the winner's conflicts by construction —
	// both moved the same files). Abort it first so the branch is restored;
	// an error just means no rebase was in progress.
	_ = g.gitClient.RebaseAbort(ctx, g.repoPath)

	if err := g.gitClient.Fetch(ctx, g.repoPath); err != nil {
		return err
	}
	branch, err := g.gitClient.GetCurrentBranch(ctx, g.repoPath)
	if err != nil {
		return err
	}
	return g.gitClient.Reset(ctx, g.repoPath, "hard", "origin/"+branch)
}

// MigrateStorage runs the v1 → v2 storage migration explicitly
// (`sx vault migrate`). Returns ErrStorageUpToDate when the vault is
// already current.
func (g *GitVault) MigrateStorage(ctx context.Context) (*MigrationResult, error) {
	fileLock, err := g.acquireFileLock(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() { _ = fileLock.Unlock() }()

	if err := g.cloneOrUpdate(ctx); err != nil {
		return nil, fmt.Errorf("failed to clone/update repository: %w", err)
	}

	plan, err := planStorageMigration(g.repoPath)
	if err != nil {
		return nil, err
	}
	if err := g.ensureMigratedLocked(ctx); err != nil {
		return nil, err
	}
	return &MigrationResult{Assets: len(plan.Assets)}, nil
}

// PlanStorageMigration reports what MigrateStorage would do (dry run).
func (g *GitVault) PlanStorageMigration(ctx context.Context) (*MigrationPlan, error) {
	fileLock, err := g.acquireFileLock(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() { _ = fileLock.Unlock() }()

	if err := g.cloneOrUpdate(ctx); err != nil {
		return nil, fmt.Errorf("failed to clone/update repository: %w", err)
	}
	return planStorageMigration(g.repoPath)
}
