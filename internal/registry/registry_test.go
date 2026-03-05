package registry

import (
	"testing"
)

func TestSkillFormatInstalls(t *testing.T) {
	tests := []struct {
		installs int
		want     string
	}{
		{0, "0"},
		{500, "500"},
		{999, "999"},
		{1000, "1.0K"},
		{1500, "1.5K"},
		{12345, "12.3K"},
		{123897, "123.9K"},
		{999999, "1000.0K"},
		{1000000, "1.0M"},
		{1500000, "1.5M"},
		{10000000, "10.0M"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			s := Skill{Installs: tc.installs}
			got := s.FormatInstalls()
			if got != tc.want {
				t.Errorf("FormatInstalls() for %d = %q, want %q", tc.installs, got, tc.want)
			}
		})
	}
}

func TestSkillTreeURL(t *testing.T) {
	s := Skill{Source: "anthropics/skills", SkillID: "frontend-design"}
	got := s.TreeURL("main")
	want := "https://github.com/anthropics/skills/tree/main/skills/frontend-design"
	if got != want {
		t.Errorf("TreeURL() = %q, want %q", got, want)
	}

	got = s.TreeURL("master")
	want = "https://github.com/anthropics/skills/tree/master/skills/frontend-design"
	if got != want {
		t.Errorf("TreeURL(master) = %q, want %q", got, want)
	}
}

func TestParseSkillsResponse(t *testing.T) {
	// Simulates the RSC payload format from skills.sh
	body := `1:"$Sreact.fragment"
14:["$","$L1c",null,{"initialSkills":[{"source":"vercel-labs/skills","skillId":"find-skills","name":"find-skills","installs":417896},{"source":"anthropics/skills","skillId":"frontend-design","name":"frontend-design","installs":123897},{"source":"microsoft/copilot","skillId":"azure-deploy","name":"azure-deploy","installs":110422}]}]`

	skills, err := ParseSkillsResponse(body)
	if err != nil {
		t.Fatalf("ParseSkillsResponse() error: %v", err)
	}

	if len(skills) != 3 {
		t.Fatalf("expected 3 skills, got %d", len(skills))
	}

	// Verify first skill
	if skills[0].Source != "vercel-labs/skills" {
		t.Errorf("skills[0].Source = %q, want %q", skills[0].Source, "vercel-labs/skills")
	}
	if skills[0].SkillID != "find-skills" {
		t.Errorf("skills[0].SkillID = %q, want %q", skills[0].SkillID, "find-skills")
	}
	if skills[0].Name != "find-skills" {
		t.Errorf("skills[0].Name = %q, want %q", skills[0].Name, "find-skills")
	}
	if skills[0].Installs != 417896 {
		t.Errorf("skills[0].Installs = %d, want %d", skills[0].Installs, 417896)
	}

	// Verify ordering preserved
	if skills[1].SkillID != "frontend-design" {
		t.Errorf("skills[1].SkillID = %q, want %q", skills[1].SkillID, "frontend-design")
	}
	if skills[2].SkillID != "azure-deploy" {
		t.Errorf("skills[2].SkillID = %q, want %q", skills[2].SkillID, "azure-deploy")
	}
}

func TestParseSkillsResponseEmpty(t *testing.T) {
	_, err := ParseSkillsResponse("no skills data here")
	if err == nil {
		t.Error("expected error for empty response, got nil")
	}
}

func TestParseSkillsResponseEscaped(t *testing.T) {
	// The HTML page version has escaped JSON (double backslash quotes)
	// Our RSC endpoint returns clean JSON, but verify the regex handles it
	body := `{"source":"org/repo","skillId":"my-skill","name":"my-skill","installs":42}`
	skills, err := ParseSkillsResponse(body)
	if err != nil {
		t.Fatalf("ParseSkillsResponse() error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Installs != 42 {
		t.Errorf("Installs = %d, want 42", skills[0].Installs)
	}
}

func TestSearch(t *testing.T) {
	skills := []Skill{
		{Source: "anthropics/skills", SkillID: "frontend-design", Name: "frontend-design", Installs: 100},
		{Source: "vercel-labs/agent-skills", SkillID: "react-best-practices", Name: "react-best-practices", Installs: 200},
		{Source: "microsoft/copilot", SkillID: "azure-deploy", Name: "azure-deploy", Installs: 300},
		{Source: "some-org/python-tools", SkillID: "python-linting", Name: "python-linting", Installs: 50},
	}

	tests := []struct {
		query string
		want  int
	}{
		{"", 4},             // empty query returns all
		{"frontend", 1},     // matches name
		{"react", 1},        // matches name
		{"azure", 1},        // matches name
		{"python", 1},       // matches name and source
		{"anthropics", 1},   // matches source only
		{"vercel", 1},       // matches source only
		{"microsoft", 1},    // matches source only
		{"nonexistent", 0},  // no matches
		{"FRONTEND", 1},     // case insensitive
		{"deploy", 1},       // partial match
	}

	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			results := Search(skills, tc.query)
			if len(results) != tc.want {
				t.Errorf("Search(%q) returned %d results, want %d", tc.query, len(results), tc.want)
			}
		})
	}
}
