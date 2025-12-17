package commands

import (
	"context"

	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/logger"
)

// installSelectedClientHooks installs hooks only to the specified clients.
// If enabledClientIDs is nil or empty, installs to all detected clients.
func installSelectedClientHooks(ctx context.Context, out *outputHelper, enabledClientIDs []string) {
	log := logger.Get()
	registry := clients.Global()
	installedClients := registry.DetectInstalled()

	// Build enabled set (nil/empty means all enabled)
	var enabledSet map[string]bool
	if len(enabledClientIDs) > 0 {
		enabledSet = make(map[string]bool)
		for _, id := range enabledClientIDs {
			enabledSet[id] = true
		}
	}

	for _, client := range installedClients {
		// Skip if not in enabled set (when set is defined)
		if enabledSet != nil && !enabledSet[client.ID()] {
			log.Debug("skipping hook installation for disabled client", "client", client.ID())
			continue
		}

		if err := client.InstallHooks(ctx); err != nil {
			out.printfErr("Warning: failed to install hooks for %s: %v\n", client.DisplayName(), err)
			log.Error("failed to install client hooks", "client", client.ID(), "error", err)
		}
	}
}
