package commands

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/clients"
	mcpserver "github.com/sleuth-io/sx/internal/mcp"
)

// NewServeCommand creates the serve command
func NewServeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the MCP server over stdio",
		Long: `Start an MCP (Model Context Protocol) server that exposes tools for AI clients.

The server runs over stdio and provides tools for querying integrated services.
This enables AI coding assistants to query GitHub, CircleCI, Linear, and other services.

Tools provided:
  - query: Query integrated services using natural language`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(cmd, args)
		},
	}

	return cmd
}

// runServe executes the serve command
func runServe(cmd *cobra.Command, args []string) error {
	// Create context that cancels on interrupt
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	// Create the MCP server with the global client registry
	server := mcpserver.NewServer(clients.Global())

	// Run the server (blocks until context is cancelled or error)
	return server.Run(ctx)
}
