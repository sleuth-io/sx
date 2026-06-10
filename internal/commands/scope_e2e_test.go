package commands

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// These are end-to-end tests for scoping: they drive the REAL `sx install`
// command against a REAL on-disk vault and assert the actual side effects —
// the scope rows written to sx.toml, and (for the git vault) the commit pushed
// to the remote. They verify that the thing actually happened, not just that a
// function returned the right value.

// seedScopeVault sets up a sandboxed path vault with one registered asset and a
// real git identity (so vault mutations, which require a real user.email, work).
// Returns the env and the vault directory.
func seedScopeVault(t *testing.T) (*TestEnv, string) {
	t.Helper()
	env := NewTestEnv(t)
	configureGitIdentityForTest(t, env.HomeDir) // actor → test@example.com

	vaultDir := env.SetupPathVault()
	env.AddSkillToVault(vaultDir, "my-skill", "1.0.0")
	env.WriteLockFile(vaultDir, `lock-version = "1"
version = "1.0.0"
created-by = "test"

[[assets]]
name = "my-skill"
version = "1.0.0"
type = "skill"

[assets.source-path]
path = "assets/my-skill/1.0.0"
`)
	// Run from home so no ambient git project influences scope resolution.
	env.Chdir(env.HomeDir)
	return env, vaultDir
}

// manifestScopes loads the vault's sx.toml and returns the named asset's scopes.
func manifestScopes(t *testing.T, vaultDir, name string) []manifest.Scope {
	t.Helper()
	m, ok, err := manifest.Load(vaultDir)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if !ok || m == nil {
		t.Fatalf("no manifest at %s", vaultDir)
	}
	a := m.FindAsset(name)
	if a == nil {
		t.Fatalf("asset %q not in manifest", name)
	}
	return a.Scopes
}

func hasScope(scopes []manifest.Scope, kind manifest.ScopeKind, value string) bool {
	for _, s := range scopes {
		if s.Kind != kind {
			continue
		}
		switch kind {
		case manifest.ScopeKindUser:
			if s.User == value {
				return true
			}
		case manifest.ScopeKindTeam:
			if s.Team == value {
				return true
			}
		case manifest.ScopeKindBot:
			if s.Bot == value {
				return true
			}
		case manifest.ScopeKindRepo, manifest.ScopeKindPath:
			if s.Repo == value {
				return true
			}
		case manifest.ScopeKindOrg:
			// org has no value to match
		}
	}
	return false
}

// TestScopeE2E_UserScopeWrittenToManifest: `sx install <name> --user me`
// resolves "me" to the caller's git identity and actually records a user scope
// in sx.toml.
func TestScopeE2E_UserScopeWrittenToManifest(t *testing.T) {
	_, vaultDir := seedScopeVault(t)

	cmd := NewInstallCommand()
	cmd.SetArgs([]string{"my-skill", "--user", "me", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("install --user me: %v", err)
	}

	scopes := manifestScopes(t, vaultDir, "my-skill")
	if !hasScope(scopes, manifest.ScopeKindUser, "test@example.com") {
		t.Fatalf("expected a user scope for test@example.com, got %+v", scopes)
	}
}

// TestScopeE2E_UserScopedAssetInstallsForCaller proves the full chain: scope an
// asset to yourself, then run install, and the asset actually lands on disk in
// your global client dir (a user-scope matching the caller resolves to a global
// install for that user).
func TestScopeE2E_UserScopedAssetInstallsForCaller(t *testing.T) {
	env, _ := seedScopeVault(t)

	// Scope to me.
	set := NewInstallCommand()
	set.SetArgs([]string{"my-skill", "--user", "me", "--yes"})
	if err := set.Execute(); err != nil {
		t.Fatalf("install --user me: %v", err)
	}

	// Now run a normal install — the user-scoped asset should install for us.
	inst := NewInstallCommand()
	inst.SetArgs([]string{})
	if err := inst.Execute(); err != nil {
		t.Fatalf("install: %v", err)
	}

	env.AssertFileExists(env.GlobalClaudeDir() + "/skills/my-skill")
}

// TestScopeE2E_TeamScopeByNonAdminWritesToManifest: scoping an asset to a team
// you are NOT an admin of succeeds and records the team scope (SD-10170: scoping
// is distribution, not team management).
func TestScopeE2E_TeamScopeByNonAdminWritesToManifest(t *testing.T) {
	_, vaultDir := seedScopeVault(t)

	// Create a team admined by someone else, directly via the vault API.
	v, err := vaultpkg.NewPathVault("file://" + vaultDir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	if err := v.CreateTeam(context.Background(), mgmt.Team{
		Name:    "platform",
		Members: []string{"other@example.com"},
		Admins:  []string{"other@example.com"},
	}); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	// The caller (test@example.com) is not an admin, but scoping must still work.
	cmd := NewInstallCommand()
	cmd.SetArgs([]string{"my-skill", "--team", "platform", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("install --team platform by non-admin should succeed: %v", err)
	}

	if scopes := manifestScopes(t, vaultDir, "my-skill"); !hasScope(scopes, manifest.ScopeKindTeam, "platform") {
		t.Fatalf("expected a team scope for platform, got %+v", scopes)
	}
}

// TestScopeE2E_NonexistentTeamNotWritten: scoping to a team that doesn't exist
// must fail and leave the asset's scopes untouched.
func TestScopeE2E_NonexistentTeamNotWritten(t *testing.T) {
	_, vaultDir := seedScopeVault(t)

	cmd := NewInstallCommand()
	cmd.SetArgs([]string{"my-skill", "--team", "ghost", "--yes"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	if err := cmd.Execute(); err == nil {
		t.Fatal("install --team ghost should fail (team not found)")
	}

	if scopes := manifestScopes(t, vaultDir, "my-skill"); hasScope(scopes, manifest.ScopeKindTeam, "ghost") {
		t.Fatalf("nonexistent team must not be recorded, got %+v", scopes)
	}
}

// TestScopeE2E_GitVaultScopePushedToRemote is the strongest end-to-end check:
// scoping an asset against a git vault via the real command must COMMIT AND PUSH
// the scope to the remote, not just touch the local clone. It asserts the bare
// remote's sx.toml carries the user scope afterward.
func TestScopeE2E_GitVaultScopePushedToRemote(t *testing.T) {
	env := NewTestEnv(t)
	configureGitIdentityForTest(t, env.HomeDir)

	// Bare "remote" seeded with my-skill (no scopes) via a working clone.
	bare := env.MkdirAll(env.TempDir + "/remote.git")
	gitRunE2E(t, "", "init", "--bare", "-b", "main", bare)
	seed := env.MkdirAll(env.TempDir + "/seed")
	gitRunE2E(t, "", "init", "-b", "main", seed)
	gitRunE2E(t, seed, "config", "user.email", "test@example.com")
	gitRunE2E(t, seed, "config", "user.name", "Test User")
	// Asset files + version list so GetAssetDetails finds my-skill.
	env.WriteFile(seed+"/assets/my-skill/1.0.0/metadata.toml", "[asset]\nname = \"my-skill\"\ntype = \"skill\"\nversion = \"1.0.0\"\n")
	env.WriteFile(seed+"/assets/my-skill/1.0.0/SKILL.md", "You are my-skill")
	env.WriteFile(seed+"/assets/my-skill/list.txt", "1.0.0\n")
	if err := manifest.Save(seed, &manifest.Manifest{
		SchemaVersion: manifest.CurrentSchemaVersion,
		Assets: []manifest.Asset{{
			Name: "my-skill", Version: "1.0.0", Type: asset.TypeSkill,
			SourcePath: &manifest.SourcePath{Path: "./assets/my-skill/1.0.0"},
		}},
	}); err != nil {
		t.Fatalf("seed manifest: %v", err)
	}
	gitRunE2E(t, seed, "add", ".")
	gitRunE2E(t, seed, "commit", "-m", "seed")
	gitRunE2E(t, seed, "remote", "add", "origin", bare)
	gitRunE2E(t, seed, "push", "origin", "main")

	// Point sx at the git vault.
	configDir := env.MkdirAll(env.HomeDir + "/.config/sx")
	env.WriteFile(configDir+"/config.json", `{"type":"git","repositoryUrl":"file://`+bare+`"}`)
	env.Chdir(env.HomeDir)

	cmd := NewInstallCommand()
	cmd.SetArgs([]string{"my-skill", "--user", "me", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("install --user me on git vault: %v", err)
	}

	// The scope must have been pushed to the bare remote, not just applied locally.
	remoteToml := gitOutE2E(t, "", "--git-dir", bare, "show", "main:sx.toml")
	if !strings.Contains(remoteToml, `kind = "user"`) || !strings.Contains(remoteToml, "test@example.com") {
		t.Fatalf("expected user scope pushed to the remote sx.toml, got:\n%s", remoteToml)
	}
}

// TestScopeE2E_AddFlagsApplyScopeWithYes: `sx add <name> --team <t> --yes`
// pre-fills the team scope and (with --yes) applies it without prompting — the
// scope actually lands in sx.toml. Proves the add command's unified scope flags
// are wired through resolveScopeFlags to the real apply path.
func TestScopeE2E_AddFlagsApplyScopeWithYes(t *testing.T) {
	_, vaultDir := seedScopeVault(t)

	// A team to scope to (admined by someone else — non-admin scoping is allowed).
	v, err := vaultpkg.NewPathVault("file://" + vaultDir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	if err := v.CreateTeam(context.Background(), mgmt.Team{
		Name: "platform", Members: []string{"other@example.com"}, Admins: []string{"other@example.com"},
	}); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	cmd := NewAddCommand()
	cmd.SetArgs([]string{"my-skill", "--team", "platform", "--user", "me", "--yes", "--no-install"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add --team platform --user me --yes: %v", err)
	}

	scopes := manifestScopes(t, vaultDir, "my-skill")
	if !hasScope(scopes, manifest.ScopeKindTeam, "platform") {
		t.Errorf("expected team scope platform, got %+v", scopes)
	}
	if !hasScope(scopes, manifest.ScopeKindUser, "test@example.com") {
		t.Errorf("expected user scope for caller, got %+v", scopes)
	}
}

// TestScopeE2E_AddOrgFlagGoesGlobal: `sx add <name> --org --yes` clears scopes
// (global).
func TestScopeE2E_AddOrgFlagGoesGlobal(t *testing.T) {
	_, vaultDir := seedScopeVault(t)

	// First give it a non-global scope so we can see --org replace it.
	set := NewInstallCommand()
	set.SetArgs([]string{"my-skill", "--user", "me", "--yes"})
	if err := set.Execute(); err != nil {
		t.Fatalf("seed user scope: %v", err)
	}

	cmd := NewAddCommand()
	cmd.SetArgs([]string{"my-skill", "--org", "--yes", "--no-install"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("add --org --yes: %v", err)
	}

	if scopes := manifestScopes(t, vaultDir, "my-skill"); len(scopes) != 0 {
		t.Fatalf("--org should clear all scopes (global), got %+v", scopes)
	}
}

// TestScopeE2E_InstallMultiScope: `sx install <name> --user me --team X --yes`
// sets BOTH scopes in one call — proving sx install now takes the same
// repeatable, multi-scope flags as sx add (no more single-exclusive-scope).
func TestScopeE2E_InstallMultiScope(t *testing.T) {
	_, vaultDir := seedScopeVault(t)

	v, err := vaultpkg.NewPathVault("file://" + vaultDir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	if err := v.CreateTeam(context.Background(), mgmt.Team{
		Name: "platform", Members: []string{"other@example.com"}, Admins: []string{"other@example.com"},
	}); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	cmd := NewInstallCommand()
	cmd.SetArgs([]string{"my-skill", "--user", "me", "--team", "platform", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("install --user me --team platform --yes: %v", err)
	}

	scopes := manifestScopes(t, vaultDir, "my-skill")
	if !hasScope(scopes, manifest.ScopeKindUser, "test@example.com") {
		t.Errorf("expected user scope for caller, got %+v", scopes)
	}
	if !hasScope(scopes, manifest.ScopeKindTeam, "platform") {
		t.Errorf("expected team scope platform, got %+v", scopes)
	}
}

// TestScopeE2E_InstallAppendsByDefault: a second `sx install` with a new scope
// appends by default — the first scope must survive without any extra flag.
func TestScopeE2E_InstallAppendsByDefault(t *testing.T) {
	_, vaultDir := seedScopeVault(t)

	v, err := vaultpkg.NewPathVault("file://" + vaultDir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	if err := v.CreateTeam(context.Background(), mgmt.Team{
		Name: "platform", Members: []string{"other@example.com"}, Admins: []string{"other@example.com"},
	}); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	// First scope: just me.
	set := NewInstallCommand()
	set.SetArgs([]string{"my-skill", "--user", "me", "--yes"})
	if err := set.Execute(); err != nil {
		t.Fatalf("install --user me --yes: %v", err)
	}

	// Add a team scope with NO append flag — the user scope must survive.
	add := NewInstallCommand()
	add.SetArgs([]string{"my-skill", "--team", "platform", "--yes"})
	if err := add.Execute(); err != nil {
		t.Fatalf("install --team platform --yes: %v", err)
	}

	scopes := manifestScopes(t, vaultDir, "my-skill")
	if !hasScope(scopes, manifest.ScopeKindUser, "test@example.com") {
		t.Errorf("default append dropped the existing user scope, got %+v", scopes)
	}
	if !hasScope(scopes, manifest.ScopeKindTeam, "platform") {
		t.Errorf("expected appended team scope platform, got %+v", scopes)
	}
}

// TestScopeE2E_InstallReplaceScopeDropsExisting: `--replace-scope` makes the
// named scopes the asset's complete set, dropping anything previously there.
func TestScopeE2E_InstallReplaceScopeDropsExisting(t *testing.T) {
	_, vaultDir := seedScopeVault(t)

	v, err := vaultpkg.NewPathVault("file://" + vaultDir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	if err := v.CreateTeam(context.Background(), mgmt.Team{
		Name: "platform", Members: []string{"other@example.com"}, Admins: []string{"other@example.com"},
	}); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	// Start scoped to me.
	set := NewInstallCommand()
	set.SetArgs([]string{"my-skill", "--user", "me", "--yes"})
	if err := set.Execute(); err != nil {
		t.Fatalf("install --user me --yes: %v", err)
	}

	// Replace the whole set with just the team scope.
	repl := NewInstallCommand()
	repl.SetArgs([]string{"my-skill", "--team", "platform", "--replace-scope", "--yes"})
	if err := repl.Execute(); err != nil {
		t.Fatalf("install --team platform --replace-scope --yes: %v", err)
	}

	scopes := manifestScopes(t, vaultDir, "my-skill")
	if hasScope(scopes, manifest.ScopeKindUser, "test@example.com") {
		t.Errorf("--replace-scope should have dropped the user scope, got %+v", scopes)
	}
	if !hasScope(scopes, manifest.ScopeKindTeam, "platform") {
		t.Errorf("expected team scope platform after replace, got %+v", scopes)
	}
}

// seedScopeTeam creates the "platform" team in the path vault at vaultDir so
// team-scoping tests have a team to target.
func seedScopeTeam(t *testing.T, vaultDir string) {
	t.Helper()
	v, err := vaultpkg.NewPathVault("file://" + vaultDir)
	if err != nil {
		t.Fatalf("NewPathVault: %v", err)
	}
	if err := v.CreateTeam(context.Background(), mgmt.Team{
		Name: "platform", Members: []string{"other@example.com"}, Admins: []string{"other@example.com"},
	}); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
}

// TestScopeE2E_InstallConfirmApplies: without --yes, the change is previewed and
// applied when the user confirms at the prompt (stdin "y").
func TestScopeE2E_InstallConfirmApplies(t *testing.T) {
	_, vaultDir := seedScopeVault(t)
	seedScopeTeam(t, vaultDir)

	cmd := NewInstallCommand()
	cmd.SetArgs([]string{"my-skill", "--team", "platform"}) // no --yes
	cmd.SetIn(strings.NewReader("y\n"))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("install --team platform (confirm y): %v", err)
	}

	if scopes := manifestScopes(t, vaultDir, "my-skill"); !hasScope(scopes, manifest.ScopeKindTeam, "platform") {
		t.Errorf("confirming the prompt should apply the scope, got %+v", scopes)
	}
}

// TestScopeE2E_InstallDeclineAborts: declining the prompt (stdin "n") leaves the
// asset's scope untouched and is not an error.
func TestScopeE2E_InstallDeclineAborts(t *testing.T) {
	_, vaultDir := seedScopeVault(t)
	seedScopeTeam(t, vaultDir)

	cmd := NewInstallCommand()
	cmd.SetArgs([]string{"my-skill", "--team", "platform"}) // no --yes
	cmd.SetIn(strings.NewReader("n\n"))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("declining the prompt should not error: %v", err)
	}

	if scopes := manifestScopes(t, vaultDir, "my-skill"); hasScope(scopes, manifest.ScopeKindTeam, "platform") {
		t.Errorf("declining should leave the scope unchanged, got %+v", scopes)
	}
}

// TestScopeE2E_InstallNoChangeIsNoOp: re-applying a scope the asset already has
// must detect "no changes" and no-op WITHOUT prompting. Empty stdin is the trap:
// if it tried to confirm, the read would EOF-error and the command would fail.
func TestScopeE2E_InstallNoChangeIsNoOp(t *testing.T) {
	_, vaultDir := seedScopeVault(t)
	seedScopeTeam(t, vaultDir)

	// Scope to platform once.
	set := NewInstallCommand()
	set.SetArgs([]string{"my-skill", "--team", "platform", "--yes"})
	if err := set.Execute(); err != nil {
		t.Fatalf("seed scope: %v", err)
	}

	// Re-apply the same scope with no --yes and empty stdin: must no-op, not prompt.
	again := NewInstallCommand()
	again.SetArgs([]string{"my-skill", "--team", "platform"})
	again.SetIn(strings.NewReader(""))
	if err := again.Execute(); err != nil {
		t.Fatalf("re-applying an existing scope should no-op without prompting, got: %v", err)
	}

	if scopes := manifestScopes(t, vaultDir, "my-skill"); !hasScope(scopes, manifest.ScopeKindTeam, "platform") {
		t.Errorf("the existing scope should still be present, got %+v", scopes)
	}
}

// TestInstallSetTarget_Validations covers the up-front guards that reject bad
// flag combinations before any vault work.
func TestInstallSetTarget_Validations(t *testing.T) {
	t.Run("--dry-run with a scope flag errors", func(t *testing.T) {
		cmd := NewInstallCommand()
		cmd.SetArgs([]string{"my-skill", "--team", "platform", "--dry-run"})
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected an error combining --dry-run with a scope flag")
		}
	})

	t.Run("scope flag without an asset name errors", func(t *testing.T) {
		cmd := NewInstallCommand()
		cmd.SetArgs([]string{"--team", "platform"})
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected an error: scope flags require an asset name")
		}
	})
}

// TestScopeE2E_AddReplaceScopeDropsExisting mirrors the install replace test for
// `sx add`: --replace-scope makes the named scopes the complete set, since both
// commands share the resolver.
func TestScopeE2E_AddReplaceScopeDropsExisting(t *testing.T) {
	_, vaultDir := seedScopeVault(t)
	seedScopeTeam(t, vaultDir)

	// Seed: scope to me (append to empty).
	set := NewAddCommand()
	set.SetArgs([]string{"my-skill", "--user", "me", "--yes", "--no-install"})
	if err := set.Execute(); err != nil {
		t.Fatalf("add --user me: %v", err)
	}

	// Replace the whole set with just the team.
	repl := NewAddCommand()
	repl.SetArgs([]string{"my-skill", "--team", "platform", "--replace-scope", "--yes", "--no-install"})
	if err := repl.Execute(); err != nil {
		t.Fatalf("add --team platform --replace-scope: %v", err)
	}

	scopes := manifestScopes(t, vaultDir, "my-skill")
	if hasScope(scopes, manifest.ScopeKindUser, "test@example.com") {
		t.Errorf("--replace-scope on sx add should drop the user scope, got %+v", scopes)
	}
	if !hasScope(scopes, manifest.ScopeKindTeam, "platform") {
		t.Errorf("expected team scope platform after replace, got %+v", scopes)
	}
}

func gitRunE2E(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func gitOutE2E(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// TestScopeE2E_AddScopeFlagRejectsNonPersonal: --scope only ever meant
// "personal"; any other value must error loudly instead of doing nothing.
func TestScopeE2E_AddScopeFlagRejectsNonPersonal(t *testing.T) {
	cmd := NewAddCommand()
	cmd.SetArgs([]string{"some-asset", "--scope", "bogus"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "personal") {
		t.Fatalf("expected --scope bogus to error mentioning personal, got %v", err)
	}
}
