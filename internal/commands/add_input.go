package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sleuth-io/sx/internal/buildinfo"
	"github.com/sleuth-io/sx/internal/github"
	"github.com/sleuth-io/sx/internal/ui/components"
	"github.com/sleuth-io/sx/internal/utils"
)

// isURL checks if the input looks like a URL
func isURL(input string) bool {
	return strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://")
}

// loadZipFile prompts for, loads, and validates the zip file, directory, or URL
func loadZipFile(out *outputHelper, status *components.Status, zipFile string) (string, []byte, error) {
	// Prompt for zip file, directory, or URL if not provided
	if zipFile == "" {
		var err error
		zipFile, err = out.prompt("Enter path or URL to asset zip file or directory: ")
		if err != nil {
			return "", nil, fmt.Errorf("failed to read input: %w", err)
		}
	}

	if zipFile == "" {
		return "", nil, errors.New("zip file, directory path, or URL is required")
	}

	// Check if it's a GitHub tree URL (directory)
	if github.IsTreeURL(zipFile) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		zipData, err := downloadFromGitHub(ctx, status, zipFile)
		if err != nil {
			return "", nil, err
		}
		return zipFile, zipData, nil
	}

	// Check if it's a regular URL (zip file)
	if isURL(zipFile) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		zipData, err := downloadZipFromURL(ctx, status, zipFile)
		if err != nil {
			return "", nil, err
		}
		return zipFile, zipData, nil
	}

	// Expand path
	zipFile, err := utils.NormalizePath(zipFile)
	if err != nil {
		return "", nil, fmt.Errorf("invalid path: %w", err)
	}

	// Check if file or directory exists
	if !utils.FileExists(zipFile) {
		return "", nil, fmt.Errorf("file or directory not found: %s", zipFile)
	}

	// Read zip file or create zip from directory
	var zipData []byte

	if utils.IsDirectory(zipFile) {
		status.Start("Creating zip from directory")
		zipData, err = utils.CreateZip(zipFile)
		if err != nil {
			status.Fail("Failed to create zip")
			return "", nil, fmt.Errorf("failed to create zip from directory: %w", err)
		}
		status.Done("")
	} else if isSingleFileAsset(zipFile) {
		// Handle single .md files for agents and commands
		status.Start("Creating zip from single file")
		zipData, err = createZipFromSingleFile(zipFile)
		if err != nil {
			status.Fail("Failed to create zip")
			return "", nil, fmt.Errorf("failed to create zip from file: %w", err)
		}
		status.Done("")
	} else {
		status.Start("Reading asset")
		zipData, err = os.ReadFile(zipFile)
		if err != nil {
			status.Fail("Failed to read file")
			return "", nil, fmt.Errorf("failed to read zip file: %w", err)
		}

		// Verify it's a valid zip
		if !utils.IsZipFile(zipData) {
			status.Fail("Invalid zip file")
			return "", nil, errors.New("file is not a valid zip archive")
		}
		status.Done("")
	}

	return zipFile, zipData, nil
}

// downloadZipFromURL downloads a zip file from a URL
func downloadZipFromURL(ctx context.Context, status *components.Status, zipURL string) ([]byte, error) {
	status.Start("Downloading asset from URL")

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 5 * time.Minute,
	}

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, zipURL, nil)
	if err != nil {
		status.Fail("Failed to create request")
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set user agent
	req.Header.Set("User-Agent", buildinfo.GetUserAgent())

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		status.Fail("Failed to download")
		return nil, fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		status.Fail(fmt.Sprintf("HTTP %d", resp.StatusCode))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Read response body
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		status.Fail("Failed to read response")
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Verify it's a valid zip
	if !utils.IsZipFile(data) {
		status.Fail("Invalid zip archive")
		return nil, errors.New("downloaded file is not a valid zip archive")
	}

	status.Done("")
	return data, nil
}

// downloadFromGitHub downloads files from a GitHub directory URL and returns them as a zip.
func downloadFromGitHub(ctx context.Context, status *components.Status, gitHubURL string) ([]byte, error) {
	treeURL := github.ParseTreeURL(gitHubURL)
	if treeURL == nil {
		return nil, fmt.Errorf("invalid GitHub directory URL: %s", gitHubURL)
	}

	statusMsg := fmt.Sprintf("Downloading from %s/%s", treeURL.Owner, treeURL.Repo)
	if treeURL.Path != "" {
		statusMsg = fmt.Sprintf("Downloading from %s/%s/%s", treeURL.Owner, treeURL.Repo, treeURL.Path)
	}
	status.Start(statusMsg)

	fetcher := github.NewFetcher()
	zipData, err := fetcher.FetchDirectory(ctx, treeURL)
	if err != nil {
		status.Fail("Failed to download")
		return nil, fmt.Errorf("failed to download from GitHub: %w", err)
	}

	status.Done("")
	return zipData, nil
}
