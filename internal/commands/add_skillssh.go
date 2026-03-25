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
	"github.com/sleuth-io/sx/internal/github"
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

// repoAsset represents an asset discovered in a GitHub repository.
type repoAsset struct {
	Name string // e.g. "code-reviewer"
	Type string // e.g. "agent", "skill"
	Dir  string // path in repo, e.g. "agents/code-reviewer"
}

// assetDirTypes maps top-level directory names in a repo to asset type labels.
var assetDirTypes = []struct {
	Dir  string
	Type string
}{
	{"skills", "skill"},
	{"agents", "agent"},
	{"commands", "command"},
	{"rules", "rule"},
	{"hooks", "hook"},
	{"mcp", "mcp"},
}

// repoAssetToGitHubURL converts a repo asset to a GitHub URL.
// Uses blob URLs for single files (.md) and tree URLs for directories.
func repoAssetToGitHubURL(owner, repo, assetDir, branch string) string {
	if assetDir == "" {
		return fmt.Sprintf("https://github.com/%s/%s/tree/%s", owner, repo, branch)
	}
	if strings.HasSuffix(assetDir, ".md") {
		return fmt.Sprintf("https://github.com/%s/%s/blob/%s/%s", owner, repo, branch, assetDir)
	}
	return fmt.Sprintf("https://github.com/%s/%s/tree/%s/%s", owner, repo, branch, assetDir)
}

// skillsShToTreeURL converts a skills.sh reference to a GitHub tree URL using the given branch.
func skillsShToTreeURL(owner, repo, skill, branch string) string {
	if skill != "" {
		return fmt.Sprintf("https://github.com/%s/%s/tree/%s/skills/%s", owner, repo, branch, skill)
	}
	return fmt.Sprintf("https://github.com/%s/%s/tree/%s", owner, repo, branch)
}

// addFromSkillsSh handles adding an asset from a skills.sh owner/repo[/asset] reference.
func addFromSkillsSh(cmd *cobra.Command, input string, opts addOptions) error {
	owner, repo, assetName, ok := parseSkillsShReference(input)
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

	// If a specific asset name was given, find it across all asset directories.
	if assetName != "" {
		// First try the skills-specific resolution (handles skillId != dir name).
		status.Start("Resolving asset directory for " + assetName)
		resolvedDir, err := github.ResolveSkillDirectory(ctx, owner, repo, branch, assetName)
		if err == nil {
			status.Done("")
			treeURL := skillsShToTreeURL(owner, repo, resolvedDir, branch)
			return runAddWithFlags(cmd, treeURL, opts)
		}

		// Not found in skills/ — search other asset directories.
		found, searchErr := resolveAssetDirectory(ctx, owner, repo, branch, assetName)
		if searchErr != nil {
			status.Fail("Could not find asset")
			return fmt.Errorf("failed to resolve asset directory for %s: %w", assetName, searchErr)
		}
		status.Done("")
		treeURL := repoAssetToGitHubURL(owner, repo, found.Dir, branch)
		return runAddWithFlags(cmd, treeURL, opts)
	}

	// No specific asset — list all available assets and let user pick
	return addFromSkillsShRepo(cmd, owner, repo, branch, opts)
}

// addFromSkillsShRepo lists all assets in a repo and lets the user select which to add.
func addFromSkillsShRepo(cmd *cobra.Command, owner, repo, branch string, opts addOptions) error {
	styledOut := newOutputHelper(cmd)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	status := components.NewStatus(cmd.OutOrStdout())
	status.Start(fmt.Sprintf("Fetching assets from %s/%s", owner, repo))

	assets, err := listRepoAssets(ctx, owner, repo, branch)
	if err != nil {
		status.Fail("Failed to fetch assets")
		return fmt.Errorf("failed to list assets in %s/%s: %w", owner, repo, err)
	}
	status.Done("")

	// Filter by --type if specified
	if opts.Type != "" {
		var filtered []repoAsset
		for _, a := range assets {
			if a.Type == opts.Type {
				filtered = append(filtered, a)
			}
		}
		assets = filtered
	}

	if len(assets) == 0 {
		styledOut.println("No assets found in this repository.")
		return nil
	}

	// If non-interactive, we can't prompt
	if opts.isNonInteractive() {
		return fmt.Errorf("specify an asset name: %s/%s/<asset-name>", owner, repo)
	}

	// Let user pick assets in a loop
	var addedAny bool
	for {
		styledOut.println()

		options := make([]components.Option, len(assets)+1)
		options[0] = components.Option{
			Label: "Done",
			Value: "done",
		}
		for i, a := range assets {
			label := fmt.Sprintf("%s (%s)", a.Name, a.Type)
			options[i+1] = components.Option{
				Label: label,
				Value: a.Dir,
			}
		}

		selected, err := components.SelectWithDefault("Select an asset to add:", options, 0)
		if err != nil || selected.Value == "done" {
			break
		}

		styledOut.println()

		treeURL := repoAssetToGitHubURL(owner, repo, selected.Value, branch)
		if err := runAddSkipInstall(cmd, treeURL); err != nil {
			styledOut.printfErr("Failed to add asset: %v\n", err)
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

// listRepoAssets lists all assets across known asset directories in a GitHub repo.
// Uses a single Git Trees API call to fetch the entire repo tree, then filters client-side.
func listRepoAssets(ctx context.Context, owner, repo, branch string) ([]repoAsset, error) {
	tree, err := fetchGitTree(ctx, owner, repo, branch)
	if err != nil {
		return nil, err
	}
	return assetsFromTree(tree), nil
}

// resolveAssetDirectory searches all asset directories for an asset by name.
func resolveAssetDirectory(ctx context.Context, owner, repo, branch, assetName string) (*repoAsset, error) {
	tree, err := fetchGitTree(ctx, owner, repo, branch)
	if err != nil {
		return nil, err
	}
	for _, a := range assetsFromTree(tree) {
		if a.Name == assetName {
			return &a, nil
		}
	}
	return nil, fmt.Errorf("asset %q not found in any known directory", assetName)
}

// assetsFromTree extracts assets from a git tree by matching known asset directory patterns.
// Matches entries like "skills/foo" (dir) or "agents/bar.md" (file) at exactly one level deep.
func assetsFromTree(tree []gitTreeEntry) []repoAsset {
	// Build lookup: asset dir name -> type label
	dirToType := make(map[string]string, len(assetDirTypes))
	for _, dt := range assetDirTypes {
		dirToType[dt.Dir] = dt.Type
	}

	var assets []repoAsset
	for _, entry := range tree {
		parts := strings.SplitN(entry.Path, "/", 3)
		if len(parts) != 2 {
			continue // Only match "dir/name", not deeper paths
		}
		assetType, ok := dirToType[parts[0]]
		if !ok {
			continue
		}
		name := parts[1]
		// Include directories (tree) and .md files (blob)
		if entry.Type == "tree" {
			assets = append(assets, repoAsset{Name: name, Type: assetType, Dir: entry.Path})
		} else if entry.Type == "blob" && strings.HasSuffix(name, ".md") {
			assets = append(assets, repoAsset{Name: strings.TrimSuffix(name, ".md"), Type: assetType, Dir: entry.Path})
		}
	}
	return assets
}

// gitTreeEntry represents an item from the GitHub Git Trees API.
type gitTreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"` // "blob" or "tree"
}

// fetchGitTree fetches the full repo tree in a single API call.
func fetchGitTree(ctx context.Context, owner, repo, ref string) ([]gitTreeEntry, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1", owner, repo, ref)

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

	var result struct {
		Tree []gitTreeEntry `json:"tree"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Tree, nil
}
