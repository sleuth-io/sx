# Shared vaults in cloud-synced folders

A vault placed inside a folder that a cloud-sync client already shares —
Dropbox, Google Drive, OneDrive, or iCloud Drive — gives a team shared
assets with **zero infrastructure**: no git, no GitHub accounts, no
server. This is the recommended sharing model for teams without git
access (marketing, design, ops).

There is nothing special about such a vault. It is a plain path vault
(`--type path`); the sync client does the sharing. Everything in
[vault-spec.md](vault-spec.md) applies unchanged.

## Setup

Interactive (recommended):

```
sx init
  → Share with my team
  → Shared folder
```

`sx init` detects sync roots on your machine (Dropbox, Google Drive,
OneDrive, iCloud) and offers them as locations, suggesting an `sx-vault`
subfolder. Scripted setup:

```bash
sx init --type path --path ~/Dropbox/sx-vault
```

Typical sync-root locations:

| Provider | macOS | Windows | Linux |
|---|---|---|---|
| Dropbox | `~/Library/CloudStorage/Dropbox*` or `~/Dropbox` | `%USERPROFILE%\Dropbox` | `~/Dropbox` |
| Google Drive | `~/Library/CloudStorage/GoogleDrive-<account>/My Drive` | `%USERPROFILE%\My Drive` | `~/Google Drive` |
| OneDrive | `~/Library/CloudStorage/OneDrive*` | `%USERPROFILE%\OneDrive*` | `~/OneDrive` |
| iCloud Drive | `~/Library/Mobile Documents/com~apple~CloudDocs` | — | — |

## Sharing with teammates

1. Share the folder with your teammates in the provider's UI (Dropbox
   "Share folder", Google Drive "Share", etc.). They need **edit**
   access to publish assets; view access is enough to install.
2. Each teammate waits for the folder to sync, then runs:

   ```bash
   sx init --type path --path "<their local path to the folder>"
   ```

   `sx init` detects the existing vault and joins it — nothing is
   created or overwritten.

Paths differ per machine (a Google Drive folder mounts under a
per-account path, for example); each user's config stores their own
local path to the same shared folder.

## Who did what (identity)

Changes are attributed to your git identity (`git config user.email`),
falling back to `$USER@hostname` when git isn't configured. For stable
attribution — and for team/user scoping to work — each teammate should
set their email once:

```bash
git config --global user.email you@company.com
```

Note that filesystem sharing has no enforcement: anyone with edit access
to the folder can modify anything. Scopes and teams in the manifest are
honored by `sx`, not enforced against a hostile editor. See
[Limitations](#limitations-and-when-to-graduate).

## Conflicts and how sx handles them

Sync clients don't merge concurrent edits — when two machines write the
same file before syncing, the client keeps one version and saves the
other as a conflicted copy (`sx (Bob's conflicted copy 2026-07-04).toml`,
`list.txt (1)`, …). sx's writes are atomic and versions are immutable,
so conflicts are rare and confined to the few shared index files:

- **Conflicted `sx.toml` (the manifest): sx stops with an error** until
  a human resolves it. To fix: compare the two files, merge any missing
  `[[assets]]` entries into `sx.toml`, delete the conflicted copy, and
  let the folder finish syncing.
- **Conflicted `list.txt` or asset directories: sx warns and ignores
  them.** Delete the conflicted copy once the folder has synced. A
  version present in the ignored copy can be re-published.
- **`name (N)` directories** (Google Drive's duplicate suffix) are
  skipped with a warning **only when the base `name` also exists**. An
  asset you deliberately named `foo (1)` with no sibling `foo` works
  normally. Nothing is ever deleted automatically.
- OS junk and sync temp files (`.DS_Store`, `.tmp.driveupload`, …) are
  ignored everywhere.

To keep conflicts rare: avoid two people publishing at the same moment,
and let the folder finish syncing before publishing after being offline.

## Limitations and when to graduate

- **No cross-machine locking.** sx's file lock serializes processes on
  one machine only; the sync client is the (eventually-consistent)
  transport between machines.
- **No enforced permissions or RBAC.** Everyone with folder edit access
  can change anything; scoping is honored cooperatively.
- **Sync lag.** A teammate sees your publish only after both sides have
  synced.

When the team outgrows these — more publishers, enforcement needs,
audit requirements — migrate losslessly to a git vault or Skills.new
with `sx vault copy --from <profile> --to <profile>`. See
[copy.md](copy.md) for details.
