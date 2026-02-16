package singlefile

import (
	"fmt"
	"strings"

	"github.com/sleuth-io/sx/internal/metadata"
)

// NoTransform returns nil (no transformation)
func NoTransform() func(*metadata.Metadata, []byte) []byte {
	return nil
}

// WithDescriptionFrontmatter adds YAML frontmatter with description if present
func WithDescriptionFrontmatter() func(*metadata.Metadata, []byte) []byte {
	return func(meta *metadata.Metadata, content []byte) []byte {
		description := meta.Asset.Description
		if description == "" {
			return append([]byte(strings.TrimSpace(string(content))), '\n')
		}

		var sb strings.Builder
		sb.WriteString("---\n")
		sb.WriteString(fmt.Sprintf("description: %s\n", description))
		sb.WriteString("---\n\n")
		sb.WriteString(strings.TrimSpace(string(content)))
		sb.WriteString("\n")
		return []byte(sb.String())
	}
}

// WithRuleFrontmatter adds YAML frontmatter for rules (description + globs/paths)
func WithRuleFrontmatter(pathsField string) func(*metadata.Metadata, []byte) []byte {
	return func(meta *metadata.Metadata, content []byte) []byte {
		var sb strings.Builder

		description := getRuleDescription(meta)
		globs := getRuleGlobs(meta)
		title := getRuleTitle(meta)

		// Only add frontmatter if there's something to add
		if len(globs) > 0 || description != "" {
			sb.WriteString("---\n")

			if description != "" {
				sb.WriteString(fmt.Sprintf("description: %s\n", description))
			}

			if len(globs) > 0 {
				if len(globs) == 1 {
					sb.WriteString(fmt.Sprintf("%s:\n  - %s\n", pathsField, globs[0]))
				} else {
					sb.WriteString(pathsField + ":\n")
					for _, glob := range globs {
						sb.WriteString(fmt.Sprintf("  - %s\n", glob))
					}
				}
			}

			sb.WriteString("---\n\n")
		}

		// Title as heading if content doesn't start with one
		contentStr := strings.TrimSpace(string(content))
		if title != "" && !strings.HasPrefix(contentStr, "#") {
			sb.WriteString("# ")
			sb.WriteString(title)
			sb.WriteString("\n\n")
		}

		sb.WriteString(contentStr)
		sb.WriteString("\n")
		return []byte(sb.String())
	}
}

// WithCopilotRuleFrontmatter adds YAML frontmatter for Copilot rules (applyTo field)
func WithCopilotRuleFrontmatter() func(*metadata.Metadata, []byte) []byte {
	return func(meta *metadata.Metadata, content []byte) []byte {
		var sb strings.Builder

		description := getRuleDescription(meta)
		globs := getRuleGlobs(meta)
		title := getRuleTitle(meta)

		sb.WriteString("---\n")

		// Copilot uses comma-separated globs in applyTo
		if len(globs) > 0 {
			sb.WriteString(fmt.Sprintf("applyTo: \"%s\"\n", strings.Join(globs, ",")))
		}

		if description != "" {
			escapedDesc := strings.ReplaceAll(description, `"`, `\"`)
			sb.WriteString(fmt.Sprintf("description: \"%s\"\n", escapedDesc))
		}

		sb.WriteString("---\n\n")

		// Title as heading if content doesn't start with one
		contentStr := strings.TrimSpace(string(content))
		if title != "" && !strings.HasPrefix(contentStr, "#") {
			sb.WriteString("# ")
			sb.WriteString(title)
			sb.WriteString("\n\n")
		}

		sb.WriteString(contentStr)
		sb.WriteString("\n")
		return []byte(sb.String())
	}
}

// WithCursorRuleFrontmatter adds MDC frontmatter for Cursor rules
func WithCursorRuleFrontmatter(pathScope string) func(*metadata.Metadata, []byte) []byte {
	return func(meta *metadata.Metadata, content []byte) []byte {
		var sb strings.Builder

		description := getRuleDescription(meta)
		globs := getRuleGlobsWithScope(meta, pathScope)
		title := getRuleTitle(meta)
		alwaysApply := shouldAlwaysApply(meta, pathScope, globs)

		sb.WriteString("---\n")

		if description != "" {
			sb.WriteString(fmt.Sprintf("description: %s\n", description))
		}

		if alwaysApply {
			sb.WriteString("alwaysApply: true\n")
		} else if len(globs) > 0 {
			if len(globs) == 1 {
				sb.WriteString(fmt.Sprintf("globs: %s\n", globs[0]))
			} else {
				sb.WriteString("globs:\n")
				for _, glob := range globs {
					sb.WriteString(fmt.Sprintf("  - %s\n", glob))
				}
			}
		}

		sb.WriteString("---\n\n")

		sb.WriteString("# ")
		sb.WriteString(title)
		sb.WriteString("\n\n")

		sb.WriteString(strings.TrimSpace(string(content)))
		sb.WriteString("\n")
		return []byte(sb.String())
	}
}

func getRuleDescription(meta *metadata.Metadata) string {
	if meta.Rule != nil && meta.Rule.Description != "" {
		return meta.Rule.Description
	}
	return meta.Asset.Description
}

func getRuleGlobs(meta *metadata.Metadata) []string {
	if meta.Rule != nil && len(meta.Rule.Globs) > 0 {
		return meta.Rule.Globs
	}
	return nil
}

func getRuleGlobsWithScope(meta *metadata.Metadata, pathScope string) []string {
	if meta.Rule != nil && len(meta.Rule.Globs) > 0 {
		return meta.Rule.Globs
	}
	if pathScope != "" {
		scope := strings.TrimSuffix(pathScope, "/")
		return []string{scope + "/**/*"}
	}
	return nil
}

func getRuleTitle(meta *metadata.Metadata) string {
	if meta.Rule != nil && meta.Rule.Title != "" {
		return meta.Rule.Title
	}
	return meta.Asset.Name
}

func shouldAlwaysApply(meta *metadata.Metadata, pathScope string, globs []string) bool {
	if meta.Rule != nil && meta.Rule.Cursor != nil {
		if alwaysApply, ok := meta.Rule.Cursor["always-apply"].(bool); ok {
			return alwaysApply
		}
	}
	return pathScope == "" && len(globs) == 0
}
