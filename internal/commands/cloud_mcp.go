package commands

import (
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/logger"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// buildCloudServeMCPServer constructs a fresh MCP server with the
// vault's tools registered. Invoked once per cloud serve reconnect to
// avoid leaking session state across connections.
//
// Parallel to “mcpserver.Server.registerVaultTools“ but returns a
// standalone “*mcp.Server“ that can be driven by an in-memory
// transport. Returns an error rather than silently producing a
// tool-less server — a relay that connects but serves nothing is
// indistinguishable from a healthy empty vault and leaves the operator
// without a signal that something is wrong.
func buildCloudServeMCPServer() (*mcp.Server, error) {
	log := logger.Get()

	impl := &mcp.Implementation{
		Name:    "skills",
		Version: "1.0.0",
	}
	server := mcp.NewServer(impl, nil)

	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("cloud serve: load sx config: %w", err)
	}

	vault, err := vaultpkg.NewFromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("cloud serve: create vault from config: %w", err)
	}

	tools := vault.GetMCPTools()
	if tools == nil {
		// A vault with no MCP tools is legitimate (e.g. PathVault) —
		// we return the empty server without error. The operator sees
		// "connected, no tools" and knows that's expected for this
		// vault type.
		log.Debug("cloud serve: vault provides no MCP tools")
		return server, nil
	}
	defs, ok := tools.([]vaultpkg.ToolDef)
	if !ok {
		return nil, fmt.Errorf("cloud serve: vault returned unexpected MCP tools type %T", tools)
	}
	if len(defs) == 0 {
		return nil, errors.New("cloud serve: vault returned empty tool list")
	}
	for _, def := range defs {
		mcp.AddTool(server, def.Tool, def.Handler)
		log.Debug("cloud serve: registered MCP tool", "name", def.Tool.Name)
	}
	return server, nil
}
