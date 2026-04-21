package mgmt

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestCurrentGitActorWithConfiguredRepo(t *testing.T) {
	ResetActorCache()
	dir := t.TempDir()

	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v (%s)", args, err, out)
		}
	}

	run("init")
	run("config", "user.email", "Test@Example.COM")
	run("config", "user.name", "Test User")

	actor, err := CurrentGitActor(context.Background(), dir)
	if err != nil {
		t.Fatalf("CurrentGitActor failed: %v", err)
	}
	if actor.Email != "test@example.com" {
		t.Errorf("expected normalized email, got %s", actor.Email)
	}
	if actor.Name != "Test User" {
		t.Errorf("expected name 'Test User', got %s", actor.Name)
	}

	str := actor.String()
	if str != "Test User <test@example.com>" {
		t.Errorf("expected formatted string, got %s", str)
	}
}

func TestCurrentGitActorCached(t *testing.T) {
	ResetActorCache()
	dir := t.TempDir()

	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v (%s)", err, out)
	}
	cmd = exec.Command("git", "config", "user.email", "first@example.com")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config failed: %v (%s)", err, out)
	}

	first, err := CurrentGitActor(context.Background(), dir)
	if err != nil {
		t.Fatalf("CurrentGitActor failed: %v", err)
	}

	// Change the config — cached value should still win
	cmd = exec.Command("git", "config", "user.email", "second@example.com")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config failed: %v (%s)", err, out)
	}

	cached, err := CurrentGitActor(context.Background(), dir)
	if err != nil {
		t.Fatalf("CurrentGitActor failed: %v", err)
	}
	if cached.Email != first.Email {
		t.Errorf("expected cached email %s, got %s", first.Email, cached.Email)
	}

	ResetActorCache()
	fresh, err := CurrentGitActor(context.Background(), dir)
	if err != nil {
		t.Fatalf("CurrentGitActor failed: %v", err)
	}
	if fresh.Email != "second@example.com" {
		t.Errorf("expected fresh email second@example.com, got %s", fresh.Email)
	}
}

func TestActor_RequireRealIdentity(t *testing.T) {
	cases := []struct {
		name    string
		actor   Actor
		wantErr bool
	}{
		{"real identity", Actor{Email: "alice@acme.com"}, false},
		{"synthetic fallback", Actor{Email: "local:alice@host", Synthetic: true}, true},
		{"empty email", Actor{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.actor.RequireRealIdentity()
			if (err != nil) != tc.wantErr {
				t.Errorf("got err=%v, wantErr=%v", err, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, ErrIdentityNotSet) {
				t.Errorf("expected ErrIdentityNotSet, got %v", err)
			}
		})
	}
}

// TestFallbackEmail_UsesLocalPrefix guarantees that fallback identities
// cannot collide with real emails in team admin lists — the previous
// format (USER@host) could be spoofed via $USER.
func TestFallbackEmail_UsesLocalPrefix(t *testing.T) {
	t.Setenv("USER", "evilalice@acme.com")

	got := fallbackEmail()
	if got == "" {
		t.Skip("no hostname/user available on this runner")
	}
	if !strings.HasPrefix(got, "local:") {
		t.Errorf("fallback email must carry the local: prefix to prevent admin-list collisions, got %q", got)
	}
}

// TestCurrentGitActor_SyntheticFlag verifies that an unconfigured repo
// produces an actor flagged as synthetic so mgmt mutations can reject
// it. Overrides HOME and GIT_CONFIG_* so `git config` cannot walk up to
// a developer's global config and find a real identity.
func TestCurrentGitActor_SyntheticFlag(t *testing.T) {
	ResetActorCache()
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")

	actor, err := CurrentGitActor(context.Background(), dir)
	if err != nil {
		t.Skipf("no fallback identity available: %v", err)
	}
	if !actor.Synthetic {
		t.Errorf("expected Synthetic=true when git is unconfigured, got actor=%+v", actor)
	}
	if err := actor.RequireRealIdentity(); err == nil {
		t.Error("expected RequireRealIdentity to reject synthetic actor")
	}
}
