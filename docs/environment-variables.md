# Environment Variables

sx honors a small set of environment variables that relocate its own
state — config and cache. They work on every platform and are the
supported way to redirect that state in CI, Docker, test harnesses,
demo recordings, or any sandbox.

Note their scope: these variables isolate **sx's own state only**.
Client install targets (`~/.claude`, `~/.codex`, …) are derived from
the home directory, and repo-scoped clients (GitHub Copilot's
`.github/hooks/`, Kiro's `.kiro/hooks/`) write into the **current
repository's working tree** — so fully sandboxing `sx install` also
requires overriding `$HOME` and running from a scratch directory. See
the recipe below.

## State-relocating variables

### `SX_CONFIG_DIR`

Overrides the **config directory** — where sx reads and writes
`config.json` (vault URL, profile, auth). Point it at an empty
directory and sx starts unconfigured, leaving real user config
untouched.

When unset, sx uses the platform default:

| Platform | Default |
|----------|---------|
| Linux    | `$XDG_CONFIG_HOME/sx` (falls back to `~/.config/sx`) |
| macOS    | `~/Library/Application Support/sx` |
| Windows  | `%AppData%\sx` |

`SKILLS_CONFIG_DIR` is the legacy alias, checked only when
`SX_CONFIG_DIR` is unset. Prefer `SX_CONFIG_DIR` in new setups.

### `SX_CACHE_DIR`

Overrides the **cache directory** — downloaded assets, cloned git
repositories, ETags, and lock files. When unset, sx uses the platform
default:

| Platform | Default |
|----------|---------|
| Linux    | `$XDG_CACHE_HOME/sx` (falls back to `~/.cache/sx`) |
| macOS    | `~/Library/Caches/sx` |
| Windows  | `%LocalAppData%\sx` |

`SKILLS_CACHE_DIR` is the legacy alias, checked only when
`SX_CACHE_DIR` is unset.

## Sandboxing sx completely

Config and cache cover sx's own state, but `sx install` writes assets
into client directories under the home directory. A complete sandbox
overrides all three:

```bash
set -u                               # fail loudly on any unset variable
SANDBOX="$(mktemp -d)"
VAULT_URL="https://github.com/acme/vault"   # your vault URL

export HOME="$SANDBOX/home"          # client install targets (~/.claude, …)
export SX_CONFIG_DIR="$SANDBOX/config"
export SX_CACHE_DIR="$SANDBOX/cache"
export DISABLE_AUTOUPDATER=1         # no background binary replacement
mkdir -p "$HOME"
cd "$HOME"   # repo-scoped clients write into the current repo's working tree

sx init --type git --repo-url "$VAULT_URL"
sx install
sx config   # verify: reported config path is under $SX_CONFIG_DIR
```

Always verify isolation with `sx config` — it prints the effective
config path and directories. Also make sure the sandbox environment
doesn't inherit a stray `SX_PROFILE`, `SX_BOT`, or `SX_BOT_KEY`
(below), which would change which profile or identity a sandboxed run
resolves.

## Client-honored variables

These are not sx's own variables. They belong to a client, and sx reads
them so that installs land where that client actually looks.

### `KIROCREW_HOME`

Redirects KiroCrew's user data root — the directory holding its
skills, agents, and other crew configuration. When unset, that root is
`~/.kiro/crew` (see [kirodotdev/KiroCrew](https://github.com/kirodotdev/KiroCrew)).

`KIROCREW_HOME` *is* the crew root. It does not name a parent under
which a `.kiro/crew` directory gets created, so
`KIROCREW_HOME=/opt/crew-work` puts skills in `/opt/crew-work/skills`,
not `/opt/crew-work/.kiro/crew/skills`. Pointing it at separate
directories is how one machine keeps several independent KiroCrew
profiles.

A leading `~` is expanded, and a relative value is made absolute
against the working directory. A blank or whitespace-only value counts
as unset.

Every KiroCrew install follows it, because KiroCrew installs are
global-only: KiroCrew reads skills from this one crew root and never
looks for a `.kiro/crew` directory inside a repository, so repo- and
path-scoped installs are skipped rather than written into the working
tree.

## Other variables

These behave as flag or setting equivalents rather than relocating
state:

| Variable | Effect |
|----------|--------|
| `SX_PROFILE` | Selects the active config profile, overriding the one saved in config |
| `SX_BOT` | Runs as the named bot identity (see [Bots](bots.md)) |
| `SX_BOT_KEY` | Authentication key for the bot identity (see [Bots](bots.md)) |
| `SX_STRICT` | `1` makes hook installs that soft-skip count as failures (same as `--strict`) |
| `SX_SSH_KEY` | SSH key path (or inline key content) for git operations (same as `--ssh-key`; legacy alias `SKILLS_SSH_KEY`) |
| `SX_SYNC_SILENT` | `true` silences background sync output (legacy alias `SKILLS_SYNC_SILENT`) |
| `SX_CLOUD_URL` | Overrides the cloud relay URL for `sx cloud` |
| `SX_CLI_PATH` | Overrides which sx binary gets written into client hook/MCP configs |
| `SLEUTH_SERVER_URL` | Overrides the Sleuth server URL for `type=sleuth` vaults |
| `DISABLE_AUTOUPDATER` | Truthy (`1`/`true`/`yes`/`on`) disables the background self-updater — recommended in CI and containers |

## What is *not* an sx environment variable

`SX_CONFIG` is **not** read by sx on any platform. A variable of that
name previously appeared inside the generated vault `install.sh` as a
script-local file path (it has since been renamed `SX_CONFIG_FILE`
there). Setting `SX_CONFIG` in the environment has no effect — use
`SX_CONFIG_DIR` (a directory, not a file path) to relocate config.
