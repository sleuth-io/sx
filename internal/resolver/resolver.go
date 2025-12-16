package resolver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/buildinfo"
	"github.com/sleuth-io/sx/internal/git"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/requirements"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
	"github.com/sleuth-io/sx/internal/version"
)

// Resolver resolves requirements to lock file assets
type Resolver struct {
	vault vaultpkg.Vault
	ctx   context.Context
}

// New creates a new resolver
func New(ctx context.Context, vault vaultpkg.Vault) *Resolver {
	return &Resolver{
		vault: vault,
		ctx:   ctx,
	}
}

// Resolve resolves a list of requirements to lock file assets
func (r *Resolver) Resolve(reqs []requirements.Requirement) (*lockfile.LockFile, error) {
	// Map to track resolved assets by name
	resolved := make(map[string]*lockfile.Asset)
	// Queue of assets to process (for dependency resolution)
	queue := make([]requirements.Requirement, len(reqs))
	copy(queue, reqs)

	// Track what we're processing to detect circular dependencies
	processing := make(map[string]bool)

	for len(queue) > 0 {
		req := queue[0]
		queue = queue[1:]

		// Determine the name for this requirement
		var name string
		switch req.Type {
		case requirements.RequirementTypeRegistry:
			name = req.Name
		case requirements.RequirementTypeGit:
			name = req.GitName
		case requirements.RequirementTypePath, requirements.RequirementTypeHTTP:
			// For path/HTTP, we need to download and extract to get the name
			// We'll handle this specially
			name = ""
		}

		// Skip if already resolved
		if name != "" && resolved[name] != nil {
			continue
		}

		// Check for circular dependencies
		if processing[name] {
			return nil, fmt.Errorf("circular dependency detected: %s", name)
		}
		processing[name] = true

		// Resolve this requirement
		asset, deps, err := r.resolveRequirement(req)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve %s: %w", req.String(), err)
		}

		// Add to resolved map
		resolved[asset.Name] = asset

		// Add dependencies to queue
		queue = append(queue, deps...)

		delete(processing, name)
	}

	// Build lock file
	lockFile := &lockfile.LockFile{
		LockVersion: "1.0",
		Version:     generateLockFileVersion(resolved),
		CreatedBy:   buildinfo.GetCreatedBy(),
		Assets:      make([]lockfile.Asset, 0, len(resolved)),
	}

	for _, asset := range resolved {
		lockFile.Assets = append(lockFile.Assets, *asset)
	}

	return lockFile, nil
}

// resolveRequirement resolves a single requirement
func (r *Resolver) resolveRequirement(req requirements.Requirement) (*lockfile.Asset, []requirements.Requirement, error) {
	switch req.Type {
	case requirements.RequirementTypeRegistry:
		return r.resolveRegistry(req)
	case requirements.RequirementTypeGit:
		return r.resolveGit(req)
	case requirements.RequirementTypePath:
		return r.resolvePath(req)
	case requirements.RequirementTypeHTTP:
		return r.resolveHTTP(req)
	default:
		return nil, nil, fmt.Errorf("unknown requirement type: %s", req.Type)
	}
}

// resolveRegistry resolves a registry asset
func (r *Resolver) resolveRegistry(req requirements.Requirement) (*lockfile.Asset, []requirements.Requirement, error) {
	// Get available versions
	versions, err := r.vault.GetVersionList(r.ctx, req.Name)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get version list: %w", err)
	}

	// Filter by version specifier
	var matchedVersions []string
	if req.VersionSpec != "" {
		// Parse specifiers
		specs, err := version.ParseMultipleSpecifiers(req.VersionOperator + req.VersionSpec)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid version specifier: %w", err)
		}

		matchedVersions, err = version.FilterByMultiple(versions, specs)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to filter versions: %w", err)
		}
	} else {
		matchedVersions = versions
	}

	if len(matchedVersions) == 0 {
		return nil, nil, fmt.Errorf("no matching versions found for %s%s%s", req.Name, req.VersionOperator, req.VersionSpec)
	}

	// Select best version
	selectedVersion, err := version.SelectBest(matchedVersions)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to select best version: %w", err)
	}

	// Get metadata for selected version
	meta, err := r.vault.GetMetadata(r.ctx, req.Name, selectedVersion)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get metadata for %s@%s: %w", req.Name, selectedVersion, err)
	}

	// Build asset
	asset := &lockfile.Asset{
		Name:    req.Name,
		Version: selectedVersion,
		Type:    meta.Asset.Type,
		SourceHTTP: &lockfile.SourceHTTP{
			URL: r.buildAssetURL(req.Name, selectedVersion),
			// Hashes will be computed during download in actual implementation
			// For now, we'll leave them empty and compute on demand
			Hashes: make(map[string]string),
		},
	}

	// Parse dependencies
	var deps []requirements.Requirement
	for _, depStr := range meta.Asset.Dependencies {
		depReq, err := requirements.ParseLine(depStr)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid dependency %s: %w", depStr, err)
		}
		deps = append(deps, depReq)
	}

	return asset, deps, nil
}

// resolveGit resolves a git source asset
func (r *Resolver) resolveGit(req requirements.Requirement) (*lockfile.Asset, []requirements.Requirement, error) {
	// Resolve ref to commit SHA
	commitSHA, err := r.resolveGitRef(req.GitURL, req.GitRef)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve git ref: %w", err)
	}

	// For now, we'll create a minimal asset
	// In a full implementation, we'd clone the repo and extract metadata
	resolvedAsset := &lockfile.Asset{
		Name:    req.GitName,
		Version: "0.0.0+git" + commitSHA[:7],
		Type:    asset.TypeSkill, // Default, should be read from metadata
		SourceGit: &lockfile.SourceGit{
			URL:          req.GitURL,
			Ref:          commitSHA,
			Subdirectory: req.GitSubdirectory,
		},
	}

	// TODO: Clone repo, extract metadata, parse dependencies
	return resolvedAsset, nil, nil
}

// resolvePath resolves a local path asset
func (r *Resolver) resolvePath(req requirements.Requirement) (*lockfile.Asset, []requirements.Requirement, error) {
	// Expand path
	path := req.Path
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		path = filepath.Join(home, path[2:])
	}

	// Check if file exists
	if _, err := os.Stat(path); err != nil {
		return nil, nil, fmt.Errorf("path not found: %w", err)
	}

	// Read metadata from zip
	// For now, create a minimal asset
	resolvedAsset := &lockfile.Asset{
		Name:    filepath.Base(path),
		Version: "0.0.0+local",
		Type:    asset.TypeSkill,
		SourcePath: &lockfile.SourcePath{
			Path: req.Path, // Use original path, not expanded
		},
	}

	// TODO: Extract metadata from zip, parse dependencies
	return resolvedAsset, nil, nil
}

// resolveHTTP resolves an HTTP source asset
func (r *Resolver) resolveHTTP(req requirements.Requirement) (*lockfile.Asset, []requirements.Requirement, error) {
	// Download asset
	resp, err := http.Get(req.URL)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to download asset: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("HTTP %d: failed to download asset", resp.StatusCode)
	}

	// Read data and compute hash
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read asset data: %w", err)
	}

	hash := sha256.Sum256(data)
	hashStr := hex.EncodeToString(hash[:])

	// Extract name from URL
	name := filepath.Base(req.URL)
	name = strings.TrimSuffix(name, ".zip")

	resolvedAsset := &lockfile.Asset{
		Name:    name,
		Version: "0.0.0+http",
		Type:    asset.TypeSkill,
		SourceHTTP: &lockfile.SourceHTTP{
			URL: req.URL,
			Hashes: map[string]string{
				"sha256": hashStr,
			},
			Size: int64(len(data)),
		},
	}

	// TODO: Extract metadata from zip, parse dependencies
	return resolvedAsset, nil, nil
}

// resolveGitRef resolves a git ref (branch, tag, or commit) to a commit SHA
func (r *Resolver) resolveGitRef(url, ref string) (string, error) {
	// Use git client to resolve the ref
	gitClient := git.NewClient()
	return gitClient.LsRemote(context.Background(), url, ref)
}

// buildAssetURL builds the URL for an asset based on repository conventions
func (r *Resolver) buildAssetURL(name, version string) string {
	// This should use the repository's base URL
	// For now, return a placeholder that follows the spec
	return fmt.Sprintf("https://app.skills.new/api/skills/assets/%s/%s/%s-%s.zip", name, version, name, version)
}

// generateLockFileVersion generates a version/hash for the lock file
func generateLockFileVersion(assets map[string]*lockfile.Asset) string {
	// Create a deterministic hash of all assets
	h := sha256.New()

	// Sort asset names for deterministic output
	var names []string
	for name := range assets {
		names = append(names, name)
	}

	// Simple hash of asset keys
	for _, name := range names {
		asset := assets[name]
		fmt.Fprintf(h, "%s@%s\n", asset.Name, asset.Version)
	}

	hash := h.Sum(nil)
	return hex.EncodeToString(hash[:16]) // Use first 16 bytes
}
