package commands

import "testing"

func TestIsRemoteMCPURL(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"https://example.com/mcp", true},
		{"https://api.example.com/v1/mcp", true},
		{"http://localhost:8080/mcp", true},
		{"https://example.com/assets/my-skill.zip", false},     // zip URL
		{"https://example.com/download/asset.ZIP", false},      // zip URL (case insensitive)
		{"https://github.com/org/repo/tree/main/skill", false}, // GitHub tree URL
		{"./my-skill", false},                                  // local path
		{"my-skill", false},                                    // asset name
		{"", false},                                            // empty
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := isRemoteMCPURL(tc.input)
			if got != tc.want {
				t.Errorf("isRemoteMCPURL(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestLooksLikeZipURL(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"https://example.com/asset.zip", true},
		{"https://example.com/asset.ZIP", true},
		{"https://example.com/mcp", false},
		{"https://example.com/mcp/endpoint", false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := looksLikeZipURL(tc.input)
			if got != tc.want {
				t.Errorf("looksLikeZipURL(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestNameFromMCPURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://example.com/mcp", "example-mcp"},
		{"https://api.example.com/v1/mcp", "api-example-v1-mcp"},
		{"https://mcp.example.io", "mcp-example"},
		{"https://example.dev/server", "example-server"},
		{"https://my-service.example.ai/mcp", "my-service-example-mcp"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := nameFromMCPURL(tc.input)
			if got != tc.want {
				t.Errorf("nameFromMCPURL(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
