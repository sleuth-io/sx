# Verification log: sx desktop app (M2–M5)

Date: 2026-07-03 · Branch: `vault-format-v2` · Method: Playwright driving the
full app (Go bindings included) through `wails dev`, in a sandboxed
environment (`SX_CONFIG_DIR` + `HOME` pointed at `/tmp/sx-app-home`, path
vault at `/tmp/sx-app-vault` seeded with the real `sx` CLI), plus native
`sx.app` launch, plus CLI cross-checks after every app mutation.

## What was run and observed

| Flow | Observed |
|---|---|
| Library render | Cards with type badges, revision counts, relative timestamps from the fixture vault. Search narrows live; type chips filter; empty-search and empty-library states show the right copy. |
| Asset detail | Markdown rendered (headings, lists), file tabs (SKILL.md / metadata.toml), History dropdown. Selecting an old revision shows the amber "Viewing an older revision" pill and correct old content. Escape closes. |
| Onboarding — solo | "Just me" created `$HOME/SX Library`, saved the config, landed in the empty library with drop-teaching copy. |
| Onboarding — team, bad URL | Compact one-line error ("couldn't connect: repository not found: … — check the URL and that you have access"). **Nothing persisted** — relaunch stays in onboarding. |
| Drop → draft | `CreateDraftFromPaths` on a loose `.md`: name slugged from filename, type detected (skill), file renamed to canonical `SKILL.md`, description lifted from frontmatter. Draft card (dashed amber) appears; survives reload. |
| Draft sheet → publish | Confirm sheet with name/kind/description + editable content. Publish created `release-notes@1` in the vault (archive + root view + manifest row, verified on disk) and removed the draft. `sx vault list` agrees. |
| Edit → publish changes | Edit on brand-voice seeded a draft from the latest revision; a content change published revision 3. |
| Restore | Viewing revision 1 → "Restore this revision" created revision 4 with revision 1's content; scope preserved; CLI shows `Latest Version: v4`. |
| Collections | Created "writing" via chip form; membership toggles in the detail panel; chip filter narrows the grid; collection bar shows description + actions; delete leaves assets in the library. `sx collection list` shows `writing (2 assets)`. |
| Use in my AI tools | Asset and collection installs delivered SKILL.md + metadata into the sandboxed `~/.claude/skills/` through the same client code as `sx install`; uninstall removed it. |
| Native app | `wails build` bundle launches and runs cleanly against the sandboxed config. |

## Bugs found by this loop and fixed

1. `SetupGitVault` persisted the config **before** validating the repo — a
   typo'd URL bricked the app into a broken library view. Now validates
   first.
2. Vault-layer errors (multi-line, full of CLI remediation like
   `--ssh-key`) reached the UI verbatim. Added `friendlyVaultError`.
3. The app binary didn't register any AI clients (the CLI does it via blank
   imports in `main`) — installs reported "no AI tools". Imports added.
4. Collection mutations require a real (non-synthetic) actor identity; a
   machine without `git config user.email` couldn't create collections.
   Onboarding now collects an email when no identity exists and stores it
   as the profile identity (same mechanism the CLI uses).
5. Legacy single-file `config.json` migration dropped the `identity` field
   (pre-existing CLI bug, exposed by the app's sandbox).

## Design pass (screenshots: onboarding, library, draft sheet, detail panel)

Checked against the spec's checklist: consistent 4/8px spacing rhythm, one
accent color + per-type badge hues, empty states that teach the next action,
loading skeletons, single-sentence errors, no "version/commit/scope/manifest"
vocabulary anywhere in primary UI (history entries are "revisions"), light
and dark tokens defined for every surface. Remaining polish noted for later:
keyboard focus rings are browser-default, and dark mode has only been
inspected via tokens, not screenshotted.

## Needs human eyes/hands (cannot be automated here)

- Finder drag-and-drop onto the native window (the drop *handler* is fully
  tested via the same binding the drop event calls; the native event wiring
  is Wails' `EnableFileDrop`).
- Native window chrome/traffic-light inset on macOS, and the screenshot of
  the native window (screen-recording permission not available to the
  automation environment).
- Windows and Linux builds (CI matrix exists in
  `.github/workflows/app-release.yml`; not yet exercised).

## Known gaps (deliberate, spec-aligned)

- skills.new sign-in from the app (onboarding points to `sx init`; the app
  picks up the resulting config).
- Sleuth-vault collections (feature-detected; UI hides collections there).
- Artifacts are unsigned pending Apple Developer ID / Windows cert.
- Dedup detection and self-improving loops are v1.1/v2 per the spec.

## Round 3: research-driven layout redesign (2026-07-03)

Research: Apple HIG sidebars, NN/g (cards-vs-lists, sort order, empty
states, drag-drop, onboarding), VS Code/App Store install patterns. Changes,
each verified via Playwright against a 30-asset fixture vault:

- Source-list sidebar: LIBRARY (All / per-type / In your AI tools / Drafts,
  all with counts), COLLECTIONS with inline "+ new" and a create-your-first
  empty prompt, footer showing detected AI tools + Settings.
- Dense list view (default) with grid toggle, both persisted; default sort
  "Recently updated" with Name option; Cmd+F or "/" focuses search.
- Install UX: App Store-style state machine — "Use in my AI tools" →
  Installing… → "✓ In your AI tools ▾" with Update/Remove menu; installed ✓
  on rows; "In your AI tools" sidebar scope; one-time plain-language hint
  ("nothing leaves your computer"); sidebar footer answers what "my AI
  tools" means. App installs/uninstalls now update the shared CLI tracker
  (verified count 0 → 1 → 0 through the full cycle), so the app and
  `sx install --repair` agree.
