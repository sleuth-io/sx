package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/sleuth-io/skills/internal/buildinfo"
	sleuthConfig "github.com/sleuth-io/skills/internal/config"
	"github.com/sleuth-io/skills/internal/git"
	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/metadata"
)

// SleuthRepository implements Repository for Sleuth HTTP servers
type SleuthRepository struct {
	serverURL   string
	authToken   string
	httpClient  *http.Client
	httpHandler *HTTPSourceHandler
	pathHandler *PathSourceHandler
	gitHandler  *GitSourceHandler
}

// NewSleuthRepository creates a new Sleuth repository
func NewSleuthRepository(serverURL, authToken string) *SleuthRepository {
	gitClient := git.NewClient()
	return &SleuthRepository{
		serverURL:   serverURL,
		authToken:   authToken,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		httpHandler: NewHTTPSourceHandler(),
		pathHandler: NewPathSourceHandler(""), // Lock file dir not applicable for Sleuth
		gitHandler:  NewGitSourceHandler(gitClient),
	}
}

// Authenticate performs authentication with the Sleuth server
func (s *SleuthRepository) Authenticate(ctx context.Context) (string, error) {
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
func (s *SleuthRepository) GetLockFile(ctx context.Context, cachedETag string) (content []byte, etag string, notModified bool, err error) {
	endpoint := s.serverURL + "/api/skills/lock"

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

// GetArtifact downloads an artifact using its source configuration
func (s *SleuthRepository) GetArtifact(ctx context.Context, artifact *lockfile.Artifact) ([]byte, error) {
	// Dispatch to appropriate source handler based on artifact source type
	switch artifact.GetSourceType() {
	case "http":
		return s.httpHandler.Fetch(ctx, artifact)
	case "path":
		return s.pathHandler.Fetch(ctx, artifact)
	case "git":
		return s.gitHandler.Fetch(ctx, artifact)
	default:
		return nil, fmt.Errorf("unsupported source type: %s", artifact.GetSourceType())
	}
}

// AddArtifact uploads an artifact to the Sleuth server
func (s *SleuthRepository) AddArtifact(ctx context.Context, artifact *lockfile.Artifact, zipData []byte) error {
	endpoint := s.serverURL + "/api/skills/artifacts"

	// Create multipart writer
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add file part
	part, err := writer.CreateFormFile("file", fmt.Sprintf("%s-%s.zip", artifact.Name, artifact.Version))
	if err != nil {
		return fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(zipData); err != nil {
		return fmt.Errorf("failed to write zip data: %w", err)
	}

	// Add metadata fields
	_ = writer.WriteField("name", artifact.Name)
	_ = writer.WriteField("version", artifact.Version)
	_ = writer.WriteField("type", string(artifact.Type))

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
		return fmt.Errorf("failed to upload artifact: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse response
	var uploadResp struct {
		Success  bool `json:"success"`
		Artifact struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			URL     string `json:"url"`
		} `json:"artifact"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&uploadResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if !uploadResp.Success {
		return fmt.Errorf("upload failed: server returned success=false")
	}

	// Update artifact with source information if server returns URL
	if uploadResp.Artifact.URL != "" {
		artifact.SourceHTTP = &lockfile.SourceHTTP{
			URL: uploadResp.Artifact.URL,
		}
	}

	return nil
}

// GetVersionList retrieves available versions for an artifact
func (s *SleuthRepository) GetVersionList(ctx context.Context, name string) ([]string, error) {
	endpoint := fmt.Sprintf("%s/api/skills/artifacts/%s/versions", s.serverURL, name)

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

	var response struct {
		Versions []string `json:"versions"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return response.Versions, nil
}

// GetMetadata retrieves metadata for a specific artifact version
func (s *SleuthRepository) GetMetadata(ctx context.Context, name, version string) (*metadata.Metadata, error) {
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

// VerifyIntegrity checks hashes and sizes for downloaded artifacts
func (s *SleuthRepository) VerifyIntegrity(data []byte, hashes map[string]string, size int64) error {
	// Verify size if provided
	if size > 0 {
		if int64(len(data)) != size {
			return fmt.Errorf("size mismatch: expected %d bytes, got %d bytes", size, len(data))
		}
	}

	// Verify hashes (httpHandler already does this, but provide a standalone method)
	return s.httpHandler.verifyHashes(data, hashes)
}
