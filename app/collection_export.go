package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/sleuth-io/sx/internal/asset"
	geminihandlers "github.com/sleuth-io/sx/internal/clients/gemini/handlers"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

// Collection bundle export — the extension API's "export" capability
// (sx.collections.export, API 1.6.0). A collection's member assets are
// bundled into one downloadable zip in a chosen format. The builder is
// dialog-free so it unit-tests like any bridge core;
// ExportCollectionBundle adds the native save dialog on top (the same
// split as ImportDraftsFromFolder / importDraftsFrom).

// Known export formats. "zip" is a plain archive of every member asset;
// the plugin formats mirror docs/plugins-spec.md and carry skill assets
// only (the other types don't ride plugins yet — see the routing table
// there).
const (
	exportFormatZip    = "zip"
	exportFormatClaude = "claude-code"
	exportFormatCodex  = "codex"
	exportFormatGemini = "gemini"
)

// claudeBundlePlugin is .claude-plugin/plugin.json for an exported
// Claude Code plugin. Field names mirror the derived-marketplace types
// in internal/manifest/pluginexport.go.
type claudeBundlePlugin struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version"`
}

// codexBundlePlugin is .codex-plugin/plugin.json — the codexPlugin shape
// from internal/manifest/pluginexport.go, pointed at the bundle's
// skills/ directory.
type codexBundlePlugin struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Skills      string `json:"skills"`
}

// geminiExtensionManifest is gemini-extension.json (docs/plugins-spec.md,
// Gemini extension format).
type geminiExtensionManifest struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

// buildCollectionBundle builds the export archive in memory: the
// dialog-free core of ExportCollectionBundle.
func (a *App) buildCollectionBundle(name, format string) ([]byte, error) {
	switch format {
	case exportFormatZip, exportFormatClaude, exportFormatCodex, exportFormatGemini:
	default:
		return nil, fmt.Errorf("unknown export format %q (want claude-code, codex, gemini, or zip)", format)
	}
	col, err := a.findCollection(name)
	if err != nil {
		return nil, err
	}
	if len(col.Assets) == 0 {
		return nil, fmt.Errorf("collection %s has no assets to export", name)
	}
	description := col.Description
	if description == "" {
		description = fmt.Sprintf("Skills in the %s collection", name)
	}

	tmp, err := os.MkdirTemp("", "sx-export-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	root := filepath.Join(tmp, "bundle")
	if format == exportFormatGemini {
		// The gemini skill handler writes commands/ directly under a
		// target whose basename is ".gemini" — naming the staging root
		// that way reuses the exact conversion users get on install.
		root = filepath.Join(tmp, geminihandlers.ConfigDir)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	// Bridge methods can run before startup wires a.ctx (tests, early
	// boot); the gemini handler takes a context, so never hand it nil.
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	skills := 0
	for _, assetName := range col.Assets {
		zipData, err := a.latestAssetZip(assetName)
		if err != nil {
			return nil, err
		}
		var meta *metadata.Metadata
		if metaBytes, err := utils.ReadZipFile(zipData, "metadata.toml"); err == nil {
			meta, _ = metadata.Parse(metaBytes)
		}
		isSkill := meta != nil && meta.Asset.Type.Key == asset.TypeSkill.Key
		switch format {
		case exportFormatZip:
			// Everything ships: one folder per asset with its files.
			if err := utils.ExtractZip(zipData, filepath.Join(root, assetName)); err != nil {
				return nil, fmt.Errorf("unpacking %s: %w", assetName, err)
			}
		case exportFormatClaude, exportFormatCodex:
			if !isSkill {
				continue // plugin formats carry skills only (v1)
			}
			skills++
			if err := utils.ExtractZip(zipData, filepath.Join(root, "skills", assetName)); err != nil {
				return nil, fmt.Errorf("unpacking %s: %w", assetName, err)
			}
		case exportFormatGemini:
			if !isSkill {
				continue // plugin formats carry skills only (v1)
			}
			skills++
			// The install-path conversion: SKILL.md → commands/<name>.toml
			// with sx→Gemini syntax rewrites.
			if err := geminihandlers.NewSkillHandler(meta).Install(ctx, zipData, root); err != nil {
				return nil, fmt.Errorf("converting %s: %w", assetName, err)
			}
		}
	}
	if format != exportFormatZip && skills == 0 {
		return nil, fmt.Errorf("collection %s has no skill assets — the %s format exports skills only", name, format)
	}

	var manifestPath string
	var manifestPayload any
	switch format {
	case exportFormatClaude:
		manifestPath = filepath.Join(root, ".claude-plugin", "plugin.json")
		manifestPayload = claudeBundlePlugin{Name: name, Description: description, Version: "1.0.0"}
	case exportFormatCodex:
		manifestPath = filepath.Join(root, ".codex-plugin", "plugin.json")
		manifestPayload = codexBundlePlugin{Name: name, Version: "1.0.0", Description: description, Skills: "./skills"}
	case exportFormatGemini:
		manifestPath = filepath.Join(root, "gemini-extension.json")
		manifestPayload = geminiExtensionManifest{Name: name, Version: "1.0.0", Description: description}
	}
	if manifestPath != "" {
		if err := writeBundleJSON(manifestPath, manifestPayload); err != nil {
			return nil, err
		}
	}
	return utils.CreateZip(root)
}

func writeBundleJSON(path string, payload any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// ExportCollectionBundle builds a collection bundle and saves it where
// the user picks. Returns the saved path, or "" when the dialog is
// cancelled — the export capability's bridge entry point.
func (a *App) ExportCollectionBundle(name, format string) (string, error) {
	zipData, err := a.buildCollectionBundle(name, format)
	if err != nil {
		return "", err
	}
	path, err := wailsruntime.SaveFileDialog(a.ctx, wailsruntime.SaveDialogOptions{
		Title:           "Export " + name,
		DefaultFilename: name + "-" + format + ".zip",
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "Zip archives (*.zip)", Pattern: "*.zip"},
		},
	})
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil // cancelled
	}
	if err := os.WriteFile(path, zipData, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
