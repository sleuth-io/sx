package vault

import (
	"slices"
	"testing"
)

// TestGHPRCreateArgsPinsRepo is a regression test for the snap-confined gh bug:
// sx keeps its clone under a hidden cache dir (~/.cache/sx/git-repos/...), which a
// snap-installed gh cannot read. Without --repo, `gh pr create` falls back to
// cwd-based detection, fails with "fatal: not a git repository", and no PR opens.
// Pinning --repo makes gh use the API instead. If this test fails because --repo
// went missing, that bug is back — do not "fix" the test by dropping the flag.
func TestGHPRCreateArgsPinsRepo(t *testing.T) {
	cases := []struct {
		name     string
		repoURL  string
		wantRepo string
	}{
		{"https", "https://github.com/sleuth-io/skills-repository", "github.com/sleuth-io/skills-repository"},
		{"https with .git", "https://github.com/sleuth-io/skills-repository.git", "github.com/sleuth-io/skills-repository"},
		{"ssh", "git@github.com:sleuth-io/skills-repository.git", "github.com/sleuth-io/skills-repository"},
		{"ssh scheme", "ssh://git@github.com/sleuth-io/skills-repository.git", "github.com/sleuth-io/skills-repository"},
		{"enterprise host", "https://ghe.example.com/team/repo.git", "ghe.example.com/team/repo"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := ghPRCreateArgs(tc.repoURL, "main", "sx/add-foo-1", "Add foo 1", "body")

			i := slices.Index(args, "--repo")
			if i < 0 {
				t.Fatalf("ghPRCreateArgs must include --repo so gh resolves via the API and not the sandboxed local clone; got %v", args)
			}
			if i+1 >= len(args) {
				t.Fatalf("--repo has no value in %v", args)
			}
			if got := args[i+1]; got != tc.wantRepo {
				t.Errorf("--repo value = %q, want %q", got, tc.wantRepo)
			}

			// The PR contents must still be passed through.
			for _, want := range []string{"pr", "create", "--base", "main", "--head", "sx/add-foo-1"} {
				if !slices.Contains(args, want) {
					t.Errorf("missing arg %q in %v", want, args)
				}
			}
		})
	}
}

// TestRepoSlugEmptyWhenUnknown documents that an unparseable URL yields no slug,
// so ghPRCreateArgs simply omits --repo rather than passing a bogus value.
func TestRepoSlugEmptyWhenUnknown(t *testing.T) {
	if got := repoSlug(""); got != "" {
		t.Errorf("repoSlug(\"\") = %q, want empty", got)
	}
	if slices.Contains(ghPRCreateArgs("", "main", "h", "t", "b"), "--repo") {
		t.Error("ghPRCreateArgs should omit --repo when the slug is unknown")
	}
}
