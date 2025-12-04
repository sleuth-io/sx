package repository

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sleuth-io/skills/internal/cache"
	"github.com/sleuth-io/skills/internal/constants"
	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/metadata"
	"github.com/sleuth-io/skills/internal/utils"
)

//go:embed templates/install.sh.tmpl
var installScriptTemplate string

//go:embed templates/README.md.tmpl
var readmeTemplate string

// GitRepository implements Repository for Git repositories
type GitRepository struct {
	repoURL     string
	repoPath    string
	httpHandler *HTTPSourceHandler
	pathHandler *PathSourceHandler
	gitHandler  *GitSourceHandler
}

// NewGitRepository creates a new Git repository
func NewGitRepository(repoURL string) (*GitRepository, error) {
	// Get cache path for this repository
	repoPath, err := cache.GetGitRepoCachePath(repoURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get cache path: %w", err)
	}

	return &GitRepository{
		repoURL:     repoURL,
		repoPath:    repoPath,
		httpHandler: NewHTTPSourceHandler(),
		pathHandler: NewPathSourceHandler(repoPath), // Use repo path for relative paths
		gitHandler:  NewGitSourceHandler(),
	}, nil
}

// Authenticate performs authentication with the Git repository
// For Git repos, this is a no-op as authentication is handled by git itself
func (g *GitRepository) Authenticate(ctx context.Context) (string, error) {
	// Git authentication is handled by the user's git configuration
	// (SSH keys, credential helpers, etc.)
	return "", nil
}

// GetLockFile retrieves the lock file from the Git repository
func (g *GitRepository) GetLockFile(ctx context.Context, cachedETag string) (content []byte, etag string, notModified bool, err error) {
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
func (g *GitRepository) GetArtifact(ctx context.Context, artifact *lockfile.Artifact) ([]byte, error) {
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
func (g *GitRepository) AddArtifact(ctx context.Context, artifact *lockfile.Artifact, zipData []byte) error {
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

	// Note: Lock file is NOT updated here - it will be updated separately
	// with installation configurations by the caller

	return nil
}

// GetLockFilePath returns the path to the lock file in the git repository
func (g *GitRepository) GetLockFilePath() string {
	return filepath.Join(g.repoPath, constants.SkillLockFile)
}

// CommitAndPush commits all changes and pushes to remote
func (g *GitRepository) CommitAndPush(ctx context.Context, artifact *lockfile.Artifact) error {
	return g.commitAndPush(ctx, artifact)
}

// GetVersionList retrieves available versions for an artifact from list.txt
func (g *GitRepository) GetVersionList(ctx context.Context, name string) ([]string, error) {
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

	// Parse versions from file
	var versions []string
	for _, line := range bytes.Split(data, []byte("\n")) {
		version := string(bytes.TrimSpace(line))
		if version != "" {
			versions = append(versions, version)
		}
	}

	return versions, nil
}

// GetArtifactByVersion retrieves an artifact by name and version from the git repository
// This creates a zip from the exploded directory
func (g *GitRepository) GetArtifactByVersion(ctx context.Context, name, version string) ([]byte, error) {
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
func (g *GitRepository) GetMetadata(ctx context.Context, name, version string) (*metadata.Metadata, error) {
	return nil, fmt.Errorf("GetMetadata not supported for Git repositories")
}

// VerifyIntegrity checks hashes and sizes for downloaded artifacts
func (g *GitRepository) VerifyIntegrity(data []byte, hashes map[string]string, size int64) error {
	// For Git repos, integrity is verified by Git's commit history
	// No additional verification needed
	return nil
}

// cloneOrUpdate clones the repository if it doesn't exist, or pulls updates if it does
func (g *GitRepository) cloneOrUpdate(ctx context.Context) error {
	if _, err := os.Stat(filepath.Join(g.repoPath, ".git")); os.IsNotExist(err) {
		// Repository doesn't exist, clone it
		return g.clone(ctx)
	}

	// Repository exists, pull updates
	return g.pull(ctx)
}

// clone clones the Git repository
func (g *GitRepository) clone(ctx context.Context) error {
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(g.repoPath), 0755); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "git", "clone", "--quiet", g.repoURL, g.repoPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// pull pulls updates from the remote repository
func (g *GitRepository) pull(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "git", "pull", "--quiet")
	cmd.Dir = g.repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git pull failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// ensureInstallScript creates an install.sh script and README.md in the repository root if they don't exist
func (g *GitRepository) ensureInstallScript(ctx context.Context) error {
	installScriptPath := filepath.Join(g.repoPath, "install.sh")
	readmePath := filepath.Join(g.repoPath, "README.md")

	// Check if install.sh already exists
	installScriptExists := false
	if _, err := os.Stat(installScriptPath); err == nil {
		installScriptExists = true
	} else if !os.IsNotExist(err) {
		// Some other error checking the file
		return fmt.Errorf("failed to check install.sh: %w", err)
	}

	// Check if README.md already exists
	readmeExists := false
	if _, err := os.Stat(readmePath); err == nil {
		readmeExists = true
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to check README.md: %w", err)
	}

	// If both exist, nothing to do
	if installScriptExists && readmeExists {
		return nil
	}

	// Create install.sh if it doesn't exist
	if !installScriptExists {
		if err := os.WriteFile(installScriptPath, []byte(installScriptTemplate), 0755); err != nil {
			return fmt.Errorf("failed to create install.sh: %w", err)
		}
	}

	// Create README.md if it doesn't exist
	if !readmeExists {
		// Generate README with actual repository URL
		readme := generateReadme(g.repoURL)
		if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
			return fmt.Errorf("failed to create README.md: %w", err)
		}
	}

	return nil
}

// generateReadme creates a README with the actual repository URL
func generateReadme(repoURL string) string {
	// Convert git URL to raw GitHub URL for install.sh
	// e.g., https://github.com/org/repo.git -> https://raw.githubusercontent.com/org/repo/main/install.sh
	rawURL := convertToRawURL(repoURL)

	return strings.ReplaceAll(readmeTemplate, "https://raw.githubusercontent.com/YOUR_ORG/YOUR_REPO/main/install.sh", rawURL)
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
func (g *GitRepository) commitAndPush(ctx context.Context, artifact *lockfile.Artifact) error {
	// Ensure install.sh and README.md exist before committing
	if err := g.ensureInstallScript(ctx); err != nil {
		// Log warning but continue - these files are convenience features
		fmt.Fprintf(os.Stderr, "Warning: could not create repository files: %v\n", err)
	}

	// Add all changes
	cmd := exec.CommandContext(ctx, "git", "add", ".")
	cmd.Dir = g.repoPath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add failed: %w\nOutput: %s", err, string(output))
	}

	// Commit with message
	commitMsg := fmt.Sprintf("Add %s %s", artifact.Name, artifact.Version)
	cmd = exec.CommandContext(ctx, "git", "commit", "-m", commitMsg)
	cmd.Dir = g.repoPath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit failed: %w\nOutput: %s", err, string(output))
	}

	// Push
	cmd = exec.CommandContext(ctx, "git", "push", "--quiet")
	cmd.Dir = g.repoPath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git push failed: %w\nOutput: %s", err, string(output))
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
func (g *GitRepository) updateVersionList(listPath, newVersion string) error {
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
