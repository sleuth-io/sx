// SxAPI v1 — the ONLY surface extensions may touch. Everything here is a
// deliberate, versioned contract (docs/app-plugins-spec.md): if an
// extension needs something this file doesn't offer, the answer is an API
// addition, never an escape hatch to app internals.

export const SX_API_VERSION = "1.5.0";

/** Capabilities an extension may declare. Undeclared calls throw.
 * `net:<host>` is a parameterized family (API 1.4.0): each declared
 * host is its own grant, and sx.net.fetch refuses every other host. */
export type Permission =
  | "assets:read"
  | "usage:read"
  | "drafts:write"
  | "views:sidebar"
  | "views:asset-tab"
  | "views:dashboard"
  | "views:main"
  | "commands"
  | "events"
  | "editor"
  | "assets:write-metadata"
  | "secrets"
  | "storage:shared"
  | `net:${string}`;

/** plugin.json — the extension manifest. */
export interface PluginManifest {
  id: string;
  name: string;
  version: string;
  minAppVersion?: string;
  description?: string;
  author?: string;
  permissions: Permission[];
}

/** The default export of an extension's main.js. */
export interface SxPlugin {
  onload(sx: SxAPI): void | Promise<void>;
  onunload?(): void;
}

// ---- Read models (plain data, decoupled from bridge types) ----

export interface AssetSummary {
  name: string;
  type: string;
  description: string;
  updatedAt?: string;
}

export interface AssetFileContent {
  path: string;
  content: string;
}

export interface CollectionSummary {
  name: string;
  description: string;
  assets: string[];
}

export interface UsageEvent {
  timestamp: string;
  actor: string;
  assetName: string;
  assetVersion: string;
  assetType: string;
}

export interface UserActivity {
  actor: string;
  events: number;
  distinctAssets: number;
}

export interface UserStats {
  knownUsers: string[];
  active: UserActivity[];
}

export interface AuditEvent {
  timestamp: string;
  actor: string;
  event: string;
  targetType: string;
  target: string;
}

export interface DraftInput {
  name: string;
  files: AssetFileContent[];
}

// ---- View contracts ----
// Mount points are React-free: the extension receives a bare DOM element
// it owns until dispose. The host removes the element on teardown, so a
// leaked mount can never outlive its extension.

export interface ViewMount {
  el: HTMLElement;
  /** Called before the host removes el; release listeners/timers here. */
  onDispose(cb: () => void): void;
}

export interface SidebarPanelSpec {
  id: string;
  title: string;
  mount(view: ViewMount): void;
}

export interface AssetTabSpec {
  id: string;
  title: string;
  mount(view: ViewMount, ctx: { assetName: string }): void;
}

export interface MainViewSpec {
  id: string;
  title: string;
  mount(view: ViewMount): void;
}

export interface DashboardWidgetSpec {
  id: string;
  title: string;
  mount(view: ViewMount): void;
}

export interface CommandSpec {
  id: string;
  title: string;
  run(): void | Promise<void>;
  /** Optional placement in core menus besides the palette: "new" adds
   * the command to the “+ New” dropdown (creation-shaped actions only). */
  menu?: "new";
  /** "editor" commands only appear while a draft editor is open. */
  context?: "editor";
  /** Short hint line shown in menus (not the palette). */
  hint?: string;
}

// ---- Events ----

export interface BeforePublishContext {
  name: string;
  description: string;
  files: AssetFileContent[];
}

/** Warnings surface in the publish sheet; publishing stays allowed. */
export interface PublishWarning {
  message: string;
  detail?: string;
}

export interface EventMap {
  "draft-saved": { draftId: string };
  "asset-published": { name: string };
  "asset-installed": { name: string };
  "vault-synced": Record<string, never>;
}

// ---- The API object handed to onload ----

export interface SxAPI {
  readonly app: { version: string };
  readonly api: { version: string };

  /** Always available. */
  readonly ui: {
    notice(message: string): void;
    confirm(message: string, action: string): Promise<boolean>;
    /** Open an asset's detail panel — how list-shaped extensions
     * (search results, related assets) make rows navigable. Added in
     * API 1.1.0. */
    openAsset(name: string): void;
    /** Navigate to one of THIS extension's registered main views (API
     * 1.4.0) — how a command routes the user into the extension's
     * full-page surface. Requires views:main. */
    openView(viewId: string): void;
  };

  /** Always available; per plugin, per profile, stored app-side. */
  readonly storage: {
    loadData<T>(): Promise<T | null>;
    saveData<T>(data: T): Promise<void>;
  };

  /** Requires storage:shared (API 1.5.0). One JSON document per
   * extension shared by EVERYONE in the library — it lives in the vault
   * (.sx/app-plugins/<id>.json), so saves sync to the team (and commit,
   * on a git vault: keep writes user-action-shaped). Whole-document
   * last-writer-wins; capped at 256 KB. */
  readonly sharedStorage: {
    load<T>(): Promise<T | null>;
    save<T>(data: T): Promise<void>;
  };

  /** Requires assets:read. */
  readonly assets: {
    list(): Promise<AssetSummary[]>;
    listCollections(): Promise<CollectionSummary[]>;
    readFiles(name: string): Promise<AssetFileContent[]>;
  };

  /** Requires usage:read. */
  readonly usage: {
    events(sinceDays: number): Promise<UsageEvent[]>;
    auditEvents(sinceDays: number): Promise<AuditEvent[]>;
    /** Per-user adoption: everyone the vault knows plus who used what. */
    userStats(sinceDays: number): Promise<UserStats>;
  };

  /** Requires assets:write-metadata (API 1.3.0). Publishes a new
   * revision with unchanged content and updated DESCRIPTIVE metadata —
   * never content, type, scoping, or installs. */
  writeAssetMetadata(
    name: string,
    patch: {
      description?: string;
      keywords?: string[];
      owner?: string;
      status?: string;
    },
  ): Promise<void>;

  /** Requires usage:read (API 1.2.0). Team names and membership for
   * grouping — team management stays core. */
  readonly teams: {
    list(): Promise<{ name: string; members: string[] }[]>;
  };

  /** Requires secrets (API 1.4.0). Named secrets in the OS keychain,
   * scoped to this extension and profile — for API keys and tokens
   * that must never land in plugin data files or the vault. */
  readonly secrets: {
    /** Resolves to "" when the secret is unset. */
    get(name: string): Promise<string>;
    /** Setting "" deletes the secret. */
    set(name: string, value: string): Promise<void>;
  };

  /** Requires a matching net:<host> permission (API 1.4.0). The ONLY
   * network egress an extension has: https-only, and the URL's host
   * must equal a declared net:<host> grant exactly. Returns the real
   * Response, so streaming bodies (SSE) work. */
  readonly net: {
    fetch(url: string, init?: RequestInit): Promise<Response>;
  };

  /** Requires editor (API 1.2.0). Operates on the draft the user has
   * open; every call throws when no editor is open. Positions are
   * character offsets into the document. */
  readonly editor: {
    active(): boolean;
    getValue(): string;
    getCursor(): number;
    getSelection(): { text: string; from: number; to: number };
    replaceSelection(text: string): void;
    replaceRange(from: number, to: number, text: string): void;
  };

  /** Requires drafts:write. Never publishes — that stays a human action. */
  readonly drafts: {
    create(draft: DraftInput): Promise<{ id: string }>;
    /** API 1.3.0: enumerate open drafts. */
    list(): Promise<
      { id: string; name: string; type: string; targetAsset: string }[]
    >;
    /** API 1.3.0: replace a draft's files (create-only before). */
    updateFiles(id: string, files: AssetFileContent[]): Promise<void>;
    /** Native folder picker → one draft per skill folder / markdown file. */
    importFromFolder(): Promise<{ created: string[]; skipped: number }>;
  };

  /** Requires views:sidebar. */
  registerSidebarPanel(spec: SidebarPanelSpec): void;
  /** Requires views:asset-tab. */
  registerAssetTab(spec: AssetTabSpec): void;
  /** Requires views:dashboard. */
  registerDashboardWidget(spec: DashboardWidgetSpec): void;
  /** Requires views:main (API 1.3.0): a full-page view, listed in the
   * sidebar's LIBRARY section. */
  registerMainView(spec: MainViewSpec): void;
  /** Requires commands. */
  registerCommand(spec: CommandSpec): void;

  /** Requires events. */
  on<K extends keyof EventMap>(
    event: K,
    handler: (payload: EventMap[K]) => void,
  ): void;
  /**
   * Requires events. Subscribers may return warnings that render in the
   * publish sheet (the doctor hook); returning nothing approves silently.
   */
  onBeforePublish(
    handler: (
      ctx: BeforePublishContext,
    ) => PublishWarning[] | Promise<PublishWarning[]> | void,
  ): void;
}
