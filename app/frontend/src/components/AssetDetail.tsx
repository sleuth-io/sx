import { useEffect, useState } from "react";
import ReactMarkdown from "react-markdown";
import {
  GetAsset,
  InstallAsset,
  RestoreRevision,
  SetCollectionMembership,
} from "../../wailsjs/go/main/App";
import type { main } from "../../wailsjs/go/models";
import FileRail from "./FileRail";
import TypeBadge from "./TypeBadge";

/**
 * Slide-over panel showing one asset: its files rendered as markdown, and a
 * quiet history control. Version vocabulary stays out of the primary UI —
 * history entries are just "revisions".
 */
export default function AssetDetail({
  name,
  collections,
  onClose,
  onEdit,
  onChanged,
  onToast,
  onCollectionsChanged,
}: {
  name: string;
  collections: main.Collection[];
  onClose: () => void;
  onEdit: () => void;
  onChanged: () => void;
  onToast: (message: string) => void;
  onCollectionsChanged: () => void;
}) {
  const [detail, setDetail] = useState<main.AssetDetail | null>(null);
  const [error, setError] = useState("");
  const [revision, setRevision] = useState("");
  const [activeFile, setActiveFile] = useState(0);

  useEffect(() => {
    setDetail(null);
    setError("");
    GetAsset(name, revision)
      .then((d) => {
        setDetail(d);
        setActiveFile(0);
      })
      .catch((e) => setError(String(e)));
  }, [name, revision]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const [restoring, setRestoring] = useState(false);
  const [installing, setInstalling] = useState(false);

  async function install() {
    setInstalling(true);
    try {
      const result = await InstallAsset(name);
      onToast(`Ready to use in ${result.clients.join(", ")}`);
    } catch (e) {
      onToast(String(e));
    } finally {
      setInstalling(false);
    }
  }

  async function toggleCollection(collection: string, member: boolean) {
    try {
      await SetCollectionMembership(collection, name, member);
      onCollectionsChanged();
    } catch (e) {
      onToast(String(e));
    }
  }

  const files = detail?.files ?? [];
  const isLatest =
    !detail || detail.version === detail.versions[detail.versions.length - 1];

  async function restore() {
    if (!detail) return;
    setRestoring(true);
    setError("");
    try {
      await RestoreRevision(detail.name, detail.version);
      setRevision("");
      onChanged();
    } catch (e) {
      setError(String(e));
    } finally {
      setRestoring(false);
    }
  }

  return (
    <div className="fixed inset-0 z-40 flex justify-end">
      <button
        aria-label="Close"
        className="absolute inset-0 bg-black/20"
        onClick={onClose}
      />
      <aside className="relative flex h-full w-[760px] max-w-[90vw] flex-col border-l border-line bg-surface shadow-xl">
        <header className="flex items-start gap-3 border-b border-line px-6 pb-4 pt-6">
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2">
              <h2 className="truncate text-base font-semibold">{name}</h2>
              {detail && (
                <TypeBadge type={detail.type} label={detail.typeLabel} />
              )}
            </div>
            {detail?.description && (
              <p className="mt-1 text-sm text-ink-soft">
                {detail.description}
              </p>
            )}
          </div>
          <button
            onClick={() => void install()}
            disabled={installing}
            className="rounded-lg bg-accent px-3 py-1.5 text-sm font-medium text-white transition hover:opacity-90 disabled:opacity-50"
            title="Install into Claude Code, Cursor, and other detected AI tools"
          >
            {installing ? "Setting up…" : "Use in my AI tools"}
          </button>
          <button
            onClick={onEdit}
            className="rounded-lg border border-line px-3 py-1.5 text-sm font-medium text-ink-soft transition hover:border-accent hover:text-ink"
          >
            Edit
          </button>
          <button
            onClick={onClose}
            className="rounded-lg px-2 py-1 text-sm text-ink-faint transition hover:bg-canvas hover:text-ink"
          >
            ✕
          </button>
        </header>

        {detail && detail.versions.length > 1 && (
          <div className="flex items-center gap-2 border-b border-line px-6 py-2.5 text-xs text-ink-soft">
            <span>History</span>
            <select
              value={detail.version}
              onChange={(e) => {
                const v = e.target.value;
                setRevision(
                  v === detail.versions[detail.versions.length - 1] ? "" : v,
                );
              }}
              className="rounded-md border border-line bg-canvas px-2 py-1 text-xs outline-none"
            >
              {[...detail.versions].reverse().map((v, i) => (
                <option key={v} value={v}>
                  {i === 0 ? "Current" : `Revision ${v}`}
                </option>
              ))}
            </select>
            {!isLatest && (
              <>
                <span className="rounded-full bg-amber-50 px-2 py-0.5 font-medium text-amber-700 dark:bg-amber-950 dark:text-amber-300">
                  Viewing an older revision
                </span>
                <div className="flex-1" />
                <button
                  onClick={() => void restore()}
                  disabled={restoring}
                  className="rounded-md bg-accent px-2.5 py-1 font-medium text-white transition hover:opacity-90 disabled:opacity-50"
                >
                  {restoring ? "Restoring…" : "Restore this revision"}
                </button>
              </>
            )}
          </div>
        )}

        {collections.length > 0 && (
          <div className="flex flex-wrap items-center gap-1.5 border-b border-line px-6 py-2.5">
            <span className="text-xs text-ink-soft">Collections</span>
            {collections.map((c) => {
              const member = (c.assets ?? []).includes(name);
              return (
                <button
                  key={c.name}
                  onClick={() => void toggleCollection(c.name, !member)}
                  className={`rounded-full px-2.5 py-0.5 text-xs font-medium transition ${
                    member
                      ? "bg-accent text-white"
                      : "border border-line text-ink-faint hover:text-ink"
                  }`}
                  title={member ? `Remove from ${c.name}` : `Add to ${c.name}`}
                >
                  {c.name}
                </button>
              );
            })}
          </div>
        )}

        <div className="flex min-h-0 flex-1">
          {files.length > 1 && (
            <FileRail
              files={files}
              active={activeFile}
              onSelect={setActiveFile}
            />
          )}
          <div className="min-w-0 flex-1 overflow-y-auto px-6 py-5">
            {error && (
              <div className="rounded-lg bg-danger-soft px-4 py-3 text-sm text-danger">
                {error}
              </div>
            )}
            {!detail && !error && (
              <div className="space-y-3">
                <div className="h-4 w-2/3 animate-pulse rounded bg-canvas" />
                <div className="h-4 w-full animate-pulse rounded bg-canvas" />
                <div className="h-4 w-5/6 animate-pulse rounded bg-canvas" />
              </div>
            )}
            {detail && files[activeFile] && (
              <FileView file={files[activeFile]} />
            )}
          </div>
        </div>
      </aside>
    </div>
  );
}

function FileView({ file }: { file: main.AssetFile }) {
  const isMarkdown = /\.(md|markdown)$/i.test(file.path);
  if (isMarkdown) {
    return (
      <article className="prose-sx">
        <ReactMarkdown>{file.content}</ReactMarkdown>
      </article>
    );
  }
  return (
    <pre className="overflow-x-auto rounded-lg border border-line bg-canvas p-4 font-mono text-xs leading-relaxed">
      {file.content}
    </pre>
  );
}
