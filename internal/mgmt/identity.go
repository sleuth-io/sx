package mgmt

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"sync"
)

// ErrIdentityNotSet is returned when no git email or fallback identity can
// be determined.
var ErrIdentityNotSet = errors.New("identity not set: run 'git config --global user.email \"you@example.com\"'")

// Actor is a resolved caller identity used for audit, usage, and install
// targeting.
type Actor struct {
	Email string
	Name  string

	// Synthetic is true when Email was derived from $USER@host instead of
	// a real git config value. Synthetic actors cannot pass mgmt
	// mutations because their identity can be spoofed by flipping $USER;
	// see RequireRealIdentity.
	Synthetic bool
}

// String returns "name <email>" if both are set, just the email otherwise.
func (a Actor) String() string {
	if a.Name != "" && a.Email != "" {
		return fmt.Sprintf("%s <%s>", a.Name, a.Email)
	}
	return a.Email
}

// RequireRealIdentity returns ErrIdentityNotSet if the actor is
// synthetic. Call this at the top of any mgmt mutation helper that
// writes to shared vault state (teams, installations, scopes) — a
// synthetic identity is fine for reads but cannot be trusted as the
// authoritative actor behind a persisted change.
func (a Actor) RequireRealIdentity() error {
	if a.Email == "" || a.Synthetic {
		return ErrIdentityNotSet
	}
	return nil
}

// actorCache caches the result of CurrentGitActor per repoPath for the
// duration of the CLI execution so repeated calls don't shell out.
var (
	actorCacheMu sync.Mutex
	actorCache   = make(map[string]Actor)
)

// CurrentGitActor resolves the caller's identity via `git config user.email`
// (scoped to the given repoPath if non-empty, falling back to global git
// config). If git is unconfigured or unavailable, it tries to synthesize an
// identity from $USER and the hostname. Returns ErrIdentityNotSet only when
// every source fails.
func CurrentGitActor(ctx context.Context, repoPath string) (Actor, error) {
	actorCacheMu.Lock()
	if cached, ok := actorCache[repoPath]; ok {
		actorCacheMu.Unlock()
		return cached, nil
	}
	actorCacheMu.Unlock()

	email := readGitConfig(ctx, repoPath, "user.email")
	name := readGitConfig(ctx, repoPath, "user.name")

	synthetic := false
	if email == "" {
		email = fallbackEmail()
		synthetic = email != ""
	}
	if email == "" {
		return Actor{}, ErrIdentityNotSet
	}

	actor := Actor{
		Email:     NormalizeEmail(email),
		Name:      strings.TrimSpace(name),
		Synthetic: synthetic,
	}

	actorCacheMu.Lock()
	actorCache[repoPath] = actor
	actorCacheMu.Unlock()
	return actor, nil
}

// ResetActorCache clears the actor cache. Exposed for tests.
func ResetActorCache() {
	actorCacheMu.Lock()
	defer actorCacheMu.Unlock()
	actorCache = make(map[string]Actor)
}

func readGitConfig(ctx context.Context, repoPath, key string) string {
	args := []string{"config", "--get", key}
	cmd := exec.CommandContext(ctx, "git", args...)
	if repoPath != "" {
		cmd.Dir = repoPath
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// fallbackEmail synthesizes an identifier from `$USER` and the machine
// hostname, prefixed with `local:` so it can never collide with a real
// email in a team's admin or member list. Only used for read-side actor
// resolution on unconfigured workstations; RequireRealIdentity rejects
// these values for any mutation.
func fallbackEmail() string {
	username := os.Getenv("USER")
	if username == "" {
		if u, err := user.Current(); err == nil {
			username = u.Username
		}
	}
	if username == "" {
		return ""
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "local:" + username
	}
	return "local:" + username + "@" + host
}
