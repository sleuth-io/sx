// Package sxvault exposes a small, stable management facade over SX vaults.
//
// Scope: this facade currently covers the publish / manage write path
// (OpenSkillsNew / OpenGit / OpenPath constructors; EnsureBot, PutAgent,
// PutSkillZip, InstallAssetToBot mutators; ListAssets read-only browse).
// Read-side primitives that the internal vault.Vault interface supports —
// GetMetadata, GetAssetByVersion, RemoveAsset, RenameAsset, asset-uninstall —
// are intentionally NOT re-exported yet and are reserved for a follow-up
// release once consumer needs are clearer. Library consumers needing them
// today should treat the absence as a "not yet" rather than a "never."
package sxvault

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/git"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/mgmt"
	"github.com/sleuth-io/sx/internal/utils"
	"github.com/sleuth-io/sx/internal/vault"
)

const DefaultSkillsNewURL = "https://app.skills.new"

type Actor struct {
	// Name is the human-readable name used for git commit attribution by
	// OpenGit. The Sleuth backend (OpenSkillsNewWithOptions) ignores it and
	// identifies the actor solely by Email.
	Name string
	// Email identifies the actor for audit/identity context across both
	// backends. The git backend also uses it for commit attribution.
	Email string
}

type GitOptions struct {
	// AuthToken authenticates HTTP(S) git remotes through basic auth. SSH
	// remotes ignore this value and use the caller's SSH configuration.
	// SSHKeyPath takes precedence: when both are set with an HTTP(S) URL,
	// the underlying git client rewrites the URL to SSH and AuthToken is
	// not used.
	AuthToken string

	// AuthUsername is the HTTP(S) basic-auth username to pair with AuthToken.
	// Empty uses a host-specific default, currently "x-access-token" except
	// gitlab.com / *.gitlab.com, which use "oauth2". Self-hosted GitLab
	// instances on custom domains are NOT auto-detected — set this to
	// "oauth2" explicitly when targeting one, or git authentication will
	// fail with the default "x-access-token".
	AuthUsername string

	// SSHKeyPath, when non-empty, points the underlying git client at this
	// SSH private key for SSH remotes. It bypasses the process-global SSH
	// key path set by the CLI's --ssh-key flag / SX_SSH_KEY env var, letting
	// library consumers scope SSH auth to a single Client.
	//
	// When set with an HTTP(S) URL, the git client rewrites the URL to SSH
	// at clone time and SSH key auth is what's used end-to-end —
	// AuthToken is ignored on that combination. buildGitClientOptions
	// drops the basic-auth env in this case so the resulting client carries
	// no inert/conflicting credentials.
	SSHKeyPath string

	Actor Actor
}

// SkillsNewOptions configures OpenSkillsNewWithOptions. Sleuth-backed vaults
// do not use the SSH key / basic-auth knobs that GitOptions carries, so this
// type intentionally exposes a narrower surface.
type SkillsNewOptions struct {
	// AuthToken is the bearer token sent on every Sleuth API request.
	// Required — OpenSkillsNewWithOptions returns an error when empty.
	// There is no host-specific default (unlike GitOptions.AuthUsername),
	// because Sleuth endpoints accept exactly one credential shape.
	AuthToken string
	// Actor identifies the caller for audit/identity context propagated
	// via mgmt.ContextWithIdentity. Only Actor.Email is consumed on the
	// Sleuth path — Actor.Name is ignored here (see Actor.Name doc).
	Actor Actor
}

type Client struct {
	v     vault.Vault
	actor Actor
}

type Bot struct {
	Name string
	// Description is the bot's human-readable identity.
	//
	// On UPDATE (bot already exists): EnsureBot rewrites the stored
	// description only when Description is non-empty and differs from the
	// current value. An empty Description preserves whatever is stored
	// — useful for re-running EnsureBot from an agent-publish flow
	// without re-stamping identity.
	//
	// On CREATE (bot doesn't exist yet): EnsureBot writes Description
	// verbatim, so an empty Description produces a bot with no
	// description at all. Pre-seed identity with an explicit
	// EnsureBot(Bot{Name: ..., Description: "..."}) once before relying
	// on the "empty preserves" path.
	Description string
}

type AgentSpec struct {
	BotName   string
	AssetName string
	Version   string
	// Description is the agent asset's description, written into the asset
	// metadata.toml. It is separate from the bot's identity description —
	// publishing multiple agents on the same bot should give each its own
	// Description without rewriting the bot's identity each time.
	Description string
	// BotDescription, when non-empty, becomes the bot's description (on
	// create, or as an update when it differs from the stored value). Leave
	// empty to avoid rewriting an existing bot's description on every
	// publish; pre-create the bot via EnsureBot if you want bot identity
	// fully controlled outside the agent-publish path.
	BotDescription string
	Prompt         string
	// Skills lists existing vault skills to also install on BotName
	// alongside the agent. Each entry must refer to a skill (asset type
	// "skill") already present in the vault — PutAgent verifies this and
	// fails fast on missing names or on a name that resolves to a
	// different asset type. Empty entries are dropped and duplicates are
	// collapsed. Skills are installed on the bot but are NOT persisted
	// into the agent asset's metadata.toml; re-publishing the agent into
	// another vault requires re-supplying Skills.
	//
	// Semantics on re-publish are additive only. Re-publishing the same
	// agent with a shorter Skills list leaves the previously-installed
	// skills attached to BotName — PutAgent never uninstalls. To remove
	// a skill from a bot, call the uninstall path explicitly (out of
	// scope for this facade today).
	//
	// Cost note for git vaults: each skill install is a separate manifest
	// commit + push, so an agent with N skills produces roughly N+2
	// commits per PutAgent call. Keep skill lists modest or expect the
	// publish latency to grow linearly.
	Skills []string
}

type AgentResult struct {
	// BotKey is the one-time raw bot API token returned only when PutAgent
	// creates a new bot in a vault type that issues bot tokens. It is empty
	// when the bot already exists and for file-backed Git vaults.
	BotKey string
}

type SkillZipSpec struct {
	Name    string
	Version string
	// Description overrides metadata.toml's skill description when non-empty.
	// Empty preserves any description already embedded in the uploaded zip.
	Description string
	ZipData     []byte
	// BotName, when non-empty, installs the published skill onto that bot
	// after the asset upload completes. Leave empty to publish the skill
	// without attaching it to any bot.
	BotName string
}

type AssetSummary struct {
	Name          string
	Type          string
	LatestVersion string
	VersionsCount int
	Description   string
	// CreatedAt and UpdatedAt are the vault-recorded timestamps for the
	// asset's earliest and latest versions. They may be the zero value when
	// the underlying vault doesn't track timestamps for an entry.
	CreatedAt time.Time
	UpdatedAt time.Time
}

func OpenSkillsNew(serverURL, authToken string) (*Client, error) {
	return OpenSkillsNewWithOptions(serverURL, SkillsNewOptions{AuthToken: authToken})
}

func OpenSkillsNewWithOptions(serverURL string, opts SkillsNewOptions) (*Client, error) {
	serverURL = strings.TrimRight(strings.TrimSpace(serverURL), "/")
	if serverURL == "" {
		serverURL = DefaultSkillsNewURL
	}
	authToken := strings.TrimSpace(opts.AuthToken)
	if authToken == "" {
		return nil, errors.New("sxvault: skills.new auth token required")
	}
	u, err := url.Parse(serverURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("sxvault: invalid skills.new server URL %q", serverURL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("sxvault: invalid skills.new server URL scheme %q", u.Scheme)
	}
	return &Client{v: vault.NewSleuthVault(serverURL, authToken), actor: opts.Actor}, nil
}

// PathOptions configures OpenPath. Path vaults have no auth surface —
// local filesystem access is the trust boundary — so Actor is the only
// knob, kept on a struct for future extension symmetric with GitOptions.
type PathOptions struct {
	// Actor identifies the caller for audit/identity context propagated
	// via mgmt.ContextWithIdentity. PathVault doesn't consult identity
	// directly today, but downstream audit / logging hooks read it
	// through actorContext on every method call.
	Actor Actor
}

// OpenPath opens a file-backed vault rooted at dir. The directory must
// already exist; the underlying PathVault treats absent directories as a
// hard error rather than auto-creating. dir may be either a bare directory
// path (/vault/data) or a file:// URL (file:///vault/data) — the leading
// scheme is stripped before normalization.
func OpenPath(dir string, opts PathOptions) (*Client, error) {
	dir = strings.TrimSpace(dir)
	dir = strings.TrimPrefix(dir, "file://")
	if dir == "" {
		return nil, errors.New("sxvault: path required")
	}
	pv, err := vault.NewPathVault("file://" + dir)
	if err != nil {
		return nil, err
	}
	return &Client{v: pv, actor: opts.Actor}, nil
}

func OpenGit(repoURL string, opts GitOptions) (*Client, error) {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return nil, errors.New("sxvault: git repo URL required")
	}
	gitOpts, err := buildGitClientOptions(repoURL, opts)
	if err != nil {
		return nil, err
	}
	gitClient := git.NewClientWithOptions(gitOpts...)
	gv, err := vault.NewGitVaultWithOptions(repoURL, vault.WithGitClient(gitClient))
	if err != nil {
		return nil, err
	}
	return &Client{v: gv, actor: opts.Actor}, nil
}

// buildGitClientOptions translates a sxvault.GitOptions plus the target repo
// URL into the git.ClientOption list used to construct the underlying client.
// Extracted so tests can assert on the resulting client env via the public
// git.Client.ExtraEnv accessor without reaching into vault internals.
func buildGitClientOptions(repoURL string, opts GitOptions) ([]git.ClientOption, error) {
	info := git.ParseRemoteAuthInfo(repoURL)
	// Reject a URL that looks like HTTP(S) but doesn't parse cleanly,
	// regardless of whether a token was supplied. Otherwise a malformed
	// URL with no token would silently sail past OpenGit and only fail at
	// clone time with a less actionable error.
	if !info.HTTP && git.LooksLikeHTTPRemote(repoURL) {
		return nil, fmt.Errorf("sxvault: malformed git URL %q", repoURL)
	}
	gitOpts := []git.ClientOption{git.WithCommitActor(opts.Actor.Name, opts.Actor.Email)}
	sshKey := strings.TrimSpace(opts.SSHKeyPath)
	if tok := strings.TrimSpace(opts.AuthToken); tok != "" && info.HTTP && sshKey == "" {
		// SSH key takes precedence on HTTP(S): the git client rewrites
		// HTTP(S) → SSH at clone time so the basic-auth extraheader env
		// never runs. When SSHKeyPath is unset, we wire the basic-auth
		// env through normally.
		gitOpts = append(gitOpts, git.WithHTTPBasicAuth(info.Scheme, info.Host, git.DefaultHTTPAuthUsername(info.Host, opts.AuthUsername), tok))
	}
	if sshKey != "" {
		gitOpts = append(gitOpts, git.WithSSHKey(sshKey))
	}
	return gitOpts, nil
}

// EnsureBot creates the named bot if missing and updates its description
// when bot.Description is non-empty and differs from the stored value. An
// empty bot.Description on an existing bot is a no-op — the stored
// description is preserved, so repeated EnsureBot calls from agent-publish
// flows don't rewrite the bot's identity. On the create path (bot does
// not yet exist) EnsureBot REJECTS an empty bot.Description with an
// error: creating an identity-less bot is almost never what the caller
// wants, and the "empty preserves" semantic only makes sense once an
// identity is already in place.
//
// The returned string is a one-time raw bot API token only when the backend
// creates a new bot and issues a token. It is empty when the bot already
// exists and for file-backed Git vaults.
func (c *Client) EnsureBot(ctx context.Context, bot Bot) (string, error) {
	if c == nil || c.v == nil {
		return "", errors.New("sxvault: nil client")
	}
	return c.ensureBot(c.actorContext(ctx), bot)
}

// ensureBot is the actor-wrapped-context internal form of EnsureBot. Public
// methods wrap ctx with actorContext once at entry and call this directly,
// so cross-method calls inside the facade don't re-wrap.
func (c *Client) ensureBot(ctx context.Context, bot Bot) (string, error) {
	name := strings.TrimSpace(bot.Name)
	if name == "" {
		return "", errors.New("sxvault: bot name required")
	}
	desc := strings.TrimSpace(bot.Description)
	existing, err := c.v.GetBot(ctx, name)
	if err == nil {
		if desc == "" || existing.Description == desc {
			return "", nil
		}
		existing.Description = desc
		return "", c.v.UpdateBot(ctx, *existing)
	}
	if !errors.Is(err, mgmt.ErrBotNotFound) {
		return "", err
	}
	if desc == "" {
		return "", fmt.Errorf("sxvault: bot description required when creating bot %q", name)
	}
	return c.v.CreateBot(ctx, mgmt.Bot{
		Name:        name,
		Description: desc,
	})
}

// PutAgent uploads an agent asset and installs it (plus any listed skills)
// on the named bot.
//
// Re-publishing an existing AssetName@Version is idempotent for the manifest:
// the version is listed once and installations are re-run, which also makes
// this the recovery path for a publish that failed midway. The stored zip
// bytes themselves follow the vault backend — Sleuth vaults preserve the
// original (any new Prompt / Description in spec is silently discarded); Git
// vaults overwrite the stored bytes. Bump the version when you need a
// guaranteed update across all backends.
//
// PutAgent is NOT transactional. The flow runs as: EnsureBot → validate
// skills → upload asset → install agent on bot → install each skill on bot.
// If any step after the asset upload fails (e.g. a transient git push race
// on the Nth skill), partial install state is left in the vault. Every step
// is idempotent, so the caller should retry the same PutAgent call to
// converge — the asset upload no-ops on version match and the install path
// is an upsert.
func (c *Client) PutAgent(ctx context.Context, spec AgentSpec) (AgentResult, error) {
	if c == nil || c.v == nil {
		return AgentResult{}, errors.New("sxvault: nil client")
	}
	spec.BotName = strings.TrimSpace(spec.BotName)
	spec.AssetName = strings.TrimSpace(spec.AssetName)
	spec.Version = strings.TrimSpace(spec.Version)
	spec.Prompt = strings.TrimSpace(spec.Prompt)
	if spec.BotName == "" || spec.AssetName == "" || spec.Version == "" {
		return AgentResult{}, errors.New("sxvault: bot name, asset name, and version are required")
	}
	if spec.Prompt == "" {
		return AgentResult{}, errors.New("sxvault: agent prompt is required")
	}
	ctx = c.actorContext(ctx)
	// Ensure the bot first so a misnamed BotName or missing BotDescription
	// fails in 1 round-trip, before the N×2 skill validation cost. EnsureBot
	// is idempotent and cheap when the bot already exists.
	botKey, err := c.ensureBot(ctx, Bot{Name: spec.BotName, Description: spec.BotDescription})
	if err != nil {
		return AgentResult{}, err
	}
	skills := cleanNames(spec.Skills)
	for _, skill := range skills {
		if err := c.validateSkillExists(ctx, skill); err != nil {
			return AgentResult{}, err
		}
	}
	zipData, err := agentZip(spec)
	if err != nil {
		return AgentResult{}, err
	}
	ast := &lockfile.Asset{Name: spec.AssetName, Version: spec.Version, Type: asset.TypeAgent}
	if err := c.addAsset(ctx, ast, zipData); err != nil {
		return AgentResult{}, err
	}
	if err := c.installAssetToBot(ctx, spec.AssetName, spec.BotName); err != nil {
		return AgentResult{}, err
	}
	for _, skill := range skills {
		if err := c.installAssetToBot(ctx, skill, spec.BotName); err != nil {
			return AgentResult{}, err
		}
	}
	return AgentResult{BotKey: botKey}, nil
}

// validateSkillExists asserts the named asset is a published skill in the
// vault. It costs 1 list + 1 metadata call per skill — the metadata fetch
// targets the HIGHEST-semver version, not the last entry in the version
// list (which is on-disk append order, not release order, so a backport
// published after a newer release would otherwise gate on the wrong
// metadata).
func (c *Client) validateSkillExists(ctx context.Context, skill string) error {
	versions, vErr := c.v.GetVersionList(ctx, skill)
	if vErr != nil {
		return fmt.Errorf("sxvault: checking skill %q: %w", skill, vErr)
	}
	if len(versions) == 0 {
		return fmt.Errorf("sxvault: skill %q not found in vault", skill)
	}
	latest := highestSemver(versions)
	meta, mErr := c.v.GetMetadata(ctx, skill, latest)
	if mErr != nil {
		return fmt.Errorf("sxvault: reading metadata for %q: %w", skill, mErr)
	}
	if meta.Asset.Type.Key != asset.TypeSkill.Key {
		return fmt.Errorf("sxvault: %q is type %q, not skill", skill, meta.Asset.Type.Key)
	}
	return nil
}

// highestSemver returns the highest-semver version from versions. Entries
// that don't parse as semver fall to the bottom of the ordering; if no
// entries parse, the last entry is returned (preserving the previous
// last-appended behaviour as a fallback rather than failing the caller).
func highestSemver(versions []string) string {
	var best *semver.Version
	bestStr := versions[len(versions)-1]
	for _, v := range versions {
		parsed, err := semver.NewVersion(v)
		if err != nil {
			continue
		}
		if best == nil || parsed.GreaterThan(best) {
			best = parsed
			bestStr = v
		}
	}
	return bestStr
}

// PutSkillZip uploads a skill zip and, when spec.BotName is non-empty,
// installs the skill on that bot.
//
// Re-publishing an existing Name@Version is idempotent for the manifest: the
// version is listed once and installations are re-run, which also makes this
// the recovery path for a publish that failed midway. The stored zip bytes
// themselves follow the vault backend — Sleuth vaults preserve the original
// (new ZipData on a re-publish is silently discarded); Git vaults overwrite
// the stored bytes. Bump the version when you need a guaranteed update
// across all backends.
func (c *Client) PutSkillZip(ctx context.Context, spec SkillZipSpec) error {
	if c == nil || c.v == nil {
		return errors.New("sxvault: nil client")
	}
	spec.Name = strings.TrimSpace(spec.Name)
	spec.Version = strings.TrimSpace(spec.Version)
	spec.BotName = strings.TrimSpace(spec.BotName)
	if spec.Name == "" || spec.Version == "" {
		return errors.New("sxvault: skill name and version are required")
	}
	zipData, err := normalizeSkillZip(spec)
	if err != nil {
		return err
	}
	ctx = c.actorContext(ctx)
	// Validate the target bot exists before any side effects, mirroring
	// PutAgent's skill pre-check — otherwise we publish the skill and then
	// fail (or, worse, succeed at the manifest write but leave a dangling
	// install scope referencing a phantom bot).
	if spec.BotName != "" {
		if _, bErr := c.v.GetBot(ctx, spec.BotName); bErr != nil {
			if errors.Is(bErr, mgmt.ErrBotNotFound) {
				return fmt.Errorf("sxvault: bot %q not found in vault", spec.BotName)
			}
			return fmt.Errorf("sxvault: checking bot %q: %w", spec.BotName, bErr)
		}
	}
	ast := &lockfile.Asset{Name: spec.Name, Version: spec.Version, Type: asset.TypeSkill}
	if err := c.addAsset(ctx, ast, zipData); err != nil {
		return err
	}
	if spec.BotName == "" {
		return nil
	}
	return c.InstallAssetToBot(ctx, spec.Name, spec.BotName)
}

func (c *Client) InstallAssetToBot(ctx context.Context, assetName, botName string) error {
	if c == nil || c.v == nil {
		return errors.New("sxvault: nil client")
	}
	return c.installAssetToBot(c.actorContext(ctx), assetName, botName)
}

// installAssetToBot is the actor-wrapped-context internal form. Callers
// that already wrapped ctx via actorContext should use this directly so the
// wrap doesn't run twice.
func (c *Client) installAssetToBot(ctx context.Context, assetName, botName string) error {
	assetName = strings.TrimSpace(assetName)
	botName = strings.TrimSpace(botName)
	if assetName == "" || botName == "" {
		return errors.New("sxvault: asset name and bot name are required")
	}
	return c.v.SetAssetInstallation(ctx, assetName, vault.InstallTarget{
		Kind: vault.InstallKindBot,
		Bot:  botName,
	})
}

func (c *Client) ListAssets(ctx context.Context, typ string) ([]AssetSummary, error) {
	return c.ListAssetsWithOptions(ctx, ListOptions{Type: typ})
}

type ListOptions struct {
	// Type filters to a single asset-type key (e.g. "skill", "agent",
	// "mcp"). Empty returns every type.
	Type string
	// Search filters returned assets by a free-text query. Semantics
	// differ by backend: Git and Path vaults do a case-insensitive
	// substring match against the asset's name OR description; Sleuth
	// vaults pass the query to the backend's GraphQL search, which may
	// rank or fuzzy-match. Don't depend on exact ordering or scoring
	// across backends.
	Search string
	// Limit caps the number of returned assets. Zero or negative means
	// "backend default" — for Git and Path that is uncapped; for Sleuth
	// the backend silently caps at 50 regardless of the value passed,
	// so callers that need to enumerate every asset in a large Sleuth
	// vault need a different strategy (per-asset lookups) than this
	// list call provides today.
	Limit int
}

func (c *Client) ListAssetsWithOptions(ctx context.Context, opts ListOptions) ([]AssetSummary, error) {
	if c == nil || c.v == nil {
		return nil, errors.New("sxvault: nil client")
	}
	res, err := c.v.ListAssets(c.actorContext(ctx), vault.ListAssetsOptions{
		Type:   strings.TrimSpace(opts.Type),
		Search: strings.TrimSpace(opts.Search),
		Limit:  opts.Limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]AssetSummary, 0, len(res.Assets))
	for _, a := range res.Assets {
		out = append(out, AssetSummary{
			Name:          a.Name,
			Type:          a.Type.Key,
			LatestVersion: a.LatestVersion,
			VersionsCount: a.VersionsCount,
			Description:   a.Description,
			CreatedAt:     a.CreatedAt,
			UpdatedAt:     a.UpdatedAt,
		})
	}
	return out, nil
}

func (c *Client) addAsset(ctx context.Context, ast *lockfile.Asset, zipData []byte) error {
	if err := c.v.AddAsset(ctx, ast, zipData); err != nil {
		// Re-publishing an existing name@version is intentionally a no-op
		// for stored content. We still fall through to InheritInstallations
		// so a retry after a half-failed prior publish (asset bytes
		// written, manifest update never ran) can complete the install.
		// Only Sleuth surfaces ErrVersionExists today; in that path we
		// deliberately skip the SourcePath fallback below because the
		// real asset lives behind an HTTP source on the server, not under
		// the local assets/ tree the fallback fabricates.
		var exists *vault.ErrVersionExists
		if !errors.As(err, &exists) {
			return err
		}
		return c.v.InheritInstallations(ctx, ast)
	}
	if ast.SourcePath == nil && ast.SourceHTTP == nil && ast.SourceGit == nil {
		ast.SourcePath = &lockfile.SourcePath{Path: "assets/" + ast.Name + "/" + ast.Version}
	}
	return c.v.InheritInstallations(ctx, ast)
}

func (c *Client) actorContext(ctx context.Context) context.Context {
	if c == nil || strings.TrimSpace(c.actor.Email) == "" {
		return ctx
	}
	return mgmt.ContextWithIdentity(ctx, c.actor.Email)
}

func agentZip(spec AgentSpec) ([]byte, error) {
	prompt := []byte(strings.TrimSpace(spec.Prompt) + "\n")
	zipData, err := utils.CreateZipFromContent("AGENT.md", prompt)
	if err != nil {
		return nil, err
	}
	meta := &metadata.Metadata{
		MetadataVersion: metadata.CurrentMetadataVersion,
		Asset: metadata.Asset{
			Name:        spec.AssetName,
			Version:     spec.Version,
			Type:        asset.TypeAgent,
			Description: strings.TrimSpace(spec.Description),
		},
		Agent: &metadata.AgentConfig{PromptFile: "AGENT.md"},
	}
	metaBytes, err := metadata.Marshal(meta)
	if err != nil {
		return nil, err
	}
	return utils.AddFileToZip(zipData, "metadata.toml", metaBytes)
}

func normalizeSkillZip(spec SkillZipSpec) ([]byte, error) {
	if !utils.IsZipFile(spec.ZipData) {
		return nil, errors.New("sxvault: uploaded skill must be a zip file")
	}
	meta := &metadata.Metadata{
		MetadataVersion: metadata.CurrentMetadataVersion,
		Asset: metadata.Asset{
			Name:        spec.Name,
			Version:     spec.Version,
			Type:        asset.TypeSkill,
			Description: strings.TrimSpace(spec.Description),
		},
		Skill: &metadata.SkillConfig{PromptFile: "SKILL.md"},
	}
	if raw, err := utils.ReadZipFile(spec.ZipData, "metadata.toml"); err == nil {
		parsed, parseErr := metadata.Parse(raw)
		if parseErr != nil {
			return nil, parseErr
		}
		meta = parsed
		meta.Asset.Name = spec.Name
		meta.Asset.Version = spec.Version
		meta.Asset.Type = asset.TypeSkill
		if strings.TrimSpace(spec.Description) != "" {
			meta.Asset.Description = strings.TrimSpace(spec.Description)
		}
		if meta.Skill == nil {
			meta.Skill = &metadata.SkillConfig{PromptFile: "SKILL.md"}
		}
		if strings.TrimSpace(meta.Skill.PromptFile) == "" {
			meta.Skill.PromptFile = "SKILL.md"
		}
	}
	if _, err := utils.ReadZipFile(spec.ZipData, meta.Skill.PromptFile); err != nil {
		return nil, fmt.Errorf("sxvault: skill prompt file %q missing from zip: %w", meta.Skill.PromptFile, err)
	}
	metaBytes, err := metadata.Marshal(meta)
	if err != nil {
		return nil, err
	}
	return utils.AddFileToZip(spec.ZipData, "metadata.toml", metaBytes)
}

func cleanNames(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
