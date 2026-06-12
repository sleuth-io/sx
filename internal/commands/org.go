package commands

import (
	"context"
	"errors"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/components"
)

// orgAdminManager is implemented by file-backed vaults that store the
// vault-level org-admin list. The Sleuth vault manages roles server-side.
type orgAdminManager interface {
	AddOrgAdmins(ctx context.Context, emails []string) (int, error)
	RemoveOrgAdmin(ctx context.Context, email string) error
	ListOrgAdmins(ctx context.Context) ([]string, error)
}

func orgAdminVault() (orgAdminManager, error) {
	vault, err := createVault()
	if err != nil {
		return nil, err
	}
	mgr, ok := vault.(orgAdminManager)
	if !ok {
		return nil, errors.New("org-admins are managed on the server for this vault; this command is only for git/path vaults")
	}
	return mgr, nil
}

// NewOrgCommand returns `sx org` (vault-level governance).
func NewOrgCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "org", Short: "Manage vault-level governance"}
	admin := &cobra.Command{Use: "admin", Short: "Manage org-admins (who controls scope)"}
	admin.AddCommand(newOrgAdminAddCommand(), newOrgAdminListCommand(), newOrgAdminRemoveCommand())
	cmd.AddCommand(admin)
	return cmd
}

func newOrgAdminAddCommand() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "add <email...>",
		Short: "Add org-admins (turns on scope governance)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			out := newOutputHelper(cmd)

			mgr, err := orgAdminVault()
			if err != nil {
				return err
			}
			current, err := mgr.ListOrgAdmins(ctx)
			if err != nil {
				return err
			}
			// Bootstrap: seeding the first org-admin locks scope governance.
			if len(current) == 0 && !yes {
				styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
				styledOut.Warning("This vault has no org-admins yet. Setting them turns on scope governance:\n  from now on only these people can set broad scopes and change this list.")
				confirmed, err := components.ConfirmWithIO("Continue?", false, cmd.InOrStdin(), cmd.OutOrStdout())
				if err != nil {
					return err
				}
				if !confirmed {
					out.println("Cancelled.")
					return nil
				}
			}

			added, err := mgr.AddOrgAdmins(ctx, args)
			if err != nil {
				return err
			}
			out.printf("✓ Added %d org-admin(s)\n", added)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the governance-lock confirmation")
	return cmd
}

func newOrgAdminListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List org-admins",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			out := newOutputHelper(cmd)

			mgr, err := orgAdminVault()
			if err != nil {
				return err
			}
			admins, err := mgr.ListOrgAdmins(ctx)
			if err != nil {
				return err
			}
			if len(admins) == 0 {
				out.println("No org-admins — this vault is ungoverned (anyone can set any scope).")
				return nil
			}
			for _, a := range admins {
				out.printf("  %s\n", a)
			}
			return nil
		},
	}
}

func newOrgAdminRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <email>",
		Short: "Remove an org-admin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			out := newOutputHelper(cmd)

			mgr, err := orgAdminVault()
			if err != nil {
				return err
			}
			if err := mgr.RemoveOrgAdmin(ctx, args[0]); err != nil {
				return err
			}
			out.printf("✓ Removed org-admin %s\n", args[0])
			return nil
		},
	}
}
