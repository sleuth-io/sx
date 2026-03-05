// Package registry provides access to the skills.sh skill directory.
package registry

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/sleuth-io/sx/internal/buildinfo"
)

// Skill represents a skill from the skills.sh directory.
type Skill struct {
	Source   string // e.g., "anthropics/skills"
	SkillID string // e.g., "frontend-design"
	Name     string // e.g., "frontend-design"
	Installs int    // e.g., 123897
}

// TreeURL returns the GitHub tree URL for this skill.
func (s Skill) TreeURL(branch string) string {
	return fmt.Sprintf("https://github.com/%s/tree/%s/skills/%s", s.Source, branch, s.SkillID)
}

// FormatInstalls returns a human-readable install count (e.g., "123.9K", "1.2M").
func (s Skill) FormatInstalls() string {
	if s.Installs >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(s.Installs)/1_000_000)
	}
	if s.Installs >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(s.Installs)/1_000)
	}
	return fmt.Sprintf("%d", s.Installs)
}

var skillPattern = regexp.MustCompile(
	`\{"source":"([^"]+)","skillId":"([^"]+)","name":"([^"]+)","installs":(\d+)\}`,
)

// FetchSkills fetches the full skills directory from skills.sh.
func FetchSkills(ctx context.Context) ([]Skill, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://skills.sh", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", buildinfo.GetUserAgent())
	// Request RSC payload for clean JSON data
	req.Header.Set("RSC", "1")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch skills.sh: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("skills.sh returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return ParseSkillsResponse(string(body))
}

// ParseSkillsResponse parses skills from a skills.sh RSC response body.
func ParseSkillsResponse(body string) ([]Skill, error) {
	matches := skillPattern.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil, fmt.Errorf("no skills found in skills.sh response")
	}

	skills := make([]Skill, 0, len(matches))
	for _, m := range matches {
		installs := 0
		fmt.Sscanf(m[4], "%d", &installs)
		skills = append(skills, Skill{
			Source:   m[1],
			SkillID:  m[2],
			Name:     m[3],
			Installs: installs,
		})
	}

	return skills, nil
}

// Search filters skills by a query string, matching against name and source.
func Search(skills []Skill, query string) []Skill {
	if query == "" {
		return skills
	}
	query = strings.ToLower(query)
	var results []Skill
	for _, s := range skills {
		if strings.Contains(strings.ToLower(s.Name), query) ||
			strings.Contains(strings.ToLower(s.Source), query) {
			results = append(results, s)
		}
	}
	return results
}
