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

	"github.com/sleuth-io/sx/internal/buildinfo"
	sleuthConfig "github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/git"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/metadata"
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
	endpoint := s.serverURL + "/api/skills/artifacts"

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
	endpoint := fmt.Sprintf("%s/api/skills/artifacts/%s/list.txt", s.serverURL, name)

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
	return parseVersionList(body), nil
}

// GetMetadata retrieves metadata for a specific asset version
func (s *SleuthVault) GetMetadata(ctx context.Context, name, version string) (*metadata.Metadata, error) {
	endpoint := fmt.Sprintf("%s/api/skills/artifacts/%s/%s/metadata.toml", s.serverURL, name, version)

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
	endpoint := fmt.Sprintf("%s/api/skills/installations/%s/%s", s.serverURL, assetName, version)

	req, err := http.NewRequestWithContext(ctx, "DELETE", endpoint, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", buildinfo.GetUserAgent())
	if s.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.authToken)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to remove asset: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
