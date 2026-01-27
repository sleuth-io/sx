// Package rule provides operations for injecting rules into markdown files.
package rule

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// DefaultPromptFile is the default prompt file for rule assets
const DefaultPromptFile = "RULE.md"

// Injection represents an instruction to be injected
type Injection struct {
	Name    string // Asset name (used for ordering and identification)
	Title   string // Display title (used as subheading)
	Content string // Instruction content
}

// InjectInstructions injects multiple instructions into a target markdown file.
// Instructions are placed in a managed section identified by heading and endMarker.
// If the section doesn't exist, it's appended to the file.
// Instructions are ordered alphabetically by name for reproducibility.
func InjectInstructions(targetPath, heading, endMarker string, instructions []Injection) error {
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Read existing content or start fresh
	existingContent, _ := os.ReadFile(targetPath)
	content := string(existingContent)

	// Sort instructions alphabetically by name
	sorted := make([]Injection, len(instructions))
	copy(sorted, instructions)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	// Build the managed section content
	managedContent := buildManagedSection(heading, endMarker, sorted)

	// Replace or append the managed section
	newContent := replaceManagedSection(content, heading, endMarker, managedContent)

	// Write the file
	if err := os.WriteFile(targetPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// RemoveInstruction removes a single instruction from a target file's managed section.
// If this leaves the managed section empty, the entire section is removed.
func RemoveInstruction(targetPath, heading, endMarker, name string) error {
	content, err := os.ReadFile(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist, nothing to remove
		}
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Extract existing instructions from managed section
	instructions := ExtractInstructions(string(content), heading, endMarker)

	// Filter out the instruction to remove
	var remaining []Injection
	for _, inst := range instructions {
		if inst.Name != name {
			remaining = append(remaining, inst)
		}
	}

	if len(remaining) == len(instructions) {
		return nil // Instruction wasn't present
	}

	var newContent string
	if len(remaining) == 0 {
		// No instructions left - remove the entire managed section
		newContent = removeManagedSection(string(content), heading, endMarker)
	} else {
		// Rebuild with remaining instructions
		managedContent := buildManagedSection(heading, endMarker, remaining)
		newContent = replaceManagedSection(string(content), heading, endMarker, managedContent)
	}

	if err := os.WriteFile(targetPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// InstructionExists checks if an instruction with the given name exists in the managed section
func InstructionExists(targetPath, heading, endMarker, name string) bool {
	content, err := os.ReadFile(targetPath)
	if err != nil {
		return false
	}

	instructions := ExtractInstructions(string(content), heading, endMarker)
	for _, inst := range instructions {
		if inst.Name == name {
			return true
		}
	}
	return false
}

// buildManagedSection creates the full managed section content
func buildManagedSection(heading, endMarker string, instructions []Injection) string {
	if len(instructions) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(heading)
	sb.WriteString("\n\n")

	// Determine subheading level (one deeper than section heading)
	subheadingPrefix := getSubheadingPrefix(heading)

	for i, inst := range instructions {
		// Write subheading with title
		sb.WriteString(subheadingPrefix)
		sb.WriteString(" ")
		sb.WriteString(inst.Title)
		sb.WriteString("\n\n")

		// Write content
		sb.WriteString(strings.TrimSpace(inst.Content))

		if i < len(instructions)-1 {
			sb.WriteString("\n\n")
		}
	}

	sb.WriteString("\n\n")
	sb.WriteString(endMarker)
	sb.WriteString("\n")

	return sb.String()
}

// getSubheadingPrefix returns the heading prefix one level deeper than the given heading
func getSubheadingPrefix(heading string) string {
	// Count leading # characters
	trimmed := strings.TrimLeft(heading, "#")
	level := len(heading) - len(trimmed)

	// Return one level deeper
	return strings.Repeat("#", level+1)
}

// replaceManagedSection replaces the managed section in content, or appends if not found
func replaceManagedSection(content, heading, endMarker, managedContent string) string {
	// Build regex to find existing managed section
	pattern := buildSectionPattern(heading, endMarker)
	re := regexp.MustCompile(pattern)

	if re.MatchString(content) {
		// Replace existing section
		return re.ReplaceAllString(content, managedContent)
	}

	// Append to end of file
	content = strings.TrimRight(content, "\n")
	if content != "" {
		content += "\n\n"
	}
	content += managedContent

	return content
}

// removeManagedSection removes the managed section entirely from content
func removeManagedSection(content, heading, endMarker string) string {
	pattern := buildSectionPattern(heading, endMarker)
	re := regexp.MustCompile(pattern)

	result := re.ReplaceAllString(content, "")

	// Clean up extra blank lines
	result = regexp.MustCompile(`\n{3,}`).ReplaceAllString(result, "\n\n")
	result = strings.TrimRight(result, "\n")
	if result != "" {
		result += "\n"
	}

	return result
}

// buildSectionPattern builds a regex pattern to match the managed section
func buildSectionPattern(heading, endMarker string) string {
	// Escape special regex characters in heading and end marker
	escapedHeading := regexp.QuoteMeta(heading)
	escapedEndMarker := regexp.QuoteMeta(endMarker)

	// Match from heading to end marker (including trailing newline if present)
	return `(?s)` + escapedHeading + `\n.*?` + escapedEndMarker + `\n?`
}

// ExtractInstructions parses instructions from an existing managed section
func ExtractInstructions(content, heading, endMarker string) []Injection {
	pattern := buildSectionPattern(heading, endMarker)
	re := regexp.MustCompile(pattern)

	match := re.FindString(content)
	if match == "" {
		return nil
	}

	// Remove heading and end marker
	sectionContent := strings.TrimPrefix(match, heading)
	sectionContent = strings.TrimSuffix(sectionContent, endMarker)
	sectionContent = strings.TrimSuffix(sectionContent, endMarker+"\n")
	sectionContent = strings.TrimSpace(sectionContent)

	if sectionContent == "" {
		return nil
	}

	// Determine subheading prefix
	subheadingPrefix := getSubheadingPrefix(heading)

	// Split by subheadings
	subheadingPattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(subheadingPrefix) + ` +(.+)$`)
	matches := subheadingPattern.FindAllStringSubmatchIndex(sectionContent, -1)

	var instructions []Injection
	for i, m := range matches {
		title := sectionContent[m[2]:m[3]]

		// Find content end (either next subheading or end of section)
		contentStart := m[1]
		var contentEnd int
		if i+1 < len(matches) {
			contentEnd = matches[i+1][0]
		} else {
			contentEnd = len(sectionContent)
		}

		instContent := strings.TrimSpace(sectionContent[contentStart:contentEnd])

		instructions = append(instructions, Injection{
			Name:    title, // Use title as name when extracting (we don't store name separately)
			Title:   title,
			Content: instContent,
		})
	}

	return instructions
}
