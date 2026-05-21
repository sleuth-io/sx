package mgmt

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

func TestCurrentGitActorRespectsIdentityOverride(t *testing.T) {
	ResetActorCache()
	t.Cleanup(func() {
		SetIdentityOverride("")
		ResetActorCache()
	})
	SetIdentityOverride("profile-user@example.com")

	actor, err := CurrentGitActor(context.Background(), "")
	if err != nil {
		t.Fatalf("CurrentGitActor: %v", err)
	}
	if actor.Email != "profile-user@example.com" {
		t.Fatalf("got %s, want profile-user@example.com", actor.Email)
	}
	if actor.Synthetic {
		t.Fatalf("override should not be synthetic")
	}
}

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

// TestCurrentGitActor_CacheKeyIncludesSXBot verifies the actor cache
// is keyed on (repoPath, SX_BOT) so a process that resolves a human
// actor first does not return that stale identity once SX_BOT is set
// later in the same run — and vice versa.
func TestCurrentGitActor_CacheKeyIncludesSXBot(t *testing.T) {
	ResetActorCache()
	t.Cleanup(ResetActorCache)
	dir := t.TempDir()

	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v (%s)", err, out)
	}
	cmd = exec.Command("git", "config", "user.email", "alice@example.com")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config failed: %v (%s)", err, out)
	}

	human, err := CurrentGitActor(context.Background(), dir)
	if err != nil {
		t.Fatalf("CurrentGitActor (human): %v", err)
	}
	if human.IsBot() {
		t.Fatalf("expected human actor, got bot")
	}

	t.Setenv("SX_BOT", "ci-bot")
	bot, err := CurrentGitActor(context.Background(), dir)
	if err != nil {
		t.Fatalf("CurrentGitActor (bot): %v", err)
	}
	if !bot.IsBot() || bot.Bot != "ci-bot" {
		t.Errorf("expected bot=ci-bot after setting SX_BOT, got %+v", bot)
	}

	t.Setenv("SX_BOT", "")
	again, err := CurrentGitActor(context.Background(), dir)
	if err != nil {
		t.Fatalf("CurrentGitActor (human again): %v", err)
	}
	if again.IsBot() || again.Email != human.Email {
		t.Errorf("expected human actor after unsetting SX_BOT, got %+v", again)
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

// TestContextWithIdentity_PrefersContextOverGlobal verifies that an
// identity carried on the context wins over the process-global override.
// This is the foundation for parallel multi-profile lock fetches where
// each goroutine resolves identity against its own profile without
// touching the shared override.
func TestContextWithIdentity_PrefersContextOverGlobal(t *testing.T) {
	ResetActorCache()
	t.Cleanup(func() {
		SetIdentityOverride("")
		ResetActorCache()
	})
	SetIdentityOverride("global@example.com")

	ctx := ContextWithIdentity(context.Background(), "ctx@example.com")
	actor, err := CurrentGitActor(ctx, "")
	if err != nil {
		t.Fatalf("CurrentGitActor: %v", err)
	}
	if actor.Email != "ctx@example.com" {
		t.Errorf("expected ctx@example.com to win, got %s", actor.Email)
	}

	// Same call without the context wrapping should fall back to the
	// global override.
	actor, err = CurrentGitActor(context.Background(), "")
	if err != nil {
		t.Fatalf("CurrentGitActor (no ctx identity): %v", err)
	}
	if actor.Email != "global@example.com" {
		t.Errorf("expected global@example.com fallback, got %s", actor.Email)
	}
}

// TestContextWithIdentity_EmptyIsExplicitOptOut verifies that an empty
// email is recorded as an explicit "no identity for this call" — not a
// no-op pass-through. The multi-profile fan-out relies on this so a
// profile with empty Identity falls back to git config rather than
// inheriting the seeded global override (which carries the first
// profile's email).
func TestContextWithIdentity_EmptyIsExplicitOptOut(t *testing.T) {
	ResetActorCache()
	t.Cleanup(func() {
		SetIdentityOverride("")
		ResetActorCache()
	})
	SetIdentityOverride("global@example.com")

	dir := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v (%s)", err, out)
	}
	cmd = exec.Command("git", "config", "user.email", "git-config@example.com")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git config: %v (%s)", err, out)
	}

	// ContextWithIdentity("") must skip both the per-call and global
	// overrides and fall through to git config.
	actor, err := CurrentGitActor(ContextWithIdentity(context.Background(), ""), dir)
	if err != nil {
		t.Fatalf("CurrentGitActor: %v", err)
	}
	if actor.Email != "git-config@example.com" {
		t.Errorf("explicit-empty ctx must skip global override; got %s, want git-config@example.com", actor.Email)
	}
}

// TestCurrentGitActor_MixedActiveSetFallsBackToGitConfig is the
// regression for the fan-out hazard: when the active set mixes a
// profile with an explicit Identity and one without, the
// no-Identity profile must resolve against git config — not against
// the seeded global override (which carries the first profile's
// email). Simulates loadActiveLockFiles by setting the global
// override and then resolving from a goroutine carrying
// ContextWithIdentity("").
func TestCurrentGitActor_MixedActiveSetFallsBackToGitConfig(t *testing.T) {
	ResetActorCache()
	t.Cleanup(func() {
		SetIdentityOverride("")
		ResetActorCache()
	})
	SetIdentityOverride("alice@work.com")

	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "bob@personal.com"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}

	// Profile A: explicit Identity — should resolve to alice@work.com
	actorA, err := CurrentGitActor(ContextWithIdentity(context.Background(), "alice@work.com"), dir)
	if err != nil {
		t.Fatalf("profile A: %v", err)
	}
	if actorA.Email != "alice@work.com" {
		t.Errorf("profile A: got %s, want alice@work.com", actorA.Email)
	}

	// Profile B: empty Identity — must NOT inherit alice@work.com from
	// the seeded global override; must fall through to git config.
	actorB, err := CurrentGitActor(ContextWithIdentity(context.Background(), ""), dir)
	if err != nil {
		t.Fatalf("profile B: %v", err)
	}
	if actorB.Email != "bob@personal.com" {
		t.Errorf("profile B: expected git-config fallback bob@personal.com, got %s", actorB.Email)
	}
}

// TestCurrentGitActor_ConcurrentContextIdentitiesDontBleed exercises the
// multi-profile fan-out invariant: N goroutines each carrying a
// different ContextWithIdentity must each see their own identity, with
// no cross-bleed even if they execute simultaneously.
func TestCurrentGitActor_ConcurrentContextIdentitiesDontBleed(t *testing.T) {
	ResetActorCache()
	t.Cleanup(func() {
		SetIdentityOverride("")
		ResetActorCache()
	})
	// Set a global override that none of the goroutines should ever
	// resolve to (they all carry context identities).
	SetIdentityOverride("global@example.com")

	const goroutines = 16
	emails := make([]string, goroutines)
	for i := range emails {
		emails[i] = fmt.Sprintf("profile-%d@example.com", i)
	}

	got := make([]string, goroutines)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, email := range emails {
		wg.Add(1)
		go func(i int, email string) {
			defer wg.Done()
			<-start
			actor, err := CurrentGitActor(ContextWithIdentity(context.Background(), email), "")
			if err != nil {
				t.Errorf("goroutine %d: %v", i, err)
				return
			}
			got[i] = actor.Email
		}(i, email)
	}
	close(start)
	wg.Wait()

	for i, want := range emails {
		if got[i] != want {
			t.Errorf("goroutine %d resolved %q, want %q", i, got[i], want)
		}
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
