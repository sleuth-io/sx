package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/mgmt"
	"github.com/sleuth-io/sx/internal/ui"
)

// NewAuditCommand creates the `sx audit` command.
func NewAuditCommand() *cobra.Command {
	var actor string
	var eventPrefix string
	var target string
	var sinceStr string
	var limit int
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Show audit events from the vault",
		Long: `Show audit events recorded in the vault's .sx/audit/ log (for git/path
vaults) or via the skills.new audit stream (for sleuth vaults).

Filters compose (all filters must match). Default limit is 50 rows, newest
first. Use --json to emit machine-readable output.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			v, err := loadVault()
			if err != nil {
				return err
			}

			since, err := parseSinceFlag(sinceStr)
			if err != nil {
				return err
			}

			events, err := v.QueryAuditEvents(ctx, mgmt.AuditFilter{
				Actor:       actor,
				EventPrefix: eventPrefix,
				Target:      target,
				Since:       since,
				Limit:       limit,
			})
			if err != nil {
				return err
			}

			if jsonOutput {
				return emitAuditJSON(cmd, events)
			}
			printAuditEvents(cmd, events)
			return nil
		},
	}

	cmd.Flags().StringVar(&actor, "actor", "", "Filter by actor email")
	cmd.Flags().StringVar(&eventPrefix, "event", "", "Filter by event name prefix (e.g. team. or asset.removed)")
	cmd.Flags().StringVar(&target, "target", "", "Filter by target (team name, asset name, etc.)")
	cmd.Flags().StringVar(&sinceStr, "since", "all", "Time range (7d, 30d, 90d, all)")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum number of rows to return")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	return cmd
}

func printAuditEvents(cmd *cobra.Command, events []mgmt.AuditEvent) {
	out := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
	out.Newline()
	out.Header("Audit events")
	out.Newline()
	if len(events) == 0 {
		out.Muted("No matching events.")
		return
	}
	for _, ev := range events {
		ts := ev.Timestamp.Format("2006-01-02 15:04")
		line := fmt.Sprintf("  %s %s %s %s",
			out.MutedText(ts),
			out.BoldText(ev.Actor),
			out.EmphasisText(ev.Event),
			out.MutedText(ev.Target),
		)
		out.Println(line)
		if len(ev.Data) > 0 {
			dataBytes, _ := json.Marshal(ev.Data)
			out.Muted("    " + string(dataBytes))
		}
	}
	out.Newline()
}

func emitAuditJSON(cmd *cobra.Command, events []mgmt.AuditEvent) error {
	data, err := json.MarshalIndent(events, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return err
}
