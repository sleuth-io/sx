package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/mgmt"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/components"
	"github.com/sleuth-io/sx/internal/vault"
)

// NewTeamCommand creates the `sx team` command tree.
func NewTeamCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "team",
		Short: "Manage teams and team memberships",
		Long:  "Create teams, manage members, admins, and repositories associated with a team.",
	}
	cmd.AddCommand(
		newTeamListCommand(),
		newTeamShowCommand(),
		newTeamCreateCommand(),
		newTeamDeleteCommand(),
		newTeamMemberCommand(),
		newTeamAdminCommand(),
		newTeamRepoCommand(),
	)
	return cmd
}

func newTeamListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List teams",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			v, err := loadVault()
			if err != nil {
				return err
			}
			teams, err := v.ListTeams(ctx)
			if err != nil {
				return err
			}
			return printTeamList(cmd, teams)
		},
	}
}

func newTeamShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Show team details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			v, err := loadVault()
			if err != nil {
				return err
			}
			team, err := v.GetTeam(ctx, args[0])
			if err != nil {
				return err
			}
			return printTeamDetails(cmd, team)
		},
	}
}

func newTeamCreateCommand() *cobra.Command {
	var description string
	var admins []string
	var members []string
	var repos []string

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new team",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			v, err := loadVault()
			if err != nil {
				return err
			}

			// CreateTeam has no team-admin check — there's no existing
			// team to check against. Authorization falls to the vault
			// backend: for git vaults, the remote's write-access
			// control is the real gate (no push = no team). For Sleuth
			// vaults, the server enforces org-admin. We resolve the
			// actor up-front to surface "set git user.email" errors
			// early rather than deep inside the transaction.
			if _, err := v.CurrentActor(ctx); err != nil {
				return err
			}

			if strings.TrimSpace(args[0]) == "" {
				return mgmt.ErrEmptyTeamName
			}

			team := mgmt.Team{
				Name:         args[0],
				Description:  description,
				Members:      members,
				Admins:       admins,
				Repositories: repos,
			}

			status := components.NewStatus(cmd.OutOrStdout())
			status.Start("Creating team " + team.Name)
			if err := v.CreateTeam(ctx, team); err != nil {
				status.Fail("Failed to create team")
				return err
			}
			status.Done("Created team " + team.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&description, "description", "", "Team description")
	cmd.Flags().StringSliceVar(&admins, "admin", nil, "Initial admin email (can be given multiple times)")
	cmd.Flags().StringSliceVar(&members, "member", nil, "Initial member email (can be given multiple times)")
	cmd.Flags().StringSliceVar(&repos, "repo", nil, "Initial repository URL (can be given multiple times)")
	return cmd
}

func newTeamDeleteCommand() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a team",
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
					fmt.Sprintf("Delete team %q? This cannot be undone.", args[0]),
					false,
					cmd.InOrStdin(),
					cmd.OutOrStdout(),
				)
				if err != nil || !confirmed {
					return nil
				}
			}

			if err := requireTeamAdmin(ctx, v, args[0]); err != nil {
				return err
			}

			status := components.NewStatus(cmd.OutOrStdout())
			status.Start("Deleting team " + args[0])
			if err := v.DeleteTeam(ctx, args[0]); err != nil {
				status.Fail("Failed to delete team")
				return err
			}
			status.Done("Deleted team " + args[0])
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompts")
	return cmd
}

func newTeamMemberCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "member",
		Short: "Manage team membership",
	}

	var admin bool
	addCmd := &cobra.Command{
		Use:   "add <team> <email>",
		Short: "Add a member to a team",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTeamMutation(cmd, args[0], func(ctx context.Context, v vault.Vault) error {
				return v.AddTeamMember(ctx, args[0], args[1], admin)
			},
				fmt.Sprintf("Adding %s to team %s", args[1], args[0]),
				fmt.Sprintf("Added %s to team %s", args[1], args[0]))
		},
	}
	addCmd.Flags().BoolVar(&admin, "admin", false, "Grant admin rights to the new member")

	removeCmd := &cobra.Command{
		Use:   "remove <team> <email>",
		Short: "Remove a member from a team",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTeamMutation(cmd, args[0], func(ctx context.Context, v vault.Vault) error {
				return v.RemoveTeamMember(ctx, args[0], args[1])
			},
				fmt.Sprintf("Removing %s from team %s", args[1], args[0]),
				fmt.Sprintf("Removed %s from team %s", args[1], args[0]))
		},
	}

	cmd.AddCommand(addCmd, removeCmd)
	return cmd
}

func newTeamAdminCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Manage team admin rights",
	}

	setCmd := &cobra.Command{
		Use:   "set <team> <email>",
		Short: "Grant admin rights",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTeamMutation(cmd, args[0], func(ctx context.Context, v vault.Vault) error {
				return v.SetTeamAdmin(ctx, args[0], args[1], true)
			},
				fmt.Sprintf("Granting admin to %s on team %s", args[1], args[0]),
				fmt.Sprintf("Granted admin to %s on team %s", args[1], args[0]))
		},
	}
	unsetCmd := &cobra.Command{
		Use:   "unset <team> <email>",
		Short: "Revoke admin rights",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTeamMutation(cmd, args[0], func(ctx context.Context, v vault.Vault) error {
				return v.SetTeamAdmin(ctx, args[0], args[1], false)
			},
				fmt.Sprintf("Revoking admin from %s on team %s", args[1], args[0]),
				fmt.Sprintf("Revoked admin from %s on team %s", args[1], args[0]))
		},
	}

	cmd.AddCommand(setCmd, unsetCmd)
	return cmd
}

func newTeamRepoCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Manage team repositories",
	}

	addCmd := &cobra.Command{
		Use:   "add <team> <repo-url>",
		Short: "Associate a repository with a team",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTeamMutation(cmd, args[0], func(ctx context.Context, v vault.Vault) error {
				return v.AddTeamRepository(ctx, args[0], args[1])
			},
				"Adding repo to team "+args[0],
				"Added repo to team "+args[0])
		},
	}
	removeCmd := &cobra.Command{
		Use:   "remove <team> <repo-url>",
		Short: "Dissociate a repository from a team",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTeamMutation(cmd, args[0], func(ctx context.Context, v vault.Vault) error {
				return v.RemoveTeamRepository(ctx, args[0], args[1])
			},
				"Removing repo from team "+args[0],
				"Removed repo from team "+args[0])
		},
	}

	cmd.AddCommand(addCmd, removeCmd)
	return cmd
}

// runTeamMutation wraps a mutation that needs the admin guard. It resolves
// the vault, checks that the caller is a team admin, and runs fn with a
// fresh context + status spinner. progressMsg is shown while fn runs (e.g.
// "Adding alice to team platform") and doneMsg replaces it on success.
func runTeamMutation(cmd *cobra.Command, team string, fn func(ctx context.Context, v vault.Vault) error, progressMsg, doneMsg string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	v, err := loadVault()
	if err != nil {
		return err
	}
	if err := requireTeamAdmin(ctx, v, team); err != nil {
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

// requireTeamAdmin verifies the current actor is a team admin. This is a
// fast-fail UX check — the authoritative enforcement lives inside each
// common*Team helper, which re-checks admin after the flock + reload to
// close the TOCTOU window opened by this pre-check. Because
// commonCreateTeam auto-adds the creator as admin, there is no
// "bootstrap" escape: every team always has at least one admin.
func requireTeamAdmin(ctx context.Context, v vault.Vault, teamName string) error {
	actor, err := v.CurrentActor(ctx)
	if err != nil {
		return err
	}
	team, err := v.GetTeam(ctx, teamName)
	if err != nil {
		return err
	}
	if !team.IsAdmin(actor.Email) {
		return fmt.Errorf("%s is not an admin of team %s", actor.Email, teamName)
	}
	return nil
}

// loadVault returns the Vault for the active profile, discarding the
// config. Wraps loadConfigAndVault for commands that don't need the
// config separately.
func loadVault() (vault.Vault, error) {
	_, v, err := loadConfigAndVault()
	return v, err
}

func printTeamList(cmd *cobra.Command, teams []mgmt.Team) error {
	out := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
	if len(teams) == 0 {
		out.Muted("No teams defined yet. Create one with 'sx team create <name>'.")
		return nil
	}
	out.Header("Teams")
	out.Newline()
	for _, t := range teams {
		line := fmt.Sprintf("  %s %s",
			out.BoldText(t.Name),
			out.MutedText(fmt.Sprintf("%d members, %d repos",
				len(t.Members), len(t.Repositories))),
		)
		out.Println(line)
		if t.Description != "" {
			out.Muted("    " + t.Description)
		}
	}
	out.Newline()
	return nil
}

func printTeamDetails(cmd *cobra.Command, team *mgmt.Team) error {
	out := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
	out.Newline()
	out.Header(team.Name)
	if team.Description != "" {
		out.Println(team.Description)
	}
	out.Newline()

	if len(team.Members) > 0 {
		out.Bold("Members")
		for _, m := range team.Members {
			marker := " "
			if team.IsAdmin(m) {
				marker = "*"
			}
			out.Println(fmt.Sprintf("  %s %s", marker, m))
		}
		out.Newline()
	}

	if len(team.Repositories) > 0 {
		out.Bold("Repositories")
		for _, r := range team.Repositories {
			out.ListItem("•", r)
		}
		out.Newline()
	}

	return nil
}
