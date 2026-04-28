package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/asset"
)

func TestRoundTrip_AllScopeKinds(t *testing.T) {
	m := &Manifest{
		SchemaVersion: CurrentSchemaVersion,
		CreatedBy:     "sx test",
		Assets: []Asset{
			{
				Name:    "global-skill",
				Version: "1.0.0",
				Type:    asset.TypeSkill,
				SourceHTTP: &SourceHTTP{
					URL:    "https://example.com/global.zip",
					Hashes: map[string]string{"sha256": "abc"},
				},
			},
			{
				Name:    "scoped-skill",
				Version: "2.0.0",
				Type:    asset.TypeSkill,
				SourceHTTP: &SourceHTTP{
					URL:    "https://example.com/scoped.zip",
					Hashes: map[string]string{"sha256": "def"},
				},
				Scopes: []Scope{
					{Kind: ScopeKindRepo, Repo: "github.com/acme/infra"},
					{Kind: ScopeKindPath, Repo: "github.com/acme/docs", Paths: []string{"README.md"}},
					{Kind: ScopeKindTeam, Team: "platform"},
					{Kind: ScopeKindUser, User: "alice@acme.com"},
				},
			},
		},
		Teams: []Team{
			{
				Name:         "platform",
				Description:  "Platform eng",
				Members:      []string{"alice@acme.com", "bob@acme.com"},
				Admins:       []string{"alice@acme.com"},
				Repositories: []string{"github.com/acme/infra"},
			},
		},
	}

	data, err := Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	parsed, err := Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if parsed.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("schema version: got %d want %d", parsed.SchemaVersion, CurrentSchemaVersion)
	}
	if len(parsed.Assets) != 2 {
		t.Fatalf("assets: got %d want 2", len(parsed.Assets))
	}
	if len(parsed.Teams) != 1 {
		t.Fatalf("teams: got %d want 1", len(parsed.Teams))
	}

	if scoped := parsed.FindAsset("scoped-skill"); scoped == nil {
		t.Fatal("scoped-skill not found after round-trip")
	} else if got := len(scoped.Scopes); got != 4 {
		t.Errorf("scoped-skill scopes: got %d want 4", got)
	}

	team := parsed.Teams[0]
	if team.Name != "platform" {
		t.Errorf("team name: got %q", team.Name)
	}
	if !team.IsMember("ALICE@acme.com") {
		t.Error("IsMember case-insensitive check failed")
	}
	if !team.IsAdmin("alice@ACME.com") {
		t.Error("IsAdmin case-insensitive check failed")
	}
}

func TestRoundTrip_Bots(t *testing.T) {
	m := &Manifest{
		SchemaVersion: CurrentSchemaVersion,
		Teams: []Team{
			{Name: "platform", Members: []string{"alice@acme.com"}, Admins: []string{"alice@acme.com"}},
		},
		Bots: []Bot{
			{Name: "python-backend", Description: "Backend CI bot", Teams: []string{"platform"}},
			{Name: "frontend-bot"},
		},
		Assets: []Asset{
			{
				Name: "deploy", Version: "1.0.0", Type: asset.TypeSkill,
				SourceHTTP: &SourceHTTP{URL: "https://example.com/d.zip", Hashes: map[string]string{"sha256": "x"}},
				Scopes: []Scope{
					{Kind: ScopeKindBot, Bot: "python-backend"},
					{Kind: ScopeKindOrg},
				},
			},
		},
	}

	data, err := Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	parsed, err := Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(parsed.Bots) != 2 {
		t.Fatalf("bots: got %d want 2", len(parsed.Bots))
	}
	bot, err := parsed.FindBot("python-backend")
	if err != nil {
		t.Fatalf("FindBot: %v", err)
	}
	if !bot.IsOnTeam("platform") {
		t.Error("expected python-backend on platform team")
	}
	if got := parsed.Assets[0].Scopes; len(got) != 2 {
		t.Fatalf("scopes: got %d want 2", len(got))
	}
	if parsed.Assets[0].Scopes[0].Kind != ScopeKindBot || parsed.Assets[0].Scopes[0].Bot != "python-backend" {
		t.Errorf("bot scope round-trip: %+v", parsed.Assets[0].Scopes[0])
	}
}

func TestBotUpsertDelete(t *testing.T) {
	m := &Manifest{SchemaVersion: CurrentSchemaVersion}

	if _, err := m.UpsertBot(Bot{Name: "  "}); !errors.Is(err, ErrEmptyBotName) {
		t.Errorf("empty name: got %v want ErrEmptyBotName", err)
	}

	if _, err := m.UpsertBot(Bot{Name: "ci", Teams: []string{"platform"}}); err != nil {
		t.Fatalf("upsert create: %v", err)
	}
	if len(m.Bots) != 1 {
		t.Fatalf("bots after create: got %d want 1", len(m.Bots))
	}
	// Replace
	if _, err := m.UpsertBot(Bot{Name: "ci", Description: "updated"}); err != nil {
		t.Fatalf("upsert replace: %v", err)
	}
	if m.Bots[0].Description != "updated" {
		t.Errorf("description: got %q want updated", m.Bots[0].Description)
	}
	if err := m.DeleteBot("ci"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := m.DeleteBot("ci"); !errors.Is(err, ErrBotNotFound) {
		t.Errorf("delete missing: got %v want ErrBotNotFound", err)
	}
}

func TestParse_UnsupportedSchema(t *testing.T) {
	data := []byte("schema_version = 99\n")
	_, err := Parse(data)
	if err == nil {
		t.Fatal("expected ErrUnsupportedSchema, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported manifest schema") {
		t.Errorf("wrong error: %v", err)
	}
}

func TestParse_MissingSchemaDefaultsToCurrent(t *testing.T) {
	data := []byte("created_by = \"sx test\"\n")
	m, err := Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.SchemaVersion != CurrentSchemaVersion {
		t.Errorf("default schema version: got %d want %d", m.SchemaVersion, CurrentSchemaVersion)
	}
}

func TestNormalization_EmailsAndRepos(t *testing.T) {
	m := &Manifest{
		Teams: []Team{
			{
				Name: "  platform  ",
				Members: []string{
					"  Alice@Acme.com  ",
					"bob@acme.com",
					"ALICE@acme.com", // duplicate of Alice after normalize
				},
				Admins: []string{"ALICE@acme.com"},
			},
		},
	}

	data, err := Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	parsed, _ := Parse(data)
	team := parsed.Teams[0]
	if team.Name != "platform" {
		t.Errorf("team name trim: got %q", team.Name)
	}
	if len(team.Members) != 2 {
		t.Errorf("dedupe: got %d want 2 — members=%v", len(team.Members), team.Members)
	}
	// Admins should be lowercased.
	if team.Admins[0] != "alice@acme.com" {
		t.Errorf("admin lowercase: got %q", team.Admins[0])
	}
}

func TestScopeValidate(t *testing.T) {
	cases := []struct {
		name    string
		scope   Scope
		wantErr bool
	}{
		{"org empty", Scope{Kind: ScopeKindOrg}, false},
		{"repo missing url", Scope{Kind: ScopeKindRepo}, true},
		{"repo ok", Scope{Kind: ScopeKindRepo, Repo: "github.com/x/y"}, false},
		{"path missing paths", Scope{Kind: ScopeKindPath, Repo: "github.com/x/y"}, true},
		{"path ok", Scope{Kind: ScopeKindPath, Repo: "github.com/x/y", Paths: []string{"a"}}, false},
		{"team missing", Scope{Kind: ScopeKindTeam}, true},
		{"team ok", Scope{Kind: ScopeKindTeam, Team: "core"}, false},
		{"user missing", Scope{Kind: ScopeKindUser}, true},
		{"user ok", Scope{Kind: ScopeKindUser, User: "a@b.c"}, false},
		{"bot missing", Scope{Kind: ScopeKindBot}, true},
		{"bot ok", Scope{Kind: ScopeKindBot, Bot: "python-backend"}, false},
		{"unknown kind", Scope{Kind: "bogus"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.scope.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("got err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestTeamUpsertDelete(t *testing.T) {
	m := &Manifest{SchemaVersion: CurrentSchemaVersion}

	if _, err := m.UpsertTeam(Team{Name: "  "}); !errors.Is(err, ErrEmptyTeamName) {
		t.Errorf("empty name: got %v want ErrEmptyTeamName", err)
	}

	if _, err := m.UpsertTeam(Team{Name: "core", Members: []string{"a@b.c"}}); err != nil {
		t.Fatalf("upsert create: %v", err)
	}
	if len(m.Teams) != 1 {
		t.Fatalf("teams after create: got %d", len(m.Teams))
	}

	if _, err := m.UpsertTeam(Team{Name: "core", Members: []string{"c@d.e"}}); err != nil {
		t.Fatalf("upsert replace: %v", err)
	}
	if len(m.Teams) != 1 {
		t.Errorf("teams after replace: got %d want 1", len(m.Teams))
	}
	if m.Teams[0].Members[0] != "c@d.e" {
		t.Errorf("upsert did not replace members: %v", m.Teams[0].Members)
	}

	if _, err := m.FindTeam("missing"); !errors.Is(err, ErrTeamNotFound) {
		t.Errorf("find missing: got %v want ErrTeamNotFound", err)
	}

	if err := m.DeleteTeam("core"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(m.Teams) != 0 {
		t.Errorf("teams after delete: got %d", len(m.Teams))
	}
	if err := m.DeleteTeam("core"); !errors.Is(err, ErrTeamNotFound) {
		t.Errorf("delete missing: got %v want ErrTeamNotFound", err)
	}
}

func TestAssetUpsertRemove(t *testing.T) {
	m := &Manifest{SchemaVersion: CurrentSchemaVersion}

	m.UpsertAsset(Asset{Name: "foo", Version: "1.0.0", Type: asset.TypeSkill})
	m.UpsertAsset(Asset{Name: "foo", Version: "2.0.0", Type: asset.TypeSkill})
	m.UpsertAsset(Asset{Name: "bar", Version: "1.0.0", Type: asset.TypeSkill})

	if len(m.Assets) != 3 {
		t.Fatalf("assets: got %d want 3", len(m.Assets))
	}

	// Replace existing (name, version) tuple.
	m.UpsertAsset(Asset{Name: "foo", Version: "1.0.0", Type: asset.TypeSkill, Clients: []string{"claude"}})
	if len(m.Assets) != 3 {
		t.Errorf("replace should not grow: got %d", len(m.Assets))
	}
	if len(m.FindAsset("foo").Clients) != 1 {
		t.Error("upsert did not replace asset body")
	}

	// Remove all versions of foo.
	removed := m.RemoveAsset("foo", "")
	if removed != 2 {
		t.Errorf("removed: got %d want 2", removed)
	}
	if len(m.Assets) != 1 {
		t.Errorf("remaining: got %d want 1", len(m.Assets))
	}
}

func TestSaveLoad(t *testing.T) {
	dir := t.TempDir()

	m := &Manifest{
		SchemaVersion: CurrentSchemaVersion,
		CreatedBy:     "sx test",
		Teams: []Team{
			{Name: "core", Members: []string{"a@b.c"}, Admins: []string{"a@b.c"}},
		},
	}
	if err := Save(dir, m); err != nil {
		t.Fatalf("save: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Fatalf("stat: %v", err)
	}

	loaded, exists, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !exists {
		t.Fatal("load: exists=false for just-written file")
	}
	if loaded.Teams[0].Name != "core" {
		t.Errorf("loaded team name: got %q", loaded.Teams[0].Name)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	dir := t.TempDir()
	m, exists, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if exists {
		t.Error("exists=true for missing file")
	}
	if m != nil {
		t.Error("manifest should be nil when missing")
	}
}

func TestTeamsForMember(t *testing.T) {
	m := &Manifest{
		Teams: []Team{
			{Name: "a", Members: []string{"alice@acme.com", "bob@acme.com"}},
			{Name: "b", Members: []string{"alice@acme.com"}},
			{Name: "c", Members: []string{"carol@acme.com"}},
		},
	}

	got := m.TeamsForMember("ALICE@acme.com")
	if len(got) != 2 {
		t.Errorf("alice teams: got %d want 2", len(got))
	}

	if m.TeamsForMember("") != nil {
		t.Error("empty email should return nil")
	}
	if m.TeamsForMember("noone@acme.com") != nil {
		t.Error("unknown email should return nil")
	}
}
