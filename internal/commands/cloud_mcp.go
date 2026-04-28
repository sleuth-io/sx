package commands

import (
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/logger"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// buildCloudServeMCPServerFromConfig constructs a fresh MCP server with the
// vault's tools registered, using a pre-loaded config snapshot. Invoked once
// per cloud serve reconnect to avoid leaking session state across
// connections; the caller passes the same “cfg“ it loaded once at startup
// so the disk isn't re-read on every reconnect (and the startup log line
// describes the same vault the factory will instantiate).
//
// Parallel to “mcpserver.Server.registerVaultTools“ but returns a
// standalone “*mcp.Server“ that can be driven by an in-memory
// transport. Returns an error rather than silently producing a
// tool-less server — a relay that connects but serves nothing is
// indistinguishable from a healthy empty vault and leaves the operator
// without a signal that something is wrong.
//
// Vaults can supply tools in two shapes:
//   - “[]vaultpkg.ToolDef“ — explicit tool list (used by SleuthVault).
//   - “*vaultpkg.AssetShimRegistrar“ — a registrar that wires the standard
//     asset listing/loading toolset onto a server (used by PathVault and
//     GitVault, which have no bespoke per-vault tools but want claude.ai /
//     chatgpt.com to see real list_my_assets / load_my_asset surfaces).
func buildCloudServeMCPServerFromConfig(cfg *config.Config) (*mcp.Server, error) {
	log := logger.Get()

	impl := &mcp.Implementation{
		Name:    "skills",
		Version: "1.0.0",
	}
	server := mcp.NewServer(impl, nil)

	vault, err := vaultpkg.NewFromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("cloud serve: create vault from config: %w", err)
	}

	tools := vault.GetMCPTools()
	if tools == nil {
		log.Debug("cloud serve: vault provides no MCP tools")
		return server, nil
	}

	switch t := tools.(type) {
	case *vaultpkg.AssetShimRegistrar:
		t.Register(server)
		return server, nil
	case []vaultpkg.ToolDef:
		if len(t) == 0 {
			return nil, errors.New("cloud serve: vault returned empty tool list")
		}
		for _, def := range t {
			mcp.AddTool(server, def.Tool, def.Handler)
			log.Debug("cloud serve: registered MCP tool", "name", def.Tool.Name)
		}
		return server, nil
	default:
		return nil, fmt.Errorf("cloud serve: vault returned unexpected MCP tools type %T", tools)
	}
}
