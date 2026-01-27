package utils

import (
	"regexp"
	"strings"
)

// MarkdownSection represents a parsed section from a markdown file
type MarkdownSection struct {
	Heading string // The heading text (without ## prefix)
	Content string // The section content (including any sub-headings)
	Level   int    // Heading level (2 for ##, 3 for ###, etc.)
}

var headingRegex = regexp.MustCompile(`^(#{2,6})\s+(.+)$`)

// ParseMarkdownSections extracts ## sections from markdown content
func ParseMarkdownSections(content string) []MarkdownSection {
	var sections []MarkdownSection

	lines := strings.Split(content, "\n")
	var currentSection *MarkdownSection
	var currentContent strings.Builder

	for i, line := range lines {
		if match := headingRegex.FindStringSubmatch(line); match != nil {
			level := len(match[1])
			heading := strings.TrimSpace(match[2])

			// Only treat ## (level 2) as top-level sections
			if level == 2 {
				// Save previous section
				if currentSection != nil {
					currentSection.Content = strings.TrimSpace(currentContent.String())
					sections = append(sections, *currentSection)
				}

				// Start new section
				currentSection = &MarkdownSection{
					Heading: heading,
					Level:   level,
				}
				currentContent.Reset()
			} else if currentSection != nil {
				// Include sub-headings in current section
				currentContent.WriteString(line)
				currentContent.WriteString("\n")
			}
		} else if currentSection != nil {
			currentContent.WriteString(line)
			if i < len(lines)-1 {
				currentContent.WriteString("\n")
			}
		}
	}

	// Save last section
	if currentSection != nil {
		currentSection.Content = strings.TrimSpace(currentContent.String())
		sections = append(sections, *currentSection)
	}

	return sections
}

// RemoveMarkdownSections removes specified sections from markdown content
func RemoveMarkdownSections(content string, headingsToRemove []string) string {
	// Build a set of headings to remove
	removeSet := make(map[string]bool)
	for _, h := range headingsToRemove {
		removeSet[h] = true
	}

	lines := strings.Split(content, "\n")
	var result []string
	var skipUntilNextH2 bool

	for _, line := range lines {
		if match := headingRegex.FindStringSubmatch(line); match != nil {
			level := len(match[1])
			heading := strings.TrimSpace(match[2])

			if level == 2 {
				// Check if this h2 should be removed
				if removeSet[heading] {
					skipUntilNextH2 = true
					continue
				}
				skipUntilNextH2 = false
			}
		}

		if !skipUntilNextH2 {
			result = append(result, line)
		}
	}

	return CleanupMarkdownSpacing(strings.Join(result, "\n"))
}

// CleanupMarkdownSpacing reduces multiple consecutive blank lines to a maximum of two
func CleanupMarkdownSpacing(content string) string {
	// Replace 3+ consecutive newlines with 2
	for strings.Contains(content, "\n\n\n") {
		content = strings.ReplaceAll(content, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(content) + "\n"
}
