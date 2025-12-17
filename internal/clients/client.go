package clients

import (
	"context"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/metadata"
)

// Client represents an AI coding client that can have assets installed
type Client interface {
	// Identity
	ID() string          // Machine name: "claude-code", "cursor", "cline"
	DisplayName() string // Human name: "Claude Code", "Cursor", "Cline"

	// Detection
	IsInstalled() bool  // Check if this client is installed/configured
	GetVersion() string // Get client version (empty if not available)

	// Capabilities - what asset types this client supports
	SupportsAssetType(assetType asset.Type) bool

	// Installation - client has FULL control over installation mechanism
	// Receives all assets to install at once (batch)
	InstallAssets(ctx context.Context, req InstallRequest) (InstallResponse, error)

	// Uninstallation - remove assets
	UninstallAssets(ctx context.Context, req UninstallRequest) (UninstallResponse, error)

	// Asset operations - for MCP server support
	// ListAssets returns all installed assets for a given scope
	ListAssets(ctx context.Context, scope *InstallScope) ([]InstalledSkill, error)
	// ReadSkill reads the content of a specific skill by name
	ReadSkill(ctx context.Context, name string, scope *InstallScope) (*SkillContent, error)

	// EnsureAssetSupport ensures asset infrastructure is set up for the current context.
	// This is called after installation to ensure rules files, MCP servers, etc. are configured.
	// For Cursor, this creates local .cursor/rules/skills.md with skills from all applicable scopes.
	// Clients that don't need post-install setup can return nil.
	EnsureAssetSupport(ctx context.Context, scope *InstallScope) error

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

	// VerifyAssets checks if assets are actually installed (not just tracked).
	// Used by --repair mode to detect discrepancies between tracker and filesystem.
	// Each client implements verification according to its own installation structure.
	VerifyAssets(ctx context.Context, assets []*lockfile.Asset, scope *InstallScope) []VerifyResult
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
	Assets  []*AssetBundle // All assets to install (batch)
	Scope   *InstallScope  // Where to install (global/repo/path)
	Options InstallOptions // Additional options
}

// AssetBundle contains asset + metadata + zip data
type AssetBundle struct {
	Asset    *lockfile.Asset
	Metadata *metadata.Metadata
	ZipData  []byte
}

// InstallScope defines where assets should be installed
type InstallScope struct {
	Type     ScopeType // Global, Repository, Path
	RepoRoot string    // Repository root (if applicable)
	RepoURL  string    // Repository URL (if applicable)
	Path     string    // Specific path within repo (if applicable)
}

type ScopeType string

const (
	ScopeGlobal     ScopeType = "global"
	ScopeRepository ScopeType = "repo" // Must match lockfile.ScopeRepo
	ScopePath       ScopeType = "path"
)

// ClientID constants for supported AI coding clients
const (
	ClientIDClaudeCode = "claude-code"
	ClientIDCursor     = "cursor"
)

// AllClientIDs returns all known client IDs
func AllClientIDs() []string {
	return []string{ClientIDClaudeCode, ClientIDCursor}
}

// IsValidClientID checks if the given ID is a known client ID
func IsValidClientID(id string) bool {
	for _, valid := range AllClientIDs() {
		if id == valid {
			return true
		}
	}
	return false
}

// InstallOptions contains optional installation settings
type InstallOptions struct {
	Force   bool // Force reinstall even if already installed
	DryRun  bool // Don't actually install, just validate
	Verbose bool // Verbose output
}

// InstallResponse contains results per asset
type InstallResponse struct {
	Results []AssetResult
}

// UninstallRequest contains assets to uninstall
type UninstallRequest struct {
	Assets  []asset.Asset
	Scope   *InstallScope
	Options UninstallOptions
}

type UninstallOptions struct {
	Force   bool // Force uninstall even if dependencies exist
	DryRun  bool // Don't actually uninstall
	Verbose bool // Verbose output
}

// UninstallResponse contains results per asset
type UninstallResponse struct {
	Results []AssetResult
}

// AssetResult represents the result of installing/uninstalling one asset
type AssetResult struct {
	AssetName string
	Status    ResultStatus
	Message   string
	Error     error
}

type ResultStatus string

const (
	StatusSuccess ResultStatus = "success"
	StatusFailed  ResultStatus = "failed"
	StatusSkipped ResultStatus = "skipped"
)

// VerifyResult represents the result of verifying a single asset's installation
type VerifyResult struct {
	Asset     *lockfile.Asset // The asset that was verified
	Installed bool            // Whether the asset is actually installed correctly
	Message   string          // Details about what was found or missing
}

// BaseClient provides default implementations for common functionality
type BaseClient struct {
	id           string
	displayName  string
	capabilities map[string]bool
}

func (b *BaseClient) ID() string          { return b.id }
func (b *BaseClient) DisplayName() string { return b.displayName }

func (b *BaseClient) SupportsAssetType(assetType asset.Type) bool {
	return b.capabilities[assetType.Key]
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
