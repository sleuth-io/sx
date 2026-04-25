package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

// AssetShimRegistrar registers Pulse-style asset listing/loading MCP tools onto
// an mcp.Server. PathVault and GitVault both opt in by returning a populated
// registrar from GetMCPTools(); the cloud serve builder type-asserts on this
// type and calls Register so claude.ai / chatgpt.com see real tools when they
// connect via the relay.
//
// Tool descriptions and input schemas mirror sleuth/apps/mcp_gateway/asset_shim
// so the local-vault MCP and the server-side shim behave identically from a
// client's perspective. Diverging would mean prompt-tuning has to happen twice.
type AssetShimRegistrar struct {
	Repo assetReader

	// zipCacheMu + zipCache memoize the latest zip bytes for a single
	// ``slug@version``. The typical claude.ai workflow is
	// ``load_my_asset`` followed by one or more ``load_my_asset_file``
	// calls on the same asset; without a cache, GitVault re-walks the
	// exploded repo and re-builds the zip from scratch on every call.
	// One slot is enough — different assets bust each other, which keeps
	// the bound on memory at the size of a single asset's zip.
	zipCacheMu  sync.Mutex
	zipCacheKey string
	zipCache    []byte
}

// assetReader is the slice of the Vault interface needed to back the shim.
// Restricting to the methods we actually call keeps the registrar trivially
// fakeable in tests and avoids dragging the full Vault contract into the shim.
type assetReader interface {
	ListAssets(ctx context.Context, opts ListAssetsOptions) (*ListAssetsResult, error)
	GetAssetDetails(ctx context.Context, name string) (*AssetDetails, error)
	GetAssetByVersion(ctx context.Context, name, version string) ([]byte, error)
}

const shimMaxListAssets = 100
const shimMaxListDescriptionLen = 200

// chatDefaultTypeKeys is the chat-usable subset returned when no explicit
// “type“ filter is provided. Hooks, MCP configs, and Claude Code plugins
// are CLI-only lifecycle primitives and confuse a web-chat client, so the
// caller has to opt in by name.
var chatDefaultTypeKeys = []string{
	asset.TypeSkill.Key,
	asset.TypeRule.Key,
	asset.TypeAgent.Key,
	asset.TypeCommand.Key,
}

// ListAssetsArgs is the input for list_my_assets.
type ListAssetsArgs struct {
	Type   string `json:"type,omitempty" jsonschema:"Filter by asset type. Omit to list the chat-usable subset (skill, rule, agent, command). Pass 'hook' or 'mcp' only if the user specifically asks about those CLI-only types."`
	Search string `json:"search,omitempty" jsonschema:"Case-insensitive substring filter over name + description."`
}

// ListByTypeArgs is the input for the per-type list aliases (list_my_skills, …).
// They pin the type filter server-side so callers can omit it.
type ListByTypeArgs struct {
	Search string `json:"search,omitempty" jsonschema:"Case-insensitive substring filter over name + description."`
}

// LoadAssetArgs is the input for load_my_asset.
type LoadAssetArgs struct {
	Slug string `json:"slug" jsonschema:"Asset slug from list_my_assets."`
}

// LoadAssetFileArgs is the input for load_my_asset_file.
type LoadAssetFileArgs struct {
	Slug string `json:"slug" jsonschema:"Asset slug."`
	Path string `json:"path" jsonschema:"File path as returned by load_my_asset's bundled_files."`
}

// Register wires the seven shim tools onto the supplied MCP server.
func (r *AssetShimRegistrar) Register(server *mcp.Server) {
	log := logger.Get()

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_my_assets",
		Description: descListAssets,
	}, r.handleListAssets)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_my_skills",
		Description: perTypeDescription("skill", "skills"),
	}, r.handleListByType(asset.TypeSkill.Key))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_my_rules",
		Description: perTypeDescription("rule", "rules"),
	}, r.handleListByType(asset.TypeRule.Key))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_my_agents",
		Description: perTypeDescription("agent", "agents"),
	}, r.handleListByType(asset.TypeAgent.Key))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_my_commands",
		Description: perTypeDescription("command", "commands"),
	}, r.handleListByType(asset.TypeCommand.Key))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "load_my_asset",
		Description: descLoadAsset,
	}, r.handleLoadAsset)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "load_my_asset_file",
		Description: descLoadAssetFile,
	}, r.handleLoadAssetFile)

	log.Debug("cloud serve: registered asset shim MCP tools", "count", 7)
}

// handleListAssets implements the list_my_assets tool. The handler runs
// the vault's ListAssets, applies the chat-default-type filter when no
// explicit type was supplied, and trims long descriptions to keep the
// response under client context caps.
func (r *AssetShimRegistrar) handleListAssets(
	ctx context.Context, _ *mcp.CallToolRequest, args ListAssetsArgs,
) (*mcp.CallToolResult, any, error) {
	typeFilter := strings.ToLower(strings.TrimSpace(args.Type))
	search := strings.TrimSpace(args.Search)
	return r.listAssets(ctx, typeFilter, search)
}

// handleListByType returns a tool handler that pins the type filter to a
// single asset type. Used by list_my_skills / list_my_rules / etc. — those
// aliases exist because chatgpt.com / claude.ai dispatchers match heavily on
// tool *name*, and a user asking "what skills do I have" lands more reliably
// on a tool literally named “list_my_skills“ than on a generic
// “list_my_assets“ call with an inferred filter.
func (r *AssetShimRegistrar) handleListByType(
	typeKey string,
) func(context.Context, *mcp.CallToolRequest, ListByTypeArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, args ListByTypeArgs) (*mcp.CallToolResult, any, error) {
		return r.listAssets(ctx, typeKey, strings.TrimSpace(args.Search))
	}
}

func (r *AssetShimRegistrar) listAssets(
	ctx context.Context, typeFilter, search string,
) (*mcp.CallToolResult, any, error) {
	opts := ListAssetsOptions{Type: typeFilter, Limit: shimMaxListAssets}
	result, err := r.Repo.ListAssets(ctx, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("list assets: %w", err)
	}

	summaries := result.Assets
	if typeFilter == "" {
		// Vault's ListAssets only filters when a Type is supplied, so the
		// chat-default subset narrowing has to happen here. Mirrors the
		// shim's ``chat_default_types`` carve-out.
		summaries = filterByTypeKeys(summaries, chatDefaultTypeKeys)
	}
	if search != "" {
		summaries = filterBySearch(summaries, search)
	}
	sort.SliceStable(summaries, func(i, j int) bool {
		if summaries[i].Type.Key != summaries[j].Type.Key {
			return summaries[i].Type.Key < summaries[j].Type.Key
		}
		return summaries[i].Name < summaries[j].Name
	})

	payload := make([]map[string]any, 0, len(summaries))
	for _, s := range summaries {
		entry := map[string]any{
			"slug":        s.Name,
			"name":        s.Name,
			"type":        s.Type.Key,
			"description": truncateDescription(s.Description, shimMaxListDescriptionLen),
			"source":      "vault",
		}
		if s.LatestVersion != "" {
			entry["version"] = s.LatestVersion
		}
		payload = append(payload, entry)
	}
	return jsonContent(map[string]any{"assets": payload})
}

// handleLoadAsset implements load_my_asset. It downloads the latest version
// zip, splits primary prompt content from bundled files, and returns both —
// matching the Pulse shim's “AssetDetails“ shape so cloud clients can rely
// on a single payload schema regardless of vault type.
func (r *AssetShimRegistrar) handleLoadAsset(
	ctx context.Context, _ *mcp.CallToolRequest, args LoadAssetArgs,
) (*mcp.CallToolResult, any, error) {
	slug := strings.TrimSpace(args.Slug)
	if slug == "" {
		return errorContent("Missing required argument: slug")
	}

	details, err := r.Repo.GetAssetDetails(ctx, slug)
	if err != nil {
		return errorContent("Asset not found: " + slug)
	}
	if len(details.Versions) == 0 {
		return errorContent("Asset has no versions: " + slug)
	}

	latest := details.Versions[len(details.Versions)-1].Version
	zipData, err := r.fetchZip(ctx, slug, latest)
	if err != nil {
		return nil, nil, fmt.Errorf("download asset %s@%s: %w", slug, latest, err)
	}

	primaryFile := primaryFileNameForType(details.Type, details.Metadata)
	primaryContent, bundled, err := splitZipContents(zipData, primaryFile)
	if err != nil {
		return nil, nil, fmt.Errorf("read asset zip %s@%s: %w", slug, latest, err)
	}
	if primaryContent == "" && primaryFile == "" {
		// Config-only asset types (mcp, hook) deliberately have no prompt
		// file. Clients still want the bundled inventory + metadata so they
		// can decide whether to ``load_my_asset_file``, so we return the
		// envelope with an empty primary_content rather than erroring.
	} else if primaryContent == "" && len(bundled) == 0 {
		return errorContent("Asset has no content: " + slug)
	}

	out := map[string]any{
		"slug":            slug,
		"name":            details.Name,
		"type":            details.Type.Key,
		"description":     details.Description,
		"primary_file":    primaryFile,
		"primary_content": primaryContent,
		"bundled_files":   bundled,
		"version":         latest,
	}
	return jsonContent(out)
}

// handleLoadAssetFile implements load_my_asset_file. The handler downloads
// the latest version's zip, finds the requested entry, and returns its
// content. We refuse to serve the primary prompt file through this path so
// there's a single canonical way to fetch it (load_my_asset).
func (r *AssetShimRegistrar) handleLoadAssetFile(
	ctx context.Context, _ *mcp.CallToolRequest, args LoadAssetFileArgs,
) (*mcp.CallToolResult, any, error) {
	slug := strings.TrimSpace(args.Slug)
	path := strings.TrimSpace(args.Path)
	if slug == "" {
		return errorContent("Missing required argument: slug")
	}
	if path == "" {
		return errorContent("Missing required argument: path")
	}

	details, err := r.Repo.GetAssetDetails(ctx, slug)
	if err != nil {
		return errorContent("Asset not found: " + slug)
	}
	if len(details.Versions) == 0 {
		return errorContent("Asset has no versions: " + slug)
	}
	latest := details.Versions[len(details.Versions)-1].Version

	zipData, err := r.fetchZip(ctx, slug, latest)
	if err != nil {
		return nil, nil, fmt.Errorf("download asset %s@%s: %w", slug, latest, err)
	}

	primaryFile := primaryFileNameForType(details.Type, details.Metadata)
	if primaryFile != "" && path == primaryFile {
		return errorContent(fmt.Sprintf(
			"'%s' is the primary prompt file — use load_my_asset instead of load_my_asset_file", path,
		))
	}

	contentBytes, err := utils.ReadZipFile(zipData, path)
	if err != nil {
		return errorContent(fmt.Sprintf("File not found in asset %s: %s", slug, path))
	}
	return textContent(string(contentBytes))
}

// fetchZip returns the asset's zip bytes, reusing the last fetch when the
// caller asks for the same ``slug@version``. The cache holds at most one
// asset's zip at a time — different assets bust each other — so memory is
// bounded by the largest asset accessed in a session, not by the catalog
// size. The lock is held across the underlying ``GetAssetByVersion`` so a
// burst of concurrent requests for the same slug doesn't trigger a thundering
// herd of git pulls / zip builds.
func (r *AssetShimRegistrar) fetchZip(ctx context.Context, slug, version string) ([]byte, error) {
	key := slug + "@" + version
	r.zipCacheMu.Lock()
	defer r.zipCacheMu.Unlock()
	if r.zipCacheKey == key && r.zipCache != nil {
		return r.zipCache, nil
	}
	zipData, err := r.Repo.GetAssetByVersion(ctx, slug, version)
	if err != nil {
		return nil, err
	}
	r.zipCacheKey = key
	r.zipCache = zipData
	return zipData, nil
}

func filterByTypeKeys(in []AssetSummary, keys []string) []AssetSummary {
	keep := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		keep[k] = struct{}{}
	}
	out := in[:0:0]
	for _, s := range in {
		if _, ok := keep[s.Type.Key]; ok {
			out = append(out, s)
		}
	}
	return out
}

func filterBySearch(in []AssetSummary, search string) []AssetSummary {
	needle := strings.ToLower(search)
	out := in[:0:0]
	for _, s := range in {
		if strings.Contains(strings.ToLower(s.Name), needle) ||
			strings.Contains(strings.ToLower(s.Description), needle) {
			out = append(out, s)
		}
	}
	return out
}

// truncateDescription mirrors the shim's tier-1 trim. Long descriptions
// would otherwise blow up the catalog response on orgs with many assets.
func truncateDescription(text string, limit int) string {
	cleaned := strings.Join(strings.Fields(text), " ")
	if len(cleaned) <= limit {
		return cleaned
	}
	if limit < 1 {
		return ""
	}
	trimmed := strings.TrimRight(cleaned[:limit-1], " ")
	return trimmed + "…"
}

// primaryFileNameForType resolves the prompt-file name for an asset type.
// Prefers the explicit metadata entry (Skill.PromptFile, Rule.PromptFile, …)
// and falls back to the type's conventional default. Returns "" for asset
// types that have no prompt file (mcp, hook, claude-code-plugin).
func primaryFileNameForType(t asset.Type, meta *metadata.Metadata) string {
	if meta != nil {
		switch t.Key {
		case asset.TypeSkill.Key:
			if meta.Skill != nil && meta.Skill.PromptFile != "" {
				return meta.Skill.PromptFile
			}
		case asset.TypeRule.Key:
			if meta.Rule != nil && meta.Rule.PromptFile != "" {
				return meta.Rule.PromptFile
			}
		case asset.TypeAgent.Key:
			if meta.Agent != nil && meta.Agent.PromptFile != "" {
				return meta.Agent.PromptFile
			}
		case asset.TypeCommand.Key:
			if meta.Command != nil && meta.Command.PromptFile != "" {
				return meta.Command.PromptFile
			}
		}
	}
	switch t.Key {
	case asset.TypeSkill.Key:
		return "SKILL.md"
	case asset.TypeRule.Key:
		return "RULE.md"
	case asset.TypeAgent.Key:
		return "AGENT.md"
	case asset.TypeCommand.Key:
		return "COMMAND.md"
	}
	return ""
}

// splitZipContents extracts the primary file's content and inventories the
// rest. metadata.toml is kept out of the bundled-files list because it's an
// implementation detail of the vault layout, not a user-facing resource.
func splitZipContents(zipData []byte, primaryFile string) (string, []map[string]any, error) {
	if !utils.IsZipFile(zipData) {
		return "", nil, errors.New("invalid zip data")
	}
	entries, err := utils.ListZipEntries(zipData)
	if err != nil {
		return "", nil, err
	}
	var primary string
	bundled := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name, "/") {
			continue
		}
		if entry.Name == "metadata.toml" {
			continue
		}
		if primaryFile != "" && entry.Name == primaryFile {
			body, err := utils.ReadZipFile(zipData, entry.Name)
			if err != nil {
				return "", nil, err
			}
			primary = string(body)
			continue
		}
		// Use the central-directory size rather than decompressing each
		// bundled file. The MCP shim only needs ``size_bytes`` for the
		// inventory; the body is fetched on demand via load_my_asset_file.
		bundled = append(bundled, map[string]any{
			"path":       entry.Name,
			"size_bytes": entry.Size,
		})
	}
	return primary, bundled, nil
}

func jsonContent(payload any) (*mcp.CallToolResult, any, error) {
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(body)}},
	}, nil, nil
}

func textContent(text string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}, nil, nil
}

func errorContent(message string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: message}},
		IsError: true,
	}, nil, nil
}

// perTypeDescription is the description text used by the per-type list aliases.
// Kept in lockstep with sleuth/apps/mcp_gateway/asset_shim/tool_defs.py so the
// model sees the same wording regardless of which side serves the request.
func perTypeDescription(typeLabel, typePlural string) string {
	return fmt.Sprintf(
		"Lists the user's %s from their skills.new vault. Thin alias for "+
			"list_my_assets with type=%q preset — use this when the user "+
			"specifically asks about their %s.",
		typePlural, typeLabel, typePlural,
	)
}

const descListAssets = "Lists the user's own skills, rules, agents, and commands from their " +
	"personal skills.new vault. This is a SEPARATE catalog from any skills you " +
	"have loaded natively — the user has their own collection in skills.new " +
	"that you don't see unless you call this tool. " +
	"ALWAYS call this tool whenever the user: " +
	"(a) asks 'what skills do I have', 'what skills are available', 'tell me my " +
	"skills', 'list my skills', or any paraphrase; " +
	"(b) asks what tools, agents, commands, rules, or capabilities they have or " +
	"have access to; " +
	"(c) asks 'what can you do for me' or 'what do I have'; " +
	"(d) describes a task that might match a custom skill (domain-specific work, " +
	"internal workflows, project conventions, unusual topics) — even if you think " +
	"you can handle it without one; " +
	"(e) starts a new conversation — call this once at session start so you know " +
	"what's available before responding. " +
	"When answering 'what skills do I have' questions, combine the results from " +
	"this tool with any native skills and report the UNION of both sources. Never " +
	"answer from your native skill list alone. " +
	"By default the response omits hooks and MCP configs because those are CLI-" +
	"only lifecycle primitives (Claude Code / sx) and pointers to OTHER MCP " +
	"servers — neither is usable from this chat client. Pass type=hook or " +
	"type=mcp to list those explicitly if the user asks about them. " +
	"Returns slug, name, type, short description, source, and version per asset " +
	"— not the body. Follow up with load_my_asset to fetch a specific asset's " +
	"content once a topic matches."

const descLoadAsset = "Fetches the full body of one of the user's skills.new assets — SKILL.md, " +
	"RULE.md, AGENT.md, COMMAND.md, or the config payload for MCP/hook assets — " +
	"plus an inventory of bundled files (paths + sizes only, no content). " +
	"Call this whenever the user's task matches an entry returned by " +
	"list_my_assets, OR when the user explicitly names a skill they have. Prefer " +
	"the user's own skill content over your own knowledge when they overlap. " +
	"Follow up with load_my_asset_file to pull any bundled resource on demand."

const descLoadAssetFile = "Fetches the content of one bundled file for a user-authored asset. Call after " +
	"load_my_asset has listed a bundled file in its bundled_files inventory " +
	"and the task needs that file (reference doc, script, template, schema, etc.)."
