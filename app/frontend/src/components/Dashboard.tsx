import PluginMount from "./PluginMount";
import { useSlot } from "../plugins/registry";

/**
 * The dashboard surface: a grid of widgets contributed by extensions
 * through the views:dashboard capability (the Library Dashboard built-in
 * provides the default set). Rendered as a Library scope so it lives
 * where users already navigate.
 */
export default function Dashboard() {
  const widgets = useSlot("dashboard-widget");

  if (widgets.length === 0) {
    return (
      <div className="m-5 rounded-lg border border-dashed border-line px-4 py-8 text-center text-sm text-ink-faint">
        No dashboard widgets — enable the Library Dashboard extension in
        Settings
      </div>
    );
  }

  return (
    <div className="grid grid-cols-1 gap-4 p-5 lg:grid-cols-2 xl:grid-cols-3">
      {widgets.map((w) => (
        <section
          key={w.pluginId + ":" + w.spec.id}
          data-dashboard-widget={w.spec.id}
          className="overflow-hidden rounded-xl border border-line bg-surface"
        >
          <h3 className="border-b border-line px-3 py-2 text-xs font-semibold tracking-wide text-ink-soft">
            {w.spec.title.toUpperCase()}
          </h3>
          <PluginMount pluginId={w.pluginId} mount={w.spec.mount} />
        </section>
      ))}
    </div>
  );
}
