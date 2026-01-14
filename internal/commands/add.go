package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/assets/detectors"
	"github.com/sleuth-io/sx/internal/buildinfo"
	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/constants"
	"github.com/sleuth-io/sx/internal/github"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/components"
	"github.com/sleuth-io/sx/internal/utils"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
	versionpkg "github.com/sleuth-io/sx/internal/version"
)

// NewAddCommand creates the add command
func NewAddCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add [source-or-asset-name]",
		Short: "Add an asset or configure an existing one",
		Long: `Add an asset from a local zip file, directory, URL, GitHub path, or marketplace.
If the argument is an existing asset name, configure its installation scope instead.

Examples:
  sx add ./my-skill           # Add from local directory
  sx add https://...          # Add from URL
  sx add https://github.com/owner/repo/tree/main/path  # Add from GitHub
  sx add plugin@marketplace   # Add plugin from Claude Code marketplace
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

	// Check if input is plugin@marketplace syntax
	if input != "" && IsMarketplaceReference(input) {
		return addFromMarketplace(ctx, cmd, out, status, input, promptInstall)
	}

	// Check if input is an existing asset name (not a file, directory, or URL)
	if input != "" && !isURL(input) && !github.IsTreeURL(input) {
		if _, err := os.Stat(input); os.IsNotExist(err) {
			// Not a file/directory - check if it's an existing asset
			return configureExistingAsset(ctx, cmd, out, status, input, promptInstall)
		}
	}

	// Get and validate zip file
	zipFile, zipData, err := loadZipFile(out, status, input)
	if err != nil {
		return err
	}

	// Detect asset name and type
	name, assetType, metadataExists, err := detectAssetInfo(out, status, zipFile, zipData)
	if err != nil {
		return err
	}

	// Create vault instance
	vault, err := createVault()
	if err != nil {
		return err
	}

	// Check versions and content
	version, contentsIdentical, err := checkVersionAndContents(ctx, status, vault, name, zipData)
	if err != nil {
		return err
	}

	// Handle identical content case
	var addErr error
	if contentsIdentical {
		addErr = handleIdenticalAsset(ctx, out, status, vault, name, version, assetType)
	} else {
		// Add new or updated asset
		addErr = addNewAsset(ctx, out, status, vault, name, assetType, version, zipFile, zipData, metadataExists)
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

// configureExistingAsset handles configuring scope for an asset that already exists in the vault
func configureExistingAsset(ctx context.Context, cmd *cobra.Command, out *outputHelper, status *components.Status, assetName string, promptInstall bool) error {
	// Load vault and lock file
	vault, lockFile, err := loadVaultAndLockFile(ctx, status)
	if err != nil {
		return err
	}

	// Find and select asset version
	foundAssets := findAssetsByName(lockFile, assetName)
	foundAsset, err := selectAssetVersion(foundAssets, assetName, out)
	if errors.Is(err, ErrAssetNotFound) {
		// Not in lock file - check if it exists in vault
		return handleNewAssetFromVault(ctx, cmd, out, status, vault, assetName, promptInstall)
	}
	if err != nil {
		return err
	}

	// Configure existing asset
	return configureFoundAsset(ctx, cmd, out, vault, foundAsset, promptInstall)
}

// configureFoundAsset handles configuring an asset that was found in the lock file
func configureFoundAsset(ctx context.Context, cmd *cobra.Command, out *outputHelper, vault vaultpkg.Vault, foundAsset *lockfile.Asset, promptInstall bool) error {
	out.printf("Configuring scope for %s@%s\n", foundAsset.Name, foundAsset.Version)

	// Normalize nil to empty slice for global installations
	currentScopes := foundAsset.Scopes
	if currentScopes == nil {
		currentScopes = []lockfile.Scope{}
	}

	// Prompt for scope configurations
	scopes, err := promptForRepositories(out, foundAsset.Name, foundAsset.Version, currentScopes)
	if err != nil {
		return fmt.Errorf("failed to configure repositories: %w", err)
	}

	// If nil, user chose to remove from installation
	if scopes == nil {
		return handleAssetRemoval(ctx, cmd, out, vault, foundAsset, promptInstall)
	}

	// Update asset with new repositories
	foundAsset.Scopes = scopes

	// Update lock file
	if err := updateLockFile(ctx, out, vault, foundAsset); err != nil {
		return fmt.Errorf("failed to update lock file: %w", err)
	}

	// Prompt to run install (if enabled)
	if promptInstall {
		promptRunInstall(cmd, ctx, out)
	}

	return nil
}

// promptRunInstall asks if the user wants to run install after adding an asset
func promptRunInstall(cmd *cobra.Command, ctx context.Context, out *outputHelper) {
	out.println()
	confirmed, err := components.ConfirmWithIO("Run install now to install the asset?", true, cmd.InOrStdin(), cmd.OutOrStdout())
	if err != nil {
		return
	}

	if !confirmed {
		out.println("Run 'sx install' when ready to install.")
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

// detectAssetInfo extracts or detects asset name and type, then confirms with user
func detectAssetInfo(out *outputHelper, status *components.Status, zipFile string, zipData []byte) (name string, assetType asset.Type, metadataExists bool, err error) {
	// Extract or detect name and type
	name, assetType, metadataExists, err = extractOrDetectNameAndType(status, zipFile, zipData)
	if err != nil {
		return
	}

	// Confirm name and type with user
	name, assetType, err = confirmNameAndType(out, name, assetType)
	if err != nil {
		return
	}

	return name, assetType, metadataExists, nil
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
func checkVersionAndContents(ctx context.Context, status *components.Status, vault vaultpkg.Vault, name string, zipData []byte) (version string, identical bool, err error) {
	status.Start("Checking for existing versions")
	versions, err := vault.GetVersionList(ctx, name)
	status.Clear()
	if err != nil {
		return "", false, fmt.Errorf("failed to get version list: %w", err)
	}

	version, identical, err = determineSuggestedVersionAndCheckIdentical(ctx, status, vault, name, versions, zipData)
	if err != nil {
		return "", false, err
	}

	return version, identical, nil
}

// handleIdenticalAsset handles the case when content is identical to existing version
func handleIdenticalAsset(ctx context.Context, out *outputHelper, status *components.Status, vault vaultpkg.Vault, name, version string, assetType asset.Type) error {
	_ = status // status not needed for identical assets (no git operations)
	out.println()
	out.printf("✓ Asset %s@%s already exists in vault with identical contents\n", name, version)

	// Check if already in lock file to get current scopes
	var currentScopes []lockfile.Scope
	lockFilePath := constants.SkillLockFile
	if existingArt, exists := lockfile.FindAsset(lockFilePath, name); exists {
		currentScopes = existingArt.Scopes
	}

	// Prompt for repository configurations (pass current if exists)
	scopes, err := promptForRepositories(out, name, version, currentScopes)
	if err != nil {
		return fmt.Errorf("failed to configure repositories: %w", err)
	}

	// If nil, user chose not to install (lock file already handled in prompt)
	if scopes == nil {
		return nil
	}

	// Update the lock file with asset
	lockAsset := &lockfile.Asset{
		Name:    name,
		Version: version,
		Type:    assetType,
		SourcePath: &lockfile.SourcePath{
			Path: fmt.Sprintf("./assets/%s/%s", name, version),
		},
		Scopes: scopes,
	}

	if err := updateLockFile(ctx, out, vault, lockAsset); err != nil {
		return fmt.Errorf("failed to update lock file: %w", err)
	}

	return nil
}

// addNewAsset adds a new or updated asset to the vault
func addNewAsset(ctx context.Context, out *outputHelper, status *components.Status, vault vaultpkg.Vault, name string, assetType asset.Type, version, zipFile string, zipData []byte, metadataExists bool) error {
	// Prompt user for version
	version, err := promptForVersion(out, version)
	if err != nil {
		return err
	}

	// Create full metadata with confirmed version
	meta := createMetadata(name, version, assetType, zipFile, zipData)

	// Always update metadata.toml to ensure version is correct
	zipData, err = updateMetadataInZip(meta, zipData, metadataExists)
	if err != nil {
		return err
	}

	// Create asset entry (what it is)
	lockAsset := &lockfile.Asset{
		Name:    meta.Asset.Name,
		Version: meta.Asset.Version,
		Type:    meta.Asset.Type,
		SourcePath: &lockfile.SourcePath{
			Path: fmt.Sprintf("./assets/%s/%s", meta.Asset.Name, meta.Asset.Version),
		},
	}

	// Upload asset files to vault
	out.println()
	status.Start("Adding asset to vault")
	if err := vault.AddAsset(ctx, lockAsset, zipData); err != nil {
		status.Fail("Failed to add asset")
		return fmt.Errorf("failed to add asset: %w", err)
	}
	status.Done("")

	out.printf("✓ Successfully added %s@%s\n", meta.Asset.Name, meta.Asset.Version)

	// Check if already in lock file to get current scopes
	var currentScopes []lockfile.Scope
	lockFilePath := constants.SkillLockFile
	if existingArt, exists := lockfile.FindAsset(lockFilePath, lockAsset.Name); exists {
		currentScopes = existingArt.Scopes
	}

	// Prompt for scope configurations (how/where it's used)
	scopes, err := promptForRepositories(out, lockAsset.Name, lockAsset.Version, currentScopes)
	if err != nil {
		return fmt.Errorf("failed to configure scopes: %w", err)
	}

	// If nil, user chose not to install (lock file already handled in prompt)
	if scopes == nil {
		return nil
	}

	// Set scopes on asset
	lockAsset.Scopes = scopes

	// Update lock file with asset
	if err := updateLockFile(ctx, out, vault, lockAsset); err != nil {
		return fmt.Errorf("failed to update lock file: %w", err)
	}

	return nil
}

// extractOrDetectNameAndType extracts name and type from metadata or auto-detects them
func extractOrDetectNameAndType(status *components.Status, zipFile string, zipData []byte) (name string, assetType asset.Type, metadataExists bool, err error) {
	status.Start("Detecting asset name and type")

	metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
	if err == nil {
		// Metadata exists, parse it
		meta, err := metadata.Parse(metadataBytes)
		if err != nil {
			status.Fail("Failed to parse metadata")
			return "", asset.Type{}, false, fmt.Errorf("failed to parse metadata: %w", err)
		}
		status.Done("")
		return meta.Asset.Name, meta.Asset.Type, true, nil
	}

	// No metadata, auto-detect name and type
	status.Update("Auto-detecting asset type")

	// List files in zip
	files, err := utils.ListZipFiles(zipData)
	if err != nil {
		status.Fail("Failed to list zip files")
		return "", asset.Type{}, false, fmt.Errorf("failed to list zip files: %w", err)
	}

	// Auto-detect values
	name = guessAssetName(zipFile)

	// Use handlers to detect type
	detectedMeta := detectors.DetectAssetType(files, name, "")
	assetType = detectedMeta.Asset.Type

	status.Done("")
	return name, assetType, false, nil
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
func determineSuggestedVersionAndCheckIdentical(ctx context.Context, status *components.Status, vault vaultpkg.Vault, name string, versions []string, newZipData []byte) (version string, identical bool, err error) {
	if len(versions) == 0 {
		// No existing versions, suggest 1
		return "1", false, nil
	}

	// Get the latest version
	latestVersion := versions[len(versions)-1]

	// Try to get the asset for comparison
	status.Start("Comparing with v" + latestVersion)

	var existingZipData []byte

	// Check if this is a git vault (has GetAssetByVersion method)
	if gitVault, ok := vault.(*vaultpkg.GitVault); ok {
		existingZipData, err = gitVault.GetAssetByVersion(ctx, name, latestVersion)
	} else {
		// For other vaults, we'd need to construct an asset and use GetAsset
		// For now, just suggest incrementing the version
		status.Clear()
		return versionpkg.IncrementMajor(latestVersion), false, nil
	}

	if err != nil {
		// If we can't get the existing version, suggest incrementing
		status.Clear()
		return versionpkg.IncrementMajor(latestVersion), false, nil
	}

	// Compare the contents
	contentsIdentical, err := utils.CompareZipContents(newZipData, existingZipData)
	status.Clear()
	if err != nil {
		return "", false, fmt.Errorf("failed to compare contents: %w", err)
	}

	if contentsIdentical {
		return latestVersion, true, nil
	}

	// Contents differ, suggest next version
	return versionpkg.IncrementMajor(latestVersion), false, nil
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
func createMetadata(name, version string, assetType asset.Type, zipFile string, zipData []byte) *metadata.Metadata {
	// Try to read existing metadata from zip first
	if metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml"); err == nil {
		if existingMeta, err := metadata.Parse(metadataBytes); err == nil {
			// Use existing metadata, just update name/version/type
			existingMeta.Asset.Name = name
			existingMeta.Asset.Version = version
			existingMeta.Asset.Type = assetType
			return existingMeta
		}
		// If parse fails, fall through to create new metadata
	}

	// No existing metadata or failed to parse - create new metadata using detection
	files, _ := utils.ListZipFiles(zipData)
	meta := detectors.DetectAssetType(files, name, version)

	// Override with our confirmed values
	meta.Asset.Name = name
	meta.Asset.Version = version
	meta.Asset.Type = assetType

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

// guessAssetName extracts a reasonable asset name from the zip file path or URL
func guessAssetName(zipPath string) string {
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
	base := strings.TrimPrefix(zipPath, "./")
	base = strings.TrimPrefix(base, "../")

	// If it's just a path, get the last component
	if idx := strings.LastIndex(base, "/"); idx != -1 {
		base = base[idx+1:]
	}
	if idx := strings.LastIndex(base, "\\"); idx != -1 {
		base = base[idx+1:]
	}

	// Strip any file extension
	if idx := strings.LastIndex(base, "."); idx != -1 {
		base = base[:idx]
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
func promptForRepositories(out *outputHelper, assetName, version string, currentRepos []lockfile.Scope) ([]lockfile.Scope, error) {
	// Use the new UI components (they automatically fall back to simple text in non-TTY)
	styledOut := ui.NewOutput(out.cmd.OutOrStdout(), out.cmd.ErrOrStderr())
	ioc := components.NewIOContext(out.cmd.InOrStdin(), out.cmd.OutOrStdout())
	return promptForRepositoriesWithUI(assetName, version, currentRepos, styledOut, ioc)
}
