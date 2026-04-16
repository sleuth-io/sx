package mgmt

import (
	"context"
	"os/exec"
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
