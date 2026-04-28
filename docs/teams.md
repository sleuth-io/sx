# Team-scoped installs

`--team <name>` installs an asset for every member of a named team.
Teams are first-class objects in the vault — they have members,
admins, and a list of repositories the team owns. Team-scoped installs
flatten to the team's repositories at resolve time, so a team install
both targets the people in the team and the codebases they work on.

This document covers the team CRUD surface and team-scoped installs.
For the manifest schema, see
[manifest-spec.md](manifest-spec.md#assetsscopes--install-targets).
For the audit log generated alongside team mutations, see
[audit.md](audit.md). For the broader scope picker, see
[scoping.md](scoping.md).

## Lifecycle

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

Deleting a team cascades:

* every `kind = "team"` scope referencing it is removed from its
  asset, and an `install.cleared` audit event is emitted per asset
  with `reason = "team_deleted"`
* every bot whose `teams` list referenced it has the team stripped,
  and a `bot.team_removed` audit event is emitted per bot with
  `reason = "team_deleted"`

Both cascades happen inside the same transaction, so a future team
re-created under the same name does not silently inherit orphaned
asset scopes or bot memberships.

## Targeted installs

```bash
sx install code-reviewer --team platform
```

Adds a `kind = "team"` scope row to the asset in the vault's `sx.toml`.
At resolve time the row expands to "every team member running inside
one of the team's repositories" — so the asset reaches the right
people in the right codebases without you listing every repo
separately.

`--team <name>` requires the caller to be an admin of the named team,
re-checked inside the transaction against the freshly-loaded team list.

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
