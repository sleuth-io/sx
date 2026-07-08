import { useEffect, useState } from "react";
import CodeMirror from "@uiw/react-codemirror";
import { markdown } from "@codemirror/lang-markdown";
import { EditorView } from "@uiw/react-codemirror";

const baseTheme = EditorView.theme({
  "&": { fontSize: "12.5px", height: "100%" },
  ".cm-content": {
    fontFamily:
      'ui-monospace, "SF Mono", SFMono-Regular, Menlo, Consolas, monospace',
    padding: "12px 0",
  },
  ".cm-gutters": {
    fontFamily:
      'ui-monospace, "SF Mono", SFMono-Regular, Menlo, Consolas, monospace',
    border: "none",
    background: "transparent",
    color: "var(--color-ink-faint)",
  },
  "&.cm-focused": { outline: "none" },
  ".cm-scroller": { overflow: "auto" },
});

function usePrefersDark() {
  const [dark, setDark] = useState(
    () => window.matchMedia("(prefers-color-scheme: dark)").matches,
  );
  useEffect(() => {
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = (e: MediaQueryListEvent) => setDark(e.matches);
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, []);
  return dark;
}

/** CodeMirror-backed markdown editor with line numbers and highlighting. */
export default function MarkdownEditor({
  value,
  onChange,
  readOnly = false,
  onView,
}: {
  value: string;
  onChange: (value: string) => void;
  readOnly?: boolean;
  /** Receives the live CodeMirror view (for the extension editor API). */
  onView?: (view: EditorView) => void;
}) {
  const dark = usePrefersDark();
  return (
    <CodeMirror
      value={value}
      onChange={onChange}
      readOnly={readOnly}
      onCreateEditor={(view) => onView?.(view)}
      theme={dark ? "dark" : "light"}
      extensions={[markdown(), baseTheme, EditorView.lineWrapping]}
      basicSetup={{
        lineNumbers: true,
        foldGutter: false,
        highlightActiveLine: !readOnly,
        highlightActiveLineGutter: !readOnly,
      }}
      height="100%"
      className="h-full overflow-hidden rounded-lg border border-line"
    />
  );
}
