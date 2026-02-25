package github

import (
	"testing"
)

func TestParseTreeURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected *TreeURL
	}{
		{
			name: "full path with branch",
			url:  "https://github.com/metabase/metabase/tree/master/.claude/skills/docs-write",
			expected: &TreeURL{
				Owner: "metabase",
				Repo:  "metabase",
				Ref:   "master",
				Path:  ".claude/skills/docs-write",
			},
		},
		{
			name: "with trailing slash",
			url:  "https://github.com/owner/repo/tree/main/path/to/skill/",
			expected: &TreeURL{
				Owner: "owner",
				Repo:  "repo",
				Ref:   "main",
				Path:  "path/to/skill",
			},
		},
		{
			name: "root of repo",
			url:  "https://github.com/anthropics/skills/tree/main",
			expected: &TreeURL{
				Owner: "anthropics",
				Repo:  "skills",
				Ref:   "main",
				Path:  "",
			},
		},
		{
			name: "with tag ref",
			url:  "https://github.com/owner/repo/tree/v1.0.0/skills/my-skill",
			expected: &TreeURL{
				Owner: "owner",
				Repo:  "repo",
				Ref:   "v1.0.0",
				Path:  "skills/my-skill",
			},
		},
		{
			name: "with commit sha",
			url:  "https://github.com/owner/repo/tree/abc123def/path",
			expected: &TreeURL{
				Owner: "owner",
				Repo:  "repo",
				Ref:   "abc123def",
				Path:  "path",
			},
		},
		{
			name:     "blob URL (not tree)",
			url:      "https://github.com/owner/repo/blob/main/file.txt",
			expected: nil,
		},
		{
			name:     "repo root without tree",
			url:      "https://github.com/owner/repo",
			expected: nil,
		},
		{
			name:     "not github",
			url:      "https://gitlab.com/owner/repo/tree/main/path",
			expected: nil,
		},
		{
			name: "http (not https)",
			url:  "http://github.com/owner/repo/tree/main/path",
			expected: &TreeURL{
				Owner: "owner",
				Repo:  "repo",
				Ref:   "main",
				Path:  "path",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseTreeURL(tt.url)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %+v", result)
				}
				return
			}

			if result == nil {
				t.Fatalf("expected %+v, got nil", tt.expected)
				return // Make staticcheck happy
			}

			if result.Owner != tt.expected.Owner {
				t.Errorf("Owner: expected %q, got %q", tt.expected.Owner, result.Owner)
			}
			if result.Repo != tt.expected.Repo {
				t.Errorf("Repo: expected %q, got %q", tt.expected.Repo, result.Repo)
			}
			if result.Ref != tt.expected.Ref {
				t.Errorf("Ref: expected %q, got %q", tt.expected.Ref, result.Ref)
			}
			if result.Path != tt.expected.Path {
				t.Errorf("Path: expected %q, got %q", tt.expected.Path, result.Path)
			}
		})
	}
}

func TestIsTreeURL(t *testing.T) {
	tests := []struct {
		url      string
		expected bool
	}{
		{"https://github.com/owner/repo/tree/main/path", true},
		{"https://github.com/owner/repo/tree/main", true},
		{"https://github.com/owner/repo/blob/main/file.txt", false},
		{"https://github.com/owner/repo", false},
		{"https://example.com/file.zip", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			result := IsTreeURL(tt.url)
			if result != tt.expected {
				t.Errorf("IsTreeURL(%q) = %v, want %v", tt.url, result, tt.expected)
			}
		})
	}
}

func TestIsBlobURL(t *testing.T) {
	tests := []struct {
		url      string
		expected bool
	}{
		{"https://github.com/owner/repo/blob/main/file.txt", true},
		{"https://github.com/owner/repo/blob/main/path/to/file.go", true},
		{"https://github.com/owner/repo/tree/main/path", false},
		{"https://github.com/owner/repo", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			result := IsBlobURL(tt.url)
			if result != tt.expected {
				t.Errorf("IsBlobURL(%q) = %v, want %v", tt.url, result, tt.expected)
			}
		})
	}
}

func TestTreeURL_ContentsAPIURL(t *testing.T) {
	tests := []struct {
		name     string
		treeURL  TreeURL
		expected string
	}{
		{
			name: "with path",
			treeURL: TreeURL{
				Owner: "metabase",
				Repo:  "metabase",
				Ref:   "master",
				Path:  ".claude/skills/docs-write",
			},
			expected: "https://api.github.com/repos/metabase/metabase/contents/.claude/skills/docs-write?ref=master",
		},
		{
			name: "root (no path)",
			treeURL: TreeURL{
				Owner: "anthropics",
				Repo:  "skills",
				Ref:   "main",
				Path:  "",
			},
			expected: "https://api.github.com/repos/anthropics/skills/contents?ref=main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.treeURL.ContentsAPIURL()
			if result != tt.expected {
				t.Errorf("ContentsAPIURL() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestTreeURL_RawURL(t *testing.T) {
	treeURL := TreeURL{
		Owner: "metabase",
		Repo:  "metabase",
		Ref:   "master",
		Path:  ".claude/skills/docs-write",
	}

	result := treeURL.RawURL("SKILL.md")
	expected := "https://raw.githubusercontent.com/metabase/metabase/master/.claude/skills/docs-write/SKILL.md"

	if result != expected {
		t.Errorf("RawURL() = %q, want %q", result, expected)
	}
}

func TestTreeURL_SkillName(t *testing.T) {
	tests := []struct {
		name     string
		treeURL  TreeURL
		expected string
	}{
		{
			name: "nested path",
			treeURL: TreeURL{
				Owner: "metabase",
				Repo:  "metabase",
				Ref:   "master",
				Path:  ".claude/skills/docs-write",
			},
			expected: "docs-write",
		},
		{
			name: "single path component",
			treeURL: TreeURL{
				Owner: "owner",
				Repo:  "repo",
				Ref:   "main",
				Path:  "my-skill",
			},
			expected: "my-skill",
		},
		{
			name: "empty path (use repo name)",
			treeURL: TreeURL{
				Owner: "anthropics",
				Repo:  "skills",
				Ref:   "main",
				Path:  "",
			},
			expected: "skills",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.treeURL.SkillName()
			if result != tt.expected {
				t.Errorf("SkillName() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestTreeURL_String(t *testing.T) {
	tests := []struct {
		name     string
		treeURL  TreeURL
		expected string
	}{
		{
			name: "with path",
			treeURL: TreeURL{
				Owner: "metabase",
				Repo:  "metabase",
				Ref:   "master",
				Path:  ".claude/skills/docs-write",
			},
			expected: "https://github.com/metabase/metabase/tree/master/.claude/skills/docs-write",
		},
		{
			name: "without path",
			treeURL: TreeURL{
				Owner: "anthropics",
				Repo:  "skills",
				Ref:   "main",
				Path:  "",
			},
			expected: "https://github.com/anthropics/skills/tree/main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.treeURL.String()
			if result != tt.expected {
				t.Errorf("String() = %q, want %q", result, tt.expected)
			}
		})
	}
}
