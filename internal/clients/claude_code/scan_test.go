package claude_code

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/clients"
)

func TestScanInstalledAssets(t *testing.T) {
	// Create isolated test environment
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	claudeDir := filepath.Join(homeDir, ".claude")

	t.Setenv("HOME", homeDir)

	// Create skills directory with unmanaged skill (directory with SKILL.md)
	skillsDir := filepath.Join(claudeDir, "skills")
	unmanagedSkillDir := filepath.Join(skillsDir, "my-skill")
	if err := os.MkdirAll(unmanagedSkillDir, 0755); err != nil {
		t.Fatalf("Failed to create skill directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(unmanagedSkillDir, "SKILL.md"), []byte("# My Skill"), 0644); err != nil {
		t.Fatalf("Failed to create SKILL.md: %v", err)
	}

	// Create managed skill (has metadata.toml, should be skipped)
	managedSkillDir := filepath.Join(skillsDir, "managed-skill")
	if err := os.MkdirAll(managedSkillDir, 0755); err != nil {
		t.Fatalf("Failed to create managed skill directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(managedSkillDir, "SKILL.md"), []byte("# Managed Skill"), 0644); err != nil {
		t.Fatalf("Failed to create SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(managedSkillDir, "metadata.toml"), []byte("name = \"managed-skill\""), 0644); err != nil {
		t.Fatalf("Failed to create metadata.toml: %v", err)
	}

	// Create agents directory with unmanaged agent (single .md file)
	agentsDir := filepath.Join(claudeDir, "agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatalf("Failed to create agents directory: %v", err)
	}
	agentContent := `---
name: my-agent
description: Test agent
---

Agent prompt here.
`
	if err := os.WriteFile(filepath.Join(agentsDir, "my-agent.md"), []byte(agentContent), 0644); err != nil {
		t.Fatalf("Failed to create agent file: %v", err)
	}

	// Create managed agent (has companion .metadata.toml, should be skipped)
	if err := os.WriteFile(filepath.Join(agentsDir, "managed-agent.md"), []byte(agentContent), 0644); err != nil {
		t.Fatalf("Failed to create managed agent file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentsDir, "managed-agent.metadata.toml"), []byte("name = \"managed-agent\""), 0644); err != nil {
		t.Fatalf("Failed to create agent metadata file: %v", err)
	}

	// Create a non-.md file in agents (should be ignored)
	if err := os.WriteFile(filepath.Join(agentsDir, "readme.txt"), []byte("readme"), 0644); err != nil {
		t.Fatalf("Failed to create readme.txt: %v", err)
	}

	// Create client and scan
	client := &Client{}
	ctx := context.Background()
	globalScope := &clients.InstallScope{Type: clients.ScopeGlobal}

	assets, err := client.ScanInstalledAssets(ctx, globalScope)
	if err != nil {
		t.Fatalf("ScanInstalledAssets failed: %v", err)
	}

	// Verify results
	foundSkill := false
	foundAgent := false
	foundManagedSkill := false
	foundManagedAgent := false

	for _, a := range assets {
		switch a.Name {
		case "my-skill":
			foundSkill = true
			if a.Type != asset.TypeSkill {
				t.Errorf("my-skill has wrong type: got %v, want %v", a.Type, asset.TypeSkill)
			}
		case "my-agent":
			foundAgent = true
			if a.Type != asset.TypeAgent {
				t.Errorf("my-agent has wrong type: got %v, want %v", a.Type, asset.TypeAgent)
			}
		case "managed-skill":
			foundManagedSkill = true
		case "managed-agent":
			foundManagedAgent = true
		}
	}

	if !foundSkill {
		t.Error("Expected to find unmanaged skill 'my-skill'")
	}
	if !foundAgent {
		t.Error("Expected to find unmanaged agent 'my-agent'")
	}
	if foundManagedSkill {
		t.Error("Should not find managed skill 'managed-skill' (has metadata.toml)")
	}
	if foundManagedAgent {
		t.Error("Should not find managed agent 'managed-agent' (has .metadata.toml)")
	}

	t.Logf("Found %d unmanaged assets", len(assets))
	for _, a := range assets {
		t.Logf("  - %s (%s)", a.Name, a.Type.Label)
	}
}

func TestScanInstalledAssets_SkillWithLowercaseFile(t *testing.T) {
	// Test that skill.md (lowercase) is also detected
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	claudeDir := filepath.Join(homeDir, ".claude")

	t.Setenv("HOME", homeDir)

	skillDir := filepath.Join(claudeDir, "skills", "lowercase-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("Failed to create skill directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# Lowercase Skill"), 0644); err != nil {
		t.Fatalf("Failed to create skill.md: %v", err)
	}

	client := &Client{}
	ctx := context.Background()
	globalScope := &clients.InstallScope{Type: clients.ScopeGlobal}

	assets, err := client.ScanInstalledAssets(ctx, globalScope)
	if err != nil {
		t.Fatalf("ScanInstalledAssets failed: %v", err)
	}

	found := false
	for _, a := range assets {
		if a.Name == "lowercase-skill" {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected to find skill with lowercase skill.md file")
	}
}

func TestScanInstalledAssets_EmptyDirectories(t *testing.T) {
	// Test that empty directories don't cause errors
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	claudeDir := filepath.Join(homeDir, ".claude")

	t.Setenv("HOME", homeDir)

	// Create empty skills and agents directories
	if err := os.MkdirAll(filepath.Join(claudeDir, "skills"), 0755); err != nil {
		t.Fatalf("Failed to create skills directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(claudeDir, "agents"), 0755); err != nil {
		t.Fatalf("Failed to create agents directory: %v", err)
	}

	client := &Client{}
	ctx := context.Background()
	globalScope := &clients.InstallScope{Type: clients.ScopeGlobal}

	assets, err := client.ScanInstalledAssets(ctx, globalScope)
	if err != nil {
		t.Fatalf("ScanInstalledAssets failed: %v", err)
	}

	if len(assets) != 0 {
		t.Errorf("Expected 0 assets from empty directories, got %d", len(assets))
	}
}

func TestScanInstalledAssets_NoDirectories(t *testing.T) {
	// Test that missing directories don't cause errors
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")

	t.Setenv("HOME", homeDir)

	// Don't create any directories

	client := &Client{}
	ctx := context.Background()
	globalScope := &clients.InstallScope{Type: clients.ScopeGlobal}

	assets, err := client.ScanInstalledAssets(ctx, globalScope)
	if err != nil {
		t.Fatalf("ScanInstalledAssets failed: %v", err)
	}

	if len(assets) != 0 {
		t.Errorf("Expected 0 assets when directories don't exist, got %d", len(assets))
	}
}
