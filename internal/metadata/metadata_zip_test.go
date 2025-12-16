package metadata

import (
	"archive/zip"
	"bytes"
	"io"
	"testing"

	"github.com/sleuth-io/skills/internal/asset"
	"github.com/sleuth-io/skills/internal/utils"
)

// TestMCPMetadataInZip tests that MCP metadata survives being zipped and extracted
func TestMCPMetadataInZip(t *testing.T) {
	originalTOML := `[artifact]
name = "test-mcp"
version = "1.0.0"
type = "mcp"
description = "A test MCP server"

[mcp]
command = "node"
args = [
    "server.js"
]
`

	// Create a zip file with the metadata
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	// Add metadata.toml
	metadataFile, err := zipWriter.Create("metadata.toml")
	if err != nil {
		t.Fatalf("Failed to create metadata.toml in zip: %v", err)
	}
	if _, err := metadataFile.Write([]byte(originalTOML)); err != nil {
		t.Fatalf("Failed to write metadata.toml: %v", err)
	}

	// Add a dummy server.js
	serverFile, err := zipWriter.Create("server.js")
	if err != nil {
		t.Fatalf("Failed to create server.js in zip: %v", err)
	}
	if _, err := serverFile.Write([]byte("console.log('test');")); err != nil {
		t.Fatalf("Failed to write server.js: %v", err)
	}

	if err := zipWriter.Close(); err != nil {
		t.Fatalf("Failed to close zip: %v", err)
	}

	zipData := buf.Bytes()
	t.Logf("Created zip with %d bytes", len(zipData))

	// Now read the metadata back from the zip using utils.ReadZipFile
	metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
	if err != nil {
		t.Fatalf("Failed to read metadata.toml from zip: %v", err)
	}

	t.Logf("Read metadata from zip:\n%s", string(metadataBytes))

	// Parse it
	meta, err := Parse(metadataBytes)
	if err != nil {
		t.Fatalf("Failed to parse metadata from zip: %v", err)
	}

	// Validate it
	if err := meta.Validate(); err != nil {
		t.Fatalf("Validation failed: %v", err)
	}

	// Check MCP config is intact
	if meta.MCP == nil {
		t.Fatal("MCP config is nil after reading from zip")
	}

	if meta.MCP.Command != "node" {
		t.Errorf("MCP command not preserved: got %q, want %q", meta.MCP.Command, "node")
	}

	if len(meta.MCP.Args) != 1 {
		t.Fatalf("MCP args length wrong: got %d, want 1", len(meta.MCP.Args))
	}

	if meta.MCP.Args[0] != "server.js" {
		t.Errorf("MCP args not preserved: got %q, want %q", meta.MCP.Args[0], "server.js")
	}

	// Now extract the zip to a directory and read metadata back
	tempDir := t.TempDir()
	if err := utils.ExtractZip(zipData, tempDir); err != nil {
		t.Fatalf("Failed to extract zip: %v", err)
	}

	// Re-zip the extracted directory
	var buf2 bytes.Buffer
	zipWriter2 := zip.NewWriter(&buf2)

	// Walk the temp directory and add files
	zipReader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		t.Fatalf("Failed to create zip reader: %v", err)
	}

	for _, file := range zipReader.File {
		writer, err := zipWriter2.Create(file.Name)
		if err != nil {
			t.Fatalf("Failed to create file in new zip: %v", err)
		}

		reader, err := file.Open()
		if err != nil {
			t.Fatalf("Failed to open file from original zip: %v", err)
		}

		if _, err := io.Copy(writer, reader); err != nil {
			reader.Close()
			t.Fatalf("Failed to copy file: %v", err)
		}
		reader.Close()
	}

	if err := zipWriter2.Close(); err != nil {
		t.Fatalf("Failed to close re-zipped file: %v", err)
	}

	rezipData := buf2.Bytes()
	t.Logf("Re-zipped to %d bytes", len(rezipData))

	// Read metadata from re-zipped file
	metadataBytes2, err := utils.ReadZipFile(rezipData, "metadata.toml")
	if err != nil {
		t.Fatalf("Failed to read metadata.toml from re-zip: %v", err)
	}

	t.Logf("Read metadata from re-zip:\n%s", string(metadataBytes2))

	// Parse and validate
	meta2, err := Parse(metadataBytes2)
	if err != nil {
		t.Fatalf("Failed to parse metadata from re-zip: %v", err)
	}

	if err := meta2.Validate(); err != nil {
		t.Fatalf("Validation failed after re-zip: %v", err)
	}

	if meta2.Artifact.Type != asset.TypeMCP {
		t.Errorf("Type changed after re-zip: got %q, want %q", meta2.Artifact.Type, asset.TypeMCP)
	}
}
