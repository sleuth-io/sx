// buildSxAPI constructs the per-extension, permission-filtered API object.
// Undeclared capabilities throw at the call site — the manifest's
// permission list is enforcement, not documentation. Mounted views are
// tracked per plugin so the host can dispose every DOM trace on disable.

import {
  ExportCollectionBundle,
  GetAsset,
  GetDraft,
  ListDrafts,
  PluginTeams,
  RepoAssets,
  TeamAssets,
  PluginWriteMetadata,
  UpdateDraft,
  ListAssets,
  ListCollections,
  CreateDraftFromFiles,
  ImportDraftsFromFolder,
  PluginLoadData,
  PluginSaveData,
  PluginSecretGet,
  PluginSecretSet,
  PluginSharedLoad,
  PluginSharedSave,
  PluginUsageEvents,
  PluginAuditEvents,
  PluginCurrentUser,
  PluginUserStats,
} from "../../wailsjs/go/main/App";
import type {
  AssetTabSpec,
  CollectionExportFormat,
  CollectionViewSpec,
  CommandSpec,
  DashboardWidgetSpec,
  MainViewSpec,
  RepoViewSpec,
  Permission,
  PluginManifest,
  SidebarPanelSpec,
  TeamViewSpec,
  SxAPI,
  ViewMount,
} from "./api";
import { SX_API_VERSION } from "./api";
import { getAppVersion } from "./host";
import { registerSlotEntry } from "./registry";
import { subscribeBeforePublish, subscribeEvent } from "./events";

// ---- UI services, provided by the app shell ----
// The Library screen registers real implementations on mount; extensions
// loaded before that (or in tests) degrade to console/no-op gracefully.

interface UIHandlers {
  notice(message: string): void;
  confirm(message: string, action: string): Promise<boolean>;
  /** Reload the app's data views (drafts/assets lists). */
  refresh(): void;
  /** Open the named asset's detail panel. */
  openAsset(name: string): void;
  /** Navigate to a plugin main view by its "pluginId:viewId" key. */
  openView(key: string): void;
}

let ui: UIHandlers = {
  notice: (m) => console.log("extension notice:", m),
  confirm: async () => false,
  refresh: () => {},
  openAsset: () => {},
  openView: () => {},
};

export function setPluginUIHandlers(handlers: UIHandlers): void {
  ui = handlers;
}

// ---- Editor access ----
// DraftSheet hands the live CodeMirror view in while a draft editor is
// mounted; extensions reach it only through the permission-gated
// sx.editor surface. Null means "no editor open" and every op throws.

export interface EditorHandle {
  getValue(): string;
  getCursor(): number;
  getSelection(): { text: string; from: number; to: number };
  replaceSelection(text: string): void;
  replaceRange(from: number, to: number, text: string): void;
}

let editorHandle: EditorHandle | null = null;
const editorListeners = new Set<() => void>();

export function setPluginEditor(handle: EditorHandle | null): void {
  editorHandle = handle;
  for (const cb of editorListeners) cb();
}

export function subscribeEditor(cb: () => void): () => void {
  editorListeners.add(cb);
  return () => editorListeners.delete(cb);
}

export function editorOpen(): boolean {
  return editorHandle !== null;
}

function needEditor(): EditorHandle {
  if (!editorHandle) throw new Error("no draft editor is open");
  return editorHandle;
}

// ---- Mount tracking ----
// The UI slot components call mountEntry() when rendering an extension's
// view; the host calls disposePluginMounts() on disable. Elements and
// dispose callbacks are tracked per plugin so no DOM or listener can
// outlive its owner.

interface TrackedMount {
  el: HTMLElement;
  disposers: (() => void)[];
}

const mounts = new Map<string, TrackedMount[]>();

/** Mount one slot entry into el, tracking it for teardown. Returns a
 * dispose function the React wrapper calls on unmount as well. */
export function mountEntry(
  pluginId: string,
  el: HTMLElement,
  mount: (view: ViewMount) => void,
): () => void {
  const tracked: TrackedMount = { el, disposers: [] };
  const list = mounts.get(pluginId) ?? [];
  mounts.set(pluginId, [...list, tracked]);
  try {
    mount({ el, onDispose: (cb) => tracked.disposers.push(cb) });
  } catch (e) {
    console.error(`extension ${pluginId}: mount failed`, e);
  }
  return () => disposeOne(pluginId, tracked);
}

function disposeOne(pluginId: string, tracked: TrackedMount): void {
  const list = mounts.get(pluginId) ?? [];
  const idx = list.indexOf(tracked);
  if (idx < 0) return; // already disposed (host teardown won the race)
  list.splice(idx, 1);
  for (const d of tracked.disposers) {
    try {
      d();
    } catch (e) {
      console.error(`extension ${pluginId}: dispose failed`, e);
    }
  }
  tracked.el.replaceChildren();
}

export function disposePluginMounts(pluginId: string): void {
  for (const m of [...(mounts.get(pluginId) ?? [])]) {
    disposeOne(pluginId, m);
  }
  mounts.delete(pluginId);
}

// ---- API construction ----

export function buildSxAPI(manifest: PluginManifest): SxAPI {
  const granted = new Set<Permission>(manifest.permissions);
  const id = manifest.id;

  function need(p: Permission): void {
    if (!granted.has(p)) {
      throw new Error(
        `extension ${id} used "${p}" without declaring it in plugin.json`,
      );
    }
  }

  const api: SxAPI = {
    app: {
      version: getAppVersion(),
      currentUser: async () => (await PluginCurrentUser().catch(() => "")) ?? "",
    },
    api: { version: SX_API_VERSION },

    ui: {
      notice: (message) => ui.notice(message),
      confirm: (message, action) => ui.confirm(message, action),
      openAsset: (name) => ui.openAsset(name),
      openView: (viewId) => {
        // Namespaced by the caller's id: an extension can open only its
        // OWN main views, never navigate the user into someone else's.
        need("views:main");
        ui.openView(id + ":" + viewId);
      },
    },

    storage: {
      async loadData<T>(): Promise<T | null> {
        const raw = await PluginLoadData(id);
        if (!raw) return null;
        try {
          return JSON.parse(raw) as T;
        } catch {
          return null;
        }
      },
      async saveData<T>(data: T): Promise<void> {
        await PluginSaveData(id, JSON.stringify(data));
      },
    },

    sharedStorage: {
      async load<T>(): Promise<T | null> {
        need("storage:shared");
        const raw = await PluginSharedLoad(id);
        if (!raw) return null;
        try {
          return JSON.parse(raw) as T;
        } catch {
          return null;
        }
      },
      async save<T>(data: T): Promise<void> {
        need("storage:shared");
        await PluginSharedSave(id, JSON.stringify(data));
      },
    },

    assets: {
      async list() {
        need("assets:read");
        const items = await ListAssets();
        return (items ?? [])
          // Extensions are invisible outside the Extensions screen —
          // including to other extensions' queries, stats, and search.
          .filter((a) => a.type !== "app-plugin")
          .map((a) => ({
            name: a.name,
            type: a.type,
            description: a.description,
            updatedAt: a.updatedAt,
          }));
      },
      async listCollections() {
        need("assets:read");
        const cols = await ListCollections();
        return (cols ?? []).map((c) => ({
          name: c.name,
          description: c.description,
          assets: c.assets ?? [],
        }));
      },
      async readFiles(name: string) {
        need("assets:read");
        const detail = await GetAsset(name, "");
        // list() hides extensions; guessing an id must not read one
        // anyway — hidden from discovery AND from access.
        if (detail.type === "app-plugin") {
          throw new Error(`asset ${name} is not readable through the extension API`);
        }
        return (detail.files ?? []).map((f) => ({
          path: f.path,
          content: f.content,
        }));
      },
    },

    usage: {
      async events(sinceDays: number) {
        need("usage:read");
        return (await PluginUsageEvents(sinceDays)) ?? [];
      },
      async auditEvents(sinceDays: number) {
        need("usage:read");
        return (await PluginAuditEvents(sinceDays)) ?? [];
      },
      async userStats(sinceDays: number) {
        need("usage:read");
        const res = await PluginUserStats(sinceDays);
        return {
          knownUsers: res.knownUsers ?? [],
          active: res.active ?? [],
        };
      },
    },

    async writeAssetMetadata(name, patch) {
      need("assets:write-metadata");
      await PluginWriteMetadata(name, {
        description: patch.description ?? null,
        keywords: patch.keywords ?? null,
        owner: patch.owner ?? null,
        status: patch.status ?? null,
      } as never);
      ui.refresh();
    },

    teams: {
      async list() {
        need("usage:read");
        const [teams, assets] = await Promise.all([
          PluginTeams(),
          // Which assets each team receives (API 1.7.0) — best-effort:
          // vaults that can't report it just leave the lists empty.
          TeamAssets().catch(() => ({}) as Record<string, string[]>),
        ]);
        return (teams ?? []).map((t) => ({
          name: t.name,
          members: t.members ?? [],
          assets: assets?.[t.name] ?? [],
        }));
      },
    },

    repos: {
      /** Repository URL → asset names scoped to it (API 1.7.0). */
      async list() {
        need("assets:read");
        const repos = await RepoAssets();
        return Object.entries(repos ?? {}).map(([url, assets]) => ({
          url,
          assets: assets ?? [],
        }));
      },
    },

    collections: {
      async export(name: string, format: CollectionExportFormat) {
        need("export");
        // The bridge shows the native save dialog and resolves "" on
        // cancel — cancelling is a user choice, not an error.
        return (await ExportCollectionBundle(name, format)) ?? "";
      },
    },

    secrets: {
      async get(name: string) {
        need("secrets");
        return (await PluginSecretGet(id, name)) ?? "";
      },
      async set(name: string, value: string) {
        need("secrets");
        await PluginSecretSet(id, name, value);
      },
    },

    net: {
      async fetch(url: string, init?: RequestInit) {
        let host: string;
        let protocol: string;
        try {
          const u = new URL(url);
          host = u.hostname;
          protocol = u.protocol;
        } catch {
          throw new Error(`sx.net.fetch: invalid URL ${JSON.stringify(url)}`);
        }
        if (protocol !== "https:") {
          throw new Error("sx.net.fetch is https-only");
        }
        // The permission IS the allowlist: exact host match, no
        // subdomain wildcards, checked per call.
        const grant = ("net:" + host) as Permission;
        if (!granted.has(grant)) {
          throw new Error(
            `extension ${id} fetched ${host} without declaring "net:${host}" in plugin.json`,
          );
        }
        // Redirects are refused outright: following one would re-send
        // the request — custom headers (API keys) included — to a host
        // the user never consented to.
        return fetch(url, { ...init, redirect: "error" });
      },
    },

    editor: {
      active: () => {
        need("editor");
        return editorHandle !== null;
      },
      getValue: () => {
        need("editor");
        return needEditor().getValue();
      },
      getCursor: () => {
        need("editor");
        return needEditor().getCursor();
      },
      getSelection: () => {
        need("editor");
        return needEditor().getSelection();
      },
      replaceSelection: (text) => {
        need("editor");
        needEditor().replaceSelection(text);
      },
      replaceRange: (from, to, text) => {
        need("editor");
        needEditor().replaceRange(from, to, text);
      },
    },

    drafts: {
      async create(draft) {
        need("drafts:write");
        const created = await CreateDraftFromFiles(
          draft.name,
          draft.files.map((f) => ({ path: f.path, content: f.content })),
        );
        // The library must show the new draft NOW, not on the next
        // focus-triggered reload.
        ui.refresh();
        return { id: created.id };
      },
      async list() {
        need("drafts:write");
        const drafts = await ListDrafts();
        return (drafts ?? [])
          // Extension drafts are as off-limits as extension assets: one
          // extension must not see (or below, edit) another in progress.
          .filter((d) => d.type !== "app-plugin")
          .map((d) => ({
            id: d.id,
            name: d.name,
            type: d.type,
            targetAsset: d.targetAsset ?? "",
          }));
      },
      async updateFiles(id: string, files) {
        need("drafts:write");
        const draft = await GetDraft(id);
        if (draft.type === "app-plugin") {
          throw new Error(`draft ${id} is not editable through the extension API`);
        }
        draft.files = files.map((f) => ({ path: f.path, content: f.content })) as never;
        await UpdateDraft(draft);
        ui.refresh();
      },
      async importFromFolder() {
        need("drafts:write");
        const res = await ImportDraftsFromFolder();
        ui.refresh();
        return { created: res.created ?? [], skipped: res.skipped ?? 0 };
      },
    },

    registerSidebarPanel(spec: SidebarPanelSpec) {
      need("views:sidebar");
      registerSlotEntry("sidebar-panel", id, spec);
    },
    registerAssetTab(spec: AssetTabSpec) {
      need("views:asset-tab");
      registerSlotEntry("asset-tab", id, spec);
    },
    registerDashboardWidget(spec: DashboardWidgetSpec) {
      need("views:dashboard");
      registerSlotEntry("dashboard-widget", id, spec);
    },
    registerMainView(spec: MainViewSpec) {
      need("views:main");
      registerSlotEntry("main-view", id, spec);
    },
    registerCollectionView(spec: CollectionViewSpec) {
      need("views:collection");
      registerSlotEntry("collection-view", id, spec);
    },
    registerTeamView(spec: TeamViewSpec) {
      need("views:team");
      registerSlotEntry("team-view", id, spec);
    },
    registerRepoView(spec: RepoViewSpec) {
      need("views:repo");
      registerSlotEntry("repo-view", id, spec);
    },
    registerCommand(spec: CommandSpec) {
      need("commands");
      registerSlotEntry("command", id, spec);
    },

    on(event, handler) {
      need("events");
      subscribeEvent(id, event, handler as (payload: unknown) => void);
    },
    onBeforePublish(handler) {
      need("events");
      subscribeBeforePublish(id, handler);
    },
  };

  return Object.freeze(api);
}
