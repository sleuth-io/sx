package commands

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sleuth-io/sx/internal/config"
)

// setupSleuthConfig creates a Sleuth-type config pointing to the given server URL.
func setupSleuthConfig(t *testing.T, homeDir, serverURL string) {
	t.Helper()

	configDir := filepath.Join(homeDir, ".config", "sx")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("Failed to create config dir: %v", err)
	}

	mpc := &config.MultiProfileConfig{
		DefaultProfile: "default",
		Profiles: map[string]*config.Profile{
			"default": {
				Type:      config.RepositoryTypeSleuth,
				ServerURL: serverURL,
				AuthToken: "test-token",
			},
		},
	}

	data, err := json.MarshalIndent(mpc, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	configFile := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(configFile, data, 0600); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
}

// TestRoleCommandRejectsLocalProfile verifies that all role subcommands
// return an error when the active profile is not a Sleuth profile.
func TestRoleCommandRejectsLocalProfile(t *testing.T) {
	env := NewTestEnv(t)
	env.SetupPathVault()

	subcommands := []struct {
		name string
		args []string
	}{
		{"list", []string{"list"}},
		{"set", []string{"set", "some-slug"}},
		{"clear", []string{"clear"}},
		{"current", []string{"current"}},
	}

	for _, sc := range subcommands {
		t.Run(sc.name, func(t *testing.T) {
			cmd := NewRoleCommand()
			cmd.SetArgs(sc.args)
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			err := cmd.Execute()
			if err == nil {
				t.Fatal("Expected error for local profile, got nil")
			}
			if !strings.Contains(err.Error(), "only supported for remote") {
				t.Errorf("Expected 'only supported for remote' in error, got: %v", err)
			}
		})
	}
}

// TestRoleList tests the role list command with a mock server.
func TestRoleList(t *testing.T) {
	t.Run("lists roles and marks active", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/skills/sx.profiles" {
				t.Errorf("Unexpected path: %s", r.URL.Path)
			}
			if r.Method != http.MethodGet {
				t.Errorf("Expected GET, got %s", r.Method)
			}

			active := "backend"
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"profiles": []map[string]string{
					{"title": "Backend Engineer", "slug": "backend", "description": "Backend development role"},
					{"title": "Frontend Engineer", "slug": "frontend", "description": "Frontend development role"},
				},
				"active": active,
			})
		}))
		defer server.Close()

		env := NewTestEnv(t)
		setupSleuthConfig(t, env.HomeDir, server.URL)

		cmd := NewRoleCommand()
		cmd.SetArgs([]string{"list"})
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)

		if err := cmd.Execute(); err != nil {
			t.Fatalf("role list failed: %v", err)
		}

		output := stdout.String()

		if !strings.Contains(output, "Backend Engineer") {
			t.Errorf("Expected 'Backend Engineer' in output, got:\n%s", output)
		}
		if !strings.Contains(output, "Frontend Engineer") {
			t.Errorf("Expected 'Frontend Engineer' in output, got:\n%s", output)
		}
		if !strings.Contains(output, "backend") {
			t.Errorf("Expected slug 'backend' in output, got:\n%s", output)
		}
		if !strings.Contains(output, "frontend") {
			t.Errorf("Expected slug 'frontend' in output, got:\n%s", output)
		}
		if !strings.Contains(output, "Backend development role") {
			t.Errorf("Expected description in output, got:\n%s", output)
		}
	})

	t.Run("shows message when no roles", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"profiles": []map[string]string{},
				"active":   nil,
			})
		}))
		defer server.Close()

		env := NewTestEnv(t)
		setupSleuthConfig(t, env.HomeDir, server.URL)

		cmd := NewRoleCommand()
		cmd.SetArgs([]string{"list"})
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)

		if err := cmd.Execute(); err != nil {
			t.Fatalf("role list failed: %v", err)
		}

		output := stdout.String()
		if !strings.Contains(output, "No roles configured") {
			t.Errorf("Expected 'No roles configured' message, got:\n%s", output)
		}
	})
}

// TestRoleSet tests the role set command with a mock server.
func TestRoleSet(t *testing.T) {
	t.Run("sets active role", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/skills/sx.profiles/active" {
				t.Errorf("Unexpected path: %s", r.URL.Path)
			}
			if r.Method != http.MethodPost {
				t.Errorf("Expected POST, got %s", r.Method)
			}

			var body map[string]*string
			json.NewDecoder(r.Body).Decode(&body)
			if body["slug"] == nil || *body["slug"] != "backend" {
				t.Errorf("Expected slug 'backend', got %v", body["slug"])
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"profile": map[string]string{
					"title":       "Backend Engineer",
					"slug":        "backend",
					"description": "Backend development role",
				},
			})
		}))
		defer server.Close()

		env := NewTestEnv(t)
		setupSleuthConfig(t, env.HomeDir, server.URL)

		cmd := NewRoleCommand()
		cmd.SetArgs([]string{"set", "backend"})
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)

		if err := cmd.Execute(); err != nil {
			t.Fatalf("role set failed: %v", err)
		}

		output := stdout.String()
		if !strings.Contains(output, "Active role set to") {
			t.Errorf("Expected success message, got:\n%s", output)
		}
		if !strings.Contains(output, "Backend Engineer") {
			t.Errorf("Expected role title in output, got:\n%s", output)
		}
	})

	t.Run("returns error for unknown slug", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Skill profile not found",
			})
		}))
		defer server.Close()

		env := NewTestEnv(t)
		setupSleuthConfig(t, env.HomeDir, server.URL)

		cmd := NewRoleCommand()
		cmd.SetArgs([]string{"set", "nonexistent"})
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)

		err := cmd.Execute()
		if err == nil {
			t.Fatal("Expected error for unknown slug, got nil")
		}
		if !strings.Contains(err.Error(), "Skill profile not found") {
			t.Errorf("Expected 'Skill profile not found' in error, got: %v", err)
		}
	})

	t.Run("requires slug argument", func(t *testing.T) {
		env := NewTestEnv(t)
		setupSleuthConfig(t, env.HomeDir, "http://unused")

		cmd := NewRoleCommand()
		cmd.SetArgs([]string{"set"})
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)

		err := cmd.Execute()
		if err == nil {
			t.Fatal("Expected error for missing slug argument, got nil")
		}
	})
}

// TestRoleClear tests the role clear command with a mock server.
func TestRoleClear(t *testing.T) {
	t.Run("clears active role", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/skills/sx.profiles/active" {
				t.Errorf("Unexpected path: %s", r.URL.Path)
			}

			var body map[string]*string
			json.NewDecoder(r.Body).Decode(&body)
			if body["slug"] != nil {
				t.Errorf("Expected null slug for clear, got %v", *body["slug"])
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"profile": nil,
			})
		}))
		defer server.Close()

		env := NewTestEnv(t)
		setupSleuthConfig(t, env.HomeDir, server.URL)

		cmd := NewRoleCommand()
		cmd.SetArgs([]string{"clear"})
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)

		if err := cmd.Execute(); err != nil {
			t.Fatalf("role clear failed: %v", err)
		}

		output := stdout.String()
		if !strings.Contains(output, "Active role cleared") {
			t.Errorf("Expected 'Active role cleared', got:\n%s", output)
		}
	})
}

// TestRoleCurrent tests the role current command with a mock server.
func TestRoleCurrent(t *testing.T) {
	t.Run("shows active role", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			active := "backend"
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"profiles": []map[string]string{
					{"title": "Backend Engineer", "slug": "backend", "description": "Backend role"},
					{"title": "Frontend Engineer", "slug": "frontend", "description": "Frontend role"},
				},
				"active": active,
			})
		}))
		defer server.Close()

		env := NewTestEnv(t)
		setupSleuthConfig(t, env.HomeDir, server.URL)

		cmd := NewRoleCommand()
		cmd.SetArgs([]string{"current"})
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)

		if err := cmd.Execute(); err != nil {
			t.Fatalf("role current failed: %v", err)
		}

		output := stdout.String()
		if !strings.Contains(output, "Backend Engineer") {
			t.Errorf("Expected 'Backend Engineer' in output, got:\n%s", output)
		}
		if !strings.Contains(output, "backend") {
			t.Errorf("Expected slug 'backend' in output, got:\n%s", output)
		}
	})

	t.Run("shows message when no active role", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"profiles": []map[string]string{
					{"title": "Backend Engineer", "slug": "backend", "description": "Backend role"},
				},
				"active": nil,
			})
		}))
		defer server.Close()

		env := NewTestEnv(t)
		setupSleuthConfig(t, env.HomeDir, server.URL)

		cmd := NewRoleCommand()
		cmd.SetArgs([]string{"current"})
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)

		if err := cmd.Execute(); err != nil {
			t.Fatalf("role current failed: %v", err)
		}

		output := stdout.String()
		if !strings.Contains(output, "No active role set") {
			t.Errorf("Expected 'No active role set' message, got:\n%s", output)
		}
	})
}
