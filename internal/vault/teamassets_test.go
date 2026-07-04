package vault

import (
	"reflect"
	"testing"

	"github.com/sleuth-io/sx/internal/manifest"
)

func TestManifestTeamAssetsDedupesNonAdjacentVersionRows(t *testing.T) {
	dir := t.TempDir()
	m := &manifest.Manifest{
		SchemaVersion: 2,
		Assets: []manifest.Asset{
			{Name: "a", Version: "1", Scopes: []manifest.Scope{{Kind: manifest.ScopeKindTeam, Team: "mk"}}},
			{Name: "b", Version: "1", Scopes: []manifest.Scope{{Kind: manifest.ScopeKindTeam, Team: "mk"}}},
			// A second version of "a" appended after "b" — non-adjacent.
			{Name: "a", Version: "2", Scopes: []manifest.Scope{{Kind: manifest.ScopeKindTeam, Team: "mk"}}},
		},
	}
	if err := manifest.Save(dir, m); err != nil {
		t.Fatal(err)
	}

	got, err := manifestTeamAssets(dir)
	if err != nil {
		t.Fatalf("manifestTeamAssets: %v", err)
	}
	want := map[string][]string{"mk": {"a", "b"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("teamAssets = %v, want %v", got, want)
	}
}
