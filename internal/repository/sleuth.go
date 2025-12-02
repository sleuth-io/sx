package repository

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	sleuthConfig "github.com/sleuth-io/skills/internal/config"
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
	return &SleuthRepository{
		serverURL:   serverURL,
		authToken:   authToken,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		httpHandler: NewHTTPSourceHandler(),
		pathHandler: NewPathSourceHandler(""), // Lock file dir not applicable for Sleuth
		gitHandler:  NewGitSourceHandler(),
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
	req.Header.Set("User-Agent", "skills-cli/0.1.0")
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
	// TODO: Implement artifact upload to Sleuth server
	// This will involve:
	// 1. POST to /api/skills/artifacts with multipart form data
	// 2. Server will extract metadata, validate, and add to lock file
	// 3. Return updated lock file or artifact URL
	return fmt.Errorf("AddArtifact not yet implemented for Sleuth repository")
}

// GetVersionList retrieves available versions for an artifact
func (s *SleuthRepository) GetVersionList(ctx context.Context, name string) ([]string, error) {
	endpoint := fmt.Sprintf("%s/api/skills/artifacts/%s/versions", s.serverURL, name)

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "skills-cli/0.1.0")
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

	// TODO: Parse response and return version list
	return nil, fmt.Errorf("GetVersionList not yet fully implemented")
}

// GetMetadata retrieves metadata for a specific artifact version
func (s *SleuthRepository) GetMetadata(ctx context.Context, name, version string) (*metadata.Metadata, error) {
	endpoint := fmt.Sprintf("%s/api/skills/artifacts/%s/%s/metadata.toml", s.serverURL, name, version)

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "skills-cli/0.1.0")
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
