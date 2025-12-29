package version

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// Version represents a semantic version
type Version struct {
	Major int
	Minor int
	Patch int
	Pre   string // Pre-release suffix (e.g., "alpha", "beta")
	Build string // Build metadata (e.g., "+20250120")
}

// Parse parses a semantic version string
func Parse(v string) (*Version, error) {
	// Handle build metadata (e.g., "1.2.3+20250120")
	var build string
	if idx := strings.Index(v, "+"); idx != -1 {
		build = v[idx+1:]
		v = v[:idx]
	}

	// Handle pre-release (e.g., "1.2.3-alpha")
	var pre string
	if idx := strings.Index(v, "-"); idx != -1 {
		pre = v[idx+1:]
		v = v[:idx]
	}

	// Parse major.minor.patch
	parts := strings.Split(v, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return nil, fmt.Errorf("invalid version format: %s", v)
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid major version: %s", parts[0])
	}

	minor := 0
	if len(parts) >= 2 {
		minor, err = strconv.Atoi(parts[1])
		if err != nil {
			return nil, fmt.Errorf("invalid minor version: %s", parts[1])
		}
	}

	patch := 0
	if len(parts) >= 3 {
		patch, err = strconv.Atoi(parts[2])
		if err != nil {
			return nil, fmt.Errorf("invalid patch version: %s", parts[2])
		}
	}

	return &Version{
		Major: major,
		Minor: minor,
		Patch: patch,
		Pre:   pre,
		Build: build,
	}, nil
}

// String returns the string representation of the version
func (v *Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		s += "-" + v.Pre
	}
	if v.Build != "" {
		s += "+" + v.Build
	}
	return s
}

// Compare compares two versions
// Returns -1 if v < other, 0 if v == other, 1 if v > other
func (v *Version) Compare(other *Version) int {
	if v.Major != other.Major {
		if v.Major < other.Major {
			return -1
		}
		return 1
	}

	if v.Minor != other.Minor {
		if v.Minor < other.Minor {
			return -1
		}
		return 1
	}

	if v.Patch != other.Patch {
		if v.Patch < other.Patch {
			return -1
		}
		return 1
	}

	// Pre-release versions have lower precedence
	if v.Pre != "" && other.Pre == "" {
		return -1
	}
	if v.Pre == "" && other.Pre != "" {
		return 1
	}
	if v.Pre != other.Pre {
		return strings.Compare(v.Pre, other.Pre)
	}

	// Build metadata is ignored in precedence comparison
	return 0
}

// Specifier represents a version specifier (e.g., ">=1.2.3")
type Specifier struct {
	Operator string
	Version  *Version
}

// ParseSpecifier parses a version specifier
func ParseSpecifier(spec string) (*Specifier, error) {
	spec = strings.TrimSpace(spec)

	operators := []string{"~=", "==", ">=", "<=", "!=", ">", "<"}
	for _, op := range operators {
		if strings.HasPrefix(spec, op) {
			versionStr := strings.TrimSpace(spec[len(op):])
			version, err := Parse(versionStr)
			if err != nil {
				return nil, err
			}
			return &Specifier{
				Operator: op,
				Version:  version,
			}, nil
		}
	}

	// No operator, treat as exact version
	version, err := Parse(spec)
	if err != nil {
		return nil, err
	}
	return &Specifier{
		Operator: "==",
		Version:  version,
	}, nil
}

// Matches checks if a version matches the specifier
func (s *Specifier) Matches(v *Version) bool {
	cmp := v.Compare(s.Version)

	switch s.Operator {
	case "==":
		return cmp == 0
	case "!=":
		return cmp != 0
	case ">":
		return cmp > 0
	case ">=":
		return cmp >= 0
	case "<":
		return cmp < 0
	case "<=":
		return cmp <= 0
	case "~=":
		// Compatible release: >= version, < next minor
		if cmp < 0 {
			return false
		}
		// Check if major and minor match
		return v.Major == s.Version.Major && v.Minor == s.Version.Minor
	default:
		return false
	}
}

// Filter filters a list of version strings by the specifier
func (s *Specifier) Filter(versions []string) ([]string, error) {
	var matched []string

	for _, vStr := range versions {
		v, err := Parse(vStr)
		if err != nil {
			// Skip invalid versions
			continue
		}

		if s.Matches(v) {
			matched = append(matched, vStr)
		}
	}

	return matched, nil
}

// SelectBest selects the best (highest) version from a list
func SelectBest(versions []string) (string, error) {
	if len(versions) == 0 {
		return "", fmt.Errorf("no versions available")
	}

	// Parse all versions
	parsed := make([]*Version, 0, len(versions))
	versionMap := make(map[string]string) // version.String() -> original string

	for _, vStr := range versions {
		v, err := Parse(vStr)
		if err != nil {
			continue // Skip invalid versions
		}
		parsed = append(parsed, v)
		versionMap[v.String()] = vStr
	}

	if len(parsed) == 0 {
		return "", fmt.Errorf("no valid versions found")
	}

	// Sort versions in descending order
	sort.Slice(parsed, func(i, j int) bool {
		return parsed[i].Compare(parsed[j]) > 0
	})

	// Return the highest version (first after sorting descending)
	best := parsed[0]
	return versionMap[best.String()], nil
}

// ParseMultipleSpecifiers parses comma-separated specifiers (e.g., ">=1.0,<2.0")
func ParseMultipleSpecifiers(spec string) ([]*Specifier, error) {
	if spec == "" {
		return nil, nil
	}

	parts := strings.Split(spec, ",")
	specifiers := make([]*Specifier, 0, len(parts))

	for _, part := range parts {
		s, err := ParseSpecifier(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		specifiers = append(specifiers, s)
	}

	return specifiers, nil
}

// FilterByMultiple filters versions by multiple specifiers (all must match)
func FilterByMultiple(versions []string, specifiers []*Specifier) ([]string, error) {
	if len(specifiers) == 0 {
		return versions, nil
	}

	result := versions
	for _, spec := range specifiers {
		var err error
		result, err = spec.Filter(result)
		if err != nil {
			return nil, err
		}
	}

	return result, nil
}

// Sort sorts a list of version strings in ascending order (oldest first) using semantic versioning rules.
// Invalid versions are placed at the end in their original order.
func Sort(versions []string) []string {
	if len(versions) == 0 {
		return versions
	}

	// Parse all versions using semver
	type versionPair struct {
		parsed   *semver.Version
		original string
	}

	parsed := make([]versionPair, 0, len(versions))
	invalid := make([]string, 0)

	for _, vStr := range versions {
		v, err := semver.NewVersion(vStr)
		if err != nil {
			invalid = append(invalid, vStr) // Keep invalid at end
			continue
		}
		parsed = append(parsed, versionPair{parsed: v, original: vStr})
	}

	// Sort versions in ascending order (oldest first)
	sort.Slice(parsed, func(i, j int) bool {
		return parsed[i].parsed.LessThan(parsed[j].parsed)
	})

	// Build result
	result := make([]string, 0, len(versions))
	for _, pair := range parsed {
		result = append(result, pair.original)
	}
	result = append(result, invalid...)

	return result
}

// IncrementMajor returns the next major version, preserving the original format.
// Examples: "1" -> "2", "1.0" -> "2.0", "1.0.0" -> "2.0.0"
// If the version cannot be parsed, returns "2" as a fallback.
func IncrementMajor(currentVersion string) string {
	v, err := semver.NewVersion(currentVersion)
	if err != nil {
		// Can't parse as semver, return "2" as fallback
		return "2"
	}

	// Increment major version
	next := v.IncMajor()

	// Preserve original format by counting dots
	dots := strings.Count(currentVersion, ".")
	switch dots {
	case 0:
		// Simple integer format (e.g., "1" -> "2")
		return fmt.Sprintf("%d", next.Major())
	case 1:
		// X.Y format (e.g., "1.0" -> "2.0")
		return fmt.Sprintf("%d.%d", next.Major(), next.Minor())
	default:
		// X.Y.Z format or more (e.g., "1.0.0" -> "2.0.0")
		return next.String()
	}
}
