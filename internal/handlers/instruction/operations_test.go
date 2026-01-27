package instruction

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	testHeading   = "## Shared Instructions"
	testEndMarker = "---"
)

func TestInjectInstructions_NewFile(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "CLAUDE.md")

	instructions := []Injection{
		{Name: "test-instruction", Title: "Test Instruction", Content: "This is test content."},
	}

	err := InjectInstructions(targetPath, testHeading, testEndMarker, instructions)
	if err != nil {
		t.Fatalf("InjectInstructions failed: %v", err)
	}

	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	expected := `## Shared Instructions

### Test Instruction

This is test content.

---
`
	if string(content) != expected {
		t.Errorf("Content mismatch.\nExpected:\n%s\nGot:\n%s", expected, string(content))
	}
}

func TestInjectInstructions_ExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "CLAUDE.md")

	// Create existing file with some content
	existingContent := `# My Project

Some project documentation here.
`
	if err := os.WriteFile(targetPath, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	instructions := []Injection{
		{Name: "coding-standards", Title: "Coding Standards", Content: "Follow these coding standards."},
	}

	err := InjectInstructions(targetPath, testHeading, testEndMarker, instructions)
	if err != nil {
		t.Fatalf("InjectInstructions failed: %v", err)
	}

	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	expected := `# My Project

Some project documentation here.

## Shared Instructions

### Coding Standards

Follow these coding standards.

---
`
	if string(content) != expected {
		t.Errorf("Content mismatch.\nExpected:\n%s\nGot:\n%s", expected, string(content))
	}
}

func TestInjectInstructions_UpdateExistingSection(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "CLAUDE.md")

	// Create file with existing managed section
	existingContent := `# My Project

## Shared Instructions

### Old Instruction

Old content here.

---

## Other Section

This should remain.
`
	if err := os.WriteFile(targetPath, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	instructions := []Injection{
		{Name: "new-instruction", Title: "New Instruction", Content: "New content here."},
	}

	err := InjectInstructions(targetPath, testHeading, testEndMarker, instructions)
	if err != nil {
		t.Fatalf("InjectInstructions failed: %v", err)
	}

	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	expected := `# My Project

## Shared Instructions

### New Instruction

New content here.

---

## Other Section

This should remain.
`
	if string(content) != expected {
		t.Errorf("Content mismatch.\nExpected:\n%s\nGot:\n%s", expected, string(content))
	}
}

func TestInjectInstructions_MultipleInstructions(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "CLAUDE.md")

	// Instructions are provided out of order - should be sorted alphabetically
	instructions := []Injection{
		{Name: "zebra", Title: "Zebra Standards", Content: "Zebra content."},
		{Name: "alpha", Title: "Alpha Standards", Content: "Alpha content."},
		{Name: "beta", Title: "Beta Standards", Content: "Beta content."},
	}

	err := InjectInstructions(targetPath, testHeading, testEndMarker, instructions)
	if err != nil {
		t.Fatalf("InjectInstructions failed: %v", err)
	}

	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	// Should be sorted alphabetically by name
	expected := `## Shared Instructions

### Alpha Standards

Alpha content.

### Beta Standards

Beta content.

### Zebra Standards

Zebra content.

---
`
	if string(content) != expected {
		t.Errorf("Content mismatch.\nExpected:\n%s\nGot:\n%s", expected, string(content))
	}
}

func TestRemoveInstruction_SingleInstruction(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "CLAUDE.md")

	// Create file with managed section containing one instruction
	existingContent := `# My Project

## Shared Instructions

### Test Instruction

Test content.

---

## Other Content

Keep this.
`
	if err := os.WriteFile(targetPath, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	err := RemoveInstruction(targetPath, testHeading, testEndMarker, "Test Instruction")
	if err != nil {
		t.Fatalf("RemoveInstruction failed: %v", err)
	}

	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	// Section should be completely removed when last instruction is removed
	expected := `# My Project

## Other Content

Keep this.
`
	if string(content) != expected {
		t.Errorf("Content mismatch.\nExpected:\n%s\nGot:\n%s", expected, string(content))
	}
}

func TestRemoveInstruction_OneOfMultiple(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "CLAUDE.md")

	// First inject multiple instructions
	instructions := []Injection{
		{Name: "alpha", Title: "Alpha", Content: "Alpha standards content."},
		{Name: "beta", Title: "Beta", Content: "Beta standards content."},
	}

	err := InjectInstructions(targetPath, testHeading, testEndMarker, instructions)
	if err != nil {
		t.Fatalf("InjectInstructions failed: %v", err)
	}

	// Remove one instruction
	err = RemoveInstruction(targetPath, testHeading, testEndMarker, "Alpha")
	if err != nil {
		t.Fatalf("RemoveInstruction failed: %v", err)
	}

	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	// Beta should remain
	expected := "## Shared Instructions\n\n### Beta\n\nBeta standards content.\n\n---\n" //nolint:dupword
	if string(content) != expected {
		t.Errorf("Content mismatch.\nExpected:\n%s\nGot:\n%s", expected, string(content))
	}
}

func TestRemoveInstruction_NonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "nonexistent.md")

	// Should not error when file doesn't exist
	err := RemoveInstruction(targetPath, testHeading, testEndMarker, "any")
	if err != nil {
		t.Errorf("RemoveInstruction should not error on non-existent file: %v", err)
	}
}

func TestRemoveInstruction_NotPresent(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "CLAUDE.md")

	existingContent := `# My Project

## Shared Instructions

### Other Instruction

Content.

---
`
	if err := os.WriteFile(targetPath, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Remove instruction that doesn't exist
	err := RemoveInstruction(targetPath, testHeading, testEndMarker, "NonExistent")
	if err != nil {
		t.Fatalf("RemoveInstruction failed: %v", err)
	}

	// Content should be unchanged
	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if string(content) != existingContent {
		t.Errorf("Content should be unchanged.\nExpected:\n%s\nGot:\n%s", existingContent, string(content))
	}
}

func TestInstructionExists(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "CLAUDE.md")

	existingContent := `## Shared Instructions

### My Instruction

Content here.

---
`
	if err := os.WriteFile(targetPath, []byte(existingContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Should find existing instruction
	if !InstructionExists(targetPath, testHeading, testEndMarker, "My Instruction") {
		t.Error("InstructionExists should return true for existing instruction")
	}

	// Should not find non-existent instruction
	if InstructionExists(targetPath, testHeading, testEndMarker, "Other Instruction") {
		t.Error("InstructionExists should return false for non-existent instruction")
	}
}

func TestInstructionExists_NonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "nonexistent.md")

	if InstructionExists(targetPath, testHeading, testEndMarker, "any") {
		t.Error("InstructionExists should return false for non-existent file")
	}
}

func TestBuildManagedSection_EmptyInstructions(t *testing.T) {
	result := buildManagedSection(testHeading, testEndMarker, []Injection{})
	if result != "" {
		t.Errorf("Expected empty string for empty instructions, got: %s", result)
	}
}

func TestGetSubheadingPrefix(t *testing.T) {
	tests := []struct {
		heading  string
		expected string
	}{
		{"## Section", "###"},
		{"# Section", "##"},
		{"### Section", "####"},
		{"#### Section", "#####"},
	}

	for _, tt := range tests {
		result := getSubheadingPrefix(tt.heading)
		if result != tt.expected {
			t.Errorf("getSubheadingPrefix(%q) = %q, expected %q", tt.heading, result, tt.expected)
		}
	}
}

func TestExtractInstructions(t *testing.T) {
	content := `# Project

## Shared Instructions

### First Instruction

First content here.

### Second Instruction

Second content here.

---

## Other
`

	instructions := ExtractInstructions(content, testHeading, testEndMarker)

	if len(instructions) != 2 {
		t.Fatalf("Expected 2 instructions, got %d", len(instructions))
	}

	if instructions[0].Title != "First Instruction" {
		t.Errorf("First instruction title = %q, expected 'First Instruction'", instructions[0].Title)
	}
	if instructions[0].Content != "First content here." {
		t.Errorf("First instruction content = %q, expected 'First content here.'", instructions[0].Content)
	}

	if instructions[1].Title != "Second Instruction" {
		t.Errorf("Second instruction title = %q, expected 'Second Instruction'", instructions[1].Title)
	}
	if instructions[1].Content != "Second content here." {
		t.Errorf("Second instruction content = %q, expected 'Second content here.'", instructions[1].Content)
	}
}

func TestExtractInstructions_NoSection(t *testing.T) {
	content := `# Project

Some content without managed section.
`

	instructions := ExtractInstructions(content, testHeading, testEndMarker)
	if len(instructions) != 0 {
		t.Errorf("Expected 0 instructions when no managed section, got %d", len(instructions))
	}
}

func TestInjectInstructions_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "subdir", "nested", "CLAUDE.md")

	instructions := []Injection{
		{Name: "test", Title: "Test", Content: "Content."},
	}

	err := InjectInstructions(targetPath, testHeading, testEndMarker, instructions)
	if err != nil {
		t.Fatalf("InjectInstructions failed: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		t.Error("File should have been created")
	}
}

func TestInjectInstructions_MultilineContent(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "CLAUDE.md")

	instructions := []Injection{
		{
			Name:  "multiline",
			Title: "Multiline Instruction",
			Content: `This is line one.

This is line two with a gap.

- Bullet point 1
- Bullet point 2`,
		},
	}

	err := InjectInstructions(targetPath, testHeading, testEndMarker, instructions)
	if err != nil {
		t.Fatalf("InjectInstructions failed: %v", err)
	}

	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	expected := `## Shared Instructions

### Multiline Instruction

This is line one.

This is line two with a gap.

- Bullet point 1
- Bullet point 2

---
`
	if string(content) != expected {
		t.Errorf("Content mismatch.\nExpected:\n%s\nGot:\n%s", expected, string(content))
	}
}

func TestCustomHeadingAndEndMarker(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "CLAUDE.md")

	customHeading := "# Custom Instructions Section"
	customEndMarker := "<!-- END INSTRUCTIONS -->"

	instructions := []Injection{
		{Name: "test", Title: "Test", Content: "Content."},
	}

	err := InjectInstructions(targetPath, customHeading, customEndMarker, instructions)
	if err != nil {
		t.Fatalf("InjectInstructions failed: %v", err)
	}

	content, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	expected := `# Custom Instructions Section

## Test

Content.

<!-- END INSTRUCTIONS -->
`
	if string(content) != expected {
		t.Errorf("Content mismatch.\nExpected:\n%s\nGot:\n%s", expected, string(content))
	}

	// Verify we can check existence with custom markers
	if !InstructionExists(targetPath, customHeading, customEndMarker, "Test") {
		t.Error("InstructionExists should find instruction with custom markers")
	}
}
