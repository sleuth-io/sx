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
}

// String returns "name <email>" if both are set, just the email otherwise.
func (a Actor) String() string {
	if a.Name != "" && a.Email != "" {
		return fmt.Sprintf("%s <%s>", a.Name, a.Email)
	}
	return a.Email
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

	if email == "" {
		email = fallbackEmail()
	}
	if email == "" {
		return Actor{}, ErrIdentityNotSet
	}

	actor := Actor{Email: NormalizeEmail(email), Name: strings.TrimSpace(name)}

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

// fallbackEmail synthesizes an email-like identifier from $USER@<hostname>.
// Used when git is unconfigured — lets path vaults on developer workstations
// still produce non-anonymous audit entries.
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
		return username
	}
	return username + "@" + host
}
