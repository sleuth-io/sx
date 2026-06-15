package vault

import (
	"testing"

	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
)

// TestAssetEditPermission_Matrix exercises the edit gate (assetEditReason):
// team-scoped skills are editable only by members of that team (or an
// org-admin); non-team-scoped and brand-new skills are open to anyone.
func TestAssetEditPermission_Matrix(t *testing.T) {
	platform := manifest.Team{Name: "platform", Members: []string{"alice@x.com", "carol@x.com"}, Admins: []string{"alice@x.com"}}
	backend := manifest.Team{Name: "backend", Members: []string{"dave@x.com"}, Admins: []string{"dave@x.com"}}

	mk := func(scopes []manifest.Scope, orgAdmins []string) *manifest.Manifest {
		m := &manifest.Manifest{
			Teams:  []manifest.Team{platform, backend},
			Assets: []manifest.Asset{{Name: "skill", Scopes: scopes}},
		}
		if len(orgAdmins) > 0 {
			m.Org = &manifest.Org{Admins: orgAdmins}
		}
		return m
	}
	team := func(n string) manifest.Scope { return manifest.Scope{Kind: manifest.ScopeKindTeam, Team: n} }
	repo := manifest.Scope{Kind: manifest.ScopeKindRepo, Repo: "github.com/acme/r"}
	user := func(e string) manifest.Scope { return manifest.Scope{Kind: manifest.ScopeKindUser, User: e} }
	a := func(e string) mgmt.Actor { return mgmt.Actor{Email: e} }

	cases := []struct {
		name    string
		m       *manifest.Manifest
		actor   mgmt.Actor
		allowed bool
	}{
		// Not team-scoped → anyone.
		{"no-scopes/anyone", mk(nil, nil), a("nobody@x.com"), true},
		{"repo-scoped/anyone", mk([]manifest.Scope{repo}, nil), a("nobody@x.com"), true},

		// Team-scoped → members only.
		{"team-scoped/admin-member", mk([]manifest.Scope{team("platform")}, nil), a("alice@x.com"), true},
		{"team-scoped/plain-member", mk([]manifest.Scope{team("platform")}, nil), a("carol@x.com"), true},
		{"team-scoped/non-member-denied", mk([]manifest.Scope{team("platform")}, nil), a("nobody@x.com"), false},
		{"team-scoped/other-team-member-denied", mk([]manifest.Scope{team("platform")}, nil), a("dave@x.com"), false},

		// Org-admin always.
		{"team-scoped/org-admin", mk([]manifest.Scope{team("platform")}, []string{"boss@x.com"}), a("boss@x.com"), true},

		// Multiple team scopes → member of any.
		{"multi-team/member-of-one", mk([]manifest.Scope{team("platform"), team("backend")}, nil), a("dave@x.com"), true},
		{"multi-team/non-member-denied", mk([]manifest.Scope{team("platform"), team("backend")}, nil), a("nobody@x.com"), false},

		// Team + repo scopes → team scope still restricts editing to members.
		{"team+repo/member", mk([]manifest.Scope{team("platform"), repo}, nil), a("carol@x.com"), true},
		{"team+repo/non-member-denied", mk([]manifest.Scope{team("platform"), repo}, nil), a("nobody@x.com"), false},

		// Team + user scopes → the team scope still rules; being the scoped user
		// does not grant edit rights (must be a team member).
		{"team+user/scoped-user-non-member-denied", mk([]manifest.Scope{team("platform"), user("bob@x.com")}, nil), a("bob@x.com"), false},
		{"team+user/team-member-allowed", mk([]manifest.Scope{team("platform"), user("bob@x.com")}, nil), a("carol@x.com"), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			denial := assetEditDenial(c.m, "skill", c.actor)
			if (denial == nil) != c.allowed {
				t.Fatalf("allowed=%v want=%v (denial=%v)", denial == nil, c.allowed, denial)
			}
		})
	}

	// A brand-new asset (absent from the manifest) is editable by anyone.
	t.Run("new-asset/anyone", func(t *testing.T) {
		if d := assetEditDenial(mk(nil, nil), "does-not-exist", a("nobody@x.com")); d != nil {
			t.Fatalf("new asset should be editable, got %v", d)
		}
	})
}
