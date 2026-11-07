package publish

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sleuth-io/sx/v2/internal/asset"
	"github.com/sleuth-io/sx/v2/internal/metadata"
	"github.com/sleuth-io/sx/v2/internal/utils"
)

// Issue 243 reported a config-only MCP asset stored with an empty command and
// nil args. This pins down that the zip sx uploads still carries them, so a
// regression on the client side shows up here and not at install time.
func TestConfigOnlyMCPKeepsCommandAndArgsInUploadedZip(t *testing.T) {
	dir := t.TempDir()
	src := `metadata-version = "1.0"

[asset]
name = "google-workspace-mcp"
version = "1"
type = "mcp"
description = "Google Workspace MCP with complete Google Docs and Drive tools"

[mcp]
command = "uvx"
args = [
    "workspace-mcp",
    "--tool-tier",
    "complete",
    "--tools",
    "docs",
    "drive"
]
`
	if err := os.WriteFile(filepath.Join(dir, "metadata.toml"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	zipData, err := utils.CreateZip(dir)
	if err != nil {
		t.Fatal(err)
	}

	meta := BuildMetadata("google-workspace-mcp", "5", asset.TypeMCP, zipData)
	out, err := ApplyMetadata(meta, zipData, true)
	if err != nil {
		t.Fatal(err)
	}
	mcpType := asset.TypeMCP
	if err := metadata.ValidateZip(out, &mcpType); err != nil {
		t.Fatalf("uploaded zip should validate: %v", err)
	}

	raw, err := utils.ReadZipFile(out, "metadata.toml")
	if err != nil {
		t.Fatal(err)
	}
	uploaded, err := metadata.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if uploaded.MCP == nil {
		t.Fatal("uploaded metadata lost the [mcp] section")
	}
	if uploaded.MCP.Command != "uvx" {
		t.Errorf("command = %q, want uvx", uploaded.MCP.Command)
	}
	want := []string{"workspace-mcp", "--tool-tier", "complete", "--tools", "docs", "drive"}
	if !slices.Equal(uploaded.MCP.Args, want) {
		t.Errorf("args = %v, want %v", uploaded.MCP.Args, want)
	}
	if uploaded.Asset.Version != "5" {
		t.Errorf("version = %q, want 5", uploaded.Asset.Version)
	}
}
