# Kiro Client

sx installs hooks to both Kiro IDE and Kiro CLI, which use different formats and locations.

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
