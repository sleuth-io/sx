import { useCallback, useEffect, useState } from "react";
import { GetVaultInfo } from "../wailsjs/go/main/App";
import type { main } from "../wailsjs/go/models";
import Onboarding from "./screens/Onboarding";
import Library from "./screens/Library";

export default function App() {
  const [vault, setVault] = useState<main.VaultInfo | null | undefined>(
    undefined,
  );

  const refresh = useCallback(() => {
    GetVaultInfo().then((info) => setVault(info.configured ? info : null));
  }, []);

  useEffect(refresh, [refresh]);

  if (vault === undefined) {
    return <div className="h-full bg-canvas" />;
  }
  if (vault === null) {
    return <Onboarding onDone={refresh} />;
  }
  return <Library vault={vault} onVaultChanged={refresh} />;
}
