import { useEffect, useRef, useState } from "react";
import {
  DiscardDraft,
  PublishDraft,
  UpdateDraft,
} from "../../wailsjs/go/main/App";
import type { main } from "../../wailsjs/go/models";
import { collectPublishWarnings, emitEvent } from "../plugins/events";
import type { PublishWarning } from "../plugins/api";
import FileRail from "./FileRail";
import MarkdownEditor from "./MarkdownEditor";
import { setPluginEditor } from "../plugins/sxapi";
import type { EditorView } from "@uiw/react-codemirror";

const TYPE_OPTIONS = [
  { key: "skill", label: "Skill" },
  { key: "rule", label: "Rule" },
  { key: "agent", label: "Agent" },
  { key: "command", label: "Command" },
];

/**
 * The draft editor: confirm what a drop is, edit its content, and publish.
 * Publishing is the only way anything reaches the library; "Save for later"
 * keeps the draft on this machine.
 */
export default function DraftSheet({
  draft: initial,
  onClose,
  onPublished,
}: {
  draft: main.Draft;
  onClose: () => void;
  onPublished: (name: string) => void;
}) {
  const [draft, setDraft] = useState(initial);
  const [activeFile, setActiveFile] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [dirty, setDirty] = useState(false);
  // Warnings from before-publish extension subscribers (the doctor hook).
  // Non-blocking: the user sees them and chooses "Publish anyway".
  const [warnings, setWarnings] = useState<PublishWarning[] | null>(null);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !busy) void saveAndClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  });

  // The live CodeMirror view, exposed to extensions (sx.editor) while
  // this sheet is open. Edits made through the view flow back into the
  // draft via updateFileContent, so extension edits behave exactly like
  // typing. Cleared on unmount so sx.editor throws instead of writing
  // into a dead view.
  const activeFileRef = useRef(activeFile);
  activeFileRef.current = activeFile;
  function registerEditorView(view: EditorView) {
    setPluginEditor({
      getValue: () => view.state.doc.toString(),
      getCursor: () => view.state.selection.main.head,
      getSelection: () => {
        const sel = view.state.selection.main;
        return {
          text: view.state.sliceDoc(sel.from, sel.to),
          from: sel.from,
          to: sel.to,
        };
      },
      replaceSelection: (text: string) => {
        const sel = view.state.selection.main;
        view.dispatch({ changes: { from: sel.from, to: sel.to, insert: text } });
        updateFileContent(activeFileRef.current, view.state.doc.toString());
      },
      replaceRange: (from: number, to: number, text: string) => {
        view.dispatch({ changes: { from, to, insert: text } });
        updateFileContent(activeFileRef.current, view.state.doc.toString());
      },
    });
  }
  useEffect(() => () => setPluginEditor(null), []);

  function update(patch: Partial<main.Draft>) {
    setDraft((d) => ({ ...d, ...patch }) as main.Draft);
    setDirty(true);
    // Any edit invalidates shown warnings: the next Publish re-runs the
    // checks instead of force-publishing stale state.
    setWarnings(null);
  }

  function updateFileContent(index: number, content: string) {
    // Functional update: the extension editor handle calls this from a
    // closure created renders ago, so it must not read `draft` directly.
    setDraft(
      (d) =>
        ({
          ...d,
          files: d.files.map((f, i) => (i === index ? { ...f, content } : f)),
        }) as main.Draft,
    );
    setDirty(true);
    setWarnings(null);
  }

  async function persist(): Promise<main.Draft | null> {
    try {
      const saved = await UpdateDraft(draft);
      setDirty(false);
      emitEvent("draft-saved", { draftId: saved.id });
      return saved;
    } catch (e) {
      setError(String(e));
      return null;
    }
  }

  async function saveAndClose() {
    // A failed save keeps the sheet open with the error visible instead of
    // silently discarding the edits.
    if (dirty && !(await persist())) return;
    onClose();
  }

  async function publish(force = false) {
    setBusy(true);
    setError("");
    setWarnings(null);
    const saved = dirty ? await persist() : draft;
    if (!saved) {
      setBusy(false);
      return;
    }
    // Extensions get one look before anything reaches the library. Their
    // warnings render in the sheet; "Publish anyway" re-runs with force.
    if (!force) {
      const found = await collectPublishWarnings({
        name: saved.name,
        description: saved.description,
        files: (saved.files ?? []).map((f) => ({
          path: f.path,
          content: f.content,
        })),
      });
      if (found.length > 0) {
        setWarnings(found.map((f) => f.warning));
        setBusy(false);
        return;
      }
    }
    try {
      const card = await PublishDraft(saved.id);
      emitEvent("asset-published", { name: card.name });
      onPublished(card.name);
    } catch (e) {
      setError(String(e));
      setBusy(false);
    }
  }

  async function discard() {
    setBusy(true);
    try {
      await DiscardDraft(draft.id);
      onClose();
    } catch (e) {
      setError(String(e));
      setBusy(false);
    }
  }

  const isUpdate = !!draft.targetAsset;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-6">
      <div className="absolute inset-0 bg-black/30" />
      <div className="relative flex h-[85vh] w-[980px] max-w-full flex-col overflow-hidden rounded-2xl border border-line bg-surface shadow-2xl">
        <header className="flex items-center gap-3 border-b border-line px-6 py-4">
          <div className="flex-1">
            <div className="flex items-center gap-2">
              <span className="rounded-full bg-amber-50 px-2 py-0.5 text-[11px] font-medium text-amber-700 dark:bg-amber-950 dark:text-amber-300">
                Draft
              </span>
              <span className="text-sm text-ink-soft">
                {isUpdate
                  ? `Changes to ${draft.targetAsset}`
                  : "New addition to your library"}
              </span>
            </div>
          </div>
          <button
            onClick={() => void saveAndClose()}
            disabled={busy}
            className="rounded-lg px-2 py-1 text-sm text-ink-faint transition hover:bg-canvas hover:text-ink"
            title="Save draft and close"
          >
            ✕
          </button>
        </header>

        <div className="grid gap-3 border-b border-line px-6 py-4 sm:grid-cols-[1fr_1fr_auto]">
          <label className="block">
            <span className="mb-1 block text-xs font-medium text-ink-soft">
              Name
            </span>
            <input
              value={draft.name}
              onChange={(e) => update({ name: e.target.value })}
              disabled={isUpdate || busy}
              className="w-full rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent disabled:text-ink-faint"
            />
          </label>
          <label className="block">
            <span className="mb-1 block text-xs font-medium text-ink-soft">
              What is it for?
            </span>
            <input
              value={draft.description}
              onChange={(e) => update({ description: e.target.value })}
              placeholder="One sentence your teammates (and their AI tools) will see"
              disabled={busy}
              className="w-full rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent"
            />
          </label>
          {!isUpdate && (
            <label className="block">
              <span className="mb-1 block text-xs font-medium text-ink-soft">
                Kind
              </span>
              <select
                value={draft.type}
                onChange={(e) => update({ type: e.target.value })}
                disabled={busy}
                className="h-[38px] w-full rounded-lg border border-line bg-canvas py-0 pl-3 pr-7 text-sm outline-none focus:border-accent"
              >
                {TYPE_OPTIONS.map((t) => (
                  <option key={t.key} value={t.key}>
                    {t.label}
                  </option>
                ))}
              </select>
            </label>
          )}
        </div>

        <div className="flex min-h-0 flex-1">
          {draft.files.length > 1 && (
            <FileRail
              files={draft.files}
              active={activeFile}
              onSelect={setActiveFile}
            />
          )}
          <div className="min-h-0 min-w-0 flex-1 p-4">
            {draft.files[activeFile] && (
              <MarkdownEditor
                value={draft.files[activeFile].content}
                onChange={(content) => updateFileContent(activeFile, content)}
                readOnly={busy}
                onView={registerEditorView}
              />
            )}
          </div>
        </div>

        {error && (
          <div className="mx-6 mb-3 rounded-lg bg-danger-soft px-4 py-3 text-sm text-danger">
            {error}
          </div>
        )}

        {warnings && warnings.length > 0 && (
          <div
            data-publish-warnings
            className="mx-6 mb-3 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm dark:border-amber-900 dark:bg-amber-950"
          >
            <div className="mb-1 font-medium text-amber-800 dark:text-amber-200">
              Worth a look before publishing
            </div>
            <ul className="space-y-1 text-amber-800 dark:text-amber-200">
              {warnings.map((w, i) => (
                <li key={i}>
                  • {w.message}
                  {w.detail && (
                    <span className="text-amber-700/80 dark:text-amber-300/70">
                      {" "}
                      — {w.detail}
                    </span>
                  )}
                </li>
              ))}
            </ul>
          </div>
        )}

        <footer className="flex items-center gap-2 border-t border-line px-6 py-4">
          <button
            onClick={() => void discard()}
            disabled={busy}
            className="rounded-lg px-3 py-2 text-sm font-medium text-danger transition hover:bg-danger-soft disabled:opacity-50"
          >
            Discard
          </button>
          <div className="flex-1" />
          <button
            onClick={() => void saveAndClose()}
            disabled={busy}
            className="rounded-lg border border-line px-4 py-2 text-sm font-medium text-ink-soft transition hover:text-ink disabled:opacity-50"
          >
            Save for later
          </button>
          <button
            onClick={() => void publish(warnings !== null)}
            disabled={busy || !draft.name.trim()}
            className="rounded-lg bg-accent px-5 py-2 text-sm font-medium text-white transition hover:opacity-90 disabled:opacity-50"
          >
            {busy
              ? "Publishing…"
              : warnings && warnings.length > 0
                ? "Publish anyway"
                : isUpdate
                  ? "Publish changes"
                  : "Publish"}
          </button>
        </footer>
      </div>
    </div>
  );
}
