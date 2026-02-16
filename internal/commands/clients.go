package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/clients"
	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/ui"
)

// ClientsOutput represents the output for the clients command
type ClientsOutput struct {
	Clients []ClientInfo `json:"clients"`
}

// NewClientsCommand creates the clients command
func NewClientsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clients",
		Short: "List and manage AI coding assistants",
		Long:  "Shows all AI coding assistants that sx can install assets to, along with their installation status.",
		RunE:  runClients,
	}
	cmd.Flags().Bool("json", false, "Output in JSON format")

	cmd.AddCommand(newClientsEnableCommand())
	cmd.AddCommand(newClientsDisableCommand())
	cmd.AddCommand(newClientsResetCommand())

	return cmd
}

func newClientsEnableCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <client-id>",
		Short: "Enable a client for asset installation",
		Long:  "Enables a client so that assets will be installed to it.\n\nValid client IDs: " + strings.Join(clients.AllClientIDs(), ", "),
		Args:  cobra.ExactArgs(1),
		RunE:  runClientsEnable,
	}
}

func newClientsDisableCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <client-id>",
		Short: "Disable a client from asset installation",
		Long:  "Disables a client so that assets will not be installed to it.\n\nValid client IDs: " + strings.Join(clients.AllClientIDs(), ", "),
		Args:  cobra.ExactArgs(1),
		RunE:  runClientsDisable,
	}
}

func newClientsResetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Reset to default (all detected clients enabled)",
		Long:  "Clears the enabled clients configuration, reverting to the default behavior where all detected clients receive assets.",
		Args:  cobra.NoArgs,
		RunE:  runClientsReset,
	}
}

func runClientsEnable(cmd *cobra.Command, args []string) error {
	clientID := args[0]
	out := ui.NewOutput(os.Stdout, os.Stderr)

	if !clients.IsValidClientID(clientID) {
		return fmt.Errorf("unknown client ID: %s\nValid IDs: %s", clientID, strings.Join(clients.AllClientIDs(), ", "))
	}

	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// If enabledClients is empty, all are enabled - need to explicitly set the list
	if len(mpc.EnabledClients) == 0 {
		// Start with all clients enabled, then the enable is a no-op but config is explicit
		mpc.EnabledClients = clients.AllClientIDs()
	}

	if slices.Contains(mpc.EnabledClients, clientID) {
		out.Success(fmt.Sprintf("%s is already enabled", clientID))
		return nil
	}

	mpc.EnabledClients = append(mpc.EnabledClients, clientID)

	if err := config.SaveMultiProfile(mpc); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	out.Success(fmt.Sprintf("Enabled %s", clientID))
	return nil
}

func runClientsDisable(cmd *cobra.Command, args []string) error {
	clientID := args[0]
	out := ui.NewOutput(os.Stdout, os.Stderr)

	if !clients.IsValidClientID(clientID) {
		return fmt.Errorf("unknown client ID: %s\nValid IDs: %s", clientID, strings.Join(clients.AllClientIDs(), ", "))
	}

	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// If enabledClients is empty, all are enabled - need to populate first
	if len(mpc.EnabledClients) == 0 {
		mpc.EnabledClients = clients.AllClientIDs()
	}

	if !slices.Contains(mpc.EnabledClients, clientID) {
		out.Success(fmt.Sprintf("%s is already disabled", clientID))
		return nil
	}

	// Remove the client from enabled list
	mpc.EnabledClients = slices.DeleteFunc(mpc.EnabledClients, func(id string) bool {
		return id == clientID
	})

	if err := config.SaveMultiProfile(mpc); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	out.Success(fmt.Sprintf("Disabled %s", clientID))
	return nil
}

func runClientsReset(cmd *cobra.Command, args []string) error {
	out := ui.NewOutput(os.Stdout, os.Stderr)

	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if len(mpc.EnabledClients) == 0 {
		out.Success("Already using default (all detected clients enabled)")
		return nil
	}

	mpc.EnabledClients = nil

	if err := config.SaveMultiProfile(mpc); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	out.Success("Reset to default (all detected clients enabled)")
	return nil
}

func runClients(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")

	clientInfos := gatherClientInfo()

	if jsonOutput {
		output := ClientsOutput{Clients: clientInfos}
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}

	out := ui.NewOutput(os.Stdout, os.Stderr)
	out.Header("Detected AI Coding Assistants")
	out.Newline()
	PrintClientsSection(out, clientInfos)
	out.Printf("Use %s or %s to configure which clients receive assets.\n", out.EmphasisText("sx clients enable <id>"), out.EmphasisText("disable"))
	out.Printf("Use %s to revert to default (all detected clients).\n", out.EmphasisText("sx clients reset"))
	out.Printf("Use %s for more details about your configuration.\n", out.EmphasisText("sx config"))

	return nil
}

// PrintClientsSection outputs styled client information to the given writer.
// Used by both 'sx clients' and 'sx config' commands.
func PrintClientsSection(out *ui.Output, clientInfos []ClientInfo) {
	for _, info := range clientInfos {
		// Client name as emphasized text
		out.Bold(info.Name)

		if info.Installed {
			out.SuccessItem("Status: installed")
		} else {
			out.Muted("  Status: not detected")
		}

		if !info.Enabled {
			out.Muted("  ⚠ Disabled in config")
		} else if !info.Installed {
			out.Muted("  ⚠ Enabled in config but not detected")
		}

		if info.Version != "" {
			out.Printf("  Version: %s\n", out.EmphasisText(info.Version))
		}

		if info.Directory != "" {
			out.Printf("  Directory: %s\n", out.MutedText(info.Directory))
		}

		if info.HooksInstalled {
			out.SuccessItem("Hooks: installed")
		}

		if len(info.Supports) > 0 {
			out.Printf("  Supports: %s\n", out.MutedText(strings.Join(info.Supports, ", ")))
		}

		out.Newline()
	}
}

// PrintClientsSectionWriter outputs styled client information to the given io.Writer.
// Used for compatibility with existing code that uses io.Writer.
func PrintClientsSectionWriter(w io.Writer, clientInfos []ClientInfo) {
	out := ui.NewOutput(w, w)
	PrintClientsSection(out, clientInfos)
}
