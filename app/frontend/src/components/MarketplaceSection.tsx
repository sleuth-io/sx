import { useCallback, useEffect, useRef, useState } from "react";
import {
  GetMarketplaceURL,
  InstallMarketplaceExtension,
  SearchMarketplace,
  SetMarketplaceURL,
  SetPluginDecision,
} from "../../wailsjs/go/main/App";
import type { main } from "../../wailsjs/go/models";
import { refreshVaultPlugins } from "../plugins/boot";
import { enablePlugin, listPlugins } from "../plugins/host";
import { currentPolicy, policyBlocks, recordConsent } from "../plugins/policy";

/**
 * The marketplace browser inside Settings → Extensions: search a shared
 * extensions repository (itself just an sx vault of app-plugin assets) and
 * install entries into this library. Installing only copies the asset —
 * the extension arrives disabled and enabling it still walks the org
 * policy check and the consent sheet like any other vault extension.
 */
export default function MarketplaceSection() {
  // The full catalog, fetched ONCE per open (and per source change).
  // Each backend fetch clones/updates the repo and unpacks every bundle,
  // so keystrokes must never reach it — filtering is client-side.
  const [catalog, setCatalog] = useState<main.MarketplaceExtension[] | null>(
    null,
  );
  const [query, setQuery] = useState("");
  const [searching, setSearching] = useState(false);
  const [installing, setInstalling] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [repoURL, setRepoURL] = useState("");
  const [editingRepo, setEditingRepo] = useState(false);
  const [opened, setOpened] = useState(false);
  const fetchSeq = useRef(0);

  useEffect(() => {
    GetMarketplaceURL()
      .then(setRepoURL)
      .catch(() => {});
  }, []);

  const fetchCatalog = useCallback(async () => {
    const seq = ++fetchSeq.current;
    setSearching(true);
    setError("");
    try {
      const found = await SearchMarketplace("");
      if (seq === fetchSeq.current) setCatalog(found);
    } catch (e) {
      if (seq === fetchSeq.current) {
        setCatalog([]);
        setError(String(e));
      }
    } finally {
      if (seq === fetchSeq.current) setSearching(false);
    }
  }, []);

  async function open() {
    setOpened(true);
    await fetchCatalog();
  }

  const needle = query.trim().toLowerCase();
  const results =
    catalog?.filter(
      (r) =>
        !needle ||
        r.name.toLowerCase().includes(needle) ||
        r.id.toLowerCase().includes(needle) ||
        r.description.toLowerCase().includes(needle),
    ) ?? null;

  async function install(entry: main.MarketplaceExtension) {
    setInstalling(entry.assetName);
    setError("");
    setNotice("");
    try {
      await InstallMarketplaceExtension(entry.assetName);
      await refreshVaultPlugins();
      // Install means "I want this running": the permission list was on
      // the card the user just clicked, so that click is the consent —
      // enable right away instead of parking it disabled. Org policy
      // still has the final word.
      const blocked = policyBlocks(entry.id, false);
      const manifest = listPlugins().find(
        (p) => p.manifest.id === entry.id,
      )?.manifest;
      if (!blocked && manifest) {
        await recordConsent(manifest);
        await enablePlugin(entry.id);
        await SetPluginDecision(entry.id, true);
        setNotice(`${entry.name} is installed and running.`);
      } else {
        setNotice(
          blocked
            ? `${entry.name} was added to the library, but ${blocked}`
            : `${entry.name} added to this library — enable it above when you're ready.`,
        );
      }
      setCatalog(
        (prev) =>
          prev?.map((r) =>
            r.assetName === entry.assetName ? { ...r, installed: true } : r,
          ) ?? prev,
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
      if (opened) await fetchCatalog();
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
        Extensions shared in a public repository. Installing copies one into
        this library and turns it on — each entry lists what it can access.
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
                {r.installed ? (
                  <span className="shrink-0 text-xs font-medium text-emerald-600 dark:text-emerald-400">
                    ✓ In library
                  </span>
                ) : (
                  <button
                    onClick={() => void install(r)}
                    disabled={installing !== ""}
                    className="shrink-0 rounded-lg bg-accent px-3 py-1.5 text-xs font-medium text-white transition hover:opacity-90 disabled:opacity-50"
                  >
                    {installing === r.assetName ? "Installing…" : "Install"}
                  </button>
                )}
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
