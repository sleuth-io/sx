package metadata

import (
	"testing"
)

func TestParseValidMetadata(t *testing.T) {
	metadataData := []byte(`
[artifact]
name = "test-skill"
version = "1.0.0"
type = "skill"
description = "A test skill"
authors = ["Test Author <test@example.com>"]
`)

	meta, err := Parse(metadataData)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if meta.Artifact.Name != "test-skill" {
		t.Errorf("Expected name test-skill, got %s", meta.Artifact.Name)
	}

	if meta.Artifact.Version != "1.0.0" {
		t.Errorf("Expected version 1.0.0, got %s", meta.Artifact.Version)
	}

	if meta.Artifact.Type != "skill" {
		t.Errorf("Expected type skill, got %s", meta.Artifact.Type)
	}

	if len(meta.Artifact.Authors) > 0 && meta.Artifact.Authors[0] != "Test Author <test@example.com>" {
		t.Errorf("Expected author 'Test Author <test@example.com>', got %s", meta.Artifact.Authors[0])
	}
}

func TestValidateMetadata(t *testing.T) {
	tests := []struct {
		name     string
		metadata *Metadata
		wantErr  bool
	}{
		{
			name: "valid metadata",
			metadata: &Metadata{
				Artifact: Artifact{
					Name:        "test-skill",
					Version:     "1.0.0",
					Type:        "skill",
					Description: "A test skill",
					Authors:     []string{"Test Author"},
				},
				Skill: &SkillConfig{
					PromptFile: "prompt.md",
				},
			},
			wantErr: false,
		},
		{
			name: "missing name",
			metadata: &Metadata{
				Artifact: Artifact{
					Version:     "1.0.0",
					Type:        "skill",
					Description: "A test skill",
				},
			},
			wantErr: true,
		},
		{
			name: "invalid semver",
			metadata: &Metadata{
				Artifact: Artifact{
					Name:        "test-skill",
					Version:     "invalid",
					Type:        "skill",
					Description: "A test skill",
				},
			},
			wantErr: true,
		},
		{
			name: "invalid type",
			metadata: &Metadata{
				Artifact: Artifact{
					Name:        "test-skill",
					Version:     "1.0.0",
					Type:        "invalid-type",
					Description: "A test skill",
				},
			},
			wantErr: true,
		},
		{
			name: "missing description",
			metadata: &Metadata{
				Artifact: Artifact{
					Name:    "test-skill",
					Version: "1.0.0",
					Type:    "skill",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.metadata.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMetadataWithDependencies(t *testing.T) {
	metadataData := []byte(`
[artifact]
name = "test-skill"
version = "1.0.0"
type = "skill"
description = "A test skill with dependencies"
dependencies = ["dep1", "dep2"]
`)

	meta, err := Parse(metadataData)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(meta.Artifact.Dependencies) != 2 {
		t.Fatalf("Expected 2 dependencies, got %d", len(meta.Artifact.Dependencies))
	}

	if meta.Artifact.Dependencies[0] != "dep1" {
		t.Errorf("Expected first dependency 'dep1', got %s", meta.Artifact.Dependencies[0])
	}

	if meta.Artifact.Dependencies[1] != "dep2" {
		t.Errorf("Expected second dependency 'dep2', got %s", meta.Artifact.Dependencies[1])
	}
}
