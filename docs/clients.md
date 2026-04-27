# Client Support

sx supports two kinds of AI clients:

1. **File-based clients** (Claude Code, Cursor, Codex, Copilot, Gemini, Kiro, Cline) — sx writes asset files into well-known directories the client reads on startup. This is the default `sx install` flow.
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
| GitHub Copilot | IDE ext | Full support                                                                                   |
| Kiro           | CLI+IDE | Full support. See [Kiro-specific docs](kiro.md) for hook setup.                                |

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
