package vault

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Khan/genqlient/graphql"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/buildinfo"
	"github.com/sleuth-io/sx/internal/cache"
	"github.com/sleuth-io/sx/internal/git"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/mgmt"
	vaultgql "github.com/sleuth-io/sx/internal/vault/graphql"
	"github.com/sleuth-io/sx/internal/version"
)

// authDoer wraps an http.Client to add the Sleuth auth + User-Agent headers
// on every request. Used to back the genqlient client so generated query
// functions share the same auth + User-Agent headers as direct httpClient.Do
// calls elsewhere in this package.
type authDoer struct {
	client    *http.Client
	authToken string
}

func (d *authDoer) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", buildinfo.GetUserAgent())
	if d.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+d.authToken)
	}
	return d.client.Do(req)
}

// gqlClient builds a genqlient client that reuses the vault's httpClient
// and auth. Cheap to construct; called per request rather than cached so
// authToken changes (e.g. bot key swaps) are picked up.
func (s *SleuthVault) gqlClient() graphql.Client {
	return graphql.NewClient(s.serverURL+"/graphql", &authDoer{
		client:    s.httpClient,
		authToken: s.authToken,
	})
}

// scopeEntityPersonal is the scope entity value for personal (user-only) installations.
const scopeEntityPersonal = "personal"

// vaultAssetsByNamePageSize must mirror the `first:` value in
// internal/vault/graphql/operations/vault_assets_by_name.graphql. Used to
// detect a saturated result page so the caller can be told that more
// matches exist beyond the page.
const vaultAssetsByNamePageSize = 50

// SleuthVault implements Vault for Sleuth HTTP servers
type SleuthVault struct {
	serverURL       string
	authToken       string
	httpClient      *http.Client
	streamingClient *http.Client // Longer timeout for SSE streaming
	httpHandler     *HTTPSourceHandler
	pathHandler     *PathSourceHandler
	gitHandler      *GitSourceHandler
}

// refreshLockFileCache fetches a fresh lock file from the server and updates the local cache.
// This ensures subsequent operations see the latest state after a mutation.
func (s *SleuthVault) refreshLockFileCache() {
	data, etag, _, err := s.GetLockFile(context.Background(), "")
	if err != nil {
		return
	}
	if etag != "" {
		_ = cache.SaveETag(s.serverURL, etag)
	}
	_ = cache.SaveLockFile(s.serverURL, data)
}

// NewSleuthVault creates a new Sleuth repository. If SX_BOT_KEY is set
// in the environment, it overrides authToken — the bot's API key
// becomes the bearer for every request, so the same Sleuth vault wired
// to a CI runner authenticates as the bot rather than the user who ran
// `sx cloud connect` on a different machine. The user-token path stays
// untouched when SX_BOT_KEY is empty so interactive use is unchanged.
func NewSleuthVault(serverURL, authToken string) *SleuthVault {
	if botKey := strings.TrimSpace(os.Getenv(mgmt.SXBotKeyEnv)); botKey != "" {
		authToken = botKey
	}
	gitClient := git.NewClient()
	return &SleuthVault{
		serverURL:       serverURL,
		authToken:       authToken,
		httpClient:      &http.Client{Timeout: 30 * time.Second},
		streamingClient: &http.Client{Timeout: 120 * time.Second}, // Longer timeout for AI queries
		httpHandler:     NewHTTPSourceHandler(authToken),
		pathHandler:     NewPathSourceHandler(""), // Lock file dir not applicable for Sleuth
		gitHandler:      NewGitSourceHandler(gitClient),
	}
}

// GetScopeOptions returns additional scope options for the Sleuth vault
func (s *SleuthVault) GetScopeOptions() []ScopeOption {
	return []ScopeOption{
		{Label: "Just for me", Value: scopeEntityPersonal, Description: "Install only for your account"},
	}
}

// Authenticate performs authentication with the Sleuth server
func (s *SleuthVault) Authenticate(ctx context.Context) (string, error) {
	// Token is always provided via config during initialization
	// OAuth device flow is performed during 'sx init' and token is saved to config
	return s.authToken, nil
}

// GetLockFile retrieves the lock file from the Sleuth server
func (s *SleuthVault) GetLockFile(ctx context.Context, cachedETag string) (content []byte, etag string, notModified bool, err error) {
	endpoint := s.serverURL + "/api/skills/sx.lock"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, "", false, fmt.Errorf("failed to create request: %w", err)
	}

	// Add headers
	req.Header.Set("User-Agent", buildinfo.GetUserAgent())
	if s.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.authToken)
	}
	if cachedETag != "" {
		req.Header.Set("If-None-Match", cachedETag)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, "", false, fmt.Errorf("failed to fetch lock file: %w", err)
	}
	defer resp.Body.Close()

	// Check for 304 Not Modified
	if resp.StatusCode == http.StatusNotModified {
		return nil, cachedETag, true, nil
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, "", false, ErrLockFileNotFound
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", false, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	// Read response body
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", false, fmt.Errorf("failed to read response body: %w", err)
	}

	// Get ETag from response
	newETag := resp.Header.Get("ETag")

	return data, newETag, false, nil
}

// GetAsset downloads an asset using its source configuration
func (s *SleuthVault) GetAsset(ctx context.Context, asset *lockfile.Asset) ([]byte, error) {
	// Dispatch to appropriate source handler based on asset source type
	switch asset.GetSourceType() {
	case "http":
		return s.httpHandler.Fetch(ctx, asset)
	case "path":
		return s.pathHandler.Fetch(ctx, asset)
	case "git":
		return s.gitHandler.Fetch(ctx, asset)
	default:
		return nil, fmt.Errorf("unsupported source type: %s", asset.GetSourceType())
	}
}

// AddAsset uploads an asset to the Sleuth server
func (s *SleuthVault) AddAsset(ctx context.Context, asset *lockfile.Asset, zipData []byte) error {
	endpoint := s.serverURL + "/api/skills/assets"

	// Create multipart writer
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add file part
	part, err := writer.CreateFormFile("file", fmt.Sprintf("%s-%s.zip", asset.Name, asset.Version))
	if err != nil {
		return fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(zipData); err != nil {
		return fmt.Errorf("failed to write zip data: %w", err)
	}

	// Add metadata fields
	_ = writer.WriteField("name", asset.Name)
	_ = writer.WriteField("version", asset.Version)
	_ = writer.WriteField("type", asset.Type.Key)

	// Close writer
	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close writer: %w", err)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", buildinfo.GetUserAgent())
	if s.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.authToken)
	}

	// Execute request
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to upload asset: %w", err)
	}
	defer resp.Body.Close()

	// Parse response
	var uploadResp struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
		Asset   struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			URL     string `json:"url"`
		} `json:"asset"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&uploadResp); err != nil {
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			return fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		if uploadResp.Error != "" {
			// Check for version conflict error
			if strings.Contains(uploadResp.Error, "already exists") {
				return &ErrVersionExists{
					Name:    asset.Name,
					Version: asset.Version,
					Message: uploadResp.Error,
				}
			}
			return errors.New(uploadResp.Error)
		}
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	if !uploadResp.Success {
		if uploadResp.Error != "" {
			return errors.New(uploadResp.Error)
		}
		return errors.New("upload failed: server returned success=false")
	}

	// Update asset with source information if server returns URL
	if uploadResp.Asset.URL != "" {
		asset.SourceHTTP = &lockfile.SourceHTTP{
			URL: uploadResp.Asset.URL,
		}
	}

	return nil
}

// GetVersionList retrieves available versions for an asset
func (s *SleuthVault) GetVersionList(ctx context.Context, name string) ([]string, error) {
	endpoint := fmt.Sprintf("%s/api/skills/assets/%s/list.txt", s.serverURL, name)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", buildinfo.GetUserAgent())
	if s.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.authToken)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch version list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	// Read plain text response (newline-separated versions)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse versions using common parser
	versions := parseVersionList(body)

	// Sort versions in ascending order (oldest first) to ensure consistency
	// regardless of backend ordering
	return version.Sort(versions), nil
}

// GetMetadata retrieves metadata for a specific asset version
func (s *SleuthVault) GetMetadata(ctx context.Context, name, version string) (*metadata.Metadata, error) {
	endpoint := fmt.Sprintf("%s/api/skills/assets/%s/%s/metadata.toml", s.serverURL, name, version)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", buildinfo.GetUserAgent())
	if s.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.authToken)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	// Read and parse metadata
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return metadata.Parse(data)
}

// GetAssetByVersion downloads an asset by name and version
func (s *SleuthVault) GetAssetByVersion(ctx context.Context, name, ver string) ([]byte, error) {
	endpoint := fmt.Sprintf("%s/api/skills/assets/%s/%s/%s-%s.zip", s.serverURL, name, ver, name, ver)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", buildinfo.GetUserAgent())
	if s.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.authToken)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch asset: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(resp.Body)
}

// VerifyIntegrity checks hashes and sizes for downloaded assets
func (s *SleuthVault) VerifyIntegrity(data []byte, hashes map[string]string, size int64) error {
	// Verify size if provided
	if size > 0 {
		if int64(len(data)) != size {
			return fmt.Errorf("size mismatch: expected %d bytes, got %d bytes", size, len(data))
		}
	}

	// Verify hashes (httpHandler already does this, but provide a standalone method)
	return s.httpHandler.verifyHashes(data, hashes)
}

// PostUsageStats sends asset usage statistics to the Sleuth server
func (s *SleuthVault) PostUsageStats(ctx context.Context, jsonlData string) error {
	endpoint := s.serverURL + "/api/skills/usage"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader([]byte(jsonlData)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-ndjson")
	req.Header.Set("Authorization", "Bearer "+s.authToken)
	req.Header.Set("User-Agent", buildinfo.GetUserAgent())

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to post usage stats: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// RemoveAsset removes an asset from the Sleuth server's lock file.
// The delete flag is passed to the server mutation for permanent deletion.
func (s *SleuthVault) RemoveAsset(ctx context.Context, assetName, version string, delete bool) error {
	if version != "" {
		return errors.New("version-specific removal is not supported for Sleuth vaults")
	}

	input := vaultgql.RemoveAssetInstallationsInput{AssetName: assetName}
	// Only set Delete when the caller asked for permanent deletion, mirroring
	// the old "omit when false" wire shape. Server-side default is false, so
	// nil and false are equivalent on this field.
	if delete {
		input.Delete = &delete
	}

	resp, err := vaultgql.RemoveAssetInstallations(ctx, s.gqlClient(), input)
	if err != nil {
		return err
	}
	if resp.RemoveAssetInstallations == nil {
		return errors.New("missing removeAssetInstallations payload in response")
	}
	if err := gqlMutationErrors(resp.RemoveAssetInstallations.Errors); err != nil {
		return err
	}
	if !resp.RemoveAssetInstallations.Success {
		return errors.New("failed to remove asset installations")
	}
	s.refreshLockFileCache()
	return nil
}

// RenameAsset renames an asset on the Sleuth server using a GraphQL mutation.
func (s *SleuthVault) RenameAsset(ctx context.Context, oldName, newName string) error {
	resp, err := vaultgql.RenameAsset(ctx, s.gqlClient(), vaultgql.RenameAssetInput{
		OldName: oldName,
		NewName: newName,
	})
	if err != nil {
		return err
	}
	if resp.RenameAsset == nil {
		return errors.New("missing renameAsset payload in response")
	}
	if err := gqlMutationErrors(resp.RenameAsset.Errors); err != nil {
		return err
	}
	if !resp.RenameAsset.Success {
		return errors.New("failed to rename asset")
	}
	s.refreshLockFileCache()
	return nil
}

// sxAssetTypeToGQL maps sx's internal asset.Type to the generated
// vaultgql.AssetType enum. Returns the zero AssetType (empty string) for
// asset kinds the backend doesn't model — callers should skip those.
func sxAssetTypeToGQL(t asset.Type) vaultgql.AssetType {
	switch t.Key {
	case asset.TypeSkill.Key:
		return vaultgql.AssetTypeSkill
	case asset.TypeMCP.Key:
		return vaultgql.AssetTypeMcp
	case asset.TypeAgent.Key:
		return vaultgql.AssetTypeAgent
	case asset.TypeCommand.Key:
		return vaultgql.AssetTypeCommand
	case asset.TypeHook.Key:
		return vaultgql.AssetTypeHook
	case asset.TypeRule.Key:
		return vaultgql.AssetTypeRule
	case asset.TypeClaudeCodePlugin.Key:
		return vaultgql.AssetTypeClaudeCodePlugin
	}
	return ""
}

// gqlAssetTypeToSX is the inverse of sxAssetTypeToGQL. For unknown enum
// values it returns an asset.Type with the raw GraphQL key (uppercased),
// which IsValid() reports as false so callers can detect and warn.
func gqlAssetTypeToSX(t vaultgql.AssetType) asset.Type {
	switch t {
	case vaultgql.AssetTypeSkill:
		return asset.TypeSkill
	case vaultgql.AssetTypeMcp:
		return asset.TypeMCP
	case vaultgql.AssetTypeAgent:
		return asset.TypeAgent
	case vaultgql.AssetTypeCommand:
		return asset.TypeCommand
	case vaultgql.AssetTypeHook:
		return asset.TypeHook
	case vaultgql.AssetTypeRule:
		return asset.TypeRule
	case vaultgql.AssetTypeClaudeCodePlugin:
		return asset.TypeClaudeCodePlugin
	}
	return asset.Type{Key: string(t)}
}

// ListAssets retrieves a list of all assets in the vault using GraphQL
func (s *SleuthVault) ListAssets(ctx context.Context, opts ListAssetsOptions) (*ListAssetsResult, error) {
	// If no type specified, query all asset types and combine results
	if opts.Type == "" {
		allAssets := make([]AssetSummary, 0)
		var lastErr error
		for _, t := range asset.AllTypes() {
			// Skip types not supported by the backend
			if sxAssetTypeToGQL(t) == "" {
				continue
			}
			typeOpts := ListAssetsOptions{
				Type:   t.Key,
				Search: opts.Search,
				Limit:  opts.Limit,
			}
			result, err := s.listAssetsByType(ctx, typeOpts)
			if err != nil {
				// Track the error but continue - we want to return partial results
				// if some types succeed
				lastErr = err
				continue
			}
			allAssets = append(allAssets, result.Assets...)
		}
		// If we got no assets and had errors, return the last error
		if len(allAssets) == 0 && lastErr != nil {
			return nil, lastErr
		}
		return &ListAssetsResult{Assets: allAssets}, nil
	}

	return s.listAssetsByType(ctx, opts)
}

// listAssetsByType retrieves assets of a specific type from the vault
func (s *SleuthVault) listAssetsByType(ctx context.Context, opts ListAssetsOptions) (*ListAssetsResult, error) {
	// Set default limit if not specified (max 50 enforced by backend)
	limit := opts.Limit
	if limit == 0 || limit > 50 {
		limit = 50
	}

	gqlType := sxAssetTypeToGQL(asset.FromString(opts.Type))
	if gqlType == "" {
		// Type not supported by backend, return empty result
		return &ListAssetsResult{Assets: []AssetSummary{}}, nil
	}

	var searchPtr *string
	if opts.Search != "" {
		searchPtr = &opts.Search
	}

	resp, err := vaultgql.VaultAssets(ctx, s.gqlClient(), &limit, gqlType, searchPtr)
	if err != nil {
		return nil, err
	}

	// Convert to result struct. VaultAsset is a polymorphic interface;
	// shared getters cover every concrete subtype (Skill, MCP, Agent, ...).
	result := &ListAssetsResult{
		Assets: make([]AssetSummary, 0, len(resp.Vault.Assets.Nodes)),
	}
	for _, node := range resp.Vault.Assets.Nodes {
		assetType := gqlAssetTypeToSX(node.GetType())
		if !assetType.IsValid() {
			log := logger.Get()
			log.Warn("unknown asset type from GraphQL", "type", string(node.GetType()), "asset", node.GetSlug())
		}
		result.Assets = append(result.Assets, AssetSummary{
			Name:          node.GetSlug(),
			Type:          assetType,
			LatestVersion: node.GetLatestVersion(),
			VersionsCount: node.GetVersionsCount(),
			Description:   node.GetDescription(),
			CreatedAt:     node.GetCreatedAt(),
			UpdatedAt:     node.GetUpdatedAt(),
		})
	}

	return result, nil
}

// GetAssetDetails retrieves detailed information about a specific asset using GraphQL
func (s *SleuthVault) GetAssetDetails(ctx context.Context, name string) (*AssetDetails, error) {
	resp, err := vaultgql.VaultAssetsByName(ctx, s.gqlClient(), name)
	if err != nil {
		return nil, err
	}

	// Find exact match by name. VaultAsset is a polymorphic interface
	// (Skill/MCP/Agent/...); the shared getters cover every concrete subtype
	// so we don't switch on type here.
	var match vaultgql.VaultAssetsByNameVaultAssetsVaultAssetsConnectionNodesVaultAsset
	for _, node := range resp.Vault.Assets.Nodes {
		if node.GetName() == name {
			match = node
			break
		}
	}
	if match == nil {
		if len(resp.Vault.Assets.Nodes) >= vaultAssetsByNamePageSize {
			logger.Get().Warn(
				"VaultAssetsByName result saturated page size; exact match may exist beyond the page",
				"page_size", vaultAssetsByNamePageSize,
				"search", name,
			)
		}
		return nil, fmt.Errorf("asset '%s' not found", name)
	}

	versions := match.GetVersions()
	details := &AssetDetails{
		Name:        match.GetName(),
		Type:        gqlAssetTypeToSX(match.GetType()),
		Description: match.GetDescription(),
		CreatedAt:   match.GetCreatedAt(),
		UpdatedAt:   match.GetUpdatedAt(),
		Versions:    make([]AssetVersion, 0, len(versions)),
	}

	for _, v := range versions {
		details.Versions = append(details.Versions, AssetVersion{
			Version:    v.Version,
			CreatedAt:  v.CreatedAt,
			FilesCount: v.FilesCount,
		})
	}

	// Backend returns versions in descending order (newest first)
	// Reverse to ascending order (oldest first) for consistency with GitVault/PathVault
	for i, j := 0, len(details.Versions)-1; i < j; i, j = i+1, j-1 {
		details.Versions[i], details.Versions[j] = details.Versions[j], details.Versions[i]
	}

	// Get metadata for latest version if available
	if len(details.Versions) > 0 {
		latestVersion := details.Versions[len(details.Versions)-1].Version
		meta, err := s.GetMetadata(ctx, name, latestVersion)
		if err == nil {
			details.Metadata = meta
		}
		// Ignore metadata errors - not critical for asset details
	}

	return details, nil
}

// QueryIntegrationStream queries integrated services using SSE streaming.
// The onEvent callback is called for each event received, which can be used
// to send MCP log notifications to keep the connection alive.
func (s *SleuthVault) QueryIntegrationStream(
	ctx context.Context,
	query, integration string,
	gitContext any,
	onEvent func(eventType, content string),
) (string, error) {
	endpoint := s.serverURL + "/api/skills/ai-query/stream"

	// Build JSON body matching the SSE endpoint format
	reqBody := map[string]any{
		"query":    query,
		"provider": strings.ToUpper(integration),
		"context":  gitContext,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", buildinfo.GetUserAgent())
	if s.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.authToken)
	}

	resp, err := s.streamingClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute SSE request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	// Read SSE events line by line
	scanner := bufio.NewScanner(resp.Body)
	// Increase buffer size to 1MB to handle large SSE responses (default is 64KB)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 1024*1024)
	var finalResult string
	var finalError string

	for scanner.Scan() {
		line := scanner.Text()

		// SSE format: "data: {...}"
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		var event map[string]any
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		eventType, _ := event["type"].(string)

		// Stream tool call events to callback for progress visibility
		if eventType == "ToolCallEvent" {
			toolName, _ := event["tool"].(string)
			if onEvent != nil && toolName != "" {
				onEvent("tool_call", fmt.Sprintf("Calling %s...", toolName))
			}
		}

		// Capture final result
		if eventType == "done" {
			if result, ok := event["result"].(map[string]any); ok {
				finalResult, _ = result["data"].(string)
			}
			break
		}

		// Capture error
		if eventType == "error" {
			finalError, _ = event["error"].(string)
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("error reading SSE stream: %w", err)
	}

	if finalError != "" {
		return "", fmt.Errorf("query error: %s", finalError)
	}

	return finalResult, nil
}

// SetInstallations sets the installation scopes for an asset using GraphQL mutation
func (s *SleuthVault) SetInstallations(ctx context.Context, asset *lockfile.Asset, scopeEntity string) error {
	// Build repositories list from asset scopes. Empty slice means global install
	// (server interprets repositories=[] as org-wide when personalOnly is false).
	var repositories []vaultgql.RepositoryInstallationInput
	if asset.IsGlobal() {
		repositories = []vaultgql.RepositoryInstallationInput{}
	} else {
		for _, scope := range asset.Scopes {
			repositories = append(repositories, vaultgql.RepositoryInstallationInput{
				Url:   scope.Repo,
				Paths: scope.Paths,
			})
		}
	}

	input := vaultgql.SetAssetInstallationsInput{
		AssetName:    asset.Name,
		Repositories: repositories,
	}
	if asset.Version != "" {
		input.AssetVersion = &asset.Version
	}
	if scopeEntity == scopeEntityPersonal {
		personalOnly := true
		input.PersonalOnly = &personalOnly
	}

	resp, err := vaultgql.SetAssetInstallations(ctx, s.gqlClient(), input)
	if err != nil {
		// Preserve the friendly PERMISSION_DENIED message for the most
		// common error mode (caller lacks write permission). The server
		// surfaces it as a top-level GraphQL error string, which genqlient
		// includes verbatim in the wrapped err.
		if strings.Contains(err.Error(), "PERMISSION_DENIED") {
			return fmt.Errorf("permission denied. Check that you have write permissions on %s", s.serverURL)
		}
		return err
	}
	if resp.SetAssetInstallations == nil {
		return errors.New("missing setAssetInstallations payload in response")
	}
	if err := gqlMutationErrors(resp.SetAssetInstallations.Errors); err != nil {
		return err
	}

	// Invalidate lock file cache so next GetLockFile fetches fresh data.
	// This is best-effort - ignore errors.
	_ = cache.InvalidateLockFileCache(s.serverURL)

	return nil
}

// InheritInstallations is a no-op for Sleuth vaults.
// The server auto-inherits installations when AddAsset uploads a new version.
func (s *SleuthVault) InheritInstallations(ctx context.Context, asset *lockfile.Asset) error {
	return nil
}

// Role represents a skill profile (role) from the server
type Role struct {
	Title       string `json:"title"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
}

// RoleListResponse represents the response from the roles list endpoint
type RoleListResponse struct {
	Roles  []Role  `json:"profiles"`
	Active *string `json:"active"`
}

// ListRoles retrieves the list of available roles from the server
func (s *SleuthVault) ListRoles(ctx context.Context) (*RoleListResponse, error) {
	endpoint := s.serverURL + "/api/skills/sx.profiles"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", buildinfo.GetUserAgent())
	if s.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.authToken)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch roles: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result RoleListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse roles response: %w", err)
	}

	return &result, nil
}

// SetActiveRole sets or clears the active role on the server.
// Pass nil to clear the active role.
func (s *SleuthVault) SetActiveRole(ctx context.Context, slug *string) (*Role, error) {
	endpoint := s.serverURL + "/api/skills/sx.profiles/active"

	reqBody := map[string]*string{"slug": slug}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", buildinfo.GetUserAgent())
	if s.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.authToken)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to set active role: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		var errResp struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && errResp.Error != "" {
			return nil, fmt.Errorf("%s", errResp.Error)
		}
		return nil, errors.New("role not found")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Profile *Role `json:"profile"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result.Profile, nil
}
