# Usage analytics

Every `sx install` records a per-asset usage event so vault admins can
see what's actually being adopted. Events are appended to
`.sx/usage/YYYY-MM.jsonl` under the vault root; the `sx stats` command
aggregates them into a dashboard.

## The `sx stats` command

```bash
sx stats                               # last 7 days
sx stats --since 30d                   # widen the window
sx stats --since all                   # lifetime totals
sx stats --assets                      # per-asset view only
sx stats --teams                       # per-team view only
sx stats --since 30d --json            # machine-readable
```

`--since` accepts `Nd` (days) or `all`. The dashboard renders four
sections: totals, top assets, per-team adoption, and top actors. The
`--assets` and `--teams` flags narrow to one section each.

### What the dashboard shows

- **Total events** — number of recorded installs in the window.
- **Top assets** — each asset's total use count plus the number of
  unique users who installed it.
- **Team adoption** — for each team, the percentage of members who
  recorded any install in the window (e.g. `platform 3/5 = 60%`).
- **Top actors** — users with the most installs in the window.

## JSON output

`sx stats --json` produces:

```json
{
  "since": "2026-03-18T00:00:00Z",
  "total_events": 127,
  "assets": [
    { "AssetName": "code-reviewer", "TotalUses": 42, "UniqueActors": 17 }
  ],
  "teams": [
    { "name": "platform", "member_count": 5, "active_members": 3, "adoption_pct": 60.0 }
  ],
  "top_actors": [
    { "Actor": "alice@acme.com", "TotalUses": 9 }
  ]
}
```

When `--since all` is used the `since` field is omitted rather than
serialised as a zero time.

## Storage format

Each event is one JSON object on a line in `.sx/usage/YYYY-MM.jsonl`:

```json
{
  "ts": "2026-04-17T10:04:12.445Z",
  "actor": "alice@acme.com",
  "asset_name": "code-reviewer",
  "asset_version": "1.2.3",
  "asset_type": "skill"
}
```

## Fault tolerance

- A malformed JSONL line is logged at `warn` and skipped. One bad line
  never drops a whole batch of good ones from a flush.
- An event with an unparseable timestamp is stamped with
  `time.Unix(0, 0)` so it falls outside any `--since Nd` window rather
  than skewing "recent" totals; `--since all` still counts it.

## Git vault: lazy flush

Recording a usage event does not commit or push on git vaults.
Appending to a JSONL file under an active write flock would otherwise
produce a commit per install and flood the history. Events are held
locally and swept into the next management commit (team mutation,
install-set, etc.), which keeps history clean while preserving
durability across CLI runs.

## Sleuth vault

The [skills.new](https://skills.new) hosted vault ingests the same
events through its usage endpoint. `sx stats` falls back to the
server's aggregation API; the web UI provides per-asset charts and
per-user drill-downs that the CLI does not render.
