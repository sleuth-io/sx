package vault

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/bootstrap"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/mgmt"
)

// ErrLockFileNotFound is returned when the lock file does not exist in the vault
var ErrLockFileNotFound = errors.New("lock file not found")

// ErrNotImplemented is returned by vaults that do not support a given
// management operation.
var ErrNotImplemented = errors.New("operation not supported for this vault type")

// ErrVersionExists is returned when attempting to add an asset version that already exists
type ErrVersionExists struct {
	Name    string
	Version string
	Message string
}

func (e *ErrVersionExists) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("version %s already exists for asset %s", e.Version, e.Name)
}

// Vault represents a source of assets with read and write capabilities
// This interface unifies the concepts of "vault" and "source fetcher"
type Vault interface {
	// Authenticate performs authentication with the repository
	// Returns an auth token or empty string if no auth needed
	Authenticate(ctx context.Context) (string, error)

	// GetLockFile retrieves the lock file from the repository
	// Returns lock file content and ETag for caching
	// If cachedETag matches, returns notModified=true with empty content
	GetLockFile(ctx context.Context, cachedETag string) (content []byte, etag string, notModified bool, err error)

	// GetAsset downloads an asset using its source configuration from the lock file
	// The asset parameter contains the source configuration (source-http, source-git, source-path)
	GetAsset(ctx context.Context, asset *lockfile.Asset) ([]byte, error)

	// AddAsset uploads an asset to the repository
	AddAsset(ctx context.Context, asset *lockfile.Asset, zipData []byte) error

	// SetInstallations configures where an asset should be installed
	// Updates the lock file with the installation scopes
	// scopeEntity is a vault-specific value from ScopeOptionProvider (e.g., "personal").
	// Empty string means standard global/repo scoping via asset.Scopes.
	SetInstallations(ctx context.Context, asset *lockfile.Asset, scopeEntity string) error

	// InheritInstallations preserves existing installation scopes for an asset.
	// Called when no scope flags are provided (e.g., `sx add ./skill --yes`).
	// For server-managed vaults (Sleuth), this is a no-op since the server
	// auto-inherits installations when a new version is uploaded.
	// For file-based vaults (Path, Git), this copies scopes from any existing
	// version of the asset in the lock file.
	InheritInstallations(ctx context.Context, asset *lockfile.Asset) error

	// GetVersionList retrieves available versions for an asset (for resolution)
	// Only applicable to repositories with version management (Sleuth, not Git)
	GetVersionList(ctx context.Context, name string) ([]string, error)

	// GetMetadata retrieves metadata for a specific asset version
	GetMetadata(ctx context.Context, name, version string) (*metadata.Metadata, error)

	// GetAssetByVersion downloads an asset by name and version
	// Used for comparing content when adding assets
	GetAssetByVersion(ctx context.Context, name, version string) ([]byte, error)

	// VerifyIntegrity checks hashes and sizes for downloaded assets
	VerifyIntegrity(data []byte, hashes map[string]string, size int64) error

	// PostUsageStats sends asset usage statistics to the repository
	// jsonlData is newline-separated JSON (JSONL format)
	PostUsageStats(ctx context.Context, jsonlData string) error

	// RemoveAsset removes an asset from the lock file.
	// If delete is true, also permanently removes the asset files from the vault.
	// If version is empty, removes all versions of the asset.
	RemoveAsset(ctx context.Context, assetName, version string, delete bool) error

	// RenameAsset renames an asset in the vault.
	// All versions and installations are preserved under the new name.
	RenameAsset(ctx context.Context, oldName, newName string) error

	// ListAssets returns a list of all assets in the vault
	// This enables asset discovery via `sx vault list`
	ListAssets(ctx context.Context, opts ListAssetsOptions) (*ListAssetsResult, error)

	// GetAssetDetails returns detailed information about a specific asset
	// This enables asset inspection via `sx vault show <name>`
	GetAssetDetails(ctx context.Context, name string) (*AssetDetails, error)

	// GetMCPTools returns additional MCP tools provided by this vault
	// Returns nil if the vault doesn't provide any MCP tools
	GetMCPTools() any

	// GetBootstrapOptions returns bootstrap options provided by this vault
	// These are options for MCP servers or other infrastructure the vault provides
	GetBootstrapOptions(ctx context.Context) []bootstrap.Option

	// CurrentActor returns the identity of the caller as resolved by this
	// vault. For git/path vaults this comes from `git config user.email`;
	// for sleuth vaults it comes from the authenticated user token.
	CurrentActor(ctx context.Context) (mgmt.Actor, error)

	// Team management
	ListTeams(ctx context.Context) ([]mgmt.Team, error)
	GetTeam(ctx context.Context, name string) (*mgmt.Team, error)
	CreateTeam(ctx context.Context, team mgmt.Team) error
	UpdateTeam(ctx context.Context, team mgmt.Team) error
	DeleteTeam(ctx context.Context, name string) error
	AddTeamMember(ctx context.Context, team, email string, admin bool) error
	RemoveTeamMember(ctx context.Context, team, email string) error
	SetTeamAdmin(ctx context.Context, team, email string, admin bool) error
	AddTeamRepository(ctx context.Context, team, repoURL string) error
	RemoveTeamRepository(ctx context.Context, team, repoURL string) error

	// Bot management. Bots are non-human service identities. They gain
	// repo context through teams (the same way human team members do)
	// and can also be a direct install target via InstallKindBot.
	ListBots(ctx context.Context) ([]mgmt.Bot, error)
	GetBot(ctx context.Context, name string) (*mgmt.Bot, error)
	// CreateBot creates a new bot. The returned rawToken is non-empty
	// only on Sleuth vaults, which auto-issue a default API key as part
	// of bot creation; file-based vaults treat bots as identity-only and
	// always return "". Callers print the token (shown once) when
	// non-empty so the auto-issued key is not silently wasted.
	CreateBot(ctx context.Context, bot mgmt.Bot) (rawToken string, err error)
	UpdateBot(ctx context.Context, bot mgmt.Bot) error
	DeleteBot(ctx context.Context, name string) error
	AddBotTeam(ctx context.Context, bot, team string) error
	RemoveBotTeam(ctx context.Context, bot, team string) error

	// SetAssetInstallation records a new installation target for an
	// asset. File-backed vaults append the target to the asset's scope
	// list in sx.toml; Sleuth vaults delegate to the server.
	SetAssetInstallation(ctx context.Context, assetName string, target InstallTarget) error

	// ClearAssetInstallations removes every installation target from an
	// asset. Soft no-op if the asset is absent from the vault.
	ClearAssetInstallations(ctx context.Context, assetName string) error

	// RecordUsageEvents appends usage events to the vault's persistent
	// usage log. Replaces the string-based PostUsageStats for new code.
	RecordUsageEvents(ctx context.Context, events []mgmt.UsageEvent) error

	// GetUsageStats returns an aggregated usage summary across the vault.
	GetUsageStats(ctx context.Context, filter mgmt.UsageFilter) (*mgmt.UsageSummary, error)

	// QueryAuditEvents returns audit events matching the filter.
	QueryAuditEvents(ctx context.Context, filter mgmt.AuditFilter) ([]mgmt.AuditEvent, error)
}

// InstallKind identifies which kind of installation a CLI command is asking
// the vault to record.
type InstallKind string

const (
	InstallKindOrg  InstallKind = "org"
	InstallKindRepo InstallKind = "repo"
	InstallKindPath InstallKind = "path"
	InstallKindTeam InstallKind = "team"
	InstallKindUser InstallKind = "user"
	InstallKindBot  InstallKind = "bot"
)

// InstallTarget describes a single installation target for an asset. Only
// fields relevant to the chosen Kind need to be set.
type InstallTarget struct {
	Kind  InstallKind
	Repo  string   // Repo and Path
	Paths []string // Path
	Team  string   // Team
	User  string   // User (email)
	Bot   string   // Bot (name)
}

// AuditData returns the payload attached to an install.set audit event
// for this target. Single source of truth for what each kind records.
func (t InstallTarget) AuditData() map[string]any {
	data := map[string]any{"kind": string(t.Kind)}
	switch t.Kind {
	case InstallKindOrg:
		// org-wide install carries no extra data
	case InstallKindRepo:
		data["repo"] = t.Repo
	case InstallKindPath:
		data["repo"] = t.Repo
		data["paths"] = t.Paths
	case InstallKindTeam:
		data["team"] = t.Team
	case InstallKindUser:
		data["user"] = t.User
	case InstallKindBot:
		data["bot"] = t.Bot
	}
	return data
}

// Describe returns a short human-readable summary of the target, suitable
// for commit messages and CLI output.
func (t InstallTarget) Describe() string {
	switch t.Kind {
	case InstallKindOrg:
		return "org (global)"
	case InstallKindRepo:
		return "repo " + t.Repo
	case InstallKindPath:
		return fmt.Sprintf("path %s#%s", t.Repo, strings.Join(t.Paths, ","))
	case InstallKindTeam:
		return "team " + t.Team
	case InstallKindUser:
		return "user " + t.User
	case InstallKindBot:
		return "bot " + t.Bot
	}
	return string(t.Kind)
}

// BotApiKeyManager is implemented by vaults that issue API tokens for
// bot identities. File-based vaults (path/git) do not implement this —
// their trust model is "vault read access ⇒ asset access" so bots are
// identity-only there. Sleuth vaults issue real OAuth tokens via the
// existing skills.new createBotApiKey mutation.
type BotApiKeyManager interface {
	CreateBotApiKey(ctx context.Context, botName, label string) (rawToken string, key mgmt.BotApiKey, err error)
	ListBotApiKeys(ctx context.Context, botName string) ([]mgmt.BotApiKey, error)
	DeleteBotApiKey(ctx context.Context, botName, keyID string) error
}

// ScopeOption represents a vault-specific scope option (e.g., "personal", "team")
// displayed in the interactive UI alongside the built-in global/repo options.
type ScopeOption struct {
	Label       string // Display text (e.g., "Just for me")
	Value       string // Machine value passed to SetInstallations
	Description string // Help text
}

// ScopeOptionProvider is implemented by vaults that provide additional scope options
// beyond global and per-repository scoping.
type ScopeOptionProvider interface {
	GetScopeOptions() []ScopeOption
}

// SourceHandler handles fetching assets from specific source types
// This is used internally by Vault implementations to handle different source types
type SourceHandler interface {
	// Fetch retrieves asset data from the source
	Fetch(ctx context.Context, asset *lockfile.Asset) ([]byte, error)
}

// ListAssetsOptions contains options for listing vault assets
type ListAssetsOptions struct {
	Type   string // Filter by asset type (skill, mcp, etc.)
	Search string // Search query for filtering assets
	Limit  int    // Maximum number of assets to return (default 100)
}

// AssetSummary contains summary information about a vault asset
type AssetSummary struct {
	Name          string
	Type          asset.Type
	LatestVersion string
	VersionsCount int
	Description   string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ListAssetsResult contains the results of a ListAssets call
type ListAssetsResult struct {
	Assets []AssetSummary
}

// AssetVersion contains version information for an asset
type AssetVersion struct {
	Version    string
	CreatedAt  time.Time
	FilesCount int
}

// AssetDetails contains detailed information about a specific asset
type AssetDetails struct {
	Name        string
	Type        asset.Type
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Versions    []AssetVersion
	Metadata    *metadata.Metadata // Metadata for latest version (or nil if not available)
}
