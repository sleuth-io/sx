# Audit log

Every mutation on a git or path vault appends a structured JSONL event
to `.sx/audit/YYYY-MM.jsonl` under the vault root. The log is an
append-only record of who did what and when, intended for compliance
reviews and incident response.

For the commands that generate these events, see [teams.md](teams.md).

## Querying the log

```bash
sx audit                                    # recent events (default --since 7d)
sx audit --since 30d                        # widen the window
sx audit --since all                        # the whole log
sx audit --actor alice@acme.com             # one actor
sx audit --event install.set                # one event type
sx audit --target code-reviewer             # one asset / team
sx audit --since 7d --json                  # machine-readable
sx audit --limit 20                         # cap the output
```

Filters are AND-combined: `--actor alice@acme.com --event install.set
--target code-reviewer --since 30d` returns the set where all four
conditions hold. `--since` accepts `Nd` (days) or `all`.

## Events

| Event | TargetType | Target | Data keys |
|-------|------------|--------|-----------|
| `team.created` | `team` | team name | `description`, `members`, `admins`, `repositories` |
| `team.updated` | `team` | team name | _(none — replaces whole team body)_ |
| `team.deleted` | `team` | team name | _(none)_ |
| `team.member_added` | `team` | team name | `member`, `admin` |
| `team.member_removed` | `team` | team name | `member` |
| `team.admin_set` | `team` | team name | `member` |
| `team.admin_unset` | `team` | team name | `member` |
| `team.repo_added` | `team` | team name | `repository` |
| `team.repo_removed` | `team` | team name | `repository` |
| `bot.created` | `bot` | bot name | `description`, `teams` |
| `bot.updated` | `bot` | bot name | _(none — replaces whole bot body)_ |
| `bot.deleted` | `bot` | bot name | `cleared_assets` (asset names whose `kind = "bot"` scopes were cascaded) |
| `bot.team_added` | `bot` | bot name | `team` |
| `bot.team_removed` | `bot` | bot name | `team`, optional `reason` (e.g. `team_deleted`) |
| `install.set` | `installation` | asset name | `kind`, plus one of `repo`/`paths`/`team`/`user`/`bot` |
| `install.cleared` | `installation` | asset name | `kind`, `reason` (e.g. `team_deleted`, `bot_deleted`) |

Cascade events are emitted automatically:

* `team.deleted` cascades to `install.cleared` (one per asset that had a
  `kind = "team"` scope on the deleted team, with `reason =
  "team_deleted"`) and to `bot.team_removed` (one per bot that was on
  the deleted team, with `reason = "team_deleted"`).
* `bot.deleted` cascades to `install.cleared` (one per asset that had a
  `kind = "bot"` scope on the deleted bot, with `reason =
  "bot_deleted"`).

Bot API key creation and deletion are audited only for Sleuth vaults,
on the server-side audit stream — file-based vaults reject bot key
operations entirely, so no local audit constants exist for those
events.

## No-op skipping

Repeating a mutation that matches the current state (e.g. re-adding an
email that's already a team member, granting admin to someone who
already has it, or re-installing with an identical scope) is a silent
no-op: the manifest is not rewritten and no audit event is emitted.
This keeps the log free of noise from idempotent retries — scripts
can safely re-run without padding the audit trail.

## Storage format

Events are appended to monthly JSONL files: `.sx/audit/YYYY-MM.jsonl`.
Each line is a self-contained JSON object:

```json
{
  "ts": "2026-04-17T10:04:12.445Z",
  "actor": "alice@acme.com",
  "event": "install.set",
  "target_type": "installation",
  "target": "code-reviewer",
  "data": { "kind": "team", "team": "platform" }
}
```

Append-only semantics are enforced by the vault flock on path and git
vaults. A mutation's audit write is best-effort: if the append fails
(disk full, etc.) the manifest mutation is already durable and the
audit gap surfaces as an operational alarm, not as a blocked write.

## Sleuth vault

When the configured vault is [skills.new](https://skills.new), audit
events are logged server-side instead of under `.sx/audit/`. The
`sx audit` CLI still works and issues a GraphQL query; the web UI
provides richer filtering and export.
