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
