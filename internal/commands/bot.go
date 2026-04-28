package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/mgmt"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/components"
	"github.com/sleuth-io/sx/internal/vault"
)

// NewBotCommand creates the `sx bot` command tree, mirroring `sx team`.
// Bots are non-human service identities that consume assets — see
// docs/bots.md for the full lifecycle and resolution rules.
func NewBotCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bot",
		Short: "Manage bots (non-human service identities)",
		Long: `Bots are non-human service identities that consume assets — typically
CI jobs, agents, or automation. Bots gain repository context by being
members of teams (the same way human team members do); assets can also
be installed directly to a bot via 'sx install --bot <name>'.

Once a bot exists, set SX_BOT=<name> in the bot's runtime environment
(or pass --bot via the install flag) to resolve installs against the
bot identity.`,
	}
	cmd.AddCommand(
		newBotListCommand(),
		newBotShowCommand(),
		newBotCreateCommand(),
		newBotUpdateCommand(),
		newBotDeleteCommand(),
		newBotTeamCommand(),
		newBotKeyCommand(),
	)
	return cmd
}

func newBotListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List bots",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			v, err := loadVault()
			if err != nil {
				return err
			}
			bots, err := v.ListBots(ctx)
			if err != nil {
				return err
			}
			return printBotList(cmd, bots)
		},
	}
}

func newBotShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Show bot details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			v, err := loadVault()
			if err != nil {
				return err
			}
			bot, err := v.GetBot(ctx, args[0])
			if err != nil {
				return err
			}
			return printBotDetails(cmd, v, bot)
		},
	}
}

func newBotCreateCommand() *cobra.Command {
	var description string
	var teams []string

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new bot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			if strings.TrimSpace(args[0]) == "" {
				return mgmt.ErrEmptyBotName
			}

			v, err := loadVault()
			if err != nil {
				return err
			}
			// No pre-flight CurrentActor() call: the transactional
			// RequireRealIdentity guard inside withManifest is the
			// single source of truth, and a pre-flight call duplicates
			// work on file vaults while running a different code path
			// on Sleuth. Synthetic identities are caught at commit time.

			bot := mgmt.Bot{
				Name:        args[0],
				Description: description,
				Teams:       teams,
			}

			status := components.NewStatus(cmd.OutOrStdout())
			status.Start("Creating bot " + bot.Name)
			rawToken, err := v.CreateBot(ctx, bot)
			if err != nil {
				status.Fail("Failed to create bot")
				return err
			}
			status.Done("Created bot " + bot.Name)
			// Sleuth vaults auto-issue an initial API key as part of
			// bot creation; print it once so CI authors don't have to
			// follow up with `sx bot key create`. File-based vaults
			// always return an empty rawToken.
			if rawToken != "" {
				out := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
				out.Newline()
				out.Bold("Bot key (shown once — copy it now):")
				out.Println("  " + rawToken)
				out.Newline()
				out.Muted("Set this as SX_BOT_KEY in the bot's runtime, alongside SX_BOT=" + bot.Name + ".")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&description, "description", "", "Bot description")
	cmd.Flags().StringSliceVar(&teams, "team", nil, "Initial team membership (can be given multiple times)")
	return cmd
}

func newBotUpdateCommand() *cobra.Command {
	var description string
	var setDescription bool

	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update a bot's description",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			v, err := loadVault()
			if err != nil {
				return err
			}
			existing, err := v.GetBot(ctx, args[0])
			if err != nil {
				return err
			}
			if setDescription {
				existing.Description = description
			}
			// Description-only edit: clear the in-memory team list so
			// the vault's UpdateBot omits teamIds from its GraphQL
			// input. Without this, a concurrent `sx bot team add/remove`
			// between our GetBot and UpdateBot would be silently
			// overwritten with the stale list we read. CLI today only
			// supports description edits, so the unconditional clear is
			// safe; if a `--team` flag lands later, route through that
			// path instead and keep teams populated.
			existing.Teams = nil
			status := components.NewStatus(cmd.OutOrStdout())
			status.Start("Updating bot " + existing.Name)
			if err := v.UpdateBot(ctx, *existing); err != nil {
				status.Fail("Failed to update bot")
				return err
			}
			status.Done("Updated bot " + existing.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&description, "description", "", "New description")
	// Cobra's bool flag can't tell "absent" from "empty", so add a
	// changed-flag check to know whether the user really set it.
	cmd.PreRun = func(cmd *cobra.Command, args []string) {
		setDescription = cmd.Flags().Changed("description")
	}
	return cmd
}

func newBotDeleteCommand() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a bot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			v, err := loadVault()
			if err != nil {
				return err
			}

			if !yes {
				confirmed, err := components.ConfirmWithIO(
					fmt.Sprintf("Delete bot %q? This cannot be undone.", args[0]),
					false,
					cmd.InOrStdin(),
					cmd.OutOrStdout(),
				)
				if err != nil || !confirmed {
					return nil
				}
			}

			status := components.NewStatus(cmd.OutOrStdout())
			status.Start("Deleting bot " + args[0])
			if err := v.DeleteBot(ctx, args[0]); err != nil {
				status.Fail("Failed to delete bot")
				return err
			}
			status.Done("Deleted bot " + args[0])
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompts")
	return cmd
}

func newBotTeamCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "team",
		Short: "Manage bot team memberships",
	}

	addCmd := &cobra.Command{
		Use:   "add <bot> <team>",
		Short: "Add a bot to a team",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBotMutation(cmd, func(ctx context.Context, v vault.Vault) error {
				return v.AddBotTeam(ctx, args[0], args[1])
			},
				fmt.Sprintf("Adding bot %s to team %s", args[0], args[1]),
				fmt.Sprintf("Added bot %s to team %s", args[0], args[1]))
		},
	}
	removeCmd := &cobra.Command{
		Use:   "remove <bot> <team>",
		Short: "Remove a bot from a team",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBotMutation(cmd, func(ctx context.Context, v vault.Vault) error {
				return v.RemoveBotTeam(ctx, args[0], args[1])
			},
				fmt.Sprintf("Removing bot %s from team %s", args[0], args[1]),
				fmt.Sprintf("Removed bot %s from team %s", args[0], args[1]))
		},
	}
	cmd.AddCommand(addCmd, removeCmd)
	return cmd
}

func newBotKeyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key",
		Short: "Manage bot API keys (Sleuth vaults only)",
		Long: `Bot API keys are issued by skills.new for bots that authenticate
against the hosted vault. File-based vaults (path/git) treat bots as
identity-only — set SX_BOT=<name> in the bot's runtime to claim its
identity. Trust boundary on file-based vaults: anyone with vault read
access can claim any bot identity.`,
	}

	var label string
	createCmd := &cobra.Command{
		Use:   "create <bot>",
		Short: "Create a new API key for a bot (printed once)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			v, err := loadVault()
			if err != nil {
				return err
			}
			km, ok := v.(vault.BotApiKeyManager)
			if !ok {
				return errors.New("this vault does not support bot API keys; set SX_BOT=<name> in the bot runtime instead")
			}
			raw, _, err := km.CreateBotApiKey(ctx, args[0], label)
			if err != nil {
				return err
			}
			out := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
			out.Bold("Bot key (shown once — copy it now):")
			out.Println("  " + raw)
			out.Newline()
			out.Muted("Set this as SX_BOT_KEY in the bot's runtime, alongside SX_BOT=" + args[0] + ".")
			return nil
		},
	}
	createCmd.Flags().StringVar(&label, "label", "", "Label for this key (e.g. 'ci-default')")

	listCmd := &cobra.Command{
		Use:   "list <bot>",
		Short: "List API keys for a bot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			v, err := loadVault()
			if err != nil {
				return err
			}
			km, ok := v.(vault.BotApiKeyManager)
			if !ok {
				return errors.New("this vault does not support bot API keys")
			}
			keys, err := km.ListBotApiKeys(ctx, args[0])
			if err != nil {
				return err
			}
			out := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
			if len(keys) == 0 {
				out.Muted("No API keys for bot " + args[0] + ".")
				return nil
			}
			out.Header("API keys for " + args[0])
			out.Newline()
			for _, k := range keys {
				line := fmt.Sprintf("  %s  %s  %s",
					out.BoldText(k.MaskedToken),
					k.Label,
					k.CreatedAt.Format(time.RFC3339))
				out.Println(line)
			}
			out.Newline()
			return nil
		},
	}

	deleteCmd := &cobra.Command{
		Use:   "delete <bot> <key-id>",
		Short: "Delete an API key for a bot",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			v, err := loadVault()
			if err != nil {
				return err
			}
			km, ok := v.(vault.BotApiKeyManager)
			if !ok {
				return errors.New("this vault does not support bot API keys")
			}
			return km.DeleteBotApiKey(ctx, args[0], args[1])
		},
	}

	cmd.AddCommand(createCmd, listCmd, deleteCmd)
	return cmd
}

// runBotMutation wraps a bot mutation with a status spinner. Bot
// management is not gated on team-admin status (any vault writer can
// manage bots) — the vault's outer write-access control is the gate.
func runBotMutation(cmd *cobra.Command, fn func(ctx context.Context, v vault.Vault) error, progressMsg, doneMsg string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	v, err := loadVault()
	if err != nil {
		return err
	}
	status := components.NewStatus(cmd.OutOrStdout())
	status.Start(progressMsg)
	if err := fn(ctx, v); err != nil {
		status.Fail(err.Error())
		return err
	}
	status.Done(doneMsg)
	return nil
}

func printBotList(cmd *cobra.Command, bots []mgmt.Bot) error {
	out := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
	if len(bots) == 0 {
		out.Muted("No bots defined yet. Create one with 'sx bot create <name>'.")
		return nil
	}
	out.Header("Bots")
	out.Newline()
	for _, b := range bots {
		line := fmt.Sprintf("  %s %s",
			out.BoldText(b.Name),
			out.MutedText(fmt.Sprintf("%d teams", len(b.Teams))),
		)
		out.Println(line)
		if b.Description != "" {
			out.Muted("    " + b.Description)
		}
	}
	out.Newline()
	return nil
}

func printBotDetails(cmd *cobra.Command, v vault.Vault, bot *mgmt.Bot) error {
	out := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
	out.Newline()
	out.Header(bot.Name)
	if bot.Description != "" {
		out.Println(bot.Description)
	}
	out.Newline()

	if len(bot.Teams) > 0 {
		out.Bold("Teams")
		for _, t := range bot.Teams {
			out.ListItem("•", t)
		}
		out.Newline()
	}

	if km, ok := v.(vault.BotApiKeyManager); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		keys, err := km.ListBotApiKeys(ctx, bot.Name)
		switch {
		case err != nil:
			// Surface the failure as a muted note so missing keys
			// don't get confused with "no keys exist". Don't fail
			// the whole command — the rest of the bot's metadata is
			// still useful.
			out.Muted("API keys unavailable: " + err.Error())
			out.Newline()
		case len(keys) > 0:
			out.Bold("API Keys")
			for _, k := range keys {
				line := fmt.Sprintf("  %s  %s", k.MaskedToken, k.Label)
				out.Println(line)
			}
			out.Newline()
		}
	}

	return nil
}
