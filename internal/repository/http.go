package repository

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/utils"
)

// HTTPSourceHandler handles artifacts with source-http
type HTTPSourceHandler struct {
	client *http.Client
}

// NewHTTPSourceHandler creates a new HTTP source handler
func NewHTTPSourceHandler() *HTTPSourceHandler {
	return &HTTPSourceHandler{
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// Fetch downloads an artifact from an HTTP URL
func (h *HTTPSourceHandler) Fetch(ctx context.Context, artifact *lockfile.Artifact) ([]byte, error) {
	if artifact.SourceHTTP == nil {
		return nil, fmt.Errorf("artifact does not have source-http")
	}

	source := artifact.SourceHTTP

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, "GET", source.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add user agent
	req.Header.Set("User-Agent", "skills-cli/0.1.0")

	// Execute request
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download artifact: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Read response body
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Verify size if provided
	if source.Size > 0 {
		if int64(len(data)) != source.Size {
			return nil, fmt.Errorf("size mismatch: expected %d bytes, got %d bytes", source.Size, len(data))
		}
	}

	// Verify hashes
	if err := h.verifyHashes(data, source.Hashes); err != nil {
		return nil, fmt.Errorf("hash verification failed: %w", err)
	}

	// Verify it's a valid zip file
	if !utils.IsZipFile(data) {
		return nil, fmt.Errorf("downloaded file is not a valid zip archive")
	}

	return data, nil
}

// verifyHashes verifies the downloaded data against provided hashes
func (h *HTTPSourceHandler) verifyHashes(data []byte, hashes map[string]string) error {
	if len(hashes) == 0 {
		return fmt.Errorf("no hashes provided for verification")
	}

	for algo, expected := range hashes {
		if err := utils.VerifyHash(data, algo, expected); err != nil {
			return err
		}
	}

	return nil
}

// DownloadWithProgress downloads a file with progress reporting
// This is used for user-facing downloads with progress bars
func (h *HTTPSourceHandler) DownloadWithProgress(ctx context.Context, url string, progressCallback func(current, total int64)) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "skills-cli/0.1.0")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	total := resp.ContentLength

	// Read with progress reporting
	var data []byte
	buffer := make([]byte, 32*1024) // 32KB chunks
	current := int64(0)

	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			data = append(data, buffer[:n]...)
			current += int64(n)
			if progressCallback != nil {
				progressCallback(current, total)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}
	}

	return data, nil
}
