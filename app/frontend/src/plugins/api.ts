// SxAPI v1 — the ONLY surface extensions may touch. Everything here is a
// deliberate, versioned contract (docs/app-plugins-spec.md): if an
// extension needs something this file doesn't offer, the answer is an API
// addition, never an escape hatch to app internals.

export const SX_API_VERSION = "1.11.0";

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
  | "views:collection"
  | "views:team"
  | "views:repo"
  | "export"
  | "llm:use"
  | "assets:consolidate"
  | "benchmarks"
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

// ---- Installations & consolidation (API 1.9.0) ----

export interface AssetInstallationRow {
  kind: "org" | "repo" | "path" | "team" | "user" | "bot";
  repo?: string;
  paths?: string[];
  team?: string;
  user?: string;
  bot?: string;
}

export interface AssetInstallations {
  everyone: boolean;
  installations: AssetInstallationRow[];
}

export interface ConsolidateResult {
  movedInstallations: number;
  retired: string[];
  /** Sources NOT retired because part of their reach was refused (see
   * skipped) — retiring them would have shrunk someone's access. */
  kept: string[];
  /** Vault-refused install moves (RBAC); the consolidation continued. */
  skipped: string[];
}

// ---- Benchmarks (API 1.10.0) ----

/** One benchmark result in the interchange shape shared with skills.new
 * (docs/benchmarks-spec.md). File vaults hold these at
 * .sx/benchmarks/<asset>.json; skills.new stores them as real benchmark
 * rows, so records with source "server" are benchmarks skills.new ran
 * itself. */
export interface BenchmarkRecord {
  /** RFC3339 timestamp of the run. */
  at: string;
  source: "app" | "server";
  executor: { provider: string; model: string };
  runs_per_config: number;
  /** Who ran it (vault identity or skills.new user). */
  by?: string;
  /** Pulse's run_summary shape: with_skill/without_skill stat blocks
   * ({pass_rate: {mean, stddev, min, max}, ...}) plus a delta block. */
  summary: {
    with_skill: Record<string, { mean: number; stddev?: number; min?: number; max?: number }>;
    without_skill: Record<string, { mean: number; stddev?: number; min?: number; max?: number }>;
    delta: Record<string, number>;
  };
  per_eval?: { eval_key: string; with_pass: number; without_pass: number; status: string }[];
  notes?: string[];
  /** App records: the content hash the run was benchmarked against. */
  skill_hash?: string;
  /** Server records: the benchmarked version and whether it is still
   * the asset's current one — the staleness signal. */
  skill_version?: string | null;
  is_current_version?: boolean | null;
}

// ---- LLM (API 1.9.0) ----

export interface LLMMessage {
  role: "system" | "user" | "assistant";
  content: string;
}

export interface LLMRequest {
  messages: LLMMessage[];
  /** JSON Schema the reply must validate against. When set, the result's
   * `json` field carries the parsed document. */
  schema?: Record<string, unknown>;
  /** Ask the configured provider for a specific model; most extensions
   * should omit this and respect the user's choice. */
  model?: string;
  maxTokens?: number;
}

export interface LLMResult {
  /** The reply text; a bare JSON document when `schema` was given. */
  text: string;
  /** Parsed JSON reply — present only when the request carried `schema`. */
  json?: unknown;
  /** Which provider/model answered (e.g. "ollama" / "llama3:8b"). */
  provider: string;
  model: string;
  /** Zero when the provider can't report token counts (CLI providers). */
  usage: { inputTokens: number; outputTokens: number };
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
  /** Where the view's sidebar row lives (API 1.9.0): "library" (the
   * default) lists it with the core navigation; "tools" lists it under
   * the collapsed TOOLS section — for utilities that act ON the library
   * (dedupe, assistants) rather than views OF it. */
  section?: "library" | "tools";
  mount(view: ViewMount): void;
}

export interface CollectionViewSpec {
  id: string;
  title: string;
  mount(view: ViewMount, ctx: { collection: string }): void;
}

export interface TeamViewSpec {
  id: string;
  title: string;
  mount(view: ViewMount, ctx: { team: string }): void;
}

export interface RepoViewSpec {
  id: string;
  title: string;
  /** ctx.repo is the repository URL exactly as the vault records it. */
  mount(view: ViewMount, ctx: { repo: string }): void;
}

/** Bundle formats sx.collections.export can produce (API 1.6.0). */
export type CollectionExportFormat = "claude-code" | "codex" | "gemini" | "zip";

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
  readonly app: {
    version: string;
    /** The identity vault changes are attributed to — how team-shaped
     * extensions know which entries are "mine" (API 1.5.0). Resolves
     * to "" when the vault can't name one. */
    currentUser(): Promise<string>;
  };
  readonly api: { version: string };

  /** Always available. */
  readonly ui: {
    notice(message: string): void;
    confirm(message: string, action: string): Promise<boolean>;
    /** Open an asset's detail panel — how list-shaped extensions
     * (search results, related assets) make rows navigable. Added in
     * API 1.1.0. `opts.tab` (API 1.11.0) opens one of THIS extension's
     * own asset tabs (by its registered id) instead of the content
     * view; older apps ignore it. */
    openAsset(name: string, opts?: { tab?: string }): void;
    /** Navigate to one of THIS extension's registered main views (API
     * 1.4.0) — how a command routes the user into the extension's
     * full-page surface. Requires views:main. */
    openView(viewId: string): void;
    /** Open the app's Settings on a specific tab (API 1.9.0) — how an
     * extension whose feature needs app-level setup (e.g. llm:use with
     * no AI provider configured) sends the user to the right place
     * instead of describing a path in prose. */
    openSettings(section?: "libraries" | "extensions" | "ai"): void;
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

  /** Requires assets:read (consolidate additionally requires
   * assets:consolidate). */
  readonly assets: {
    list(): Promise<AssetSummary[]>;
    listCollections(): Promise<CollectionSummary[]>;
    readFiles(name: string): Promise<AssetFileContent[]>;
    /** Every install row on an asset (API 1.9.0). `everyone` means no
     * rows exist and the whole library receives it. */
    installations(name: string): Promise<AssetInstallations>;
    /** Consolidate a duplicate cluster onto one survivor (API 1.9.0,
     * requires assets:consolidate — the dangerous grant). Moves every
     * `from` asset's install rows onto `into` (reach never shrinks;
     * an org-wide source makes the survivor org-wide), then RETIRES
     * the `from` assets — removed from the library, recoverable from
     * version history. Always confirm with the user first. */
    consolidate(req: {
      into: string;
      from: string[];
    }): Promise<ConsolidateResult>;
  };

  /** Requires usage:read. */
  readonly usage: {
    events(sinceDays: number): Promise<UsageEvent[]>;
    auditEvents(sinceDays: number): Promise<AuditEvent[]>;
    /** Events at or after `since` (RFC3339, e.g. an event's own
     * `timestamp`), newest first — the incremental-refresh companion to `events` (API 1.8.0). Cache a
     * window, then pull only what's newer than your newest cached event
     * (the server filter is `>=`, so the boundary event repeats — dedupe
     * on merge). Feature-detect: `typeof sx.usage.eventsSince ===
     * "function"` before use, for apps predating 1.8.0. */
    eventsSince(since: string): Promise<UsageEvent[]>;
    /** Audit events at or after `since` (RFC3339), newest first — the
     * incremental companion to `auditEvents` (API 1.8.0). */
    auditEventsSince(since: string): Promise<AuditEvent[]>;
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

  /** Requires usage:read (API 1.2.0) for team names and membership —
   * team management stays core. */
  readonly teams: {
    /** `assets` (names shared with the team, API 1.7.0) is populated
     * only when the extension ALSO holds assets:read — the same gate
     * repos.list() uses for the same class of data; otherwise it is []. */
    list(): Promise<{ name: string; members: string[]; assets: string[] }[]>;
  };

  /** Requires assets:read (API 1.7.0). Repository URL → asset names
   * scoped to install there — the repo-centric read repo-view
   * extensions build on. */
  readonly repos: {
    list(): Promise<{ url: string; assets: string[] }[]>;
  };

  /** Requires export (API 1.6.0). Bundles a collection's member assets
   * into a single file behind a native save dialog. "zip" carries every
   * asset (one folder each); the plugin formats ("claude-code", "codex",
   * "gemini") carry skill assets only. Resolves to the saved file path,
   * or "" when the user cancels the dialog. */
  readonly collections: {
    export(name: string, format: CollectionExportFormat): Promise<string>;
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

  /** Requires benchmarks (API 1.10.0). Per-asset benchmark records,
   * unified across vault types: .sx/benchmarks/<asset>.json on file
   * vaults, real benchmark rows on skills.new — so `list` returns
   * server-run benchmarks too. Records are capped per asset (newest
   * kept); `latest` is the dashboard's one-call bulk read. */
  readonly benchmarks: {
    list(assetName: string): Promise<BenchmarkRecord[]>;
    add(assetName: string, record: BenchmarkRecord): Promise<void>;
    latest(): Promise<Record<string, BenchmarkRecord>>;
  };

  /** Requires llm:use (API 1.9.0). One completion against whatever LLM
   * provider the USER configured in Settings — an installed CLI
   * (claude/codex/gemini), a local Ollama model, or any hosted API with
   * their own key. Provider-neutral by design: extensions must not
   * assume a vendor, a model, or latency. Rejects when no provider is
   * configured — surface that error, don't retry. */
  readonly llm: {
    complete(req: LLMRequest): Promise<LLMResult>;
    /** The configured provider id ("" when unconfigured) — for showing
     * setup hints before making a call. */
    provider(): Promise<string>;
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
  /** Requires views:collection (API 1.6.0): a tab on the Library's
   * collection view; mount receives the collection name. With no
   * registrations the collection view renders exactly as before. */
  registerCollectionView(spec: CollectionViewSpec): void;
  /** Requires views:team (API 1.7.0): a tab on the Library's team view;
   * mount receives the team name. With no registrations the team view
   * renders exactly as before. */
  registerTeamView(spec: TeamViewSpec): void;
  /** Requires views:repo (API 1.7.0): a tab on the Library's repository
   * view; mount receives the repository URL. With no registrations the
   * repo view renders exactly as before. */
  registerRepoView(spec: RepoViewSpec): void;
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
