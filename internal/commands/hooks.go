package commands

import (
	"context"
	"os"

	"github.com/sleuth-io/sx/internal/bootstrap"
	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/vault"
)

// installSelectedClientHooks installs hooks only to the specified clients.
// If enabledClientIDs is nil or empty, installs to all detected clients.
// Uses config's BootstrapOptions to determine which options to install.
func installSelectedClientHooks(ctx context.Context, out *outputHelper, enabledClientIDs []string) {
	log := logger.Get()
	styledOut := ui.NewOutput(os.Stdout, os.Stderr)
	registry := clients.Global()
	installedClients := registry.DetectInstalled()

	// Load config to get bootstrap options
	mpc, err := config.LoadMultiProfile()
	if err != nil {
		log.Error("failed to load config for bootstrap options", "error", err)
		// Continue with defaults (nil = yes)
		mpc = &config.MultiProfileConfig{}
	}

	// Load vault to get its bootstrap options
	cfg, _ := config.Load()
	var vaultOpts []bootstrap.Option
	if cfg != nil {
		if v, err := vault.NewFromConfig(cfg); err == nil {
			vaultOpts = v.GetBootstrapOptions(ctx)
		}
	}

	// Build enabled set for clients (nil/empty means all enabled)
	var enabledClientSet map[string]bool
	if len(enabledClientIDs) > 0 {
		enabledClientSet = make(map[string]bool)
		for _, id := range enabledClientIDs {
			enabledClientSet[id] = true
		}
	}

	for _, client := range installedClients {
		// Skip if not in enabled set (when set is defined)
		if enabledClientSet != nil && !enabledClientSet[client.ID()] {
			log.Debug("skipping hook installation for disabled client", "client", client.ID())
			continue
		}

		// Gather all options (vault + this client), deduplicating by key
		seen := make(map[string]bool)
		var allOpts []bootstrap.Option
		for _, opt := range vaultOpts {
			if !seen[opt.Key] {
				seen[opt.Key] = true
				allOpts = append(allOpts, opt)
			}
		}
		if clientOpts := client.GetBootstrapOptions(ctx); clientOpts != nil {
			for _, opt := range clientOpts {
				if !seen[opt.Key] {
					seen[opt.Key] = true
					allOpts = append(allOpts, opt)
				}
			}
		}

		// Filter to enabled options only
		enabledOpts := bootstrap.Filter(allOpts, mpc.GetBootstrapOption)

		if len(enabledOpts) > 0 {
			// Show what we're about to install
			styledOut.Printf("Configuring %s:\n", styledOut.BoldText(client.DisplayName()))

			// Show the settings file being modified
			settingsFile := client.GetBootstrapPath()
			if settingsFile != "" {
				styledOut.Printf("    %s %s\n", styledOut.MutedText("→"), settingsFile)
			}

			for _, opt := range enabledOpts {
				if opt.MCPConfig != nil {
					styledOut.SuccessItem("Added MCP server: " + opt.MCPConfig.Name)
				} else {
					styledOut.SuccessItem("Added " + opt.Description)
				}
			}

			if err := client.InstallBootstrap(ctx, enabledOpts); err != nil {
				out.printfErr("Warning: failed to install for %s: %v\n", client.DisplayName(), err)
				log.Error("failed to install client bootstrap", "client", client.ID(), "error", err)
			}
		}
	}
}
