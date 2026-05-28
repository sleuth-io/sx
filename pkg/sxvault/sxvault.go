// Package sxvault exposes a small, stable management facade over SX vaults.
//
// Scope: this facade currently covers the publish / manage write path
// (OpenSkillsNew / OpenGit / OpenPath constructors; EnsureBot, DeleteBot,
// PutAgent, PutSkillZip, InstallAssetToBot mutators) plus read-only browse
// and download (ListAssets, GetAssetZip). GetAssetZip is the one narrow
// read path: it wraps the internal GetMetadata / GetAssetByVersion calls
// and exposes only the asset type, description, and raw zip bytes through
// AssetZip. The raw GetMetadata / GetAssetByVersion interfaces, along with
// the broad mutators the internal vault.Vault supports — RemoveAsset,
// RenameAsset, and asset-uninstall across kinds beyond bot — remain
// internal and are NOT re-exported yet (UninstallAssetFromBot is the
// narrow, bot-only public uninstall form), reserved for a follow-up
// release once consumer needs are clearer. Library consumers needing them
// today should treat the absence as a "not yet" rather than a "never."
package sxvault

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

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

type BotSummary struct {
	Name            string
	Slug            string
	Description     string
	Teams           []string
	InstalledSkills []BotSkillSummary
}

type BotSkillSummary struct {
	Name            string
	IsDirectInstall bool
}

type TeamSummary struct {
	Name         string
	Description  string
	MemberCount  int
	Repositories []string
}

type BotRuntimeTokenSpec struct {
	BotName string
	// Label is stored with the short-lived runtime token for audit/display.
	// Empty is allowed and lets the backend apply its default label.
	Label string
	// TTLSeconds controls how long the runtime token is valid. Zero means
	// use the backend default. The Skills.new backend currently accepts
	// values from 60 seconds to 24 hours.
	TTLSeconds int
}

type BotRuntimeTokenResult struct {
	Token     string
	ExpiresAt time.Time
}

// ErrBotRuntimeTokensUnsupported is returned by runtime-token methods when the
// underlying vault does not implement runtime tokens (i.e., any non-skills.new
// backend). Callers should match with errors.Is to detect this case.
var ErrBotRuntimeTokensUnsupported = errors.New("sxvault: bot runtime tokens are only supported by skills.new vaults")

// ErrBotNotFound is reported (via errors.Is) when a method that resolves a
// bot by name — EnsureBot's update path, PutSkillZip's bot pre-check,
// UninstallAssetFromBot — is given a bot the vault doesn't have. It
// re-exports the internal sentinel so library consumers can branch on
// "bot doesn't exist" without string-matching the error message.
var ErrBotNotFound = mgmt.ErrBotNotFound

type SkillZipSpec struct {
	Name    string
	Version string
	// Description overrides metadata.toml's skill description when non-empty.
	// Empty preserves any description already embedded in the uploaded zip.
	Description string
	ZipData     []byte
	// BotName, when non-empty, installs the published skill onto that bot
	// after the asset upload completes. For a new Skills.new asset, this
	// makes the first upload bot-scoped instead of inheriting the upload
	// endpoint's org-wide default. Leave empty to publish the skill without
	// attaching it to any bot.
	BotName string
}

// SkillZipResult contains the persisted skill identity after PutSkillZip.
// Server-backed vaults may normalize spec.Name into a slug; use Name for
// follow-up install/uninstall calls against the same vault. Today only the
// Sleuth vault normalizes the name — for git/path vaults Name is spec.Name
// unchanged. IsFirstVersion is true when the upload created the first stored
// version of this skill (Sleuth-only; git/path vaults always report false).
type SkillZipResult struct {
	Name           string
	Version        string
	IsFirstVersion bool
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

type AssetZip struct {
	Name        string
	Version     string
	Type        string
	Description string
	Data        []byte
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
//
// Unlike PutSkillZip there is no PutAgentWithResult: the persisted agent
// slug and the upload's IsFirstVersion flag are consumed internally (the
// latter to clear the default org install before the bot install) but are
// not surfaced to callers yet. Add a WithResult variant if a consumer needs
// that observability.
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
	upload, err := c.addAssetWithResult(ctx, ast, zipData)
	if err != nil {
		return AgentResult{}, err
	}
	// upload.Name is already canonical and non-empty (defaultAddAssetResult
	// fills it from spec.AssetName when the vault didn't provide one).
	agentName := upload.Name
	// Mirror PutSkillZip: on a brand-new asset the Sleuth upload applies a
	// default org-wide install, but an agent is always bot-targeted, so strip
	// that default before the bot install lands. IsFirstVersion is Sleuth-only
	// (git/local leave it false), so this is a no-op for those backends.
	if upload.IsFirstVersion {
		if err := c.v.ClearAssetInstallations(ctx, agentName); err != nil {
			return AgentResult{}, fmt.Errorf("sxvault: clearing default installations for %q: %w", agentName, err)
		}
	}
	if err := c.installAssetToBot(ctx, agentName, spec.BotName); err != nil {
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
	if meta == nil {
		return fmt.Errorf("sxvault: metadata for %q@%s was nil", skill, latest)
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
	_, err := c.PutSkillZipWithResult(ctx, spec)
	return err
}

// PutSkillZipWithResult uploads a skill zip and returns the persisted skill
// identity from the backing vault. For Skills.new this is the server-returned
// slug, which may differ from spec.Name when the uploaded display name collides
// with an existing asset slug.
func (c *Client) PutSkillZipWithResult(ctx context.Context, spec SkillZipSpec) (SkillZipResult, error) {
	if c == nil || c.v == nil {
		return SkillZipResult{}, errors.New("sxvault: nil client")
	}
	spec.Name = strings.TrimSpace(spec.Name)
	spec.Version = strings.TrimSpace(spec.Version)
	spec.BotName = strings.TrimSpace(spec.BotName)
	if spec.Name == "" || spec.Version == "" {
		return SkillZipResult{}, errors.New("sxvault: skill name and version are required")
	}
	zipData, err := normalizeSkillZip(spec)
	if err != nil {
		return SkillZipResult{}, err
	}
	ctx = c.actorContext(ctx)
	// Validate the target bot exists before any side effects, mirroring
	// PutAgent's skill pre-check — otherwise we publish the skill and then
	// fail (or, worse, succeed at the manifest write but leave a dangling
	// install scope referencing a phantom bot).
	if spec.BotName != "" {
		if _, bErr := c.v.GetBot(ctx, spec.BotName); bErr != nil {
			if errors.Is(bErr, mgmt.ErrBotNotFound) {
				return SkillZipResult{}, fmt.Errorf("sxvault: bot %q not found in vault: %w", spec.BotName, ErrBotNotFound)
			}
			return SkillZipResult{}, fmt.Errorf("sxvault: checking bot %q: %w", spec.BotName, bErr)
		}
	}
	ast := &lockfile.Asset{Name: spec.Name, Version: spec.Version, Type: asset.TypeSkill}
	upload, err := c.addAssetWithResult(ctx, ast, zipData)
	if err != nil {
		return SkillZipResult{}, err
	}
	// upload.Name / upload.Version are already canonical and non-empty
	// (defaultAddAssetResult fills them from the spec when the vault didn't).
	result := SkillZipResult{
		Name:           upload.Name,
		Version:        upload.Version,
		IsFirstVersion: upload.IsFirstVersion,
	}
	if spec.BotName == "" {
		return result, nil
	}
	// IsFirstVersion is only ever set by the Sleuth HTTP upload response;
	// git/local vaults go through AddAsset and leave it false, so this clear
	// is Sleuth-only by design. It strips the server's default org-wide
	// install on a brand-new asset so a bot-targeted publish lands only on
	// the bot, not everyone. Non-Sleuth vaults have no such default to clear.
	if upload.IsFirstVersion {
		if err := c.v.ClearAssetInstallations(ctx, result.Name); err != nil {
			return SkillZipResult{}, fmt.Errorf("sxvault: clearing default installations for %q: %w", result.Name, err)
		}
	}
	// ctx is already actor-wrapped above; use the internal form so the wrap
	// doesn't run twice (mirrors PutAgent).
	if err := c.installAssetToBot(ctx, result.Name, spec.BotName); err != nil {
		return SkillZipResult{}, err
	}
	return result, nil
}

func (c *Client) ListBots(ctx context.Context) ([]BotSummary, error) {
	if c == nil || c.v == nil {
		return nil, errors.New("sxvault: nil client")
	}
	bots, err := c.v.ListBots(c.actorContext(ctx))
	if err != nil {
		return nil, err
	}
	out := make([]BotSummary, 0, len(bots))
	for _, b := range bots {
		out = append(out, BotSummary{
			Name:            b.Name,
			Slug:            b.Slug,
			Description:     b.Description,
			Teams:           append([]string(nil), b.Teams...),
			InstalledSkills: botSkillSummaries(b.InstalledSkills),
		})
	}
	return out, nil
}

func botSkillSummaries(skills []mgmt.BotSkill) []BotSkillSummary {
	if len(skills) == 0 {
		return nil
	}
	out := make([]BotSkillSummary, 0, len(skills))
	for _, skill := range skills {
		out = append(out, BotSkillSummary{
			Name:            skill.Name,
			IsDirectInstall: skill.IsDirectInstall,
		})
	}
	return out
}

// DeleteBot removes the named bot from the vault. File-backed vaults also
// remove any bot-scoped asset installations targeting that bot; asset versions
// themselves remain in the vault.
func (c *Client) DeleteBot(ctx context.Context, botName string) error {
	if c == nil || c.v == nil {
		return errors.New("sxvault: nil client")
	}
	botName = strings.TrimSpace(botName)
	if botName == "" {
		return errors.New("sxvault: bot name required")
	}
	return c.v.DeleteBot(c.actorContext(ctx), botName)
}

// defaultTeamsLimit is the page size ListTeams requests. It aliases the
// vault package's canonical server-side maximum so the public "list all
// teams" surface and the internal lookups share one value. ListTeams
// surfaces an error rather than silently truncating when a vault exceeds it.
const defaultTeamsLimit = vault.DefaultTeamsLimit

func (c *Client) ListTeams(ctx context.Context) ([]TeamSummary, error) {
	if c == nil || c.v == nil {
		return nil, errors.New("sxvault: nil client")
	}
	result, err := c.v.ListTeams(c.actorContext(ctx), vault.ListTeamsOptions{Limit: defaultTeamsLimit})
	if err != nil {
		return nil, err
	}
	// ListTeams reads as an "all teams" call but the backend caps results at
	// defaultTeamsLimit. Fail loudly on truncation instead of handing back a
	// silent partial view — there is no pagination knob on this surface yet.
	if result.HasMore {
		return nil, fmt.Errorf("sxvault: vault has more than %d teams; listing that many is not yet supported", defaultTeamsLimit)
	}
	out := make([]TeamSummary, 0, len(result.Teams))
	for _, t := range result.Teams {
		out = append(out, TeamSummary{
			Name:         t.Name,
			Description:  t.Description,
			MemberCount:  t.MemberCount,
			Repositories: append([]string(nil), t.Repositories...),
		})
	}
	slices.SortFunc(out, func(a, b TeamSummary) int {
		return strings.Compare(a.Name, b.Name)
	})
	return out, nil
}

func (c *Client) AddBotTeam(ctx context.Context, botName, teamName string) error {
	if c == nil || c.v == nil {
		return errors.New("sxvault: nil client")
	}
	botName = strings.TrimSpace(botName)
	teamName = strings.TrimSpace(teamName)
	if botName == "" || teamName == "" {
		return errors.New("sxvault: bot name and team name are required")
	}
	return c.v.AddBotTeam(c.actorContext(ctx), botName, teamName)
}

func (c *Client) RemoveBotTeam(ctx context.Context, botName, teamName string) error {
	if c == nil || c.v == nil {
		return errors.New("sxvault: nil client")
	}
	botName = strings.TrimSpace(botName)
	teamName = strings.TrimSpace(teamName)
	if botName == "" || teamName == "" {
		return errors.New("sxvault: bot name and team name are required")
	}
	return c.v.RemoveBotTeam(c.actorContext(ctx), botName, teamName)
}

func (c *Client) CreateBotRuntimeToken(ctx context.Context, spec BotRuntimeTokenSpec) (BotRuntimeTokenResult, error) {
	if c == nil || c.v == nil {
		return BotRuntimeTokenResult{}, errors.New("sxvault: nil client")
	}
	botName := strings.TrimSpace(spec.BotName)
	if botName == "" {
		return BotRuntimeTokenResult{}, errors.New("sxvault: bot name required")
	}
	manager, ok := c.v.(vault.BotRuntimeTokenManager)
	if !ok {
		return BotRuntimeTokenResult{}, ErrBotRuntimeTokensUnsupported
	}
	token, expiresAt, err := manager.CreateBotRuntimeToken(
		c.actorContext(ctx),
		botName,
		strings.TrimSpace(spec.Label),
		spec.TTLSeconds,
	)
	if err != nil {
		return BotRuntimeTokenResult{}, err
	}
	return BotRuntimeTokenResult{Token: token, ExpiresAt: expiresAt}, nil
}

func (c *Client) RevokeBotRuntimeTokens(ctx context.Context, botName string) (int, error) {
	if c == nil || c.v == nil {
		return 0, errors.New("sxvault: nil client")
	}
	botName = strings.TrimSpace(botName)
	if botName == "" {
		return 0, errors.New("sxvault: bot name required")
	}
	manager, ok := c.v.(vault.BotRuntimeTokenManager)
	if !ok {
		return 0, ErrBotRuntimeTokensUnsupported
	}
	return manager.RevokeBotRuntimeTokens(c.actorContext(ctx), botName)
}

func (c *Client) InstallAssetToBot(ctx context.Context, assetName, botName string) error {
	if c == nil || c.v == nil {
		return errors.New("sxvault: nil client")
	}
	return c.installAssetToBot(c.actorContext(ctx), assetName, botName)
}

func (c *Client) UninstallAssetFromBot(ctx context.Context, assetName, botName string) error {
	if c == nil || c.v == nil {
		return errors.New("sxvault: nil client")
	}
	assetName = strings.TrimSpace(assetName)
	botName = strings.TrimSpace(botName)
	if assetName == "" || botName == "" {
		return errors.New("sxvault: asset name and bot name are required")
	}
	return c.v.RemoveAssetInstallation(c.actorContext(ctx), assetName, vault.InstallTarget{
		Kind: vault.InstallKindBot,
		Bot:  botName,
	})
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

// GetAssetZip downloads a single asset version from the vault. It is the
// only download affordance on the public surface.
//
// When version is empty, the highest-semver version is selected; if no
// version parses as semver, the last entry in the vault's version list is
// used as a fallback (see highestSemver). The returned AssetZip.Type is the
// asset type key (e.g. "skill", "agent"), AssetZip.Description comes from the
// asset metadata, and AssetZip.Data holds the zip bytes exactly as the vault
// backend stores them.
func (c *Client) GetAssetZip(ctx context.Context, name, version string) (AssetZip, error) {
	if c == nil || c.v == nil {
		return AssetZip{}, errors.New("sxvault: nil client")
	}
	name = strings.TrimSpace(name)
	version = strings.TrimSpace(version)
	if name == "" {
		return AssetZip{}, errors.New("sxvault: asset name required")
	}
	ctx = c.actorContext(ctx)
	if version == "" {
		versions, err := c.v.GetVersionList(ctx, name)
		if err != nil {
			return AssetZip{}, fmt.Errorf("sxvault: listing versions for %q: %w", name, err)
		}
		if len(versions) == 0 {
			return AssetZip{}, fmt.Errorf("sxvault: asset %q not found in vault", name)
		}
		version = highestSemver(versions)
	}
	meta, err := c.v.GetMetadata(ctx, name, version)
	if err != nil {
		return AssetZip{}, fmt.Errorf("sxvault: reading metadata for %q@%s: %w", name, version, err)
	}
	if meta == nil {
		return AssetZip{}, fmt.Errorf("sxvault: metadata for %q@%s was nil", name, version)
	}
	data, err := c.v.GetAssetByVersion(ctx, name, version)
	if err != nil {
		return AssetZip{}, fmt.Errorf("sxvault: reading asset zip for %q@%s: %w", name, version, err)
	}
	return AssetZip{
		Name:        name,
		Version:     version,
		Type:        meta.Asset.Type.Key,
		Description: meta.Asset.Description,
		Data:        data,
	}, nil
}

type assetAdderWithResult interface {
	AddAssetWithResult(ctx context.Context, asset *lockfile.Asset, zipData []byte) (vault.AddAssetResult, error)
}

func (c *Client) addAssetWithResult(ctx context.Context, ast *lockfile.Asset, zipData []byte) (vault.AddAssetResult, error) {
	var result vault.AddAssetResult
	var err error
	if adder, ok := c.v.(assetAdderWithResult); ok {
		result, err = adder.AddAssetWithResult(ctx, ast, zipData)
	} else {
		err = c.v.AddAsset(ctx, ast, zipData)
	}
	// defaultAddAssetResult trims and fills Name/Version from ast, so every
	// return path below hands back a non-empty, canonical result.
	result = defaultAddAssetResult(result, ast)
	if err != nil {
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
			return vault.AddAssetResult{}, err
		}
		// On a version conflict the server returns no AddAssetResult, so
		// result still carries the spec defaults (the requested Name). If
		// the conflict response reported the persisted slug, prefer it so a
		// re-publish of a collision-resolved upload routes the bot install
		// to the uploaded asset, not a different one sharing the name.
		if slug := strings.TrimSpace(exists.Slug); slug != "" {
			result.Name = slug
		}
		if err := c.v.InheritInstallations(ctx, ast); err != nil {
			return vault.AddAssetResult{}, err
		}
		return result, nil
	}
	if ast.SourcePath == nil && ast.SourceHTTP == nil && ast.SourceGit == nil {
		ast.SourcePath = &lockfile.SourcePath{Path: "assets/" + ast.Name + "/" + ast.Version}
	}
	if err := c.v.InheritInstallations(ctx, ast); err != nil {
		return vault.AddAssetResult{}, err
	}
	return result, nil
}

// defaultAddAssetResult is the single source of truth for the canonical
// asset identity: it trims the vault-provided Name/Version and falls back to
// the requested ast values when the vault left them empty. Callers can rely
// on the returned Name/Version being non-empty and trimmed.
func defaultAddAssetResult(result vault.AddAssetResult, ast *lockfile.Asset) vault.AddAssetResult {
	result.Name = strings.TrimSpace(result.Name)
	if result.Name == "" {
		result.Name = strings.TrimSpace(ast.Name)
	}
	result.Version = strings.TrimSpace(result.Version)
	if result.Version == "" {
		result.Version = strings.TrimSpace(ast.Version)
	}
	return result
}

func (c *Client) actorContext(ctx context.Context) context.Context {
	if c == nil || strings.TrimSpace(c.actor.Email) == "" {
		return ctx
	}
	return mgmt.ContextWithIdentity(ctx, c.actor.Email)
}

func agentZip(spec AgentSpec) ([]byte, error) {
	prompt := []byte(agentMarkdown(spec))
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

func agentMarkdown(spec AgentSpec) string {
	prompt := strings.TrimSpace(spec.Prompt)
	if hasFrontmatter(prompt) {
		return prompt + "\n"
	}
	name := agentFrontmatterName(spec.AssetName)
	description := agentFrontmatterDescription(spec.Description, spec.BotDescription, spec.AssetName)
	return "---\nname: " + name + "\ndescription: " + description + "\n---\n\n" + prompt + "\n"
}

// hasFrontmatter reports whether s already begins with a YAML frontmatter
// block (a leading line of exactly --- and a later closing line of exactly
// ---), as opposed to merely starting with a markdown horizontal rule.
func hasFrontmatter(s string) bool {
	lines := strings.Split(s, "\n")
	if len(lines) < 2 || lines[0] != "---" {
		return false
	}
	return slices.Contains(lines[1:], "---")
}

func agentFrontmatterName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == ' ' || r == '.':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 64 {
		out = strings.Trim(out[:64], "-")
	}
	if out == "" {
		return "agent"
	}
	return out
}

func agentFrontmatterDescription(values ...string) string {
	for _, value := range values {
		value = strings.Join(strings.Fields(value), " ")
		if value == "" {
			continue
		}
		if utf8.RuneCountInString(value) > 1024 {
			runes := []rune(value)
			value = string(runes[:1024])
		}
		return yamlQuote(value)
	}
	return yamlQuote("Custom agent")
}

// yamlQuote wraps a string in YAML double-quoted scalar form, escaping
// backslashes and double quotes. This is always-safe: callers don't need
// to think about colons, leading dashes, or other YAML-special chars.
func yamlQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
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
