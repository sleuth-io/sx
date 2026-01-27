package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/ui/components"
	"github.com/sleuth-io/sx/internal/utils"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// Section is an alias for utils.MarkdownSection for convenience
type Section = utils.MarkdownSection

// isRuleFile checks if the path is a rule file that should be imported as a rule asset
func isRuleFile(path string) bool {
	// Check if any client recognizes this as a rule file
	return clients.IsRuleFile(path)
}

// isInstructionFile checks if the path is an instruction file that can be parsed for sections
func isInstructionFile(path string) bool {
	return clients.IsInstructionFile(path)
}

// isImportableRuleFile checks if a file can be imported as rule(s)
func isImportableRuleFile(path string) bool {
	return isRuleFile(path) || isInstructionFile(path)
}

// createZipFromRuleFile creates a zip archive from a rule file
// Parses frontmatter using the appropriate client parser and stores clean content
func createZipFromRuleFile(filePath string) ([]byte, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// Parse the rule file using the registry
	parsed, err := clients.ParseRuleFile(filePath, content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse rule file: %w", err)
	}

	// Derive asset name from filename
	baseName := filepath.Base(filePath)
	ext := filepath.Ext(baseName)
	assetName := strings.TrimSuffix(baseName, ext)

	// Create metadata with parsed globs
	meta := &metadata.Metadata{
		MetadataVersion: "1.0",
		Asset: metadata.Asset{
			Name:    assetName,
			Type:    asset.TypeRule,
			Version: "1.0", // Default version, user will confirm
		},
		Rule: &metadata.RuleConfig{
			Title:       assetName,
			PromptFile:  "RULE.md",
			Globs:       parsed.Globs,
			Description: parsed.Description,
		},
	}

	// Store clean content (without frontmatter) in RULE.md
	cleanContent := strings.TrimSpace(parsed.Content)
	if cleanContent == "" {
		cleanContent = string(content) // Fallback to original if parsing stripped everything
	}

	// Create zip with RULE.md
	zipData, err := utils.CreateZipFromContent("RULE.md", []byte(cleanContent))
	if err != nil {
		return nil, err
	}

	// Add metadata.toml to zip
	metaBytes, err := metadata.Marshal(meta)
	if err != nil {
		return nil, err
	}

	return utils.AddFileToZip(zipData, "metadata.toml", metaBytes)
}

// createRuleFromSection creates a rule asset from a parsed section
func createRuleFromSection(section Section, name string) ([]byte, *metadata.Metadata, error) {
	// Create metadata
	meta := &metadata.Metadata{
		MetadataVersion: "1.0",
		Asset: metadata.Asset{
			Name:    name,
			Type:    asset.TypeRule,
			Version: "1.0",
		},
		Rule: &metadata.RuleConfig{
			Title:      section.Heading,
			PromptFile: "RULE.md",
		},
	}

	// Create zip with RULE.md containing the section content
	zipData, err := utils.CreateZipFromContent("RULE.md", []byte(section.Content))
	if err != nil {
		return nil, nil, err
	}

	// Add metadata.toml to zip
	metaBytes, err := metadata.Marshal(meta)
	if err != nil {
		return nil, nil, err
	}

	zipData, err = utils.AddFileToZip(zipData, "metadata.toml", metaBytes)
	if err != nil {
		return nil, nil, err
	}

	return zipData, meta, nil
}

// addFromInstructionFile handles adding rules from an instruction file (CLAUDE.md, AGENTS.md)
func addFromInstructionFile(ctx context.Context, cmd *cobra.Command, out *outputHelper, status *components.Status, filePath string, promptInstall bool) error {
	// Parse sections from file
	sections, err := readAndParseSections(filePath)
	if err != nil {
		return err
	}

	if len(sections) == 0 {
		out.println("No ## sections found in file. Nothing to import.")
		return nil
	}

	// Display sections and prompt for selection
	displaySections(out, filePath, sections)
	selectedSections, err := promptSectionSelection(out, sections)
	if err != nil {
		return fmt.Errorf("failed to select sections: %w", err)
	}

	if len(selectedSections) == 0 {
		out.println("No sections selected. Nothing to import.")
		return nil
	}

	// Create vault instance
	vault, err := createVault()
	if err != nil {
		return err
	}

	// Add each selected section as a rule
	var addedRules []string
	for _, section := range selectedSections {
		name, version, err := addSectionAsRule(ctx, cmd, out, status, vault, section)
		if err != nil {
			out.printfErr("Failed to add section %q: %v\n", section.Heading, err)
			continue
		}
		addedRules = append(addedRules, name+"@"+version)
	}

	// Summary
	if len(addedRules) > 0 {
		out.println()
		out.printf("Successfully added %d rules: %s\n", len(addedRules), strings.Join(addedRules, ", "))

		// Prompt to remove sections from source file
		if err := promptRemoveSections(cmd, out, filePath, selectedSections); err != nil {
			out.printfErr("Warning: failed to remove sections: %v\n", err)
		}

		if promptInstall {
			promptRunInstall(cmd, ctx, out)
		}
	}

	return nil
}

// readAndParseSections reads a file and parses its markdown sections
func readAndParseSections(filePath string) ([]Section, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	return utils.ParseMarkdownSections(string(content)), nil
}

// displaySections shows the available sections to the user
func displaySections(out *outputHelper, filePath string, sections []Section) {
	out.printf("Found %d sections in %s:\n\n", len(sections), filepath.Base(filePath))
	for i, section := range sections {
		out.printf("  %d. %s\n", i+1, section.Heading)
	}
	out.println()
}

// promptSectionSelection prompts user to select which sections to import
func promptSectionSelection(out *outputHelper, sections []Section) ([]Section, error) {
	out.println("Enter section numbers to import (comma-separated), or 'all' for all:")
	input, err := out.prompt("> ")
	if err != nil {
		return nil, err
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil
	}

	if strings.ToLower(input) == "all" {
		return sections, nil
	}

	return parseSectionIndices(out, input, sections), nil
}

// parseSectionIndices parses comma-separated section indices
func parseSectionIndices(out *outputHelper, input string, sections []Section) []Section {
	var selected []Section
	for part := range strings.SplitSeq(input, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		var idx int
		if _, err := fmt.Sscanf(part, "%d", &idx); err != nil {
			out.printfErr("Invalid section number: %s\n", part)
			continue
		}

		if idx < 1 || idx > len(sections) {
			out.printfErr("Section number out of range: %d\n", idx)
			continue
		}

		selected = append(selected, sections[idx-1])
	}
	return selected
}

// addSectionAsRule adds a single section as a rule asset to the vault
func addSectionAsRule(ctx context.Context, cmd *cobra.Command, out *outputHelper, status *components.Status, vault vaultpkg.Vault, section Section) (string, string, error) {
	// Get asset name
	name, err := promptRuleName(cmd, out, section.Heading)
	if err != nil {
		return "", "", err
	}

	// Create zip to check content
	zipData, _, err := createRuleFromSection(section, name)
	if err != nil {
		return "", "", fmt.Errorf("failed to create rule: %w", err)
	}

	// Check if identical content already exists
	version, isIdentical, err := checkRuleContent(ctx, status, vault, name, zipData)
	if err != nil {
		return "", "", err
	}

	if isIdentical {
		out.printf("✓ Rule %s@%s already exists with identical content\n", name, version)
		// Configure scopes for existing asset
		if err := configureRuleScopes(ctx, out, vault, name, version); err != nil {
			return "", "", err
		}
		return name, version, nil
	}

	// Prompt for version
	versionInput, err := components.InputWithIO("Version", "", version, cmd.InOrStdin(), cmd.OutOrStdout())
	if err != nil {
		return "", "", fmt.Errorf("failed to read version: %w", err)
	}
	if versionInput != "" {
		version = versionInput
	}

	// Upload the rule
	if err := uploadRuleZip(ctx, status, vault, out, name, version, zipData); err != nil {
		return "", "", err
	}

	// Configure scopes
	if err := configureRuleScopes(ctx, out, vault, name, version); err != nil {
		return "", "", err
	}

	return name, version, nil
}

// promptRuleName prompts for the rule name
func promptRuleName(cmd *cobra.Command, out *outputHelper, heading string) (string, error) {
	name := utils.Slugify(heading)
	if name == "" {
		name = "rule"
	}

	out.println()
	nameInput, err := components.InputWithIO("Name for \""+heading+"\"", "", name, cmd.InOrStdin(), cmd.OutOrStdout())
	if err != nil {
		return "", fmt.Errorf("failed to read name: %w", err)
	}
	if nameInput != "" {
		name = nameInput
	}
	return name, nil
}

// checkRuleContent checks if identical content already exists in vault
func checkRuleContent(ctx context.Context, status *components.Status, vault vaultpkg.Vault, name string, zipData []byte) (version string, identical bool, err error) {
	status.Start("Checking for existing versions of " + name)
	versions, err := vault.GetVersionList(ctx, name)
	status.Clear()
	if err != nil {
		return "", false, fmt.Errorf("failed to get version list: %w", err)
	}

	if len(versions) == 0 {
		return "1", false, nil
	}

	// Get latest version and compare content
	latestVersion := versions[len(versions)-1]
	status.Start("Comparing with v" + latestVersion)

	gitVault, ok := vault.(*vaultpkg.GitVault)
	if !ok {
		status.Clear()
		return incrementVersion(latestVersion), false, nil
	}

	existingZip, err := gitVault.GetAssetByVersion(ctx, name, latestVersion)
	status.Clear()
	if err != nil {
		return incrementVersion(latestVersion), false, nil
	}

	identical, err = utils.CompareZipContents(zipData, existingZip)
	if err != nil {
		return incrementVersion(latestVersion), false, nil
	}

	if identical {
		return latestVersion, true, nil
	}

	return incrementVersion(latestVersion), false, nil
}

// uploadRuleZip uploads a pre-created zip to the vault
func uploadRuleZip(ctx context.Context, status *components.Status, vault vaultpkg.Vault, out *outputHelper, name, version string, zipData []byte) error {
	// Update metadata with version
	metaBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
	if err != nil {
		return fmt.Errorf("failed to read metadata: %w", err)
	}

	meta, err := metadata.Parse(metaBytes)
	if err != nil {
		return fmt.Errorf("failed to parse metadata: %w", err)
	}

	meta.Asset.Version = version
	newMetaBytes, err := metadata.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	zipData, err = utils.ReplaceFileInZip(zipData, "metadata.toml", newMetaBytes)
	if err != nil {
		return fmt.Errorf("failed to update metadata: %w", err)
	}

	lockAsset := &lockfile.Asset{
		Name:    name,
		Version: version,
		Type:    asset.TypeRule,
		SourcePath: &lockfile.SourcePath{
			Path: fmt.Sprintf("./assets/%s/%s", name, version),
		},
	}

	status.Start("Adding " + name + " to vault")
	if err := vault.AddAsset(ctx, lockAsset, zipData); err != nil {
		status.Fail("Failed")
		return fmt.Errorf("failed to add asset: %w", err)
	}
	status.Done("")

	out.printf("✓ Added %s@%s\n", name, version)
	return nil
}

// configureRuleScopes prompts for and saves scope configuration
func configureRuleScopes(ctx context.Context, out *outputHelper, vault vaultpkg.Vault, name, version string) error {
	scopes, err := promptForRepositories(out, name, version, nil)
	if err != nil {
		return fmt.Errorf("failed to configure scopes: %w", err)
	}

	if scopes != nil {
		lockAsset := &lockfile.Asset{
			Name:    name,
			Version: version,
			Type:    asset.TypeRule,
			Scopes:  scopes,
			SourcePath: &lockfile.SourcePath{
				Path: fmt.Sprintf("./assets/%s/%s", name, version),
			},
		}
		if err := updateLockFile(ctx, out, vault, lockAsset); err != nil {
			return fmt.Errorf("failed to update lock file: %w", err)
		}
	}
	return nil
}

// incrementVersion increments a version string (e.g., "1" -> "2")
func incrementVersion(version string) string {
	var major int
	if _, err := fmt.Sscanf(version, "%d", &major); err == nil {
		return strconv.Itoa(major + 1)
	}
	return version + ".1"
}

// promptRemoveSections asks if user wants to remove imported sections from source file
func promptRemoveSections(cmd *cobra.Command, out *outputHelper, filePath string, sections []Section) error {
	if len(sections) == 0 {
		return nil
	}

	out.println()
	confirmed, err := components.ConfirmWithIO(
		fmt.Sprintf("Remove %d imported section(s) from %s?", len(sections), filepath.Base(filePath)),
		false,
		cmd.InOrStdin(),
		cmd.OutOrStdout(),
	)
	if err != nil {
		return err
	}

	if !confirmed {
		return nil
	}

	// Collect headings to remove
	headings := make([]string, len(sections))
	for i, s := range sections {
		headings[i] = s.Heading
	}

	// Read current file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Remove sections and write back
	newContent := utils.RemoveMarkdownSections(string(content), headings)
	if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	out.printf("✓ Removed %d section(s) from %s\n", len(sections), filepath.Base(filePath))
	return nil
}
