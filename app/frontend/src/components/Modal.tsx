import { ReactNode, useEffect } from "react";

/** Centered modal shell with backdrop + Escape-to-close. */
export default function Modal({
  title,
  children,
  onClose,
  width = "w-[420px]",
}: {
  title: string;
  children: ReactNode;
  onClose: () => void;
  width?: string;
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-6">
      <button
        aria-label="Close"
        className="absolute inset-0 bg-black/30"
        onClick={onClose}
      />
      <div
        className={`relative ${width} max-w-full rounded-2xl border border-line bg-surface shadow-2xl`}
      >
        <header className="flex items-center border-b border-line px-5 py-3.5">
          <h2 className="flex-1 text-sm font-semibold">{title}</h2>
          <button
            onClick={onClose}
            className="rounded-lg px-2 py-0.5 text-sm text-ink-faint transition hover:bg-canvas hover:text-ink"
          >
            ✕
          </button>
        </header>
        <div className="px-5 py-4">{children}</div>
      </div>
    </div>
  );
}
