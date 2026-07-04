# SX v2 Spec: Vault Format v2 + Desktop App

Status: **Approved for implementation**
Decisions locked (2026-07-03): Wails (Go) desktop framework · app lives in this repo · clean-break migration · write-time dedup deferred to app v1.1.

This spec has two parts, executed in order:

1. **Part 1 — Vault Format v2**: restructure vault storage so the latest version of every asset is directly usable at `assets/{name}/`, with version history moved under `.sx/versions/`. Includes automatic, seamless migration of v1 vaults.
2. **Part 2 — Desktop App**: a native Mac/Linux/Windows app (Wails) that makes the vault usable by non-technical users: drag a markdown file in, browse the library, organize collections, install to AI clients.

---

## Part 1: Vault Format v2

### Motivation

Today's layout (`assets/{name}/{version}/…` + `assets/{name}/list.txt`) is readable but not *usable in place*:

- You can't point `.claude/skills`, an editor, or an Obsidian vault at `assets/` — every skill appears N times, once per version.
- Search/grep/embedding over the repo hits N copies of everything.
- A human browsing GitHub must read `list.txt` to find the current version.

v2 makes `assets/` a plain folder of skills — one canonical, current copy per asset — and moves all versioning machinery into `.sx/` where audit and usage logs already live.

### v2 Layout

```
{vault-root}/
  sx.toml                                # manifest, schema_version = 2
  assets/
    {asset-name}/                        # THE asset — latest version, directly usable
      SKILL.md                           # (or AGENT.md, mcp.json, etc. per asset type)
      metadata.toml                      # includes version = "<current>"
      references/…                       # any other asset files
  .sx/
    audit/YYYY-MM.jsonl                  # unchanged
    usage/YYYY-MM.jsonl                  # unchanged
    versions/
      {asset-name}/
        list.txt                         # moved from assets/{name}/list.txt
        {version}/                       # immutable archive, full copy per version
          SKILL.md
          metadata.toml
          references/…
```

### Invariants

1. **`assets/{name}/` is always a byte-identical copy of `.sx/versions/{name}/{latest}/`.** Publish writes both (dual write). The root view is a materialized convenience; the archive is canonical.
2. **Archive paths are immutable.** Once `.sx/versions/{name}/{v}/` is written, it is never modified or moved. This preserves cacheability and pinned-version resolution.
3. **Lock files reference archive paths only.** `source-path` / `source-git-dir` entries point at `.sx/versions/{name}/{version}`, never at the mutable root view. The root view is for humans and agents; machinery uses the archive.
4. **`assets/` contains nothing but asset directories.** No `list.txt`, no sidecars. Reserved: an asset may not be named `.sx`.

### Schema & manifest changes

- Bump `CurrentSchemaVersion` from 1 to 2 (`internal/manifest/manifest.go:33`). A v2 vault's `sx.toml` has `schema_version = 2`.
- The manifest's `schema_version` is the **single source of truth for layout**: v1 → old paths, v2 → new paths. No per-asset flags, no mixed states.
- Add `[[collections]]` to the v2 manifest schema (used by Part 2, harmless to CLI):

```toml
[[collections]]
name = "onboarding"
description = "Everything a new marketer needs"
assets = ["brand-voice", "campaign-checklist"]
created-by = "alice@acme.com"
```

Collections are curated, named lists of asset names. They carry no ACL semantics (teams remain the ACL mechanism); they are an organizational and bulk-install unit.

### Why seamless migration works

Three existing mechanisms make this safe:

- **Old clients fail loudly, not weirdly.** v1 clients already reject manifests with a higher `schema_version` via `ErrUnsupportedSchema` (`internal/manifest/manifest.go:35-39`). After migration, an old client's manifest-touching command produces a clear "unsupported schema version — upgrade sx" error rather than silent corruption or confusing "asset not found".
- **Old clients fail with the same clear message on installs too** (verified against v1.8.1): for git and path vaults the lock file is resolved client-side *from the manifest*, so `sx install` on a migrated vault reports the unsupported-schema error rather than silently misbehaving. Only Sleuth vaults (server-resolved lock) keep old-client installs working. The loud, actionable failure is the property that matters for the clean break.
- **There is a single write choke point.** All git-vault mutations flow through `acquireFileLock` → `cloneOrUpdate` → mutate → `commitAndPush` (`internal/vault/gitrepo.go:125-152, 200-240`). Migration slots into that transaction: it is one commit, atomic from the remote's perspective.

### Migration algorithm

Trigger: **first mutation** of a v1 vault by a v2 client (add/edit/delete/scope/team/bot change — anything that would commit). Also exposed explicitly as `sx vault migrate` for admins who want to do it proactively. Reads never trigger migration; a v2 client reads v1 vaults indefinitely via the read fallback.

Within the existing write transaction (file lock held, fresh clone/pull):

1. Verify `sx.toml` has `schema_version = 1`. If already 2 (raced with another client), skip to the requested mutation.
2. For each asset directory `assets/{name}/`:
   a. `git mv assets/{name}/{version}` → `.sx/versions/{name}/{version}` for every version dir (preserves history/blame).
   b. `git mv assets/{name}/list.txt` → `.sx/versions/{name}/list.txt`.
   c. Determine latest version (existing `version.Sort` on the list).
   d. Copy `.sx/versions/{name}/{latest}/` → `assets/{name}/` (the materialized root view).
3. Set `schema_version = 2` in `sx.toml`.
4. Rewrite `sx.toml` source paths to reference `.sx/versions/…` (per-user lock files are regenerated on each client's next `sx install`, not by the migration).
5. Append audit event `vault.migrated` (new event type in `internal/mgmt/audit.go`) with `{from: 1, to: 2, assets: N}`.
6. Commit `"Migrate vault storage to format v2"` and push it — **then** apply the user's requested mutation as its own commit.

Concurrency and edge rules:

- **Cross-machine race**: two v2 clients migrate simultaneously → second push is rejected → existing retry pulls, sees `schema_version = 2`, skips migration, applies only its mutation. Idempotent by construction.
- **PR-branch flows** (`gitrepo_pr.go`): migration must never happen inside a PR branch (a v2 PR against a v1 main is unmergeable garbage). *(As implemented — see docs/vault-spec.md, which is normative: PR-branch writes skip migration entirely and use the v1 layout; the vault migrates on the next direct write.)*
- **Path vaults** (`pathrepo.go`): same steps minus git. *(As implemented: the migration runs in place under the vault's file lock and is resumable — an interrupted run is completed by the next write. The originally speced `.sx/migrate-tmp/` staging was dropped; per-asset moves are atomic renames.)*
- **Sleuth vault**: unaffected. Storage is server-side behind the HTTP/GraphQL API (`internal/vault/sleuth.go`); no client-visible layout exists. skills.new migrates (or doesn't) on its own schedule.
- **HTTP sources**: unaffected; they are fetch-only handlers, not a vault layout.

### Code changes

| Area | Change |
|---|---|
| `internal/vault/layout/` (new package) | Single source of truth for path construction, v1 and v2: `AssetRoot(name)`, `VersionDir(name, v)`, `VersionList(name)`, keyed by schema version. Kills the current gitrepo/pathrepo duplication. |
| `internal/vault/gitrepo.go` | `AddAsset`, `GetAssetByVersion`, `GetMetadata`, `GetVersionList`, delete/rename flows: route paths through `layout`, branch on manifest schema version. Add `migrateToV2()` called from the write transaction. Dual-write root view on publish. |
| `internal/vault/pathrepo.go` | Same, minus git; staged migration with rename. |
| `internal/manifest/` | `CurrentSchemaVersion = 2`; keep parsing v1 (read fallback); add `Collections` table; migration helper `Manifest.MigrateToV2()`. |
| `internal/lockfile/` | Lock regeneration on migration; add optional `schema_version` informational field to the lock (old clients ignore unknown TOML keys). |
| `internal/mgmt/audit.go` | New event type `vault.migrated`. |
| `internal/commands/` | New `sx vault migrate` command (explicit, with `--dry-run` printing the move plan). |
| `pkg/sxvault` | No signature changes; behavior gains v2 support transparently. |
| `internal/vault/vaultcopy` | No layout logic needed (it writes via destination `AddAsset`), but verify a v1→v2 copy e2e and document that `vault copy` to a fresh vault is an alternative migration path. |

### Documentation updates

- Rewrite `docs/vault-spec.md` for v2. It is **already stale** (documents zip packaging; reality is exploded storage) — fix both in one pass. Keep a "Format v1 (legacy)" appendix describing the read-fallback behavior.
- Update `docs/lock-spec.md` examples to `.sx/versions/…` paths.
- Update `docs/manifest-spec.md`: `schema_version = 2`, `[[collections]]`.

### Testing

- Unit: `layout` package path construction for both versions; manifest v1/v2 round-trip; collections parsing.
- E2E (pattern of `gitrepo_repair_e2e_test.go`):
  - Migrate a populated v1 git vault → assert layout, root-view == latest archive, lock regenerated, audit event, single migration commit, `git log --follow` traces history through the moves.
  - Concurrent migration race (two clients, push rejection path).
  - v2 client reads v1 vault without migrating (read fallback).
  - v1-format fixture + old-client simulation: manifest read fails with `ErrUnsupportedSchema`; install from regenerated lock succeeds.
  - PR-branch flow on v1 vault migrates on main first.
  - Path vault migration, including resuming an interrupted run (see migratev2 tests).
  - `vault copy` v1 source → v2 destination.
- Migrate the real `sleuth-io/skills-repository` as the final acceptance test (after tagging a release).

### Rollout

1. Ship in the next minor release with prominent changelog: "vaults auto-upgrade to format v2 on first write; older sx versions can no longer modify upgraded vaults — upgrade all team members' CLIs."
2. `sx --version` check on `ErrUnsupportedSchema` already gives old clients an actionable message; verify wording says "run: brew upgrade sx (or re-download)".

---

## Part 2: Desktop App

### Product goal

The "very, very, very easy" front door. A marketing person can: install one app → drag a `.md` file into it → it's now a named, versioned, shared asset → teammates see it → any AI client can use it. Zero exposure to manifests, versions, scopes, or git.

v1 explicitly excludes: write-time dedup (v1.1), self-improving skills (v2), team/RBAC administration UI (CLI/skills.new remain the admin surface).

### Architecture

- **Framework**: Wails (v2 stable channel; re-evaluate v3 at implementation time only if it has reached stable). Frontend: React + TypeScript + Vite + Tailwind.
- **Location**: this repo, same Go module (`github.com/sleuth-io/sx`), under `app/`:

```
app/
  main.go               # Wails entry point
  bridge/               # Go structs/methods bound to the frontend
    vault.go            # open/list/get/add/versions
    collections.go
    installs.go         # AI-client install/uninstall
    settings.go
  frontend/             # React app (Vite)
  wails.json
```

Because the app shares the module, `bridge/` may import `internal/vault`, `internal/clients`, `internal/manifest` directly. Prefer routing through `pkg/sxvault` where it suffices; where it doesn't (e.g. client installs, collections), extend `pkg/sxvault` rather than letting the bridge accumulate logic — the bridge should stay a thin translation layer. The CLI and app must share one code path for every operation.

- **No daemon, no server**: the app is a direct vault client, exactly like the CLI. Git/path/Sleuth backends all work as-is.

### Onboarding flow

First launch offers three paths, mapped to existing backends:

1. **"Just me"** → creates a local path vault at `~/SxLibrary` (name TBD). Zero setup. Upgrade path to shared later via `vault copy`.
2. **"My team (git)"** → paste a git URL (the skills-repository pattern); auth via system git credentials or a token field. Uses `OpenGit`.
3. **"skills.new"** → OAuth device flow (same as `sx init`). Uses `OpenSkillsNew`.

Identity: reuse the CLI's config (`~/.sx/config.toml`) so app and CLI stay in sync — the app is a second client of the same config, not a parallel world.

### v1 features

**Library.** Card grid of every asset in the vault: name, description (from metadata), type badge, version, last-updated, author. Search-as-you-type over name/description. Filter by type and collection. Reads via `ListAssets` + root-view metadata; on v2 vaults this is one directory walk.

**Drag-and-drop add.** Drop a `.md` file (or a folder for multi-file skills) anywhere on the window:

- Name defaults from frontmatter `name:` or filename; description from frontmatter or first paragraph; type inferred (frontmatter `description` + skill-ish content → skill; overridable in a single confirm sheet with Name / Description / Type).
- Versions are created **only on explicit Publish**. A drop (new asset or onto an existing one) lands as a draft with a confirm sheet whose primary action is Publish: first publish → `1.0`, publishing an update → auto-bump minor. The word "version" appears nowhere in the primary UI; history lives behind a "History" panel (reads `.sx/versions/{name}/list.txt`, one entry per publish, offers view + restore).
- Publish goes through the same `AddAsset` path as the CLI: archive + root view + audit event + commit/push. Push/pull progress surfaces as a subtle sync indicator, not git vocabulary.

**Edit.** Built-in markdown editor (CodeMirror) for `SKILL.md` etc., plus "Open in default editor" with file-watch. Saves accumulate in a **local draft** (stored in the app's data dir; the asset card shows a "Draft" badge; the vault is untouched) until the user hits **Publish**, which bumps the version and writes archive + root view in one commit. Drafts survive app restarts and can be discarded. The History panel is the undo story for published versions.

**Collections.** Create/rename/delete collections; add/remove assets via drag or checkbox. Stored in the v2 manifest `[[collections]]` table, so they sync to every vault member and are visible to the CLI (`sx collection list` — small CLI addition, read-only in this phase).

**Use with AI (install).** Per asset and per collection: "Use in…" toggles for detected clients (Claude Code, Cursor, Cline, etc. — reuse `internal/clients` detection and `InstallAssets`). Default scope: user-global. The app maintains installs the same way `sx install` does (lock-file driven), so CLI and app never fight.

**Sync & conflicts.** Poll the vault on focus + every N minutes (ETag-cheap). Remote changes appear automatically. Write conflicts (git push rejected) are retried via the existing pull-rebase-retry in `gitrepo.go`; if a same-asset conflict truly collides, last-writer-wins with the loser preserved as a version — never a merge UI.

**Update check.** On launch, compare against latest GitHub release; notify + link. (Auto-update deferred.)

### Out of scope for v1 (committed roadmap)

- **v1.1 — Write-time dedup**: on add/edit, compare name/description/content (shingling + cosine over local embeddings, provider-pluggable) against the library; surface "This looks 85% similar to *brand-voice* — open it / keep both". Groundwork in v1: content hashing per asset stored in metadata at publish.
- **v2 — Self-improving skills**: surface `.sx/usage` analytics per asset ("installed by 12, unused by 9"), staleness nudges (Guru-style verification), then usage-feedback-driven suggested edits with human approval.

### Build, packaging, release

- `make app-dev` (wails dev), `make app-build`; CI matrix job (macOS arm64/amd64 → `.app` in `.dmg`, Windows amd64 → NSIS installer, Linux amd64/arm64 → AppImage + .deb).
- GoReleaser keeps owning the CLI; the app ships from a separate GitHub Actions workflow triggered by the same tag, uploading to the same release.
- **Signing prerequisites** (needed before public release, not for dev builds): Apple Developer ID cert + notarization for macOS; an OV/EV code-signing cert for Windows. Unsigned builds are fine through the beta.
- CLI release remains independently usable; the app requires sx ≥ the v2-format release.

### Verification & quality loop (applies to the entire spec)

Nothing here is "done" because the code compiles and unit tests pass. Every slice is verified end-to-end by actually running it, and the work iterates autonomously — create, test, fix, judge — until it meets the bar. The user reviews only end results and gives minor feedback; they never debug process.

**CLI & migration verification (Part 1).**
- Use the **docker-interactive-testing** skill for every user-facing flow: real sx binary, real git remote (bare repo inside the container), real PTY via tmux for interactive prompts. Verify migration by building a populated v1 fixture vault → running one write command → inspecting the resulting layout, `git log`, regenerated lock, and audit log inside the container.
- Simulate the mixed-version team: pin a pre-v2 sx release binary in the container against a migrated vault; confirm mutations fail with the `ErrUnsupportedSchema` upgrade message and lock-file installs still succeed.
- `make prepush` green before any slice is called complete (project rule).

**App functional verification (Part 2).**
- `wails dev` exposes the full app, Go bindings included, in a browser: drive every flow with Playwright — onboarding for all three vault kinds (the git vault pointing at a dockerized remote), drag-and-drop add, draft → publish, history/restore, collections, install toggles, and sync/conflict behavior. Cross-check every app mutation with the CLI against the same vault (`sx vault list`, audit log).
- Launch the **built native app on this machine** (`make app-build`, then open it) to verify what the browser can't: real drag-and-drop from Finder, window chrome and menus, app lifecycle, and behavior without the dev server.

**Design & taste review.**
- After each UI milestone, capture screenshots of every screen in every state — empty, loading, populated, error, draft, conflict, light and dark — via Playwright and native `screencapture`, then review them against a written checklist: visual hierarchy and spacing consistency, typography scale, alignment, empty states that teach the next action, no dead-end error messages, sensible focus order and keyboard support, and copy tone (the words "version", "commit", "scope", "manifest" never appear in primary UI).
- Iterate the loop — find issues, fix, re-screenshot, re-judge — until a fresh screenshot pass produces nothing a design-literate reviewer would flag.

**Definition of done (every milestone).** All acceptance criteria demonstrated by execution, not by reading code; e2e suites and `make prepush` green; screenshot review clean; and a short verification log (what was run, what was observed) accompanies the milestone so the result can be reviewed without replaying the process.

### Milestones & acceptance criteria

**M1 — Format v2 (Part 1 complete).**
`make prepush` green; all Part 1 e2e tests pass; `sleuth-io/skills-repository` migrated by running one `sx` write command against it; opening the migrated repo shows one clean directory per skill under `assets/`; `.claude/skills` symlinked at a migrated `assets/{name}` works in Claude Code.

**M2 — App skeleton + read path.**
App opens all three vault kinds; library renders the migrated skills-repository; search/filter work.

**M3 — Write path.**
Drag-drop add, draft editing + explicit publish, history/restore — all visible to the CLI (`sx vault list` agrees) and producing correct audit events.

**M4 — Collections + installs.**
Collections CRUD synced through the manifest; "Use in Claude Code" installs/uninstalls an asset and a collection; CLI `sx collection list` reads them.

**M5 — Packaging.**
CI produces installable artifacts for all three OSes from a tag; update check works.

### Risks

- **Wails v2 maturity on Linux** (WebKitGTK variance): mitigate by testing on Ubuntu LTS + Fedora in CI early (M2).
- **Root-view drift** (root ≠ latest archive after a partial failure): the invariant is checked and repaired on every vault open (cheap compare of `metadata.toml` version vs list head; repair = re-copy from archive; audit `vault.repaired`).
- **Old-client breakage in mixed-version teams**: accepted (clean-break decision); mitigated by loud `ErrUnsupportedSchema` messaging on every old-client operation that touches the manifest (including installs on git/path vaults — verified). Release notes must tell teams to upgrade together.
- **Same-module app bloating CLI builds**: `app/` is excluded from the CLI build graph (no imports from `cmd/sx`); GoReleaser config untouched.

### Execution order

1. Part 1, PR-sized slices: `layout` package → v2 read path + fallback → v2 write path → migration + `sx vault migrate` → lock regeneration → docs rewrite → e2e suite → migrate skills-repository.
2. Part 2: M2 → M3 → M4 → M5, each a PR stack.
3. Then v1.1 dedup (separate spec addendum when reached).
