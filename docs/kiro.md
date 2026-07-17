# Kiro Client

sx installs hooks to both Kiro IDE and Kiro CLI, which use different formats and locations.

## Agents

> **Upstream docs**: [Kiro IDE custom agents](https://kiro.dev/docs/custom-agents/) · [Kiro CLI v3 agent config](https://kiro.dev/docs/cli/v3/agent-config/)

sx installs agent assets to `.kiro/agents/` in **two formats simultaneously**:

| File | Format | Used by |
|------|--------|---------|
| `{name}.md` | Markdown with YAML frontmatter | Kiro IDE, Kiro CLI v3 (`--v3`) |
| `{name}.json` | JSON config | Kiro CLI v2 (default) |

The body of `AGENT.md` becomes the system prompt in both formats.

### Kiro-Specific Fields (`[agent.kiro]`)

Add a `[agent.kiro]` section to `metadata.toml` to set agent configuration fields:

```toml
[agent]
prompt-file = "AGENT.md"

[agent.kiro]
model = "claude-sonnet-4"
tools = ["read", "write", "shell"]
```

Fields route to output formats based on compatibility:

| Field | `.md` (IDE / CLI v3) | `.json` (CLI v2) | Notes |
|-------|----------------------|------------------|-------|
| `model` | ✅ | ✅ | |
| `tools` | ✅ | ✅ | |
| `mcpServers` | ✅ | ✅ | |
| `resources` | ✅ | ✅ | |
| `welcomeMessage` | ✅ | ✅ | |
| `permissions` | ✅ | ❌ | CLI v2 has no permissions model |
| `allowedTools` | ❌ | ✅ | v2-only; silently dropped from `.md` |
| `toolAliases` | ❌ | ✅ | v2-only; silently dropped from `.md` |
| `toolsSettings` | ❌ | ✅ | v2-only; silently dropped from `.md` |
| `hooks` | ❌ | ✅ | v2-only; silently dropped from `.md` |
| `includeMcpJson` | ❌ | ✅ | v2-only; silently dropped from `.md` |
| `keyboardShortcut` | ❌ | ✅ | v2-only; excluded from `.md` |
| *(unknown)* | ✅ | ✅ | Passed through for forward compat |

Valid `permissions` capability values — see [Kiro permissions reference](https://kiro.dev/docs/cli/v3/permissions/#available-capabilities): `fs_read`, `fs_write`, `filesystem` (shorthand for both), `shell`, `web_fetch`, `web_search`, `mcp`, `subagent`, `skill`, `diagnostics`, `context`, `builtin`, `all`.

> **Design decision**: sx dual-writes `.md` (IDE + CLI v3) and `.json` (CLI v2 default) so the agent works regardless of which version is active. Known v2-only fields are excluded from `.md`; `permissions` is excluded from `.json` (CLI v2 has no permissions model). Unknown fields pass through to both formats — both the Kiro IDE and CLI engines tolerate extra keys, preserving forward compatibility with future Kiro schema additions. Note: `kiro-cli agent validate` only parses JSON; it will reject `.md` files — this is expected and not a bug in sx output.

### CLI Setup for Agents

Installed CLI agents (`.json`) are not activated automatically. To use one as the default:

```bash
kiro-cli agent set-default <name>
```

## What sx Installs

| Location                       | Hooks Installed                                            |
|--------------------------------|------------------------------------------------------------|
| `.kiro/agents/default.json`    | `agentSpawn` (auto-update), `postToolUse` (usage tracking) |
| `.kiro/hooks/*.kiro.hook`      | `postToolUse` (usage tracking)                             |

## CLI Setup Required

CLI hooks are installed to `.kiro/agents/default.json`, but this agent doesn't auto-load. Run once:

```bash
kiro-cli agent set-default default
```

IDE hooks auto-load with no setup required.

## Limitations

- **No global hooks** - Kiro doesn't support `~/.kiro/hooks/` ([feature request](https://github.com/kirodotdev/Kiro/issues/5440))
- **CLI requires one-time setup** - See above

## Troubleshooting

Check sx logs:

```bash
tail -f ~/.cache/sx/sx.log | grep kiro
```

Verify installed hooks:

```bash
# CLI
cat .kiro/agents/default.json

# IDE
ls .kiro/hooks/
```

Verify installed agents (file listing):

```bash
ls ~/.kiro/agents/
# Expect: {name}.md and {name}.json for each installed agent
```

Verify agents are registered in the runtime chat harness:

```bash
# v2 engine (default) — requires --trust-all-tools for the subagent enumeration tool
kiro-cli chat --no-interactive --trust-all-tools \
  "What agents do you have available - list their name and description in a table"

# v3 engine
kiro-cli chat --no-interactive --v3 \
  "What agents do you have available - list their name and description in a table"
```

Note: `kiro-cli agent validate --path <file>.md` will error with a JSON parse failure — this is
expected. The `validate` subcommand only understands the v2 JSON format regardless of `--v3`.
