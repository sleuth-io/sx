package mgmt

import (
	"errors"
	"slices"
	"strings"
	"time"
)

// Audit and creation timestamps for bots are not stored on the manifest
// row itself; the audit event stream is the authoritative source for
// "when was X created and by whom". The mgmt.Bot view is the interface
// type returned by Vault methods — it carries no timestamps either to
// keep file-based and Sleuth implementations symmetric.

// ErrBotNotFound is returned when a bot lookup fails.
var ErrBotNotFound = errors.New("bot not found")

// ErrBotExists is returned when attempting to create a bot that already
// exists in the vault.
var ErrBotExists = errors.New("bot already exists")

// ErrEmptyBotName is returned when a bot name is blank or whitespace-only.
// Names are the primary key of bots in the manifest, so an empty one would
// collide with any other empty-named bot and hide from lookups.
var ErrEmptyBotName = errors.New("bot name cannot be empty")

// Bot is a non-human service identity that consumes assets. Bots gain
// repository context by being members of one or more teams; assets can
// also be installed directly to a bot via the InstallKindBot scope.
//
// File-based vaults (path/git) treat bots as identity-only — the trust
// boundary is "vault read access ⇒ asset access", so anyone with access
// to the vault can claim any bot identity by setting SX_BOT=<name>.
// Sleuth vaults issue real OAuth API keys via createBotApiKey; that
// capability is exposed only on the BotApiKeyManager interface.
type Bot struct {
	Name        string
	Description string
	Teams       []string
}

// IsOnTeam returns true if the bot is a member of the named team.
func (b *Bot) IsOnTeam(name string) bool {
	return slices.Contains(b.Teams, strings.TrimSpace(name))
}

// BotApiKey is metadata about a bot API key, returned by Sleuth vaults.
// The raw token is only available at creation time. File-based vaults
// don't issue keys, so callers should expect ErrNotImplemented from
// the BotApiKeyManager methods on those.
type BotApiKey struct {
	ID          string
	Label       string
	MaskedToken string
	CreatedAt   time.Time
}
