package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
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
		Short: "Manage vault assets (list, show)",
		Long:  "Browse and inspect assets in the configured vault.",
	}

	cmd.AddCommand(newVaultListCommand())
	cmd.AddCommand(newVaultShowCommand())

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
		return nil
	}

	ui := ui.NewOutput(out.cmd.OutOrStdout(), out.cmd.ErrOrStderr())

	ui.Newline()
	if typeFilter != "" {
		ui.Header(fmt.Sprintf("%s Assets", typeFilter))
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
	output := make([]map[string]interface{}, 0, len(result.Assets))
	for _, assetInfo := range result.Assets {
		output = append(output, map[string]interface{}{
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
		ui.KeyValue("Total Versions", fmt.Sprintf("%d", len(details.Versions)))
		ui.Newline()

		ui.Bold("Versions")
		// Display in descending order (newest first) for readability
		for i := len(details.Versions) - 1; i >= 0; i-- {
			v := details.Versions[i]
			versionLine := fmt.Sprintf("  %s", ui.EmphasisText("v"+v.Version))

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
	versions := make([]map[string]interface{}, 0, len(details.Versions))
	for _, v := range details.Versions {
		versions = append(versions, map[string]interface{}{
			"version":    v.Version,
			"createdAt":  v.CreatedAt,
			"filesCount": v.FilesCount,
		})
	}

	output := map[string]interface{}{
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
