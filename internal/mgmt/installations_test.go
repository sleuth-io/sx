package mgmt

import (
	"testing"
)

func TestSaveAndLoadInstallations(t *testing.T) {
	dir := t.TempDir()
	ifile := &InstallationsFile{
		Installations: []Installation{
			{Asset: "my-skill", Kind: InstallKindTeam, Team: "platform"},
			{Asset: "my-skill", Kind: InstallKindUser, User: "Alice@Example.com"},
		},
	}

	if err := SaveInstallations(dir, ifile); err != nil {
		t.Fatalf("SaveInstallations failed: %v", err)
	}

	loaded, ok, err := LoadInstallations(dir)
	if err != nil {
		t.Fatalf("LoadInstallations failed: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true after save")
	}
	if len(loaded.Installations) != 2 {
		t.Fatalf("expected 2 installations, got %d", len(loaded.Installations))
	}

	// Email should be normalized on save
	if loaded.Installations[1].User != "alice@example.com" {
		t.Errorf("expected normalized email, got %s", loaded.Installations[1].User)
	}
}

func TestLoadInstallationsMissingFile(t *testing.T) {
	dir := t.TempDir()
	ifile, ok, err := LoadInstallations(dir)
	if err != nil {
		t.Fatalf("LoadInstallations on missing file should succeed, got %v", err)
	}
	if ok {
		t.Error("expected ok=false for missing file")
	}
	if ifile != nil {
		t.Errorf("expected nil file, got %v", ifile)
	}
}

func TestInstallationValidate(t *testing.T) {
	tests := []struct {
		name    string
		ins     Installation
		wantErr bool
	}{
		{"valid team", Installation{Asset: "my-skill", Kind: InstallKindTeam, Team: "platform"}, false},
		{"valid user", Installation{Asset: "my-skill", Kind: InstallKindUser, User: "a@b.com"}, false},
		{"team missing name", Installation{Asset: "my-skill", Kind: InstallKindTeam}, true},
		{"user missing email", Installation{Asset: "my-skill", Kind: InstallKindUser}, true},
		{"missing asset", Installation{Kind: InstallKindTeam, Team: "platform"}, true},
		{"unknown kind", Installation{Asset: "my-skill", Kind: "wut", Team: "x"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.ins.Validate()
			if tc.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

func TestInstallationsForAsset(t *testing.T) {
	ifile := &InstallationsFile{
		Installations: []Installation{
			{Asset: "my-skill", Kind: InstallKindTeam, Team: "platform"},
			{Asset: "other", Kind: InstallKindTeam, Team: "platform"},
			{Asset: "my-skill", Kind: InstallKindUser, User: "alice@example.com"},
		},
	}

	got := ifile.ForAsset("my-skill")
	if len(got) != 2 {
		t.Errorf("expected 2 installations for my-skill, got %d", len(got))
	}

	got = ifile.ForAsset("nonexistent")
	if len(got) != 0 {
		t.Errorf("expected 0 installations, got %d", len(got))
	}
}

func TestInstallationsUpsertDedupes(t *testing.T) {
	ifile := &InstallationsFile{}

	if !ifile.Upsert(Installation{Asset: "my-skill", Kind: InstallKindTeam, Team: "platform"}) {
		t.Error("first upsert should return true")
	}
	if ifile.Upsert(Installation{Asset: "my-skill", Kind: InstallKindTeam, Team: "platform"}) {
		t.Error("duplicate upsert should return false")
	}
	if len(ifile.Installations) != 1 {
		t.Errorf("expected 1 installation, got %d", len(ifile.Installations))
	}

	// Email normalization makes this a duplicate
	if ifile.Upsert(Installation{Asset: "my-skill", Kind: InstallKindUser, User: "Alice@Example.com"}) != true {
		t.Error("expected new user upsert to succeed")
	}
	if ifile.Upsert(Installation{Asset: "my-skill", Kind: InstallKindUser, User: "alice@example.com"}) {
		t.Error("case-different email should be duplicate")
	}
	if len(ifile.Installations) != 2 {
		t.Errorf("expected 2 installations, got %d", len(ifile.Installations))
	}
}

func TestInstallationsRemove(t *testing.T) {
	ifile := &InstallationsFile{
		Installations: []Installation{
			{Asset: "my-skill", Kind: InstallKindTeam, Team: "platform"},
			{Asset: "my-skill", Kind: InstallKindTeam, Team: "mobile"},
			{Asset: "my-skill", Kind: InstallKindUser, User: "alice@example.com"},
			{Asset: "other", Kind: InstallKindTeam, Team: "platform"},
		},
	}

	// Remove by specific team
	removed := ifile.Remove(Installation{Asset: "my-skill", Kind: InstallKindTeam, Team: "platform"})
	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}
	if len(ifile.Installations) != 3 {
		t.Errorf("expected 3 remaining, got %d", len(ifile.Installations))
	}

	// Remove all my-skill installs
	removed = ifile.RemoveForAsset("my-skill")
	if removed != 2 {
		t.Errorf("expected 2 removed, got %d", removed)
	}
	if len(ifile.Installations) != 1 || ifile.Installations[0].Asset != "other" {
		t.Errorf("expected only 'other' left, got %v", ifile.Installations)
	}
}
