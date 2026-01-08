package commands

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestVaultCommandWithPathRepository tests the vault list and show commands
// using a path repository with multiple assets
func TestVaultCommandWithPathRepository(t *testing.T) {
	env := NewTestEnv(t)

	// Setup path vault
	vaultDir := env.SetupPathVault()

	// Add multiple skills with different versions
	env.AddSkillToVault(vaultDir, "code-review", "1.0.0")
	env.AddSkillToVault(vaultDir, "code-review", "2.0.0")
	env.AddSkillToVault(vaultDir, "code-review", "3.0.0")
	env.AddSkillToVault(vaultDir, "test-generator", "1.0.0")
	env.AddSkillToVault(vaultDir, "bug-finder", "1.5.0")

	// Create list.txt files for all assets
	env.WriteFile(filepath.Join(vaultDir, "assets", "code-review", "list.txt"),
		"1.0.0\n2.0.0\n3.0.0\n")
	env.WriteFile(filepath.Join(vaultDir, "assets", "test-generator", "list.txt"),
		"1.0.0\n")
	env.WriteFile(filepath.Join(vaultDir, "assets", "bug-finder", "list.txt"),
		"1.5.0\n")

	// Create a working directory
	workingDir := env.MkdirAll(filepath.Join(env.TempDir, "working"))
	env.Chdir(workingDir)

	// Test 1: vault list (text output)
	t.Run("list shows all assets", func(t *testing.T) {
		cmd := NewVaultCommand()
		cmd.SetArgs([]string{"list"})

		var stdout bytes.Buffer
		cmd.SetOut(&stdout)

		if err := cmd.Execute(); err != nil {
			t.Fatalf("vault list failed: %v", err)
		}

		output := stdout.String()

		// Verify all assets are listed
		if !strings.Contains(output, "code-review") {
			t.Errorf("Expected 'code-review' in output, got:\n%s", output)
		}
		if !strings.Contains(output, "test-generator") {
			t.Errorf("Expected 'test-generator' in output, got:\n%s", output)
		}
		if !strings.Contains(output, "bug-finder") {
			t.Errorf("Expected 'bug-finder' in output, got:\n%s", output)
		}

		// Verify version count for multi-version asset
		if !strings.Contains(output, "(3 versions)") {
			t.Errorf("Expected '(3 versions)' for code-review, got:\n%s", output)
		}
	})

	// Test 2: vault list --type skill
	t.Run("list filters by type", func(t *testing.T) {
		cmd := NewVaultCommand()
		cmd.SetArgs([]string{"list", "--type", "skill"})

		var stdout bytes.Buffer
		cmd.SetOut(&stdout)

		if err := cmd.Execute(); err != nil {
			t.Fatalf("vault list --type skill failed: %v", err)
		}

		output := stdout.String()

		if !strings.Contains(output, "skill Assets") {
			t.Errorf("Expected 'skill Assets' header, got:\n%s", output)
		}
	})

	// Test 3: vault list --json
	t.Run("list json output is valid", func(t *testing.T) {
		cmd := NewVaultCommand()
		cmd.SetArgs([]string{"list", "--json"})

		var stdout bytes.Buffer
		cmd.SetOut(&stdout)

		if err := cmd.Execute(); err != nil {
			t.Fatalf("vault list --json failed: %v", err)
		}

		output := stdout.String()

		// Parse JSON to verify it's valid
		var assets []map[string]any
		if err := json.Unmarshal([]byte(output), &assets); err != nil {
			t.Fatalf("Invalid JSON output: %v\nOutput:\n%s", err, output)
		}

		// Verify we have 3 assets
		if len(assets) != 3 {
			t.Errorf("Expected 3 assets in JSON output, got %d", len(assets))
		}

		// Verify structure of first asset
		if len(assets) > 0 {
			asset := assets[0]
			requiredFields := []string{"name", "type", "latestVersion", "versionsCount", "description"}
			for _, field := range requiredFields {
				if _, ok := asset[field]; !ok {
					t.Errorf("Expected field '%s' in asset JSON", field)
				}
			}
		}

		// Find code-review asset and verify version count
		var codeReview map[string]any
		for _, asset := range assets {
			if name, ok := asset["name"].(string); ok && name == "code-review" {
				codeReview = asset
				break
			}
		}

		if codeReview == nil {
			t.Errorf("Expected to find 'code-review' asset in JSON output")
		} else {
			if versionsCount, ok := codeReview["versionsCount"].(float64); !ok || int(versionsCount) != 3 {
				t.Errorf("Expected code-review to have 3 versions, got %v", codeReview["versionsCount"])
			}
			if latestVersion, ok := codeReview["latestVersion"].(string); !ok || latestVersion != "3.0.0" {
				t.Errorf("Expected code-review latest version to be '3.0.0', got %v", codeReview["latestVersion"])
			}
		}
	})

	// Test 4: vault show <asset-name>
	t.Run("show displays asset details", func(t *testing.T) {
		cmd := NewVaultCommand()
		cmd.SetArgs([]string{"show", "code-review"})

		var stdout bytes.Buffer
		cmd.SetOut(&stdout)

		if err := cmd.Execute(); err != nil {
			t.Fatalf("vault show code-review failed: %v", err)
		}

		output := stdout.String()

		// Verify asset details are shown
		if !strings.Contains(output, "code-review") {
			t.Errorf("Expected 'code-review' in output, got:\n%s", output)
		}
		if !strings.Contains(output, "Skill") {
			t.Errorf("Expected 'Skill' in output, got:\n%s", output)
		}
		if !strings.Contains(output, "Latest Version: v3.0.0") {
			t.Errorf("Expected 'Latest Version: v3.0.0', got:\n%s", output)
		}
		if !strings.Contains(output, "Total Versions: 3") {
			t.Errorf("Expected 'Total Versions: 3', got:\n%s", output)
		}

		// Verify all versions are listed
		if !strings.Contains(output, "Versions") {
			t.Errorf("Expected 'Versions' section, got:\n%s", output)
		}
		if !strings.Contains(output, "v1.0.0") {
			t.Errorf("Expected version v1.0.0 in list, got:\n%s", output)
		}
		if !strings.Contains(output, "v2.0.0") {
			t.Errorf("Expected version v2.0.0 in list, got:\n%s", output)
		}
		if !strings.Contains(output, "v3.0.0") {
			t.Errorf("Expected version v3.0.0 in list, got:\n%s", output)
		}
	})

	// Test 5: vault show <asset-name> --json
	t.Run("show json output is valid", func(t *testing.T) {
		cmd := NewVaultCommand()
		cmd.SetArgs([]string{"show", "test-generator", "--json"})

		var stdout bytes.Buffer
		cmd.SetOut(&stdout)

		if err := cmd.Execute(); err != nil {
			t.Fatalf("vault show test-generator --json failed: %v", err)
		}

		output := stdout.String()

		// Parse JSON to verify it's valid
		var assetDetails map[string]any
		if err := json.Unmarshal([]byte(output), &assetDetails); err != nil {
			t.Fatalf("Invalid JSON output: %v\nOutput:\n%s", err, output)
		}

		// Verify structure
		requiredFields := []string{"name", "type", "description", "versions"}
		for _, field := range requiredFields {
			if _, ok := assetDetails[field]; !ok {
				t.Errorf("Expected field '%s' in asset details JSON", field)
			}
		}

		// Verify name
		if name, ok := assetDetails["name"].(string); !ok || name != "test-generator" {
			t.Errorf("Expected name to be 'test-generator', got %v", assetDetails["name"])
		}

		// Verify versions array
		if versions, ok := assetDetails["versions"].([]any); !ok {
			t.Errorf("Expected 'versions' to be an array")
		} else if len(versions) != 1 {
			t.Errorf("Expected 1 version, got %d", len(versions))
		} else {
			// Verify version structure
			version := versions[0].(map[string]any)
			if v, ok := version["version"].(string); !ok || v != "1.0.0" {
				t.Errorf("Expected version '1.0.0', got %v", version["version"])
			}
		}

		// Verify metadata exists (optional field)
		if _, ok := assetDetails["metadata"]; ok {
			t.Log("✓ Metadata field present")
		}
	})

	// Test 6: vault show non-existent asset
	t.Run("show non-existent asset returns error", func(t *testing.T) {
		cmd := NewVaultCommand()
		cmd.SetArgs([]string{"show", "non-existent-skill"})

		var stdout bytes.Buffer
		cmd.SetOut(&stdout)

		err := cmd.Execute()
		if err == nil {
			t.Errorf("Expected error for non-existent asset, but command succeeded")
		} else {
			if !strings.Contains(err.Error(), "not found") {
				t.Errorf("Expected 'not found' in error message, got: %v", err)
			}
		}
	})

	t.Log("✓ All vault command tests passed!")
}

// TestVaultCommandEmptyRepository tests vault commands with an empty repository
func TestVaultCommandEmptyRepository(t *testing.T) {
	env := NewTestEnv(t)

	// Setup empty path vault (no assets)
	vaultDir := env.SetupPathVault()

	// Create assets directory but leave it empty
	env.MkdirAll(filepath.Join(vaultDir, "assets"))

	workingDir := env.MkdirAll(filepath.Join(env.TempDir, "working"))
	env.Chdir(workingDir)

	t.Run("list empty vault", func(t *testing.T) {
		cmd := NewVaultCommand()
		cmd.SetArgs([]string{"list"})

		var stdout bytes.Buffer
		cmd.SetOut(&stdout)

		if err := cmd.Execute(); err != nil {
			t.Fatalf("vault list on empty vault failed: %v", err)
		}

		output := stdout.String()

		if !strings.Contains(output, "No assets found in vault") {
			t.Errorf("Expected 'No assets found in vault', got:\n%s", output)
		}
	})

	t.Run("list empty vault json", func(t *testing.T) {
		cmd := NewVaultCommand()
		cmd.SetArgs([]string{"list", "--json"})

		var stdout bytes.Buffer
		cmd.SetOut(&stdout)

		if err := cmd.Execute(); err != nil {
			t.Fatalf("vault list --json on empty vault failed: %v", err)
		}

		output := stdout.String()

		// Parse JSON to verify it's valid
		var assets []map[string]any
		if err := json.Unmarshal([]byte(output), &assets); err != nil {
			t.Fatalf("Invalid JSON output: %v\nOutput:\n%s", err, output)
		}

		if len(assets) != 0 {
			t.Errorf("Expected empty array, got %d assets", len(assets))
		}
	})
}
