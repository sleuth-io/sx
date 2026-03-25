// Package github provides utilities for working with GitHub URLs and the GitHub API.
package github

import (
	"fmt"
	"regexp"
	"strings"
)

// TreeURL represents a parsed GitHub tree URL pointing to a directory.
// Example: https://github.com/metabase/metabase/tree/master/.claude/skills/docs-write
type TreeURL struct {
	Owner string // Repository owner (e.g., "metabase")
	Repo  string // Repository name (e.g., "metabase")
	Ref   string // Branch, tag, or commit (e.g., "master")
	Path  string // Path within the repository (e.g., ".claude/skills/docs-write")
}

// treeURLPattern matches GitHub tree URLs.
// Captures: owner, repo, ref, path
var treeURLPattern = regexp.MustCompile(
	`^https?://github\.com/([^/]+)/([^/]+)/tree/([^/]+)(?:/(.*))?$`,
)

// blobURLPattern matches GitHub blob URLs (single files).
// Captures: owner, repo, ref, path
var blobURLPattern = regexp.MustCompile(
	`^https?://github\.com/([^/]+)/([^/]+)/blob/([^/]+)/(.+)$`,
)

// ParseTreeURL parses a GitHub tree URL into its components.
// Returns nil if the URL is not a valid GitHub tree URL.
func ParseTreeURL(url string) *TreeURL {
	url = strings.TrimSuffix(url, "/")

	matches := treeURLPattern.FindStringSubmatch(url)
	if matches == nil {
		return nil
	}

	return &TreeURL{
		Owner: matches[1],
		Repo:  matches[2],
		Ref:   matches[3],
		Path:  matches[4], // May be empty for root
	}
}

// IsTreeURL checks if a URL is a GitHub tree URL (directory).
func IsTreeURL(url string) bool {
	return treeURLPattern.MatchString(strings.TrimSuffix(url, "/"))
}

// IsBlobURL checks if a URL is a GitHub blob URL (single file).
func IsBlobURL(url string) bool {
	return blobURLPattern.MatchString(url)
}

// ParseBlobURL parses a GitHub blob URL into a TreeURL (reusing the same struct).
// Returns nil if the URL is not a valid GitHub blob URL.
func ParseBlobURL(url string) *TreeURL {
	matches := blobURLPattern.FindStringSubmatch(url)
	if matches == nil {
		return nil
	}
	return &TreeURL{
		Owner: matches[1],
		Repo:  matches[2],
		Ref:   matches[3],
		Path:  matches[4],
	}
}

// IsGitHubURL checks if a URL is any kind of GitHub URL we can handle.
func IsGitHubURL(url string) bool {
	return IsTreeURL(url) || IsBlobURL(url)
}

// ContentsAPIURL returns the GitHub API URL for listing directory contents.
// Example: https://api.github.com/repos/metabase/metabase/contents/.claude/skills/docs-write?ref=master
func (t *TreeURL) ContentsAPIURL() string {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents", t.Owner, t.Repo)
	if t.Path != "" {
		url += "/" + t.Path
	}
	url += "?ref=" + t.Ref
	return url
}

// RawURL returns the raw.githubusercontent.com URL for a file within this tree.
// Example: https://raw.githubusercontent.com/metabase/metabase/master/.claude/skills/docs-write/SKILL.md
func (t *TreeURL) RawURL(filename string) string {
	path := filename
	if t.Path != "" {
		path = t.Path + "/" + filename
	}
	return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s",
		t.Owner, t.Repo, t.Ref, path)
}

// String returns the original GitHub tree URL.
func (t *TreeURL) String() string {
	url := fmt.Sprintf("https://github.com/%s/%s/tree/%s", t.Owner, t.Repo, t.Ref)
	if t.Path != "" {
		url += "/" + t.Path
	}
	return url
}

// SkillName returns a suggested name for the skill based on the path.
// Uses the last component of the path, or repo name if path is empty.
func (t *TreeURL) SkillName() string {
	if t.Path == "" {
		return t.Repo
	}
	parts := strings.Split(t.Path, "/")
	return parts[len(parts)-1]
}
