# Bots

Bots are non-human service identities that consume assets — typically CI
jobs, agents, or other automation. Bots gain repository context by being
members of teams (the same way human team members do); assets can also be
installed directly to a bot via the `--bot` install scope.

For the manifest schema, see [manifest-spec.md](manifest-spec.md). For
the audit log generated alongside bot mutations, see
[audit.md](audit.md).

## Lifecycle

### Creating a bot

```bash
sx bot create python-backend \
  --description "Python backend CI bot" \
  --team platform \
  --team data
```

The bot must reference existing teams. A bot with no teams is still
useful — it can receive direct `--bot` installs and any org-wide
asset.

### Inspecting bots

```bash
sx bot list
sx bot show python-backend
```

`show` prints the bot's description, team list, and (for Sleuth vaults)
its API keys.

### Updating a bot

```bash
sx bot update python-backend --description "New description"
sx bot team add python-backend platform
sx bot team remove python-backend data
```

### Deleting a bot

```bash
sx bot delete python-backend --yes
```

Cascades: every `kind = "bot"` scope referencing the deleted bot is
removed from its asset, and an `install.cleared` audit event is emitted
per asset with `reason = "bot_deleted"`.

## Targeted installs

```bash
sx install code-reviewer --bot python-backend
```

Adds a `kind = "bot"` scope row to the asset in the vault's `sx.toml`
(file-based vaults) or calls `installSkillToBot` (Sleuth vaults). Bot
installs are admin-equivalent: any caller with vault write access can
manage bot scopes.

## Acting as a bot

Set `SX_BOT=<name>` in the bot's runtime environment so subsequent `sx
install` and `sx serve` calls resolve installs against the bot identity:

```bash
SX_BOT=python-backend sx install
```

The resolved lock file includes:

* assets installed directly to the bot (`kind = "bot"`)
* assets installed to any team the bot belongs to (`kind = "team"`)
* assets installed to repositories owned by those teams (`kind = "repo"`,
  `kind = "path"`)
* **org-wide assets** (`kind = "org"` or empty scopes)

It excludes:

* assets installed to a different bot
* assets installed to a different team
* user-scoped installs (`kind = "user"` — bots are not human users)

> **Difference from skills.new**: skills.new currently excludes org-wide
> installs from a bot's view. sx includes them; bots see the same
> org-wide surface a human team member would. The skills.new behavior
> is being aligned in a separate change.

## Trust boundaries

**File-based vaults (path/git)** treat bots as **identity-only**. Anyone
with vault read access can claim any bot identity by setting
`SX_BOT=<name>` — this matches the existing "git access ⇒ asset access"
security model. There are no API keys; `sx bot key …` returns an error
on these vault types.

**Sleuth vaults** issue real OAuth tokens via the existing skills.new
`createBotApiKey` mutation. Pass the raw token in `SX_BOT_KEY` alongside
`SX_BOT=<name>` to authenticate as the bot.

```bash
sx bot key create python-backend --label ci-default
# prints raw token once — store in CI secrets
```

```bash
# In CI:
export SX_BOT=python-backend
export SX_BOT_KEY=<raw-token>
sx install
```

## Bot identity is read-only

A bot identity (resolved via `SX_BOT`) **cannot mutate vault state**.
`sx team create`, `sx bot create`, `sx install --bot …`, and similar
calls all reject bot actors with `bot identities cannot mutate vault
state — switch to a real git user.email`.

This keeps the audit log clean (every mutation is attributed to a real
human) and makes bot CI runs read-only by construction.
