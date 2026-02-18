package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sleuth-io/sx/internal/assets"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/logger"
)

// hookAssetInfo holds name and type for display purposes in hook mode
type hookAssetInfo struct {
	name string
	typ  string
}

// checkHookModeFastPath checks if we should skip installation in hook mode.
// Returns true if installation should be skipped (already ran for this session).
func checkHookModeFastPath(ctx context.Context, hookClientID string, out *outputHelper) bool {
	if hookClientID == "" {
		return false
	}

	registry := clients.Global()
	hookClient, err := registry.Get(hookClientID)
	if err != nil {
		return false
	}

	shouldInstall, err := hookClient.ShouldInstall(ctx)
	if err != nil {
		log := logger.Get()
		log.Warn("ShouldInstall check failed", "client", hookClientID, "error", err)
		return false
	}

	if !shouldInstall {
		log := logger.Get()
		log.Info("install skipped by client", "client", hookClientID, "reason", "already ran for this session")
		outputHookModeJSON(out, map[string]any{"continue": true})
		return true
	}

	return false
}

// outputHookModeJSON outputs a JSON response for hook mode
func outputHookModeJSON(out *outputHelper, response map[string]any) {
	jsonBytes, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return
	}
	out.printlnAlways(string(jsonBytes))
}

// buildHookModeMessage builds the system message for hook mode output
func buildHookModeMessage(installResult *assets.InstallResult, downloads []*assets.AssetWithMetadata) string {
	var installedAssets []hookAssetInfo
	for _, name := range installResult.Installed {
		for _, art := range downloads {
			if art.Asset.Name == name {
				installedAssets = append(installedAssets, hookAssetInfo{
					name: name,
					typ:  strings.ToLower(art.Metadata.Asset.Type.Label),
				})
				break
			}
		}
	}

	return formatInstalledAssetsMessage(installedAssets)
}

// formatInstalledAssetsMessage formats the message based on number of installed assets
func formatInstalledAssetsMessage(installedAssets []hookAssetInfo) string {
	const (
		bold      = "\033[1m"
		blue      = "\033[34m"
		resetBold = "\033[22m"
		reset     = "\033[0m"
	)

	switch len(installedAssets) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("%ssx%s installed the %s%s %s%s.",
			bold, resetBold, blue, installedAssets[0].name, installedAssets[0].typ, reset)
	default:
		return formatMultipleAssetsMessage(installedAssets, bold, blue, resetBold, reset)
	}
}

// formatMultipleAssetsMessage formats the message for multiple installed assets
func formatMultipleAssetsMessage(installedAssets []hookAssetInfo, bold, blue, resetBold, reset string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%ssx%s installed:\n", bold, resetBold)

	// Show up to 3 assets
	displayCount := min(len(installedAssets), 3)
	for i := range displayCount {
		fmt.Fprintf(&sb, "- The %s%s %s%s\n", blue, installedAssets[i].name, installedAssets[i].typ, reset)
	}

	// Add "and N more" if there are more than 3
	if len(installedAssets) > 3 {
		fmt.Fprintf(&sb, "and %d more", len(installedAssets)-3)
	} else {
		// Remove trailing newline for <= 3 items
		result := sb.String()
		return strings.TrimSuffix(result, "\n")
	}

	return sb.String()
}

// outputHookModeInstallResult outputs the JSON response for hook mode after successful installation
func outputHookModeInstallResult(out *outputHelper, installResult *assets.InstallResult, downloads []*assets.AssetWithMetadata) error {
	message := buildHookModeMessage(installResult, downloads)
	response := map[string]any{
		"systemMessage": message,
		"continue":      true,
	}

	jsonBytes, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON response: %w", err)
	}

	out.printlnAlways(string(jsonBytes))
	return nil
}
