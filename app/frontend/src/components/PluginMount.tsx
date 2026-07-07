import { useEffect, useRef } from "react";
import { mountEntry } from "../plugins/sxapi";
import type { ViewMount } from "../plugins/api";

/**
 * Bridge between React and the extension ViewMount contract: renders the
 * bare element an extension owns, mounts on attach, disposes on unmount.
 * Extensions never see React; the host still tears the mount down if the
 * extension is disabled while visible.
 */
export default function PluginMount({
  pluginId,
  mount,
  className,
}: {
  pluginId: string;
  mount: (view: ViewMount) => void;
  className?: string;
}) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!ref.current) return;
    return mountEntry(pluginId, ref.current, mount);
  }, [pluginId, mount]);

  return <div ref={ref} className={className} />;
}
