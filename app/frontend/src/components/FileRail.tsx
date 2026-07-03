/**
 * Vertical file list, mirroring the skills.new web app: an uppercase FILES
 * label and one row per file, directory prefix de-emphasized.
 */
export default function FileRail({
  files,
  active,
  onSelect,
}: {
  files: { path: string }[];
  active: number;
  onSelect: (index: number) => void;
}) {
  return (
    <nav className="w-52 shrink-0 overflow-y-auto border-r border-line">
      <div className="px-4 pb-1 pt-3 text-[11px] font-semibold tracking-wide text-ink-faint">
        FILES
      </div>
      <ul>
        {files.map((f, i) => {
          const slash = f.path.lastIndexOf("/");
          const dir = slash >= 0 ? f.path.slice(0, slash + 1) : "";
          const base = slash >= 0 ? f.path.slice(slash + 1) : f.path;
          return (
            <li key={f.path}>
              <button
                onClick={() => onSelect(i)}
                title={f.path}
                className={`w-full truncate px-4 py-1.5 text-left text-xs transition ${
                  i === active
                    ? "bg-accent-soft font-medium text-accent"
                    : "text-ink-soft hover:bg-canvas hover:text-ink"
                }`}
              >
                {dir && <span className="text-ink-faint">{dir}</span>}
                {base}
              </button>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
