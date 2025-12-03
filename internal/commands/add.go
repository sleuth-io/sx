package commands

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/skills/internal/config"
	"github.com/sleuth-io/skills/internal/constants"
	"github.com/sleuth-io/skills/internal/handlers"
	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/metadata"
	"github.com/sleuth-io/skills/internal/repository"
	"github.com/sleuth-io/skills/internal/utils"
)

// NewAddCommand creates the add command
func NewAddCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add [zip-file]",
		Short: "Add a local zip file artifact to the repository",
		Long: `Take a local zip file, detect metadata from its contents, prompt for
confirmation/edits, install it to the repository, and update the lock file.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var zipFile string
			if len(args) > 0 {
				zipFile = args[0]
			}
			return runAdd(cmd, zipFile)
		},
	}

	return cmd
}

// runAdd executes the add command
func runAdd(cmd *cobra.Command, zipFile string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	out := newOutputHelper(cmd)

	// Get and validate zip file
	zipFile, zipData, err := loadZipFile(out, zipFile)
	if err != nil {
		return err
	}

	// Detect artifact name and type
	name, artifactType, metadataExists, err := detectArtifactInfo(out, zipFile, zipData)
	if err != nil {
		return err
	}

	// Create repository instance
	repo, err := createRepository()
	if err != nil {
		return err
	}

	// Check versions and content
	version, contentsIdentical, err := checkVersionAndContents(ctx, out, repo, name, zipData)
	if err != nil {
		return err
	}

	// Handle identical content case
	if contentsIdentical {
		return handleIdenticalArtifact(ctx, out, repo, name, version, artifactType)
	}

	// Add new or updated artifact
	return addNewArtifact(ctx, out, repo, name, artifactType, version, zipFile, zipData, metadataExists)
}

// loadZipFile prompts for, loads, and validates the zip file
func loadZipFile(out *outputHelper, zipFile string) (string, []byte, error) {
	// Prompt for zip file if not provided
	if zipFile == "" {
		var err error
		zipFile, err = out.prompt("Enter path to artifact zip file: ")
		if err != nil {
			return "", nil, fmt.Errorf("failed to read input: %w", err)
		}
	}

	if zipFile == "" {
		return "", nil, fmt.Errorf("zip file path is required")
	}

	// Expand path
	zipFile, err := utils.NormalizePath(zipFile)
	if err != nil {
		return "", nil, fmt.Errorf("invalid path: %w", err)
	}

	// Check if file exists
	if !utils.FileExists(zipFile) {
		return "", nil, fmt.Errorf("file not found: %s", zipFile)
	}

	// Read zip file
	out.println()
	out.println("Reading artifact...")
	zipData, err := os.ReadFile(zipFile)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read zip file: %w", err)
	}

	// Verify it's a valid zip
	if !utils.IsZipFile(zipData) {
		return "", nil, fmt.Errorf("file is not a valid zip archive")
	}

	return zipFile, zipData, nil
}

// detectArtifactInfo extracts or detects artifact name and type, then confirms with user
func detectArtifactInfo(out *outputHelper, zipFile string, zipData []byte) (name, artifactType string, metadataExists bool, err error) {
	// Extract or detect name and type
	name, artifactType, metadataExists, err = extractOrDetectNameAndType(out, zipFile, zipData)
	if err != nil {
		return "", "", false, err
	}

	// Confirm name and type with user
	name, artifactType, err = confirmNameAndType(out, name, artifactType)
	if err != nil {
		return "", "", false, err
	}

	return name, artifactType, metadataExists, nil
}

// createRepository loads config and creates a repository instance
func createRepository() (repository.Repository, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w\nRun 'skills init' to configure", err)
	}

	var repo repository.Repository
	switch cfg.Type {
	case config.RepositoryTypeSleuth:
		repo = repository.NewSleuthRepository(cfg.GetServerURL(), cfg.AuthToken)
	case config.RepositoryTypeGit:
		repo, err = repository.NewGitRepository(cfg.RepositoryURL)
		if err != nil {
			return nil, fmt.Errorf("failed to create git repository: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported repository type: %s", cfg.Type)
	}

	return repo, nil
}

// checkVersionAndContents queries repository for versions and checks if content is identical
func checkVersionAndContents(ctx context.Context, out *outputHelper, repo repository.Repository, name string, zipData []byte) (version string, identical bool, err error) {
	out.println()
	out.println("Checking for existing versions...")
	versions, err := repo.GetVersionList(ctx, name)
	if err != nil {
		return "", false, fmt.Errorf("failed to get version list: %w", err)
	}

	version, identical, err = determineSuggestedVersionAndCheckIdentical(ctx, out, repo, name, versions, zipData)
	if err != nil {
		return "", false, err
	}

	return version, identical, nil
}

// handleIdenticalArtifact handles the case when content is identical to existing version
func handleIdenticalArtifact(ctx context.Context, out *outputHelper, repo repository.Repository, name, version, artifactType string) error {
	out.println()
	out.printf("✓ Artifact %s@%s already exists in repository with identical contents\n", name, version)

	// Prompt for repository configurations
	repositories, err := promptForRepositories(out, name, version)
	if err != nil {
		return fmt.Errorf("failed to configure repositories: %w", err)
	}

	// If nil, user chose not to install (lock file already handled in prompt)
	if repositories == nil {
		return nil
	}

	// Update the lock file with artifact
	artifact := &lockfile.Artifact{
		Name:    name,
		Version: version,
		Type:    lockfile.ArtifactType(artifactType),
		SourcePath: &lockfile.SourcePath{
			Path: fmt.Sprintf("./artifacts/%s/%s", name, version),
		},
		Repositories: repositories,
	}

	if err := updateLockFile(ctx, out, repo, artifact); err != nil {
		return fmt.Errorf("failed to update lock file: %w", err)
	}

	return nil
}

// addNewArtifact adds a new or updated artifact to the repository
func addNewArtifact(ctx context.Context, out *outputHelper, repo repository.Repository, name, artifactType, version, zipFile string, zipData []byte, metadataExists bool) error {
	// Prompt user for version
	version, err := promptForVersion(out, version)
	if err != nil {
		return err
	}

	// Create full metadata with confirmed version
	meta := createMetadata(name, version, artifactType, zipFile, zipData)

	// Add metadata to zip if needed
	if !metadataExists {
		zipData, err = addMetadataToZip(meta, zipData)
		if err != nil {
			return err
		}
	}

	// Create artifact entry (what it is)
	artifact := &lockfile.Artifact{
		Name:    meta.Artifact.Name,
		Version: meta.Artifact.Version,
		Type:    lockfile.ArtifactType(meta.Artifact.Type),
		SourcePath: &lockfile.SourcePath{
			Path: fmt.Sprintf("./artifacts/%s/%s", meta.Artifact.Name, meta.Artifact.Version),
		},
	}

	// Upload artifact files to repository
	out.println()
	out.println("Adding artifact to repository...")
	if err := repo.AddArtifact(ctx, artifact, zipData); err != nil {
		return fmt.Errorf("failed to add artifact: %w", err)
	}

	out.println()
	out.printf("✓ Successfully added %s@%s\n", meta.Artifact.Name, meta.Artifact.Version)

	// Prompt for repository configurations (how/where it's used)
	repositories, err := promptForRepositories(out, artifact.Name, artifact.Version)
	if err != nil {
		return fmt.Errorf("failed to configure repositories: %w", err)
	}

	// If nil, user chose not to install (lock file already handled in prompt)
	if repositories == nil {
		return nil
	}

	// Set repositories on artifact
	artifact.Repositories = repositories

	// Update lock file with artifact
	if err := updateLockFile(ctx, out, repo, artifact); err != nil {
		return fmt.Errorf("failed to update lock file: %w", err)
	}

	return nil
}

// promptForMetadata prompts the user to enter metadata
func promptForMetadata(out *outputHelper, zipFile string, zipData []byte) (*metadata.Metadata, error) {
	// List files in zip
	files, err := utils.ListZipFiles(zipData)
	if err != nil {
		return nil, fmt.Errorf("failed to list zip files: %w", err)
	}

	// Prompt for name with default from zip file path
	defaultName := guessArtifactName(zipFile)
	name, err := out.promptWithDefault("Artifact name", defaultName)
	if err != nil {
		return nil, fmt.Errorf("failed to read name: %w", err)
	}

	// Prompt for version with default 1.0
	version, err := out.promptWithDefault("Version", "1.0")
	if err != nil {
		return nil, fmt.Errorf("failed to read version: %w", err)
	}

	// Auto-detect type using handlers
	detectedMeta := handlers.DetectArtifactType(files, name, version)
	artifactType := detectedMeta.Artifact.Type

	// Prompt for type with detected default
	typeInput, err := out.promptWithDefault("Type", artifactType)
	if err != nil {
		return nil, fmt.Errorf("failed to read type: %w", err)
	}
	if typeInput != "" {
		artifactType = typeInput
	}

	// If type changed, create new metadata, otherwise use detected
	if typeInput != "" && typeInput != detectedMeta.Artifact.Type {
		// User changed type, start fresh
		meta := &metadata.Metadata{
			MetadataVersion: "1.0",
			Artifact: metadata.Artifact{
				Name:    name,
				Version: version,
				Type:    artifactType,
			},
		}

		// Create type-specific sections
		switch artifactType {
		case "skill":
			meta.Skill = &metadata.SkillConfig{PromptFile: "SKILL.md"}
		case "agent":
			meta.Agent = &metadata.AgentConfig{PromptFile: "AGENT.md"}
		case "command":
			meta.Command = &metadata.CommandConfig{PromptFile: "COMMAND.md"}
		case "hook":
			event, err := out.prompt("Hook event (e.g., pre-commit): ")
			if err != nil {
				return nil, fmt.Errorf("failed to read hook event: %w", err)
			}
			meta.Hook = &metadata.HookConfig{
				Event:      event,
				ScriptFile: "hook.sh",
			}
		case "mcp", "mcp-remote":
			command, err := out.prompt("Command (e.g., node): ")
			if err != nil {
				return nil, fmt.Errorf("failed to read command: %w", err)
			}
			argsInput, err := out.prompt("Args (comma-separated, e.g., dist/index.js): ")
			if err != nil {
				return nil, fmt.Errorf("failed to read args: %w", err)
			}
			args := strings.Split(argsInput, ",")
			for i := range args {
				args[i] = strings.TrimSpace(args[i])
			}
			meta.MCP = &metadata.MCPConfig{
				Command: command,
				Args:    args,
			}
		}

		return meta, nil
	}

	// Type didn't change, use detected metadata
	return detectedMeta, nil
}

// extractOrDetectNameAndType extracts name and type from metadata or auto-detects them
func extractOrDetectNameAndType(out *outputHelper, zipFile string, zipData []byte) (name string, artifactType string, metadataExists bool, err error) {
	out.println("Detecting artifact name and type...")

	metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
	if err == nil {
		// Metadata exists, parse it
		meta, err := metadata.Parse(metadataBytes)
		if err != nil {
			return "", "", false, fmt.Errorf("failed to parse metadata: %w", err)
		}
		return meta.Artifact.Name, meta.Artifact.Type, true, nil
	}

	// No metadata, auto-detect name and type
	out.println("No metadata.toml found in zip. Auto-detecting...")

	// List files in zip
	files, err := utils.ListZipFiles(zipData)
	if err != nil {
		return "", "", false, fmt.Errorf("failed to list zip files: %w", err)
	}

	// Auto-detect values
	name = guessArtifactName(zipFile)

	// Use handlers to detect type
	detectedMeta := handlers.DetectArtifactType(files, name, "")
	artifactType = detectedMeta.Artifact.Type

	return name, artifactType, false, nil
}

// confirmNameAndType displays name and type and asks for confirmation
func confirmNameAndType(out *outputHelper, name, artifactType string) (string, string, error) {
	out.println()
	out.println("Detected artifact:")
	out.printf("  Name: %s\n", name)
	out.printf("  Type: %s\n", artifactType)
	out.println()

	response, err := out.prompt("Is this correct? (Y/n): ")
	if err != nil {
		return "", "", fmt.Errorf("failed to read confirmation: %w", err)
	}
	response = strings.ToLower(response)

	if response == "n" || response == "no" {
		// Prompt for custom name and type
		nameInput, err := out.promptWithDefault("Artifact name", name)
		if err != nil {
			return "", "", fmt.Errorf("failed to read name: %w", err)
		}
		if nameInput != "" {
			name = nameInput
		}

		typeInput, err := out.promptWithDefault("Artifact type", artifactType)
		if err != nil {
			return "", "", fmt.Errorf("failed to read type: %w", err)
		}
		if typeInput != "" {
			artifactType = typeInput
		}
	} else if response != "" && response != "y" && response != "yes" {
		return "", "", fmt.Errorf("cancelled by user")
	}

	return name, artifactType, nil
}

// determineSuggestedVersionAndCheckIdentical determines the version to suggest and whether contents are identical
func determineSuggestedVersionAndCheckIdentical(ctx context.Context, out *outputHelper, repo repository.Repository, name string, versions []string, newZipData []byte) (version string, identical bool, err error) {
	if len(versions) == 0 {
		// No existing versions, suggest 1.0
		return "1.0", false, nil
	}

	// Get the latest version
	latestVersion := versions[len(versions)-1]
	out.printf("Found existing version: %s\n", latestVersion)

	// Try to get the artifact for comparison
	out.println("Comparing with existing version...")

	var existingZipData []byte

	// Check if this is a git repository (has GetArtifactByVersion method)
	if gitRepo, ok := repo.(*repository.GitRepository); ok {
		existingZipData, err = gitRepo.GetArtifactByVersion(ctx, name, latestVersion)
	} else {
		// For other repos, we'd need to construct an artifact and use GetArtifact
		// For now, just suggest incrementing the version
		return suggestNextVersion(latestVersion), false, nil
	}

	if err != nil {
		// If we can't get the existing version, suggest incrementing
		return suggestNextVersion(latestVersion), false, nil
	}

	// Compare the contents
	contentsIdentical, err := utils.CompareZipContents(newZipData, existingZipData)
	if err != nil {
		return "", false, fmt.Errorf("failed to compare contents: %w", err)
	}

	if contentsIdentical {
		out.println("Contents are identical to existing version.")
		return latestVersion, true, nil
	}

	// Contents differ, suggest next version
	out.println("Contents differ from existing version.")
	return suggestNextVersion(latestVersion), false, nil
}

// suggestNextVersion suggests the next major version
func suggestNextVersion(currentVersion string) string {
	// Simple version incrementing: split by '.', increment first number
	parts := strings.Split(currentVersion, ".")
	if len(parts) == 0 {
		return "2.0"
	}

	major := 1
	if val, err := strconv.Atoi(parts[0]); err == nil {
		major = val + 1
	}

	return fmt.Sprintf("%d.0", major)
}

// promptForVersion prompts the user to confirm or edit the version
func promptForVersion(out *outputHelper, suggestedVersion string) (string, error) {
	out.println()
	version, err := out.promptWithDefault("Version", suggestedVersion)
	if err != nil {
		return "", fmt.Errorf("failed to read version: %w", err)
	}

	return version, nil
}

// createMetadata creates a metadata object with the given name, version, and type
func createMetadata(name, version, artifactType, zipFile string, zipData []byte) *metadata.Metadata {
	// List files in zip for handler detection
	files, _ := utils.ListZipFiles(zipData)

	// Use handlers to create metadata with type-specific config
	meta := handlers.DetectArtifactType(files, name, version)

	// Override with our confirmed values
	meta.Artifact.Name = name
	meta.Artifact.Version = version
	meta.Artifact.Type = artifactType

	return meta
}

// addMetadataToZip marshals and adds metadata to the zip
func addMetadataToZip(meta *metadata.Metadata, zipData []byte) ([]byte, error) {
	metadataBytes, err := metadata.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	newZipData, err := utils.AddFileToZip(zipData, "metadata.toml", metadataBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to add metadata to zip: %w", err)
	}

	return newZipData, nil
}

// autoDetectMetadata creates metadata by auto-detecting values from the zip
func autoDetectMetadata(zipFile string, zipData []byte) (*metadata.Metadata, error) {
	// List files in zip
	files, err := utils.ListZipFiles(zipData)
	if err != nil {
		return nil, fmt.Errorf("failed to list zip files: %w", err)
	}

	// Auto-detect values
	name := guessArtifactName(zipFile)
	version := "1.0"

	// Use handlers to detect type and create metadata
	meta := handlers.DetectArtifactType(files, name, version)

	return meta, nil
}

// guessArtifactName extracts a reasonable artifact name from the zip file path
func guessArtifactName(zipPath string) string {
	// Get base filename
	base := strings.TrimSuffix(strings.TrimSuffix(zipPath, ".zip"), ".ZIP")
	base = strings.TrimPrefix(base, "./")
	base = strings.TrimPrefix(base, "../")

	// If it's just a path, get the last component
	if idx := strings.LastIndex(base, "/"); idx != -1 {
		base = base[idx+1:]
	}
	if idx := strings.LastIndex(base, "\\"); idx != -1 {
		base = base[idx+1:]
	}

	// Strip version suffix if present (e.g., "my-skill-1.0.0" -> "my-skill")
	parts := strings.Split(base, "-")
	if len(parts) > 1 {
		lastPart := parts[len(parts)-1]
		// Check if last part looks like a version
		if strings.Contains(lastPart, ".") {
			allDigitsOrDots := true
			for _, c := range lastPart {
				if c != '.' && (c < '0' || c > '9') {
					allDigitsOrDots = false
					break
				}
			}
			if allDigitsOrDots {
				base = strings.Join(parts[:len(parts)-1], "-")
			}
		}
	}

	if base == "" {
		base = "my-artifact"
	}

	return base
}

// promptForRepositories prompts user for repository configurations and returns them
// Returns nil, nil if user chooses not to install (which removes it from lock file if present)
func promptForRepositories(out *outputHelper, artifactName, version string) ([]lockfile.Repository, error) {
	out.println()
	out.println("How would you like to install this artifact?")
	out.println("  1. Make it available globally (in all projects)")
	out.println("  2. Make it available for specific code repositories")
	out.println("  3. No, don't install it")
	out.println()

	choice, err := out.promptWithDefault("Choose an option", "1")
	if err != nil {
		return nil, fmt.Errorf("failed to read choice: %w", err)
	}

	switch choice {
	case "1":
		return []lockfile.Repository{}, nil // Empty array = global
	case "2":
		return collectRepositories(out)
	case "3":
		// Remove from lock file if present
		lockFilePath := constants.SkillLockFile
		if _, exists := lockfile.FindArtifact(lockFilePath, artifactName); exists {
			if err := lockfile.RemoveArtifact(lockFilePath, artifactName, version); err != nil {
				out.printfErr("Warning: failed to remove from lock file: %v\n", err)
			} else {
				out.println()
				out.println("✓ Removed from lock file")
			}
		} else {
			out.println()
			out.println("Artifact available in repository only")
		}
		return nil, nil // nil means don't install
	default:
		return nil, fmt.Errorf("invalid choice: %s", choice)
	}
}

// collectRepositories collects one or more repository configurations
// For each repo, asks if they want specific paths or the entire repo
// Requires at least one repository to be specified
func collectRepositories(out *outputHelper) ([]lockfile.Repository, error) {
	var repositories []lockfile.Repository

	for {
		repoURL, err := promptForRepo(out)
		if err != nil {
			return nil, err
		}

		// Ask if they want specific paths or entire repo
		response, err := out.prompt("Do you want to install for the entire repository? (Y/n): ")
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}
		response = strings.ToLower(strings.TrimSpace(response))

		var paths []string
		if response == "n" || response == "no" {
			// Collect paths
			for {
				pathStr, err := promptForPath(out)
				if err != nil {
					return nil, err
				}
				paths = append(paths, pathStr)

				if !promptForAnother(out, "Add another path in this repository? (y/N): ") {
					break
				}
			}
			if len(paths) > 0 {
				out.printf("✓ Will install for %s at paths: %v\n", repoURL, paths)
			} else {
				out.printf("✓ Will install for entire repository: %s\n", repoURL)
			}
		} else {
			out.printf("✓ Will install for entire repository: %s\n", repoURL)
		}

		repositories = append(repositories, lockfile.Repository{
			Repo:  repoURL,
			Paths: paths, // Empty if entire repo
		})

		if !promptForAnother(out, "Add another repository? (y/N): ") {
			break
		}
	}

	out.println()

	if len(repositories) == 0 {
		return nil, fmt.Errorf("at least one repository is required")
	}

	return repositories, nil
}

// updateLockFile updates the repository's lock file with the artifact
func updateLockFile(ctx context.Context, out *outputHelper, repo repository.Repository, artifact *lockfile.Artifact) error {
	// For git repos, update the lock file and commit
	if gitRepo, ok := repo.(*repository.GitRepository); ok {
		out.println()
		out.println("Updating repository lock file...")
		lockFilePath := gitRepo.GetLockFilePath()
		if err := lockfile.AddOrUpdateArtifact(lockFilePath, artifact); err != nil {
			return err
		}

		if artifact.IsGlobal() {
			out.println("✓ Updated lock file (global installation)")
		} else {
			out.printf("✓ Updated lock file with %d repository installation(s)\n", len(artifact.Repositories))
		}

		out.println("Committing and pushing to repository...")
		if err := gitRepo.CommitAndPush(ctx, artifact); err != nil {
			return err
		}
		out.println("✓ Changes pushed to repository")
	}
	return nil
}

// promptForRepo prompts for and validates a repository URL
// Accepts full URLs or GitHub slugs (e.g., "user/repo")
func promptForRepo(out *outputHelper) (string, error) {
	repoInput, err := out.prompt("Code repository URL or GitHub slug (e.g., user/repo): ")
	if err != nil {
		return "", fmt.Errorf("failed to read repository: %w", err)
	}
	repo := strings.TrimSpace(repoInput)
	if repo == "" {
		return "", fmt.Errorf("repository is required")
	}

	// If it's just a slug (e.g., "user/repo"), convert to full GitHub URL
	if !strings.Contains(repo, "://") && !strings.HasPrefix(repo, "git@") {
		// Check if it looks like a GitHub slug (contains exactly one slash)
		parts := strings.Split(repo, "/")
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			repo = "https://github.com/" + repo
		}
	}

	return repo, nil
}

// promptForPath prompts for and validates a path within a repository
func promptForPath(out *outputHelper) (string, error) {
	pathInput, err := out.prompt("Path within repository (e.g., backend/services): ")
	if err != nil {
		return "", fmt.Errorf("failed to read path: %w", err)
	}
	path := strings.TrimSpace(pathInput)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	return path, nil
}

// promptForAnother asks if the user wants to add another entry
func promptForAnother(out *outputHelper, prompt string) bool {
	another, err := out.prompt(prompt)
	if err != nil {
		return false
	}
	return strings.ToLower(strings.TrimSpace(another)) == "y"
}
