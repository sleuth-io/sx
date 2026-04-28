# Manifest Specification (`sx.toml`)

The manifest is the single source of truth for a vault. It lives at
`sx.toml` in the vault root and holds every managed asset, its install
scopes, and the team definitions those scopes reference. `sx` commands
that mutate vault state read and write this file transactionally.

## File location

```
<vault-root>/
  sx.toml              ← the manifest (this spec)
  .sx/
    audit/YYYY-MM.jsonl   ← audit event stream (append-only)
    usage/YYYY-MM.jsonl   ← usage event stream (append-only)
  assets/
    <name>/<version>/ …   ← asset file storage (vault-specific)
```

The per-user resolved lock file is not stored in the vault. See
[lock-spec.md](lock-spec.md).

## Top-level fields

```toml
schema_version = 1
created_by     = "sx 0.1.0"       # informational
```

| Field            | Type    | Required | Notes                                         |
|------------------|---------|----------|-----------------------------------------------|
| `schema_version` | integer | yes      | Gates on-disk compatibility. Current value: 1 |
| `created_by`     | string  | no       | Build/version that last wrote the file        |
| `assets`         | array   | no       | Managed assets; may be empty                  |
| `teams`          | array   | no       | Team definitions; may be empty                |
| `bots`           | array   | no       | Bot definitions; may be empty                 |

A build that encounters a `schema_version` higher than it understands
refuses to read the file. When the field is absent it defaults to the
current version for forward compatibility.

## `[[assets]]` — managed assets

Each asset describes one installable package: its identity, source, and
the scopes that determine who receives it.

```toml
[[assets]]
name    = "code-reviewer"
version = "1.2.3"
type    = "skill"
clients = ["claude"]            # optional; matches all clients if omitted

  [assets.source-http]
  url    = "https://vault.company.com/assets/code-reviewer/1.2.3.zip"
  hashes = { sha256 = "abc…" }
  size   = 12345

  [[assets.scopes]]
  kind = "repo"
  repo = "github.com/acme/infra"

  [[assets.scopes]]
  kind  = "path"
  repo  = "github.com/acme/docs"
  paths = ["README.md", "CONTRIBUTING.md"]

  [[assets.scopes]]
  kind = "team"
  team = "platform"

  [[assets.scopes]]
  kind = "user"
  user = "alice@acme.com"
```

### Asset fields

| Field          | Type              | Notes                                                       |
|----------------|-------------------|-------------------------------------------------------------|
| `name`         | string            | Required. Primary key within the manifest                   |
| `version`      | string            | Required. Exact pin, e.g. `"1.2.3"`                         |
| `type`         | string            | Required. One of `skill`, `rule`, `agent`, `command`, `mcp`, `hook` |
| `clients`      | array of string   | Optional. Which AI clients this asset targets. Omit to match all |
| `dependencies` | array of table    | Optional. Each entry has `name` and optional `version`      |

### Source

Exactly one of `source-http`, `source-path`, `source-git` is set per asset.

```toml
[assets.source-http]
url    = "https://…/asset.zip"
hashes = { sha256 = "…" }
size   = 12345

[assets.source-path]
path   = "./assets/code-reviewer/1.2.3"

[assets.source-git]
url          = "https://github.com/acme/tools.git"
ref          = "abc1234…"   # exact commit SHA
subdirectory = "skills/reviewer"
```

## `[[assets.scopes]]` — install targets

The `scopes` array on an asset enumerates every install target. An asset
with no scopes declared is **org-wide** — available everywhere.

| `kind`   | Required fields     | Effect                                                                 |
|----------|---------------------|------------------------------------------------------------------------|
| `org`    | _(none)_            | Explicit org-wide marker. Same effect as an empty scopes array         |
| `repo`   | `repo`              | Available in the named repository                                      |
| `path`   | `repo`, `paths`     | Available for specific paths within a repository                       |
| `team`   | `team`              | Available to every member of the named team                            |
| `user`   | `user` (email)      | Available to a single user. Marks the asset global in that user's lock |
| `bot`    | `bot` (name)        | Available to a single bot identity. See [bots.md](bots.md)             |

Team and user scopes are identity-dependent: the vault resolves them
against the caller's git identity when producing the per-user lock file
(see [lock-spec.md](lock-spec.md)).

## `[[teams]]` — team definitions

```toml
[[teams]]
name         = "platform"
description  = "Platform eng"
members      = ["alice@acme.com", "bob@acme.com"]
admins       = ["alice@acme.com"]
repositories = ["github.com/acme/infra", "github.com/acme/tools"]
```

| Field          | Type            | Notes                                                 |
|----------------|-----------------|-------------------------------------------------------|
| `name`         | string          | Required. Primary key                                 |
| `description`  | string          | Optional                                              |
| `members`      | array of string | Normalised to lowercase emails, deduplicated, sorted  |
| `admins`       | array of string | Must be a subset of `members`; at least one required  |
| `repositories` | array of string | Normalised repo URLs; drives team-scope resolution    |

Every team must have at least one admin at all times. A mutation that
would leave the team without an admin is rejected by the CLI.

## `[[bots]]` — bot definitions

```toml
[[bots]]
name        = "python-backend"
description = "Backend CI bot"
teams       = ["platform", "data"]
```

| Field         | Type            | Notes                                     |
|---------------|-----------------|-------------------------------------------|
| `name`        | string          | Required. Primary key                     |
| `description` | string          | Optional                                  |
| `teams`       | array of string | Team names the bot is a member of         |

Bots gain repository context through their team memberships, the same
way human team members do. See [bots.md](bots.md) for the lifecycle and
resolution rules. File-based vaults (path/git) treat bots as
identity-only — there are no API keys stored in the manifest. Sleuth
vaults issue real OAuth tokens via `sx bot key create`.

## Mutation semantics

* Every CLI that mutates the manifest reads the file, applies the change,
  and atomically writes the result (temp file + rename).
* Git vaults wrap each mutation in a repo-wide file lock plus clone / pull
  / commit / push so concurrent operators don't race.
* After the manifest write, the CLI appends one audit event to
  `.sx/audit/YYYY-MM.jsonl`. The audit write is best-effort; durable
  manifest state is preserved even if the audit append fails.

## Example manifest

```toml
schema_version = 1
created_by     = "sx 0.1.0"

[[assets]]
name    = "code-reviewer"
version = "1.2.3"
type    = "skill"

  [assets.source-http]
  url    = "https://vault.company.com/assets/code-reviewer/1.2.3.zip"
  hashes = { sha256 = "aaaa…" }

  [[assets.scopes]]
  kind = "team"
  team = "platform"

[[assets]]
name    = "changelog-bot"
version = "0.4.0"
type    = "agent"

  [assets.source-git]
  url = "https://github.com/acme/bots.git"
  ref = "b5c2…"
  # no scopes → org-wide

[[teams]]
name         = "platform"
members      = ["alice@acme.com", "bob@acme.com"]
admins       = ["alice@acme.com"]
repositories = ["github.com/acme/infra"]
```
