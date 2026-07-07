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
| — | `sx.ui.notice(msg)`, `sx.ui.confirm(msg, action)`, `sx.ui.openAsset(name)` (API 1.1.0 — opens an asset's detail panel, for making result rows navigable), `sx.storage.loadData()/saveData(data)`, `sx.app.version`, `sx.api.version` |
| `assets:read` | `sx.assets.list()`, `sx.assets.listCollections()`, `sx.assets.readFiles(name)` |
| `usage:read` | `sx.usage.events(days)`, `sx.usage.auditEvents(days)`, `sx.usage.userStats(days)` |
| `drafts:write` | `sx.drafts.create({name, files})`, `sx.drafts.importFromFolder()` — drafts only, never publishes |
| `views:sidebar` / `views:asset-tab` / `views:dashboard` | `sx.registerSidebarPanel/AssetTab/DashboardWidget({id, title, mount})` — `mount(view)` receives a bare DOM element (`view.el`); register cleanup with `view.onDispose(cb)`. No React is exposed. |
| `commands` | `sx.registerCommand({id, title, run})` — appears in the ⌘K palette |
| `events` | `sx.on("draft-saved" \| "asset-published" \| "asset-installed" \| "vault-synced", handler)` and `sx.onBeforePublish(ctx => warnings)` — returned warnings render in the publish sheet |

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
there is no Node, no filesystem, and no network access in API v1. The
permission proxy is an integrity boundary, not a hardened sandbox:
publishing an extension to a vault is trusted-code distribution within
your org, governed by the `[app-plugins]` allowlist
([manifest-spec.md](manifest-spec.md#app-plugins--desktop-app-extension-policy)),
enable-time consent, and audited versioning. Treat vault write access
like commit access.
