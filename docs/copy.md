# Copying a vault: `sx vault copy`

Move everything in one vault into another — assets and all their versions,
teams, bots, installation scopes, audit history, and usage history. The source
and destination can be any backend, so you can convert a skills.new vault into a
git vault (or vice versa) without leaving the contents behind. For the full
step-by-step skills.new exit path, see
[Migrating from skills.new to your own git vault](migrate-from-skills-new.md).

```bash
sx vault copy --from <profile> --to <profile> [--only ...] [--dry-run] [--yes]
```

`--from` and `--to` name [profiles](profiles.md) — each profile points at one
vault (skills.new, git, or path). The command reads from the source profile's
vault and writes to the destination profile's vault; the two must be different.

## What gets copied

| Category | Detail |
|----------|--------|
| Teams | name, members, admins, and team→repository associations |
| Bots | name, description, and team memberships (API keys are **not** copyable — regenerate them) |
| Assets | every version of every asset, content included |
| Installation scopes | each asset's org/repo/path/team/user/bot installs |
| Audit history | every event, with its original timestamp and actor preserved |
| Usage history | every usage event, with its original actor preserved |

Copying runs in dependency order — teams and bots first, then assets (so an
asset's team/bot scopes have something to resolve against), then audit and
usage.

### Smart, version-aware asset copy

Assets are uploaded version by version, oldest first. The destination's own
versioning absorbs them: content that already exists is skipped, and changed
content lands as a new version. Re-running a copy doesn't duplicate asset
versions.

## Previewing

Without `--yes`, the command performs a **read-only preview** and prints what it
would copy plus anything that can't transfer. Apply with `--yes`:

```bash
sx vault copy --from skills-new --to git-vault            # preview only
sx vault copy --from skills-new --to git-vault --dry-run  # explicit preview
sx vault copy --from skills-new --to git-vault --yes      # apply
```

Use `--only` to restrict to specific categories
(`teams,bots,assets,collections,audit,usage`):

```bash
sx vault copy --from a --to b --only assets,teams --yes
```

The copy is **best-effort**: if one item fails (an asset that won't download, a
scope that won't resolve), it's recorded as a warning and the copy continues —
one bad item never aborts the whole migration. The final report lists every
warning.

Repo scope rows and team repository rows written by older sx versions
that kept a port (`gitea.corp.com:3000/acme/x`) are migrated in their
modern portless form (`gitea.corp.com/acme/x`) so they keep matching
on the destination. Each such rewrite is listed in the report; if a
row's numeric segment was a genuine path component rather than a port,
re-add it on the destination as `https://host/2024/team/app`.

## Directionality and what's lossy

Copies between **git and path vaults are fully lossless** — those backends store
everything (manifest, audit, usage) as files.

Copies involving **skills.new** are lossless for assets, teams, bots, scopes,
collections (including their collection-level install rows), audit, and usage
in both directions, with these exceptions:

- **Bot API keys** can't be copied (they're shown once at creation). Regenerate
  them on a skills.new destination; git and path vaults have no bot keys — a
  job claims a bot identity with `SX_BOT=<name>` instead (see [Bots](bots.md)).
- **Cross-org copies** require the referenced entities to exist in the
  destination org: a team can only include members who are users of that org,
  and repo/user-scoped installs only land if that repo/user exists there.
  Targets that can't be resolved are skipped with a warning.
- **Audit and usage import is additive.** If a copy's audit/usage stage fails
  part-way and you re-run it, the already-imported events are duplicated on the
  destination.

(skills.new publishes an uploaded asset org-wide by default; the copy detects a
source asset that has no installations and clears that auto-applied install on
the destination, so an uninstalled asset stays uninstalled.)

To keep user-scoped installs intact, the copy's scope writes are trusted: they
carry "just for me" installs belonging to users other than the operator, which
the file-backed vaults' self-only rule would otherwise reject (see
[Users](users.md)). The scope already existed in the source, so replicating it
is not the escalation that rule guards against.

The preview/report always names what was skipped, so nothing is lost silently.

## See also

- [Scoping](scoping.md) — the install scopes that travel with each asset
- [Audit log](audit.md) and [Usage analytics](stats.md) — the histories copied
- [Profiles](profiles.md) — how `--from`/`--to` resolve to vaults
