package manifest

import (
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/mgmt"
)

// TestResolve_BotIdentity verifies that a bot caller's resolved lock
// file includes:
//   - direct bot installs (kind=bot matching the bot)
//   - team installs (kind=team where the bot is on the team)
//   - org-wide installs (kind=org) — explicitly diverges from the
//     skills.new bug being fixed in parallel.
//
// And excludes:
//   - kind=user installs (bots are not human users)
//   - kind=bot installs targeting a different bot
//   - kind=team installs for a team the bot is not on
func TestResolve_BotIdentity(t *testing.T) {
	m := &Manifest{
		SchemaVersion: CurrentSchemaVersion,
		Teams: []Team{
			{
				Name:         "platform",
				Members:      []string{"alice@acme.com"},
				Admins:       []string{"alice@acme.com"},
				Repositories: []string{"github.com/acme/infra"},
			},
			{
				Name:         "data",
				Members:      []string{"bob@acme.com"},
				Admins:       []string{"bob@acme.com"},
				Repositories: []string{"github.com/acme/etl"},
			},
		},
		Bots: []Bot{
			{Name: "python-backend", Teams: []string{"platform"}},
			{Name: "other-bot", Teams: []string{"data"}},
		},
		Assets: []Asset{
			{
				Name: "direct-install", Version: "1.0", Type: asset.TypeSkill,
				Scopes: []Scope{{Kind: ScopeKindBot, Bot: "python-backend"}},
			},
			{
				Name: "via-team", Version: "1.0", Type: asset.TypeSkill,
				Scopes: []Scope{{Kind: ScopeKindTeam, Team: "platform"}},
			},
			{
				Name: "global", Version: "1.0", Type: asset.TypeSkill,
				Scopes: []Scope{{Kind: ScopeKindOrg}},
			},
			{
				Name: "user-only", Version: "1.0", Type: asset.TypeSkill,
				Scopes: []Scope{{Kind: ScopeKindUser, User: "alice@acme.com"}},
			},
			{
				Name: "other-bot-only", Version: "1.0", Type: asset.TypeSkill,
				Scopes: []Scope{{Kind: ScopeKindBot, Bot: "other-bot"}},
			},
			{
				Name: "data-team", Version: "1.0", Type: asset.TypeSkill,
				Scopes: []Scope{{Kind: ScopeKindTeam, Team: "data"}},
			},
			{
				Name: "no-scope", Version: "1.0", Type: asset.TypeSkill,
				// Empty scopes are global by manifest convention.
			},
		},
	}

	actor := mgmt.Actor{Email: "bot:python-backend", Name: "python-backend", Bot: "python-backend"}
	lf := Resolve(m, actor)

	got := make(map[string]bool, len(lf.Assets))
	for _, a := range lf.Assets {
		got[a.Name] = true
	}

	want := []string{"direct-install", "via-team", "global", "no-scope"}
	for _, name := range want {
		if !got[name] {
			t.Errorf("missing expected asset for bot: %s", name)
		}
	}

	skip := []string{"user-only", "other-bot-only", "data-team"}
	for _, name := range skip {
		if got[name] {
			t.Errorf("bot resolution leaked asset: %s", name)
		}
	}

	// Verify the team install resolved into a repo scope.
	for _, a := range lf.Assets {
		if a.Name == "via-team" {
			if len(a.Scopes) != 1 || a.Scopes[0].Repo != "github.com/acme/infra" {
				t.Errorf("via-team scopes: got %+v", a.Scopes)
			}
		}
	}
}

// TestResolve_HumanIgnoresBotScopes verifies the inverse: a human
// caller's resolution silently drops bot-scoped installs.
func TestResolve_HumanIgnoresBotScopes(t *testing.T) {
	m := &Manifest{
		SchemaVersion: CurrentSchemaVersion,
		Bots: []Bot{
			{Name: "python-backend"},
		},
		Assets: []Asset{
			{
				Name: "bot-only", Version: "1.0", Type: asset.TypeSkill,
				Scopes: []Scope{{Kind: ScopeKindBot, Bot: "python-backend"}},
			},
		},
	}
	actor := mgmt.Actor{Email: "alice@acme.com"}
	lf := Resolve(m, actor)

	for _, a := range lf.Assets {
		if a.Name == "bot-only" {
			t.Errorf("human caller should not see bot-only asset")
		}
	}
}

// TestResolve_TeamWithNoReposIsGlobal: a skill scoped to a team the caller is a
// member of installs globally when the team has no repositories (matches
// skills.new), and scopes to the team's repos when it has them. A non-member
// gets neither.
func TestResolve_TeamWithNoReposIsGlobal(t *testing.T) {
	m := &Manifest{
		SchemaVersion: CurrentSchemaVersion,
		Teams: []Team{
			{Name: "my-team", Members: []string{"ines@acme.com"}, Admins: []string{"ines@acme.com"}},
			{Name: "with-repos", Members: []string{"ines@acme.com"}, Repositories: []string{"github.com/acme/app"}},
		},
		Assets: []Asset{
			{Name: "team-no-repos", Version: "1.0", Type: asset.TypeSkill,
				Scopes: []Scope{{Kind: ScopeKindTeam, Team: "my-team"}}},
			{Name: "team-with-repos", Version: "1.0", Type: asset.TypeSkill,
				Scopes: []Scope{{Kind: ScopeKindTeam, Team: "with-repos"}}},
		},
	}

	// Member of both teams.
	lf := Resolve(m, mgmt.Actor{Email: "ines@acme.com"})
	got := map[string][]Scope{}
	for _, a := range lf.Assets {
		// a.Scopes is []lockfile.Scope; capture presence + whether global (no scopes).
		got[a.Name] = nil
		if len(a.Scopes) > 0 {
			got[a.Name] = []Scope{{Repo: a.Scopes[0].Repo}}
		}
	}

	if _, ok := got["team-no-repos"]; !ok {
		t.Fatalf("team with no repos should install for member (global), got assets %+v", got)
	}
	if s := got["team-no-repos"]; s != nil {
		t.Fatalf("team with no repos should be global (no scopes), got %+v", s)
	}
	if s := got["team-with-repos"]; len(s) != 1 || s[0].Repo != "github.com/acme/app" {
		t.Fatalf("team with repos should scope to its repo, got %+v", s)
	}

	// A non-member gets neither.
	lf2 := Resolve(m, mgmt.Actor{Email: "stranger@acme.com"})
	for _, a := range lf2.Assets {
		if a.Name == "team-no-repos" || a.Name == "team-with-repos" {
			t.Fatalf("non-member should not receive team-scoped asset %q", a.Name)
		}
	}
}
