import { useCallback, useEffect, useState } from "react";
import { CheckForUpdate, GetVaultInfo, Quit } from "../wailsjs/go/main/App";
import { BrowserOpenURL } from "../wailsjs/runtime/runtime";
import type { main } from "../wailsjs/go/models";
import Onboarding from "./screens/Onboarding";
import Library from "./screens/Library";

export default function App() {
  const [vault, setVault] = useState<main.VaultInfo | null | undefined>(
    undefined,
  );
  const [update, setUpdate] = useState<main.UpdateInfo | null>(null);

  const refresh = useCallback(() => {
    GetVaultInfo()
      .then((info) => setVault(info.configured ? info : null))
      .catch(() => setVault(null));
  }, []);

  useEffect(refresh, [refresh]);
  useEffect(() => {
    CheckForUpdate()
      .then((u) => (u.available || u.installed) && setUpdate(u))
      .catch(() => {});
  }, []);

  if (vault === undefined) {
    return <div className="h-full bg-canvas" />;
  }
  return (
    <div className="flex h-full flex-col">
      {update?.installed && (
        <button
          onClick={() => void Quit()}
          className="shrink-0 bg-accent px-4 py-1.5 text-center text-xs font-medium text-white hover:opacity-90"
        >
          sx {update.version} was installed — click to quit, then reopen sx
        </button>
      )}
      {update?.available && (
        <button
          onClick={() => BrowserOpenURL(update.url)}
          className="shrink-0 bg-accent px-4 py-1.5 text-center text-xs font-medium text-white hover:opacity-90"
        >
          A new version of sx is available ({update.version}) — click to
          download
        </button>
      )}
      <div className="min-h-0 flex-1">
        {vault === null ? (
          <Onboarding onDone={refresh} />
        ) : (
          <Library
            /* Remount on profile/vault change so every list reloads */
            key={vault.type + ":" + vault.location}
            vault={vault}
            onVaultChanged={refresh}
          />
        )}
      </div>
    </div>
  );
}
