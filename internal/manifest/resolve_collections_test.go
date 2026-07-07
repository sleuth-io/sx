package manifest

import (
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/mgmt"
)

// Collection scopes are dereferenced at resolve time: member assets reach
// everyone a collection scope reaches, on top of (never instead of) their
// own scope rows.
func TestResolve_CollectionScopesReachMembers(t *testing.T) {
	m := &Manifest{
		SchemaVersion: CurrentSchemaVersion,
		Teams: []Team{
			{Name: "platform", Members: []string{"alice@acme.com"}, Admins: []string{"alice@acme.com"}},
		},
		Assets: []Asset{
			// Reaches alice ONLY via the collection's team row.
			{
				Name: "member-scoped-elsewhere", Version: "1", Type: asset.TypeSkill,
				Scopes: []Scope{{Kind: ScopeKindUser, User: "someone-else@acme.com"}},
			},
			// Direct repo scope plus the collection's team row: both apply.
			{
				Name: "member-with-repo", Version: "1", Type: asset.TypeSkill,
				Scopes: []Scope{{Kind: ScopeKindRepo, Repo: "github.com/acme/app"}},
			},
			// Not in any collection; scoped away from alice.
			{
				Name: "outsider", Version: "1", Type: asset.TypeSkill,
				Scopes: []Scope{{Kind: ScopeKindUser, User: "someone-else@acme.com"}},
			},
			// Scope-less stays org-wide regardless of collection rows.
			{Name: "global", Version: "1", Type: asset.TypeSkill},
		},
		Collections: []Collection{
			{
				Name:   "essentials",
				Assets: []string{"member-scoped-elsewhere", "member-with-repo", "global"},
				Scopes: []Scope{{Kind: ScopeKindTeam, Team: "platform"}},
			},
		},
	}

	lock := Resolve(m, mgmt.Actor{Email: "alice@acme.com"})
	byName := map[string][]string{}
	for _, a := range lock.Assets {
		repos := []string{}
		for _, s := range a.Scopes {
			repos = append(repos, s.Repo)
		}
		byName[a.Name] = repos
	}

	// platform has no repositories, so its members get team-scoped assets
	// globally (nil scopes).
	if repos, ok := byName["member-scoped-elsewhere"]; !ok || len(repos) != 0 {
		t.Fatalf("member-scoped-elsewhere = %v (present %v), want global via collection team row", repos, ok)
	}
	// The direct repo scope must survive alongside the collection grant —
	// team-with-no-repos makes the whole asset global for alice.
	if repos, ok := byName["member-with-repo"]; !ok || len(repos) != 0 {
		t.Fatalf("member-with-repo = %v (present %v), want global (team row widens past the repo scope)", repos, ok)
	}
	if _, ok := byName["outsider"]; ok {
		t.Fatalf("outsider resolved for alice; collections must not leak to non-members")
	}
	if repos, ok := byName["global"]; !ok || len(repos) != 0 {
		t.Fatalf("global = %v (present %v), want unconditionally global", repos, ok)
	}

	// A non-member gets neither collection-granted asset.
	lock = Resolve(m, mgmt.Actor{Email: "mallory@acme.com"})
	names := map[string]bool{}
	for _, a := range lock.Assets {
		names[a.Name] = true
	}
	if names["member-scoped-elsewhere"] {
		t.Fatalf("non-member resolved member-scoped-elsewhere")
	}
	if !names["global"] {
		t.Fatalf("non-member lost the org-wide asset")
	}
}

// An org row on a collection makes its members org-wide without touching
// their own scopes; repo rows union with direct repo scopes.
func TestResolve_CollectionOrgAndRepoRows(t *testing.T) {
	m := &Manifest{
		SchemaVersion: CurrentSchemaVersion,
		Assets: []Asset{
			{
				Name: "org-via-collection", Version: "1", Type: asset.TypeSkill,
				Scopes: []Scope{{Kind: ScopeKindUser, User: "someone-else@acme.com"}},
			},
			{
				Name: "two-repos", Version: "1", Type: asset.TypeSkill,
				Scopes: []Scope{{Kind: ScopeKindRepo, Repo: "github.com/acme/app"}},
			},
		},
		Collections: []Collection{
			{
				Name:   "everywhere",
				Assets: []string{"org-via-collection"},
				Scopes: []Scope{{Kind: ScopeKindOrg}},
			},
			{
				Name:   "infra",
				Assets: []string{"two-repos"},
				Scopes: []Scope{{Kind: ScopeKindRepo, Repo: "github.com/acme/infra"}},
			},
		},
	}

	lock := Resolve(m, mgmt.Actor{Email: "mallory@acme.com"})
	byName := map[string]int{}
	globals := map[string]bool{}
	for _, a := range lock.Assets {
		byName[a.Name] = len(a.Scopes)
		globals[a.Name] = a.Scopes == nil
	}
	if !globals["org-via-collection"] {
		t.Fatalf("org-via-collection: want global via collection org row, got %d scopes", byName["org-via-collection"])
	}
	if byName["two-repos"] != 2 {
		t.Fatalf("two-repos: want direct repo + collection repo = 2 scopes, got %d", byName["two-repos"])
	}
}
