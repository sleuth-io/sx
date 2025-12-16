package commands

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/skills/internal/asset"
	"github.com/sleuth-io/skills/internal/assets/detectors"
	"github.com/sleuth-io/skills/internal/buildinfo"
	"github.com/sleuth-io/skills/internal/config"
	"github.com/sleuth-io/skills/internal/constants"
	"github.com/sleuth-io/skills/internal/github"
	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/metadata"
	"github.com/sleuth-io/skills/internal/ui"
	vaultpkg "github.com/sleuth-io/skills/internal/vault"
	"github.com/sleuth-io/skills/internal/ui/components"
	"github.com/sleuth-io/skills/internal/utils"
)

// NewAddCommand creates the add command
func NewAddCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add [source-or-asset-name]",
		Short: "Add an asset or configure an existing one",
		Long: `Add an asset from a local zip file, directory, URL, or GitHub path.
If the argument is an existing asset name, configure its installation scope instead.

Examples:
  sx add ./my-skill           # Add from local directory
  sx add https://...          # Add from URL
  sx add https://github.com/owner/repo/tree/main/path  # Add from GitHub
  sx add my-skill             # Configure scope for existing asset`,
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
	return runAddWithOptions(cmd, zipFile, true)
}

// runAddSkipInstall executes the add command without prompting to install
func runAddSkipInstall(cmd *cobra.Command, zipFile string) error {
	return runAddWithOptions(cmd, zipFile, false)
}

// runAddWithOptions executes the add command with configurable options
func runAddWithOptions(cmd *cobra.Command, input string, promptInstall bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	out := newOutputHelper(cmd)
	status := components.NewStatus(cmd.OutOrStdout())

	// Check if input is an existing artifact name (not a file, directory, or URL)
	if input != "" && !isURL(input) && !github.IsTreeURL(input) {
		if _, err := os.Stat(input); os.IsNotExist(err) {
			// Not a file/directory - check if it's an existing artifact
			return configureExistingArtifact(ctx, cmd, out, status, input, promptInstall)
		}
	}

	// Get and validate zip file
	zipFile, zipData, err := loadZipFile(out, input)
	if err != nil {
		return err
	}

	// Detect artifact name and type
	name, artifactType, metadataExists, err := detectArtifactInfo(out, zipFile, zipData)
	if err != nil {
		return err
	}

	// Create vault instance
	vault, err := createVault()
	if err != nil {
		return err
	}

	// Check versions and content
	version, contentsIdentical, err := checkVersionAndContents(ctx, out, status, vault, name, zipData)
	if err != nil {
		return err
	}

	// Handle identical content case
	var addErr error
	if contentsIdentical {
		addErr = handleIdenticalArtifact(ctx, out, status, vault, name, version, artifactType)
	} else {
		// Add new or updated artifact
		addErr = addNewArtifact(ctx, out, status, vault, name, artifactType, version, zipFile, zipData, metadataExists)
	}

	if addErr != nil {
		return addErr
	}

	// Prompt to run install (if enabled)
	if promptInstall {
		promptRunInstall(cmd, ctx, out)
	}

	return nil
}

// configureExistingArtifact handles configuring scope for an artifact that already exists in the vault
func configureExistingArtifact(ctx context.Context, cmd *cobra.Command, out *outputHelper, status *components.Status, artifactName string, promptInstall bool) error {
	// Create vault instance
	vault, err := createVault()
	if err != nil {
		return err
	}

	// Load lock file to find the artifact
	status.Start("Syncing vault")
	lockFileContent, _, _, err := vault.GetLockFile(ctx, "")
	status.Clear()
	var lockFile *lockfile.LockFile
	if err != nil {
		// Lock file doesn't exist yet - create empty one
		lockFile = &lockfile.LockFile{
			Artifacts: []lockfile.Artifact{},
		}
	} else {
		lockFile, err = lockfile.Parse(lockFileContent)
		if err != nil {
			return fmt.Errorf("failed to parse lock file: %w", err)
		}
	}

	// Find all artifacts with this name in lock file
	var foundArtifacts []*lockfile.Artifact
	for i := range lockFile.Artifacts {
		if lockFile.Artifacts[i].Name == artifactName {
			foundArtifacts = append(foundArtifacts, &lockFile.Artifacts[i])
		}
	}

	// Handle multiple versions - ask user which to configure
	var foundArtifact *lockfile.Artifact
	if len(foundArtifacts) > 1 {
		// Build options for version selection
		options := make([]components.Option, len(foundArtifacts))
		for i, art := range foundArtifacts {
			scopeDesc := "global"
			if len(art.Repositories) > 0 {
				scopeDesc = fmt.Sprintf("%d repositories", len(art.Repositories))
			}
			options[i] = components.Option{
				Label:       fmt.Sprintf("v%s", art.Version),
				Value:       art.Version,
				Description: fmt.Sprintf("Currently installed: %s", scopeDesc),
			}
		}

		out.println()
		out.printf("Multiple versions of %s found in lock file\n", artifactName)
		selected, err := components.SelectWithIO("Which version would you like to configure?", options, out.cmd.InOrStdin(), out.cmd.OutOrStdout())
		if err != nil {
			return fmt.Errorf("failed to select version: %w", err)
		}

		// Find the selected artifact
		for _, art := range foundArtifacts {
			if art.Version == selected.Value {
				foundArtifact = art
				break
			}
		}
	} else if len(foundArtifacts) == 1 {
		foundArtifact = foundArtifacts[0]
	}

	// If not in lock file, check if it exists in artifacts directory
	if foundArtifact == nil {
		// Try to find versions in artifacts directory
		status.Start("Checking for artifact versions")
		versions, err := vault.GetVersionList(ctx, artifactName)
		status.Clear()
		if err != nil || len(versions) == 0 {
			return fmt.Errorf("asset '%s' not found in vault", artifactName)
		}

		// Use the latest version (last in list)
		latestVersion := versions[len(versions)-1]

		// Artifact exists in vault but not installed - treat as first-time install
		out.printf("Found asset: %s v%s in vault (not yet installed)\n", artifactName, latestVersion)

		// Prompt for repositories with nil current state (new install)
		repositories, err := promptForRepositories(out, artifactName, latestVersion, nil)
		if err != nil {
			return fmt.Errorf("failed to configure repositories: %w", err)
		}

		// If nil, user chose not to install
		if repositories == nil {
			out.println()
			out.println("Asset available in vault only")
			return nil
		}

		// Create new artifact entry for lock file
		newArtifact := &lockfile.Artifact{
			Name:    artifactName,
			Type:    asset.TypeSkill, // Default to skill, could enhance later
			Version: latestVersion,
			SourcePath: &lockfile.SourcePath{
				Path: fmt.Sprintf("./artifacts/%s/%s", artifactName, latestVersion),
			},
			Repositories: repositories,
		}

		// Add to lock file
		if err := updateLockFile(ctx, out, vault, newArtifact); err != nil {
			return fmt.Errorf("failed to update lock file: %w", err)
		}

		// Prompt to run install (if enabled)
		if promptInstall {
			promptRunInstall(cmd, ctx, out)
		}

		return nil
	}

	out.printf("Configuring scope for %s@%s\n", foundArtifact.Name, foundArtifact.Version)

	// Normalize nil to empty slice for global installations
	// When artifact is in lock file with nil Repositories, it means global (TOML parses empty array as nil)
	currentRepos := foundArtifact.Repositories
	if currentRepos == nil {
		currentRepos = []lockfile.Repository{}
	}

	// Prompt for repository configurations (pass current repositories for modification)
	repositories, err := promptForRepositories(out, foundArtifact.Name, foundArtifact.Version, currentRepos)
	if err != nil {
		return fmt.Errorf("failed to configure repositories: %w", err)
	}

	// If nil, user chose to remove from installation
	if repositories == nil {
		// Remove artifact from lock file
		if pathVault, ok := vault.(*vaultpkg.PathVault); ok {
			lockFilePath := pathVault.GetLockFilePath()
			if err := lockfile.RemoveArtifact(lockFilePath, foundArtifact.Name, foundArtifact.Version); err != nil {
				return fmt.Errorf("failed to remove artifact from lock file: %w", err)
			}
		} else if gitVault, ok := vault.(*vaultpkg.GitVault); ok {
			lockFilePath := gitVault.GetLockFilePath()
			if err := lockfile.RemoveArtifact(lockFilePath, foundArtifact.Name, foundArtifact.Version); err != nil {
				return fmt.Errorf("failed to remove artifact from lock file: %w", err)
			}
			// Commit and push the removal
			if err := gitVault.CommitAndPush(ctx, foundArtifact); err != nil {
				return fmt.Errorf("failed to push removal: %w", err)
			}
		}

		// Prompt to run install to clean up the removed artifact (if enabled)
		if promptInstall {
			out.println()
			confirmed, err := components.ConfirmWithIO("Run install now to remove the asset from clients?", true, cmd.InOrStdin(), cmd.OutOrStdout())
			if err != nil {
				return nil
			}

			if confirmed {
				out.println()
				if err := runInstall(cmd, nil, false, "", false); err != nil {
					out.printfErr("Install failed: %v\n", err)
				}
			} else {
				out.println("Run 'sx install' when ready to clean up.")
			}
		}
		return nil
	}

	// Update artifact with new repositories
	foundArtifact.Repositories = repositories

	// Update lock file
	if err := updateLockFile(ctx, out, vault, foundArtifact); err != nil {
		return fmt.Errorf("failed to update lock file: %w", err)
	}

	// Prompt to run install (if enabled)
	if promptInstall {
		promptRunInstall(cmd, ctx, out)
	}

	return nil
}

// promptRunInstall asks if the user wants to run install after adding an artifact
func promptRunInstall(cmd *cobra.Command, ctx context.Context, out *outputHelper) {
	out.println()
	confirmed, err := components.ConfirmWithIO("Run install now to activate the asset?", true, cmd.InOrStdin(), cmd.OutOrStdout())
	if err != nil {
		return
	}

	if !confirmed {
		out.println("Run 'sx install' when ready to activate.")
		return
	}

	out.println()
	if err := runInstall(cmd, nil, false, "", false); err != nil {
		out.printfErr("Install failed: %v\n", err)
	}
}

// isURL checks if the input looks like a URL
func isURL(input string) bool {
	return strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://")
}

// loadZipFile prompts for, loads, and validates the zip file, directory, or URL
func loadZipFile(out *outputHelper, zipFile string) (string, []byte, error) {
	// Prompt for zip file, directory, or URL if not provided
	if zipFile == "" {
		var err error
		zipFile, err = out.prompt("Enter path or URL to asset zip file or directory: ")
		if err != nil {
			return "", nil, fmt.Errorf("failed to read input: %w", err)
		}
	}

	if zipFile == "" {
		return "", nil, fmt.Errorf("zip file, directory path, or URL is required")
	}

	// Check if it's a GitHub tree URL (directory)
	if github.IsTreeURL(zipFile) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		out.println()
		out.println("Downloading from GitHub directory...")
		zipData, err := downloadFromGitHub(ctx, out, zipFile)
		if err != nil {
			return "", nil, err
		}
		return zipFile, zipData, nil
	}

	// Check if it's a regular URL (zip file)
	if isURL(zipFile) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		out.println()
		out.println("Downloading asset from URL...")
		zipData, err := downloadZipFromURL(ctx, out, zipFile)
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
	out.println()
	var zipData []byte

	if utils.IsDirectory(zipFile) {
		out.println("Creating zip from directory...")
		zipData, err = utils.CreateZip(zipFile)
		if err != nil {
			return "", nil, fmt.Errorf("failed to create zip from directory: %w", err)
		}
	} else {
		out.println("Reading asset...")
		zipData, err = os.ReadFile(zipFile)
		if err != nil {
			return "", nil, fmt.Errorf("failed to read zip file: %w", err)
		}

		// Verify it's a valid zip
		if !utils.IsZipFile(zipData) {
			return "", nil, fmt.Errorf("file is not a valid zip archive")
		}
	}

	return zipFile, zipData, nil
}

// downloadZipFromURL downloads a zip file from a URL
func downloadZipFromURL(ctx context.Context, out *outputHelper, zipURL string) ([]byte, error) {
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 5 * time.Minute,
	}

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, "GET", zipURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set user agent
	req.Header.Set("User-Agent", buildinfo.GetUserAgent())

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Read response body
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Verify it's a valid zip
	if !utils.IsZipFile(data) {
		return nil, fmt.Errorf("downloaded file is not a valid zip archive")
	}

	out.printf("Downloaded %d bytes\n", len(data))
	return data, nil
}

// downloadFromGitHub downloads files from a GitHub directory URL and returns them as a zip.
func downloadFromGitHub(ctx context.Context, out *outputHelper, gitHubURL string) ([]byte, error) {
	treeURL := github.ParseTreeURL(gitHubURL)
	if treeURL == nil {
		return nil, fmt.Errorf("invalid GitHub directory URL: %s", gitHubURL)
	}

	out.printf("Repository: %s/%s\n", treeURL.Owner, treeURL.Repo)
	out.printf("Branch/Tag: %s\n", treeURL.Ref)
	if treeURL.Path != "" {
		out.printf("Path: %s\n", treeURL.Path)
	}
	out.println()

	fetcher := github.NewFetcher()
	zipData, err := fetcher.FetchDirectory(ctx, treeURL)
	if err != nil {
		return nil, fmt.Errorf("failed to download from GitHub: %w", err)
	}

	out.printf("Downloaded %d bytes\n", len(zipData))
	return zipData, nil
}

// detectArtifactInfo extracts or detects artifact name and type, then confirms with user
func detectArtifactInfo(out *outputHelper, zipFile string, zipData []byte) (name string, artifactType asset.Type, metadataExists bool, err error) {
	// Extract or detect name and type
	name, artifactType, metadataExists, err = extractOrDetectNameAndType(out, zipFile, zipData)
	if err != nil {
		return
	}

	// Confirm name and type with user
	name, artifactType, err = confirmNameAndType(out, name, artifactType)
	if err != nil {
		return
	}

	return name, artifactType, metadataExists, nil
}

// createVault loads config and creates a vault instance
func createVault() (vaultpkg.Vault, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w\nRun 'sx init' to configure", err)
	}

	return vaultpkg.NewFromConfig(cfg)
}

// checkVersionAndContents queries vault for versions and checks if content is identical
func checkVersionAndContents(ctx context.Context, out *outputHelper, status *components.Status, vault vaultpkg.Vault, name string, zipData []byte) (version string, identical bool, err error) {
	status.Start("Checking for existing versions")
	versions, err := vault.GetVersionList(ctx, name)
	status.Clear()
	if err != nil {
		return "", false, fmt.Errorf("failed to get version list: %w", err)
	}

	version, identical, err = determineSuggestedVersionAndCheckIdentical(ctx, out, status, vault, name, versions, zipData)
	if err != nil {
		return "", false, err
	}

	return version, identical, nil
}

// handleIdenticalArtifact handles the case when content is identical to existing version
func handleIdenticalArtifact(ctx context.Context, out *outputHelper, status *components.Status, vault vaultpkg.Vault, name, version string, artifactType asset.Type) error {
	_ = status // status not needed for identical artifacts (no git operations)
	out.println()
	out.printf("✓ Artifact %s@%s already exists in vault with identical contents\n", name, version)

	// Check if already in lock file to get current repositories
	var currentRepos []lockfile.Repository
	lockFilePath := constants.SkillLockFile
	if existingArt, exists := lockfile.FindArtifact(lockFilePath, name); exists {
		currentRepos = existingArt.Repositories
	}

	// Prompt for repository configurations (pass current if exists)
	repositories, err := promptForRepositories(out, name, version, currentRepos)
	if err != nil {
		return fmt.Errorf("failed to configure repositories: %w", err)
	}

	// If nil, user chose not to install (lock file already handled in prompt)
	if repositories == nil {
		return nil
	}

	// Update the lock file with artifact
	lockArtifact := &lockfile.Artifact{
		Name:    name,
		Version: version,
		Type:    artifactType,
		SourcePath: &lockfile.SourcePath{
			Path: fmt.Sprintf("./artifacts/%s/%s", name, version),
		},
		Repositories: repositories,
	}

	if err := updateLockFile(ctx, out, vault, lockArtifact); err != nil {
		return fmt.Errorf("failed to update lock file: %w", err)
	}

	return nil
}

// addNewArtifact adds a new or updated artifact to the vault
func addNewArtifact(ctx context.Context, out *outputHelper, status *components.Status, vault vaultpkg.Vault, name string, artifactType asset.Type, version, zipFile string, zipData []byte, metadataExists bool) error {
	// Prompt user for version
	version, err := promptForVersion(out, version)
	if err != nil {
		return err
	}

	// Create full metadata with confirmed version
	meta := createMetadata(name, version, artifactType, zipFile, zipData)

	// Always update metadata.toml to ensure version is correct
	zipData, err = updateMetadataInZip(meta, zipData, metadataExists)
	if err != nil {
		return err
	}

	// Create artifact entry (what it is)
	lockArtifact := &lockfile.Artifact{
		Name:    meta.Artifact.Name,
		Version: meta.Artifact.Version,
		Type:    meta.Artifact.Type,
		SourcePath: &lockfile.SourcePath{
			Path: fmt.Sprintf("./artifacts/%s/%s", meta.Artifact.Name, meta.Artifact.Version),
		},
	}

	// Upload artifact files to vault
	out.println()
	status.Start("Adding asset to vault")
	if err := vault.AddArtifact(ctx, lockArtifact, zipData); err != nil {
		status.Fail("Failed to add artifact")
		return fmt.Errorf("failed to add artifact: %w", err)
	}
	status.Done("")

	out.printf("✓ Successfully added %s@%s\n", meta.Artifact.Name, meta.Artifact.Version)

	// Check if already in lock file to get current repositories
	var currentRepos []lockfile.Repository
	lockFilePath := constants.SkillLockFile
	if existingArt, exists := lockfile.FindArtifact(lockFilePath, lockArtifact.Name); exists {
		currentRepos = existingArt.Repositories
	}

	// Prompt for repository configurations (how/where it's used)
	repositories, err := promptForRepositories(out, lockArtifact.Name, lockArtifact.Version, currentRepos)
	if err != nil {
		return fmt.Errorf("failed to configure repositories: %w", err)
	}

	// If nil, user chose not to install (lock file already handled in prompt)
	if repositories == nil {
		return nil
	}

	// Set repositories on artifact
	lockArtifact.Repositories = repositories

	// Update lock file with artifact
	if err := updateLockFile(ctx, out, vault, lockArtifact); err != nil {
		return fmt.Errorf("failed to update lock file: %w", err)
	}

	return nil
}

// extractOrDetectNameAndType extracts name and type from metadata or auto-detects them
func extractOrDetectNameAndType(out *outputHelper, zipFile string, zipData []byte) (name string, artifactType asset.Type, metadataExists bool, err error) {
	out.println("Detecting asset name and type...")

	metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
	if err == nil {
		// Metadata exists, parse it
		meta, err := metadata.Parse(metadataBytes)
		if err != nil {
			return "", asset.Type{}, false, fmt.Errorf("failed to parse metadata: %w", err)
		}
		return meta.Artifact.Name, meta.Artifact.Type, true, nil
	}

	// No metadata, auto-detect name and type
	out.println("No metadata.toml found in zip. Auto-detecting...")

	// List files in zip
	files, err := utils.ListZipFiles(zipData)
	if err != nil {
		return "", asset.Type{}, false, fmt.Errorf("failed to list zip files: %w", err)
	}

	// Auto-detect values
	name = guessArtifactName(zipFile)

	// Use handlers to detect type
	detectedMeta := detectors.DetectArtifactType(files, name, "")
	artifactType = detectedMeta.Artifact.Type

	return name, artifactType, false, nil
}

// confirmNameAndType displays name and type and asks for confirmation
func confirmNameAndType(out *outputHelper, name string, inType asset.Type) (outName string, outType asset.Type, err error) {
	outName = name
	outType = inType

	out.println()
	out.println("Detected asset:")
	out.printf("  Name: %s\n", outName)
	out.printf("  Type: %s\n", outType)
	out.println()

	confirmed, err := components.ConfirmWithIO("Is this correct?", true, out.cmd.InOrStdin(), out.cmd.OutOrStdout())
	if err != nil {
		err = fmt.Errorf("failed to read confirmation: %w", err)
		return
	}

	if !confirmed {
		// Prompt for custom name and type
		nameInput, err2 := components.InputWithIO("Asset name", "", outName, out.cmd.InOrStdin(), out.cmd.OutOrStdout())
		if err2 != nil {
			err = fmt.Errorf("failed to read name: %w", err2)
			return
		}
		if nameInput != "" {
			outName = nameInput
		}

		typeInput, err2 := components.InputWithIO("Asset type", "", outType.Label, out.cmd.InOrStdin(), out.cmd.OutOrStdout())
		if err2 != nil {
			err = fmt.Errorf("failed to read type: %w", err2)
			return
		}
		if typeInput != "" {
			outType = asset.FromString(typeInput)
		}
	}

	return
}

// determineSuggestedVersionAndCheckIdentical determines the version to suggest and whether contents are identical
func determineSuggestedVersionAndCheckIdentical(ctx context.Context, out *outputHelper, status *components.Status, vault vaultpkg.Vault, name string, versions []string, newZipData []byte) (version string, identical bool, err error) {
	if len(versions) == 0 {
		// No existing versions, suggest 1.0
		return "1.0", false, nil
	}

	// Get the latest version
	latestVersion := versions[len(versions)-1]
	out.printf("Found existing version: %s\n", latestVersion)

	// Try to get the artifact for comparison
	status.Start("Comparing with existing version")

	var existingZipData []byte

	// Check if this is a git vault (has GetArtifactByVersion method)
	if gitVault, ok := vault.(*vaultpkg.GitVault); ok {
		existingZipData, err = gitVault.GetArtifactByVersion(ctx, name, latestVersion)
	} else {
		// For other vaults, we'd need to construct an artifact and use GetArtifact
		// For now, just suggest incrementing the version
		status.Clear()
		return suggestNextVersion(latestVersion), false, nil
	}

	if err != nil {
		// If we can't get the existing version, suggest incrementing
		status.Clear()
		return suggestNextVersion(latestVersion), false, nil
	}

	// Compare the contents
	contentsIdentical, err := utils.CompareZipContents(newZipData, existingZipData)
	status.Clear()
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
	version, err := components.InputWithIO("Version", "", suggestedVersion, out.cmd.InOrStdin(), out.cmd.OutOrStdout())
	if err != nil {
		return "", fmt.Errorf("failed to read version: %w", err)
	}

	return version, nil
}

// createMetadata creates a metadata object with the given name, version, and type
func createMetadata(name, version string, artifactType asset.Type, zipFile string, zipData []byte) *metadata.Metadata {
	// Try to read existing metadata from zip first
	if metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml"); err == nil {
		if existingMeta, err := metadata.Parse(metadataBytes); err == nil {
			// Use existing metadata, just update name/version/type
			existingMeta.Artifact.Name = name
			existingMeta.Artifact.Version = version
			existingMeta.Artifact.Type = artifactType
			return existingMeta
		}
		// If parse fails, fall through to create new metadata
	}

	// No existing metadata or failed to parse - create new metadata using detection
	files, _ := utils.ListZipFiles(zipData)
	meta := detectors.DetectArtifactType(files, name, version)

	// Override with our confirmed values
	meta.Artifact.Name = name
	meta.Artifact.Version = version
	meta.Artifact.Type = artifactType

	return meta
}

// updateMetadataInZip updates or adds metadata.toml in the zip with the correct version
func updateMetadataInZip(meta *metadata.Metadata, zipData []byte, metadataExists bool) ([]byte, error) {
	metadataBytes, err := metadata.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if metadataExists {
		// Replace existing metadata.toml in zip
		newZipData, err := utils.ReplaceFileInZip(zipData, "metadata.toml", metadataBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to replace metadata in zip: %w", err)
		}
		return newZipData, nil
	}

	// Add new metadata.toml to zip
	newZipData, err := utils.AddFileToZip(zipData, "metadata.toml", metadataBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to add metadata to zip: %w", err)
	}
	return newZipData, nil
}

// guessArtifactName extracts a reasonable artifact name from the zip file path or URL
func guessArtifactName(zipPath string) string {
	// Handle GitHub tree URLs specially
	if treeURL := github.ParseTreeURL(zipPath); treeURL != nil {
		return treeURL.SkillName()
	}

	// Handle URLs - extract path component
	if isURL(zipPath) {
		if parsed, err := url.Parse(zipPath); err == nil {
			zipPath = parsed.Path
		}
	}

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
		base = "my-asset"
	}

	return base
}

// promptForRepositories prompts user for repository configurations and returns them
// Takes currentRepos (nil if not installed, empty slice if global, or list of repos)
// Returns nil, nil if user chooses not to install (which removes it from lock file if present)
func promptForRepositories(out *outputHelper, artifactName, version string, currentRepos []lockfile.Repository) ([]lockfile.Repository, error) {
	// Use the new UI components (they automatically fall back to simple text in non-TTY)
	styledOut := ui.NewOutput(out.cmd.OutOrStdout(), out.cmd.ErrOrStderr())
	ioc := components.NewIOContext(out.cmd.InOrStdin(), out.cmd.OutOrStdout())
	return promptForRepositoriesWithUI(artifactName, version, currentRepos, styledOut, ioc)
}
