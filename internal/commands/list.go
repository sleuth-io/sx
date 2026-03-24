package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/components"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// NewListCommand creates a new list command
func NewListCommand() *cobra.Command {
	var typeFilter string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed assets",
		Long:  "Display all installed assets from the lock file with their types and versions.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd, typeFilter, jsonOutput)
		},
	}

	cmd.Flags().StringVar(&typeFilter, "type", "", "Filter by asset type (skill, mcp, agent, command, hook, rule)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}

func runList(cmd *cobra.Command, typeFilter string, jsonOutput bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out := newOutputHelper(cmd)

	if typeFilter != "" {
		t := asset.FromString(typeFilter)
		if !t.IsValid() {
			return fmt.Errorf("invalid asset type %q. Valid types: skill, mcp, agent, command, hook, rule, claude-code-plugin", typeFilter)
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w\nRun 'sx init' to configure", err)
	}

	vault, err := vaultpkg.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create vault: %w", err)
	}

	var status *components.Status
	if !jsonOutput {
		status = components.NewStatus(cmd.OutOrStdout())
		status.Start("Loading installed assets")
	}

	lockFileData, _, _, err := vault.GetLockFile(ctx, "")
	if err != nil {
		if status != nil {
			status.Fail("No lock file found")
		}
		if jsonOutput {
			return printListJSON(out, nil, typeFilter)
		}
		out.println("No assets installed. Run 'sx install' to install assets.")
		return nil
	}

	lf, err := lockfile.Parse(lockFileData)
	if err != nil {
		if status != nil {
			status.Fail("Failed to parse lock file")
		}
		return fmt.Errorf("failed to parse lock file: %w", err)
	}

	if status != nil {
		status.Done("")
	}

	assets := filterAssetsByType(lf.Assets, typeFilter)

	if jsonOutput {
		return printListJSON(out, assets, typeFilter)
	}
	return printListText(out, assets, typeFilter)
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

func printListJSON(out *outputHelper, assets []lockfile.Asset, typeFilter string) error {
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
	output := map[string][]map[string]any{
		"skills":              {},
		"mcps":                {},
		"agents":              {},
		"commands":            {},
		"hooks":               {},
		"rules":               {},
		"claude-code-plugins": {},
	}

	for _, a := range assets {
		item := map[string]any{
			"name":    a.Name,
			"version": a.Version,
		}

		switch a.Type.Key {
		case "skill":
			output["skills"] = append(output["skills"], item)
		case "mcp":
			output["mcps"] = append(output["mcps"], item)
		case "agent":
			output["agents"] = append(output["agents"], item)
		case "command":
			output["commands"] = append(output["commands"], item)
		case "hook":
			output["hooks"] = append(output["hooks"], item)
		case "rule":
			output["rules"] = append(output["rules"], item)
		case "claude-code-plugin":
			output["claude-code-plugins"] = append(output["claude-code-plugins"], item)
		}
	}

	data, err := json.Marshal(output)
	if err != nil {
		return err
	}
	out.printlnAlways(string(data))
	return nil
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

func printListText(out *outputHelper, assets []lockfile.Asset, typeFilter string) error {
	if len(assets) == 0 {
		if typeFilter != "" {
			out.println(fmt.Sprintf("No %s assets installed.", typeFilter))
		} else {
			out.println("No assets installed.")
		}
		out.println("Run 'sx install' to install assets from the vault.")
		return nil
	}

	uiOut := ui.NewOutput(out.cmd.OutOrStdout(), out.cmd.ErrOrStderr())

	uiOut.Newline()
	if typeFilter != "" {
		t := asset.FromString(typeFilter)
		uiOut.Header("Installed " + t.Label + " Assets")
	} else {
		uiOut.Header("Installed Assets")
	}
	uiOut.Newline()

	byType := make(map[string][]lockfile.Asset)
	for _, a := range assets {
		byType[a.Type.Label] = append(byType[a.Type.Label], a)
	}

	var types []string
	for typeName := range byType {
		types = append(types, typeName)
	}
	sort.Strings(types)

	for _, typeName := range types {
		typeAssets := byType[typeName]
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
