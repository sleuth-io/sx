package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/buildinfo"
	"github.com/sleuth-io/sx/internal/ui/components"
)

// skillsShPattern matches owner/repo or owner/repo/skill-name patterns.
// Owner and repo must be alphanumeric with hyphens/underscores/dots.
var skillsShPattern = regexp.MustCompile(
	`^([a-zA-Z0-9][-a-zA-Z0-9_.]*)/([a-zA-Z0-9][-a-zA-Z0-9_.]*)(?:/([a-zA-Z0-9][-a-zA-Z0-9_.]*))?$`,
)

// isSkillsShReference checks if input looks like an owner/repo or owner/repo/skill-name reference.
func isSkillsShReference(input string) bool {
	return skillsShPattern.MatchString(input)
}

// parseSkillsShReference extracts owner, repo, and optional skill name from a skills.sh reference.
// Input can be "owner/repo", "owner/repo/skill", or "skills.sh:owner/repo[/skill]".
func parseSkillsShReference(input string) (owner, repo, skill string, ok bool) {
	// Strip skills.sh: prefix if present
	input = strings.TrimPrefix(input, "skills.sh:")

	matches := skillsShPattern.FindStringSubmatch(input)
	if matches == nil {
		return "", "", "", false
	}
	return matches[1], matches[2], matches[3], true
}

// skillsShToTreeURL converts a skills.sh reference to a GitHub tree URL using the given branch.
func skillsShToTreeURL(owner, repo, skill, branch string) string {
	if skill != "" {
		return fmt.Sprintf("https://github.com/%s/%s/tree/%s/skills/%s", owner, repo, branch, skill)
	}
	return fmt.Sprintf("https://github.com/%s/%s/tree/%s", owner, repo, branch)
}

// addFromSkillsSh handles adding a skill from a skills.sh owner/repo[/skill] reference.
func addFromSkillsSh(cmd *cobra.Command, input string, opts addOptions) error {
	owner, repo, skill, ok := parseSkillsShReference(input)
	if !ok {
		return fmt.Errorf("invalid skills.sh reference: %s", input)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	status := components.NewStatus(cmd.OutOrStdout())
	status.Start(fmt.Sprintf("Resolving %s/%s", owner, repo))

	branch, err := resolveDefaultBranch(ctx, owner, repo)
	if err != nil {
		status.Fail("Failed to resolve repository")
		return fmt.Errorf("failed to resolve default branch for %s/%s: %w", owner, repo, err)
	}
	status.Done("")

	// If a specific skill was given, convert directly to tree URL and add
	if skill != "" {
		treeURL := skillsShToTreeURL(owner, repo, skill, branch)
		return runAddWithFlags(cmd, treeURL, opts)
	}

	// No specific skill — list available skills and let user pick
	return addFromSkillsShRepo(cmd, owner, repo, branch, opts)
}

// addFromSkillsShRepo lists skills in a repo and lets the user select which to add.
func addFromSkillsShRepo(cmd *cobra.Command, owner, repo, branch string, opts addOptions) error {
	styledOut := newOutputHelper(cmd)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	status := components.NewStatus(cmd.OutOrStdout())
	status.Start(fmt.Sprintf("Fetching skills from %s/%s", owner, repo))

	skills, err := listSkillsShSkills(ctx, owner, repo, branch)
	if err != nil {
		status.Fail("Failed to fetch skills")
		return fmt.Errorf("failed to list skills in %s/%s: %w", owner, repo, err)
	}
	status.Done("")

	if len(skills) == 0 {
		styledOut.println("No skills found in this repository.")
		return nil
	}

	// If non-interactive, we can't prompt
	if opts.isNonInteractive() {
		return fmt.Errorf("specify a skill name: %s/%s/<skill-name>", owner, repo)
	}

	// Let user pick skills in a loop (like browseCommunitySkills)
	var addedAny bool
	for {
		styledOut.println()

		options := make([]components.Option, len(skills)+1)
		options[0] = components.Option{
			Label: "Done",
			Value: "done",
		}
		for i, s := range skills {
			options[i+1] = components.Option{
				Label: s,
				Value: s,
			}
		}

		selected, err := components.SelectWithDefault("Select a skill to add:", options, 0)
		if err != nil || selected.Value == "done" {
			break
		}

		styledOut.println()

		treeURL := skillsShToTreeURL(owner, repo, selected.Value, branch)
		if err := runAddSkipInstall(cmd, treeURL); err != nil {
			styledOut.printfErr("Failed to add skill: %v\n", err)
		} else {
			addedAny = true
		}
	}

	if addedAny {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel2()
		promptRunInstall(cmd, ctx2, styledOut)
	}

	return nil
}

// resolveDefaultBranch queries the GitHub API to get the default branch for a repository.
func resolveDefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", buildinfo.GetUserAgent())
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GitHub API error %d: %s", resp.StatusCode, string(body))
	}

	var repoInfo struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&repoInfo); err != nil {
		return "", err
	}

	if repoInfo.DefaultBranch == "" {
		return "main", nil
	}
	return repoInfo.DefaultBranch, nil
}

// listSkillsShSkills lists the skill directories in a skills.sh repo's skills/ directory.
func listSkillsShSkills(ctx context.Context, owner, repo, branch string) ([]string, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/skills?ref=%s", owner, repo, branch)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", buildinfo.GetUserAgent())
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error %d: %s", resp.StatusCode, string(body))
	}

	var contents []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&contents); err != nil {
		return nil, err
	}

	var skills []string
	for _, item := range contents {
		if item.Type == "dir" {
			skills = append(skills, item.Name)
		}
	}
	return skills, nil
}
