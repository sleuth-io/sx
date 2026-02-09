package commands

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/ui/components"
	"github.com/sleuth-io/sx/internal/utils"
)

// addRemoteMCP handles adding a remote MCP server from a URL
func addRemoteMCP(ctx context.Context, cmd *cobra.Command, out *outputHelper, status *components.Status, rawURL string, opts addOptions) error {
	// Auto-detect name from URL
	name := nameFromMCPURL(rawURL)
	if opts.Name != "" {
		name = opts.Name
	}

	// Determine transport — must match what the server supports
	transport := "sse"
	if !opts.Yes {
		selected, err := components.SelectWithIO(
			"Which transport protocol? (must match your server)",
			[]components.Option{
				{Label: "sse (most common)", Value: "sse", Description: "Server-Sent Events — choose if unsure"},
				{Label: "http", Value: "http", Description: "Streamable HTTP transport"},
			},
			cmd.InOrStdin(), cmd.OutOrStdout(),
		)
		if err != nil {
			return fmt.Errorf("failed to select transport: %w", err)
		}
		transport = selected.Value
	}

	// Confirm name
	if !opts.Yes {
		nameInput, err := components.InputWithIO("Asset name", "", name, cmd.InOrStdin(), cmd.OutOrStdout())
		if err != nil {
			return fmt.Errorf("failed to read name: %w", err)
		}
		if nameInput != "" {
			name = nameInput
		}
	}

	// Create metadata
	meta := &metadata.Metadata{
		MetadataVersion: metadata.CurrentMetadataVersion,
		Asset: metadata.Asset{
			Name:    name,
			Version: "1",
			Type:    asset.TypeMCP,
		},
		MCP: &metadata.MCPConfig{
			Transport: transport,
			URL:       rawURL,
		},
	}

	// Marshal metadata to TOML
	metadataBytes, err := metadata.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Create a metadata-only zip
	zipData, err := utils.CreateZipFromContent("metadata.toml", metadataBytes)
	if err != nil {
		return fmt.Errorf("failed to create zip: %w", err)
	}

	// Create vault and proceed with normal add flow
	vault, err := createVault()
	if err != nil {
		return err
	}

	versionCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	version, contentsIdentical, err := checkVersionAndContents(versionCtx, status, vault, name, zipData)
	if err != nil {
		return err
	}

	if opts.Version != "" {
		version = opts.Version
		contentsIdentical = false
	}

	var addErr error
	if contentsIdentical {
		addErr = handleIdenticalAsset(ctx, out, status, vault, name, version, asset.TypeMCP, opts)
	} else {
		addErr = addNewAsset(ctx, out, status, vault, name, asset.TypeMCP, version, rawURL, zipData, true, opts)
	}

	if addErr != nil {
		return addErr
	}

	// Handle install: auto-run if --yes, prompt if interactive, skip if --no-install
	if opts.Yes && !opts.NoInstall {
		out.println()
		if err := runInstall(cmd, nil, false, "", false, ""); err != nil {
			out.printfErr("Install failed: %v\n", err)
		}
	} else if !opts.NoInstall && !opts.isNonInteractive() {
		promptRunInstall(cmd, ctx, out)
	}

	return nil
}

// nameFromMCPURL generates a suggested asset name from a remote MCP URL
func nameFromMCPURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "my-mcp"
	}

	// Use hostname + path to build name
	host := parsed.Hostname()
	path := strings.Trim(parsed.Path, "/")

	// Remove common TLD suffixes for cleaner names
	host = strings.TrimSuffix(host, ".com")
	host = strings.TrimSuffix(host, ".io")
	host = strings.TrimSuffix(host, ".dev")
	host = strings.TrimSuffix(host, ".ai")

	// Combine host and path
	var parts []string
	if host != "" {
		parts = append(parts, host)
	}
	if path != "" {
		// Replace slashes with dashes
		path = strings.ReplaceAll(path, "/", "-")
		parts = append(parts, path)
	}

	name := strings.Join(parts, "-")

	// Replace dots with dashes
	name = strings.ReplaceAll(name, ".", "-")

	// Remove any characters that aren't alphanumeric, dashes, or underscores
	var cleaned strings.Builder
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			cleaned.WriteRune(c)
		}
	}
	name = cleaned.String()

	// Trim leading/trailing dashes
	name = strings.Trim(name, "-")

	if name == "" {
		return "my-mcp"
	}

	return name
}
