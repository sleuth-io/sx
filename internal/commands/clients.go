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
	out := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	if !clients.IsValidClientID(clientID) {
		return fmt.Errorf("unknown client ID: %s\nValid IDs: %s", clientID, strings.Join(clients.AllClientIDs(), ", "))
	}

	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Migrate old config if needed
	if mpc.NeedsMigration() {
		if _, err := mpc.MigrateEnabledClients(clients.AllClientIDs()); err != nil {
			return fmt.Errorf("failed to migrate config: %w", err)
		}
	}

	// Check if already force-enabled
	if slices.Contains(mpc.ForceEnabledClients, clientID) {
		out.Success(clientID + " is already enabled")
		return nil
	}

	// Remove from ForceDisabledClients if present
	mpc.ForceDisabledClients = slices.DeleteFunc(mpc.ForceDisabledClients, func(id string) bool {
		return id == clientID
	})

	// Add to ForceEnabledClients
	mpc.ForceEnabledClients = append(mpc.ForceEnabledClients, clientID)

	if err := config.SaveMultiProfile(mpc); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	out.Success("Enabled " + clientID)
	return nil
}

func runClientsDisable(cmd *cobra.Command, args []string) error {
	clientID := args[0]
	out := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	if !clients.IsValidClientID(clientID) {
		return fmt.Errorf("unknown client ID: %s\nValid IDs: %s", clientID, strings.Join(clients.AllClientIDs(), ", "))
	}

	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Migrate old config if needed
	if mpc.NeedsMigration() {
		if _, err := mpc.MigrateEnabledClients(clients.AllClientIDs()); err != nil {
			return fmt.Errorf("failed to migrate config: %w", err)
		}
	}

	// Check if already force-disabled
	if slices.Contains(mpc.ForceDisabledClients, clientID) {
		out.Success(clientID + " is already disabled")
		return nil
	}

	// Remove from ForceEnabledClients if present
	mpc.ForceEnabledClients = slices.DeleteFunc(mpc.ForceEnabledClients, func(id string) bool {
		return id == clientID
	})

	// Add to ForceDisabledClients
	mpc.ForceDisabledClients = append(mpc.ForceDisabledClients, clientID)

	if err := config.SaveMultiProfile(mpc); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	out.Success("Disabled " + clientID)
	return nil
}

func runClientsReset(cmd *cobra.Command, args []string) error {
	out := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Check if already at default (no force settings and no old EnabledClients)
	if len(mpc.ForceEnabledClients) == 0 && len(mpc.ForceDisabledClients) == 0 && len(mpc.EnabledClients) == 0 {
		out.Success("Already using default (all detected clients enabled)")
		return nil
	}

	// Clear all client settings
	mpc.ForceEnabledClients = nil
	mpc.ForceDisabledClients = nil
	mpc.EnabledClients = nil // Clear deprecated field too

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
			out.ListItem("○", "Status: not detected")
		}

		if info.ForceDisabled {
			out.ListItem("⚠", "Disabled in config")
		} else if info.ForceEnabled && !info.Installed {
			out.ListItem("⚠", "Enabled in config but not detected")
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
