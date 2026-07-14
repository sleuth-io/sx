# SX App Plugin System Spec

Status: **Approved 2026-07-07** (with the amendments below folded in: strict
UI separation of extensions from AI assets, a first-class Extensions screen,
and a skills.new backend phase).
Based on research (2026-07-04/05): Obsidian plugin architecture deep-dive, top-plugin download analysis, and an extension-readiness audit of the sx desktop app.

> Naming note: this is distinct from [docs/plugins-spec.md](plugins-spec.md), which covers exposing vaults as Claude Code/Codex plugin *marketplaces*, and from the `claude-code-plugin` asset type. This spec is about extending the **sx desktop app itself**. In UI copy we call these **extensions** to avoid the collision; the asset type is `app-plugin`.

---

## Motivation

The desktop app is a fixed-function surface today: every view, command, and behavior is compiled in. An extension point lets us ship optional functionality (dashboards, linters, importers, AI assists) without bloating the core app, lets teams tailor the app to their workflows, and — eventually — lets third parties build on sx the way they build on Obsidian.

Obsidian is the proof that this shape works: a proprietary core app with a small plugin API produced 5,300+ community plugins and is the primary reason users stay. We cannot run Obsidian's plugins (they require Node/Electron and an ~1,000-member API we'd have to reimplement — investigated and rejected 2026-07-04), but we can build our own system that copies what worked and fixes what didn't.

**What Obsidian got right** (we copy):
- Trivially simple plugin anatomy: a manifest + one bundled JS file + optional CSS.
- Explicit lifecycle (`onload`/`onunload`), per-plugin settings storage.
- Core plugins first: the API was validated by Obsidian's own built-ins before third parties got it.

**What Obsidian got wrong** (we design against):
- **No capability boundary**: plugins run with full Node/Electron privileges; security rests entirely on human review of 5,300 repos.
- **Leaked internals**: so many plugins depend on undocumented objects and monkey-patching that the app can barely refactor.
- **No org story**: enterprises cannot control which plugins employees run.

**Our structural advantage**: the hardest part of a plugin ecosystem is distribution, and sx *is* a distribution system. A plugin is just another asset type — versioned, team-scoped, audited, admin-gated. Obsidian had to build a registry; we already have one.

## Design principles

1. **Plugins are sx assets.** Distribution, versioning, scoping, and audit come from the existing vault machinery. No new registry, no new delivery mechanism.
2. **Capability-gated API, never raw access.** Plugins talk to a versioned `sx` API object, not the Wails bridge. Every capability is declared in the manifest and enforced by a proxy. The webview has no filesystem or Node, so the API surface is the *intended* blast radius — fully true only once the P4 isolation gate lands (see resolved question 3: in P1–P3 built-ins run in the main webview context, where the proxy is a discipline boundary, not a security one).
3. **Small, versioned, documented API — internals never exposed.** If a plugin needs something the API doesn't offer, the answer is an API addition, not an escape hatch.
4. **Built-ins validate the API before it becomes a contract.** No third-party plugins until our own built-ins have exercised every extension slot through at least one release cycle.

## Plugin anatomy

A plugin is a directory (packaged/published like any asset):

```
{plugin-name}/
  plugin.json          # manifest (required)
  main.js              # single bundled ES module (required)
  styles.css           # optional, injected while enabled
  metadata.toml        # standard sx asset metadata (type = "app-plugin")
```

`plugin.json`:

```json
{
  "id": "library-dashboard",
  "name": "Library Dashboard",
  "version": "1.0.0",
  "minAppVersion": "1.2.0",
  "description": "Saved queries and adoption dashboards over your library.",
  "author": "acme.com",
  "permissions": ["assets:read", "usage:read", "views:sidebar"]
}
```

- `id`: lowercase + hyphens, unique within the vault, may not be `sx` or start with `sx-`.
- `version`: semver; independent of `minAppVersion`, which gates load.
- `permissions`: exhaustive list of capabilities the plugin may use (see below). Undeclared calls throw.

`main.js` is an ES module whose default export implements:

```ts
export default class implements SxPlugin {
  onload(sx: SxAPI): void | Promise<void>   // register everything here
  onunload(): void                          // must leave no trace
}
```

Everything registered through `sx.*` is tracked by the host and torn down automatically on unload/disable — plugins cannot leak registrations (a lesson from Obsidian's `registerEvent` pattern, made mandatory rather than conventional).

## Runtime architecture

**Loading.** The plugin host (frontend) fetches enabled plugins' `main.js` through a new bridge method and loads each via dynamic `import()` of a Blob URL. No `eval`, no remote script tags; the only code source is vault-installed assets. Verified in WKWebView (P1 spike, dev + production builds); WebView2 and WebKitGTK verify themselves through the loader preflight (see Risks).

**The `sx` API object.** Each plugin receives its own proxy instance, permission-filtered at construction. API v1 surface, grouped by permission:

| Permission | Capabilities |
|---|---|
| `assets:read` | List assets/collections, read asset files and metadata, read version history. Read-only. |
| `usage:read` | Read usage and audit event streams (the `.sx/usage/`, `.sx/audit/` JSONL data, via Go), per-user stats, and team names/membership (`sx.teams.list()`, 1.2.0). |
| `drafts:write` | Create/update drafts (never direct publishes — publish stays a human action in core UI). |
| `views:sidebar` | Register a sidebar panel (icon + React-free mount point: plugin gets a DOM element + lifecycle). |
| `views:asset-tab` | Register a tab in the asset detail pane (receives the asset context). |
| `views:dashboard` | Register a widget on a (new) dashboard/home surface. |
| `views:main` | Register a full-page view listed in the sidebar (1.3.0). |
| `assets:write-metadata` | Publish metadata-only revisions of assets: description, keywords, owner, status (1.3.0). |
| `commands` | Register commands in the command palette (new core feature, see below) with optional shortcuts. |
| `editor` | Character-offset text operations on the draft the user has open (`sx.editor`, 1.2.0): read value/cursor/selection, replace selection/range. Throws when no editor is open. |
| `events` | Subscribe to lifecycle events: `draft-saved`, `before-publish` (may return warnings shown in the publish sheet — the doctor hook), `asset-published`, `asset-installed`, `vault-synced`. |
| `net:<host>` | Host-scoped network egress via `sx.net.fetch` (1.4.0): one grant per exact hostname (no wildcards/schemes/ports in the grant), https-only, redirects refused, real streaming `Response`. The consent sheet renders one line per declared host. |
| `secrets` | Named per-extension secrets in the OS keyring via `sx.secrets.get/set` (1.4.0), keyed `<profile>/<extension-id>/<name>`; 0600-file fallback on headless machines. For API keys that must never land in plugin data files or the vault. |
| `storage:shared` | One team-shared JSON document per extension via `sx.sharedStorage.load/save` (1.5.0), stored in the vault at `.sx/app-plugins/<id>.json` — syncs to everyone; commits on git vaults; 256 KB cap, whole-document last-writer-wins. |
| `views:collection` | Register a tab on the Library's collection view (`registerCollectionView`, 1.6.0). The first tab stays the built-in asset list; the mount receives the collection name. No registrations means no tab row — the default view is untouched. |
| `export` | Export a collection's member assets as one file via `sx.collections.export` (1.6.0): a plain zip (every asset), or a Claude Code / Codex / Gemini plugin bundle (skill assets only). Saved through a native dialog; resolves "" on cancel. |
| `views:team` | Register a tab on the Library's team view (`registerTeamView`, 1.7.0). Same contract as collection views; the mount receives the team name. |
| `views:repo` | Register a tab on the Library's repository view (`registerRepoView`, 1.7.0). Same contract; the mount receives the repository URL. `sx.repos.list()` (under `assets:read`) maps repo URL → asset names scoped there. |
| (always) | `sx.ui` kit — modal, notice/toast, confirm, settings panel schema, plus `openView` into the extension's own main views (1.4.0, gated on `views:main`); `sx.storage` — `loadData()`/`saveData()` per plugin per profile (stored app-side, not in the vault; 10 MB cap — enough for an incremental event cache); `sx.app.version`, `sx.api.version`. |

**Explicitly excluded from API v1** (deferred, revisit after P6 planning):
- CodeMirror *extension* exposure — exporting our exact CM6 package instances and freezing them; Obsidian's biggest coupling trap. Defer. (Distinct from the scoped `sx.editor` text facade shipped in 1.2.0, which exposes offsets and strings, never CM objects.)
- Custom asset-type renderers / new asset types from plugins.
- Any Go-side extensibility (scripting engines, `plugin` package). Go extension points (clients, vault backends) remain compile-time.
- Theming beyond `styles.css` injection.

**Host-side prerequisites in the app** (net-new core work this spec drives):
- A **slot registry** in the frontend (sidebar panels, asset-detail tabs, dashboard widgets) — the app currently has zero component pluggability; built-in features migrate onto the same slots.
- A **command palette** (cmd-K) — needed by `commands`, and a good core feature regardless.
- A **publish pipeline hook point** so `before-publish` subscribers can contribute warnings to the confirm sheet.

## Permissions, trust, and org control

- **Enable-time consent.** Enabling a plugin shows its permission list in plain language ("Can read your library and usage data. Can add panels to the sidebar."). Permissions changing between versions re-prompts.
- **Vault policy (admin gating).** New manifest table, respected by the app:

```toml
[app-plugins]
mode = "allowlist"        # "open" (default) | "allowlist" | "disabled"
allowed = ["acme-metrics", "team-linter"]
```

Only org admins can modify it (same RBAC as scope changes, see docs/rbac.md). The allowlist governs **third-party (vault-installed) extensions only** — built-ins ship with the app and stay available, so vetting external code never silently disables the Publish Doctor safety net. `disabled` is the total switch: everything extension-shaped turns off, built-ins included, and the extensions UI is hidden. Policy changes append audit events.
- **Audit.** New audit event types: `plugin.enabled`, `plugin.disabled`, `plugin.policy-changed` (in `internal/mgmt/audit.go`).
- **No re-review gap** (Obsidian's weakness): plugin updates arrive through normal asset versioning, so pinning, history, restore, and audit all apply. A team can pin a plugin version exactly like any asset.

## Distribution

- New asset type `app-plugin` (`internal/asset/types.go`), with `[app-plugin]` section in `metadata.toml` (entry file, permissions mirror for indexing).
- Published, scoped, and installed with the existing machinery. "Installing" an app-plugin scoped to you makes it appear in the app's Extensions screen; **enabling** it is a separate, per-user, consent-gated action.
- **Strict UI separation.** Extensions ride the asset pipeline but never
  mingle with AI assets in the UI: `app-plugin` assets are filtered out of
  the Library list, search, collections, drag targets, and every AI-client
  install path (`internal/clients` already excludes them). They surface in
  exactly one place — the Extensions screen. To a user there is a library
  of AI assets and there is an Extensions screen; that both ride the same
  pipes is invisible plumbing. (Precedent: hooks and MCP configs are
  assets too and are already presented by shape, not storage.)
- Built-in plugins ship inside the app binary (vendored at build time) but run through the exact same host, API, and permission path as vault-installed ones. They are the API's permanent conformance suite.

### The marketplace

A marketplace is **just another sx vault full of `app-plugin` assets** —
no registry service, no new format. The app browses it read-only and
"install" republishes the chosen asset into the user's own vault through
the same validated path as *Add extension…*, so a marketplace install and
a hand-published extension are indistinguishable afterwards: same policy
check, same consent model, same audit trail. Install **enables the
extension for the installing user** — the permission chips on the
marketplace card are the consent surface, so a separate consent sheet
would ask the same question twice (org policy still has the final word).
Anyone else who receives it sees it disabled and walks the normal consent
sheet when they enable it.

**Install is scoped.** The Install button installs **just for the caller**
(a `kind = "user"` scope on the asset — the same row `sx add --user me`
writes), which docs/rbac.md allows for anyone, governed vault or not. The
button's menu offers **Install for everyone** (library-wide, no scopes),
shown only when the caller may set an org scope (`CanInstallForEveryone`:
ungoverned vault, or org-admin). The Extensions screen lists only
extensions that **reach** the caller — library-wide, via a team, or via
their own user scope — with a chip naming the audience ("Just you",
"Team platform", …); someone else's personal install doesn't appear.
Re-installing an extension the vault already has is an update and leaves
its sharing untouched. A personal install can later be promoted with
**Share with library** (org-scope RBAC applies); the marketplace badge
distinguishes "✓ Installed for you" from "✓ In library".

**Remove matches intent.** Removing a personally-installed extension
drops only the caller's user scope — as a full asset delete when no one
else receives it (a bare last-scope removal would otherwise read as
global). Removing a shared one deletes it for everyone, behind a confirm
that says so. Lifecycle is audited (`plugin.installed` / `updated` /
`uninstalled` / `shared`, see docs/audit.md) and every install records a
usage event (`asset_type = "app-plugin"`), so `sx stats` sees extension
adoption.

- Default repository: `https://github.com/sleuth-io/sx-extensions`
  (`DefaultMarketplaceURL`); per-profile override stored in
  `app-plugins/<profile>/marketplace.json` so teams can host their own
  (a git URL or a local/synced folder both work — the URL opens as a git
  or path vault respectively).
- Bridge surface: `SearchMarketplace(query)` (type-filtered list with
  each entry's `plugin.json` fields, installed flag, and global install
  count), `InstallMarketplaceExtension(assetName, scope)` with scope
  `"me"` or `"org"`, `CanInstallForEveryone()`, `RemoveExtensionAsset(id)`,
  `ShareExtensionWithLibrary(id)`, `Get/SetMarketplaceURL`.
- UI: **Settings → Extensions → Browse marketplace…** — search, permission
  chips per entry, split Install button (for me / for everyone), Update /
  "✓ Installed for you" / "✓ In library" states, install-count badge and
  a "Most installed" sort, editable source URL.
- **Catalog and stats files.** A marketplace may publish two root files,
  both read from the already-synced clone (`ReadRootFiles` on file-backed
  vaults — one batch read, one clone sync): `catalog.json` — the browse list, one file read instead of
  unpacking every bundle — and `stats.json` — global install counts per
  extension id. Marketplaces without them fall back to bundle scanning
  and show no counts.
- **Countable installs (public marketplace).** CI on sx-extensions moves
  each published version archive out of the tree onto a rolling GitHub
  release as `<name>-<version>.zip`, rewriting its manifest entry to
  `[assets.source-http]` (URL + sha256 + size). Installs then fetch the
  release asset through the vault's existing HTTP source handler —
  hash-verified against the git-committed manifest — and GitHub's
  per-asset `download_count` becomes the anonymous install counter (the
  Obsidian model). A nightly job aggregates the counts into `stats.json`.
  Browsing never touches those URLs: the catalog serves the list and
  `assets/<name>/` stays in-tree, so counts reflect installs, not
  browses.
- **Per-library, visibly.** Extensions belong to a library, so the
  Extensions screen names the library it's showing and re-syncs the
  whole set — vault plugins, policy, enablement — on every library
  switch (`syncVaultExtensions`). Backends that can't store
  `app-plugin` assets yet (skills.new servers predating the P5
  backend; the app probes the policy query once per profile) are gated:
  `VaultSupportsExtensions` disables install/add paths with a plain
  explanation instead of surfacing the server's type-validation error.
- **Updates.** An update is the install path again: republish the
  marketplace's latest into the vault, reload the plugin. Availability
  is a client-side version comparison (installed manifests vs the
  marketplace catalog, matched by plugin id). The Extensions list shows
  each extension's version, a per-row "Update to vX" button, and an
  "Update all (N)" header button. A running extension comes back up on
  the new code immediately — unless the update changed its permission
  set, in which case it stages disabled and the consent sheet re-prompts
  (queued, one at a time, for Update all).
- The launch marketplace content is seven extensions mapped from the most
  popular relevant Obsidian plugins (research 2026-07-06): Asset Query
  (Dataview), Related Assets (Smart Connections, via TF-IDF cosine —
  exact and explainable at library scale, no embedding model needed),
  Recent Assets (Recent Files), Activity Heatmap (Heatmap Calendar),
  Library Stats (Vault Statistics), Smart Templates (Templater/QuickAdd —
  declarative placeholders only, no code execution), Style Linter
  (Linter — report-only: publish-sheet warnings plus a per-asset Style
  tab). An eighth, Library Search (Omnisearch), was retired the same
  week: ranked full-text content search belongs in the app's MAIN search
  box, not a parallel sidebar search — `SearchAssetContent` in core now
  does it (per-revision markdown cache, AND semantics, heading-weighted,
  excerpt highlighting in result rows).

### API 1.12.0 additions (quality storage)

- `quality` permission + `sx.quality.get/add/latest/reevaluate`:
  per-asset quality evaluations (overall 0–100, category scores,
  summary, insights) unified across vault types —
  `.sx/quality/<asset>.json` on file vaults, the server's own
  evaluation document on skills.new (read-only there; `reevaluate`
  fires the server's evaluator and `get` polls its `evaluating`
  flag). Full contract in `docs/quality-spec.md`.

### API 1.9.0 additions (the sx.llm core service)

- `llm:use` permission + `sx.llm.complete({messages, schema?, model?,
  maxTokens?})`: one completion against whatever LLM provider **the
  user** configured in Settings → AI provider. Provider-agnostic by
  design — extensions never learn a vendor, an endpoint, or a key; they
  send provider-neutral messages and get `{text, json?, provider,
  model, usage}` back. `sx.llm.provider()` returns the configured
  provider id (`""` when unconfigured) so an extension can show a setup
  hint instead of a failing call.
- `sx.ui.openSettings(section?)` (always available): open the app's
  Settings on `"libraries"`, `"extensions"`, or `"ai"` — the deep link
  an llm:use extension shows when no provider is configured, instead of
  describing a menu path in prose.
- `MainViewSpec.section?: "library" | "tools"`: where the view's
  sidebar row lives. `"tools"` lists it under the collapsed TOOLS
  section — for utilities that act ON the library (dedupe, assistants)
  rather than views OF it. Default stays `"library"`.
- `sx.assets.installations(name)` (under `assets:read`): every install
  row on an asset, `everyone: true` when no rows exist.
- `assets:consolidate` permission + `sx.assets.consolidate({into,
  from[]})`: collapse a duplicate cluster onto one survivor. Every
  `from` asset's install rows move onto `into` first — reach never
  shrinks; an org-wide source makes the survivor org-wide — then the
  `from` assets are RETIRED: removed from the manifest and the
  browsable root view but keeping their version archive (new
  `RetireAsset` on file vaults; skills.new's non-delete removal has the
  same recoverable semantics). The one extension-reachable mutation
  that removes assets, so it is its own dangerous grant, RBAC-refused
  moves come back in `skipped`, and callers must confirm with the user
  first. No raw delete is exposed.
- **Why core, not `net:` + `secrets`:** the sandbox can't shell out or
  reach localhost, so only core can offer installed-CLI and Ollama
  providers; and one shared provider picker + key store beats every
  extension building its own (the Claude Assist pattern, generalized).
- **Providers** (`internal/llm`, user-selected in Settings, one for the
  whole app):
  1. *Installed CLIs* — `claude`, `codex`, or `gemini`, run headless
     with the CLI's own login. Detection probes PATH plus the usual GUI
     blind spots (`/opt/homebrew/bin`, `~/.local/bin`, npm-global…).
     User-initiated convenience: Anthropic's ToS bars third-party
     products from routing through a user's subscription, so this is
     never a default — the user explicitly picks it. **Hardening:**
     prompts are extension-authored and these CLIs are tool-running
     agents, so every invocation is pinned to its most restricted mode
     (`claude --tools ""` = no tools at all, `codex --sandbox
     read-only`, `gemini --approval-mode default` — headless has no
     approver) and runs in a fresh empty scratch dir. Residual: codex
     and gemini's restricted modes still allow reads, so choosing a CLI
     provider trusts your enabled `llm:use` extensions with read-level
     local access; the BYO-key and Ollama providers carry no local
     capabilities at all.
  2. *Ollama* — any local model; detected via `GET /api/tags`, custom
     server address supported, native JSON-schema constrained output.
  3. *Bring-your-own key* — Anthropic, Google, or any OpenAI-compatible
     endpoint (custom base URL covers OpenRouter, Groq, Mistral, vLLM,
     LM Studio, …). Keys go straight to the OS keyring (no file
     fallback) and never cross into the webview: there is a
     `LLMSetAPIKey` bridge but deliberately no getter.
- **Structured output:** pass `schema` (JSON Schema) and the reply is a
  validated JSON document (`result.json` pre-parsed). Ollama enforces
  it natively; other providers get a schema instruction plus tolerant
  extraction (fenced/prefixed replies recovered, invalid JSON rejected).
- **Attribution:** every completion is logged with the calling
  extension id and token usage — the hook for a future cost meter.

### API 1.8.0 additions (incremental usage/audit fetch)

- `sx.usage.eventsSince(iso)` and `sx.usage.auditEventsSince(iso)` (under
  the existing `usage:read`): the same reads as `events`/`auditEvents`,
  but bounded by a precise RFC3339 `since` (e.g. an event's own `timestamp`) instead of a day count. The
  Pulse `assetUsageEvents` field and sx's `UsageFilter` already carry
  `since`/`until`; this surfaces the precise lower bound to extensions so
  an analytics view can cache a window in `storage` and pull only the
  delta on each reload — a second load, even across restarts, transfers
  almost nothing. The server filter is `>=`, so the newest cached event
  repeats in the delta; dedupe on merge (usage events have no id — key on
  timestamp+actor+asset+version). Feature-detect (`typeof
  sx.usage.eventsSince === "function"`) so an extension still runs on
  apps predating 1.8.0, falling back to the windowed read. The `since` is clamped to no earlier than the day-count APIs' one-year cap, so it can only narrow the window, never force an unbounded scan.

### API 1.7.0 additions (team & repo views)

- `views:team` + `registerTeamView({id, title, mount})` and `views:repo`
  + `registerRepoView({id, title, mount})`: tabs on the Library's
  **team** and **repository** views (slot kinds `team-view` /
  `repo-view`), with the exact contract collection views established —
  first tab stays the built-in asset list, `mount(view, ctx)` receives
  `ctx.team` (the team name) or `ctx.repo` (the repository URL), zero
  registrations means zero UI change, and disabling an extension
  mid-view falls back to Assets.
- `sx.repos.list()` (under the existing `assets:read`): repository URL →
  asset names scoped to install there — the repo-centric read repo-view
  extensions build on, mirroring `sx.teams.list()` for teams.

### API 1.6.0 additions (collection views & export)

- `views:collection` permission + `registerCollectionView({id, title,
  mount})`: a tab on the Library's **collection** view (slot kind
  `collection-view`). When a collection scope is open and at least one
  view is registered, the content pane grows a small tab row — the same
  pattern as the asset detail's Content + extension tabs — whose first
  tab, **Assets**, is the built-in asset list; each registered view adds
  a tab. `mount(view, ctx)` receives `ctx.collection`, the collection's
  name. With zero registrations there is no tab row at all: the default
  collection experience is byte-identical. Mounts ride the standard
  tracking, so disabling the extension mid-view tears the tab down and
  falls back to Assets.
- `export` permission + `sx.collections.export(name, format)`: bundles a
  collection's member assets into a single file behind a **native save
  dialog** (suggested name `<collection>-<format>.zip`); resolves to the
  saved path, or `""` when the user cancels. Formats:
  - `"zip"` — one folder per member asset containing its files (all
    asset types).
  - `"claude-code"` — a Claude Code plugin directory, zipped:
    `.claude-plugin/plugin.json` (`{name, description, version:
    "1.0.0"}`) plus `skills/<asset>/…` per **skill** asset.
  - `"codex"` — the Codex analogue: `.codex-plugin/plugin.json`
    (`{name, version, description, skills: "./skills"}`) plus
    `skills/<asset>/…` per skill asset.
  - `"gemini"` — a Gemini extension: `gemini-extension.json` plus
    `commands/<asset>.toml` per skill asset, converted through the same
    skill→command translation `sx install` uses for Gemini
    (docs/plugins-spec.md).
  The plugin formats carry **skill assets only** in v1 (mirroring the
  derived-marketplace generator, internal/manifest/pluginexport.go);
  non-skill members are skipped, and a collection with no skills refuses
  those formats with a plain error.

### API 1.5.0 additions (wave-2, Review Rota)

- `storage:shared` permission + `sx.sharedStorage.load/save`: one
  team-shared JSON document per extension, stored in the vault at
  `.sx/app-plugins/<id>.json` (`AppPluginSharedStore`, git + path
  vaults). Syncing distributes it; on a git vault every save is a
  commit, so the API contract tells extensions to write on user
  actions. 256 KB cap, JSON-validated, whole-document last-writer-wins
  — rota state and shared settings, not a concurrent database. Its
  consent line says plainly that state is visible to everyone in the
  library.
- `sx.app.currentUser()`: the identity vault changes are attributed to
  ("" when unresolvable). No new permission — it's the extension
  learning who is running it, not reading other people's data; how
  team-shaped extensions mark entries "mine".

### API 1.4.0 additions (wave-2, Claude Assist)

- `secrets` permission + `sx.secrets.get(name)/set(name, value)`: named
  per-extension secrets in the **OS keyring** (macOS Keychain, Windows
  Credential Manager, Secret Service on Linux), keyed
  `<profile>/<extension-id>/<name>` so profiles and extensions never
  share entries. Headless machines without a keyring fall back to a
  0600 file in the plugin data dir (same trade-off as `sx cloud`'s
  relay token). Setting `""` deletes; a healthy keyring is never
  shadowed by a stale fallback copy. Secrets exist so API keys never
  land in `storage.saveData` files, which are plain JSON on disk.
- `net:<host>` permission family + `sx.net.fetch(url, init?)`: the only
  network egress extensions have. Each declared host is its own grant
  (exact hostname match — no wildcards, schemes, ports, or paths in the
  grant); the consent sheet renders one line per host ("Connect to
  api.anthropic.com over the internet"), so the user sees exactly where
  data can go. Https-only. Redirects are refused rather than followed:
  a redirect re-sends request headers (API keys) to a host the user
  never consented to. Returns the real `Response`, so streaming (SSE)
  works. Both validators (Go publish gate, host load gate) accept the
  family via the same pattern.
- `sx.ui.openView(viewId)` under `views:main`: navigate to one of the
  calling extension's OWN main views (key-namespaced by extension id) —
  how a palette command routes into a full-page surface.

### API 1.3.0 additions (wave-2, grid & board)

- `views:main` permission + `registerMainView` — full-page views listed
  in the sidebar's LIBRARY section (scope kind `plugin-view`).
- `drafts.list()` / `drafts.updateFiles(id, files)` under `drafts:write`.
- `assets:write-metadata` permission + `writeAssetMetadata(name, patch)`:
  publishes a NEW REVISION with unchanged content and updated
  descriptive metadata (description, keywords, owner, status → metadata
  custom fields). Revisions keep versions immutable and audited;
  sharing is inherited; app-plugin assets are refused.

### API 1.2.0 additions (wave-2 milestone)

- New `editor` permission: `sx.editor.getValue/getCursor/getSelection/
  replaceSelection/replaceRange` operate on the draft the user has open
  (DraftSheet hands the live CodeMirror view to the host while mounted;
  cleared on unmount so calls throw instead of writing into a dead
  view). Extension edits flow through draft state exactly like typing.
- `CommandSpec.context: "editor"` — commands that hide from the palette
  and menus unless a draft editor is open.
- `sx.teams.list()` under `usage:read`: team names + membership for
  metric grouping (mutations stay core; the capability already exposes
  member emails via userStats).

### API 1.1.0 additions (marketplace milestone)

- `sx.ui.openAsset(name)` — open an asset's detail panel; how list-shaped
  extensions (search results, related assets, leaderboards) make rows
  navigable. No new permission: it can only open what the user can
  already see.
- The `views:asset-tab` slot now renders: `AssetDetail` shows a
  Content tab plus one tab per registered extension asset tab.

## The Extensions screen

A first-class screen (not a settings pane), reachable from the sidebar.
Final placement is a P1 design-pass decision; the content is normative:

- **Installed** — enable/disable toggles, permission summaries in plain
  language, version and update state, per-extension settings.
- **Available from your library** — extensions published to the vault and
  scoped to you but not yet enabled; one-click install/enable behind the
  consent sheet.
- **Add your own** — two paths:
  - *Publish*: package a plugin folder as an `app-plugin` asset into the
    vault (the same flow as dragging a skill in — this is the "upload").
  - *Developer mode — load from folder*: load an unpacked local plugin
    without publishing, for authoring iteration. Gated behind an explicit
    developer-mode toggle so normal users never see it; dev-loaded
    plugins are marked as such and excluded from policy allowlists (they
    are the author's own machine, own risk).
- **Policy visibility** — in allowlist mode, unlisted extensions render as
  "blocked by your organization" rather than silently missing; disabled
  mode hides the screen per the policy table above.
- **Diagnostics** — the loader preflight result (Blob import + CSS
  injection per platform) and each extension's last load error, so a
  broken platform or plugin is visible instead of silently absent.

## Built-in plugins (the v1 set)

Chosen by mapping Obsidian's download-mass categories (measured 2026-07-05 from official stats) onto the sx domain:

| Built-in | Obsidian analogue (downloads) | What it does | Slots/permissions exercised |
|---|---|---|---|
| **Dashboard widgets** (three extensions: User Adoption, Top Assets by Usage, User Leaderboard — split so teams toggle/replace each and third-party widgets sit beside them) | Dataview (4.5M), Homepage (1.2M) | Adoption donut over known users, top-asset daily usage trends, most-active-user bars. | `views:dashboard`, `usage:read` |
| **Publish Doctor** | Linter (1.0M) | Pre-publish checks contributing warnings to the publish sheet: frontmatter validity, description quality/length, trigger-phrase presence, broken file references. Converges with the skillpack-doctor idea from the gbrain research. | `events` (`before-publish`), `assets:read` |
| **Templates** | Templater (4.8M), QuickAdd (1.9M) | Org-blessed scaffolds for new skills/commands/agents with variable substitution; quick-capture from clipboard into a draft. | `commands`, `drafts:write`, `assets:read` |
| **Importer** | Importer (1.4M) | Import from existing `.claude/` directories, an Obsidian vault folder, or a folder of loose prompts; batch-create drafts. | `commands`, `drafts:write` |
| **Related Assets** *(shipped as a marketplace extension, not a built-in — TF-IDF cosine similarity; the embedding-based upgrade still ties to the v1.1 dedup groundwork)* | Smart Connections (1.1M) | Similar-asset tab on the detail view showing the shared terms that drove each match. | `views:asset-tab`, `assets:read` |
| **Claude Assist** *(shipped 1.4.0 as a marketplace extension; 1.1.0 migrates onto `sx.llm` — provider/key/model move to Settings)* | Copilot (1.5M), Claudian (1.2M — fastest riser) | Ask-the-library chat with clickable `[[asset]]` citations, critique-the-open-draft-as-a-prompt, new-skill-from-description. Completions via `sx.llm` against the user's configured provider. | `views:main`, `commands`, `editor`, `drafts:write`, `assets:read`, `llm:use` |
| **Duplicate detector** *(id `skill-doctor`; API 1.9.0 — phases 1–2 of docs/skill-dedupe-spec.md; sidebar TOOLS section)* | — | Finds duplicate/overlapping skills (SHA-256 exact + TF-IDF cosine + an LLM catalog sweep for semantic duplicates) and fixes them: **Keep one** consolidates installations onto a survivor and retires the rest (confirmed, recoverable), **Merge with AI** composes one definitive SKILL.md as a draft (publish stays human). Team-shared dismissals. | `views:main`, `commands`, `assets:read`, `assets:consolidate`, `drafts:write`, `llm:use`, `storage:shared` |

Not translated: sync/git plugins (core product here), canvas/tasks/calendar (wrong domain), theming (deferred).

## Code changes

| Area | Change |
|---|---|
| `app/frontend/src/plugins/` (new) | Plugin host: loader, lifecycle, permission proxy, registration tracking/teardown, `SxAPI` v1 implementation + published `.d.ts`. |
| `app/frontend/src/` | Slot registry (sidebar/asset-tab/dashboard); command palette; publish-sheet warning section; Extensions screen (see "The Extensions screen"; P1 ships list + enable/disable, P2 completes it). |
| `app/bridge.go` (or `app/plugins.go`) | Bridge methods: `ListAppPlugins`, `GetPluginBundle(id)`, `SetPluginEnabled(id, bool)`, `PluginLoadData/SaveData`, `PluginFetch` (origin-checked proxy), `GetPluginPolicy`. |
| `internal/asset/types.go`, `internal/metadata/` | `app-plugin` asset type + `[app-plugin]` metadata section. |
| `internal/manifest/` | `[app-plugins]` policy table (parse + RBAC on write). |
| `internal/mgmt/audit.go` | `plugin.enabled` / `plugin.disabled` / `plugin.policy-changed` events. |
| `internal/clients/` | None — app-plugins are not installed into AI clients; the type is filtered out of client install paths. |
| Docs | This spec; author guide `docs/app-plugin-authoring.md` (P4); update docs/metadata-spec.md, docs/manifest-spec.md, docs/audit.md. |

## Task plan

Execution follows the v2-spec desktop-app milestones (this work starts after M4; the dashboard surface can inform M-series UI work earlier).

**P1 — Framework + first built-ins prove the API.**
Plugin host, permission proxy, slot registry, command palette, `before-publish` hook. Ship the **dashboard widget extensions** (initially built as one Library Dashboard built-in, later split into User Adoption / Top Assets / Leaderboard) and **Publish Doctor** as vendored built-ins running through the host.
*Acceptance:* both built-ins fully functional with the host as their only access path; disabling either at runtime removes every trace (panels, commands, listeners); `import()`-loading verified on macOS WKWebView, Windows WebView2, Linux WebKitGTK; `make prepush` green.

**P2 — Consent, policy, storage, Extensions screen.**
The full Extensions screen (installed / available / add-your-own / policy
visibility / diagnostics); enable-time permission consent; per-plugin storage; `[app-plugins]` manifest policy + RBAC + audit events.
*Acceptance:* allowlist/disabled modes enforced end-to-end (policy set via CLI-edited manifest, app obeys); permission re-prompt on changed permissions; audit events verified via `sx audit`; Playwright coverage for enable/disable/consent flows.

**P3 — Built-in set complete.**
**Templates** and **Importer** built-ins; dashboard/doctor iterated on real usage.
*Acceptance:* import of a real `.claude/` directory produces correct drafts; template-created skill publishes cleanly; screenshot/design review pass per v2-spec quality loop.

**P4 — Third-party distribution.**
`app-plugin` asset type through the full pipeline (publish via CLI/app → scope → install → appear in Extensions); authoring guide + published `SxAPI` typings + a sample-plugin template repo; API declared v1-frozen (additive changes only).
*Acceptance:* a from-scratch demo plugin authored against only the public guide + typings, published to a test vault, team-scoped, enabled, and working; version pin + restore verified; policy allowlist blocks an unlisted third-party plugin.

**P5 — skills.new backend parity.**
Once P1–P4 are proven on git/path vaults, open a Pulse PR so sleuth vaults
carry `app-plugin` assets like every other type: the asset-type enum and
GraphQL surface, bundle serving, and a server-side equivalent of the
`[app-plugins]` org policy (org-admin gated, audited — the manifest table's
ACL twin). sx side: enable the type for sleuth vaults and read policy from
the server. Ships as its own release once the Pulse deploy is live.
*Acceptance:* the P4 demo plugin published to a skills.new vault,
team-scoped, enabled in the app, and blocked by a server-side allowlist;
`sx vault copy` round-trips app-plugins between file and sleuth vaults.

**P6 — Post-v1 (separate addenda when reached).**
Related Assets (with v1.1 dedup), Claude Assist, editor extensions, theming.

## Testing & verification

Per the v2-spec verification bar — nothing is done because it compiles:

- **Unit**: permission proxy (undeclared capability throws; per-permission filtering), registration teardown completeness, manifest/policy parsing, `app-plugin` metadata round-trip.
- **Playwright** (`wails dev`): full flows — enable with consent, use each slot, disable, allowlist enforcement, doctor warnings in publish sheet, importer end-to-end.
- **Native app**: verify `import()`/Blob loading and CSS injection per-OS (this is the one genuinely platform-sensitive mechanism — first thing P1 validates).
- **Cross-check with CLI**: policy edits and audit events round-trip through `sx` commands against the same vault.
- **API conformance**: the built-ins run in CI against the host as integration tests; any API break fails the suite.

## Risks

- **API becomes a contract too early.** Mitigation: nothing is public until P4; built-ins are deliberately diverse consumers; API carries `sx.api.version` from day one with an additive-only policy after freeze.
- **Dynamic `import()` behavior varies across WebViews** (CSP, Blob URLs, WebKitGTK quirks). Mitigation: P1 acceptance explicitly gates on all three platforms; fallback is a Function-constructor module shim (still no remote code).
  *Spike result (2026-07-07, macOS):* Blob-URL `import()` and CSS
  injection verified working in WKWebView in both `wails dev` and a
  production `wails build` binary (embedded asset server + its CSP), and
  in a plain browser. Windows WebView2 / Linux WebKitGTK remain open —
  the spike's self-test graduates into a permanent loader preflight in
  the plugin host (run at startup, surfaced in Extensions diagnostics),
  so those platforms verify themselves in CI and on first run.
- **Security review shortcut culture** (Obsidian's trap). Mitigation: permission model from day one, org allowlist, no-Node-by-construction, plugin updates ride audited asset versioning. Residual risk: a permitted plugin can still misuse what it's granted (e.g. exfiltrate via `sx.net.fetch` to a host it declared) — hence per-host grants (`net:<host>`, 1.4.0) and one consent line per host at enable time.
- **Slot/registry refactor destabilizes existing UI.** Mitigation: built-in features migrate onto slots one at a time behind the same visual design; screenshot regression pass per the v2-spec quality loop.
- **Scope creep toward Obsidian's surface area** (editor extensions, themes, renderers). Mitigation: the exclusion list above is normative; additions require a spec addendum, not a PR.

## Resolved questions (decided 2026-07-07)

1. **UI naming**: "Extensions" everywhere in UI copy; `app-plugin` remains
   the asset-type key.
2. **Where the dashboard lives**: sidebar root under LIBRARY initially
   (smallest blast radius); may graduate to a home surface if it earns it.
   Final look settled in the P1 design pass.
3. **Sandboxing depth**: main webview context behind the API proxy for
   P1–P3 (built-ins only, so the proxy is a discipline boundary). The
   iframe/realm sandbox question is a **formal P4 entry gate** — it must
   be explicitly re-decided before any third-party plugin can be enabled.
   *P4 gate decision (ratified by Dylan 2026-07-08, releasing as 2.1.0
   — the decision explicitly covers 1.4.0's `net:<host>` egress, where
   the per-host consent line is declared intent rather than an enforced
   boundary in main context):* third-party extensions run
   **main-context behind the proxy** in v1.
   Rationale: the webview has no Node/filesystem, network egress exists
   only through `sx.net.fetch` to hosts declared per extension and shown
   at consent (API 1.4.0; nothing before that had any network at all),
   distribution is org-internal and governed (allowlist +
   consent + audited versioning + pinning), and the realistic threat is
   a malicious org insider who already has vault write access — commit-
   access-equivalent trust, stated plainly in the authoring guide. An
   iframe/realm sandbox remains the planned hardening before any PUBLIC
   community catalog exists (that catalog is the point where untrusted
   authors appear).
4. **skills.new**: not at launch. P4 ships git/path-vault-only; sleuth
   vault support is P5 (its own Pulse PR + release), so all vault types
   converge without coupling the app timeline to a server deploy.
   *P5 status (2026-07-08):* Pulse backend reviewed and sx client
   implemented — app-plugin asset type mapped end to end, policy twin
   via `appPluginPolicy`/`setAppPluginPolicy` (server enforces the
   org-admin gate and audits), audit-import resolves `plugin` targets,
   and `VaultSupportsExtensions` probes the server per profile so old
   deployments still gate cleanly. Known server-side gaps at review:
   `storage:shared` has no twin (SleuthVault deliberately does not
   implement `AppPluginSharedStore`, so extensions get the clear
   unsupported error) and dashboard widget querysets needed the
   app-plugin exclusion. Extension id must equal the asset slug —
   sx publishes with name = plugin id, which slugifies identically.
