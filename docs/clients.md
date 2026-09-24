# Client Support

sx supports two kinds of AI clients:

1. **File-based clients** (Claude Code, Cursor, Codex, Copilot, Gemini, Kiro, KiroCrew, Cline, OpenCode) — sx writes asset files into well-known directories the client reads on startup. This is the default `sx install` flow.
2. **Web clients** (claude.ai, chatgpt.com) — sx exposes the vault as an MCP endpoint through the skills.new cloud relay. See [cloud-relay.md](cloud-relay.md).

The two paths are independent. A vault can serve both; the same assets are reachable from a CLI tool reading `.claude/skills/` and from claude.ai talking to the relay.

## How sx installs assets (file-based clients)

sx writes files to disk. The client then reads those files when it starts or when it scans its config directories. sx does not interact with any client's UI, plugin system, or internal database.

## IDE vs. CLI variants

Many AI tools ship in two forms: a **desktop IDE** and a **CLI**. These often have different config formats and different levels of file-based support. sx targets the file-based layer in all cases.

| Client         | Form    | Notes                                                                                          |
|----------------|---------|------------------------------------------------------------------------------------------------|
| Claude Code    | CLI     | Full support                                                                                   |
| Cline          | IDE ext | Full support                                                                                   |
| Codex          | CLI     | Full support                                                                                   |
| Cursor         | IDE     | Full support                                                                                   |
| Gemini         | CLI/IDE | Full support for CLI/VS Code; rules and MCP only (JetBrains); MCP-remote only (Android Studio) |
| GitHub Copilot | IDE+CLI | Full support, including remote (http/sse) MCP. MCP servers are written to `.vscode/mcp.json` for VS Code and mirrored into the Copilot CLI config: `~/.copilot/mcp-config.json` (global scope) or `.github/mcp.json` (repo/path scope). Packaged servers are not mirrored into `.github/mcp.json` — their entries carry machine-absolute paths and that file is typically committed. Root `.mcp.json` is left to the Claude Code client. |
| Kiro           | CLI+IDE | Full support. See [Kiro-specific docs](kiro.md) for hook setup.                                |
| KiroCrew       | Agent   | Skills only. Dispatched KiroCrew agents load skills from KiroCrew's own crew root — `~/.kiro/crew/skills/` by default, or `$KIROCREW_HOME/skills/` when [`KIROCREW_HOME`](environment-variables.md#kirocrew_home) is set — which is distinct from kiro-cli's `~/.kiro/skills/`. Source: [kirodotdev/KiroCrew](https://github.com/kirodotdev/KiroCrew). Global-scope installs land under the crew root; repo/path scopes write `.kiro/crew/` into the repository tree. No MCP server or bootstrap hooks. |
| OpenCode       | CLI     | Skills, commands, agents, rules, MCP servers. Config at `~/.config/opencode/` (or `.opencode/` per-repo). Rules are written to `rules/<name>.md` and registered via the `instructions` array in `opencode.json`. |

## How hooks reference the sx CLI

Hooks and MCP entries are configuration that the client executes later, so they
have to name a binary. Which form sx writes depends on where the config lives,
not on which client it is:

- **User-scoped config** (`~/.claude/settings.json`, `~/.cursor/hooks.json`,
  `~/.gemini/settings.json`, `~/.codex/config.toml`, `~/.openclaw/hooks/`,
  Cline's hooks directory, Cursor and Kiro MCP entries) gets an **absolute path**
  to the CLI. This is what makes an app-only install work: the desktop app ships
  its own CLI, and a GUI client launched from Finder or a Dock inherits launchd's
  minimal PATH, where `~/.local/bin` is usually invisible.
- **Repo-scoped config** that gets committed keeps a bare **`sx`**, resolved from
  PATH at run time. A machine-absolute path in a shared file breaks every other
  developer and CI. This covers Copilot's `.github/hooks/sx.json` and Kiro's
  `{repoRoot}/.kiro/agents/` and `.kiro/hooks/`, and is the same reasoning that
  keeps packaged MCP servers out of `.github/mcp.json` (above).

The practical consequence: **an app-only install is complete for user-scoped
hooks, but the repo-scoped files above still need `sx` on PATH.** Install the CLI
separately (Homebrew or `install.sh`) if you rely on the Copilot or Kiro
repo-committed hooks.

`SX_CLI_PATH` overrides the resolved path everywhere. Detection accepts both
forms, so upgrading between them replaces a hook rather than duplicating it.

For package-manager installs, the recorded path is the stable spelling of the
binary: Homebrew's `/opt/homebrew/bin/sx` symlink rather than the versioned
`Cellar/sx/<version>/` target it points at, which upgrades delete. An entry
pinned to a versioned path by an older sx — including one whose target an
upgrade already removed — is rewritten to the stable spelling the next time
`sx install` runs **from a terminal**. The repair is not automatic before
then: until Homebrew cleans up the old keg, a hook naming it still invokes
the older CLI, which re-pins itself; once the keg is gone the hook can't run
at all. Either way, one terminal `sx install` with the current version fixes
it for good.

## Web clients (cloud relay)

claude.ai and chatgpt.com can't read your filesystem, so sx exposes the vault as a custom MCP connector through a relay hosted at skills.new. The relay forwards JSON-RPC over a WebSocket your local `sx cloud serve` process opens — vault content stays on your machine.

| Client      | Form | Notes                                                                                          |
|-------------|------|------------------------------------------------------------------------------------------------|
| claude.ai   | Web  | Via skills.new relay. Exposes `list_my_assets` / `load_my_asset` / `load_my_asset_file` tools. |
| chatgpt.com | Web  | Via skills.new relay. Exposes `list_my_assets` / `load_my_asset` / `load_my_asset_file` tools. |

See [cloud-relay.md](cloud-relay.md) for setup, security model, and troubleshooting.

## What "Experimental" means

Clients marked as **Experimental** in the README have working implementations, but may have gaps where the client's file format is undocumented, subject to change, or where certain asset types don't map cleanly to the client's native concepts.

If an asset type is not listed as supported for a client, it's either because:
- The client has no file-based equivalent
- The format is unknown or unstable
- It hasn't been implemented yet

## Contributing

If you find that a client reads files from a location sx doesn't know about, or that a supported asset type isn't working as expected, please [open an issue](https://github.com/sleuth-io/sx/issues).
