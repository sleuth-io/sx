# Copying a vault: `sx vault copy`

Move everything in one vault into another — assets and all their versions,
teams, bots, installation scopes, audit history, and usage history. The source
and destination can be any backend, so you can convert a skills.new vault into a
git vault (or vice versa) without leaving the contents behind.

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

## Directionality and what's lossy

Copies between **git and path vaults are fully lossless** — those backends store
everything (manifest, audit, usage) as files.

Copies involving **skills.new** are lossless for assets, teams, bots, scopes,
collections (including their collection-level install rows), audit, and usage
in both directions, with these exceptions:

- **Bot API keys** can't be copied (they're shown once at creation). Regenerate
  them on the destination.
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

The preview/report always names what was skipped, so nothing is lost silently.

## See also

- [Scoping](scoping.md) — the install scopes that travel with each asset
- [Audit log](audit.md) and [Usage analytics](stats.md) — the histories copied
- [Profiles](profiles.md) — how `--from`/`--to` resolve to vaults
