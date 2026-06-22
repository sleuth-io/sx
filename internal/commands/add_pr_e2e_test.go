package commands

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/manifest"
)

// These end-to-end tests drive the REAL `sx add` command against a REAL git
// vault to prove the RBAC-edit-gate PR fallback: when the caller may not publish
// a team-scoped skill directly (docs/rbac.md), `sx add` offers to open a pull
// request. Answering yes pushes the add to a branch on the remote (NOT the
// default branch) and reports a PR; answering no aborts with the permission
// error and pushes nothing.

// seedTeamScopedGitVault creates a bare remote seeded with my-skill@1.0.0 scoped
// to team "platform" — whose sole member is someone OTHER than the test actor
// (test@example.com). It points sx at that git vault and returns the bare repo
// path. The actor is therefore blocked by the edit gate but can still push.
func seedTeamScopedGitVault(t *testing.T, env *TestEnv) string {
	t.Helper()

	bare := env.MkdirAll(env.TempDir + "/remote.git")
	gitRunE2E(t, "", "init", "--bare", "-b", "main", bare)

	seed := env.MkdirAll(env.TempDir + "/seed")
	gitRunE2E(t, "", "init", "-b", "main", seed)
	gitRunE2E(t, seed, "config", "user.email", "test@example.com")
	gitRunE2E(t, seed, "config", "user.name", "Test User")

	env.WriteFile(seed+"/assets/my-skill/1.0.0/metadata.toml", "[asset]\nname = \"my-skill\"\ntype = \"skill\"\nversion = \"1.0.0\"\n")
	env.WriteFile(seed+"/assets/my-skill/1.0.0/SKILL.md", "You are my-skill v1")
	env.WriteFile(seed+"/assets/my-skill/list.txt", "1.0.0\n")
	if err := manifest.Save(seed, &manifest.Manifest{
		SchemaVersion: manifest.CurrentSchemaVersion,
		// platform team does NOT include the actor → edit gate denies them.
		Teams: []manifest.Team{{
			Name:    "platform",
			Members: []string{"other@example.com"},
			Admins:  []string{"other@example.com"},
		}},
		Assets: []manifest.Asset{{
			Name: "my-skill", Version: "1.0.0", Type: asset.TypeSkill,
			Scopes:     []manifest.Scope{{Kind: manifest.ScopeKindTeam, Team: "platform"}},
			SourcePath: &manifest.SourcePath{Path: "./assets/my-skill/1.0.0"},
		}},
	}); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}
	gitRunE2E(t, seed, "add", ".")
	gitRunE2E(t, seed, "commit", "-m", "seed")
	gitRunE2E(t, seed, "remote", "add", "origin", bare)
	gitRunE2E(t, seed, "push", "origin", "main")

	configDir := env.MkdirAll(env.HomeDir + "/.config/sx")
	env.WriteFile(configDir+"/config.json", `{"type":"git","repositoryUrl":"file://`+bare+`"}`)
	env.Chdir(env.HomeDir)

	return bare
}

// writeLocalSkill writes a my-skill source directory with new content (so the add
// is a genuine republish, not an identical no-op) and returns its path.
func writeLocalSkill(t *testing.T, env *TestEnv) string {
	t.Helper()
	dir := env.MkdirAll(filepath.Join(env.TempDir, "local-skill"))
	env.WriteFile(filepath.Join(dir, "metadata.toml"), "[asset]\nname = \"my-skill\"\ntype = \"skill\"\ndescription = \"my skill\"\n\n[skill]\nprompt-file = \"SKILL.md\"\n")
	env.WriteFile(filepath.Join(dir, "SKILL.md"), "You are my-skill v2 — improved")
	return dir
}

// TestAddPR_OpenPRWhenDenied_Yes: a non-member adds a team-scoped skill, is
// offered a PR, says yes — and the add lands on a NEW branch on the remote while
// the default branch is left untouched.
func TestAddPR_OpenPRWhenDenied_Yes(t *testing.T) {
	env := NewTestEnv(t)
	configureGitIdentityForTest(t, env.HomeDir) // actor → test@example.com
	bare := seedTeamScopedGitVault(t, env)
	skillDir := writeLocalSkill(t, env)

	mockPrompter := NewMockPrompter().
		ExpectConfirm("correct", true).      // confirm detected asset
		ExpectConfirm("pull request", true). // YES, open a PR instead
		ExpectPrompt("Version", "1.1.0")     // version for the new release

	addCmd := NewAddCommand()
	addCmd.SetArgs([]string{skillDir, "--no-install"})
	if err := ExecuteWithPrompter(addCmd, mockPrompter); err != nil {
		t.Fatalf("sx add (PR yes) should succeed, got: %v", err)
	}

	// The new version must be committed on the PR branch. The branch name carries
	// a random uniqueness suffix (sx/add-my-skill-1.1.0-<hex>), so resolve it by
	// prefix rather than asserting an exact name.
	branch := findRemoteBranchE2E(t, bare, "sx/add-my-skill-1.1.0-")
	got := gitOutE2E(t, "", "--git-dir", bare, "show", branch+":assets/my-skill/1.1.0/metadata.toml")
	if !strings.Contains(got, `version = "1.1.0"`) {
		t.Fatalf("expected my-skill@1.1.0 on branch %s, got:\n%s", branch, got)
	}

	// ...and the default branch must NOT have it (publish is gated behind merge).
	mainList := gitOutE2E(t, "", "--git-dir", bare, "show", "main:assets/my-skill/list.txt")
	if strings.Contains(mainList, "1.1.0") {
		t.Fatalf("default branch should not carry 1.1.0 before merge, got:\n%s", mainList)
	}
}

// TestAddPR_OpenPRWhenDenied_No: same setup, but the user declines the PR — the
// command fails with the permission error and pushes no branch.
func TestAddPR_OpenPRWhenDenied_No(t *testing.T) {
	env := NewTestEnv(t)
	configureGitIdentityForTest(t, env.HomeDir)
	bare := seedTeamScopedGitVault(t, env)
	skillDir := writeLocalSkill(t, env)

	mockPrompter := NewMockPrompter().
		ExpectConfirm("correct", true).      // confirm detected asset
		ExpectConfirm("pull request", false) // NO, do not open a PR

	addCmd := NewAddCommand()
	addCmd.SetArgs([]string{skillDir, "--no-install"})
	err := ExecuteWithPrompter(addCmd, mockPrompter)
	if err == nil {
		t.Fatal("sx add (PR no) should fail with the permission error")
	}
	if !strings.Contains(err.Error(), "permission denied") || !strings.Contains(err.Error(), "platform") {
		t.Fatalf("expected a team-scope permission error, got: %v", err)
	}

	// Nothing should have been pushed.
	branches := gitOutE2E(t, "", "--git-dir", bare, "branch", "--list")
	if strings.Contains(branches, "sx/add-my-skill") {
		t.Fatalf("declining the PR must push no branch, found:\n%s", branches)
	}
}

// findRemoteBranchE2E returns the single branch in the bare repo whose name
// starts with prefix, failing if zero or more than one match. PR branches carry
// a random suffix, so tests resolve them by prefix instead of an exact name.
func findRemoteBranchE2E(t *testing.T, bare, prefix string) string {
	t.Helper()
	out := gitOutE2E(t, "", "--git-dir", bare, "branch", "--list", prefix+"*", "--format=%(refname:short)")
	var matches []string
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if b := strings.TrimSpace(line); b != "" {
			matches = append(matches, b)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("expected exactly one branch with prefix %q, found %v", prefix, matches)
	}
	return matches[0]
}
