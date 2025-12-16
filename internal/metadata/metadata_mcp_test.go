package metadata

import (
	"testing"

	"github.com/sleuth-io/skills/internal/asset"
)

// TestMCPMetadataRoundTrip tests that MCP metadata can be parsed and serialized without losing data
func TestMCPMetadataRoundTrip(t *testing.T) {
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

	// Parse the metadata
	meta, err := Parse([]byte(originalTOML))
	if err != nil {
		t.Fatalf("Failed to parse metadata: %v", err)
	}

	// Validate it
	if err := meta.Validate(); err != nil {
		t.Fatalf("Validation failed: %v", err)
	}

	// Check fields are preserved
	if meta.Artifact.Name != "test-mcp" {
		t.Errorf("Name not preserved: got %q, want %q", meta.Artifact.Name, "test-mcp")
	}

	if meta.Artifact.Type != asset.TypeMCP {
		t.Errorf("Type not preserved: got %q, want %q", meta.Artifact.Type, asset.TypeMCP)
	}

	if meta.MCP == nil {
		t.Fatal("MCP config is nil after parsing")
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

	// Now serialize it back and parse again
	serialized, err := Marshal(meta)
	if err != nil {
		t.Fatalf("Failed to serialize metadata: %v", err)
	}

	t.Logf("Serialized metadata:\n%s", string(serialized))

	// Parse the serialized version
	meta2, err := Parse(serialized)
	if err != nil {
		t.Fatalf("Failed to parse serialized metadata: %v", err)
	}

	// Validate the round-tripped version
	if err := meta2.Validate(); err != nil {
		t.Fatalf("Validation failed after round-trip: %v", err)
	}

	// Check all fields are still preserved
	if meta2.MCP == nil {
		t.Fatal("MCP config is nil after round-trip")
	}

	if meta2.MCP.Command != "node" {
		t.Errorf("MCP command not preserved after round-trip: got %q, want %q", meta2.MCP.Command, "node")
	}

	if len(meta2.MCP.Args) != 1 {
		t.Fatalf("MCP args length wrong after round-trip: got %d, want 1", len(meta2.MCP.Args))
	}

	if meta2.MCP.Args[0] != "server.js" {
		t.Errorf("MCP args not preserved after round-trip: got %q, want %q", meta2.MCP.Args[0], "server.js")
	}
}
