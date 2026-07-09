package vault

import (
	"context"
	"net/http"
	"net/http/httptest"
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

// For USER installations the server's entityName is a display name (or a
// provider username) — the email lives in entityRef. ListUserAssets must
// key on the email or nobody's "My skills" ever matches their identity.
func TestSleuthListUserAssetsKeysOnEntityRef(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"vault":{"assets":{
			"pageInfo":{"hasNextPage":false,"endCursor":null},
			"nodes":[
				{"__typename":"Skill","slug":"alice-notes","name":"Alice Notes","installations":[
					{"entityType":"USER","entityName":"Dylan Etkin","entityRef":"Dylan.Etkin@gmail.com","entityId":"u1","monoRepoConfigId":null,"viaCollectionId":null}
				]},
				{"__typename":"Skill","slug":"team-thing","name":"Team Thing","installations":[
					{"entityType":"TEAM","entityName":"platform","entityRef":null,"entityId":"t1","monoRepoConfigId":null,"viaCollectionId":null}
				]},
				{"__typename":"Rule","slug":"legacy","name":"Legacy","installations":[
					{"entityType":"USER","entityName":"bob@example.com","entityRef":null,"entityId":"u2","monoRepoConfigId":null,"viaCollectionId":null}
				]}
			]}}}}`))
	}))
	t.Cleanup(server.Close)

	v := NewSleuthVault(server.URL, "test-token")
	out, err := v.ListUserAssets(context.Background())
	if err != nil {
		t.Fatalf("ListUserAssets: %v", err)
	}
	// entityRef wins and is normalized; a team install contributes nothing.
	if got := out["dylan.etkin@gmail.com"]; len(got) != 1 || got[0] != "alice-notes" {
		t.Fatalf("by ref = %+v, want [alice-notes]; full map %+v", got, out)
	}
	// No ref (older server): entityName is the fallback.
	if got := out["bob@example.com"]; len(got) != 1 || got[0] != "legacy" {
		t.Fatalf("fallback = %+v; full map %+v", got, out)
	}
	if len(out) != 2 {
		t.Fatalf("unexpected extra keys: %+v", out)
	}
}
