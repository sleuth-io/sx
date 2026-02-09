package metadata

import (
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
)

// TestMCPMetadataRoundTrip tests that MCP metadata can be parsed and serialized without losing data
func TestMCPMetadataRoundTrip(t *testing.T) {
	originalTOML := `[asset]
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

	// Transport should be normalized to "stdio"
	if meta.MCP.Transport != "stdio" {
		t.Errorf("Transport not normalized: got %q, want %q", meta.MCP.Transport, "stdio")
	}

	// Validate it
	if err := meta.Validate(); err != nil {
		t.Fatalf("Validation failed: %v", err)
	}

	// Check fields are preserved
	if meta.Asset.Name != "test-mcp" {
		t.Errorf("Name not preserved: got %q, want %q", meta.Asset.Name, "test-mcp")
	}

	if meta.Asset.Type != asset.TypeMCP {
		t.Errorf("Type not preserved: got %q, want %q", meta.Asset.Type, asset.TypeMCP)
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

func TestMCPMetadataRoundTrip_SSE(t *testing.T) {
	originalTOML := `[asset]
name = "sse-mcp"
version = "1.0.0"
type = "mcp"

[mcp]
transport = "sse"
url = "https://example.com/mcp/sse"
`

	meta, err := Parse([]byte(originalTOML))
	if err != nil {
		t.Fatalf("Failed to parse metadata: %v", err)
	}

	if err := meta.Validate(); err != nil {
		t.Fatalf("Validation failed: %v", err)
	}

	if meta.MCP.Transport != "sse" {
		t.Errorf("Transport = %q, want %q", meta.MCP.Transport, "sse")
	}
	if meta.MCP.URL != "https://example.com/mcp/sse" {
		t.Errorf("URL = %q, want %q", meta.MCP.URL, "https://example.com/mcp/sse")
	}
	if !meta.MCP.IsRemote() {
		t.Error("IsRemote() should return true for sse")
	}

	// Round-trip
	serialized, err := Marshal(meta)
	if err != nil {
		t.Fatalf("Failed to serialize: %v", err)
	}

	meta2, err := Parse(serialized)
	if err != nil {
		t.Fatalf("Failed to parse serialized: %v", err)
	}

	if err := meta2.Validate(); err != nil {
		t.Fatalf("Validation failed after round-trip: %v", err)
	}

	if meta2.MCP.Transport != "sse" {
		t.Errorf("Transport after round-trip = %q, want %q", meta2.MCP.Transport, "sse")
	}
	if meta2.MCP.URL != "https://example.com/mcp/sse" {
		t.Errorf("URL after round-trip = %q, want %q", meta2.MCP.URL, "https://example.com/mcp/sse")
	}
}

func TestMCPMetadataRoundTrip_HTTP(t *testing.T) {
	originalTOML := `[asset]
name = "http-mcp"
version = "1.0.0"
type = "mcp"

[mcp]
transport = "http"
url = "https://example.com/mcp"
`

	meta, err := Parse([]byte(originalTOML))
	if err != nil {
		t.Fatalf("Failed to parse metadata: %v", err)
	}

	if err := meta.Validate(); err != nil {
		t.Fatalf("Validation failed: %v", err)
	}

	if meta.MCP.Transport != "http" {
		t.Errorf("Transport = %q, want %q", meta.MCP.Transport, "http")
	}
	if meta.MCP.URL != "https://example.com/mcp" {
		t.Errorf("URL = %q, want %q", meta.MCP.URL, "https://example.com/mcp")
	}
	if !meta.MCP.IsRemote() {
		t.Error("IsRemote() should return true for http")
	}

	// Round-trip
	serialized, err := Marshal(meta)
	if err != nil {
		t.Fatalf("Failed to serialize: %v", err)
	}

	meta2, err := Parse(serialized)
	if err != nil {
		t.Fatalf("Failed to parse serialized: %v", err)
	}

	if err := meta2.Validate(); err != nil {
		t.Fatalf("Validation failed after round-trip: %v", err)
	}

	if meta2.MCP.Transport != "http" {
		t.Errorf("Transport after round-trip = %q, want %q", meta2.MCP.Transport, "http")
	}
	if meta2.MCP.URL != "https://example.com/mcp" {
		t.Errorf("URL after round-trip = %q, want %q", meta2.MCP.URL, "https://example.com/mcp")
	}
}
