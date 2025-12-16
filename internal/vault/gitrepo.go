package vault

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/gofrs/flock"
	"github.com/sleuth-io/skills/internal/cache"
	"github.com/sleuth-io/skills/internal/constants"
	"github.com/sleuth-io/skills/internal/git"
	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/metadata"
	"github.com/sleuth-io/skills/internal/utils"
)

//go:embed templates/install.sh.tmpl
var installScriptTemplate string

//go:embed templates/README.md.tmpl
var readmeTemplate string

const (
	// installScriptTemplateVersion is the current version of the install.sh template
	// Increment this when making changes to the template
	installScriptTemplateVersion = "1"

	// readmeTemplateVersion is the current version of the README.md template
	// Increment this when making changes to the template
	readmeTemplateVersion = "1"
)

// GitVault implements Vault for Git vaults
type GitVault struct {
	repoURL     string
	repoPath    string
	gitClient   *git.Client
	httpHandler *HTTPSourceHandler
	pathHandler *PathSourceHandler
	gitHandler  *GitSourceHandler
}

// NewGitVault creates a new Git repository
func NewGitVault(repoURL string) (*GitVault, error) {
	// Get cache path for this repository
	repoPath, err := cache.GetGitRepoCachePath(repoURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get cache path: %w", err)
	}

	// Create git client
	gitClient := git.NewClient()

	return &GitVault{
		repoURL:     repoURL,
		repoPath:    repoPath,
		gitClient:   gitClient,
		httpHandler: NewHTTPSourceHandler(""),       // No auth token for git repos
		pathHandler: NewPathSourceHandler(repoPath), // Use repo path for relative paths
		gitHandler:  NewGitSourceHandler(gitClient),
	}, nil
}

// Authenticate performs authentication with the Git repository
// For Git repos, this is a no-op as authentication is handled by git itself
func (g *GitVault) Authenticate(ctx context.Context) (string, error) {
	// Git authentication is handled by the user's git configuration
	// (SSH keys, credential helpers, etc.)
	return "", nil
}

// acquireFileLock acquires a file lock for the git repository to prevent cross-process conflicts
func (g *GitVault) acquireFileLock(ctx context.Context) (*flock.Flock, error) {
	// Put lock file in cache directory, not in repo path
	// Use repo path hash to create unique lock filename
	cacheDir, err := cache.GetCacheDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get cache dir: %w", err)
	}

	lockFile := filepath.Join(cacheDir, "git-repos", filepath.Base(g.repoPath)+".lock")

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(lockFile), 0755); err != nil {
		return nil, fmt.Errorf("failed to create lock directory: %w", err)
	}

	fileLock := flock.New(lockFile)

	// Try to acquire the lock with a timeout
	locked, err := fileLock.TryLockContext(ctx, 100*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire file lock: %w", err)
	}
	if !locked {
		return nil, fmt.Errorf("could not acquire file lock (timeout)")
	}

	return fileLock, nil
}

// GetLockFile retrieves the lock file from the Git repository
func (g *GitVault) GetLockFile(ctx context.Context, cachedETag string) (content []byte, etag string, notModified bool, err error) {
	// Acquire file lock to prevent concurrent git operations (both in-process and cross-process)
	fileLock, err := g.acquireFileLock(ctx)
	if err != nil {
		return nil, "", false, fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() { _ = fileLock.Unlock() }()

	// Clone or update repository
	if err := g.cloneOrUpdate(ctx); err != nil {
		return nil, "", false, fmt.Errorf("failed to clone/update repository: %w", err)
	}

	// Read skill.lock from repository root
	lockFilePath := filepath.Join(g.repoPath, constants.SkillLockFile)
	if _, err := os.Stat(lockFilePath); os.IsNotExist(err) {
		return nil, "", false, fmt.Errorf("%s not found in repository", constants.SkillLockFile)
	}

	data, err := os.ReadFile(lockFilePath)
	if err != nil {
		return nil, "", false, fmt.Errorf("failed to read lock file: %w", err)
	}

	// For Git repos, we could use the commit SHA as ETag
	// But for simplicity, we'll just return the data without ETag caching
	return data, "", false, nil
}

// GetArtifact downloads an artifact using its source configuration
func (g *GitVault) GetArtifact(ctx context.Context, artifact *lockfile.Artifact) ([]byte, error) {
	// Lock only for path-based artifacts that read from the repository
	if artifact.GetSourceType() == "path" {
		fileLock, err := g.acquireFileLock(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to acquire lock: %w", err)
		}
		defer func() { _ = fileLock.Unlock() }()
	}

	// Dispatch to appropriate source handler based on artifact source type
	switch artifact.GetSourceType() {
	case "http":
		return g.httpHandler.Fetch(ctx, artifact)
	case "path":
		return g.pathHandler.Fetch(ctx, artifact)
	case "git":
		return g.gitHandler.Fetch(ctx, artifact)
	default:
		return nil, fmt.Errorf("unsupported source type: %s", artifact.GetSourceType())
	}
}

// AddArtifact uploads an artifact to the Git repository
func (g *GitVault) AddArtifact(ctx context.Context, artifact *lockfile.Artifact, zipData []byte) error {
	// Acquire file lock to prevent concurrent git operations
	fileLock, err := g.acquireFileLock(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() { _ = fileLock.Unlock() }()

	// Clone or update repository
	if err := g.cloneOrUpdate(ctx); err != nil {
		return fmt.Errorf("failed to clone/update repository: %w", err)
	}

	// Create artifacts directory structure: artifacts/{name}/{version}/
	artifactDir := filepath.Join(g.repoPath, "artifacts", artifact.Name, artifact.Version)
	if err := os.MkdirAll(artifactDir, 0755); err != nil {
		return fmt.Errorf("failed to create artifact directory: %w", err)
	}

	// For Git repositories, store artifacts exploded (not as zip)
	// This makes them easier to browse and diff in Git
	if err := extractZipToDir(zipData, artifactDir); err != nil {
		return fmt.Errorf("failed to extract zip to directory: %w", err)
	}

	// Update list.txt with this version
	listPath := filepath.Join(g.repoPath, "artifacts", artifact.Name, "list.txt")
	if err := g.updateVersionList(listPath, artifact.Version); err != nil {
		return fmt.Errorf("failed to update version list: %w", err)
	}

	// Commit and push the artifact to the repository
	if err := g.commitAndPush(ctx, artifact); err != nil {
		return fmt.Errorf("failed to commit and push artifact: %w", err)
	}

	// Note: Lock file is NOT updated here - it will be updated separately
	// with installation configurations by the caller

	return nil
}

// GetLockFilePath returns the path to the lock file in the git repository
func (g *GitVault) GetLockFilePath() string {
	return filepath.Join(g.repoPath, constants.SkillLockFile)
}

// CommitAndPush commits all changes and pushes to remote
func (g *GitVault) CommitAndPush(ctx context.Context, artifact *lockfile.Artifact) error {
	return g.commitAndPush(ctx, artifact)
}

// GetVersionList retrieves available versions for an artifact from list.txt
func (g *GitVault) GetVersionList(ctx context.Context, name string) ([]string, error) {
	// Clone or update repository
	if err := g.cloneOrUpdate(ctx); err != nil {
		return nil, fmt.Errorf("failed to clone/update repository: %w", err)
	}

	// Read list.txt for this artifact
	listPath := filepath.Join(g.repoPath, "artifacts", name, "list.txt")
	if _, err := os.Stat(listPath); os.IsNotExist(err) {
		// No versions exist for this artifact
		return []string{}, nil
	}

	data, err := os.ReadFile(listPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read version list: %w", err)
	}

	// Parse versions from file using common parser
	return parseVersionList(data), nil
}

// GetArtifactByVersion retrieves an artifact by name and version from the git repository
// This creates a zip from the exploded directory
func (g *GitVault) GetArtifactByVersion(ctx context.Context, name, version string) ([]byte, error) {
	// Clone or update repository
	if err := g.cloneOrUpdate(ctx); err != nil {
		return nil, fmt.Errorf("failed to clone/update repository: %w", err)
	}

	// Check if artifact directory exists
	artifactDir := filepath.Join(g.repoPath, "artifacts", name, version)
	if _, err := os.Stat(artifactDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("artifact %s@%s not found", name, version)
	}

	// Create zip from directory
	zipData, err := utils.CreateZip(artifactDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create zip from directory: %w", err)
	}

	return zipData, nil
}

// GetMetadata retrieves metadata for a specific artifact version
// Not applicable for Git repositories (metadata is inside the zip)
func (g *GitVault) GetMetadata(ctx context.Context, name, version string) (*metadata.Metadata, error) {
	return nil, fmt.Errorf("GetMetadata not supported for Git repositories")
}

// VerifyIntegrity checks hashes and sizes for downloaded artifacts
func (g *GitVault) VerifyIntegrity(data []byte, hashes map[string]string, size int64) error {
	// For Git repos, integrity is verified by Git's commit history
	// No additional verification needed
	return nil
}

// cloneOrUpdate clones the repository if it doesn't exist, or pulls updates if it does
func (g *GitVault) cloneOrUpdate(ctx context.Context) error {
	if _, err := os.Stat(filepath.Join(g.repoPath, ".git")); os.IsNotExist(err) {
		// Repository doesn't exist, clone it
		return g.clone(ctx)
	}

	// Repository exists, pull updates
	return g.pull(ctx)
}

// clone clones the Git repository
func (g *GitVault) clone(ctx context.Context) error {
	return g.gitClient.Clone(ctx, g.repoURL, g.repoPath)
}

// pull pulls updates from the remote repository
func (g *GitVault) pull(ctx context.Context) error {
	return g.gitClient.Pull(ctx, g.repoPath)
}

// UpdateTemplates updates templates in the repository if needed and returns the list of updated files
// The commit parameter controls whether to commit and push changes (git-specific behavior)
func (g *GitVault) UpdateTemplates(ctx context.Context, commit bool) ([]string, error) {
	return g.updateTemplates(ctx, commit)
}

// ensureInstallScript creates an install.sh script and README.md in the repository root if they don't exist
// or regenerates them if the template version has changed
func (g *GitVault) ensureInstallScript(ctx context.Context) error {
	_, err := g.updateTemplates(ctx, false)
	return err
}

// updateTemplates is the internal implementation that returns which files were updated
func (g *GitVault) updateTemplates(ctx context.Context, commit bool) ([]string, error) {
	var updatedFiles []string
	installScriptPath := filepath.Join(g.repoPath, "install.sh")
	readmePath := filepath.Join(g.repoPath, "README.md")

	// Use version constants directly (not from templates, which contain placeholders)
	installScriptVersion := installScriptTemplateVersion
	readmeVersion := readmeTemplateVersion

	// Check if install.sh needs to be created or updated
	needInstallScriptUpdate := false
	if content, err := os.ReadFile(installScriptPath); err == nil {
		// File exists, check if version needs update
		fileVersion := extractTemplateVersion(string(content), "# Template version: ")
		needInstallScriptUpdate = shouldUpdateTemplate(fileVersion, installScriptVersion)
	} else if os.IsNotExist(err) {
		// File doesn't exist
		needInstallScriptUpdate = true
	} else {
		return nil, fmt.Errorf("failed to check install.sh: %w", err)
	}

	// Check if README.md needs to be created or updated
	needReadmeUpdate := false
	if content, err := os.ReadFile(readmePath); err == nil {
		// File exists, check if version needs update
		fileVersion := extractTemplateVersion(string(content), "<!-- Template version: ")
		needReadmeUpdate = shouldUpdateTemplate(fileVersion, readmeVersion)
	} else if os.IsNotExist(err) {
		// File doesn't exist
		needReadmeUpdate = true
	} else {
		return nil, fmt.Errorf("failed to check README.md: %w", err)
	}

	// If both files are up to date, nothing to do
	if !needInstallScriptUpdate && !needReadmeUpdate {
		return updatedFiles, nil
	}

	// Create or update install.sh if needed
	if needInstallScriptUpdate {
		// Generate install.sh with actual repository URL
		installScript := generateInstallScript(g.repoURL)
		if err := os.WriteFile(installScriptPath, []byte(installScript), 0755); err != nil {
			return nil, fmt.Errorf("failed to create install.sh: %w", err)
		}
		updatedFiles = append(updatedFiles, "install.sh")
	}

	// Create or update README.md if needed
	if needReadmeUpdate {
		// Generate README with actual repository URL
		readme := generateReadme(g.repoURL)
		if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
			return nil, fmt.Errorf("failed to create README.md: %w", err)
		}
		updatedFiles = append(updatedFiles, "README.md")
	}

	// Commit and push the changes if requested and any files were updated
	if commit && len(updatedFiles) > 0 {
		// Stage the updated files
		if err := g.gitClient.Add(ctx, g.repoPath, updatedFiles...); err != nil {
			return nil, fmt.Errorf("failed to stage updated templates: %w", err)
		}

		// Commit with a descriptive message
		commitMsg := fmt.Sprintf("Update templates to version %s/%s", installScriptTemplateVersion, readmeTemplateVersion)
		if err := g.gitClient.Commit(ctx, g.repoPath, commitMsg); err != nil {
			return nil, fmt.Errorf("failed to commit updated templates: %w", err)
		}

		// Push to remote
		if err := g.gitClient.Push(ctx, g.repoPath); err != nil {
			return nil, fmt.Errorf("failed to push updated templates: %w", err)
		}
	}

	return updatedFiles, nil
}

// extractTemplateVersion extracts the version number from a template or file content
// Returns empty string if no version found
func extractTemplateVersion(content, prefix string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			// Extract version after the prefix
			version := strings.TrimPrefix(line, prefix)
			// Remove trailing comment markers
			version = strings.TrimSuffix(version, "-->")
			return strings.TrimSpace(version)
		}
	}
	return ""
}

// shouldUpdateTemplate determines if a template file needs to be updated
// Returns true if the file should be updated (fileVersion < templateVersion or fileVersion missing)
// Returns false if fileVersion >= templateVersion (prevents downgrades)
// Panics if templateVersion is missing (programming error)
func shouldUpdateTemplate(fileVersion, templateVersion string) bool {
	// Template version must always exist - if not, it's a programming error
	if templateVersion == "" {
		panic("template version is missing - this should never happen")
	}

	// Parse template version as integer
	templateVer, err := strconv.Atoi(templateVersion)
	if err != nil {
		panic(fmt.Sprintf("template version is invalid: %s", templateVersion))
	}

	// If no version in file (empty string), treat as version 0 and update
	if fileVersion == "" {
		return true
	}

	// Parse file version as integer
	fileVer, err := strconv.Atoi(fileVersion)
	if err != nil {
		// If file version is invalid, treat as 0 and update
		return true
	}

	// Only update if template version is newer
	return fileVer < templateVer
}

// generateInstallScript creates an install.sh with the actual repository URL
func generateInstallScript(repoURL string) string {
	tmpl, err := template.New("install.sh").Parse(installScriptTemplate)
	if err != nil {
		// This should never happen with embedded templates
		panic(fmt.Sprintf("failed to parse install.sh template: %v", err))
	}

	var buf bytes.Buffer
	data := map[string]string{
		"REPO_URL":         repoURL,
		"TEMPLATE_VERSION": installScriptTemplateVersion,
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		panic(fmt.Sprintf("failed to execute install.sh template: %v", err))
	}

	return buf.String()
}

// generateReadme creates a README with the actual repository URL
func generateReadme(repoURL string) string {
	// Convert git URL to raw GitHub URL for install.sh
	// e.g., https://github.com/org/repo.git -> https://raw.githubusercontent.com/org/repo/main/install.sh
	rawURL := convertToRawURL(repoURL)

	tmpl, err := template.New("README.md").Parse(readmeTemplate)
	if err != nil {
		panic(fmt.Sprintf("failed to parse README.md template: %v", err))
	}

	var buf bytes.Buffer
	data := map[string]string{
		"INSTALL_URL":      rawURL,
		"TEMPLATE_VERSION": readmeTemplateVersion,
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		panic(fmt.Sprintf("failed to execute README.md template: %v", err))
	}

	return buf.String()
}

// convertToRawURL converts a git repository URL to a raw content URL
func convertToRawURL(repoURL string) string {
	// Remove .git suffix if present
	repoURL = strings.TrimSuffix(repoURL, ".git")

	// Handle GitHub URLs
	if strings.Contains(repoURL, "github.com") {
		// Convert SSH URL to HTTPS
		if strings.HasPrefix(repoURL, "git@github.com:") {
			repoURL = strings.Replace(repoURL, "git@github.com:", "https://github.com/", 1)
		}

		// Convert to raw.githubusercontent.com URL
		repoURL = strings.Replace(repoURL, "https://github.com/", "https://raw.githubusercontent.com/", 1)
		return repoURL + "/main/install.sh"
	}

	// For other git hosting services, use a generic placeholder
	return "https://raw.githubusercontent.com/YOUR_ORG/YOUR_REPO/main/install.sh"
}

// commitAndPush commits and pushes changes
func (g *GitVault) commitAndPush(ctx context.Context, artifact *lockfile.Artifact) error {
	// Ensure install.sh and README.md exist before committing
	if err := g.ensureInstallScript(ctx); err != nil {
		// Log warning but continue - these files are convenience features
		fmt.Fprintf(os.Stderr, "Warning: could not create repository files: %v\n", err)
	}

	// Add all changes
	if err := g.gitClient.Add(ctx, g.repoPath, "."); err != nil {
		return err
	}

	// Commit with message
	commitMsg := fmt.Sprintf("Add %s %s", artifact.Name, artifact.Version)
	if err := g.gitClient.Commit(ctx, g.repoPath, commitMsg); err != nil {
		return err
	}

	// Push
	if err := g.gitClient.Push(ctx, g.repoPath); err != nil {
		return err
	}

	return nil
}

// extractZipToDir extracts a zip file to a directory
func extractZipToDir(zipData []byte, targetDir string) error {
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return fmt.Errorf("failed to read zip: %w", err)
	}

	for _, file := range reader.File {
		// Build target path
		targetPath := filepath.Join(targetDir, file.Name)

		// Prevent zip slip vulnerability
		cleanTarget := filepath.Clean(targetPath)
		cleanDir := filepath.Clean(targetDir)
		relPath, err := filepath.Rel(cleanDir, cleanTarget)
		if err != nil || strings.HasPrefix(relPath, "..") {
			return fmt.Errorf("illegal file path: %s", file.Name)
		}

		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, file.Mode()); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", file.Name, err)
			}
			continue
		}

		// Ensure parent directory exists with proper permissions
		parentDir := filepath.Dir(targetPath)
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			return fmt.Errorf("failed to create parent directory for %s: %w", file.Name, err)
		}
		// Fix permissions on parent directory if it already existed
		if err := os.Chmod(parentDir, 0755); err != nil {
			return fmt.Errorf("failed to set permissions on parent directory for %s: %w", file.Name, err)
		}

		// Extract file
		rc, err := file.Open()
		if err != nil {
			return fmt.Errorf("failed to open file %s in zip: %w", file.Name, err)
		}

		// Use 0644 for files instead of preserving zip permissions
		// Zip files may have restrictive permissions that cause issues
		fileMode := os.FileMode(0644)
		if file.Mode()&0111 != 0 {
			// If executable bit is set, use 0755
			fileMode = 0755
		}

		outFile, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileMode)
		if err != nil {
			rc.Close()
			return fmt.Errorf("failed to create file %s: %w", file.Name, err)
		}

		_, err = io.Copy(outFile, rc)
		rc.Close()
		outFile.Close()

		if err != nil {
			return fmt.Errorf("failed to write file %s: %w", file.Name, err)
		}
	}

	return nil
}

// updateVersionList updates the list.txt file with a new version
func (g *GitVault) updateVersionList(listPath, newVersion string) error {
	var versions []string

	// Read existing versions if file exists
	if data, err := os.ReadFile(listPath); err == nil {
		for _, line := range bytes.Split(data, []byte("\n")) {
			version := string(bytes.TrimSpace(line))
			if version != "" {
				versions = append(versions, version)
			}
		}
	}

	// Check if version already exists
	for _, v := range versions {
		if v == newVersion {
			return nil // Version already in list
		}
	}

	// Add new version
	versions = append(versions, newVersion)

	// Write back to file
	var buf bytes.Buffer
	for _, v := range versions {
		buf.WriteString(v)
		buf.WriteByte('\n')
	}

	return os.WriteFile(listPath, buf.Bytes(), 0644)
}

// PostUsageStats is a no-op for Git repositories
// Git repositories don't support stats collection
func (r *GitVault) PostUsageStats(ctx context.Context, jsonlData string) error {
	return nil
}
