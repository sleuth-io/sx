package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

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
		Short: "List detected AI coding assistants",
		Long:  "Shows all AI coding assistants that sx can install assets to, along with their installation status.",
		RunE:  runClients,
	}
	cmd.Flags().Bool("json", false, "Output in JSON format")
	return cmd
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
	out.Muted("Tip: Use 'sx config' for more details or configure enabled clients in your sx config file.")

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
