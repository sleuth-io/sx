# Verification log: vault format v2 + migration

Date: 2026-07-03 · Branch: `vault-format-v2` · Method: real `sx` binaries in a
clean Ubuntu 24.04 Docker container with a real git remote and a tmux PTY
(per the docker-interactive-testing skill), plus the Go e2e suite.

## Environment

- `sx` (new): built from this branch (linux/arm64)
- `sx-old`: built from tag `v1.8.1` (last release before format v2)
- Vault: bare git repo seeded by hand in v1 format (`assets/chat/{1.0,2.0}/`,
  `list.txt`, `sx.toml` with `schema_version = 1`)

## What was run and observed

| # | Action | Observed |
|---|--------|----------|
| 1 | `sx vault list` (new binary, v1 vault) | Lists `chat v2.0` correctly. Fresh clone of the remote confirms **reads do not migrate** (still `schema_version = 1`, v1 layout). |
| 2 | `sx vault migrate --dry-run` | Prints `v1 → v2`, names the 1 asset that would move, changes nothing. |
| 3 | `sx add /root/new-skill --yes --no-install` | Succeeds. Remote log: `seed v1 vault` → `Migrate vault storage to format v2` → `Add linter 1` (migration is its own commit). Remote tree: clean `assets/{chat,linter}/` root views (SKILL.md + metadata.toml only), full archive under `.sx/versions/` with `list.txt`, `sx.toml` at `schema_version = 2` with source paths rewritten to `.sx/versions/...`, and a `vault.migrated` audit event with actor/from/to/assets. |
| 4 | `sx-old vault list` (migrated vault) | Shows an **empty** asset list (old ListAssets silently skips assets whose version lookup fails). Soft failure — documented below. |
| 5 | `sx-old install` (migrated vault) | Fails loudly and clearly: `unsupported manifest schema version: file uses schema 2, this build understands up to 1`. |
| 6 | `sx-old add … --yes` (migrated vault) | Same clear unsupported-schema failure. No corruption. |
| 7 | `sx install` (new binary, migrated vault) | Installs both `linter` and `chat` (v2.0, correct content) into `~/.claude/skills/` from the archive paths. |

## Findings & fixes made during verification

1. **Fixed:** losing the migration push race left the clone mid-rebase, breaking
   the follow-up write. Recovery now aborts the rebase before re-anchoring to
   the remote (`git.Client.RebaseAbort`, covered by `TestGitVaultMigrationRace`).
2. **Fixed:** migrating a hand-authored manifest with no `created_by` produced
   a lock file that failed validation (`created-by is required`). Migration now
   stamps `created_by` when missing.
3. **Spec correction:** for **git vaults**, old clients cannot install from a
   migrated vault either — the lock file is resolved client-side from the
   manifest, so `sx install` fails with the clear unsupported-schema message.
   The "installs keep working on old clients" property holds only for Sleuth
   vaults (server-resolved lock). The failure is loud and actionable, which is
   the property that matters.
4. **Known soft spot (accepted):** old clients' `sx vault list` on a migrated
   vault shows an empty list rather than the schema error (their ListAssets
   swallows per-asset errors). Mutations and installs fail with the clear
   message; release notes must tell teams to upgrade in lockstep.

## Go e2e suite (all passing, `-race`)

- `TestGitVaultReadsDoNotMigrate` — reads use the v1 fallback, remote untouched
- `TestGitVaultMigratesOnFirstWrite` — migration commit + write commit pushed;
  `git log --follow` traces history through the move; audit event; lock
  references archive paths
- `TestGitVaultMigrationRace` — concurrent migration: loser recovers, exactly
  one migration commit on the remote, loser's write still lands
- `TestGitVaultExplicitMigrate` — `sx vault migrate` plan + apply + idempotency
- `TestMigrateStorageToV2*` — layout conversion, source-path rewrite (incl.
  `./`-prefixed), interrupted-migration resume, uninitialized-vault no-op
- `TestPathVaultReadsAfterMigration` — all read paths on a migrated path vault
- `TestCopy_V1SourceToV2Destination` — `vault copy` reads a v1 source without
  migrating it; destination stores v2
- `TestAddPR_OpenPRWhenDenied_Yes` — PR-branch writes stay v1 on a v1 vault

`make prepush` (format + lint + tests + build) green.
