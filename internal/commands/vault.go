package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/logger"
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
	var installedOnly bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all assets in the vault",
		Long:  "Display all available assets with their types and versions.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVaultList(cmd, typeFilter, jsonOutput, installedOnly)
		},
	}

	cmd.Flags().StringVar(&typeFilter, "type", "", "Filter by asset type (skill, mcp, agent, command, hook, rule, claude-code-plugin)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	cmd.Flags().BoolVar(&installedOnly, "installed", false, "List only installed assets from lock file")

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

func runVaultList(cmd *cobra.Command, typeFilter string, jsonOutput, installedOnly bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out := newOutputHelper(cmd)

	if typeFilter != "" {
		t := asset.FromString(typeFilter)
		if !t.IsValid() {
			return fmt.Errorf("invalid asset type %q. Valid types: skill, mcp, agent, command, hook, rule, claude-code-plugin", typeFilter)
		}
	}

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
		if installedOnly {
			status.Start("Loading installed assets")
		} else {
			status.Start("Fetching assets from vault")
		}
	}

	// Get lock file (needed for both modes)
	var lf *lockfile.LockFile
	lockFileData, _, _, lfErr := vault.GetLockFile(ctx, "")
	if lfErr == nil {
		var parseErr error
		lf, parseErr = lockfile.Parse(lockFileData)
		if parseErr != nil {
			log := logger.Get()
			log.Warn("failed to parse lock file", "error", parseErr)
		}
	}

	// If --installed, use lock file data instead of querying vault
	if installedOnly {
		if status != nil {
			status.Done("")
		}

		if lf == nil {
			if jsonOutput {
				return printInstalledListJSON(out, nil, typeFilter)
			}
			out.println("No assets installed. Run 'sx install' to install assets.")
			return nil
		}

		// Convert lock file assets to vault result format
		assets := filterAssetsByType(lf.Assets, typeFilter)
		if jsonOutput {
			return printInstalledListJSON(out, assets, typeFilter)
		}
		return printInstalledListText(out, assets, typeFilter)
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
		return printVaultListJSON(out, result, typeFilter)
	}
	return printVaultListText(out, result, lf, typeFilter)
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

// getTypeLabel returns a display label for an asset type, with fallback for unknown types
func getTypeLabel(t asset.Type) string {
	if t.Label != "" {
		return t.Label
	}
	// Fallback: capitalize the key
	if t.Key != "" {
		return strings.ToUpper(t.Key[:1]) + t.Key[1:]
	}
	return "Unknown"
}

func filterAssetsByType(assets []lockfile.Asset, typeFilter string) []lockfile.Asset {
	if typeFilter == "" {
		return assets
	}

	filterType := asset.FromString(typeFilter)
	var filtered []lockfile.Asset
	for _, a := range assets {
		if a.Type.Key == filterType.Key {
			filtered = append(filtered, a)
		}
	}
	return filtered
}

func typeFilterToJSONKey(typeFilter string) string {
	switch typeFilter {
	case "skill":
		return "skills"
	case "mcp":
		return "mcps"
	case "agent":
		return "agents"
	case "command":
		return "commands"
	case "hook":
		return "hooks"
	case "rule":
		return "rules"
	case "claude-code-plugin":
		return "claude-code-plugins"
	default:
		return typeFilter + "s"
	}
}

func printVaultListText(out *outputHelper, result *vaultpkg.ListAssetsResult, lf *lockfile.LockFile, typeFilter string) error {
	uiOut := ui.NewOutput(out.cmd.OutOrStdout(), out.cmd.ErrOrStderr())

	if typeFilter != "" {
		t := asset.FromString(typeFilter)
		uiOut.Header("Vault " + getTypeLabel(t) + " Assets")
	} else {
		uiOut.Header("Vault Assets")
	}
	uiOut.Newline()

	if len(result.Assets) == 0 {
		if typeFilter != "" {
			t := asset.FromString(typeFilter)
			uiOut.Bold(getTypeLabel(t) + "s")
			uiOut.Newline()
		}
		return nil
	}

	// Build a map of asset name -> scopes from lock file for quick lookup
	scopeMap := make(map[string][]lockfile.Scope)
	isGlobalMap := make(map[string]bool)
	if lf != nil {
		for _, a := range lf.Assets {
			scopeMap[a.Name] = a.Scopes
			isGlobalMap[a.Name] = a.IsGlobal()
		}
	}

	// Group by type label
	byType := make(map[string][]vaultpkg.AssetSummary)
	for _, assetInfo := range result.Assets {
		label := getTypeLabel(assetInfo.Type)
		byType[label] = append(byType[label], assetInfo)
	}

	// Sort type names for consistent output
	var types []string
	for typeName := range byType {
		types = append(types, typeName)
	}
	sort.Strings(types)

	for _, typeName := range types {
		assets := byType[typeName]
		// Sort assets by name within each type
		sort.Slice(assets, func(i, j int) bool {
			return assets[i].Name < assets[j].Name
		})
		uiOut.Bold(typeName + "s")
		for _, assetInfo := range assets {
			scopeInfo := ""
			if isGlobal, ok := isGlobalMap[assetInfo.Name]; ok && isGlobal {
				scopeInfo = uiOut.MutedText(" (global)")
			} else if scopes, ok := scopeMap[assetInfo.Name]; ok && len(scopes) > 0 {
				scopeInfo = uiOut.MutedText(fmt.Sprintf(" (%d scopes)", len(scopes)))
			}

			assetLine := fmt.Sprintf("  %s %s%s",
				uiOut.EmphasisText(assetInfo.Name),
				uiOut.MutedText("v"+assetInfo.LatestVersion),
				scopeInfo,
			)
			uiOut.Println(assetLine)

			// Show description if available
			if assetInfo.Description != "" {
				uiOut.Println("    " + uiOut.MutedText(assetInfo.Description))
			}
		}
		uiOut.Newline()
	}

	return nil
}

// jsonAssetItem represents a single asset for JSON output
type jsonAssetItem struct {
	Name    string
	Version string
	TypeKey string
}

// newGroupedJSONOutput creates an empty grouped output map with all type keys
func newGroupedJSONOutput() map[string][]map[string]any {
	return map[string][]map[string]any{
		"skills":              {},
		"mcps":                {},
		"agents":              {},
		"commands":            {},
		"hooks":               {},
		"rules":               {},
		"claude-code-plugins": {},
	}
}

// addItemToGroupedOutput adds an asset item to the appropriate type group
func addItemToGroupedOutput(output map[string][]map[string]any, item jsonAssetItem) {
	entry := map[string]any{
		"name":    item.Name,
		"version": item.Version,
	}

	switch item.TypeKey {
	case "skill":
		output["skills"] = append(output["skills"], entry)
	case "mcp":
		output["mcps"] = append(output["mcps"], entry)
	case "agent":
		output["agents"] = append(output["agents"], entry)
	case "command":
		output["commands"] = append(output["commands"], entry)
	case "hook":
		output["hooks"] = append(output["hooks"], entry)
	case "rule":
		output["rules"] = append(output["rules"], entry)
	case "claude-code-plugin":
		output["claude-code-plugins"] = append(output["claude-code-plugins"], entry)
	}
}

func printVaultListJSON(out *outputHelper, result *vaultpkg.ListAssetsResult, typeFilter string) error {
	// If filtering by type, return dict with just that key
	if typeFilter != "" {
		items := make([]map[string]any, 0, len(result.Assets))
		for _, a := range result.Assets {
			items = append(items, map[string]any{
				"name":    a.Name,
				"version": a.LatestVersion,
			})
		}
		key := typeFilterToJSONKey(typeFilter)
		output := map[string][]map[string]any{key: items}
		data, err := json.Marshal(output)
		if err != nil {
			return err
		}
		out.printlnAlways(string(data))
		return nil
	}

	// No filter: return full object grouped by type
	output := newGroupedJSONOutput()
	for _, a := range result.Assets {
		addItemToGroupedOutput(output, jsonAssetItem{
			Name:    a.Name,
			Version: a.LatestVersion,
			TypeKey: a.Type.Key,
		})
	}

	data, err := json.Marshal(output)
	if err != nil {
		return err
	}
	out.printlnAlways(string(data))
	return nil
}

func printInstalledListText(out *outputHelper, assets []lockfile.Asset, typeFilter string) error {
	uiOut := ui.NewOutput(out.cmd.OutOrStdout(), out.cmd.ErrOrStderr())

	if typeFilter != "" {
		t := asset.FromString(typeFilter)
		uiOut.Header("Installed " + getTypeLabel(t) + " Assets")
	} else {
		uiOut.Header("Installed Assets")
	}
	uiOut.Newline()

	if len(assets) == 0 {
		if typeFilter != "" {
			t := asset.FromString(typeFilter)
			uiOut.Bold(getTypeLabel(t) + "s")
			uiOut.Newline()
		}
		return nil
	}

	byType := make(map[string][]lockfile.Asset)
	for _, a := range assets {
		label := getTypeLabel(a.Type)
		byType[label] = append(byType[label], a)
	}

	var types []string
	for typeName := range byType {
		types = append(types, typeName)
	}
	sort.Strings(types)

	for _, typeName := range types {
		typeAssets := byType[typeName]
		// Sort assets by name within each type
		sort.Slice(typeAssets, func(i, j int) bool {
			return typeAssets[i].Name < typeAssets[j].Name
		})
		uiOut.Bold(typeName + "s")
		for _, a := range typeAssets {
			scopeInfo := ""
			if a.IsGlobal() {
				scopeInfo = uiOut.MutedText(" (global)")
			} else if len(a.Scopes) > 0 {
				scopeInfo = uiOut.MutedText(fmt.Sprintf(" (%d scopes)", len(a.Scopes)))
			}

			assetLine := fmt.Sprintf("  %s %s%s",
				uiOut.EmphasisText(a.Name),
				uiOut.MutedText("v"+a.Version),
				scopeInfo,
			)
			uiOut.Println(assetLine)
		}
		uiOut.Newline()
	}

	return nil
}

func printInstalledListJSON(out *outputHelper, assets []lockfile.Asset, typeFilter string) error {
	// If filtering by type, return dict with just that key
	if typeFilter != "" {
		items := make([]map[string]any, 0, len(assets))
		for _, a := range assets {
			items = append(items, map[string]any{
				"name":    a.Name,
				"version": a.Version,
			})
		}
		key := typeFilterToJSONKey(typeFilter)
		output := map[string][]map[string]any{key: items}
		data, err := json.Marshal(output)
		if err != nil {
			return err
		}
		out.printlnAlways(string(data))
		return nil
	}

	// No filter: return full object grouped by type
	output := newGroupedJSONOutput()
	for _, a := range assets {
		addItemToGroupedOutput(output, jsonAssetItem{
			Name:    a.Name,
			Version: a.Version,
			TypeKey: a.Type.Key,
		})
	}

	data, err := json.Marshal(output)
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
		if err := runInstall(cmd, nil, false, "", false, "", "", false); err != nil {
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
		if err := runInstall(cmd, nil, false, "", false, "", "", false); err != nil {
			out.printfErr("Install failed: %v\n", err)
		}
	} else {
		out.println("Run 'sx install' when ready to apply changes to clients.")
	}

	return nil
}
