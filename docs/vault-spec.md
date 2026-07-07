# SX Vault Specification

## Overview

This specification defines the structure and protocol for asset vaults. A
vault stores versioned assets and their metadata in a standardized layout
that supports both filesystem and HTTP access patterns.

The current storage format is **v2**. Its design priorities:

- **Usable in place**: `assets/` is a plain folder of assets — the latest
  version of each one, directly readable by humans, editors, and AI agents
  (point `.claude/skills`, an Obsidian vault, or grep straight at it).
- Simple filesystem layout that works locally or over HTTP
- Immutable, cacheable version archive with efficient version discovery
- Metadata alongside assets (Maven pattern)
- Minimal protocol overhead

The manifest's `schema_version` selects the storage format: `1` → legacy
layout (see [Appendix: Format v1](#appendix-format-v1-legacy)), `2` → the
layout described here. `internal/vault/layout` is the single source of truth
for path construction in the implementation.

## Vault Types

Vaults can be accessed via:

- **Filesystem**: Local or network-mounted directories (path vaults)
- **Git**: A git repository holding the same directory structure
- **HTTP**: Web servers serving static files or dynamic APIs (read-only)

All use the identical directory structure. (The Sleuth / skills.new vault
stores assets server-side behind an API and is not covered by this layout.)

## Directory Structure (format v2)

```
{vault-root}/
  sx.toml                                 # Manifest (source of truth) — see manifest-spec.md
  assets/
    {asset-name}/                         # THE asset — latest version, directly usable
      SKILL.md                            # (or AGENT.md, mcp.json, … per asset type)
      metadata.toml                       # includes the current version
      references/…                        # any other asset files
  .sx/
    audit/YYYY-MM.jsonl                   # Audit event stream (append-only)
    usage/YYYY-MM.jsonl                   # Usage event stream (append-only)
    versions/
      {asset-name}/
        list.txt                          # Version listing
        {version}/                        # Immutable archive, full copy per version
          SKILL.md
          metadata.toml
          references/…
```

`sx.toml` holds the assets list, install scopes, teams, and collections.
See [manifest-spec.md](manifest-spec.md) for the full schema.

### Derived plugin-marketplace manifests

On every manifest save, v2 vaults with at least one skill asset also get
three **generated** files so the vault doubles as an AI-tool plugin
marketplace (see [plugins-spec.md](plugins-spec.md)):

```
{vault-root}/
  .claude-plugin/marketplace.json         # Claude Code marketplace (library + per-collection plugins)
  .codex-plugin/plugin.json               # Codex plugin (whole library)
  .agents/plugins/marketplace.json        # Codex marketplace listing
```

They are pure functions of `sx.toml` and are always overwritten — never
edit them by hand. They're removed when the vault has no skill assets.

### Invariants

1. **`assets/{name}/` is always a byte-identical copy of
   `.sx/versions/{name}/{latest}/`.** Publishing writes both (the archive
   copy first, then the materialized root view). The root view is a
   convenience for humans and agents; the archive is canonical. Clients
   repair root-view drift by re-copying from the archive.
2. **Archive paths are immutable.** Once `.sx/versions/{name}/{v}/` is
   written it is never modified or moved, preserving cacheability and
   pinned-version resolution.
3. **Lock files and manifest source paths reference archive paths only**
   (`.sx/versions/{name}/{version}`), never the mutable root view.
4. **`assets/` contains nothing but asset directories.** Version lists and
   history live under `.sx/`. Dot-prefixed entries under `assets/` are
   transient staging directories and are ignored by listings.

### Example: Filesystem Vault

```
./my-vault/
  sx.toml
  assets/
    github-mcp/
      mcp.json
      metadata.toml
    code-reviewer/
      SKILL.md
      metadata.toml
  .sx/
    vault-icon            # optional library icon (raw image bytes)
    versions/
      github-mcp/
        list.txt          # "1.2.3\n1.2.4\n"
        1.2.3/ …
        1.2.4/ …
      code-reviewer/
        list.txt          # "3.0.0\n"
        3.0.0/ …
```

## Library Icon (`.sx/vault-icon`)

Optional. Raw image bytes (PNG/JPEG/WebP/GIF, ≤1 MB; consumers sniff the
mime from content). Shared vault data: one user setting it applies to
everyone using the vault. The desktop app reads and writes it; for git
vaults changes are their own commit. skills.new vaults do not use this
file — their icon is the organization's icon, served by the API. (The
name avoids an `icon` basename, which the common macOS `Icon?` gitignore
rule would match case-insensitively and silently exclude from commits.)

## Version Listing (`list.txt`)

Required for each stored asset, at `.sx/versions/{name}/list.txt`. It
enables version discovery with a single small file fetch instead of
directory traversal.

**Who creates it**: publishing tools (`sx add`) create and update it when
publishing new versions.

### Format

Plain text, one semantic version per line:

```
1.0.0
1.2.3
2.0.0
```

- Versions in any order (clients sort/filter)
- No blank lines or comments; UTF-8; `\n` preferred, `\r\n` accepted

## Metadata Location

Following Maven conventions, metadata is stored **alongside** the asset:

- Per stored version: `.sx/versions/{name}/{version}/metadata.toml`
- Root view: `assets/{name}/metadata.toml` (copy of the latest version's)

This enables fetching metadata without downloading the full asset. See
[metadata-spec.md](metadata-spec.md) for the file format.

## URL/Path Construction

| Item | Path (relative to vault root) |
|---|---|
| Version listing | `.sx/versions/{name}/list.txt` |
| Version metadata | `.sx/versions/{name}/{version}/metadata.toml` |
| Version contents | `.sx/versions/{name}/{version}/…` |
| Latest (browsable) | `assets/{name}/…` |

HTTP vaults serve the same paths: `GET {base}/.sx/versions/{name}/list.txt`
etc.

## Resolution Protocol

When resolving `github-mcp>=1.2.3`:

1. **List versions**: read `.sx/versions/github-mcp/list.txt`
2. **Filter** by the specifier, **select** the highest compatible version
3. **Fetch metadata**: `.sx/versions/github-mcp/2.0.0/metadata.toml`
4. **Resolve dependencies** from metadata, recurse
5. **Generate lock entry** pointing at the immutable archive:

   ```toml
   [[assets]]
   name = "github-mcp"
   version = "2.0.0"
   type = "mcp"

   [assets.source-path]
   path = ".sx/versions/github-mcp/2.0.0"
   ```

## Format Migration (v1 → v2)

Vaults created before format v2 use the legacy layout (appendix below).
Migration is automatic and idempotent:

- **Reads never migrate.** A current client reads v1 vaults through a
  fallback indefinitely.
- **The first direct write migrates.** Before any mutation (add, remove,
  rename, scope change, team/bot management), the client converts the vault:
  each `assets/{name}` directory moves to `.sx/versions/{name}` (git
  history is preserved through the move), the latest version is
  materialized back at `assets/{name}`, manifest source paths are rewritten
  to archive locations, `schema_version` becomes `2`, and a
  `vault.migrated` audit event is appended. On git vaults this lands as its
  own commit (`Migrate vault storage to format v2`) pushed before the
  write's commit.
- **PR-branch writes do not migrate.** A contributor going through the
  pull-request flow keeps producing changes in the vault's existing format
  so the PR stays mergeable; the vault migrates on the next direct write.
- **Explicit**: `sx vault migrate` runs the same conversion on demand;
  `--dry-run` previews it. `sx vault copy` into a fresh vault is an
  alternative migration path.
- **Concurrent migrations** converge: the losing client discards its local
  migration commit, adopts the remote's, and proceeds.

After migration, **older sx builds can no longer use the vault**: every
operation that reads the manifest (including `sx install` on git/path
vaults, whose lock file is resolved client-side from the manifest) fails
with `unsupported manifest schema version … this build understands up to 1`.
Upgrade all team members' clients together.

## HTTP Vault Requirements

Static file servers (nginx, S3, GitHub Pages) work as-is.

### Caching Headers

```
# Archive paths are immutable:
.sx/versions/**  →  Cache-Control: public, max-age=31536000, immutable

# Root views and list.txt change on publish:
assets/**, .sx/versions/*/list.txt  →  ETag / short max-age
```

### Content Types

```
*.toml   -> application/toml
*.md     -> text/markdown
list.txt -> text/plain; charset=utf-8
```

### Authentication

HTTP vaults may require `Authorization: Bearer {token}` or basic auth.
Filesystem vaults rely on OS-level permissions. Production HTTP vaults
SHOULD use HTTPS.

## Integrity

- HTTP sources: lock entries carry hashes (required)
- Filesystem/git sources: hashes optional (transport is trusted; git commits
  provide history integrity)

## Publishing Assets

`sx add` performs, atomically per publish (one commit on git vaults):

1. Write the immutable copy: `.sx/versions/{name}/{version}/…`
2. Append the version to `.sx/versions/{name}/list.txt`
3. Refresh the root view `assets/{name}/` to mirror the latest version
4. Record the asset (with its archive source path) in `sx.toml`

Deleting a version removes its archive directory, updates `list.txt`, and
refreshes the root view to the remaining latest; deleting the last version
removes the asset's root view and archive entirely.

## Comparison with Other Systems

| System    | Version List         | Metadata Location   | Latest usable in place? |
| --------- | -------------------- | ------------------- | ----------------------- |
| **Maven** | `maven-metadata.xml` | Per asset           | No                      |
| **PyPI**  | JSON API endpoint    | Per version         | No                      |
| **npm**   | Packument            | All in one          | No                      |
| **Go**    | `@v/list`            | `@v/{version}.info` | No                      |
| **SX v2** | `list.txt`           | Per version         | **Yes** (`assets/{name}`) |

SX follows Go's simplicity with Maven's metadata-alongside-asset pattern,
plus a materialized "latest" view so the vault doubles as a plain folder of
markdown.

## Appendix: Format v1 (legacy)

Format v1 (manifests with `schema_version = 1`, or no `schema_version`
field at all) stored every version under the asset directory:

```
assets/
  {asset-name}/
    list.txt
    {version}/
      metadata.toml
      …asset files…
```

Current clients read v1 vaults transparently and migrate them on the first
direct write (see Format Migration above). v1 had no materialized latest
view — consuming an asset always required knowing its version directory.

Historical note: an earlier draft of this spec described zip-packaged
assets (`{name}-{version}.zip`); the implementation has always stored
file-backed vault assets exploded, and zips exist only as an in-memory
transport when moving assets between vaults.
