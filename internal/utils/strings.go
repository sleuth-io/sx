package utils

import "strings"

// Slugify converts a string to a valid asset name.
// The result contains only lowercase alphanumeric characters and hyphens,
// matching the asset name validation pattern ^[a-zA-Z0-9_-]+$.
//
// Transformations:
//   - Convert to lowercase
//   - Replace spaces and underscores with hyphens
//   - Remove all other special characters
//   - Collapse consecutive hyphens
//   - Trim leading/trailing hyphens
func Slugify(s string) string {
	// Convert to lowercase
	s = strings.ToLower(s)

	// Replace spaces and underscores with hyphens
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")

	// Remove non-alphanumeric characters (except hyphens)
	var result strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		}
	}

	// Clean up multiple hyphens
	s = result.String()
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}

	// Trim leading/trailing hyphens
	s = strings.Trim(s, "-")

	return s
}
