package commands

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/lockfile"
)

func TestFilterAssetsByType(t *testing.T) {
	assets := []lockfile.Asset{
		{Name: "skill-1", Version: "1.0.0", Type: asset.TypeSkill},
		{Name: "skill-2", Version: "1.0.0", Type: asset.TypeSkill},
		{Name: "mcp-1", Version: "1.0.0", Type: asset.TypeMCP},
		{Name: "hook-1", Version: "1.0.0", Type: asset.TypeHook},
	}

	tests := []struct {
		name       string
		typeFilter string
		wantCount  int
		wantNames  []string
	}{
		{
			name:       "no filter returns all",
			typeFilter: "",
			wantCount:  4,
		},
		{
			name:       "filter by skill",
			typeFilter: "skill",
			wantCount:  2,
			wantNames:  []string{"skill-1", "skill-2"},
		},
		{
			name:       "filter by mcp",
			typeFilter: "mcp",
			wantCount:  1,
			wantNames:  []string{"mcp-1"},
		},
		{
			name:       "filter by nonexistent type",
			typeFilter: "nonexistent",
			wantCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered := filterAssetsByType(assets, tt.typeFilter)
			if len(filtered) != tt.wantCount {
				t.Errorf("filterAssetsByType() returned %d assets, want %d", len(filtered), tt.wantCount)
			}

			if tt.wantNames != nil {
				for i, name := range tt.wantNames {
					if filtered[i].Name != name {
						t.Errorf("filtered[%d].Name = %s, want %s", i, filtered[i].Name, name)
					}
				}
			}
		})
	}
}

func TestListCommandInvalidType(t *testing.T) {
	cmd := NewListCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	cmd.SetArgs([]string{"--type=invalid"})
	err := cmd.Execute()

	if err == nil {
		t.Error("expected error for invalid type, got nil")
	}

	expectedMsg := `invalid asset type "invalid"`
	if err != nil && !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("error message should contain %q, got: %s", expectedMsg, err.Error())
	}
}

func TestListCommandValidTypes(t *testing.T) {
	validTypes := []string{"skill", "mcp", "agent", "command", "hook", "rule", "claude-code-plugin"}

	for _, typeFilter := range validTypes {
		t.Run(typeFilter, func(t *testing.T) {
			cmd := NewListCommand()
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs([]string{"--type=" + typeFilter})

			err := cmd.Execute()

			// The command will fail at config loading, but should NOT fail at type validation
			if err != nil && strings.Contains(err.Error(), "invalid asset type") {
				t.Errorf("type %q should be valid but got validation error: %v", typeFilter, err)
			}
		})
	}
}

func TestTypeFilterToJSONKey(t *testing.T) {
	tests := []struct {
		typeFilter string
		wantKey    string
	}{
		{"skill", "skills"},
		{"mcp", "mcps"},
		{"agent", "agents"},
		{"command", "commands"},
		{"hook", "hooks"},
		{"rule", "rules"},
		{"claude-code-plugin", "claude-code-plugins"},
	}

	for _, tt := range tests {
		t.Run(tt.typeFilter, func(t *testing.T) {
			got := typeFilterToJSONKey(tt.typeFilter)
			if got != tt.wantKey {
				t.Errorf("typeFilterToJSONKey(%q) = %q, want %q", tt.typeFilter, got, tt.wantKey)
			}
		})
	}
}

func TestPrintListJSONNoFilter(t *testing.T) {
	assets := []lockfile.Asset{
		{Name: "skill-1", Version: "1.0.0", Type: asset.TypeSkill},
		{Name: "mcp-1", Version: "2.0.0", Type: asset.TypeMCP},
		{Name: "rule-1", Version: "1.0.0", Type: asset.TypeRule},
	}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	out := newOutputHelper(cmd)

	err := printListJSON(out, assets, "")
	if err != nil {
		t.Fatalf("printListJSON() error = %v", err)
	}

	var result map[string][]map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	expectedKeys := []string{"skills", "mcps", "agents", "commands", "hooks", "rules", "claude-code-plugins"}
	for _, key := range expectedKeys {
		if _, ok := result[key]; !ok {
			t.Errorf("JSON output missing key %q", key)
		}
	}

	if len(result["skills"]) != 1 || result["skills"][0]["name"] != "skill-1" {
		t.Errorf("skills = %v, want [{name: skill-1, version: 1.0.0}]", result["skills"])
	}
	if len(result["mcps"]) != 1 || result["mcps"][0]["name"] != "mcp-1" {
		t.Errorf("mcps = %v, want [{name: mcp-1, version: 2.0.0}]", result["mcps"])
	}
	if len(result["rules"]) != 1 || result["rules"][0]["name"] != "rule-1" {
		t.Errorf("rules = %v, want [{name: rule-1, version: 1.0.0}]", result["rules"])
	}
	if len(result["agents"]) != 0 {
		t.Errorf("agents = %v, want []", result["agents"])
	}
}

func TestPrintListJSONWithTypeFilter(t *testing.T) {
	assets := []lockfile.Asset{
		{Name: "skill-1", Version: "1.0.0", Type: asset.TypeSkill},
		{Name: "skill-2", Version: "2.0.0", Type: asset.TypeSkill},
	}

	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	out := newOutputHelper(cmd)

	err := printListJSON(out, assets, "skill")
	if err != nil {
		t.Fatalf("printListJSON() error = %v", err)
	}

	var result map[string][]map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 key in output, got %d keys: %v", len(result), result)
	}

	skills, ok := result["skills"]
	if !ok {
		t.Fatal("JSON output missing 'skills' key")
	}

	if len(skills) != 2 {
		t.Errorf("skills has %d items, want 2", len(skills))
	}

	expected := []map[string]any{
		{"name": "skill-1", "version": "1.0.0"},
		{"name": "skill-2", "version": "2.0.0"},
	}
	for i, exp := range expected {
		if skills[i]["name"] != exp["name"] || skills[i]["version"] != exp["version"] {
			t.Errorf("skills[%d] = %v, want %v", i, skills[i], exp)
		}
	}
}

func TestPrintListJSONEmptyAssets(t *testing.T) {
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	out := newOutputHelper(cmd)

	err := printListJSON(out, nil, "")
	if err != nil {
		t.Fatalf("printListJSON() error = %v", err)
	}

	var result map[string][]map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	expectedKeys := []string{"skills", "mcps", "agents", "commands", "hooks", "rules", "claude-code-plugins"}
	for _, key := range expectedKeys {
		arr, ok := result[key]
		if !ok {
			t.Errorf("JSON output missing key %q", key)
		}
		if len(arr) != 0 {
			t.Errorf("%s = %v, want empty array", key, arr)
		}
	}
}

func TestPrintListJSONEmptyAssetsWithTypeFilter(t *testing.T) {
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	out := newOutputHelper(cmd)

	err := printListJSON(out, nil, "skill")
	if err != nil {
		t.Fatalf("printListJSON() error = %v", err)
	}

	var result map[string][]map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 key in output, got %d keys: %v", len(result), result)
	}

	skills, ok := result["skills"]
	if !ok {
		t.Fatal("JSON output missing 'skills' key")
	}

	if len(skills) != 0 {
		t.Errorf("skills = %v, want empty array", skills)
	}
}
