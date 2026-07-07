// Extension event bus. Core UI emits; extensions subscribe through the
// SxAPI. Subscriptions are owned by plugin id for complete teardown.
// before-publish is special: subscribers can contribute warnings that
// render in the publish sheet before the user confirms.

import type {
  BeforePublishContext,
  EventMap,
  PublishWarning,
} from "./api";

type Handler = (payload: unknown) => void;
type BeforePublishHandler = (
  ctx: BeforePublishContext,
) => PublishWarning[] | Promise<PublishWarning[]> | void;

const handlers = new Map<string, { pluginId: string; fn: Handler }[]>();
const beforePublish: { pluginId: string; fn: BeforePublishHandler }[] = [];

export function subscribeEvent(
  pluginId: string,
  event: keyof EventMap,
  fn: Handler,
): void {
  const list = handlers.get(event) ?? [];
  handlers.set(event, [...list, { pluginId, fn }]);
}

export function subscribeBeforePublish(
  pluginId: string,
  fn: BeforePublishHandler,
): void {
  beforePublish.push({ pluginId, fn });
}

export function unsubscribePlugin(pluginId: string): void {
  for (const [event, list] of handlers) {
    handlers.set(
      event,
      list.filter((h) => h.pluginId !== pluginId),
    );
  }
  for (let i = beforePublish.length - 1; i >= 0; i--) {
    if (beforePublish[i].pluginId === pluginId) beforePublish.splice(i, 1);
  }
}

/** Core UI → extensions. A throwing handler never breaks the app or its
 * sibling extensions; the error is logged against the owning plugin. */
export function emitEvent<K extends keyof EventMap>(
  event: K,
  payload: EventMap[K],
): void {
  for (const h of handlers.get(event) ?? []) {
    try {
      h.fn(payload);
    } catch (e) {
      console.error(`extension ${h.pluginId}: ${event} handler failed`, e);
    }
  }
}

/** Collect publish warnings from every subscriber. Failures degrade to a
 * warning about the extension itself rather than blocking publishing. */
export async function collectPublishWarnings(
  ctx: BeforePublishContext,
): Promise<{ pluginId: string; warning: PublishWarning }[]> {
  const out: { pluginId: string; warning: PublishWarning }[] = [];
  for (const h of beforePublish) {
    try {
      const warnings = (await h.fn(ctx)) ?? [];
      for (const warning of warnings) out.push({ pluginId: h.pluginId, warning });
    } catch (e) {
      out.push({
        pluginId: h.pluginId,
        warning: {
          message: `Extension ${h.pluginId} failed while checking this draft`,
          detail: String(e),
        },
      });
    }
  }
  return out;
}

/** Test/dev helper. */
export function resetEvents(): void {
  handlers.clear();
  beforePublish.length = 0;
}
