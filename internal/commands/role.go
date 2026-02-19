package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/components"
	"github.com/sleuth-io/sx/internal/vault"
)

// NewRoleCommand creates the role command with subcommands
func NewRoleCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "role",
		Short:  "Manage roles",
		Long:   "Manage roles for the current profile. Roles control which skills are available.",
		Hidden: true,
	}

	cmd.AddCommand(newRoleListCommand())
	cmd.AddCommand(newRoleSetCommand())
	cmd.AddCommand(newRoleClearCommand())
	cmd.AddCommand(newRoleCurrentCommand())

	return cmd
}

func newRoleListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available roles",
		RunE:  runRoleList,
	}
}

func newRoleSetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set <role-slug>",
		Short: "Set the active role",
		Args:  cobra.ExactArgs(1),
		RunE:  runRoleSet,
	}
}

func newRoleClearCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "Clear the active role",
		RunE:  runRoleClear,
	}
}

func newRoleCurrentCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show the current active role",
		RunE:  runRoleCurrent,
	}
}

// getSleuthVault loads the config and returns a SleuthVault, or an error if the
// current profile is not a Sleuth (remote) profile.
func getSleuthVault() (*vault.SleuthVault, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w\nRun 'sx init' to configure", err)
	}

	if cfg.GetType() != "sleuth" {
		return nil, errors.New("roles are only supported for remote (skills.new) profiles")
	}

	return vault.NewSleuthVault(cfg.GetServerURL(), cfg.GetAuthToken()), nil
}

func runRoleCurrent(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	sv, err := getSleuthVault()
	if err != nil {
		return err
	}

	result, err := sv.ListRoles(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch roles: %w", err)
	}

	if result.Active == nil {
		styledOut.Muted("No active role set.")
		return nil
	}

	// Find the active role to display its title
	for _, role := range result.Roles {
		if role.Slug == *result.Active {
			styledOut.Println(styledOut.BoldText(role.Title) + " " + styledOut.EmphasisText(role.Slug))
			return nil
		}
	}

	// Active slug exists but wasn't found in the role list (shouldn't happen normally)
	styledOut.Println(*result.Active)
	return nil
}

func runRoleList(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	sv, err := getSleuthVault()
	if err != nil {
		return err
	}

	status := components.NewStatus(cmd.OutOrStdout())
	status.Start("Fetching roles")

	result, err := sv.ListRoles(ctx)

	status.Done("")

	if err != nil {
		return fmt.Errorf("failed to fetch roles: %w", err)
	}

	if len(result.Roles) == 0 {
		styledOut.Muted("No roles configured.")
		return nil
	}

	styledOut.Newline()
	styledOut.Header("Roles")
	styledOut.Newline()

	for _, role := range result.Roles {
		isActive := result.Active != nil && *result.Active == role.Slug

		slugText := styledOut.EmphasisText(role.Slug)
		titleText := styledOut.BoldText(role.Title)

		if isActive {
			styledOut.SuccessItem(fmt.Sprintf("%s %s", titleText, slugText))
		} else {
			styledOut.ListItem(" ", fmt.Sprintf("%s %s", titleText, slugText))
		}

		if role.Description != "" {
			styledOut.Muted("    " + role.Description)
		}
	}

	styledOut.Newline()
	return nil
}

func runRoleSet(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	sv, err := getSleuthVault()
	if err != nil {
		return err
	}

	slug := args[0]

	status := components.NewStatus(cmd.OutOrStdout())
	status.Start("Setting active role")

	role, err := sv.SetActiveRole(ctx, &slug)

	status.Done("")

	if err != nil {
		return fmt.Errorf("failed to set role: %w", err)
	}

	styledOut.Success("Active role set to " + styledOut.BoldText(role.Title) + " (" + role.Slug + ")")
	return nil
}

func runRoleClear(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	sv, err := getSleuthVault()
	if err != nil {
		return err
	}

	status := components.NewStatus(cmd.OutOrStdout())
	status.Start("Clearing active role")

	_, err = sv.SetActiveRole(ctx, nil)

	status.Done("")

	if err != nil {
		return fmt.Errorf("failed to clear role: %w", err)
	}

	styledOut.Success("Active role cleared")
	return nil
}
