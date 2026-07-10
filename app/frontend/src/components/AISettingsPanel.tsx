import { useCallback, useEffect, useState } from "react";
import {
  LLMSetAPIKey,
  LLMSetConfig,
  LLMStatus,
  LLMTest,
} from "../../wailsjs/go/main/App";
import { llm, type main } from "../../wailsjs/go/models";

// The AI tab of Settings: pick ONE provider for the whole app —
// extensions holding llm:use all route through it. Deliberately
// vendor-neutral: an installed CLI, a local Ollama server, or any
// hosted API the user has a key for (custom base URLs cover
// OpenAI-compatible endpoints like OpenRouter, Groq, vLLM, LM Studio).
// API keys go straight to the OS keychain and are never read back here;
// keySet only says whether one is stored.

const KIND_LABELS: Record<string, string> = {
  cli: "Installed CLIs (uses that tool's own login)",
  local: "Local",
  api: "API key",
};

export default function AISettingsPanel() {
  const [status, setStatus] = useState<main.LLMStatusView | null>(null);
  const [provider, setProvider] = useState("");
  const [model, setModel] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [testResult, setTestResult] = useState("");
  const [dirty, setDirty] = useState(false);

  const load = useCallback(async () => {
    try {
      const s = await LLMStatus();
      setStatus(s);
      setProvider(s.config.provider ?? "");
      setModel(s.config.model ?? "");
      setBaseUrl(s.config.baseUrl ?? "");
      setDirty(false);
    } catch (e) {
      setError(String(e));
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const selected = status?.providers.find((p) => p.id === provider);

  function pick(p: llm.ProviderInfo) {
    setProvider(p.id);
    setTestResult("");
    setError("");
    setDirty(true);
    // Model and endpoint are per-provider settings; don't carry one
    // provider's model name into another.
    setModel(p.id === status?.config.provider ? (status?.config.model ?? "") : "");
    setBaseUrl(p.id === status?.config.provider ? (status?.config.baseUrl ?? "") : "");
    setApiKey("");
  }

  async function save(): Promise<boolean> {
    setError("");
    setTestResult("");
    setBusy("save");
    try {
      await LLMSetConfig(llm.Config.createFrom({ provider, model, baseUrl }));
      if (apiKey.trim() !== "") {
        await LLMSetAPIKey(provider, apiKey.trim());
        setApiKey("");
      }
      await load();
      return true;
    } catch (e) {
      setError(String(e));
      return false;
    } finally {
      setBusy("");
    }
  }

  // Back to "no AI": clears the provider selection (extensions with
  // llm:use show their setup prompts again). Stored API keys stay in
  // the keychain so re-picking a provider later just works.
  async function remove() {
    setError("");
    setTestResult("");
    setBusy("remove");
    try {
      await LLMSetConfig(
        llm.Config.createFrom({ provider: "", model: "", baseUrl: "" }),
      );
      setApiKey("");
      await load();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy("");
    }
  }

  async function saveAndTest() {
    if (!(await save())) return;
    setBusy("test");
    setError("");
    try {
      const reply = await LLMTest();
      setTestResult(reply.trim() || "(empty reply)");
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy("");
    }
  }

  if (!status) {
    return <div className="h-20 animate-pulse rounded-lg bg-canvas" />;
  }

  const keyStored = selected ? (status.keySet[selected.id] ?? false) : false;
  const showBaseUrl = provider === "ollama" || provider === "openai";
  const kinds = ["cli", "local", "api"];

  return (
    <div>
      <div className="mb-1 text-xs font-semibold tracking-wide text-ink-faint">
        AI PROVIDER
      </div>
      <p className="mb-3 text-xs text-ink-faint">
        Extensions you grant the <code className="font-mono">llm:use</code>{" "}
        permission send their prompts here. Pick whichever provider you
        prefer — a CLI you already use, a local model, or your own API key.
      </p>

      {kinds.map((kind) => {
        const group = status.providers.filter((p) => p.kind === kind);
        if (group.length === 0) return null;
        return (
          <div key={kind} className="mb-3">
            <div className="mb-1.5 text-[11px] font-medium text-ink-faint">
              {KIND_LABELS[kind] ?? kind}
            </div>
            <ul className="space-y-1.5">
              {group.map((p) => {
                const disabled = kind === "cli" && !p.available;
                const active = provider === p.id;
                return (
                  <li key={p.id}>
                    <button
                      disabled={disabled}
                      onClick={() => pick(p)}
                      className={`flex w-full items-center gap-3 rounded-xl border p-2.5 text-left transition ${
                        active
                          ? "border-accent bg-accent-soft/40"
                          : "border-line hover:bg-canvas"
                      } ${disabled ? "cursor-not-allowed opacity-50" : ""}`}
                    >
                      <span
                        aria-hidden
                        className={`h-3.5 w-3.5 shrink-0 rounded-full border ${
                          active ? "border-accent bg-accent" : "border-line"
                        }`}
                      />
                      <span className="min-w-0 flex-1">
                        <span className="block text-sm text-ink">{p.label}</span>
                        <span className="block truncate text-xs text-ink-faint">
                          {kind === "cli" &&
                            (p.available ? p.detail : "Not installed")}
                          {kind === "local" &&
                            (p.available
                              ? `${p.models?.length ?? 0} models at ${p.detail}`
                              : `No server detected at ${p.detail}`)}
                          {kind === "api" &&
                            (status.keySet[p.id] ? "API key saved" : "Needs an API key")}
                        </span>
                      </span>
                    </button>
                  </li>
                );
              })}
            </ul>
          </div>
        );
      })}

      {selected && (
        <div className="mb-3 space-y-2 rounded-xl border border-line p-3">
          {selected.needsModel && (
            <label className="block">
              <span className="mb-1 block text-xs text-ink-faint">Model</span>
              {provider === "ollama" && (selected.models?.length ?? 0) > 0 ? (
                <select
                  value={model}
                  onChange={(e) => {
                    setModel(e.target.value);
                    setDirty(true);
                  }}
                  className="w-full rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent"
                >
                  <option value="">Choose a model…</option>
                  {selected.models?.map((m) => (
                    <option key={m} value={m}>
                      {m}
                    </option>
                  ))}
                </select>
              ) : (
                <input
                  value={model}
                  onChange={(e) => {
                    setModel(e.target.value);
                    setDirty(true);
                  }}
                  placeholder={
                    provider === "openai"
                      ? "e.g. gpt-4o or the endpoint's model id"
                      : provider === "google"
                        ? "e.g. gemini-2.5-pro"
                        : "model id"
                  }
                  className="w-full rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent"
                />
              )}
            </label>
          )}
          {!selected.needsModel && (
            <label className="block">
              <span className="mb-1 block text-xs text-ink-faint">
                Model (optional — blank uses the {selected.label.replace(" CLI", "")}{" "}
                default)
              </span>
              <input
                value={model}
                onChange={(e) => {
                  setModel(e.target.value);
                  setDirty(true);
                }}
                className="w-full rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent"
              />
            </label>
          )}
          {showBaseUrl && (
            <label className="block">
              <span className="mb-1 block text-xs text-ink-faint">
                {provider === "ollama"
                  ? "Server address (blank for the local default)"
                  : "Endpoint (blank for OpenAI — set for OpenRouter, Groq, vLLM, LM Studio…)"}
              </span>
              <input
                value={baseUrl}
                onChange={(e) => {
                  setBaseUrl(e.target.value);
                  setDirty(true);
                }}
                placeholder={
                  provider === "ollama"
                    ? "http://127.0.0.1:11434"
                    : "https://api.openai.com"
                }
                className="w-full rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent"
              />
            </label>
          )}
          {selected.needsApiKey && (
            <label className="block">
              <span className="mb-1 block text-xs text-ink-faint">
                API key {keyStored && "(one is saved — enter a new one to replace it)"}
              </span>
              <input
                type="password"
                value={apiKey}
                onChange={(e) => {
                  setApiKey(e.target.value);
                  setDirty(true);
                }}
                placeholder={keyStored ? "••••••••" : "Paste your API key"}
                autoComplete="off"
                className="w-full rounded-lg border border-line bg-canvas px-3 py-2 text-sm outline-none focus:border-accent"
              />
              <span className="mt-1 block text-[11px] text-ink-faint">
                Stored in your OS keychain, never in files or the library.
              </span>
            </label>
          )}
        </div>
      )}

      {error && (
        <div className="mb-2 rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-400">
          {error}
        </div>
      )}
      {testResult && (
        <div className="mb-2 rounded-lg bg-accent-soft px-3 py-2 text-xs">
          Test reply: {testResult}
        </div>
      )}

      <div className="flex items-center gap-2">
        <button
          onClick={() => void save()}
          disabled={busy !== "" || provider === "" || !dirty}
          className="rounded-lg bg-accent px-4 py-1.5 text-xs font-medium text-white transition hover:opacity-90 disabled:opacity-50"
        >
          {busy === "save" ? "Saving…" : "Save"}
        </button>
        <button
          onClick={() => void saveAndTest()}
          disabled={busy !== "" || provider === ""}
          className="rounded-lg border border-line px-4 py-1.5 text-xs font-medium text-ink transition hover:bg-canvas disabled:opacity-50"
        >
          {busy === "test" ? "Testing…" : "Save & test"}
        </button>
        {status.config.provider !== "" && (
          <>
            <button
              onClick={() => void remove()}
              disabled={busy !== ""}
              title="Stop using an AI provider — extensions that need one will show their setup prompts again. Saved API keys stay in your keychain."
              className="rounded-lg border border-line px-4 py-1.5 text-xs font-medium text-ink transition hover:bg-canvas disabled:opacity-50"
            >
              {busy === "remove" ? "Removing…" : "Remove provider"}
            </button>
            <span className="text-xs text-ink-faint">
              Current: {status.config.provider}
              {status.config.model ? ` · ${status.config.model}` : ""}
            </span>
          </>
        )}
      </div>
    </div>
  );
}
