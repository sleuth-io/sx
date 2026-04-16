package mgmt

import (
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/lockfile"
)

func TestOverlayNilInstallationsReturnsBaseUnchanged(t *testing.T) {
	base := &lockfile.LockFile{
		Assets: []lockfile.Asset{
			{Name: "foo", Version: "1.0.0", Type: asset.TypeSkill},
		},
	}
	got := OverlayInstallations(base, nil, nil, Actor{Email: "alice@example.com"})
	if got != base {
		t.Error("expected same pointer when installations is nil")
	}
}

func TestOverlayEmptyInstallationsReturnsBaseUnchanged(t *testing.T) {
	base := &lockfile.LockFile{
		Assets: []lockfile.Asset{
			{Name: "foo", Version: "1.0.0", Type: asset.TypeSkill},
		},
	}
	ifile := &InstallationsFile{}
	got := OverlayInstallations(base, nil, ifile, Actor{Email: "alice@example.com"})
	if got != base {
		t.Error("expected same pointer when installations is empty")
	}
}

func TestOverlayUserMatchBecomesGlobal(t *testing.T) {
	base := &lockfile.LockFile{
		Assets: []lockfile.Asset{
			{
				Name: "foo", Version: "1.0.0", Type: asset.TypeSkill,
				Scopes: []lockfile.Scope{{Repo: "https://github.com/acme/bar.git"}},
			},
		},
	}
	ifile := &InstallationsFile{
		Installations: []Installation{
			{Asset: "foo", Kind: InstallKindUser, User: "alice@example.com"},
		},
	}

	got := OverlayInstallations(base, nil, ifile, Actor{Email: "alice@example.com"})
	if len(got.Assets[0].Scopes) != 0 {
		t.Errorf("expected asset to become global for matching user, got %v", got.Assets[0].Scopes)
	}
	// Base should be untouched
	if len(base.Assets[0].Scopes) != 1 {
		t.Errorf("base lock file was mutated")
	}
}

func TestOverlayUserMismatchIsDropped(t *testing.T) {
	base := &lockfile.LockFile{
		Assets: []lockfile.Asset{
			{
				Name: "foo", Version: "1.0.0", Type: asset.TypeSkill,
				Scopes: []lockfile.Scope{{Repo: "https://github.com/acme/bar.git"}},
			},
		},
	}
	ifile := &InstallationsFile{
		Installations: []Installation{
			{Asset: "foo", Kind: InstallKindUser, User: "alice@example.com"},
		},
	}

	got := OverlayInstallations(base, nil, ifile, Actor{Email: "bob@example.com"})
	if len(got.Assets[0].Scopes) != 1 {
		t.Errorf("expected scopes unchanged for non-matching user, got %v", got.Assets[0].Scopes)
	}
}

func TestOverlayTeamMemberExpandsToTeamRepos(t *testing.T) {
	base := &lockfile.LockFile{
		Assets: []lockfile.Asset{
			{
				Name: "foo", Version: "1.0.0", Type: asset.TypeSkill,
				Scopes: []lockfile.Scope{{Repo: "https://github.com/acme/existing.git"}},
			},
		},
	}
	teams := &TeamsFile{
		Teams: []Team{
			{
				Name:    "platform",
				Members: []string{"alice@example.com"},
				Repositories: []string{
					"github.com/acme/infra",
					"github.com/acme/tools",
				},
			},
		},
	}
	ifile := &InstallationsFile{
		Installations: []Installation{
			{Asset: "foo", Kind: InstallKindTeam, Team: "platform"},
		},
	}

	got := OverlayInstallations(base, teams, ifile, Actor{Email: "alice@example.com"})
	if len(got.Assets[0].Scopes) != 3 {
		t.Fatalf("expected 3 scopes (1 existing + 2 team repos), got %d: %v", len(got.Assets[0].Scopes), got.Assets[0].Scopes)
	}

	// Non-member should not see team repos
	got = OverlayInstallations(base, teams, ifile, Actor{Email: "bob@example.com"})
	if len(got.Assets[0].Scopes) != 1 {
		t.Errorf("expected non-member to see only base scopes, got %v", got.Assets[0].Scopes)
	}
}

func TestOverlayOrgWideAssetStaysOrgWide(t *testing.T) {
	base := &lockfile.LockFile{
		Assets: []lockfile.Asset{
			{Name: "foo", Version: "1.0.0", Type: asset.TypeSkill}, // no scopes == org-wide
		},
	}
	teams := &TeamsFile{
		Teams: []Team{
			{Name: "platform", Members: []string{"alice@example.com"}, Repositories: []string{"github.com/acme/infra"}},
		},
	}
	ifile := &InstallationsFile{
		Installations: []Installation{
			{Asset: "foo", Kind: InstallKindTeam, Team: "platform"},
		},
	}

	// Even a non-member should still see the org-wide asset
	got := OverlayInstallations(base, teams, ifile, Actor{Email: "bob@example.com"})
	if len(got.Assets[0].Scopes) != 0 {
		t.Errorf("expected org-wide asset to stay org-wide, got %v", got.Assets[0].Scopes)
	}
}

func TestOverlayMergeScopesCollapsesPaths(t *testing.T) {
	// Asset has a repo scope with specific paths; team installation adds a
	// bare repo scope for the same repo. Result should be bare repo (wins).
	base := &lockfile.LockFile{
		Assets: []lockfile.Asset{
			{
				Name: "foo", Version: "1.0.0", Type: asset.TypeSkill,
				Scopes: []lockfile.Scope{
					{Repo: "github.com/acme/infra", Paths: []string{"services/api"}},
				},
			},
		},
	}
	teams := &TeamsFile{
		Teams: []Team{
			{Name: "platform", Members: []string{"alice@example.com"}, Repositories: []string{"github.com/acme/infra"}},
		},
	}
	ifile := &InstallationsFile{
		Installations: []Installation{
			{Asset: "foo", Kind: InstallKindTeam, Team: "platform"},
		},
	}

	got := OverlayInstallations(base, teams, ifile, Actor{Email: "alice@example.com"})
	if len(got.Assets[0].Scopes) != 1 {
		t.Fatalf("expected 1 merged scope, got %d: %v", len(got.Assets[0].Scopes), got.Assets[0].Scopes)
	}
	if len(got.Assets[0].Scopes[0].Paths) != 0 {
		t.Errorf("expected path-wide collapse (no paths), got %v", got.Assets[0].Scopes[0].Paths)
	}
}

func TestOverlayTeamMissingFromFileIsDropped(t *testing.T) {
	base := &lockfile.LockFile{
		Assets: []lockfile.Asset{
			{
				Name: "foo", Version: "1.0.0", Type: asset.TypeSkill,
				Scopes: []lockfile.Scope{{Repo: "github.com/acme/existing"}},
			},
		},
	}
	teams := &TeamsFile{}
	ifile := &InstallationsFile{
		Installations: []Installation{
			{Asset: "foo", Kind: InstallKindTeam, Team: "nonexistent"},
		},
	}

	got := OverlayInstallations(base, teams, ifile, Actor{Email: "alice@example.com"})
	if len(got.Assets[0].Scopes) != 1 {
		t.Errorf("expected original scope unchanged when team missing, got %v", got.Assets[0].Scopes)
	}
}
