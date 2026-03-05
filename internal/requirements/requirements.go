package requirements

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Requirement represents a single requirement from the requirements file
type Requirement struct {
	// Original line from the file
	Raw string

	// Type of requirement
	Type RequirementType

	// For registry assets
	Name            string
	VersionSpec     string
	VersionOperator string // ==, >=, >, <=, <, ~=

	// For git sources
	GitURL          string
	GitRef          string
	GitName         string
	GitSubdirectory string

	// For path sources
	Path string

	// For HTTP sources
	URL string

	// For skills.sh sources
	SkillsShOwnerRepo string // e.g., "vercel-labs/agent-skills"
	SkillsShSkillName string // e.g., "find-skills" (empty = whole repo)
}

// RequirementType indicates the type of requirement
type RequirementType string

const (
	RequirementTypeRegistry RequirementType = "registry"
	RequirementTypeGit      RequirementType = "git"
	RequirementTypePath     RequirementType = "path"
	RequirementTypeHTTP     RequirementType = "http"
	RequirementTypeSkillsSh RequirementType = "skills-sh"
)

// Parse parses a requirements file
func Parse(filePath string) ([]Requirement, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open requirements file: %w", err)
	}
	defer file.Close()

	var requirements []Requirement
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse the requirement
		req, err := ParseLine(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}

		req.Raw = line
		requirements = append(requirements, req)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read requirements file: %w", err)
	}

	return requirements, nil
}

// ParseLine parses a single requirement line
func ParseLine(line string) (Requirement, error) {
	line = strings.TrimSpace(line)

	// skills.sh source: skills.sh:owner/repo or skills.sh:owner/repo/skill-name
	if strings.HasPrefix(line, "skills.sh:") {
		return parseSkillsShRequirement(line)
	}

	// Git source: git+https://...@ref#name=...
	if strings.HasPrefix(line, "git+") {
		return parseGitRequirement(line)
	}

	// HTTP source: https://...
	if strings.HasPrefix(line, "https://") || strings.HasPrefix(line, "http://") {
		return Requirement{
			Type: RequirementTypeHTTP,
			URL:  line,
		}, nil
	}

	// Path source: ./..., ~/..., /...
	if strings.HasPrefix(line, "./") || strings.HasPrefix(line, "../") ||
		strings.HasPrefix(line, "~/") || strings.HasPrefix(line, "/") {
		return Requirement{
			Type: RequirementTypePath,
			Path: line,
		}, nil
	}

	// Registry asset: name[version-spec]
	return parseRegistryRequirement(line)
}

// parseSkillsShRequirement parses skills.sh:owner/repo or skills.sh:owner/repo/skill-name
func parseSkillsShRequirement(line string) (Requirement, error) {
	// Remove skills.sh: prefix
	rest := strings.TrimPrefix(line, "skills.sh:")
	if rest == "" {
		return Requirement{}, fmt.Errorf("skills.sh requirement missing owner/repo: %s", line)
	}

	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 {
		return Requirement{}, fmt.Errorf("skills.sh requirement must be skills.sh:owner/repo[/skill-name]: %s", line)
	}

	owner := parts[0]
	repo := parts[1]
	if owner == "" || repo == "" {
		return Requirement{}, fmt.Errorf("skills.sh requirement has empty owner or repo: %s", line)
	}

	ownerRepo := owner + "/" + repo
	skillName := ""
	if len(parts) == 3 {
		skillName = parts[2]
		if skillName == "" {
			return Requirement{}, fmt.Errorf("skills.sh requirement has empty skill name: %s", line)
		}
	}

	return Requirement{
		Type:              RequirementTypeSkillsSh,
		SkillsShOwnerRepo: ownerRepo,
		SkillsShSkillName: skillName,
	}, nil
}

// parseGitRequirement parses git+URL@ref#name=...&path=...
func parseGitRequirement(line string) (Requirement, error) {
	// Remove git+ prefix
	line = strings.TrimPrefix(line, "git+")

	// Split by @ to separate URL and ref+params
	before, after, ok := strings.Cut(line, "@")
	if !ok {
		return Requirement{}, fmt.Errorf("git requirement missing @ref: %s", line)
	}

	url := before
	refAndParams := after

	// Split ref and params by #
	before, after, ok = strings.Cut(refAndParams, "#")
	if !ok {
		return Requirement{}, fmt.Errorf("git requirement missing #name parameter: %s", line)
	}

	ref := before
	params := after

	// Parse parameters
	var name, subdirectory string
	for param := range strings.SplitSeq(params, "&") {
		parts := strings.SplitN(param, "=", 2)
		if len(parts) != 2 {
			return Requirement{}, fmt.Errorf("invalid git parameter: %s", param)
		}

		key := parts[0]
		value := parts[1]

		switch key {
		case "name":
			name = value
		case "path":
			subdirectory = value
		default:
			return Requirement{}, fmt.Errorf("unknown git parameter: %s", key)
		}
	}

	if name == "" {
		return Requirement{}, errors.New("git requirement missing name parameter")
	}

	return Requirement{
		Type:            RequirementTypeGit,
		GitURL:          url,
		GitRef:          ref,
		GitName:         name,
		GitSubdirectory: subdirectory,
	}, nil
}

// parseRegistryRequirement parses name[version-spec]
func parseRegistryRequirement(line string) (Requirement, error) {
	// Check for version operators: ==, >=, >, <=, <, ~=
	operators := []string{"~=", "==", ">=", "<=", "!=", ">", "<"}

	for _, op := range operators {
		if before, after, ok := strings.Cut(line, op); ok {
			name := strings.TrimSpace(before)
			versionSpec := strings.TrimSpace(after)

			return Requirement{
				Type:            RequirementTypeRegistry,
				Name:            name,
				VersionOperator: op,
				VersionSpec:     versionSpec,
			}, nil
		}
	}

	// No operator, just a name (latest version)
	return Requirement{
		Type: RequirementTypeRegistry,
		Name: strings.TrimSpace(line),
	}, nil
}

// String returns a string representation of the requirement
func (r Requirement) String() string {
	switch r.Type {
	case RequirementTypeRegistry:
		if r.VersionOperator != "" {
			return fmt.Sprintf("%s%s%s", r.Name, r.VersionOperator, r.VersionSpec)
		}
		return r.Name
	case RequirementTypeGit:
		result := fmt.Sprintf("git+%s@%s#name=%s", r.GitURL, r.GitRef, r.GitName)
		if r.GitSubdirectory != "" {
			result += "&path=" + r.GitSubdirectory
		}
		return result
	case RequirementTypePath:
		return r.Path
	case RequirementTypeHTTP:
		return r.URL
	case RequirementTypeSkillsSh:
		if r.SkillsShSkillName != "" {
			return fmt.Sprintf("skills.sh:%s/%s", r.SkillsShOwnerRepo, r.SkillsShSkillName)
		}
		return "skills.sh:" + r.SkillsShOwnerRepo
	default:
		return r.Raw
	}
}
