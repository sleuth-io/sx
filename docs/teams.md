# Teams and targeted installs

Git and path vaults support first-class team management plus targeted
installs that route assets to specific repositories, paths, teams, or
individual users. This document covers the CLI surface and the data
model. The schema is defined in [manifest-spec.md](manifest-spec.md);
the lock file produced at install time is described in
[lock-spec.md](lock-spec.md). For the audit log and usage analytics
generated alongside these mutations, see [audit.md](audit.md) and
[stats.md](stats.md).

## Teams

### Creating a team

```bash
sx team create platform \
  --description "Platform engineering" \
  --member alice@acme.com \
  --member bob@acme.com \
  --admin alice@acme.com \
  --repo github.com/acme/infra \
  --repo github.com/acme/tools
```

The caller is auto-added as a member and admin, so there is always at
least one admin on creation. The team name must be non-empty after trim.

### Inspecting teams

```bash
sx team list                 # all teams with summary counts
sx team show platform        # full detail for one team
```

### Member and admin mutations

```bash
sx team member add platform carol@acme.com          # add a member
sx team member add platform carol@acme.com --admin  # add a member and promote in one step
sx team member remove platform bob@acme.com         # remove a member (also strips admin)
sx team admin set platform bob@acme.com             # promote a member to admin
sx team admin unset platform bob@acme.com           # demote back to member
```

Every destructive mutation re-checks admin membership inside the
transaction, after acquiring the vault flock, so a concurrent
demotion can't race past the pre-check.

A mutation that would leave the team with zero admins is rejected; you
must promote another admin before removing or demoting the last one.

Repeating an idempotent mutation (adding an existing member, granting
admin to someone who already has it, etc.) is a silent no-op that does
not rewrite the manifest or emit an audit event.

### Repositories

Team repositories drive scope resolution: if an asset is installed with
`--team platform`, every member gets it flattened to the team's
repositories at install time.

```bash
sx team repo add platform github.com/acme/billing
sx team repo remove platform github.com/acme/tools
```

### Deleting a team

```bash
sx team delete platform --yes
```

Deleting a team cascades: every `kind = "team"` scope referencing it is
removed from its asset, and an `install.cleared` audit event is emitted
per asset with `reason = "team_deleted"` so auditors can reconstruct
why an asset stopped installing.

## Targeted installs

`sx install <asset>` with a scope flag rewrites an asset's install
scopes in the vault's `sx.toml`:

```bash
sx install code-reviewer --org                          # everyone
sx install code-reviewer --repo github.com/acme/infra   # one repo
sx install code-reviewer --path github.com/acme/infra#docs/  # path in a repo
sx install code-reviewer --team platform                # team members
sx install code-reviewer --user alice@acme.com          # a single user
sx install code-reviewer --bot python-backend           # a bot identity
```

For the `--bot` scope, bot lifecycle, and SX_BOT identity, see
[bots.md](bots.md).

### Previewing a resolved install

`sx install --dry-run` prints the assets that would be installed for
the current user without downloading anything or touching client
directories — the equivalent of `pip freeze` against the vault's
manifest. See `sx install --help` for details.

### User-scope self-only

`--user <email>` is allowed only when `<email>` matches the caller's
git identity. This stops someone with write access to the vault from
silently flipping an asset to "global" in a teammate's resolved lock
file. The check is enforced inside the vault mutation transaction,
not just in the CLI.

### Team scope admin requirement

`--team <name>` requires the caller to be an admin of the named team,
re-checked inside the transaction against the freshly-loaded team list.

## Identity model

Team membership, admin checks, and user-scope install gating all use
the email from `git config user.email` (cached per vault root for the
CLI's lifetime).

If git is unconfigured, sx synthesizes a read-only identity of the form
`local:$USER@$HOST`. The `local:` prefix guarantees it cannot collide
with a real email in a team admin list — any mutation using this
identity is rejected with a clear "set git config user.email" message.
Read-only commands (`sx team list`, `sx install --dry-run`,
`sx stats`, `sx audit`) still work.

## Where state lives

| State | Location | Who writes |
|-------|----------|-----------|
| Manifest (assets, scopes, teams) | `<vault>/sx.toml` | Vault admins via `sx team *`, `sx add`, `sx install --scope` |
| Audit events | `<vault>/.sx/audit/YYYY-MM.jsonl` | Every mutation (see [audit.md](audit.md)) |
| Usage events | `<vault>/.sx/usage/YYYY-MM.jsonl` | Every `sx install` (see [stats.md](stats.md)) |
| Per-user resolved lock | `~/<cache>/sx/lockfiles/<vault-id>.lock` | `sx install` |
| Rotated lock history | `~/<cache>/sx/lockfiles/<vault-id>-<ts>.lock` | Every install whose resolved content differs from the previous |

## Sleuth vault

The [skills.new](https://skills.new) hosted vault supports the same
commands but delegates team state and audit storage to the server.
Team management and the audit log are accessible through the web UI;
the CLI surface here is identical so the same scripts work against
either vault type.
