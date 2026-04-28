# Scoping: Controlling where assets are installed

Assets don't have to be everywhere. You can choose where each one is installed, so projects only get the assets they need.

## Scopes

| Scope | Target | Available when |
|-------|--------|----------------|
| `org` (global) | every caller in the vault | always — installs to `~/.claude/` |
| `repo` | one git repository | running in a clone of that repo — installs to `myapp/.claude/` |
| `path` | specific paths within a repo | running under one of those paths — installs to `myapp/services/api/.claude/` |
| `team` | every member of a named team | caller is a team member; expands to the team's repositories |
| `user` | a single user (email) | caller's git identity matches the email; the asset becomes global for that user |
| `bot` | a bot identity (CI runner, agent) | `SX_BOT=<name>` is set in the runtime — see [bots.md](bots.md) |

The first three are structural and apply to every caller. `team`,
`user`, and `bot` are identity-dependent and resolved at install time:
human callers go through `git config user.email`, bots through
`SX_BOT`. The Sleuth-only **"personal"** scope offered in the
interactive `sx add` TUI is the same as `--user <your-email>` —
included as a convenience so users don't have to type their own
address.

### Per-scope deep dives

* [teams.md](teams.md) — creating teams, member/admin lifecycle,
  team-scoped installs, identity model
* [bots.md](bots.md) — bot lifecycle, SX_BOT/SX_BOT_KEY auth,
  bot-mode resolution, trust boundaries
* [manifest-spec.md](manifest-spec.md#assetsscopes--install-targets) —
  on-disk format for every scope kind

## Setting scope with `sx add` / `sx install`

`sx add` configures scope at the time an asset is published. `sx
install <name>` with scope flags modifies an existing asset's scope in
the vault.

```bash
# Installed in all projects (default on add)
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

# Install for every member of a team (git + path vaults)
sx install my-skill --team platform

# Install for yourself only — cannot target someone else
sx install my-skill --user alice@acme.com

# Install for a bot identity (CI runner, agent)
sx install my-skill --bot python-backend

# Org-wide (clears all scopes)
sx install my-skill --org
```

> **Vault vs project:** The repo URL in `--scope-repo` / `--repo` is
> your *project's* git remote — the codebase where you want the asset
> installed — not your sx vault where assets are stored.

Already added an asset and want to change its scope? Run `sx install
<name>` with one of the scope flags, or re-run `sx add <name>` for an
interactive prompt.

## How `sx install` uses scopes

When you run `sx install` from your project directory, sx reads the
vault's `sx.toml`, resolves every team and user scope against your git
identity, writes a per-user lock file to the sx cache, and installs
only what belongs in your current location:

```
~/myapp $ sx install
  ✓ coding-standards → ~/.claude/              (org — everyone)
  ✓ api-patterns     → ~/myapp/.claude/        (this project only)
  ✓ grpc-skill       → ~/myapp/services/.claude/ (path-scoped)
  ✓ platform-helper  → ~/myapp/.claude/        (team platform, you're a member)
  ✓ alice-tools      → ~/.claude/              (user-scoped to you)
```

### Previewing what will install

`sx install --dry-run` runs the same resolution — manifest + git
identity + current scope + active clients — and prints the result
without downloading anything or writing to any client directory.
It's the `pip freeze` analogue for this vault: use it to verify
scope wiring before committing a scope change, or to see which
assets you'd get in a different directory via `--target`.

```
~/myapp $ sx install --dry-run
# sx install --dry-run
# Resolved for: claude-code,cursor
# Current scope: github.com/acme/myapp

coding-standards==2.1.0  # skill; scope=global
api-patterns==1.4.0      # skill; scope=github.com/acme/myapp
grpc-skill==0.8.0        # skill; scope=github.com/acme/myapp#services
platform-helper==1.0.0   # skill; scope=github.com/acme/infra
alice-tools==0.2.0       # skill; scope=global
```

The output is a comment-prefixed header plus one line per asset in
`name==version  # type; scope=...` form so it's self-describing when
piped to a file or diffed across runs.

This happens automatically via the Claude Code hook — each new session gets exactly the assets it needs, nothing more.

## How clients use scoped assets

sx installs assets to `.claude/` (or `.cursor/`) at the appropriate directory level based on scope. Each client then discovers and loads assets from those directories according to its own rules.

## Interactive mode

If you run `sx add` without scope flags, you'll be prompted to choose:

1. Make it available globally
2. Add/modify project-specific installations
3. Remove from installation (don't install)
