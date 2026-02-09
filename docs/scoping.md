# Scoping: Controlling where assets are installed

Assets don't have to be everywhere. You can choose where each one is installed, so projects only get the assets they need.

## Scopes

| Scope | Installs to | Available when |
|-------|------------|-------------|
| Global | `~/.claude/` | Every project |
| Project | `myapp/.claude/` | Only in `myapp/` |
| Path | `myapp/services/api/.claude/` | Only in that directory |

## Setting scope with `sx add`

```bash
# Installed in all projects (default)
sx add my-skill --scope-global

# Installed only in a specific project
sx add my-skill --scope-repo git@github.com:myorg/myapp.git

# Installed only in specific directories within a project (great for monorepos)
sx add my-skill --scope-repo "git@github.com:myorg/myapp.git#services/api"

# Multiple paths in the same project
sx add my-skill --scope-repo "git@github.com:myorg/myapp.git#services/api,services/web"

# Multiple projects
sx add my-skill \
  --scope-repo git@github.com:myorg/app-a.git \
  --scope-repo git@github.com:myorg/app-b.git
```

> **Vault vs project:** The repo URL in `--scope-repo` is your *project's* git remote — the codebase where you want the asset installed — not your sx vault where assets are stored.

Already added an asset and want to change its scope? Run `sx add <name>` again to reconfigure it interactively.

## How `sx install` uses scopes

When you run `sx install` from your project directory, sx matches your current location and installs only what belongs there:

```
~/myapp $ sx install
  ✓ coding-standards → ~/.claude/              (global)
  ✓ api-patterns     → ~/myapp/.claude/        (this project only)
  ✓ grpc-skill       → ~/myapp/services/.claude/ (path-scoped)
```

This happens automatically via the Claude Code hook — each new session gets exactly the assets it needs, nothing more.

## How clients use scoped assets

sx installs assets to `.claude/` (or `.cursor/`) at the appropriate directory level based on scope. Each client then discovers and loads assets from those directories according to its own rules.

## Interactive mode

If you run `sx add` without scope flags, you'll be prompted to choose:

1. Make it available globally
2. Add/modify project-specific installations
3. Remove from installation (don't install)
