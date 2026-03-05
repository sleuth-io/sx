package requirements

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseLineSkillsSh(t *testing.T) {
	tests := []struct {
		name              string
		line              string
		wantType          RequirementType
		wantOwnerRepo     string
		wantSkillName     string
		wantErr           bool
	}{
		{
			name:          "specific skill",
			line:          "skills.sh:anthropics/skills/frontend-design",
			wantType:      RequirementTypeSkillsSh,
			wantOwnerRepo: "anthropics/skills",
			wantSkillName: "frontend-design",
		},
		{
			name:          "whole repo",
			line:          "skills.sh:vercel-labs/agent-skills",
			wantType:      RequirementTypeSkillsSh,
			wantOwnerRepo: "vercel-labs/agent-skills",
			wantSkillName: "",
		},
		{
			name:          "with whitespace",
			line:          "  skills.sh:org/repo/my-skill  ",
			wantType:      RequirementTypeSkillsSh,
			wantOwnerRepo: "org/repo",
			wantSkillName: "my-skill",
		},
		{
			name:    "missing owner/repo",
			line:    "skills.sh:",
			wantErr: true,
		},
		{
			name:    "only owner",
			line:    "skills.sh:owner",
			wantErr: true,
		},
		{
			name:    "empty owner",
			line:    "skills.sh:/repo",
			wantErr: true,
		},
		{
			name:    "empty repo",
			line:    "skills.sh:owner/",
			wantErr: true,
		},
		{
			name:    "empty skill name (trailing slash)",
			line:    "skills.sh:owner/repo/",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, err := ParseLine(tc.line)
			if tc.wantErr {
				if err == nil {
					t.Errorf("ParseLine(%q) expected error, got nil", tc.line)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseLine(%q) unexpected error: %v", tc.line, err)
			}
			if req.Type != tc.wantType {
				t.Errorf("Type = %q, want %q", req.Type, tc.wantType)
			}
			if req.SkillsShOwnerRepo != tc.wantOwnerRepo {
				t.Errorf("SkillsShOwnerRepo = %q, want %q", req.SkillsShOwnerRepo, tc.wantOwnerRepo)
			}
			if req.SkillsShSkillName != tc.wantSkillName {
				t.Errorf("SkillsShSkillName = %q, want %q", req.SkillsShSkillName, tc.wantSkillName)
			}
		})
	}
}

func TestSkillsShRequirementString(t *testing.T) {
	tests := []struct {
		name string
		req  Requirement
		want string
	}{
		{
			name: "with skill name",
			req: Requirement{
				Type:              RequirementTypeSkillsSh,
				SkillsShOwnerRepo: "anthropics/skills",
				SkillsShSkillName: "frontend-design",
			},
			want: "skills.sh:anthropics/skills/frontend-design",
		},
		{
			name: "whole repo",
			req: Requirement{
				Type:              RequirementTypeSkillsSh,
				SkillsShOwnerRepo: "vercel-labs/agent-skills",
			},
			want: "skills.sh:vercel-labs/agent-skills",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.req.String()
			if got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseLineRoundTrip(t *testing.T) {
	lines := []string{
		"skills.sh:anthropics/skills/frontend-design",
		"skills.sh:vercel-labs/agent-skills",
		"skills.sh:org/repo/my-skill",
	}

	for _, line := range lines {
		t.Run(line, func(t *testing.T) {
			req, err := ParseLine(line)
			if err != nil {
				t.Fatalf("ParseLine(%q) error: %v", line, err)
			}
			got := req.String()
			if got != line {
				t.Errorf("round-trip failed: %q -> %q", line, got)
			}
		})
	}
}

func TestParseSkillsShFromFile(t *testing.T) {
	content := `# Skills from skills.sh
skills.sh:anthropics/skills/frontend-design
skills.sh:vercel-labs/agent-skills

# Registry asset
github-mcp==1.2.3
`
	dir := t.TempDir()
	path := filepath.Join(dir, "sx.txt")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	reqs, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if len(reqs) != 3 {
		t.Fatalf("expected 3 requirements, got %d", len(reqs))
	}

	// First: specific skill
	if reqs[0].Type != RequirementTypeSkillsSh {
		t.Errorf("reqs[0].Type = %q, want %q", reqs[0].Type, RequirementTypeSkillsSh)
	}
	if reqs[0].SkillsShOwnerRepo != "anthropics/skills" {
		t.Errorf("reqs[0].SkillsShOwnerRepo = %q, want %q", reqs[0].SkillsShOwnerRepo, "anthropics/skills")
	}
	if reqs[0].SkillsShSkillName != "frontend-design" {
		t.Errorf("reqs[0].SkillsShSkillName = %q, want %q", reqs[0].SkillsShSkillName, "frontend-design")
	}

	// Second: whole repo
	if reqs[1].Type != RequirementTypeSkillsSh {
		t.Errorf("reqs[1].Type = %q, want %q", reqs[1].Type, RequirementTypeSkillsSh)
	}
	if reqs[1].SkillsShOwnerRepo != "vercel-labs/agent-skills" {
		t.Errorf("reqs[1].SkillsShOwnerRepo = %q, want %q", reqs[1].SkillsShOwnerRepo, "vercel-labs/agent-skills")
	}
	if reqs[1].SkillsShSkillName != "" {
		t.Errorf("reqs[1].SkillsShSkillName = %q, want empty", reqs[1].SkillsShSkillName)
	}

	// Third: registry
	if reqs[2].Type != RequirementTypeRegistry {
		t.Errorf("reqs[2].Type = %q, want %q", reqs[2].Type, RequirementTypeRegistry)
	}
}

func TestParseLineDoesNotConflictWithOtherTypes(t *testing.T) {
	tests := []struct {
		line     string
		wantType RequirementType
	}{
		{"git+https://github.com/user/repo.git@main#name=foo", RequirementTypeGit},
		{"https://example.com/skill.zip", RequirementTypeHTTP},
		{"./local/path.zip", RequirementTypePath},
		{"/absolute/path.zip", RequirementTypePath},
		{"~/home/path.zip", RequirementTypePath},
		{"github-mcp==1.2.3", RequirementTypeRegistry},
		{"awesome-skill", RequirementTypeRegistry},
		{"skills.sh:owner/repo", RequirementTypeSkillsSh},
	}

	for _, tc := range tests {
		t.Run(tc.line, func(t *testing.T) {
			req, err := ParseLine(tc.line)
			if err != nil {
				t.Fatalf("ParseLine(%q) error: %v", tc.line, err)
			}
			if req.Type != tc.wantType {
				t.Errorf("ParseLine(%q).Type = %q, want %q", tc.line, req.Type, tc.wantType)
			}
		})
	}
}
