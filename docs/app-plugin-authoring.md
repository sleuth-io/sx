# Authoring sx app extensions

Extensions add optional functionality to the sx desktop app — dashboard
widgets, palette commands, publish checks, sidebar panels. They are
distributed as ordinary sx assets (`type = "app-plugin"`), so publishing,
team scoping, versioning, pinning, and audit all work exactly like a
skill. See [app-plugins-spec.md](app-plugins-spec.md) for the design.

## Anatomy

```
my-extension/
  plugin.json      # manifest (required)
  main.js          # one bundled ES module (required)
  metadata.toml    # standard sx metadata, type = "app-plugin"
```

`plugin.json`:

```json
{
  "id": "team-dashboard",
  "name": "Team Dashboard",
  "version": "1.0.0",
  "minAppVersion": "2.1.0",
  "description": "What this adds, in one sentence.",
  "author": "you@company.com",
  "permissions": ["assets:read", "views:dashboard"]
}
```

- `id`: lowercase letters/digits/hyphens, unique in the vault, may not
  be `sx` or start with `sx-`.
- `permissions`: the exhaustive list of capabilities you use. Undeclared
  calls throw at runtime; unknown names are rejected at load.
- `minAppVersion`: optional; the app refuses to load the extension on
  older builds with a clear message.

`metadata.toml`:

```toml
[asset]
name = "team-dashboard"
version = "1.0.0"
type = "app-plugin"
description = "What this adds, in one sentence."

[app-plugin]
entry = "main.js"                                  # default
permissions = ["assets:read", "views:dashboard"]   # mirror for indexing
```

## The code

`main.js` is a single bundled ES module (bundle with esbuild/rollup if
you have dependencies) whose default export implements:

```ts
export default class {
  onload(sx) {
    // register everything here; sx is your ONLY door into the app
  }
  onunload() {
    // optional; registrations are torn down for you
  }
}
```

Everything registered through `sx.*` is tracked and removed automatically
when your extension is disabled — you cannot leak panels, commands, or
event handlers.

### The `sx` API (v1)

| Needs permission | Surface |
|---|---|
| — | `sx.ui.notice(msg)`, `sx.ui.confirm(msg, action)`, `sx.ui.openAsset(name)` (API 1.1.0 — opens an asset's detail panel, for making result rows navigable), `sx.storage.loadData()/saveData(data)`, `sx.app.version`, `sx.app.currentUser()` (API 1.5.0 — the identity vault changes are attributed to), `sx.api.version` |
| `views:main` (also) | `sx.ui.openView(viewId)` (API 1.4.0) — navigate to one of **your own** registered main views, e.g. from a command. |
| `assets:read` | `sx.assets.list()`, `sx.assets.listCollections()`, `sx.assets.readFiles(name)` |
| `usage:read` | `sx.usage.events(days)`, `sx.usage.auditEvents(days)`, `sx.usage.userStats(days)`, `sx.teams.list()` (names + membership, API 1.2.0) |
| `drafts:write` | `sx.drafts.create({name, files})`, `sx.drafts.list()` (API 1.3.0), `sx.drafts.updateFiles(id, files)` (1.3.0), `sx.drafts.importFromFolder()` — drafts only, never publishes |
| `views:sidebar` / `views:asset-tab` / `views:dashboard` / `views:main` (full-page view listed in the sidebar, API 1.3.0) | `sx.registerSidebarPanel/AssetTab/DashboardWidget({id, title, mount})` — `mount(view)` receives a bare DOM element (`view.el`); register cleanup with `view.onDispose(cb)`. No React is exposed. |
| `commands` | `sx.registerCommand({id, title, run, menu?, hint?, context?})` — appears in the ⌘K palette. `menu: "new"` also places it in the “+ New” dropdown (creation-shaped actions only); `context: "editor"` hides it unless a draft editor is open. |
| `editor` | `sx.editor.getValue()`, `getCursor()`, `getSelection()` → `{text, from, to}`, `replaceSelection(text)`, `replaceRange(from, to, text)` — operates on the draft the user has open (API 1.2.0). Positions are character offsets. Every call throws when no editor is open; pair editor commands with `context: "editor"`. Edits flow through the draft exactly like typing. |
| `assets:write-metadata` | `sx.writeAssetMetadata(name, {description?, keywords?, owner?, status?})` (API 1.3.0) — publishes a new revision with unchanged content and updated descriptive metadata. Never content, type, scoping, or installs; refuses app-plugin assets. |
| `events` | `sx.on("draft-saved" \| "asset-published" \| "asset-installed" \| "vault-synced", handler)` and `sx.onBeforePublish(ctx => warnings)` — returned warnings render in the publish sheet |
| `storage:shared` | `sx.sharedStorage.load()` / `save(data)` (API 1.5.0) — ONE JSON document shared by everyone in the library, stored in the vault (`.sx/app-plugins/<id>.json`). Saves sync to the whole team and **commit on git vaults**, so write on user actions, not on keystrokes. 256 KB cap; whole-document last-writer-wins. Contrast `sx.storage`, which is per user and per profile. |
| `secrets` | `sx.secrets.get(name)` / `set(name, value)` (API 1.4.0) — named secrets in the **OS keychain** (macOS Keychain, Windows Credential Manager, Secret Service on Linux; 0600-file fallback on headless machines), scoped to your extension and the active profile. For API keys and tokens — they never land in `storage` data files or the vault. Setting `""` deletes. Names: lowercase, `[a-z][a-z0-9._-]*`. |
| `net:<host>` | `sx.net.fetch(url, init?)` (API 1.4.0) — your only network egress. Https-only; the URL's host must **exactly** equal a declared `net:<host>` grant (one permission per host, no wildcards, no ports/paths in the grant). Redirects are refused (they would re-send your headers to an undeclared host). Returns the real `Response`, so streaming bodies (SSE) work. The consent sheet names each host, so declare the minimum. |

The API is versioned (`sx.api.version`) and changes additively only.
Type definitions live at `app/frontend/src/plugins/api.ts` in the sx
repo.

## Developing

A minimal working extension is a class with an `onload` registering one
command — no build step needed until you add dependencies:

```js
export default class {
  onload(sx) {
    sx.registerCommand({
      id: "hello",
      title: "Say hello",
      run: () => sx.ui.notice("Hello from my extension"),
    });
  }
}
```

Publish it like any asset:

```bash
sx add ./my-extension --yes
```

It appears in the app under Settings → Extensions (never in the skills
list), disabled. Enabling shows its permission list for consent. Team
scoping works like any asset: share it with a team and only they see it.

### Styling

Views own a bare DOM element; style it with **inline styles referencing
the app's design tokens** — `var(--color-ink)`, `var(--color-ink-soft)`,
`var(--color-ink-faint)`, `var(--color-line)`, `var(--color-canvas)`,
`var(--color-surface)`, `var(--color-accent)`, `var(--color-accent-soft)`,
`var(--color-danger)`, `var(--font-mono)`. These track light/dark mode
automatically and are part of the API contract. Do **not** use the app's
utility class names — they are build artifacts and can vanish between
releases. See the extensions in
[sx-extensions](https://github.com/sleuth-io/sx-extensions) for worked
examples.

### Sharing beyond your team

To offer an extension in the shared marketplace, open a PR against
[sx-extensions](https://github.com/sleuth-io/sx-extensions) adding your
folder under `extensions/` (the repo is itself an sx vault; maintainers
publish accepted extensions with `sx add`). Users find it under
Settings → Extensions → Browse marketplace.

## Trust model (read before publishing broadly)

Extensions run in the app's webview behind the permission-gated API —
there is no Node, no filesystem, and no network access beyond
`sx.net.fetch` to hosts the extension declared (and the user saw at
consent). The
permission proxy is an integrity boundary, not a hardened sandbox:
publishing an extension to a vault is trusted-code distribution within
your org, governed by the `[app-plugins]` allowlist
([manifest-spec.md](manifest-spec.md#app-plugins--desktop-app-extension-policy)),
enable-time consent, and audited versioning. Treat vault write access
like commit access.
