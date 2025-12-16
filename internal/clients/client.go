package clients

import (
	"context"

	"github.com/sleuth-io/skills/internal/asset"
	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/metadata"
)

// Client represents an AI coding client that can have artifacts installed
type Client interface {
	// Identity
	ID() string          // Machine name: "claude-code", "cursor", "cline"
	DisplayName() string // Human name: "Claude Code", "Cursor", "Cline"

	// Detection
	IsInstalled() bool  // Check if this client is installed/configured
	GetVersion() string // Get client version (empty if not available)

	// Capabilities - what artifact types this client supports
	SupportsArtifactType(artifactType asset.Type) bool

	// Installation - client has FULL control over installation mechanism
	// Receives all artifacts to install at once (batch)
	InstallArtifacts(ctx context.Context, req InstallRequest) (InstallResponse, error)

	// Uninstallation - remove artifacts
	UninstallArtifacts(ctx context.Context, req UninstallRequest) (UninstallResponse, error)

	// Skill operations - for MCP server support
	// ListSkills returns all installed skills for a given scope
	ListSkills(ctx context.Context, scope *InstallScope) ([]InstalledSkill, error)
	// ReadSkill reads the content of a specific skill by name
	ReadSkill(ctx context.Context, name string, scope *InstallScope) (*SkillContent, error)

	// EnsureSkillsSupport ensures skills infrastructure is set up for the current context.
	// This is called after installation to ensure rules files, MCP servers, etc. are configured.
	// For Cursor, this creates local .cursor/rules/skills.md with skills from all applicable scopes.
	// Clients that don't need post-install setup can return nil.
	EnsureSkillsSupport(ctx context.Context, scope *InstallScope) error

	// InstallHooks installs client-specific hooks (e.g., auto-update, usage tracking).
	// This is called during installation to set up hooks in the client's configuration.
	// Clients that don't need hooks can return nil.
	InstallHooks(ctx context.Context) error

	// UninstallHooks removes client-specific hooks installed by InstallHooks.
	// This is called during full uninstall (--all flag) to clean up system hooks.
	// Clients that don't need hooks can return nil.
	UninstallHooks(ctx context.Context) error

	// ShouldInstall checks if installation should proceed in hook mode.
	// Returns true to proceed, false to skip.
	// Called before any installation work begins.
	// For clients like Cursor that fire hooks on every prompt, this enables
	// tracking conversation IDs to only run install once per conversation.
	ShouldInstall(ctx context.Context) (bool, error)

	// VerifyArtifacts checks if artifacts are actually installed (not just tracked).
	// Used by --repair mode to detect discrepancies between tracker and filesystem.
	// Each client implements verification according to its own installation structure.
	VerifyArtifacts(ctx context.Context, artifacts []*lockfile.Artifact, scope *InstallScope) []VerifyResult
}

// InstalledSkill represents a skill that has been installed
type InstalledSkill struct {
	Name        string // Skill name
	Description string // Skill description from metadata
	Version     string // Skill version
}

// SkillContent contains the full content of a skill for MCP responses
type SkillContent struct {
	Name        string // Skill name
	Description string // Skill description from metadata
	Version     string // Skill version from metadata
	Content     string // Contents of SKILL.md (or configured prompt file)
	BaseDir     string // Directory where skill is installed (for resolving @ file references)
}

// InstallRequest contains everything needed for installation
type InstallRequest struct {
	Artifacts []*ArtifactBundle // All artifacts to install (batch)
	Scope     *InstallScope     // Where to install (global/repo/path)
	Options   InstallOptions    // Additional options
}

// ArtifactBundle contains artifact + metadata + zip data
type ArtifactBundle struct {
	Artifact *lockfile.Artifact
	Metadata *metadata.Metadata
	ZipData  []byte
}

// InstallScope defines where artifacts should be installed
type InstallScope struct {
	Type     ScopeType // Global, Repository, Path
	RepoRoot string    // Repository root (if applicable)
	RepoURL  string    // Repository URL (if applicable)
	Path     string    // Specific path within repo (if applicable)
}

type ScopeType string

const (
	ScopeGlobal     ScopeType = "global"
	ScopeRepository ScopeType = "repository"
	ScopePath       ScopeType = "path"
)

// InstallOptions contains optional installation settings
type InstallOptions struct {
	Force   bool // Force reinstall even if already installed
	DryRun  bool // Don't actually install, just validate
	Verbose bool // Verbose output
}

// InstallResponse contains results per artifact
type InstallResponse struct {
	Results []ArtifactResult
}

// UninstallRequest contains artifacts to uninstall
type UninstallRequest struct {
	Artifacts []asset.Asset
	Scope     *InstallScope
	Options   UninstallOptions
}

type UninstallOptions struct {
	Force   bool // Force uninstall even if dependencies exist
	DryRun  bool // Don't actually uninstall
	Verbose bool // Verbose output
}

// UninstallResponse contains results per artifact
type UninstallResponse struct {
	Results []ArtifactResult
}

// ArtifactResult represents the result of installing/uninstalling one artifact
type ArtifactResult struct {
	ArtifactName string
	Status       ResultStatus
	Message      string
	Error        error
}

type ResultStatus string

const (
	StatusSuccess ResultStatus = "success"
	StatusFailed  ResultStatus = "failed"
	StatusSkipped ResultStatus = "skipped"
)

// VerifyResult represents the result of verifying a single artifact's installation
type VerifyResult struct {
	Artifact  *lockfile.Artifact // The artifact that was verified
	Installed bool               // Whether the artifact is actually installed correctly
	Message   string             // Details about what was found or missing
}

// BaseClient provides default implementations for common functionality
type BaseClient struct {
	id           string
	displayName  string
	capabilities map[string]bool
}

func (b *BaseClient) ID() string          { return b.id }
func (b *BaseClient) DisplayName() string { return b.displayName }

func (b *BaseClient) SupportsArtifactType(artifactType asset.Type) bool {
	return b.capabilities[artifactType.Key]
}

// NewBaseClient creates a new base client with capabilities
func NewBaseClient(id, displayName string, supportedTypes []asset.Type) BaseClient {
	capabilities := make(map[string]bool)
	for _, t := range supportedTypes {
		capabilities[t.Key] = true
	}
	return BaseClient{
		id:           id,
		displayName:  displayName,
		capabilities: capabilities,
	}
}
