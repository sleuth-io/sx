import { useState } from "react";
import { CreateCollection } from "../../wailsjs/go/main/App";
import Modal from "./Modal";

/** Create-collection dialog. */
export default function CollectionModal({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated: (name: string) => void;
}) {
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function create() {
    if (!name.trim()) return;
    setBusy(true);
    setError("");
    try {
      const c = await CreateCollection(name.trim());
      onCreated(c.name);
    } catch (e) {
      setError(String(e));
      setBusy(false);
    }
  }

  return (
    <Modal title="New collection" onClose={onClose}>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void create();
        }}
      >
        <label className="block">
          <span className="mb-1 block text-xs font-medium text-ink-soft">
            Name
          </span>
          <input
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="onboarding, writing, backend…"
            disabled={busy}
            className="w-full rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent"
          />
        </label>
        <p className="mt-2 text-xs text-ink-faint">
          Collections group related assets so they can be browsed and set up
          together. An asset can be in any number of them.
        </p>
        {error && (
          <div className="mt-3 rounded-lg bg-danger-soft px-3 py-2 text-sm text-danger">
            {error}
          </div>
        )}
        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            disabled={busy}
            className="rounded-lg border border-line px-4 py-2 text-sm font-medium text-ink-soft transition hover:text-ink"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={busy || !name.trim()}
            className="rounded-lg bg-accent px-4 py-2 text-sm font-medium text-white transition hover:opacity-90 disabled:opacity-50"
          >
            {busy ? "Creating…" : "Create"}
          </button>
        </div>
      </form>
    </Modal>
  );
}
