package commands

// Register the KiroCrew client for this test binary. The blank import triggers
// the package's init(), which self-registers the client with the global
// registry (matching how cmd/sx/main.go wires it into the real binary).
// Without this, gatherClientInfo() -- which walks clients.Global().GetAll() --
// omits kirocrew while clients.AllClientIDs() (a static list) includes it, so
// TestClientInfoHasAllKnownClients fails.
import (
	_ "github.com/sleuth-io/sx/v2/internal/clients/kirocrew" // Auto-registers via init()
)
