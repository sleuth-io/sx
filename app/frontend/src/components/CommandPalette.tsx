import {
  useEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import { useSlot } from "../plugins/registry";
import { editorOpen, subscribeEditor } from "../plugins/sxapi";
import type { CommandSpec } from "../plugins/api";

/**
 * Command palette (⌘K): core commands plus everything extensions
 * register through the `commands` capability. Fuzzy-filters as you type;
 * Enter runs the selection.
 */
export default function CommandPalette({
  open,
  onClose,
  coreCommands,
}: {
  open: boolean;
  onClose: () => void;
  coreCommands: CommandSpec[];
}) {
  const [query, setQuery] = useState("");
  const [index, setIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const pluginCommands = useSlot("command");
  const hasEditor = useSyncExternalStore(subscribeEditor, editorOpen);
  const usableCommands = pluginCommands.filter(
    (c) => c.spec.context !== "editor" || hasEditor,
  );

  const commands = useMemo(() => {
    const all: (CommandSpec & { ownerKey: string })[] = [
      ...coreCommands.map((c) => ({ ...c, ownerKey: "core:" + c.id })),
      // Editor-scoped commands only surface while a draft editor is
      // open — running them anywhere else can only throw.
      ...usableCommands.map((e) => ({
        ...e.spec,
        ownerKey: e.pluginId + ":" + e.spec.id,
      })),
    ];
    const q = query.trim().toLowerCase();
    if (!q) return all;
    // Simple subsequence match keeps the palette dependency-free.
    return all.filter((c) => {
      const title = c.title.toLowerCase();
      let i = 0;
      for (const ch of q) {
        i = title.indexOf(ch, i);
        if (i < 0) return false;
        i++;
      }
      return true;
    });
  }, [coreCommands, usableCommands, query]);

  useEffect(() => {
    if (open) {
      setQuery("");
      setIndex(0);
      setTimeout(() => inputRef.current?.focus(), 0);
    }
  }, [open]);

  useEffect(() => {
    setIndex((i) => Math.min(i, Math.max(0, commands.length - 1)));
  }, [commands.length]);

  if (!open) return null;

  function run(cmd: CommandSpec | undefined) {
    if (!cmd) return;
    onClose();
    void cmd.run();
  }

  return (
    <div
      className="fixed inset-0 z-[80] flex items-start justify-center bg-black/30 pt-[15vh]"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="w-[560px] overflow-hidden rounded-xl border border-line bg-surface shadow-2xl">
        <input
          ref={inputRef}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") onClose();
            if (e.key === "ArrowDown") {
              e.preventDefault();
              setIndex((i) => Math.min(i + 1, commands.length - 1));
            }
            if (e.key === "ArrowUp") {
              e.preventDefault();
              setIndex((i) => Math.max(i - 1, 0));
            }
            if (e.key === "Enter") run(commands[index]);
          }}
          placeholder="Type a command…"
          className="w-full border-b border-line bg-transparent px-4 py-3 text-sm outline-none"
        />
        <ul className="max-h-72 overflow-y-auto py-1">
          {commands.length === 0 && (
            <li className="px-4 py-3 text-sm text-ink-faint">
              No matching commands
            </li>
          )}
          {commands.map((c, i) => (
            <li key={c.ownerKey}>
              <button
                data-command-id={c.id}
                onClick={() => run(c)}
                onMouseEnter={() => setIndex(i)}
                className={`w-full px-4 py-2 text-left text-sm transition ${
                  i === index ? "bg-accent-soft text-accent" : ""
                }`}
              >
                {c.title}
              </button>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
