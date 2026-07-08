package main

import (
	"strings"
	"testing"
)

// The Manage Installations dialog round-trip: every scope kind reads
// back as a row, adds apply, removes apply, and an org add returns the
// asset to the everyone default (asset org installs clear scopes).
func TestAssetInstallationsRoundTrip(t *testing.T) {
	a, _, _ := scopedExtensionApp(t, "alice@example.com")
	marketplaceWith(t, a, "shared-widely")
	if _, err := a.InstallMarketplaceExtension("shared-widely", ExtensionScopeOrg); err != nil {
		t.Fatalf("seed asset: %v", err)
	}

	view, err := a.GetAssetInstallations("shared-widely")
	if err != nil || !view.Everyone || len(view.Installations) != 0 {
		t.Fatalf("fresh view = %+v, %v; want everyone", view, err)
	}

	if _, err := a.CreateTeam("platform"); err != nil {
		t.Fatalf("create team: %v", err)
	}
	adds := []AssetInstallation{
		{Kind: "team", Team: "platform"},
		{Kind: "repo", Repo: "https://github.com/acme/api"},
		{Kind: "user"}, // empty user = the caller ("Personal")
	}
	for _, inst := range adds {
		if err := a.AddAssetInstallation("shared-widely", inst); err != nil {
			t.Fatalf("add %+v: %v", inst, err)
		}
	}

	view, err = a.GetAssetInstallations("shared-widely")
	if err != nil || view.Everyone || len(view.Installations) != 3 {
		t.Fatalf("view after adds = %+v, %v; want 3 rows", view, err)
	}
	// Sorted by kind: team, repo, user.
	if view.Installations[0].Kind != "team" || view.Installations[0].Team != "platform" {
		t.Fatalf("row 0 = %+v", view.Installations[0])
	}
	if view.Installations[1].Kind != "repo" || !strings.Contains(view.Installations[1].Repo, "acme/api") {
		t.Fatalf("row 1 = %+v", view.Installations[1])
	}
	if view.Installations[2].Kind != "user" || view.Installations[2].User != "alice@example.com" {
		t.Fatalf("row 2 = %+v; personal add must resolve to the caller", view.Installations[2])
	}

	// Remove the repo row (as the frontend would: pass the row back).
	if err := a.RemoveAssetInstallationRow("shared-widely", view.Installations[1]); err != nil {
		t.Fatalf("remove repo row: %v", err)
	}
	view, _ = a.GetAssetInstallations("shared-widely")
	if len(view.Installations) != 2 {
		t.Fatalf("rows after remove = %+v", view.Installations)
	}

	// Org add clears every row: back to the everyone default.
	if err := a.AddAssetInstallation("shared-widely", AssetInstallation{Kind: "org"}); err != nil {
		t.Fatalf("org add: %v", err)
	}
	view, _ = a.GetAssetInstallations("shared-widely")
	if !view.Everyone || len(view.Installations) != 0 {
		t.Fatalf("view after org add = %+v; want everyone", view)
	}
}

// Collection org installs are an explicit row that would coexist with
// the others, so AddCollectionInstallation must do the replacement the
// dialog promises.
func TestCollectionOrgInstallReplacesRows(t *testing.T) {
	a, _, _ := scopedExtensionApp(t, "alice@example.com")
	marketplaceWith(t, a, "member-tool")
	if _, err := a.InstallMarketplaceExtension("member-tool", ExtensionScopeOrg); err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	if _, err := a.CreateCollection("starter"); err != nil {
		t.Fatalf("create collection: %v", err)
	}
	if err := a.SetCollectionMembership("starter", "member-tool", true); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if _, err := a.CreateTeam("infra"); err != nil {
		t.Fatalf("create team: %v", err)
	}
	if err := a.AddCollectionInstallation("starter", AssetInstallation{Kind: "team", Team: "infra"}); err != nil {
		t.Fatalf("team install: %v", err)
	}
	if err := a.AddCollectionInstallation("starter", AssetInstallation{Kind: "repo", Repo: "https://github.com/acme/api"}); err != nil {
		t.Fatalf("repo install: %v", err)
	}
	view, err := a.GetCollectionInstallations("starter")
	if err != nil || len(view.Installations) != 2 {
		t.Fatalf("view = %+v, %v; want 2 rows", view, err)
	}

	if err := a.AddCollectionInstallation("starter", AssetInstallation{Kind: "org"}); err != nil {
		t.Fatalf("org install: %v", err)
	}
	view, _ = a.GetCollectionInstallations("starter")
	if len(view.Installations) != 1 || view.Installations[0].Kind != "org" {
		t.Fatalf("rows after org = %+v; want just the org row", view.Installations)
	}
}

func TestInstallationValidation(t *testing.T) {
	a, cfgDir, _ := scopedExtensionApp(t, "alice@example.com")
	cases := []struct {
		name string
		inst AssetInstallation
		want string
	}{
		{"unknown kind", AssetInstallation{Kind: "galaxy"}, "unknown installation kind"},
		{"repo without url", AssetInstallation{Kind: "repo"}, "enter a repository"},
		{"team without name", AssetInstallation{Kind: "team"}, "pick a team"},
		{"bot without name", AssetInstallation{Kind: "bot"}, "pick a bot"},
		{"path without paths", AssetInstallation{Kind: "path", Repo: "https://x"}, "at least one path"},
	}
	for _, tc := range cases {
		_, err := a.installationToTarget(tc.inst)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}

	// Personal resolves to the caller from the profile identity — and
	// when the profile pins none, from the vault's git actor
	// (GetVaultInfo's CurrentActor fallback).
	target, err := a.installationToTarget(AssetInstallation{Kind: "user"})
	if err != nil || target.User != "alice@example.com" {
		t.Fatalf("personal target = %+v, %v", target, err)
	}
	seedConfigWithIdentity(t, cfgDir, "")
	target, err = a.installationToTarget(AssetInstallation{Kind: "user"})
	if err != nil || target.User != "alice@example.com" {
		t.Fatalf("personal target via actor fallback = %+v, %v", target, err)
	}
}

func TestListKnownReposFromTeams(t *testing.T) {
	a, _, _ := scopedExtensionApp(t, "alice@example.com")
	if _, err := a.CreateTeam("platform"); err != nil {
		t.Fatalf("create team: %v", err)
	}
	if err := a.SetTeamRepository("platform", "https://github.com/acme/api", true); err != nil {
		t.Fatalf("add repo: %v", err)
	}
	repos, err := a.ListKnownRepos()
	if err != nil || len(repos) != 1 || !strings.Contains(repos[0], "acme/api") {
		t.Fatalf("repos = %+v, %v", repos, err)
	}
	// A vault without bots lists none (and doesn't error).
	bots, err := a.ListVaultBots()
	if err != nil || len(bots) != 0 {
		t.Fatalf("bots = %+v, %v", bots, err)
	}
}
