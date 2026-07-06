package vault

import (
	"reflect"
	"testing"

	"github.com/sleuth-io/sx/internal/manifest"
)

func TestManifestRepoAssetsGroupsRepoAndPathScopes(t *testing.T) {
	dir := t.TempDir()
	repo := "https://github.com/acme/app.git"
	m := &manifest.Manifest{
		SchemaVersion: 2,
		Assets: []manifest.Asset{
			{Name: "a", Version: "1", Scopes: []manifest.Scope{{Kind: manifest.ScopeKindRepo, Repo: repo}}},
			// Path scopes name a repository too — the asset belongs to it.
			{Name: "b", Version: "1", Scopes: []manifest.Scope{{Kind: manifest.ScopeKindPath, Repo: repo, Paths: []string{"services/api"}}}},
			// Non-repo scopes are ignored.
			{Name: "c", Version: "1", Scopes: []manifest.Scope{{Kind: manifest.ScopeKindTeam, Team: "mk"}}},
			// A second version of "a" appended after "b" — non-adjacent dedupe.
			{Name: "a", Version: "2", Scopes: []manifest.Scope{{Kind: manifest.ScopeKindRepo, Repo: repo}}},
		},
	}
	if err := manifest.Save(dir, m); err != nil {
		t.Fatal(err)
	}

	got, err := manifestRepoAssets(dir)
	if err != nil {
		t.Fatalf("manifestRepoAssets: %v", err)
	}
	want := map[string][]string{repo: {"a", "b"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("repoAssets = %v, want %v", got, want)
	}
}
