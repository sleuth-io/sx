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

- `id`: lowercase + hyphens, unique within the vault, may not contain `sx`.
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
| `usage:read` | Read usage and audit event streams (the `.sx/usage/`, `.sx/audit/` JSONL data, via Go). |
| `drafts:write` | Create/update drafts (never direct publishes — publish stays a human action in core UI). |
| `views:sidebar` | Register a sidebar panel (icon + React-free mount point: plugin gets a DOM element + lifecycle). |
| `views:asset-tab` | Register a tab in the asset detail pane (receives the asset context). |
| `views:dashboard` | Register a widget on a (new) dashboard/home surface. |
| `commands` | Register commands in the command palette (new core feature, see below) with optional shortcuts. |
| `events` | Subscribe to lifecycle events: `draft-saved`, `before-publish` (may return warnings shown in the publish sheet — the doctor hook), `asset-published`, `asset-installed`, `vault-synced`. |
| `net:fetch` | HTTP fetch proxied through Go with per-plugin allowed-origin list surfaced at enable time. |
| (always) | `sx.ui` kit — modal, notice/toast, confirm, settings panel schema; `sx.storage` — `loadData()`/`saveData()` per plugin per profile (stored app-side, not in the vault); `sx.app.version`, `sx.api.version`. |

**Explicitly excluded from API v1** (deferred, revisit after P6 planning):
- CodeMirror editor extensions — requires exporting our exact CM6 package instances and freezing them; Obsidian's biggest coupling trap. Defer.
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
allowed = ["library-dashboard", "publish-doctor"]
```

Only org admins can modify it (same RBAC as scope changes, see docs/rbac.md). In `allowlist` mode the app refuses to enable anything else; in `disabled` mode the extensions UI is hidden. Policy changes append audit events.
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
| **Library Dashboard** | Dataview (4.5M), Homepage (1.2M) | Saved queries over assets + usage/audit: "unused 30 days", team adoption, staleness, recent activity. | `views:dashboard`, `views:sidebar`, `assets:read`, `usage:read` |
| **Publish Doctor** | Linter (1.0M) | Pre-publish checks contributing warnings to the publish sheet: frontmatter validity, description quality/length, trigger-phrase presence, broken file references. Converges with the skillpack-doctor idea from the gbrain research. | `events` (`before-publish`), `assets:read` |
| **Templates** | Templater (4.8M), QuickAdd (1.9M) | Org-blessed scaffolds for new skills/commands/agents with variable substitution; quick-capture from clipboard into a draft. | `commands`, `drafts:write`, `assets:read` |
| **Importer** | Importer (1.4M) | Import from existing `.claude/` directories, an Obsidian vault folder, or a folder of loose prompts; batch-create drafts. | `commands`, `drafts:write` |
| **Related Assets** *(P4+, ties to v1.1 dedup)* | Smart Connections (1.1M) | Local-embedding similarity: "this looks 85% like *brand-voice*" at draft time; related-assets panel on detail view. Implementation shared with the v2-spec write-time dedup groundwork. | `views:asset-tab`, `events`, `assets:read` |
| **Claude Assist** *(exploratory, last)* | Copilot (1.5M), Claudian (1.2M — fastest riser) | Draft/critique/test a skill against Claude from inside the editor. Needs `net:fetch` + key management; spec addendum before build. | `views:asset-tab`, `commands`, `net:fetch` |

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
Plugin host, permission proxy, slot registry, command palette, `before-publish` hook. Ship **Library Dashboard** and **Publish Doctor** as vendored built-ins running through the host.
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
- **Security review shortcut culture** (Obsidian's trap). Mitigation: permission model from day one, org allowlist, no-Node-by-construction, plugin updates ride audited asset versioning. Residual risk: a permitted plugin can still misuse what it's granted (e.g. exfiltrate via `net:fetch`) — hence per-origin fetch grants and surfacing origins at consent time.
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
4. **skills.new**: not at launch. P4 ships git/path-vault-only; sleuth
   vault support is P5 (its own Pulse PR + release), so all vault types
   converge without coupling the app timeline to a server deploy.
