import { useState } from "react";
import {
  CreateBlankDraft,
  PickFilesForDraft,
  PickFolderForDraft,
} from "../../wailsjs/go/main/App";
import type { main } from "../../wailsjs/go/models";
import Modal from "./Modal";

const KINDS = [
  { key: "skill", label: "Skill" },
  { key: "rule", label: "Rule" },
  { key: "agent", label: "Agent" },
  { key: "command", label: "Command" },
];

/**
 * One dialog for every way an asset starts life: bring existing files
 * (markdown / zip / folder — browse or just drop them), or write from
 * scratch. Whatever the source, the result is the same draft editor.
 */
export default function AddAssetModal({
  onClose,
  onDraft,
  onError,
}: {
  onClose: () => void;
  onDraft: (draft: main.Draft) => void;
  onError: (message: string) => void;
}) {
  const [kind, setKind] = useState("skill");
  const [busy, setBusy] = useState(false);

  function run(pick: () => Promise<main.Draft>) {
    setBusy(true);
    pick()
      .then(onDraft)
      .catch((e) => {
        if (!String(e).includes("cancelled")) onError(String(e));
      })
      .finally(() => setBusy(false));
  }

  return (
    <Modal title="New asset" onClose={onClose} width="w-[480px]">
      <div className="rounded-xl border-2 border-dashed border-line px-6 py-7 text-center">
        <div className="text-2xl">📄</div>
        <div className="mt-2 text-sm font-medium">
          Bring what you already have
        </div>
        <div className="mt-1 text-xs text-ink-faint">
          Drop markdown files, a folder, or a .zip anywhere in this window —
          or browse for them.
        </div>
        <div className="mt-4 flex justify-center gap-2">
          <button
            onClick={() => run(PickFilesForDraft)}
            disabled={busy}
            className="rounded-lg border border-line px-3.5 py-2 text-sm font-medium text-ink-soft transition hover:border-accent hover:text-ink disabled:opacity-50"
          >
            Choose files…
          </button>
          <button
            onClick={() => run(PickFolderForDraft)}
            disabled={busy}
            className="rounded-lg border border-line px-3.5 py-2 text-sm font-medium text-ink-soft transition hover:border-accent hover:text-ink disabled:opacity-50"
          >
            Choose a folder…
          </button>
        </div>
      </div>

      <div className="my-4 flex items-center gap-3 text-xs text-ink-faint">
        <span className="h-px flex-1 bg-line" />
        or start from scratch
        <span className="h-px flex-1 bg-line" />
      </div>

      <div className="flex h-9 items-center gap-2">
        <select
          value={kind}
          onChange={(e) => setKind(e.target.value)}
          disabled={busy}
          className="h-full rounded-lg border border-line bg-canvas px-3 text-sm outline-none focus:border-accent"
        >
          {KINDS.map((k) => (
            <option key={k.key} value={k.key}>
              {k.label}
            </option>
          ))}
        </select>
        <button
          onClick={() => run(() => CreateBlankDraft(kind))}
          disabled={busy}
          className="h-full flex-1 rounded-lg bg-accent px-4 text-sm font-medium text-white transition hover:opacity-90 disabled:opacity-50"
        >
          {busy ? "Working…" : "Start writing"}
        </button>
      </div>
    </Modal>
  );
}
