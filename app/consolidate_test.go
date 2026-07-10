package main

import (
	"strings"
	"testing"
)

// Consolidation unions the sources' reach onto the survivor, then
// soft-retires the sources.
func TestConsolidateAssetsUnionsAndRetires(t *testing.T) {
	a, _, _ := scopedExtensionApp(t, "alice@example.com")
	for _, name := range []string{"keeper", "dupe-a", "dupe-b"} {
		marketplaceWith(t, a, name)
		if _, err := a.InstallMarketplaceExtension(name, ExtensionScopeOrg); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	if _, err := a.CreateTeam("platform"); err != nil {
		t.Fatal(err)
	}
	// Narrow all three so the union is observable: keeper=user,
	// dupe-a=team, dupe-b=repo.
	if err := a.AddAssetInstallation("keeper", AssetInstallation{Kind: "user"}); err != nil {
		t.Fatal(err)
	}
	if err := a.AddAssetInstallation("dupe-a", AssetInstallation{Kind: "team", Team: "platform"}); err != nil {
		t.Fatal(err)
	}
	if err := a.AddAssetInstallation("dupe-b", AssetInstallation{Kind: "repo", Repo: "https://github.com/acme/api"}); err != nil {
		t.Fatal(err)
	}

	result, err := a.ConsolidateAssets("skill-doctor", "keeper", []string{"dupe-a", "dupe-b"})
	if err != nil {
		t.Fatalf("consolidate: %v", err)
	}
	if result.MovedInstallations != 2 || len(result.Retired) != 2 || len(result.Skipped) != 0 {
		t.Fatalf("result = %+v", result)
	}

	view, err := a.GetAssetInstallations("keeper")
	if err != nil || view.Everyone || len(view.Installations) != 3 {
		t.Fatalf("keeper view = %+v, %v; want user+team+repo rows", view, err)
	}

	// The sources are gone from the library.
	assets, err := a.ListAssets()
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range assets {
		if asset.Name == "dupe-a" || asset.Name == "dupe-b" {
			t.Fatalf("%s should be retired", asset.Name)
		}
	}
}

// An org-wide (everyone) source forces the survivor org-wide — reach
// must never shrink for anyone who had the duplicate.
func TestConsolidateAssetsEveryoneSourceWins(t *testing.T) {
	a, _, _ := scopedExtensionApp(t, "alice@example.com")
	for _, name := range []string{"keeper", "org-dupe"} {
		marketplaceWith(t, a, name)
		if _, err := a.InstallMarketplaceExtension(name, ExtensionScopeOrg); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	// keeper narrowed to just the caller; org-dupe stays everyone.
	if err := a.AddAssetInstallation("keeper", AssetInstallation{Kind: "user"}); err != nil {
		t.Fatal(err)
	}

	result, err := a.ConsolidateAssets("skill-doctor", "keeper", []string{"org-dupe"})
	if err != nil {
		t.Fatalf("consolidate: %v", err)
	}
	if len(result.Retired) != 1 {
		t.Fatalf("result = %+v", result)
	}
	view, err := a.GetAssetInstallations("keeper")
	if err != nil || !view.Everyone {
		t.Fatalf("keeper view = %+v, %v; want everyone", view, err)
	}
}

func TestConsolidateAssetsValidation(t *testing.T) {
	a, _, _ := scopedExtensionApp(t, "alice@example.com")
	if _, err := a.ConsolidateAssets("Bad ID!", "keeper", []string{"x"}); err == nil {
		t.Fatal("invalid plugin id should be rejected")
	}
	if _, err := a.ConsolidateAssets("skill-doctor", "keeper", []string{"keeper"}); err == nil {
		t.Fatal("survivor-only from list should be rejected")
	}
	if _, err := a.ConsolidateAssets("skill-doctor", "keeper", nil); err == nil {
		t.Fatal("empty from list should be rejected")
	}
}

// Rows the survivor already has are covered without a write and never
// counted as moves.
func TestConsolidateAssetsSharedRowNotRecounted(t *testing.T) {
	a, _, _ := scopedExtensionApp(t, "alice@example.com")
	for _, name := range []string{"keeper", "dupe"} {
		marketplaceWith(t, a, name)
		if _, err := a.InstallMarketplaceExtension(name, ExtensionScopeOrg); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.CreateTeam("platform"); err != nil {
		t.Fatal(err)
	}
	// Both narrowed to the SAME team row.
	for _, name := range []string{"keeper", "dupe"} {
		if err := a.AddAssetInstallation(name, AssetInstallation{Kind: "team", Team: "platform"}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := a.ConsolidateAssets("skill-doctor", "keeper", []string{"dupe"})
	if err != nil {
		t.Fatalf("consolidate: %v", err)
	}
	if result.MovedInstallations != 0 || len(result.Retired) != 1 || len(result.Kept) != 0 {
		t.Fatalf("result = %+v; survivor already had the row", result)
	}
	view, err := a.GetAssetInstallations("keeper")
	if err != nil || len(view.Installations) != 1 {
		t.Fatalf("keeper view = %+v, %v; want the one shared team row", view, err)
	}
}

// A typo'd survivor or source must fail BEFORE anything is retired —
// a missing asset reads as present=false, never as "reaches everyone".
func TestConsolidateAssetsMissingAssetsFailClosed(t *testing.T) {
	a, _, _ := scopedExtensionApp(t, "alice@example.com")
	marketplaceWith(t, a, "real-skill")
	if _, err := a.InstallMarketplaceExtension("real-skill", ExtensionScopeOrg); err != nil {
		t.Fatal(err)
	}

	if _, err := a.ConsolidateAssets("skill-doctor", "no-such-survivor", []string{"real-skill"}); err == nil ||
		!strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing survivor must fail closed, got %v", err)
	}
	// The source must be untouched by the failed call.
	assets, err := a.ListAssets()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, as := range assets {
		if as.Name == "real-skill" {
			found = true
		}
	}
	if !found {
		t.Fatal("failed consolidation must not retire the source")
	}

	if _, err := a.ConsolidateAssets("skill-doctor", "real-skill", []string{"no-such-dupe"}); err == nil ||
		!strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing source must fail closed, got %v", err)
	}
}
