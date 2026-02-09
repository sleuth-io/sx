package asset

import (
	"testing"
)

func TestFromString_MCP(t *testing.T) {
	got := FromString("mcp")
	if got != TypeMCP {
		t.Errorf("FromString(\"mcp\") = %v, want TypeMCP", got)
	}
}

func TestFromString_MCPRemote_ReturnsTypeMCP(t *testing.T) {
	got := FromString("mcp-remote")
	if got != TypeMCP {
		t.Errorf("FromString(\"mcp-remote\") = %v, want TypeMCP", got)
	}
}

func TestFromString_MCPRemote_EqualsFromStringMCP(t *testing.T) {
	mcp := FromString("mcp")
	mcpRemote := FromString("mcp-remote")
	if mcp != mcpRemote {
		t.Errorf("FromString(\"mcp\") != FromString(\"mcp-remote\"): %v vs %v", mcp, mcpRemote)
	}
}

func TestTypeMCPRemote_IsAlias(t *testing.T) {
	if TypeMCPRemote != TypeMCP {
		t.Errorf("TypeMCPRemote should be alias for TypeMCP")
	}
}

func TestTypeMCPRemote_IsValid(t *testing.T) {
	if !TypeMCPRemote.IsValid() {
		t.Errorf("TypeMCPRemote.IsValid() = false, want true")
	}
}

func TestAllTypes_DoesNotContainDuplicateMCP(t *testing.T) {
	types := AllTypes()
	mcpCount := 0
	for _, typ := range types {
		if typ.Key == "mcp" {
			mcpCount++
		}
	}
	if mcpCount != 1 {
		t.Errorf("AllTypes() contains %d mcp entries, want 1", mcpCount)
	}
}

func TestAllTypes_DoesNotContainMCPRemote(t *testing.T) {
	types := AllTypes()
	for _, typ := range types {
		if typ.Key == "mcp-remote" {
			t.Errorf("AllTypes() should not contain mcp-remote as a separate entry")
		}
	}
}

func TestFromString_UnknownType(t *testing.T) {
	got := FromString("unknown-type")
	if got.Key != "unknown-type" {
		t.Errorf("FromString(\"unknown-type\").Key = %q, want \"unknown-type\"", got.Key)
	}
	if got.IsValid() {
		t.Errorf("Unknown type should not be valid")
	}
}

func TestType_String(t *testing.T) {
	tests := []struct {
		typ  Type
		want string
	}{
		{TypeMCP, "mcp"},
		{TypeMCPRemote, "mcp"}, // alias should also produce "mcp"
		{TypeSkill, "skill"},
		{TypeHook, "hook"},
	}
	for _, tt := range tests {
		if got := tt.typ.String(); got != tt.want {
			t.Errorf("Type.String() = %q, want %q", got, tt.want)
		}
	}
}

func TestType_UnmarshalText_MCPRemote(t *testing.T) {
	var typ Type
	if err := typ.UnmarshalText([]byte("mcp-remote")); err != nil {
		t.Fatalf("UnmarshalText failed: %v", err)
	}
	if typ != TypeMCP {
		t.Errorf("UnmarshalText(\"mcp-remote\") = %v, want TypeMCP", typ)
	}
}
