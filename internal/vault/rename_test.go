package vault

import (
	"testing"

	"github.com/sleuth-io/sx/internal/manifest"
	"github.com/sleuth-io/sx/internal/mgmt"
)

func TestRenameTeamCascadesReferences(t *testing.T) {
	mgmt.ResetActorCache()
	dir := t.TempDir()
	m := &manifest.Manifest{
		SchemaVersion: 2,
		Teams: []manifest.Team{
			{Name: "design", Members: []string{"a@example.com"}, Admins: []string{"a@example.com"}},
		},
		Bots: []manifest.Bot{{Name: "deploy-bot", Teams: []string{"design"}}},
		Assets: []manifest.Asset{
			{Name: "x", Version: "1", Scopes: []manifest.Scope{{Kind: manifest.ScopeKindTeam, Team: "design"}}},
		},
	}
	if err := manifest.Save(dir, m); err != nil {
		t.Fatal(err)
	}

	actor := mgmt.Actor{Email: "a@example.com"}
	if err := commonRenameTeam(dir, actor, "design", "brand"); err != nil {
		t.Fatalf("rename: %v", err)
	}

	got, _, err := manifest.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := got.FindTeam("brand"); err != nil {
		t.Fatal("renamed team missing")
	}
	if _, err := got.FindTeam("design"); err == nil {
		t.Fatal("old team name still present")
	}
	if got.Assets[0].Scopes[0].Team != "brand" {
		t.Fatalf("team scope not renamed: %+v", got.Assets[0].Scopes[0])
	}
	if got.Bots[0].Teams[0] != "brand" {
		t.Fatalf("bot membership not renamed: %+v", got.Bots[0].Teams)
	}

	// Renaming onto an existing team is refused.
	if err := commonRenameTeam(dir, actor, "brand", "brand"); err != nil {
		t.Fatalf("same-name rename should no-op: %v", err)
	}
}

func TestRenameCollection(t *testing.T) {
	mgmt.ResetActorCache()
	dir := t.TempDir()
	m := &manifest.Manifest{
		SchemaVersion: 2,
		Collections:   []manifest.Collection{{Name: "starter", Assets: []string{"x"}}},
	}
	if err := manifest.Save(dir, m); err != nil {
		t.Fatal(err)
	}
	actor := mgmt.Actor{Email: "a@example.com"}
	if err := commonRenameCollection(dir, actor, "starter", "essentials"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	got, _, err := manifest.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	c, err := got.FindCollection("essentials")
	if err != nil || len(c.Assets) != 1 {
		t.Fatalf("renamed collection = %+v, %v", c, err)
	}
}
