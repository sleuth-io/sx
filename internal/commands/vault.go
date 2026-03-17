package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/components"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// NewVaultCommand creates a new vault command
func NewVaultCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vault",
		Short: "Manage vault assets (list, show, remove, rename)",
		Long:  "Browse, inspect, remove, and rename assets in the configured vault.",
	}

	cmd.AddCommand(newVaultListCommand())
	cmd.AddCommand(newVaultShowCommand())
	cmd.AddCommand(newVaultRemoveCommand())
	cmd.AddCommand(newVaultRenameCommand())

	return cmd
}

func newVaultListCommand() *cobra.Command {
	var typeFilter string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all assets in the vault",
		Long:  "Display all available assets with their types and versions.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVaultList(cmd, typeFilter, jsonOutput)
		},
	}

	cmd.Flags().StringVar(&typeFilter, "type", "", "Filter by asset type (skill, mcp, agent, command, hook)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}

func newVaultShowCommand() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "show <asset-name>",
		Short: "Show details for a specific asset",
		Long:  "Display detailed information about an asset including all versions.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVaultShow(cmd, args[0], jsonOutput)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}

func runVaultList(cmd *cobra.Command, typeFilter string, jsonOutput bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out := newOutputHelper(cmd)

	// Load config and create vault
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w\nRun 'sx init' to configure", err)
	}

	vault, err := vaultpkg.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create vault: %w", err)
	}

	// Only show status for text output (not JSON)
	var status *components.Status
	if !jsonOutput {
		status = components.NewStatus(cmd.OutOrStdout())
		status.Start("Fetching assets from vault")
	}

	result, err := vault.ListAssets(ctx, vaultpkg.ListAssetsOptions{
		Type:  typeFilter,
		Limit: 0, // Use default limit (will be capped at 50 for Sleuth vaults)
	})

	if status != nil {
		status.Done("")
	}

	if err != nil {
		return fmt.Errorf("failed to list assets: %w", err)
	}

	if jsonOutput {
		return printVaultListJSON(out, result)
	}
	return printVaultListText(out, result, typeFilter)
}

func runVaultShow(cmd *cobra.Command, assetName string, jsonOutput bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out := newOutputHelper(cmd)

	// Load config and create vault
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w\nRun 'sx init' to configure", err)
	}

	vault, err := vaultpkg.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create vault: %w", err)
	}

	// Only show status for text output (not JSON)
	var status *components.Status
	if !jsonOutput {
		status = components.NewStatus(cmd.OutOrStdout())
		status.Start("Fetching asset details")
	}

	details, err := vault.GetAssetDetails(ctx, assetName)

	if status != nil {
		status.Done("")
	}

	if err != nil {
		return fmt.Errorf("failed to get asset details: %w", err)
	}

	// Check if asset is installed by looking in lock file
	var currentScopes []lockfile.Scope
	var scopesFound bool
	lockFileData, _, _, err := vault.GetLockFile(ctx, "")
	if err == nil {
		if lockFile, err := lockfile.Parse(lockFileData); err == nil {
			for i := range lockFile.Assets {
				if lockFile.Assets[i].Name == assetName {
					currentScopes = lockFile.Assets[i].Scopes
					scopesFound = true
					break
				}
			}
		}
	}

	if jsonOutput {
		return printVaultShowJSON(out, details, scopesFound, currentScopes)
	}
	return printVaultShowText(out, details, scopesFound, currentScopes)
}

func printVaultListText(out *outputHelper, result *vaultpkg.ListAssetsResult, typeFilter string) error {
	if len(result.Assets) == 0 {
		out.println("No assets found in vault.")
		out.println("Add skills with 'sx add' or browse skills.sh with 'sx add --browse'.")
		return nil
	}

	ui := ui.NewOutput(out.cmd.OutOrStdout(), out.cmd.ErrOrStderr())

	ui.Newline()
	if typeFilter != "" {
		ui.Header(typeFilter + " Assets")
	} else {
		ui.Header("Vault Assets")
	}
	ui.Newline()

	// Group by type
	byType := make(map[string][]vaultpkg.AssetSummary)
	for _, assetInfo := range result.Assets {
		byType[assetInfo.Type.Label] = append(byType[assetInfo.Type.Label], assetInfo)
	}

	// Sort type names for consistent output
	var types []string
	for typeName := range byType {
		types = append(types, typeName)
	}
	sort.Strings(types)

	for _, typeName := range types {
		assets := byType[typeName]
		ui.Bold(typeName + "s")
		for _, assetInfo := range assets {
			// Show version info
			versionInfo := ""
			if assetInfo.VersionsCount > 1 {
				versionInfo = ui.MutedText(fmt.Sprintf(" (%d versions)", assetInfo.VersionsCount))
			}

			assetLine := fmt.Sprintf("  %s %s%s",
				ui.EmphasisText(assetInfo.Name),
				ui.MutedText("v"+assetInfo.LatestVersion),
				versionInfo,
			)
			ui.Println(assetLine)

			if assetInfo.Description != "" {
				ui.Muted("    " + assetInfo.Description)
			}
		}
		ui.Newline()
	}

	return nil
}

func printVaultListJSON(out *outputHelper, result *vaultpkg.ListAssetsResult) error {
	// Create JSON-friendly output
	output := make([]map[string]any, 0, len(result.Assets))
	for _, assetInfo := range result.Assets {
		output = append(output, map[string]any{
			"name":          assetInfo.Name,
			"type":          assetInfo.Type.Key,
			"latestVersion": assetInfo.LatestVersion,
			"versionsCount": assetInfo.VersionsCount,
			"description":   assetInfo.Description,
			"createdAt":     assetInfo.CreatedAt,
			"updatedAt":     assetInfo.UpdatedAt,
		})
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}
	out.printlnAlways(string(data))
	return nil
}

func printVaultShowText(out *outputHelper, details *vaultpkg.AssetDetails, scopesFound bool, currentScopes []lockfile.Scope) error {
	ui := ui.NewOutput(out.cmd.OutOrStdout(), out.cmd.ErrOrStderr())

	ui.Newline()
	ui.Header(details.Name)
	ui.Muted(details.Type.Label)
	ui.Newline()

	if details.Description != "" {
		ui.Println(details.Description)
		ui.Newline()
	}

	// Show installation status
	if scopesFound {
		displayCurrentInstallation(currentScopes, ui)
	} else {
		ui.Newline()
		ui.Info("Installation Status:")
		ui.Println("  Not installed (available in vault only)")
		ui.Newline()
	}

	if len(details.Versions) > 0 {
		// Versions are in ascending order (oldest first), so last element is latest
		latestVersion := details.Versions[len(details.Versions)-1].Version
		ui.KeyValue("Latest Version", "v"+latestVersion)
		ui.KeyValue("Total Versions", strconv.Itoa(len(details.Versions)))
		ui.Newline()

		ui.Bold("Versions")
		// Display in descending order (newest first) for readability
		for i := len(details.Versions) - 1; i >= 0; i-- {
			v := details.Versions[i]
			versionLine := "  " + ui.EmphasisText("v"+v.Version)

			if !v.CreatedAt.IsZero() {
				versionLine += ui.MutedText(" · " + v.CreatedAt.Format("Jan 2, 2006"))
			}

			if v.FilesCount > 0 {
				versionLine += ui.MutedText(fmt.Sprintf(" · %d files", v.FilesCount))
			}

			ui.Println(versionLine)
		}
		ui.Newline()
	}

	if details.Metadata != nil {
		if len(details.Metadata.Asset.Dependencies) > 0 {
			ui.Bold("Dependencies")
			for _, dep := range details.Metadata.Asset.Dependencies {
				ui.ListItem("•", dep)
			}
			ui.Newline()
		}
	}

	return nil
}

func printVaultShowJSON(out *outputHelper, details *vaultpkg.AssetDetails, scopesFound bool, currentScopes []lockfile.Scope) error {
	// Create JSON-friendly output
	versions := make([]map[string]any, 0, len(details.Versions))
	for _, v := range details.Versions {
		versions = append(versions, map[string]any{
			"version":    v.Version,
			"createdAt":  v.CreatedAt,
			"filesCount": v.FilesCount,
		})
	}

	output := map[string]any{
		"name":        details.Name,
		"type":        details.Type.Key,
		"description": details.Description,
		"versions":    versions,
		"createdAt":   details.CreatedAt,
		"updatedAt":   details.UpdatedAt,
		"installed":   scopesFound,
	}

	if scopesFound {
		output["installationScopes"] = currentScopes
	}

	if details.Metadata != nil {
		output["metadata"] = details.Metadata
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return err
	}
	out.printlnAlways(string(data))
	return nil
}

func newVaultRemoveCommand() *cobra.Command {
	var yes bool
	var versionFlag string
	var deleteFlag bool

	cmd := &cobra.Command{
		Use:   "remove <asset-name>",
		Short: "Remove an asset from the lock file and optionally delete from vault",
		Long: `Remove an asset from the lock file. Optionally also permanently delete
the asset files from the vault with --delete.

Examples:
  sx vault remove my-skill              # Remove from lock file only
  sx vault remove my-skill --delete     # Remove and permanently delete from vault
  sx vault remove my-skill -v 1.0.0     # Remove specific version
  sx vault remove my-skill --yes        # Skip confirmation prompts`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVaultRemove(cmd, args[0], versionFlag, yes, deleteFlag)
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompts and automatically run install")
	cmd.Flags().StringVarP(&versionFlag, "version", "v", "", "Version to remove (defaults to all versions)")
	cmd.Flags().BoolVar(&deleteFlag, "delete", false, "Also permanently delete asset files from the vault")

	return cmd
}

func runVaultRemove(cmd *cobra.Command, assetName, versionFlag string, yes, deleteFlag bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	out := newOutputHelper(cmd)
	status := components.NewStatus(cmd.OutOrStdout())

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w\nRun 'sx init' to configure", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Create vault
	vault, err := vaultpkg.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create vault: %w", err)
	}

	// Get all versions from the lock file
	status.Start("Loading lock file")
	lockFileData, _, _, err := vault.GetLockFile(ctx, "")
	if err != nil {
		status.Fail("Failed to get lock file")
		return fmt.Errorf("failed to get lock file: %w", err)
	}

	lf, err := lockfile.Parse(lockFileData)
	if err != nil {
		status.Fail("Failed to parse lock file")
		return fmt.Errorf("failed to parse lock file: %w", err)
	}
	status.Clear()

	// Collect all versions of this asset
	var versions []string
	for _, a := range lf.Assets {
		if a.Name == assetName {
			versions = append(versions, a.Version)
		}
	}

	if len(versions) == 0 {
		return fmt.Errorf("asset %q not found in lock file", assetName)
	}

	// If version specified, only remove that version
	versionsToRemove := versions
	if versionFlag != "" {
		versionsToRemove = []string{versionFlag}
	}

	// Confirm removal
	if !yes {
		action := "remove from lock file"
		if deleteFlag {
			action = "permanently delete from vault"
		}
		msg := fmt.Sprintf("%s %s? This will %s.", assetName, formatVersions(versionsToRemove), action)
		confirmed, err := components.ConfirmWithIO(msg, true, cmd.InOrStdin(), cmd.OutOrStdout())
		if err != nil || !confirmed {
			return nil
		}
	}

	for _, assetVersion := range versionsToRemove {
		actionLabel := "Removing"
		if deleteFlag {
			actionLabel = "Deleting"
		}
		status.Start(fmt.Sprintf("%s %s@%s", actionLabel, assetName, assetVersion))

		if err := vault.RemoveAsset(ctx, assetName, assetVersion, deleteFlag); err != nil {
			status.Fail("Failed to remove asset")
			return fmt.Errorf("failed to remove asset: %w", err)
		}

		doneLabel := "Removed"
		if deleteFlag {
			doneLabel = "Deleted"
		}
		status.Done(fmt.Sprintf("%s %s@%s", doneLabel, assetName, assetVersion))
	}

	// Prompt to run install
	shouldInstall := yes
	if !yes {
		out.println()
		confirmed, err := components.ConfirmWithIO("Run install now to apply changes to clients?", true, cmd.InOrStdin(), cmd.OutOrStdout())
		if err != nil {
			return nil
		}
		shouldInstall = confirmed
	}

	if shouldInstall {
		out.println()
		if err := runInstall(cmd, nil, false, "", false, "", ""); err != nil {
			out.printfErr("Install failed: %v\n", err)
		}
	} else {
		out.println("Run 'sx install' when ready to apply changes to clients.")
	}

	return nil
}

func formatVersions(versions []string) string {
	if len(versions) == 1 {
		return "@" + versions[0]
	}
	return fmt.Sprintf("(%d versions)", len(versions))
}

func newVaultRenameCommand() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "rename <old-name> <new-name>",
		Short: "Rename an asset in the vault",
		Long: `Rename an asset in the vault. All versions and installations are
preserved under the new name.

Examples:
  sx vault rename old-skill new-skill        # Rename an asset
  sx vault rename old-skill new-skill --yes  # Skip confirmation`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVaultRename(cmd, args[0], args[1], yes)
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompts")

	return cmd
}

func runVaultRename(cmd *cobra.Command, oldName, newName string, yes bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	out := newOutputHelper(cmd)
	status := components.NewStatus(cmd.OutOrStdout())

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w\nRun 'sx init' to configure", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Create vault
	vault, err := vaultpkg.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create vault: %w", err)
	}

	// Validate old name exists
	status.Start("Checking asset")
	_, err = vault.GetAssetDetails(ctx, oldName)
	if err != nil {
		status.Fail("Asset not found")
		return fmt.Errorf("asset '%s' not found in vault", oldName)
	}

	// Validate new name does NOT exist (best-effort; server enforces atomicity for Sleuth vaults)
	if _, err := vault.GetAssetDetails(ctx, newName); err == nil {
		status.Fail("Name conflict")
		return fmt.Errorf("asset '%s' already exists in vault", newName)
	}
	status.Clear()

	// Confirm
	if !yes {
		msg := fmt.Sprintf("Rename '%s' to '%s'?", oldName, newName)
		confirmed, err := components.ConfirmWithIO(msg, true, cmd.InOrStdin(), cmd.OutOrStdout())
		if err != nil || !confirmed {
			return nil
		}
	}

	status.Start(fmt.Sprintf("Renaming %s to %s", oldName, newName))
	if err := vault.RenameAsset(ctx, oldName, newName); err != nil {
		status.Fail("Failed to rename asset")
		return fmt.Errorf("failed to rename asset: %w", err)
	}
	status.Done(fmt.Sprintf("Renamed %s to %s", oldName, newName))

	// Prompt to run install
	shouldInstall := yes
	if !yes {
		out.println()
		confirmed, err := components.ConfirmWithIO("Run install now to apply changes to clients?", true, cmd.InOrStdin(), cmd.OutOrStdout())
		if err != nil {
			return nil
		}
		shouldInstall = confirmed
	}

	if shouldInstall {
		out.println()
		if err := runInstall(cmd, nil, false, "", false, "", ""); err != nil {
			out.printfErr("Install failed: %v\n", err)
		}
	} else {
		out.println("Run 'sx install' when ready to apply changes to clients.")
	}

	return nil
}
