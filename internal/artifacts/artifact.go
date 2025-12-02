package artifacts

import (
	"context"

	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/metadata"
)

// InstallRequest represents a request to install artifacts
type InstallRequest struct {
	LockFile    *lockfile.LockFile
	ClientName  string // Client to filter by (e.g., "claude-code")
	Scope       *Scope // Current scope context
	TargetBase  string // Base directory for installation (e.g., ~/.claude/)
	CacheDir    string // Cache directory for artifacts
	Concurrency int    // Max concurrent downloads (default: 10)
}

// InstallResult represents the result of an installation
type InstallResult struct {
	Installed []string // Successfully installed artifacts
	Failed    []string // Failed artifacts
	Errors    []error  // Errors encountered
}

// Scope represents the current working context for scope matching
type Scope struct {
	Type     string // "global", "repo", or "path"
	RepoURL  string // Repository URL (if in a repo)
	RepoPath string // Path relative to repo root (if applicable)
}

// ArtifactWithMetadata combines lockfile artifact with parsed metadata
type ArtifactWithMetadata struct {
	Artifact *lockfile.Artifact
	Metadata *metadata.Metadata
	ZipData  []byte
}

// DownloadTask represents a single artifact download task
type DownloadTask struct {
	Artifact *lockfile.Artifact
	Index    int
}

// DownloadResult represents the result of downloading an artifact
type DownloadResult struct {
	Artifact *lockfile.Artifact
	ZipData  []byte
	Metadata *metadata.Metadata
	Error    error
	Index    int
}

// InstallTask represents a single artifact installation task
type InstallTask struct {
	Artifact *lockfile.Artifact
	ZipData  []byte
	Metadata *metadata.Metadata
}

// Fetcher defines the interface for fetching artifacts
type Fetcher interface {
	// FetchArtifact downloads a single artifact
	FetchArtifact(ctx context.Context, artifact *lockfile.Artifact) (zipData []byte, meta *metadata.Metadata, err error)

	// FetchArtifacts downloads multiple artifacts in parallel
	FetchArtifacts(ctx context.Context, artifacts []*lockfile.Artifact, concurrency int) ([]DownloadResult, error)
}

// Installer defines the interface for installing artifacts
type Installer interface {
	// Install installs a single artifact
	Install(ctx context.Context, artifact *lockfile.Artifact, zipData []byte, metadata *metadata.Metadata) error

	// InstallAll installs multiple artifacts in dependency order
	InstallAll(ctx context.Context, artifacts []*ArtifactWithMetadata) (*InstallResult, error)

	// Remove removes a single artifact
	Remove(ctx context.Context, artifact *lockfile.Artifact) error
}
