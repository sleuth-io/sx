import { useState } from "react";

/**
 * A persisted, drag-resizable panel width. Returns the current width and a
 * mousedown handler for the resize handle. `invert` flips the drag
 * direction for panels anchored to the right edge (dragging their left
 * edge outward grows them).
 */
export default function usePanelSize(
  key: string,
  fallback: number,
  min: number,
  max: number,
  invert = false,
): [number, (e: React.MouseEvent) => void] {
  const [size, setSize] = useState(() => {
    const raw = Number(localStorage.getItem(key));
    return raw >= min && raw <= max ? raw : fallback;
  });

  const startResize = (e: React.MouseEvent) => {
    e.preventDefault();
    const startX = e.clientX;
    const start = size;
    const onMove = (ev: MouseEvent) => {
      const delta = invert ? startX - ev.clientX : ev.clientX - startX;
      const next = Math.min(max, Math.max(min, start + delta));
      setSize(next);
      localStorage.setItem(key, String(Math.round(next)));
    };
    const onUp = () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
    };
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
  };

  return [size, startResize];
}
