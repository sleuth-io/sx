package commands

import (
	"reflect"
	"testing"

	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// These tests pin down the single, unified scope-resolution logic that both
// `sx add` and `sx install <name>` must use. The whole point is that there is
// ONE way to specify an asset's scope: a set of flags (--org/--repo/--path/
// --team/--user/--bot, plus --replace-scope) that resolve, via resolveScopeFlags,
// into the same scopeChange regardless of which command parsed them.
//
// Semantics (decided with the user):
//   - Default mode is ADD (append): the named targets are appended to the
//     asset's existing scopes, so calling --repo three times grows the set.
//   - --replace-scope switches to REPLACE mode: the flags describe the asset's
//     complete scope set; anything not named is dropped.
//   - --org means "global" and is exclusive: it clears all scopes (always
//     replaces) and cannot be combined with other targets.
//   - Within a kind, the input order is preserved; across kinds the order is
//     fixed: repos, then paths, then teams, then users, then bots — so commit
//     messages and audit output are stable.
//   - At least one target is required in either mode: bare flags (including a
//     lone --replace-scope) are an error.
//
// resolveScopeFlags is pure: flags in, (mode + ordered targets) out, with no
// vault or actor knowledge. Actor-dependent checks (e.g. user self-only) and
// URL normalization stay in the vault layer. Both `sx add` and `sx install`
// fold their flags into scopeFlags and run them through this one resolver.

func TestResolveScopeFlags_AppendIsDefault(t *testing.T) {
	tests := []struct {
		name  string
		flags scopeFlags
		want  []vaultpkg.InstallTarget
	}{
		{
			name:  "single team",
			flags: scopeFlags{Teams: []string{"platform"}},
			want:  []vaultpkg.InstallTarget{{Kind: vaultpkg.InstallKindTeam, Team: "platform"}},
		},
		{
			name:  "single repo passes URL through unchanged",
			flags: scopeFlags{Repos: []string{"git@github.com:acme/infra.git"}},
			want:  []vaultpkg.InstallTarget{{Kind: vaultpkg.InstallKindRepo, Repo: "git@github.com:acme/infra.git"}},
		},
		{
			name:  "path with one repo and two paths",
			flags: scopeFlags{Paths: []string{"github.com/acme/infra#services/api,services/web"}},
			want: []vaultpkg.InstallTarget{{
				Kind:  vaultpkg.InstallKindPath,
				Repo:  "github.com/acme/infra",
				Paths: []string{"services/api", "services/web"},
			}},
		},
		{
			name:  "path with a single path",
			flags: scopeFlags{Paths: []string{"github.com/acme/infra#services/api"}},
			want: []vaultpkg.InstallTarget{{
				Kind:  vaultpkg.InstallKindPath,
				Repo:  "github.com/acme/infra",
				Paths: []string{"services/api"},
			}},
		},
		{
			name:  "single user",
			flags: scopeFlags{Users: []string{"alice@acme.com"}},
			want:  []vaultpkg.InstallTarget{{Kind: vaultpkg.InstallKindUser, User: "alice@acme.com"}},
		},
		{
			name:  "single bot",
			flags: scopeFlags{Bots: []string{"python-backend"}},
			want:  []vaultpkg.InstallTarget{{Kind: vaultpkg.InstallKindBot, Bot: "python-backend"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveScopeFlags(tt.flags)
			if err != nil {
				t.Fatalf("resolveScopeFlags returned error: %v", err)
			}
			if got.Mode != scopeAdd {
				t.Errorf("Mode = %v, want scopeAdd (default)", got.Mode)
			}
			if !reflect.DeepEqual(got.Targets, tt.want) {
				t.Errorf("Targets = %+v, want %+v", got.Targets, tt.want)
			}
		})
	}
}

// TestResolveScopeFlags_OrgIsGlobalReplace: --org always resolves to a single
// global target in REPLACE mode (it clears every other scope), regardless of the
// append-by-default and regardless of --replace-scope.
func TestResolveScopeFlags_OrgIsGlobalReplace(t *testing.T) {
	for _, f := range []scopeFlags{{Org: true}, {Org: true, Replace: true}} {
		got, err := resolveScopeFlags(f)
		if err != nil {
			t.Fatalf("resolveScopeFlags(%+v) error: %v", f, err)
		}
		if got.Mode != scopeReplace {
			t.Errorf("Mode = %v, want scopeReplace for org", got.Mode)
		}
		want := []vaultpkg.InstallTarget{{Kind: vaultpkg.InstallKindOrg}}
		if !reflect.DeepEqual(got.Targets, want) {
			t.Errorf("Targets = %+v, want %+v", got.Targets, want)
		}
	}
}

// TestResolveScopeFlags_ReplaceModeAllKinds mirrors the default (append) table
// but with --replace-scope set: every kind must round-trip to the same target it
// produces in append mode — only the Mode differs.
func TestResolveScopeFlags_ReplaceModeAllKinds(t *testing.T) {
	tests := []struct {
		name  string
		flags scopeFlags
		want  []vaultpkg.InstallTarget
	}{
		{
			name:  "replace single team",
			flags: scopeFlags{Teams: []string{"platform"}, Replace: true},
			want:  []vaultpkg.InstallTarget{{Kind: vaultpkg.InstallKindTeam, Team: "platform"}},
		},
		{
			name:  "replace single repo",
			flags: scopeFlags{Repos: []string{"git@github.com:acme/infra.git"}, Replace: true},
			want:  []vaultpkg.InstallTarget{{Kind: vaultpkg.InstallKindRepo, Repo: "git@github.com:acme/infra.git"}},
		},
		{
			name:  "replace single path",
			flags: scopeFlags{Paths: []string{"github.com/acme/infra#services/api"}, Replace: true},
			want: []vaultpkg.InstallTarget{{
				Kind:  vaultpkg.InstallKindPath,
				Repo:  "github.com/acme/infra",
				Paths: []string{"services/api"},
			}},
		},
		{
			name:  "replace single user",
			flags: scopeFlags{Users: []string{"alice@acme.com"}, Replace: true},
			want:  []vaultpkg.InstallTarget{{Kind: vaultpkg.InstallKindUser, User: "alice@acme.com"}},
		},
		{
			name:  "replace single bot",
			flags: scopeFlags{Bots: []string{"python-backend"}, Replace: true},
			want:  []vaultpkg.InstallTarget{{Kind: vaultpkg.InstallKindBot, Bot: "python-backend"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveScopeFlags(tt.flags)
			if err != nil {
				t.Fatalf("resolveScopeFlags returned error: %v", err)
			}
			if got.Mode != scopeReplace {
				t.Errorf("Mode = %v, want scopeReplace", got.Mode)
			}
			if !reflect.DeepEqual(got.Targets, tt.want) {
				t.Errorf("Targets = %+v, want %+v", got.Targets, tt.want)
			}
		})
	}
}

func TestResolveScopeFlags_MultipleTargetsReplaceWholeSetInOrder(t *testing.T) {
	// Replace mode must let the caller express a full multi-scope set in one
	// invocation. Order is deterministic: repos, then paths, then teams, then
	// users, then bots — so commit messages and audit output are stable.
	flags := scopeFlags{
		Repos:   []string{"git@github.com:acme/app-a.git", "git@github.com:acme/app-b.git"},
		Teams:   []string{"platform"},
		Replace: true,
	}

	got, err := resolveScopeFlags(flags)
	if err != nil {
		t.Fatalf("resolveScopeFlags returned error: %v", err)
	}
	if got.Mode != scopeReplace {
		t.Errorf("Mode = %v, want scopeReplace", got.Mode)
	}

	want := []vaultpkg.InstallTarget{
		{Kind: vaultpkg.InstallKindRepo, Repo: "git@github.com:acme/app-a.git"},
		{Kind: vaultpkg.InstallKindRepo, Repo: "git@github.com:acme/app-b.git"},
		{Kind: vaultpkg.InstallKindTeam, Team: "platform"},
	}
	if !reflect.DeepEqual(got.Targets, want) {
		t.Errorf("Targets = %+v, want %+v", got.Targets, want)
	}
}

// TestResolveScopeFlags_FullMultiScopeOrderingAcrossEveryKind sets at least one
// of every combinable kind, each with two entries given out of canonical order,
// and asserts the resolver emits them grouped by kind in the fixed order with
// within-kind input order preserved.
func TestResolveScopeFlags_FullMultiScopeOrderingAcrossEveryKind(t *testing.T) {
	flags := scopeFlags{
		Bots:  []string{"bot-z", "bot-a"},
		Users: []string{"carol@acme.com", "bob@acme.com"},
		Teams: []string{"platform", "data"},
		Paths: []string{"github.com/acme/mono#svc/a", "github.com/acme/mono#svc/b,svc/c"},
		Repos: []string{"git@github.com:acme/r1.git", "git@github.com:acme/r2.git"},
	}

	got, err := resolveScopeFlags(flags)
	if err != nil {
		t.Fatalf("resolveScopeFlags returned error: %v", err)
	}
	if got.Mode != scopeAdd {
		t.Errorf("Mode = %v, want scopeAdd (default)", got.Mode)
	}

	want := []vaultpkg.InstallTarget{
		{Kind: vaultpkg.InstallKindRepo, Repo: "git@github.com:acme/r1.git"},
		{Kind: vaultpkg.InstallKindRepo, Repo: "git@github.com:acme/r2.git"},
		{Kind: vaultpkg.InstallKindPath, Repo: "github.com/acme/mono", Paths: []string{"svc/a"}},
		{Kind: vaultpkg.InstallKindPath, Repo: "github.com/acme/mono", Paths: []string{"svc/b", "svc/c"}},
		{Kind: vaultpkg.InstallKindTeam, Team: "platform"},
		{Kind: vaultpkg.InstallKindTeam, Team: "data"},
		{Kind: vaultpkg.InstallKindUser, User: "carol@acme.com"},
		{Kind: vaultpkg.InstallKindUser, User: "bob@acme.com"},
		{Kind: vaultpkg.InstallKindBot, Bot: "bot-z"},
		{Kind: vaultpkg.InstallKindBot, Bot: "bot-a"},
	}
	if !reflect.DeepEqual(got.Targets, want) {
		t.Errorf("Targets = %+v, want %+v", got.Targets, want)
	}
}

// TestResolveScopeFlags_MultipleOfSameKind covers the repeated-flag case for the
// scalar kinds (team/user/bot) — each value becomes its own target, in order.
func TestResolveScopeFlags_MultipleOfSameKind(t *testing.T) {
	tests := []struct {
		name  string
		flags scopeFlags
		want  []vaultpkg.InstallTarget
	}{
		{
			name:  "multiple teams",
			flags: scopeFlags{Teams: []string{"platform", "data", "infra"}},
			want: []vaultpkg.InstallTarget{
				{Kind: vaultpkg.InstallKindTeam, Team: "platform"},
				{Kind: vaultpkg.InstallKindTeam, Team: "data"},
				{Kind: vaultpkg.InstallKindTeam, Team: "infra"},
			},
		},
		{
			name:  "multiple users",
			flags: scopeFlags{Users: []string{"alice@acme.com", "bob@acme.com"}},
			want: []vaultpkg.InstallTarget{
				{Kind: vaultpkg.InstallKindUser, User: "alice@acme.com"},
				{Kind: vaultpkg.InstallKindUser, User: "bob@acme.com"},
			},
		},
		{
			name:  "multiple bots",
			flags: scopeFlags{Bots: []string{"python-backend", "go-frontend"}},
			want: []vaultpkg.InstallTarget{
				{Kind: vaultpkg.InstallKindBot, Bot: "python-backend"},
				{Kind: vaultpkg.InstallKindBot, Bot: "go-frontend"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveScopeFlags(tt.flags)
			if err != nil {
				t.Fatalf("resolveScopeFlags returned error: %v", err)
			}
			if got.Mode != scopeAdd {
				t.Errorf("Mode = %v, want scopeAdd (default)", got.Mode)
			}
			if !reflect.DeepEqual(got.Targets, tt.want) {
				t.Errorf("Targets = %+v, want %+v", got.Targets, tt.want)
			}
		})
	}
}

// TestResolveScopeFlags_MultiplePathSpecsDistinctRepos confirms each --path
// value is parsed independently into its own path target — different repos do
// not get merged, and order is preserved.
func TestResolveScopeFlags_MultiplePathSpecsDistinctRepos(t *testing.T) {
	flags := scopeFlags{Paths: []string{
		"github.com/acme/api#services/a,services/b",
		"github.com/acme/web#apps/x",
	}}

	got, err := resolveScopeFlags(flags)
	if err != nil {
		t.Fatalf("resolveScopeFlags returned error: %v", err)
	}

	want := []vaultpkg.InstallTarget{
		{Kind: vaultpkg.InstallKindPath, Repo: "github.com/acme/api", Paths: []string{"services/a", "services/b"}},
		{Kind: vaultpkg.InstallKindPath, Repo: "github.com/acme/web", Paths: []string{"apps/x"}},
	}
	if !reflect.DeepEqual(got.Targets, want) {
		t.Errorf("Targets = %+v, want %+v", got.Targets, want)
	}
}

// TestResolveScopeFlags_RepoAndPathOnSameRepoCoexist checks that a whole-repo
// target and a path target for the same repo are kept as two distinct targets —
// the resolver does not try to reconcile or dedupe them.
func TestResolveScopeFlags_RepoAndPathOnSameRepoCoexist(t *testing.T) {
	flags := scopeFlags{
		Repos: []string{"github.com/acme/infra"},
		Paths: []string{"github.com/acme/infra#services/api"},
	}

	got, err := resolveScopeFlags(flags)
	if err != nil {
		t.Fatalf("resolveScopeFlags returned error: %v", err)
	}

	want := []vaultpkg.InstallTarget{
		{Kind: vaultpkg.InstallKindRepo, Repo: "github.com/acme/infra"},
		{Kind: vaultpkg.InstallKindPath, Repo: "github.com/acme/infra", Paths: []string{"services/api"}},
	}
	if !reflect.DeepEqual(got.Targets, want) {
		t.Errorf("Targets = %+v, want %+v", got.Targets, want)
	}
}

func TestResolveScopeFlags_ReplaceScopeSwitchesToReplaceMode(t *testing.T) {
	flags := scopeFlags{Teams: []string{"platform"}, Replace: true}

	got, err := resolveScopeFlags(flags)
	if err != nil {
		t.Fatalf("resolveScopeFlags returned error: %v", err)
	}
	if got.Mode != scopeReplace {
		t.Errorf("Mode = %v, want scopeReplace", got.Mode)
	}
	want := []vaultpkg.InstallTarget{{Kind: vaultpkg.InstallKindTeam, Team: "platform"}}
	if !reflect.DeepEqual(got.Targets, want) {
		t.Errorf("Targets = %+v, want %+v", got.Targets, want)
	}
}

// TestResolveScopeFlags_AppendModeMultipleMixedTargets shows the default (append)
// mode is not limited to one target: it appends a full mixed set, in the same
// canonical order replace mode uses.
func TestResolveScopeFlags_AppendModeMultipleMixedTargets(t *testing.T) {
	flags := scopeFlags{
		Repos: []string{"git@github.com:acme/app.git"},
		Teams: []string{"platform"},
		Users: []string{"alice@acme.com"},
	}

	got, err := resolveScopeFlags(flags)
	if err != nil {
		t.Fatalf("resolveScopeFlags returned error: %v", err)
	}
	if got.Mode != scopeAdd {
		t.Errorf("Mode = %v, want scopeAdd (default)", got.Mode)
	}

	want := []vaultpkg.InstallTarget{
		{Kind: vaultpkg.InstallKindRepo, Repo: "git@github.com:acme/app.git"},
		{Kind: vaultpkg.InstallKindTeam, Team: "platform"},
		{Kind: vaultpkg.InstallKindUser, User: "alice@acme.com"},
	}
	if !reflect.DeepEqual(got.Targets, want) {
		t.Errorf("Targets = %+v, want %+v", got.Targets, want)
	}
}

func TestResolveScopeFlags_Errors(t *testing.T) {
	tests := []struct {
		name  string
		flags scopeFlags
	}{
		{
			name:  "no scope flags at all",
			flags: scopeFlags{},
		},
		{
			name:  "replace-scope with no targets",
			flags: scopeFlags{Replace: true},
		},
		{
			name:  "org is exclusive and cannot combine with a team",
			flags: scopeFlags{Org: true, Teams: []string{"platform"}},
		},
		{
			name:  "org is exclusive and cannot combine with a repo",
			flags: scopeFlags{Org: true, Repos: []string{"github.com/acme/infra"}},
		},
		{
			name:  "org is exclusive and cannot combine with a path",
			flags: scopeFlags{Org: true, Paths: []string{"github.com/acme/infra#services/api"}},
		},
		{
			name:  "org is exclusive and cannot combine with a user",
			flags: scopeFlags{Org: true, Users: []string{"alice@acme.com"}},
		},
		{
			name:  "org is exclusive and cannot combine with a bot",
			flags: scopeFlags{Org: true, Bots: []string{"python-backend"}},
		},
		{
			name:  "path missing its paths is rejected",
			flags: scopeFlags{Paths: []string{"github.com/acme/infra"}},
		},
		{
			name:  "path with empty path list is rejected",
			flags: scopeFlags{Paths: []string{"github.com/acme/infra#"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveScopeFlags(tt.flags)
			if err == nil {
				t.Fatalf("resolveScopeFlags(%+v) = nil error, want error", tt.flags)
			}
		})
	}
}

// TestResolveScopeFlags_AddAndInstallShareOneResolver documents the contract:
// the same flags produce the same scopeChange no matter which command's parser
// populated them. `sx add ./x --team platform` and
// `sx install x --team platform` must both end up calling resolveScopeFlags
// with identical scopeFlags and therefore apply identical scope to the vault.
func TestResolveScopeFlags_AddAndInstallShareOneResolver(t *testing.T) {
	flags := scopeFlags{Teams: []string{"platform"}}

	got, err := resolveScopeFlags(flags)
	if err != nil {
		t.Fatalf("resolveScopeFlags returned error: %v", err)
	}

	want := scopeChange{
		Mode:    scopeAdd,
		Targets: []vaultpkg.InstallTarget{{Kind: vaultpkg.InstallKindTeam, Team: "platform"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scopeChange = %+v, want %+v", got, want)
	}
}

// TestScopeFlags_HasTarget pins which flag combinations put a command into
// "set scope" mode. --replace-scope is a modifier, not a target, so it must not
// trigger on its own — otherwise `sx install --replace-scope x` with no scope
// would route into the resolver and error instead of doing a normal install.
func TestScopeFlags_HasTarget(t *testing.T) {
	tests := []struct {
		name  string
		flags scopeFlags
		want  bool
	}{
		{"empty", scopeFlags{}, false},
		{"replace-only", scopeFlags{Replace: true}, false},
		{"org", scopeFlags{Org: true}, true},
		{"repo", scopeFlags{Repos: []string{"u"}}, true},
		{"path", scopeFlags{Paths: []string{"u#p"}}, true},
		{"team", scopeFlags{Teams: []string{"t"}}, true},
		{"user", scopeFlags{Users: []string{"me"}}, true},
		{"bot", scopeFlags{Bots: []string{"b"}}, true},
		{"team+replace", scopeFlags{Teams: []string{"t"}, Replace: true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.flags.hasTarget(); got != tt.want {
				t.Errorf("hasTarget() = %v, want %v", got, tt.want)
			}
		})
	}
}
