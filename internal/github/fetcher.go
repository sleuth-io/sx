package github

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"time"

	"github.com/sleuth-io/sx/internal/buildinfo"
)

// Fetcher handles downloading files and directories from GitHub.
type Fetcher struct {
	client    *http.Client
	userAgent string
	token     string // Optional GitHub token for higher rate limits
}

// NewFetcher creates a new GitHub fetcher.
func NewFetcher() *Fetcher {
	return &Fetcher{
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
		userAgent: buildinfo.GetUserAgent(),
	}
}

// WithToken sets an optional GitHub token for authenticated requests.
// This increases rate limits from 60/hour to 5000/hour.
func (f *Fetcher) WithToken(token string) *Fetcher {
	f.token = token
	return f
}

// contentsResponse represents a single item from the GitHub Contents API.
type contentsResponse struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"` // "file" or "dir"
	Size        int64  `json:"size"`
	DownloadURL string `json:"download_url"`
}

// FetchDirectory downloads all files from a GitHub directory and returns them as a zip.
func (f *Fetcher) FetchDirectory(ctx context.Context, treeURL *TreeURL) ([]byte, error) {
	// Collect all files recursively
	files, err := f.listFilesRecursive(ctx, treeURL, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}

	if len(files) == 0 {
		return nil, errors.New("no files found in directory")
	}

	// Download all files and create zip
	return f.downloadAndZip(ctx, treeURL, files)
}

// fileEntry represents a file to download.
type fileEntry struct {
	Name        string // Relative path within the skill directory
	DownloadURL string
	Size        int64
}

// listFilesRecursive lists all files in a directory and its subdirectories.
func (f *Fetcher) listFilesRecursive(ctx context.Context, treeURL *TreeURL, subpath string) ([]fileEntry, error) {
	// Build the API URL for the current directory
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents", treeURL.Owner, treeURL.Repo)
	currentPath := treeURL.Path
	if subpath != "" {
		currentPath = path.Join(currentPath, subpath)
	}
	if currentPath != "" {
		apiURL += "/" + currentPath
	}
	apiURL += "?ref=" + treeURL.Ref

	// Make API request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	f.setHeaders(req)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch directory listing: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("directory not found: %s", currentPath)
	}
	if resp.StatusCode == http.StatusForbidden {
		return nil, errors.New("rate limited or access denied (consider using a GitHub token)")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var contents []contentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&contents); err != nil {
		return nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	// Collect files
	var files []fileEntry
	for _, item := range contents {
		relativePath := item.Name
		if subpath != "" {
			relativePath = path.Join(subpath, item.Name)
		}

		switch item.Type {
		case "file":
			files = append(files, fileEntry{
				Name:        relativePath,
				DownloadURL: item.DownloadURL,
				Size:        item.Size,
			})
		case "dir":
			// Recursively list subdirectory
			subFiles, err := f.listFilesRecursive(ctx, treeURL, relativePath)
			if err != nil {
				return nil, fmt.Errorf("failed to list subdirectory %s: %w", relativePath, err)
			}
			files = append(files, subFiles...)
		}
	}

	return files, nil
}

// downloadAndZip downloads all files and creates a zip archive.
func (f *Fetcher) downloadAndZip(ctx context.Context, treeURL *TreeURL, files []fileEntry) ([]byte, error) {
	buf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(buf)

	for _, file := range files {
		// Download file content
		content, err := f.downloadFile(ctx, file.DownloadURL)
		if err != nil {
			return nil, fmt.Errorf("failed to download %s: %w", file.Name, err)
		}

		// Add to zip
		w, err := zipWriter.Create(file.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to create zip entry for %s: %w", file.Name, err)
		}

		if _, err := w.Write(content); err != nil {
			return nil, fmt.Errorf("failed to write %s to zip: %w", file.Name, err)
		}
	}

	if err := zipWriter.Close(); err != nil {
		return nil, fmt.Errorf("failed to close zip: %w", err)
	}

	return buf.Bytes(), nil
}

// downloadFile downloads a single file from a URL.
func (f *Fetcher) downloadFile(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	f.setHeaders(req)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	return io.ReadAll(resp.Body)
}

// setHeaders sets common headers for GitHub requests.
func (f *Fetcher) setHeaders(req *http.Request) {
	req.Header.Set("User-Agent", f.userAgent)
	if f.token != "" {
		req.Header.Set("Authorization", "Bearer "+f.token)
	}
}
