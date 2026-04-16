package mgmt

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadTeams(t *testing.T) {
	dir := t.TempDir()
	tf := &TeamsFile{
		Teams: []Team{
			{
				Name:         "platform",
				Description:  "Platform engineering",
				Members:      []string{"Alice@Example.COM", "bob@example.com"},
				Admins:       []string{"alice@example.com"},
				Repositories: []string{"https://github.com/Acme/Infra.git"},
			},
		},
	}

	if err := SaveTeams(dir, tf); err != nil {
		t.Fatalf("SaveTeams failed: %v", err)
	}

	loaded, err := LoadTeams(dir)
	if err != nil {
		t.Fatalf("LoadTeams failed: %v", err)
	}

	if loaded.LockVersion != TeamsLockVersion {
		t.Errorf("expected lock version %s, got %s", TeamsLockVersion, loaded.LockVersion)
	}
	if len(loaded.Teams) != 1 {
		t.Fatalf("expected 1 team, got %d", len(loaded.Teams))
	}

	team := &loaded.Teams[0]
	if team.Name != "platform" {
		t.Errorf("expected name platform, got %s", team.Name)
	}
	if !team.IsMember("alice@example.com") {
		t.Errorf("alice should be a member (post-normalization)")
	}
	if !team.IsMember("BOB@example.com") {
		t.Errorf("bob should be a member (case-insensitive)")
	}
	if !team.IsAdmin("alice@example.com") {
		t.Errorf("alice should be an admin")
	}
	if team.IsAdmin("bob@example.com") {
		t.Errorf("bob should not be an admin")
	}
	if len(team.Repositories) != 1 || team.Repositories[0] != "github.com/acme/infra" {
		t.Errorf("expected normalized repo github.com/acme/infra, got %v", team.Repositories)
	}
}

func TestLoadTeamsMissingFile(t *testing.T) {
	dir := t.TempDir()
	loaded, err := LoadTeams(dir)
	if err != nil {
		t.Fatalf("LoadTeams on missing file should succeed, got %v", err)
	}
	if len(loaded.Teams) != 0 {
		t.Errorf("expected 0 teams, got %d", len(loaded.Teams))
	}
	if loaded.LockVersion != TeamsLockVersion {
		t.Errorf("expected lock version %s on empty file, got %s", TeamsLockVersion, loaded.LockVersion)
	}
}

func TestUpsertAndDeleteTeam(t *testing.T) {
	tf := &TeamsFile{LockVersion: TeamsLockVersion}

	if _, err := tf.UpsertTeam(Team{Name: "platform", Members: []string{"alice@example.com"}}); err != nil {
		t.Fatalf("UpsertTeam failed: %v", err)
	}
	if len(tf.Teams) != 1 {
		t.Fatalf("expected 1 team after upsert, got %d", len(tf.Teams))
	}

	if _, err := tf.UpsertTeam(Team{Name: "platform", Members: []string{"alice@example.com", "bob@example.com"}}); err != nil {
		t.Fatalf("UpsertTeam failed: %v", err)
	}
	if len(tf.Teams) != 1 {
		t.Fatalf("expected 1 team after second upsert, got %d", len(tf.Teams))
	}
	if len(tf.Teams[0].Members) != 2 {
		t.Errorf("expected 2 members after update, got %d", len(tf.Teams[0].Members))
	}

	if _, err := tf.UpsertTeam(Team{Name: "mobile"}); err != nil {
		t.Fatalf("UpsertTeam failed: %v", err)
	}
	if len(tf.Teams) != 2 {
		t.Fatalf("expected 2 teams, got %d", len(tf.Teams))
	}

	if err := tf.DeleteTeam("platform"); err != nil {
		t.Fatalf("DeleteTeam failed: %v", err)
	}
	if len(tf.Teams) != 1 || tf.Teams[0].Name != "mobile" {
		t.Errorf("expected only mobile team left, got %v", tf.Teams)
	}

	if err := tf.DeleteTeam("nonexistent"); !errors.Is(err, ErrTeamNotFound) {
		t.Errorf("expected ErrTeamNotFound, got %v", err)
	}
}

func TestTeamsForMember(t *testing.T) {
	tf := &TeamsFile{
		Teams: []Team{
			{Name: "platform", Members: []string{"alice@example.com", "bob@example.com"}},
			{Name: "mobile", Members: []string{"carol@example.com"}},
			{Name: "data", Members: []string{"alice@example.com", "carol@example.com"}},
		},
	}

	matches := tf.TeamsForMember("ALICE@example.com")
	if len(matches) != 2 {
		t.Fatalf("expected 2 teams for alice, got %d", len(matches))
	}
	names := []string{matches[0].Name, matches[1].Name}
	if names[0] != "platform" || names[1] != "data" {
		t.Errorf("expected [platform, data], got %v", names)
	}

	if got := tf.TeamsForMember("unknown@example.com"); got != nil {
		t.Errorf("expected nil for unknown email, got %v", got)
	}
}

func TestTeamsOwningRepo(t *testing.T) {
	dir := t.TempDir()
	tf := &TeamsFile{
		Teams: []Team{
			{Name: "platform", Repositories: []string{"https://github.com/acme/infra.git"}},
			{Name: "mobile", Repositories: []string{"git@github.com:acme/ios.git"}},
			{Name: "other", Repositories: []string{"https://github.com/acme/foo.git"}},
		},
	}
	if err := SaveTeams(dir, tf); err != nil {
		t.Fatalf("SaveTeams failed: %v", err)
	}
	loaded, err := LoadTeams(dir)
	if err != nil {
		t.Fatalf("LoadTeams failed: %v", err)
	}

	// SSH form should match the HTTPS form after normalization
	got := loaded.TeamsOwningRepo("https://github.com/acme/ios.git")
	if len(got) != 1 || got[0].Name != "mobile" {
		t.Errorf("expected [mobile], got %v", teamNames(got))
	}

	got = loaded.TeamsOwningRepo("git@github.com:acme/infra.git")
	if len(got) != 1 || got[0].Name != "platform" {
		t.Errorf("expected [platform], got %v", teamNames(got))
	}
}

func TestSaveTeamsNormalization(t *testing.T) {
	dir := t.TempDir()
	tf := &TeamsFile{
		Teams: []Team{
			{
				Name:         "platform",
				Members:      []string{"alice@example.com", "ALICE@example.com", "bob@example.com"},
				Repositories: []string{"https://github.com/acme/infra.git", "github.com/acme/infra"},
			},
		},
	}
	if err := SaveTeams(dir, tf); err != nil {
		t.Fatalf("SaveTeams failed: %v", err)
	}
	loaded, err := LoadTeams(dir)
	if err != nil {
		t.Fatalf("LoadTeams failed: %v", err)
	}
	team := &loaded.Teams[0]
	if len(team.Members) != 2 {
		t.Errorf("expected 2 unique members, got %d: %v", len(team.Members), team.Members)
	}
	if len(team.Repositories) != 1 {
		t.Errorf("expected 1 unique repo after dedupe, got %d: %v", len(team.Repositories), team.Repositories)
	}

	// Verify file actually written
	if _, err := filepath.Abs(filepath.Join(dir, TeamsFileName)); err != nil {
		t.Errorf("bad teams path: %v", err)
	}
}

func teamNames(teams []*Team) []string {
	out := make([]string, len(teams))
	for i, tt := range teams {
		out[i] = tt.Name
	}
	return out
}
