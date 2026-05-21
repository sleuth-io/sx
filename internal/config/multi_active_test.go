package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/sleuth-io/sx/internal/utils"
)

// writeConfig persists a backwards-compatible config payload at the
// canonical location for tests that exercise the multi-active resolver.
func writeConfig(t *testing.T, payload map[string]any) {
	t.Helper()
	configFile, err := utils.GetConfigFile()
	if err != nil {
		t.Fatalf("config file path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(configFile), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(configFile, data, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func newTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	SetActiveProfile("")
	t.Setenv("SX_PROFILE", "")
}

func TestEnsureActiveProfilesBackfillsFromDefault(t *testing.T) {
	newTestHome(t)
	writeConfig(t, map[string]any{
		"defaultProfile": "work",
		"profiles": map[string]any{
			"work":     map[string]any{"type": "git", "repositoryUrl": "g@x:work"},
			"personal": map[string]any{"type": "git", "repositoryUrl": "g@x:personal"},
		},
	})

	mpc, err := LoadMultiProfile()
	if err != nil {
		t.Fatalf("LoadMultiProfile: %v", err)
	}
	if got := mpc.ActiveProfiles; !slices.Equal(got, []string{"work"}) {
		t.Fatalf("ActiveProfiles=%v, want [work]", got)
	}
}

func TestGetActiveProfileNamesPlacesDefaultFirst(t *testing.T) {
	mpc := &MultiProfileConfig{
		DefaultProfile: "work",
		ActiveProfiles: []string{"personal", "work"},
		Profiles: map[string]*Profile{
			"work":     {Type: RepositoryTypeGit, RepositoryURL: "g@x:w"},
			"personal": {Type: RepositoryTypeGit, RepositoryURL: "g@x:p"},
		},
	}
	got := GetActiveProfileNames(mpc)
	want := []string{"work", "personal"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestGetActiveProfileNamesEnvOverride(t *testing.T) {
	newTestHome(t)
	mpc := &MultiProfileConfig{
		DefaultProfile: "work",
		ActiveProfiles: []string{"work"},
		Profiles: map[string]*Profile{
			"work":     {Type: RepositoryTypeGit, RepositoryURL: "g@x:w"},
			"personal": {Type: RepositoryTypeGit, RepositoryURL: "g@x:p"},
		},
	}
	t.Setenv("SX_PROFILE", "personal, work")
	defer t.Setenv("SX_PROFILE", "")
	got := GetActiveProfileNames(mpc)
	want := []string{"personal", "work"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestActivateDeactivate(t *testing.T) {
	mpc := &MultiProfileConfig{
		DefaultProfile: "work",
		ActiveProfiles: []string{"work"},
		Profiles: map[string]*Profile{
			"work":     {Type: RepositoryTypeGit, RepositoryURL: "g@x:w"},
			"personal": {Type: RepositoryTypeGit, RepositoryURL: "g@x:p"},
		},
	}

	if err := mpc.Activate("personal"); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if !mpc.IsProfileActive("personal") {
		t.Fatalf("personal should be active after Activate")
	}
	if !slices.Equal(mpc.ActiveProfiles, []string{"work", "personal"}) {
		t.Fatalf("ActiveProfiles=%v", mpc.ActiveProfiles)
	}

	if err := mpc.Deactivate("work"); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	if mpc.DefaultProfile != "personal" {
		t.Fatalf("DefaultProfile should fall back to remaining active, got %s", mpc.DefaultProfile)
	}

	if err := mpc.Deactivate("personal"); err == nil {
		t.Fatalf("Deactivate of last active should fail")
	}
}

func TestSetDefaultProfileAutoActivates(t *testing.T) {
	mpc := &MultiProfileConfig{
		DefaultProfile: "work",
		ActiveProfiles: []string{"work"},
		Profiles: map[string]*Profile{
			"work":     {Type: RepositoryTypeGit, RepositoryURL: "g@x:w"},
			"personal": {Type: RepositoryTypeGit, RepositoryURL: "g@x:p"},
		},
	}
	if err := mpc.SetDefaultProfile("personal"); err != nil {
		t.Fatalf("SetDefaultProfile: %v", err)
	}
	if !mpc.IsProfileActive("personal") {
		t.Fatalf("personal should be auto-activated when set as default")
	}
}

func TestLoadActiveReturnsEveryActive(t *testing.T) {
	newTestHome(t)
	writeConfig(t, map[string]any{
		"defaultProfile": "work",
		"activeProfiles": []string{"work", "personal"},
		"profiles": map[string]any{
			"work":     map[string]any{"type": "git", "repositoryUrl": "g@x:w", "identity": "work@example.com"},
			"personal": map[string]any{"type": "git", "repositoryUrl": "g@x:p", "identity": "me@personal.com"},
		},
	})

	configs, mpc, err := LoadActive()
	if err != nil {
		t.Fatalf("LoadActive: %v", err)
	}
	if mpc.DefaultProfile != "work" {
		t.Fatalf("DefaultProfile=%s", mpc.DefaultProfile)
	}
	if len(configs) != 2 {
		t.Fatalf("got %d configs, want 2", len(configs))
	}
	if configs[0].ProfileName != "work" {
		t.Fatalf("expected work first (default), got %s", configs[0].ProfileName)
	}
	identities := []string{configs[0].Identity, configs[1].Identity}
	wantIdentities := []string{"work@example.com", "me@personal.com"}
	if !reflect.DeepEqual(identities, wantIdentities) {
		t.Fatalf("identities=%v want %v", identities, wantIdentities)
	}
}

func TestDeleteProfileKeepsActiveSetNonEmpty(t *testing.T) {
	mpc := &MultiProfileConfig{
		DefaultProfile: "work",
		ActiveProfiles: []string{"work", "personal"},
		Profiles: map[string]*Profile{
			"work":     {Type: RepositoryTypeGit, RepositoryURL: "g@x:w"},
			"personal": {Type: RepositoryTypeGit, RepositoryURL: "g@x:p"},
		},
	}
	if err := mpc.DeleteProfile("work"); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}
	if mpc.DefaultProfile != "personal" {
		t.Fatalf("DefaultProfile should fall back, got %s", mpc.DefaultProfile)
	}
	if !slices.Equal(mpc.ActiveProfiles, []string{"personal"}) {
		t.Fatalf("ActiveProfiles=%v", mpc.ActiveProfiles)
	}
}
