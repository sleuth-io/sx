# Teams, targeted installs, audit, and usage

Git and path vaults support first-class team management, per-team and
per-user installs, an audit log of every mutation, and usage analytics.
This document covers the CLI surface and the data model. The schema is
defined in [manifest-spec.md](manifest-spec.md); the lock file produced
at install time is described in [lock-spec.md](lock-spec.md).

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
```

### User-scope self-only

`--user <email>` is allowed only when `<email>` matches the caller's
git identity. This stops someone with write access to the vault from
silently flipping an asset to "global" in a teammate's resolved lock
file. The check is enforced inside the vault mutation transaction,
not just in the CLI.

### Team scope admin requirement

`--team <name>` requires the caller to be an admin of the named team,
re-checked inside the transaction against the freshly-loaded team list.

## Audit log

Every mutation — team create/update/delete, admin grant/revoke, member
add/remove, repository add/remove, install set/cleared — appends a
structured JSONL event to `.sx/audit/YYYY-MM.jsonl`.

```bash
sx audit                                    # recent events
sx audit --actor alice@acme.com --since 30d
sx audit --event install.set --target code-reviewer
sx audit --since 7d --json
```

Filters are AND-combined. `--since` accepts `Nd` (days) or `all`.

### No-op skipping

Repeating a mutation that matches the current state (e.g. re-adding an
email that's already a team member, granting admin to someone who
already has it, or re-installing with an identical scope) is a silent
no-op: the manifest is not rewritten and no audit event is emitted.
This keeps the audit log free of noise from idempotent retries.

## Usage analytics

Usage events are appended to `.sx/usage/YYYY-MM.jsonl` as JSONL (one
event per line). The stats command renders an adoption dashboard:

```bash
sx stats                               # recent window
sx stats --since 7d
sx stats --since 30d --json            # machine-readable
sx stats --assets                      # per-asset view only
sx stats --teams                       # per-team view only
```

Fields: total events in window, top assets (unique actors, total uses),
per-team adoption (percentage of members who used any vault asset), top
actors.

A malformed JSONL line is logged and skipped at flush time so one bad
event never drops a whole batch of good ones; an event with an
unparseable timestamp is stamped with `time.Unix(0, 0)` so it falls
outside any `--since Nd` window rather than skewing "recent" totals.

## Identity model

Team membership, admin checks, and user-scope install gating all use
the email from `git config user.email` (cached per vault root for the
CLI's lifetime).

If git is unconfigured, sx synthesizes a read-only identity of the form
`local:$USER@$HOST`. The `local:` prefix guarantees it cannot collide
with a real email in a team admin list — any mutation using this
identity is rejected with a clear "set git config user.email" message.
Read-only commands (`sx team list`, `sx install`, `sx stats`,
`sx audit`) still work.

## Where state lives

| State | Location | Who writes |
|-------|----------|-----------|
| Manifest (assets, scopes, teams) | `<vault>/sx.toml` | Vault admins via `sx team *`, `sx add`, `sx install --scope` |
| Audit events | `<vault>/.sx/audit/YYYY-MM.jsonl` | Every mutation |
| Usage events | `<vault>/.sx/usage/YYYY-MM.jsonl` | Every `sx install` (lazy-flushed on git vaults) |
| Per-user resolved lock | `~/<cache>/sx/lockfiles/<vault-id>.lock` | `sx install` |
| Rotated lock history | `~/<cache>/sx/lockfiles/<vault-id>-<ts>.lock` | Every install whose resolved content differs from the previous |

## Sleuth vault

The [skills.new](https://skills.new) hosted vault supports the same
commands but delegates team state and audit storage to the server.
Team management and the audit log are accessible through the web UI;
the CLI surface here is identical so the same scripts work against
either vault type.
