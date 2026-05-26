// Package sxvault exposes a small, stable management facade over SX vaults.
package sxvault

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

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
	Name  string
	Email string
}

type GitOptions struct {
	// AuthToken authenticates HTTP(S) git remotes through basic auth. SSH
	// remotes ignore this value and use the caller's SSH configuration.
	AuthToken string

	// AuthUsername is the HTTP(S) basic-auth username to pair with AuthToken.
	// Empty uses a host-specific default, currently "x-access-token" except
	// GitLab hosts, which use "oauth2".
	AuthUsername string

	// SSHKeyPath, when non-empty, points the underlying git client at this
	// SSH private key for SSH remotes. It bypasses the process-global SSH
	// key path set by the CLI's --ssh-key flag / SX_SSH_KEY env var, letting
	// library consumers scope SSH auth to a single Client.
	SSHKeyPath string

	Actor Actor
}

type SkillsNewOptions struct {
	AuthToken string
	Actor     Actor
}

type Client struct {
	v     vault.Vault
	actor Actor
}

type Bot struct {
	Name        string
	Description string
}

type AgentSpec struct {
	BotName     string
	AssetName   string
	Version     string
	DisplayName string
	Description string
	Prompt      string
	Skills      []string
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
}

type AssetSummary struct {
	Name          string
	Type          string
	LatestVersion string
	Description   string
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

func OpenGit(repoURL string, opts GitOptions) (*Client, error) {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return nil, errors.New("sxvault: git repo URL required")
	}
	gitOpts := []git.ClientOption{git.WithCommitActor(opts.Actor.Name, opts.Actor.Email)}
	if tok := strings.TrimSpace(opts.AuthToken); tok != "" {
		info := git.ParseRemoteAuthInfo(repoURL)
		if info.HTTP {
			gitOpts = append(gitOpts, git.WithHTTPBasicAuth(info.Scheme, info.Host, git.DefaultHTTPAuthUsername(info.Host, opts.AuthUsername), tok))
		} else if git.LooksLikeHTTPRemote(repoURL) {
			return nil, fmt.Errorf("sxvault: cannot derive git auth host from %q", repoURL)
		}
	}
	if sshKey := strings.TrimSpace(opts.SSHKeyPath); sshKey != "" {
		gitOpts = append(gitOpts, git.WithSSHKey(sshKey))
	}
	gitClient := git.NewClientWithOptions(gitOpts...)
	gv, err := vault.NewGitVaultWithOptions(repoURL, vault.WithGitClient(gitClient))
	if err != nil {
		return nil, err
	}
	return &Client{v: gv, actor: opts.Actor}, nil
}

// EnsureBot creates the named bot if missing and updates its description when
// it already exists. The returned string is a one-time raw bot API token only
// when the backend creates a new bot and issues a token. It is empty when the
// bot already exists and for file-backed Git vaults.
func (c *Client) EnsureBot(ctx context.Context, bot Bot) (string, error) {
	if c == nil || c.v == nil {
		return "", errors.New("sxvault: nil client")
	}
	name := strings.TrimSpace(bot.Name)
	if name == "" {
		return "", errors.New("sxvault: bot name required")
	}
	ctx = c.actorContext(ctx)
	existing, err := c.v.GetBot(ctx, name)
	if err == nil {
		if existing.Description != strings.TrimSpace(bot.Description) {
			existing.Description = strings.TrimSpace(bot.Description)
			return "", c.v.UpdateBot(ctx, *existing)
		}
		return "", nil
	}
	if !errors.Is(err, mgmt.ErrBotNotFound) {
		return "", err
	}
	return c.v.CreateBot(ctx, mgmt.Bot{
		Name:        name,
		Description: strings.TrimSpace(bot.Description),
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
func (c *Client) PutAgent(ctx context.Context, spec AgentSpec) (AgentResult, error) {
	spec.BotName = strings.TrimSpace(spec.BotName)
	spec.AssetName = strings.TrimSpace(spec.AssetName)
	spec.Version = strings.TrimSpace(spec.Version)
	if spec.BotName == "" || spec.AssetName == "" || spec.Version == "" {
		return AgentResult{}, errors.New("sxvault: bot name, asset name, and version are required")
	}
	ctx = c.actorContext(ctx)
	botKey, err := c.EnsureBot(ctx, Bot{Name: spec.BotName, Description: spec.Description})
	if err != nil {
		return AgentResult{}, err
	}
	zipData, err := agentZip(spec)
	if err != nil {
		return AgentResult{}, err
	}
	ast := &lockfile.Asset{Name: spec.AssetName, Version: spec.Version, Type: asset.TypeAgent}
	if err := c.addAsset(ctx, ast, zipData); err != nil {
		return AgentResult{}, err
	}
	if err := c.InstallAssetToBot(ctx, spec.AssetName, spec.BotName); err != nil {
		return AgentResult{}, err
	}
	for _, skill := range cleanNames(spec.Skills) {
		if err := c.InstallAssetToBot(ctx, skill, spec.BotName); err != nil {
			return AgentResult{}, err
		}
	}
	return AgentResult{BotKey: botKey}, nil
}

// PutSkillZip uploads a skill zip and, when botName is non-empty, installs
// the skill on that bot.
//
// Re-publishing an existing Name@Version is idempotent for the manifest: the
// version is listed once and installations are re-run, which also makes this
// the recovery path for a publish that failed midway. The stored zip bytes
// themselves follow the vault backend — Sleuth vaults preserve the original
// (new ZipData on a re-publish is silently discarded); Git vaults overwrite
// the stored bytes. Bump the version when you need a guaranteed update
// across all backends.
func (c *Client) PutSkillZip(ctx context.Context, spec SkillZipSpec, botName string) error {
	spec.Name = strings.TrimSpace(spec.Name)
	spec.Version = strings.TrimSpace(spec.Version)
	if spec.Name == "" || spec.Version == "" {
		return errors.New("sxvault: skill name and version are required")
	}
	zipData, err := normalizeSkillZip(spec)
	if err != nil {
		return err
	}
	ctx = c.actorContext(ctx)
	ast := &lockfile.Asset{Name: spec.Name, Version: spec.Version, Type: asset.TypeSkill}
	if err := c.addAsset(ctx, ast, zipData); err != nil {
		return err
	}
	if strings.TrimSpace(botName) == "" {
		return nil
	}
	return c.InstallAssetToBot(ctx, spec.Name, botName)
}

func (c *Client) InstallAssetToBot(ctx context.Context, assetName, botName string) error {
	if c == nil || c.v == nil {
		return errors.New("sxvault: nil client")
	}
	assetName = strings.TrimSpace(assetName)
	botName = strings.TrimSpace(botName)
	if assetName == "" || botName == "" {
		return errors.New("sxvault: asset name and bot name are required")
	}
	return c.v.SetAssetInstallation(c.actorContext(ctx), assetName, vault.InstallTarget{
		Kind: vault.InstallKindBot,
		Bot:  botName,
	})
}

func (c *Client) ListAssets(ctx context.Context, typ string) ([]AssetSummary, error) {
	return c.ListAssetsWithOptions(ctx, ListOptions{Type: typ})
}

type ListOptions struct {
	Type   string
	Search string
	Limit  int
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
			Description:   a.Description,
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
		var exists *vault.ErrVersionExists
		if !errors.As(err, &exists) {
			return err
		}
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
