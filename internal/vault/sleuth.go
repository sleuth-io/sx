package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/buildinfo"
	"github.com/sleuth-io/sx/internal/cache"
	sleuthConfig "github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/git"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/version"
)

// SleuthVault implements Vault for Sleuth HTTP servers
type SleuthVault struct {
	serverURL   string
	authToken   string
	httpClient  *http.Client
	httpHandler *HTTPSourceHandler
	pathHandler *PathSourceHandler
	gitHandler  *GitSourceHandler
}

// NewSleuthVault creates a new Sleuth repository
func NewSleuthVault(serverURL, authToken string) *SleuthVault {
	gitClient := git.NewClient()
	return &SleuthVault{
		serverURL:   serverURL,
		authToken:   authToken,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		httpHandler: NewHTTPSourceHandler(authToken),
		pathHandler: NewPathSourceHandler(""), // Lock file dir not applicable for Sleuth
		gitHandler:  NewGitSourceHandler(gitClient),
	}
}

// Authenticate performs authentication with the Sleuth server
func (s *SleuthVault) Authenticate(ctx context.Context) (string, error) {
	if s.authToken != "" {
		// Already have a token
		return s.authToken, nil
	}

	// Perform OAuth device code flow
	token, err := sleuthConfig.Authenticate(ctx, s.serverURL)
	if err != nil {
		return "", fmt.Errorf("authentication failed: %w", err)
	}

	s.authToken = token
	return token, nil
}

// GetLockFile retrieves the lock file from the Sleuth server
func (s *SleuthVault) GetLockFile(ctx context.Context, cachedETag string) (content []byte, etag string, notModified bool, err error) {
	endpoint := s.serverURL + "/api/skills/sx.lock"

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
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
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, body)
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

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse response
	var uploadResp struct {
		Success bool `json:"success"`
		Asset   struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			URL     string `json:"url"`
		} `json:"asset"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&uploadResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if !uploadResp.Success {
		return fmt.Errorf("upload failed: server returned success=false")
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

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
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

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
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

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader([]byte(jsonlData)))
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

// RemoveAsset removes an asset from the Sleuth server's lock file
func (s *SleuthVault) RemoveAsset(ctx context.Context, assetName, version string) error {
	// Use removeAssetInstallations mutation to clear all installations
	mutation := `mutation RemoveAssetInstallations($input: RemoveAssetInstallationsInput!) {
		removeAssetInstallations(input: $input) {
			success
			errors {
				field
				messages
			}
		}
	}`

	variables := map[string]interface{}{
		"input": map[string]interface{}{
			"assetName": assetName,
		},
	}

	var gqlResp struct {
		Data struct {
			RemoveAssetInstallations struct {
				Success *bool `json:"success"`
				Errors  []struct {
					Field    string   `json:"field"`
					Messages []string `json:"messages"`
				} `json:"errors"`
			} `json:"removeAssetInstallations"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := s.executeGraphQLQuery(ctx, mutation, variables, &gqlResp); err != nil {
		return err
	}

	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("GraphQL error: %s", gqlResp.Errors[0].Message)
	}

	if len(gqlResp.Data.RemoveAssetInstallations.Errors) > 0 {
		err := gqlResp.Data.RemoveAssetInstallations.Errors[0]
		return fmt.Errorf("%s: %s", err.Field, err.Messages[0])
	}

	if gqlResp.Data.RemoveAssetInstallations.Success == nil || !*gqlResp.Data.RemoveAssetInstallations.Success {
		return fmt.Errorf("failed to remove asset installations")
	}

	return nil
}

// ListAssets retrieves a list of all assets in the vault using GraphQL
func (s *SleuthVault) ListAssets(ctx context.Context, opts ListAssetsOptions) (*ListAssetsResult, error) {
	// Build GraphQL query matching the actual schema (uses Relay pagination)
	query := `query VaultAssets($first: Int, $type: String, $search: String) {
		vault {
			assets(first: $first, type: $type, search: $search) {
				nodes {
					name
					type
					latestVersion
					versionsCount
					description
					createdAt
					updatedAt
				}
			}
		}
	}`

	// Set default limit if not specified (max 50 enforced by backend)
	limit := opts.Limit
	if limit == 0 || limit > 50 {
		limit = 50
	}

	variables := map[string]interface{}{
		"first": limit,
	}
	if opts.Type != "" {
		variables["type"] = opts.Type
	}
	if opts.Search != "" {
		variables["search"] = opts.Search
	}

	// Make GraphQL request
	var gqlResp struct {
		Data struct {
			Vault struct {
				Assets struct {
					Nodes []struct {
						Name          string    `json:"name"`
						Type          string    `json:"type"`
						LatestVersion string    `json:"latestVersion"`
						VersionsCount int       `json:"versionsCount"`
						Description   string    `json:"description"`
						CreatedAt     time.Time `json:"createdAt"`
						UpdatedAt     time.Time `json:"updatedAt"`
					} `json:"nodes"`
				} `json:"assets"`
			} `json:"vault"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := s.executeGraphQLQuery(ctx, query, variables, &gqlResp); err != nil {
		return nil, err
	}

	if len(gqlResp.Errors) > 0 {
		return nil, fmt.Errorf("GraphQL error: %s", gqlResp.Errors[0].Message)
	}

	// Convert to result struct
	result := &ListAssetsResult{
		Assets: make([]AssetSummary, 0, len(gqlResp.Data.Vault.Assets.Nodes)),
	}

	for _, node := range gqlResp.Data.Vault.Assets.Nodes {
		result.Assets = append(result.Assets, AssetSummary{
			Name:          node.Name,
			Type:          asset.FromString(node.Type),
			LatestVersion: node.LatestVersion,
			VersionsCount: node.VersionsCount,
			Description:   node.Description,
			CreatedAt:     node.CreatedAt,
			UpdatedAt:     node.UpdatedAt,
		})
	}

	return result, nil
}

// GetAssetDetails retrieves detailed information about a specific asset using GraphQL
func (s *SleuthVault) GetAssetDetails(ctx context.Context, name string) (*AssetDetails, error) {
	// Build GraphQL query matching the actual schema
	query := `query VaultAsset($name: String!) {
		vault {
			asset(name: $name) {
				name
				type
				description
				createdAt
				updatedAt
				versions {
					version
					createdAt
					filesCount
				}
			}
		}
	}`

	variables := map[string]interface{}{
		"name": name,
	}

	// Make GraphQL request
	var gqlResp struct {
		Data struct {
			Vault struct {
				Asset *struct {
					Name        string    `json:"name"`
					Type        string    `json:"type"`
					Description string    `json:"description"`
					CreatedAt   time.Time `json:"createdAt"`
					UpdatedAt   time.Time `json:"updatedAt"`
					Versions    []struct {
						Version    string    `json:"version"`
						CreatedAt  time.Time `json:"createdAt"`
						FilesCount int       `json:"filesCount"`
					} `json:"versions"`
				} `json:"asset"`
			} `json:"vault"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := s.executeGraphQLQuery(ctx, query, variables, &gqlResp); err != nil {
		return nil, err
	}

	if len(gqlResp.Errors) > 0 {
		return nil, fmt.Errorf("GraphQL error: %s", gqlResp.Errors[0].Message)
	}

	if gqlResp.Data.Vault.Asset == nil {
		return nil, fmt.Errorf("asset '%s' not found", name)
	}

	assetData := gqlResp.Data.Vault.Asset

	// Convert to result struct
	details := &AssetDetails{
		Name:        assetData.Name,
		Type:        asset.FromString(assetData.Type),
		Description: assetData.Description,
		CreatedAt:   assetData.CreatedAt,
		UpdatedAt:   assetData.UpdatedAt,
		Versions:    make([]AssetVersion, 0, len(assetData.Versions)),
	}

	for _, v := range assetData.Versions {
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

// executeGraphQLQuery executes a GraphQL query against the Sleuth server
func (s *SleuthVault) executeGraphQLQuery(ctx context.Context, query string, variables map[string]interface{}, result interface{}) error {
	endpoint := s.serverURL + "/graphql"

	// Build request body
	reqBody := map[string]interface{}{
		"query":     query,
		"variables": variables,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal GraphQL request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", buildinfo.GetUserAgent())
	if s.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.authToken)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute GraphQL query: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("failed to parse GraphQL response: %w", err)
	}

	return nil
}

// SetInstallations sets the installation scopes for an asset using GraphQL mutation
func (s *SleuthVault) SetInstallations(ctx context.Context, asset *lockfile.Asset) error {
	mutation := `mutation SetAssetInstallations($input: SetAssetInstallationsInput!) {
		setAssetInstallations(input: $input) {
			asset {
				name
				latestVersion
			}
			errors {
				field
				messages
			}
		}
	}`

	// Build repositories list from asset scopes
	var repositories []map[string]interface{}

	if asset.IsGlobal() {
		// Empty array for global installation
		repositories = []map[string]interface{}{}
	} else {
		// Convert lockfile.Scope to repository installation format
		for _, scope := range asset.Scopes {
			repo := map[string]interface{}{
				"url": scope.Repo,
			}
			if len(scope.Paths) > 0 {
				repo["paths"] = scope.Paths
			}
			repositories = append(repositories, repo)
		}
	}

	variables := map[string]interface{}{
		"input": map[string]interface{}{
			"assetName":    asset.Name,
			"assetVersion": asset.Version,
			"repositories": repositories,
		},
	}

	var gqlResp struct {
		Data struct {
			SetAssetInstallations struct {
				Asset *struct {
					Name          string `json:"name"`
					LatestVersion string `json:"latestVersion"`
				} `json:"asset"`
				Errors []struct {
					Field    string   `json:"field"`
					Messages []string `json:"messages"`
				} `json:"errors"`
			} `json:"setAssetInstallations"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := s.executeGraphQLQuery(ctx, mutation, variables, &gqlResp); err != nil {
		return err
	}

	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("GraphQL error: %s", gqlResp.Errors[0].Message)
	}

	if len(gqlResp.Data.SetAssetInstallations.Errors) > 0 {
		err := gqlResp.Data.SetAssetInstallations.Errors[0]
		return fmt.Errorf("%s: %s", err.Field, err.Messages[0])
	}

	// Invalidate lock file cache so next GetLockFile fetches fresh data
	// This is best-effort - ignore errors
	_ = cache.InvalidateLockFileCache(s.serverURL)

	return nil
}
