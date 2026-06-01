package commands

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/config"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
	"github.com/sleuth-io/sx/internal/vaultcopy"
)

func newVaultCopyCommand() *cobra.Command {
	var (
		fromProfile string
		toProfile   string
		only        []string
		dryRun      bool
		yes         bool
	)

	cmd := &cobra.Command{
		Use:   "copy --from <profile> --to <profile>",
		Short: "Copy a vault's contents into another vault",
		Long: `Copy teams, bots, assets (all versions), installation scopes, audit
history, and usage history from one vault (profile) into another.

The copy is "smart": assets are uploaded version-by-version, so matching
content is deduplicated and changed content lands as a new version in the
destination. Audit and usage events keep their original timestamps and actors.

By default every category is copied. Use --only to restrict (e.g.
--only assets,teams). A preview is always shown first; pass --yes to apply.

Some transfers are lossy depending on direction (e.g. into skills.new: bot API
keys must be regenerated). The preview lists anything that won't transfer.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runVaultCopy(cmd, fromProfile, toProfile, only, dryRun, yes)
		},
	}

	cmd.Flags().StringVar(&fromProfile, "from", "", "source profile name (required)")
	cmd.Flags().StringVar(&toProfile, "to", "", "destination profile name (required)")
	cmd.Flags().StringSliceVar(&only, "only", nil, "restrict to categories: teams,bots,assets,audit,usage")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview what would copy without writing")
	cmd.Flags().BoolVar(&yes, "yes", false, "apply the copy (without this, only a preview is shown)")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")

	return cmd
}

func runVaultCopy(cmd *cobra.Command, fromProfile, toProfile string, only []string, dryRun, yes bool) error {
	if fromProfile == toProfile {
		return errors.New("--from and --to must be different profiles")
	}

	opts, err := optionsFromOnly(only)
	if err != nil {
		return err
	}

	src, err := vaultFromProfile(fromProfile)
	if err != nil {
		return fmt.Errorf("source profile %q: %w", fromProfile, err)
	}
	dst, err := vaultFromProfile(toProfile)
	if err != nil {
		return fmt.Errorf("destination profile %q: %w", toProfile, err)
	}

	ctx := cmd.Context()
	out := cmd.OutOrStdout()

	// Always preview first so the user sees scope + losses before any writes.
	preview := opts
	preview.DryRun = true
	previewReport, err := vaultcopy.Copy(ctx, src, dst, preview)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Preview: copy %s → %s\n", fromProfile, toProfile)
	printReport(out, previewReport, true)

	if dryRun {
		return nil
	}
	if !yes {
		fmt.Fprintln(out, "\nRe-run with --yes to apply, or --dry-run to preview only.")
		return nil
	}

	opts.DryRun = false
	report, err := vaultcopy.Copy(ctx, src, dst, opts)
	if err != nil {
		printReport(out, report, false)
		return err
	}
	fmt.Fprintf(out, "\nCopied %s → %s\n", fromProfile, toProfile)
	printReport(out, report, false)
	return nil
}

func optionsFromOnly(only []string) (vaultcopy.Options, error) {
	if len(only) == 0 {
		return vaultcopy.DefaultOptions(), nil
	}
	var opts vaultcopy.Options
	for _, c := range only {
		switch strings.ToLower(strings.TrimSpace(c)) {
		case "teams":
			opts.Teams = true
		case "bots":
			opts.Bots = true
		case "assets":
			opts.Assets = true
		case "audit":
			opts.Audit = true
		case "usage":
			opts.Usage = true
		default:
			return opts, fmt.Errorf("unknown category %q (valid: teams,bots,assets,audit,usage)", c)
		}
	}
	return opts, nil
}

func vaultFromProfile(name string) (vaultpkg.Vault, error) {
	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return nil, err
	}
	profile, ok := mpc.GetProfile(name)
	if !ok {
		return nil, errors.New("profile not found (run 'sx profile list')")
	}
	return vaultpkg.NewFromConfig(profile.ToConfig(nil, nil))
}

func printReport(out io.Writer, r *vaultcopy.Report, planned bool) {
	verb := "Copied"
	if planned {
		verb = "Would copy"
	}
	fmt.Fprintf(out, "  %s: %d teams, %d bots, %d assets (%d versions), %d scopes, %d audit events, %d usage events\n",
		verb, r.Teams, r.Bots, r.Assets, r.Versions, r.Scopes, r.AuditEvents, r.UsageEvents)
	if r.SkippedVersions > 0 {
		fmt.Fprintf(out, "  Skipped %d already-present versions\n", r.SkippedVersions)
	}
	if len(r.Warnings) > 0 {
		// De-duplicate repeated warnings for a tidy report.
		seen := map[string]bool{}
		fmt.Fprintln(out, "  Notes / losses:")
		for _, w := range r.Warnings {
			if seen[w] {
				continue
			}
			seen[w] = true
			fmt.Fprintf(out, "    - %s\n", w)
		}
	}
}
