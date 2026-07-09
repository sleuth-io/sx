import { useCallback, useEffect, useState, useSyncExternalStore } from "react";
import {
  CanInstallForEveryone,
  GetMarketplaceURL,
  InstallMarketplaceExtension,
  SetMarketplaceURL,
  SetPluginDecision,
  VaultSupportsExtensions,
} from "../../wailsjs/go/main/App";
import type { main } from "../../wailsjs/go/models";
import { refreshVaultPlugins } from "../plugins/boot";
import {
  compareVersions,
  enablePlugin,
  listPlugins,
  subscribeHost,
} from "../plugins/host";
import { currentPolicy, policyBlocks, recordConsent } from "../plugins/policy";
import {
  applyUpdate,
  getCatalog,
  loadCatalog,
  subscribeCatalog,
} from "../plugins/updates";

/** Compact install-count label: 950 → "950", 1234 → "1.2k". */
function fmtInstalls(n: number): string {
  if (n < 1000) return String(n);
  const k = (n / 1000).toFixed(n < 10_000 ? 1 : 0).replace(/\.0$/, "");
  return `${k}k`;
}

/**
 * The marketplace browser inside Settings → Extensions: search a shared
 * extensions repository (itself just an sx vault of app-plugin assets) and
 * install entries into this library. Installing only copies the asset —
 * the extension arrives disabled and enabling it still walks the org
 * policy check and the consent sheet like any other vault extension.
 */
export default function MarketplaceSection() {
  // The catalog comes from the shared store (one fetch serves this
  // panel AND the Extensions list's update check). Each backend fetch
  // clones/updates the repo and unpacks every bundle, so keystrokes
  // must never reach it — filtering is client-side.
  const catalog = useSyncExternalStore(subscribeCatalog, getCatalog);
  const [query, setQuery] = useState("");
  const [searching, setSearching] = useState(false);
  const [installing, setInstalling] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [repoURL, setRepoURL] = useState("");
  const [editingRepo, setEditingRepo] = useState(false);
  const [opened, setOpened] = useState(false);
  // Reactive installed-plugin view: installed/update states derive from
  // it, so an install or update from EITHER panel flips this one too.
  const installedPlugins = useSyncExternalStore(subscribeHost, listPlugins);

  // A library whose server can't store app-plugin assets (skills.new
  // until P5) can browse but not install.
  const [supported, setSupported] = useState(true);
  // Whether the caller may install library-wide (org-admins on governed
  // vaults). Everyone may always install just for themselves.
  const [canEveryone, setCanEveryone] = useState(false);
  // Which entry's install menu is open ("" = none).
  const [menuFor, setMenuFor] = useState("");
  useEffect(() => {
    GetMarketplaceURL()
      .then(setRepoURL)
      .catch(() => {});
    VaultSupportsExtensions()
      .then(setSupported)
      // Fail CLOSED like the backend gate.
      .catch(() => setSupported(false));
    CanInstallForEveryone()
      .then(setCanEveryone)
      .catch(() => setCanEveryone(false));
  }, []);

  // The install-options menu dismisses like a normal popover: Escape
  // closes it (the backdrop below handles click-away).
  useEffect(() => {
    if (!menuFor) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setMenuFor("");
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [menuFor]);

  const fetchCatalog = useCallback(async (force = false) => {
    setSearching(true);
    setError("");
    try {
      await loadCatalog(force);
    } catch (e) {
      setError(String(e));
    } finally {
      setSearching(false);
    }
  }, []);

  async function open() {
    setOpened(true);
    await fetchCatalog();
  }

  // Sort by name (backend default) or by global install count, when the
  // marketplace publishes one.
  const [sortBy, setSortBy] = useState<"name" | "installs">("name");
  const hasCounts = (catalog ?? []).some((r) => r.installs > 0);

  const needle = query.trim().toLowerCase();
  const filtered =
    catalog?.filter(
      (r) =>
        !needle ||
        r.name.toLowerCase().includes(needle) ||
        r.id.toLowerCase().includes(needle) ||
        r.description.toLowerCase().includes(needle),
    ) ?? null;
  const results =
    filtered && sortBy === "installs"
      ? [...filtered].sort((a, b) => b.installs - a.installs)
      : filtered;

  async function install(
    entry: main.MarketplaceExtension,
    scope: "me" | "org",
  ) {
    setMenuFor("");
    setInstalling(entry.assetName);
    setError("");
    setNotice("");
    try {
      await InstallMarketplaceExtension(entry.assetName, scope);
      await refreshVaultPlugins();
      // Install means "I want this running": the permission list was on
      // the card the user just clicked, so that click is the consent —
      // enable right away instead of parking it disabled. Org policy
      // still has the final word.
      const blocked = policyBlocks(entry.id, false);
      const manifest = listPlugins().find(
        (p) => p.manifest.id === entry.id,
      )?.manifest;
      const where = scope === "me" ? "for you" : "for everyone";
      if (!blocked && manifest) {
        await recordConsent(manifest);
        await enablePlugin(entry.id);
        await SetPluginDecision(entry.id, true);
        setNotice(`${entry.name} is installed ${where} and running.`);
      } else {
        setNotice(
          blocked
            ? `${entry.name} was added to the library, but ${blocked}`
            : `${entry.name} installed ${where} — enable it above when you're ready.`,
        );
      }
    } catch (e) {
      setError(String(e));
    } finally {
      setInstalling("");
    }
  }

  async function update(entry: main.MarketplaceExtension) {
    setInstalling(entry.assetName);
    setError("");
    setNotice("");
    try {
      const outcome = await applyUpdate(entry);
      setNotice(
        outcome.state === "needs-consent"
          ? `${entry.name} updated to v${entry.version} — its permissions changed, enable it above to review them.`
          : `${entry.name} updated to v${entry.version}.`,
      );
    } catch (e) {
      setError(String(e));
    } finally {
      setInstalling("");
    }
  }

  async function saveRepoURL() {
    setEditingRepo(false);
    setError("");
    try {
      await SetMarketplaceURL(repoURL);
      const effective = await GetMarketplaceURL();
      setRepoURL(effective);
      if (opened) await fetchCatalog(true);
    } catch (e) {
      setError(String(e));
    }
  }

  if (currentPolicy().mode === "disabled") return null;

  return (
    <div className="mt-6" data-marketplace>
      <div className="mb-1 flex items-center justify-between">
        <span className="text-xs font-semibold tracking-wide text-ink-faint">
          MARKETPLACE
        </span>
        {!opened && (
          <button
            onClick={() => void open()}
            className="rounded-lg border border-line px-2.5 py-1 text-xs font-medium text-ink-soft transition hover:border-accent hover:text-ink"
          >
            Browse marketplace…
          </button>
        )}
      </div>
      <p className="mb-2 text-xs text-ink-faint">
        Extensions shared in a public repository. Install turns one on just
        for you{canEveryone ? ", or for the whole library from the menu" : ""}
        — each entry lists what it can access.
      </p>

      {opened && (
        <>
          <div className="mb-2 flex items-center gap-2">
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search extensions…"
              data-marketplace-search
              className="w-full rounded-lg border border-line bg-canvas px-3 py-1.5 text-sm outline-none transition focus:border-accent"
            />
            {hasCounts && (
              <select
                value={sortBy}
                onChange={(e) =>
                  setSortBy(e.target.value as "name" | "installs")
                }
                aria-label="Sort extensions"
                data-marketplace-sort
                className="shrink-0 rounded-lg border border-line bg-canvas px-2 py-1.5 text-xs text-ink-soft outline-none transition focus:border-accent"
              >
                <option value="name">A–Z</option>
                <option value="installs">Most installed</option>
              </select>
            )}
          </div>

          {searching && results === null && (
            <p className="py-2 text-xs text-ink-faint">
              Fetching the marketplace…
            </p>
          )}

          {results !== null && results.length === 0 && !searching && !error && (
            <p className="py-2 text-xs text-ink-faint">
              No extensions match{query ? ` “${query}”` : ""}.
            </p>
          )}

          <ul className="space-y-2">
            {(results ?? []).map((r) => (
              <li
                key={r.assetName}
                data-marketplace-entry={r.assetName}
                className="flex items-center gap-3 rounded-xl border border-line p-3"
              >
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2 text-sm font-medium">
                    {r.name}
                    <span className="text-[10px] text-ink-faint">
                      v{r.version}
                      {r.author ? ` · ${r.author}` : ""}
                      {r.installs > 0 && (
                        <span
                          title={`${r.installs.toLocaleString()} installs across all sx libraries`}
                          data-install-count={r.id}
                        >
                          {` · ⇩ ${fmtInstalls(r.installs)}`}
                        </span>
                      )}
                    </span>
                  </div>
                  {r.description && (
                    <p className="mt-0.5 text-xs text-ink-faint">
                      {r.description}
                    </p>
                  )}
                  {r.permissions.length > 0 && (
                    <p className="mt-1 flex flex-wrap gap-1">
                      {r.permissions.map((p) => (
                        <span
                          key={p}
                          className="rounded-full border border-line px-1.5 text-[10px] text-ink-faint"
                        >
                          {p}
                        </span>
                      ))}
                    </p>
                  )}
                </div>
                {(() => {
                  const mine = installedPlugins.find(
                    (p) => !p.builtIn && p.manifest.id === r.id,
                  );
                  // Derived from the LIVE host registry only — the
                  // catalog deliberately carries no installed flag: a
                  // fetched flag is stamped against whichever library
                  // was current at fetch time, and the catalog cache
                  // outlives library switches, so trusting one showed
                  // "In library" for extensions the newly selected
                  // library doesn't have.
                  const installed = !!mine;
                  if (
                    installed &&
                    mine &&
                    compareVersions(mine.manifest.version, r.version) < 0
                  ) {
                    return (
                      <button
                        onClick={() => void update(r)}
                        disabled={installing !== ""}
                        title={`v${mine.manifest.version} → v${r.version}`}
                        className="shrink-0 rounded-lg border border-accent px-3 py-1.5 text-xs font-medium text-accent transition hover:bg-accent-soft disabled:opacity-50"
                      >
                        {installing === r.assetName ? "Updating…" : "Update"}
                      </button>
                    );
                  }
                  if (installed) {
                    // A personal install is yours alone; only sharing
                    // (org or team) earns the library-wide badge.
                    const personalOnly =
                      !!mine?.scope?.personal && !mine?.scope?.shared;
                    return (
                      <span className="shrink-0 text-xs font-medium text-emerald-600 dark:text-emerald-400">
                        {personalOnly ? "✓ Installed for you" : "✓ In library"}
                      </span>
                    );
                  }
                  return (
                    <span className="relative flex shrink-0 items-center">
                      <button
                        onClick={() => void install(r, "me")}
                        disabled={installing !== "" || !supported}
                        title={
                          supported
                            ? "Install just for you — teammates aren't affected"
                            : "This library's server can't store extensions yet — switch to a git or local library to install"
                        }
                        className={`rounded-lg bg-accent px-3 py-1.5 text-xs font-medium text-white transition hover:opacity-90 disabled:opacity-50 ${
                          canEveryone ? "rounded-r-none" : ""
                        }`}
                      >
                        {installing === r.assetName
                          ? "Installing…"
                          : "Install"}
                      </button>
                      {canEveryone && (
                        <button
                          onClick={() =>
                            setMenuFor(menuFor === r.assetName ? "" : r.assetName)
                          }
                          disabled={installing !== "" || !supported}
                          aria-label={`More install options for ${r.name}`}
                          aria-expanded={menuFor === r.assetName}
                          data-install-menu={r.assetName}
                          className="rounded-lg rounded-l-none border-l border-white/30 bg-accent px-1.5 py-1.5 text-xs font-medium text-white transition hover:opacity-90 disabled:opacity-50"
                        >
                          ▾
                        </button>
                      )}
                      {menuFor === r.assetName && (
                        <>
                          {/* Invisible click-away backdrop, under the menu. */}
                          <span
                            className="fixed inset-0 z-[5]"
                            onMouseDown={() => setMenuFor("")}
                          />
                          <span className="absolute right-0 top-full z-10 mt-1 w-44 rounded-lg border border-line bg-surface p-1 shadow-lg">
                            <button
                              onClick={() => void install(r, "org")}
                              className="block w-full rounded-md px-2 py-1.5 text-left text-xs text-ink transition hover:bg-accent-soft"
                            >
                              Install for everyone
                              <span className="block text-[10px] text-ink-faint">
                                Shares it with the whole library
                              </span>
                            </button>
                          </span>
                        </>
                      )}
                    </span>
                  );
                })()}
              </li>
            ))}
          </ul>

          <div className="mt-2 flex items-center gap-2 text-[11px] text-ink-faint">
            {editingRepo ? (
              <>
                <input
                  value={repoURL}
                  onChange={(e) => setRepoURL(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") void saveRepoURL();
                  }}
                  data-marketplace-repo
                  className="w-full rounded-md border border-line bg-canvas px-2 py-1 font-mono text-[11px] outline-none focus:border-accent"
                />
                <button
                  onClick={() => void saveRepoURL()}
                  className="shrink-0 font-medium text-accent"
                >
                  Save
                </button>
              </>
            ) : (
              <>
                <span className="truncate" title={repoURL}>
                  Source: {repoURL}
                </span>
                <button
                  onClick={() => setEditingRepo(true)}
                  className="shrink-0 font-medium text-ink-soft transition hover:text-ink"
                >
                  Change
                </button>
              </>
            )}
          </div>
        </>
      )}

      {notice && (
        <div className="mt-2 rounded-lg bg-accent-soft px-3 py-2 text-xs text-accent">
          {notice}
        </div>
      )}
      {error && (
        <div className="mt-2 rounded-lg bg-danger-soft px-3 py-2 text-xs text-danger">
          {error}
        </div>
      )}
    </div>
  );
}
