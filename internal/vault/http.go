package vault

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sleuth-io/sx/internal/buildinfo"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/utils"
)

// HTTPSourceHandler handles assets with source-http
type HTTPSourceHandler struct {
	client    *http.Client
	authToken string
}

// NewHTTPSourceHandler creates a new HTTP source handler
func NewHTTPSourceHandler(authToken string) *HTTPSourceHandler {
	return &HTTPSourceHandler{
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
		authToken: authToken,
	}
}

// Fetch downloads an asset from an HTTP URL
func (h *HTTPSourceHandler) Fetch(ctx context.Context, asset *lockfile.Asset) ([]byte, error) {
	if asset.SourceHTTP == nil {
		return nil, errors.New("asset does not have source-http")
	}

	source := asset.SourceHTTP

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add user agent
	req.Header.Set("User-Agent", buildinfo.GetUserAgent())

	// Add authorization header if available
	if h.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+h.authToken)
	}

	// Execute request
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download asset: %w", err)
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
		return nil, errors.New("downloaded file is not a valid zip archive")
	}

	return data, nil
}

// verifyHashes verifies the downloaded data against provided hashes
func (h *HTTPSourceHandler) verifyHashes(data []byte, hashes map[string]string) error {
	if len(hashes) == 0 {
		return errors.New("no hashes provided for verification")
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", buildinfo.GetUserAgent())

	// Add authorization header if available
	if h.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+h.authToken)
	}

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
