# Skill Doctor — duplicate detection & consolidation

Status: **Draft for review** (2026-07-09). Builds on the app extension system
(docs/app-plugins-spec.md) and the scoped-install machinery (docs/scoping.md,
SK-623). Original intent: the app-plugins spec named "v1.1 dedup groundwork"
tied to Related Assets as post-v1 work — this is that feature, expanded from
*detection* to *detection + guided consolidation*.

## Motivation

Teams accumulate near-duplicate skills: the same "review a PR" or "write a
migration" guidance copied between repos, forked and lightly edited, or
independently reinvented. Duplicates dilute search, confuse agents about which
to load, and rot in parallel. The library is a distribution system but has no
tool to *converge* what's drifted.

**Skill Doctor** finds likely-duplicate skills and offers two resolutions:

1. **Consolidate** — pick one canonical skill, migrate every other's installs
   onto it, and retire the rest (recoverably).
2. **Merge** — have an LLM compose a new skill that keeps the essence of each
   source, land it as a **draft** for review, and on publish migrate the
   sources' installs onto it and retire them.

It ships as a **built-in extension** under the sidebar **TOOLS** section.

## Goals / non-goals

**Goals**
- Surface genuine duplicate *clusters* (not just exact copies), with a
  confidence signal the user can trust and filter.
- Make the destructive step safe: human-reviewed, reversible, audited.
- Never silently lose content in a merge — show exactly what was kept,
  dropped, or in conflict per source.
- Reuse the install-migration primitives from SK-623; add the smallest new
  API surface that the job needs.

**Non-goals (v1)**
- Cross-vault dedup. One library at a time.
- Auto-merge without review. Every destructive action is gated.
- Bundling an embedding model. Detection stays explainable TF-IDF +
  optional LLM adjudication, consistent with Related Assets' stated stance.
- Deduping non-skill asset types (rules/commands/agents). The pipeline
  generalizes, but v1 targets skills, where the problem is worst.

## Prior art we're building on and against

Researched 2026-07-09 (Pulse's own dedup + external products/papers).

**Pulse already has a "smart merge"** (`sleuth/apps/skills/service/import_flow/`)
— worth learning from, explicitly *not* worth copying wholesale:
- It detects duplicates by **exact SHA-256 of normalized content** only —
  misses everything but byte-identical copies. We add similarity + LLM
  adjudication.
- Its merge prompt embeds variant boundaries (`=== Variant N ===`) with
  **unescaped** asset names/paths — a prompt-injection seam. We use
  structured input and escape/segment safely.
- **No confidence, no undo, raw-text output parsing, and a known
  silent-omission risk** (GPT-4-class models stay faithful but cover <40%
  of diverse multi-doc info — DiverseSumm). We add a coverage ledger,
  two-tier confidence, and reversibility.
- Critically, Pulse's flow *creates a new managed asset* from import
  candidates; it **does not consolidate existing library skills or migrate
  their installs**. Ours does — that install migration is the headline.

**External lessons adopted** (CRM merge UIs, Apple/Google photo & contact
dedupe, git merge tools, LLM-KB-merge writeups, dataset-dedup papers):
- Two modes with the **less-destructive default** ("keep one, retire the
  rest" is the safe path password managers and photo apps converge on;
  smart-merge is the power path).
- **Reference/install migration is the feature** — after consolidation every
  install resolves to the survivor; nothing dangles.
- **Reversibility** (soft-delete + restore + un-merge) is the single biggest
  reducer of false-merge anxiety and lets the action itself be low-ceremony.
- **Coverage ledger** as first-class UI — machine-checkable kept/dropped/
  redundant/conflict per source, with one-click *re-include* for a
  dropped-unique unit.
- **Confidence as a filterable signal**, and an **identical-vs-similar
  split** — only auto-suggest "identical"; gate "similar" behind review.
- **Result-as-hero pane** with on-demand per-source provenance; a distinct
  color for AI-composed regions that have no single source.
- **Decompose-first merge** (extract atoms → cluster → single-pass compose →
  verify) beats refine-chaining, which suffers last-source recency bias.
- **Explicit apply gate** — never auto-apply an AI edit (the Cursor lesson).

## Detection pipeline

Runs client-side over the library (skills only in v1). Four stages, each
cheap-to-expensive, so the LLM only ever sees genuine candidates.

**Stage 0 — Normalize** (highest-leverage step). For each skill: split off YAML
frontmatter (compared as structured data, not prose), strip markdown syntax
(headings, list markers, fences, link URLs), lowercase, collapse whitespace.

**Stage 1 — Cheap similarity.**
- **Exact:** SHA-256 of normalized body → identical clusters (confidence:
  certain).
- **Near:** TF-IDF cosine over normalized bodies, all-pairs (the corpus is
  hundreds–low-thousands of docs, well under the ~10k where LSH/blocking
  earns its keep; all-pairs is milliseconds). Reuses the Related Assets
  approach and/or the core `SearchAssetContent` markdown cache. Pair is a
  **candidate at cosine ≥ 0.82**; treat **≥ 0.95 as high-confidence**,
  **0.82–0.95 as review**. Add an **overlap/containment coefficient**
  alongside cosine so a short skill wholly contained in a long one isn't
  missed by length asymmetry.

**Stage 2 — Cluster.** Connected components over the candidate-pair graph (or
agglomerative on the cosine matrix, cutoff 0.10). A cluster is 2+ skills.

**Stage 3 — LLM adjudication (optional, only if a model is configured).** Per
cluster, one structured call: *"are these the same skill, and how confident?"*
returning `{ same: bool, confidence: 0–1, rationale, distinctValue: per-skill
one-liner }`. This demotes false positives (two different `deploy` skills that
share vocabulary) and is what earns the "these really are dupes, here's why"
explanation. Without a model, Skill Doctor still works on similarity alone,
labeled as such.

Detection is cached in the extension's local storage keyed by each skill's
`updatedAt`, so a re-scan only re-embeds/re-adjudicates what changed
(incremental, like the analytics extensions).

## Action 1 — Consolidate into a canonical skill

The safe default. User picks one skill in the cluster as canonical; the rest
are sources to retire.

1. **Union the installs.** For each source, read its install targets
   (`GetAssetInstallations`), union them, and append to the canonical via the
   bulk append-mode writer (`SetAssetInstallations(..., appendMode=true)` —
   additive on every backend, org-exclusive still replaces). This is the
   exact SK-623 machinery; every teammate/repo/team/bot that had a source now
   receives the canonical.
2. **Retire the sources — recoverably.** Remove each source from the manifest
   with **`RemoveAsset(name, "", delete=false)`**: it drops the manifest entry
   and clears installs but **keeps the `.sx/versions/<name>/` archive on
   disk**, so `assetFromStorage`-style recovery (and git history on git
   vaults) can un-retire it. This is a genuine soft-delete, already supported
   by the vault primitives — no new deletion semantics needed.
3. **Audit + confirm.** One confirm sheet naming exactly what will re-point
   ("3 skills retired, 11 installs re-pointed to `code-review`") and a new
   `plugin.consolidated` audit event (data: canonical, sources[], migrated
   install count).

RBAC is the vault's existing gate: retiring a team-scoped skill needs
team-membership; org-scoped needs org-admin — surfaced as the bulk API's
skipped-target reasons, not a silent failure.

## Action 2 — Merge into a new skill

The power path. An LLM composes a new skill preserving each source's essence.

**Pipeline (decompose-first, closed-corpus, single-pass):**
1. **Extract** atomic units (instructions, examples, constraints) from each
   source in isolation → JSON with a stable `sourceId` + span. Frontmatter
   parsed separately as data. Sources are passed as *separately delimited,
   escaped* fields — never string-concatenated with in-band markers (fixes
   Pulse's injection seam).
2. **Cluster & flag** units as `unique` / `shared` / `conflicting`. Conflicts
   are **surfaced, never auto-resolved**.
3. **Compose** the merged skill in one pass over the clustered pool (not a
   refine chain — refine lets the last source dominate). Counterbalance
   source order to fight lost-in-the-middle.
4. **Verify** — classify every source unit as **kept / dropped / redundant /
   conflicting**; a `unique` unit that came back `dropped` triggers one
   re-compose. Scrutinize the tail of the output hardest (omissions cluster
   at the end).

**Output is structured**: `{ merged: {frontmatter, body}, ledger: [{ sourceId,
unit, disposition }], conflicts: [...] }`. Never raw-text-parsed.

**Landing it:** the merged skill is created as a **draft** (`drafts.create`) —
nothing destructive yet. The user reviews it in the merge UI (below), edits
freely, and **publishes** through the normal human publish path. Only *after*
publish does Skill Doctor offer to run Action 1's consolidation (migrate the
sources' installs onto the new skill, soft-retire the sources). Merge and
retire are two deliberate steps, not one.

## Merge review UI

The screen that makes a merge trustworthy. Inverts git-merge emphasis for
n>3 sources:

- **Result is the hero pane**, full-width and editable — the draft body.
- **Provenance, on demand.** Each region carries a color-coded gutter stripe
  by source; regions the LLM synthesized from multiple/none get a distinct
  **"AI-composed"** color, flagged for extra scrutiny. Click a region to
  reveal just that region's source snippets — no permanent n-pane wall.
- **Coverage ledger panel.** Per source: kept / dropped / redundant /
  conflicting counts, expandable to the units. A **"Dropped / not included"**
  list makes omission *loud*; each dropped-unique unit has a one-click
  **re-include** that re-composes with it pinned.
- **Conflicts panel.** Each conflict shows both sides with source badges; the
  user picks or edits — nothing auto-resolved.
- **Structural diff.** Merge and diff at heading/section/list-item
  granularity (markdown has structure); treat independent sections as
  commutative (both-added → keep both), run move-detection so LLM reordering
  isn't an unreviewable wall, highlight deltas at word level.
- **Explicit apply gate.** The draft is only written on the user's action;
  publish is the normal human step. Per-section accept/reject with a
  batch-accept.

## LLM access — the `sx.llm` core service (built first)

Skill Doctor is built on a shared, core-provided LLM service rather than
rolling its own BYO-key calls. This is a **prerequisite phase**, speced
in its own doc; Skill Doctor is its first consumer and Claude Assist
migrates onto it. It exists in core — not as a per-extension capability —
because only core can shell out to a CLI and reach `localhost`, which a
sandboxed extension cannot.

**Surface** (a new gated capability, `llm:use`):
`sx.llm.complete({ messages, schema?, model? })` → structured output when a
schema is given, plus token/cost from the response. One implementation,
one key store, one provider picker, one cost meter — shared by every
extension instead of each re-rolling the Claude Assist pattern.

**Providers, in preference order:**
1. **Installed CLI (use my subscription/config).** Detect `claude`, `codex`,
   and `gemini` on the host and run them headless (`claude --bare -p … --output-format json`,
   `codex exec --output-schema …`, `gemini -p … --output-format json`). Go
   does the detection with the GUI-PATH probing every Wails app needs
   (`exec.LookPath` won't see a login shell's PATH — probe `/opt/homebrew/bin`,
   `~/.local/bin`, npm-global, `$SHELL -lc 'command -v …'`), and *installed ≠
   authenticated*, so a detected CLI is still verified before it's offered.
2. **Local Ollama** — detect via `GET /api/tags`, infer via `/api/generate`.
   Free, offline, zero ToS risk. (Only reachable from core: `sx.net.fetch`
   is https-only and forbids ports, so `localhost:11434` is out for
   extensions — another reason this lives in core.)
3. **Bring-your-own API key** — key in the OS keychain, hosted HTTPS APIs
   (Anthropic / OpenAI / Gemini). The sanctioned default.

**ToS note (drives the default).** Anthropic's terms forbid a third-party
*product* routing through a user's Claude subscription/OAuth. So
BYO-key (Commercial Terms) is the sanctioned path; **CLI-subscription reuse
is offered only as a clearly-labeled, user-initiated convenience**, and
Ollama / OpenAI / Gemini carry their own terms. Centralizing this in
`sx.llm` keeps the one policy in one place rather than in every extension.

Secrets stay in the Go process (keychain via `zalando/go-keyring`,
encrypted-file fallback on headless Linux), never the webview. Skill
Doctor calls only `sx.llm.complete`; it never sees a key or a CLI path.

## New API surface

Skill Doctor needs four new capabilities, all available to third-party
extensions (not built-in-only). The destructive one is gated behind a loud
dangerous-permission consent and the vault's existing per-scope RBAC —
which, with reversibility, is what bounds the risk of opening it broadly.

| Capability | Shape | Permission | Notes |
|---|---|---|---|
| LLM inference | `sx.llm.complete({ messages, schema?, model? })` | new `llm:use` | The shared core service (above): CLI / Ollama / BYO-key behind one call. Built as a prerequisite phase. |
| Read an asset's installs | `sx.assets.installations(name)` → `AssetInstallation[]` | `assets:read` | Thin exposure of the existing `GetAssetInstallations`. |
| Consolidate | `sx.assets.consolidate({ into, from: string[] })` | new `assets:consolidate` (dangerous; loud consent) | ONE backend primitive: unions installs onto `into` (append-mode), soft-retires each `from` (`RemoveAsset delete=false`), emits `plugin.consolidated`. Atomic-ish + audited beats exposing raw delete. RBAC = the vault's existing per-scope gate; denied targets surface as skipped-with-reason. |
| Restore a retired skill | `sx.assets.restore(name)` | `assets:consolidate` | Un-retire from the kept `.sx/versions` archive (the un-merge / undo). |

`assets:consolidate` reads as loudly in the consent sheet as `secrets` or
`net:<host>` do — it can retire skills library-wide. It is open to any
extension that declares it, but every action still passes the vault RBAC
gate and is reversible via `restore`, so a rogue or buggy extension can't do
anything an org-admin couldn't undo. Deliberately **not** exposing a raw
`sx.assets.delete` — consolidate is the only retirement path, so retirement
always migrates installs first and always keeps the archive.

## Safety & reversibility

- **Merge never deletes** — it creates a draft. Retirement is a separate,
  post-publish, confirmed step.
- **Soft-delete** keeps the version archive; `sx.assets.restore` un-retires.
  On git vaults, history is a second safety net.
- **Audit**: `plugin.consolidated` (canonical, sources, install count) and the
  existing `install.set`/`install.removed` from the migration.
- **No auto-resolve** of conflicts, **no auto-apply** of AI edits, **no
  auto-merge** of merely-similar (non-identical) clusters.
- **Confidence gating**: identical clusters can be one-click consolidated;
  similar clusters require opening the review.

## Placement & packaging

- **Built-in extension** `skill-doctor`, bundled in the app binary and
  registered in `plugins/boot.ts` alongside Publish Doctor et al.
- **Sidebar TOOLS panel** as the home: scan status + cluster list with
  confidence chips and an identical/similar filter. This is the "underneath
  tools" surface the request calls for.
- The **merge review** opens as a focused full-page view (a `views:main`
  route the panel navigates to) — it's a workflow, not a glance.
- Manifest permissions: `assets:read`, `assets:write-metadata`,
  `drafts:write`, `assets:consolidate`, `llm:use`, `views:sidebar`,
  `views:main`, and `storage:shared` (required — dismissals are
  team-shared: see below). The LLM provider hosts/keys are the core
  service's concern, so Skill Doctor declares no `net:<host>`.
- **Dismissals are team-shared.** When someone marks a cluster "not a
  duplicate," that decision is written to the extension's
  `storage:shared` document (`.sx/app-plugins/skill-doctor.json`) so it
  syncs to the whole library — the team triages each false positive once
  rather than every member re-dismissing it. Keyed by a stable
  cluster signature (sorted asset names + normalized-content hashes) so a
  dismissal survives re-scans until the members' content actually changes.
- SxAPI bump to **1.9.0** (installations read + consolidate + restore +
  `llm:use`), with the `sx.llm` service landing in its own prerequisite
  release.

## Phasing

1. **`sx.llm` core service (prerequisite).** The shared LLM capability:
   installed-CLI detection (with Wails PATH probing + auth verification),
   Ollama, BYO-key; one key store, provider picker, cost meter, structured
   output. Claude Assist migrates onto it as the proof consumer. Speced in
   its own doc; Skill Doctor depends on it.
2. **Detection, read-only.** Normalize + TF-IDF + cluster + the TOOLS panel
   showing clusters with confidence and an identical/similar filter.
   `sx.assets.installations` read API, and team-shared dismissals via
   `storage:shared`. Optional `sx.llm` adjudication demotes false positives
   once phase 1 exists. Immediately useful as a "where are my dupes" report.
3. **Consolidate.** `sx.assets.consolidate` + `restore`, the confirm sheet,
   the `plugin.consolidated` audit event. The safe default path end-to-end.
4. **Merge.** The decompose-first merge pipeline over `sx.llm`, the merge
   review UI with coverage ledger. Draft-then-publish-then-consolidate.

## Testing

- Go: `consolidate` migrates the union of installs and soft-retires
  (archive present, manifest gone, restore round-trips); RBAC denials
  surface; `plugin.consolidated` emitted.
- Detection: fixture library with known identical / near / unrelated skills
  asserts cluster membership and confidence tiers; normalization edge cases
  (frontmatter reorder, whitespace, fence differences).
- Merge pipeline: prompt-injection fixtures (adversarial skill names/bodies)
  can't break source segmentation; the verify stage catches a
  deliberately-dropped unique unit; structured output is schema-validated.
- Frontend: coverage-ledger re-include re-composes; apply gate blocks until
  the user acts.

## Decisions (ratified 2026-07-09)

1. **`sx.llm` core service is built FIRST.** Skill Doctor launches on top of a
   shared LLM service with installed-CLI and Ollama detection, not a
   BYO-key-only v1. (Chosen over the faster BYO-key-first option — the
   "use my installed CLI / local model" experience is the point, and it can
   only live in core.) See LLM access, below.
2. **`assets:consolidate` is open to third-party extensions from day one** —
   behind a loud dangerous-permission consent and the vault's existing
   per-scope RBAC, not built-in-only. (Chosen over built-in-first; the
   capability is broadly useful and the RBAC gate + reversibility bound the
   risk.)
3. **v1 is skills-only.** The pipeline generalizes to rules/commands later.
4. **Dismissals are team-shared** via `storage:shared`: marking a cluster
   "not a duplicate" silences it for everyone in the library, so the team
   triages each false positive once. (Chosen over local-only.)
