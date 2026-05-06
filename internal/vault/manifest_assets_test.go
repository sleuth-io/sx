package vault

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/lockfile"
	"github.com/sleuth-io/sx/internal/manifest"
)

// TestUpsertAssetInManifest_PreservesClientsOnReUpsert covers the re-add
// path: handleIdenticalAsset / configureRuleScopes / configureExistingAsset
// build a lockfile.Asset without metadata in scope, so the incoming asset
// has empty Clients. Without preservation, the upsert would wipe an
// author-declared client filter on every scope reconfigure.
func TestUpsertAssetInManifest_PreservesClientsOnReUpsert(t *testing.T) {
	vaultRoot := t.TempDir()

	// Seed with an entry that has Clients set (simulates initial sx add).
	seeded := manifest.Asset{
		Name:    "claude-only-skill",
		Version: "1.0.0",
		Type:    asset.TypeSkill,
		Clients: []string{"claude-code"},
	}
	m := &manifest.Manifest{}
	m.UpsertAsset(seeded)
	if err := manifest.Save(vaultRoot, m); err != nil {
		t.Fatalf("seed save: %v", err)
	}

	// Re-upsert the same asset with empty Clients (the re-add shape).
	incoming := &lockfile.Asset{
		Name:    "claude-only-skill",
		Version: "1.0.0",
		Type:    asset.TypeSkill,
		// Clients intentionally empty — caller had no metadata in scope.
		SourcePath: &lockfile.SourcePath{Path: filepath.Join("./assets", "claude-only-skill", "1.0.0")},
	}
	if err := upsertAssetInManifest(vaultRoot, incoming); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Round-trip and check Clients survived.
	m2, err := loadManifest(vaultRoot)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got := m2.FindAsset("claude-only-skill")
	if got == nil {
		t.Fatal("asset missing after re-upsert")
	}
	if !slices.Equal(got.Clients, []string{"claude-code"}) {
		t.Errorf("Clients = %v, want preserved [claude-code]", got.Clients)
	}
}

// TestUpsertAssetInManifest_NewVersionDoesNotInherit covers the regression
// where a new version that intentionally drops [asset].clients would
// silently inherit the prior version's restriction (because manifest
// stores one row per name+version, and a global FindAsset(name) lookup
// would hit the older entry). Inheritance must be scoped to the *same*
// version so re-adds preserve, but new versions don't.
func TestUpsertAssetInManifest_NewVersionDoesNotInherit(t *testing.T) {
	vaultRoot := t.TempDir()

	v1 := manifest.Asset{
		Name:    "skill",
		Version: "1.0.0",
		Type:    asset.TypeSkill,
		Clients: []string{"claude-code"},
	}
	m := &manifest.Manifest{}
	m.UpsertAsset(v1)
	if err := manifest.Save(vaultRoot, m); err != nil {
		t.Fatalf("seed save: %v", err)
	}

	// Add v2 with no Clients (user removed [asset].clients in metadata).
	v2 := &lockfile.Asset{
		Name:    "skill",
		Version: "2.0.0",
		Type:    asset.TypeSkill,
		// Clients intentionally empty — represents the user removing the field.
	}
	if err := upsertAssetInManifest(vaultRoot, v2); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	m2, err := loadManifest(vaultRoot)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	var got *manifest.Asset
	for i := range m2.Assets {
		if m2.Assets[i].Version == "2.0.0" {
			got = &m2.Assets[i]
		}
	}
	if got == nil {
		t.Fatal("v2 missing")
	}
	if len(got.Clients) != 0 {
		t.Errorf("v2 Clients = %v, want empty (must not inherit from v1)", got.Clients)
	}
}

// TestUpsertAssetInManifest_IncomingClientsWins covers the inverse: when
// the incoming asset *does* carry Clients (the addNewAsset path now does
// this), the upsert should write that value rather than inherit from the
// existing entry. Otherwise an author who narrows or broadens [asset].clients
// in a new version would not see the change reflected.
func TestUpsertAssetInManifest_IncomingClientsWins(t *testing.T) {
	vaultRoot := t.TempDir()

	seeded := manifest.Asset{
		Name:    "skill",
		Version: "1.0.0",
		Type:    asset.TypeSkill,
		Clients: []string{"claude-code"},
	}
	m := &manifest.Manifest{}
	m.UpsertAsset(seeded)
	if err := manifest.Save(vaultRoot, m); err != nil {
		t.Fatalf("seed save: %v", err)
	}

	// Same name+version, incoming carries a different Clients list.
	incoming := &lockfile.Asset{
		Name:    "skill",
		Version: "1.0.0",
		Type:    asset.TypeSkill,
		Clients: []string{"cursor", "gemini"},
	}
	if err := upsertAssetInManifest(vaultRoot, incoming); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	m2, err := loadManifest(vaultRoot)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got := m2.FindAsset("skill")
	if got == nil {
		t.Fatal("asset missing")
	}
	if !slices.Equal(got.Clients, []string{"cursor", "gemini"}) {
		t.Errorf("Clients = %v, want incoming [cursor gemini]", got.Clients)
	}
}
