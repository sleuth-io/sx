# Client Support

sx works by writing asset files into well-known directories that AI clients read from (e.g. `.claude/`, `.kiro/`, `.cursor/`). This means sx support is inherently tied to the **file-based layer** of each client.

## How sx installs assets

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

## What "Experimental" means

Clients marked as **Experimental** in the README have working implementations, but may have gaps where the client's file format is undocumented, subject to change, or where certain asset types don't map cleanly to the client's native concepts.

If an asset type is not listed as supported for a client, it's either because:
- The client has no file-based equivalent
- The format is unknown or unstable
- It hasn't been implemented yet

## Contributing

If you find that a client reads files from a location sx doesn't know about, or that a supported asset type isn't working as expected, please [open an issue](https://github.com/sleuth-io/sx/issues).
