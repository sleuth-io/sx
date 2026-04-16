package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/mgmt"
	"github.com/sleuth-io/sx/internal/ui"
)

// NewStatsCommand creates the `sx stats` command.
func NewStatsCommand() *cobra.Command {
	var assetsOnly bool
	var teamsOnly bool
	var sinceStr string
	var limit int
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show asset usage and team adoption stats",
		Long: `Show summary statistics about asset usage in this vault.

Without flags, shows the default summary: top assets by usage and per-team
adoption percentages. Use --assets to see a detailed breakdown per asset, or
--teams to see adoption figures for every team.

Time range is controlled with --since (7d, 30d, 90d, or all). Default is 30d.`,
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

			filter := mgmt.UsageFilter{Since: since}
			summary, err := v.GetUsageStats(ctx, filter)
			if err != nil {
				return err
			}
			if summary == nil {
				summary = &mgmt.UsageSummary{}
			}

			teams, err := v.ListTeams(ctx)
			if err != nil {
				return err
			}

			report := buildStatsReport(summary, teams, since, limit)

			if jsonOutput {
				return emitStatsJSON(cmd, report, assetsOnly, teamsOnly)
			}

			out := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
			switch {
			case assetsOnly:
				printStatsAssets(out, report)
			case teamsOnly:
				printStatsTeams(out, report)
			default:
				printStatsSummary(out, report)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&assetsOnly, "assets", false, "Show per-asset breakdown only")
	cmd.Flags().BoolVar(&teamsOnly, "teams", false, "Show per-team adoption only")
	cmd.Flags().StringVar(&sinceStr, "since", "30d", "Time range (7d, 30d, 90d, all)")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum number of rows per section")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	return cmd
}

// parseSinceFlag converts strings like "7d", "30d", "90d", "all" into an
// absolute time. Returns a zero time for "all" (meaning no lower bound).
func parseSinceFlag(s string) (time.Time, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "all" {
		return time.Time{}, nil
	}
	if !strings.HasSuffix(s, "d") {
		return time.Time{}, fmt.Errorf("invalid --since value %q (use 7d, 30d, 90d, or all)", s)
	}
	var days int
	if _, err := fmt.Sscanf(s, "%dd", &days); err != nil || days < 0 {
		return time.Time{}, fmt.Errorf("invalid --since value %q", s)
	}
	return time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour), nil
}

// statsReport bundles the data needed to render the stats output in either
// text or JSON form.
type statsReport struct {
	Since     time.Time              `json:"since,omitzero"`
	Total     int                    `json:"total_events"`
	Assets    []mgmt.AssetUsageCount `json:"assets"`
	TeamStats []teamAdoption         `json:"teams"`
	TopActors []mgmt.ActorUsageCount `json:"top_actors"`
}

// teamAdoption is the per-team rollup: for each team, how many of its
// members have used at least one asset in the given time range.
type teamAdoption struct {
	Name          string   `json:"name"`
	MemberCount   int      `json:"member_count"`
	ActiveMembers int      `json:"active_members"`
	AdoptionPct   float64  `json:"adoption_pct"`
	TopAssets     []string `json:"top_assets,omitempty"`
}

// buildStatsReport computes the rolled-up stats view. Asset and actor
// lists are truncated to `limit` rows.
func buildStatsReport(summary *mgmt.UsageSummary, teams []mgmt.Team, since time.Time, limit int) *statsReport {
	report := &statsReport{
		Since:     since,
		Total:     summary.TotalEvents,
		Assets:    summary.PerAsset,
		TopActors: summary.PerActor,
	}
	if limit > 0 && len(report.Assets) > limit {
		report.Assets = report.Assets[:limit]
	}
	if limit > 0 && len(report.TopActors) > limit {
		report.TopActors = report.TopActors[:limit]
	}

	activeByActor := make(map[string]struct{}, len(summary.PerActor))
	for _, a := range summary.PerActor {
		activeByActor[a.Actor] = struct{}{}
	}

	for _, t := range teams {
		ta := teamAdoption{Name: t.Name, MemberCount: len(t.Members)}
		if ta.MemberCount == 0 {
			report.TeamStats = append(report.TeamStats, ta)
			continue
		}
		for _, m := range t.Members {
			if _, ok := activeByActor[m]; ok {
				ta.ActiveMembers++
			}
		}
		ta.AdoptionPct = 100.0 * float64(ta.ActiveMembers) / float64(ta.MemberCount)
		report.TeamStats = append(report.TeamStats, ta)
	}
	sort.Slice(report.TeamStats, func(i, j int) bool {
		return report.TeamStats[i].AdoptionPct > report.TeamStats[j].AdoptionPct
	})
	return report
}

func printStatsSummary(out *ui.Output, report *statsReport) {
	out.Newline()
	out.Header("Usage stats")
	if !report.Since.IsZero() {
		out.Muted(fmt.Sprintf("Since %s (UTC)", report.Since.Format("2006-01-02")))
	} else {
		out.Muted("All time")
	}
	out.Newline()

	out.KeyValue("Total events", strconv.Itoa(report.Total))
	out.Newline()

	if len(report.Assets) > 0 {
		out.Bold("Top assets")
		for _, a := range report.Assets {
			line := fmt.Sprintf("  %s %s",
				out.EmphasisText(a.AssetName),
				out.MutedText(fmt.Sprintf("%d uses · %d users", a.TotalUses, a.UniqueActors)))
			out.Println(line)
		}
		out.Newline()
	}

	if len(report.TeamStats) > 0 {
		out.Bold("Team adoption")
		for _, t := range report.TeamStats {
			line := fmt.Sprintf("  %s %s",
				out.EmphasisText(t.Name),
				out.MutedText(fmt.Sprintf("%.0f%% (%d/%d)", t.AdoptionPct, t.ActiveMembers, t.MemberCount)))
			out.Println(line)
		}
		out.Newline()
	}
}

func printStatsAssets(out *ui.Output, report *statsReport) {
	out.Newline()
	out.Header("Asset usage")
	out.Newline()
	if len(report.Assets) == 0 {
		out.Muted("No usage events recorded.")
		return
	}
	for _, a := range report.Assets {
		out.Println(fmt.Sprintf("  %s %s",
			out.BoldText(a.AssetName),
			out.MutedText(fmt.Sprintf("[%s]", a.AssetType))))
		out.Muted(fmt.Sprintf("    %d uses · %d users · last %s",
			a.TotalUses, a.UniqueActors, relativeTime(a.LastUsed)))
	}
	out.Newline()
}

func printStatsTeams(out *ui.Output, report *statsReport) {
	out.Newline()
	out.Header("Team adoption")
	out.Newline()
	if len(report.TeamStats) == 0 {
		out.Muted("No teams defined yet.")
		return
	}
	for _, t := range report.TeamStats {
		line := fmt.Sprintf("  %s %s",
			out.BoldText(t.Name),
			out.MutedText(fmt.Sprintf("%.0f%% (%d/%d active)", t.AdoptionPct, t.ActiveMembers, t.MemberCount)))
		out.Println(line)
	}
	out.Newline()
}

func emitStatsJSON(cmd *cobra.Command, report *statsReport, assetsOnly, teamsOnly bool) error {
	var payload any = report
	switch {
	case assetsOnly && teamsOnly:
		return errors.New("cannot combine --assets and --teams")
	case assetsOnly:
		payload = report.Assets
	case teamsOnly:
		payload = report.TeamStats
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return err
}

func relativeTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}
