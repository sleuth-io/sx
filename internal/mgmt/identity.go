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

	// Bot is non-empty when the caller is acting as a bot (typically via
	// SX_BOT=<name>). Email is then "bot:<name>" so audit attribution
	// stays unique and never collides with a human email. See IsBot.
	Bot string
}

// IsBot returns true when the actor is a bot identity (e.g. resolved
// from SX_BOT). Used by resolution code to switch to the bot scope rule
// instead of the human one.
func (a Actor) IsBot() bool {
	return a.Bot != ""
}

// SXBotEnv is the environment variable that, when set, makes
// CurrentGitActor return a bot actor instead of the git-email actor.
// File-based vaults treat the named bot as identity-only — anyone with
// vault read access can claim any bot — and Sleuth vaults expect the
// caller to also be authenticated via a bot API key.
const SXBotEnv = "SX_BOT"

// SXBotKeyEnv is the environment variable that holds the raw bot API
// key for Sleuth vaults. When set, the Sleuth vault constructor uses
// it as the bearer token, overriding the user OAuth token saved by
// `sx cloud connect`. File-based vaults ignore this — bots are
// identity-only there.
const SXBotKeyEnv = "SX_BOT_KEY" //nolint:gosec // env var name, not a credential

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
// authoritative actor behind a persisted change. Bot actors are
// rejected: bot identities are read-only by design (they fetch
// installed assets but never mutate vault state).
func (a Actor) RequireRealIdentity() error {
	if a.IsBot() {
		return fmt.Errorf("%w: bot identities cannot mutate vault state — switch to a real git user.email", ErrIdentityNotSet)
	}
	if a.Email == "" || a.Synthetic {
		return ErrIdentityNotSet
	}
	return nil
}

// actorCacheKey distinguishes a cached actor by repoPath and the
// SX_BOT env var. Without the bot field a process that resolved a
// human actor first would return the stale human identity even after
// SX_BOT was set later in the same process — and vice versa for tests
// that toggle the env between human and bot personas.
type actorCacheKey struct {
	repoPath string
	bot      string
}

// actorCache caches the result of CurrentGitActor per (repoPath, SX_BOT)
// for the duration of the CLI execution so repeated calls don't shell
// out.
var (
	actorCacheMu sync.Mutex
	actorCache   = make(map[actorCacheKey]Actor)
)

// CurrentGitActor resolves the caller's identity. SX_BOT short-circuits
// the resolution: if set, the actor is a bot identity, with Email
// "bot:<name>" so audit log entries are attributed unambiguously and
// never collide with a human email. Otherwise resolution proceeds via
// `git config user.email` (scoped to the given repoPath if non-empty,
// falling back to global git config), with a $USER@host fallback for
// unconfigured workstations. Returns ErrIdentityNotSet only when every
// source fails.
func CurrentGitActor(ctx context.Context, repoPath string) (Actor, error) {
	botName := strings.TrimSpace(os.Getenv(SXBotEnv))
	cacheKey := actorCacheKey{repoPath: repoPath, bot: botName}

	actorCacheMu.Lock()
	if cached, ok := actorCache[cacheKey]; ok {
		actorCacheMu.Unlock()
		return cached, nil
	}
	actorCacheMu.Unlock()

	if botName != "" {
		actor := Actor{
			Email: "bot:" + botName,
			Name:  botName,
			Bot:   botName,
		}
		actorCacheMu.Lock()
		actorCache[cacheKey] = actor
		actorCacheMu.Unlock()
		return actor, nil
	}

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
	actorCache[cacheKey] = actor
	actorCacheMu.Unlock()
	return actor, nil
}

// ResetActorCache clears the actor cache. Exposed for tests.
func ResetActorCache() {
	actorCacheMu.Lock()
	defer actorCacheMu.Unlock()
	actorCache = make(map[actorCacheKey]Actor)
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
