package commands

import "testing"

func TestIsSkillsShReference(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// Valid references
		{"anthropics/skills", true},
		{"vercel-labs/agent-skills", true},
		{"org/repo/skill-name", true},
		{"my-org/my-repo/my-skill", true},
		{"owner123/repo456", true},
		{"a/b", true},
		{"owner/repo.name", true},
		{"owner_name/repo_name", true},

		// Invalid: not owner/repo format
		{"just-a-name", false},
		{"", false},
		{"./relative/path", false},
		{"/absolute/path", false},
		{"~/home/path", false},
		{"https://example.com", false},
		{"http://example.com", false},

		// Invalid: starts with special chars
		{"-owner/repo", false},
		{"_owner/repo", false},
		{".owner/repo", false},

		// Invalid: empty segments
		{"/repo", false},
		{"owner/", false},
		{"owner//skill", false},

		// Invalid: too many segments
		{"a/b/c/d", false},

		// Invalid: special characters
		{"owner/repo/skill name", false},
		{"owner/repo@tag", false},

		// Should not match URLs
		{"https://github.com/owner/repo", false},

		// Should not match git references
		{"git+https://github.com/user/repo.git@main", false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := isSkillsShReference(tc.input)
			if got != tc.want {
				t.Errorf("isSkillsShReference(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseSkillsShReference(t *testing.T) {
	tests := []struct {
		input     string
		wantOwner string
		wantRepo  string
		wantSkill string
		wantOK    bool
	}{
		// Valid: owner/repo
		{"anthropics/skills", "anthropics", "skills", "", true},
		{"vercel-labs/agent-skills", "vercel-labs", "agent-skills", "", true},

		// Valid: owner/repo/skill
		{"anthropics/skills/frontend-design", "anthropics", "skills", "frontend-design", true},
		{"org/repo/my-skill", "org", "repo", "my-skill", true},

		// Valid: with skills.sh: prefix
		{"skills.sh:anthropics/skills", "anthropics", "skills", "", true},
		{"skills.sh:anthropics/skills/frontend-design", "anthropics", "skills", "frontend-design", true},

		// Invalid
		{"just-a-name", "", "", "", false},
		{"", "", "", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			owner, repo, skill, ok := parseSkillsShReference(tc.input)
			if ok != tc.wantOK {
				t.Fatalf("parseSkillsShReference(%q) ok = %v, want %v", tc.input, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if owner != tc.wantOwner {
				t.Errorf("owner = %q, want %q", owner, tc.wantOwner)
			}
			if repo != tc.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tc.wantRepo)
			}
			if skill != tc.wantSkill {
				t.Errorf("skill = %q, want %q", skill, tc.wantSkill)
			}
		})
	}
}

func TestSkillsShToTreeURL(t *testing.T) {
	tests := []struct {
		name   string
		owner  string
		repo   string
		skill  string
		branch string
		want   string
	}{
		{
			name:   "specific skill on main",
			owner:  "anthropics",
			repo:   "skills",
			skill:  "frontend-design",
			branch: "main",
			want:   "https://github.com/anthropics/skills/tree/main/skills/frontend-design",
		},
		{
			name:   "whole repo on main",
			owner:  "vercel-labs",
			repo:   "agent-skills",
			skill:  "",
			branch: "main",
			want:   "https://github.com/vercel-labs/agent-skills/tree/main",
		},
		{
			name:   "specific skill on master",
			owner:  "my-org",
			repo:   "my-repo",
			skill:  "my-skill",
			branch: "master",
			want:   "https://github.com/my-org/my-repo/tree/master/skills/my-skill",
		},
		{
			name:   "whole repo on custom branch",
			owner:  "org",
			repo:   "repo",
			skill:  "",
			branch: "develop",
			want:   "https://github.com/org/repo/tree/develop",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := skillsShToTreeURL(tc.owner, tc.repo, tc.skill, tc.branch)
			if got != tc.want {
				t.Errorf("skillsShToTreeURL(%q, %q, %q, %q) = %q, want %q",
					tc.owner, tc.repo, tc.skill, tc.branch, got, tc.want)
			}
		})
	}
}

func TestRepoAssetToGitHubURL(t *testing.T) {
	tests := []struct {
		name     string
		owner    string
		repo     string
		assetDir string
		branch   string
		want     string
	}{
		{
			name:     "agent directory uses tree URL",
			owner:    "obra",
			repo:     "superpowers",
			assetDir: "agents/code-reviewer",
			branch:   "main",
			want:     "https://github.com/obra/superpowers/tree/main/agents/code-reviewer",
		},
		{
			name:     "single .md file uses blob URL",
			owner:    "obra",
			repo:     "superpowers",
			assetDir: "agents/code-reviewer.md",
			branch:   "main",
			want:     "https://github.com/obra/superpowers/blob/main/agents/code-reviewer.md",
		},
		{
			name:     "skill in skills dir",
			owner:    "anthropics",
			repo:     "skills",
			assetDir: "skills/frontend-design",
			branch:   "main",
			want:     "https://github.com/anthropics/skills/tree/main/skills/frontend-design",
		},
		{
			name:     "command asset",
			owner:    "org",
			repo:     "repo",
			assetDir: "commands/deploy",
			branch:   "master",
			want:     "https://github.com/org/repo/tree/master/commands/deploy",
		},
		{
			name:     "empty dir returns repo root",
			owner:    "org",
			repo:     "repo",
			assetDir: "",
			branch:   "main",
			want:     "https://github.com/org/repo/tree/main",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := repoAssetToGitHubURL(tc.owner, tc.repo, tc.assetDir, tc.branch)
			if got != tc.want {
				t.Errorf("repoAssetToGitHubURL(%q, %q, %q, %q) = %q, want %q",
					tc.owner, tc.repo, tc.assetDir, tc.branch, got, tc.want)
			}
		})
	}
}

func TestSkillsShDoesNotConflictWithOtherInputTypes(t *testing.T) {
	// These inputs should NOT be detected as skills.sh references
	// because they're handled by other code paths
	nonSkillsShInputs := []string{
		"plugin@marketplace", // marketplace reference
		"https://github.com/owner/repo/tree/main/skills/my-skill", // GitHub tree URL
		"https://example.com/skill.zip",                           // HTTP URL
		"./local/path",                                            // local path
		"/absolute/path",                                          // absolute path
		"~/home/path",                                             // home path
		"my-asset-name",                                           // registry asset name
		"git@github.com:user/repo",                                // git SSH URL
	}

	for _, input := range nonSkillsShInputs {
		t.Run(input, func(t *testing.T) {
			if isSkillsShReference(input) {
				t.Errorf("isSkillsShReference(%q) = true, should be false (conflicts with other input type)", input)
			}
		})
	}
}
