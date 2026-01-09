package commands

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/ui"
)

// NewProfileCommand creates the profile command with subcommands
func NewProfileCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage configuration profiles",
		Long:  "Manage multiple configuration profiles for switching between different servers.",
	}

	cmd.AddCommand(newProfileListCommand())
	cmd.AddCommand(newProfileAddCommand())
	cmd.AddCommand(newProfileUseCommand())
	cmd.AddCommand(newProfileCurrentCommand())
	cmd.AddCommand(newProfileRemoveCommand())

	return cmd
}

func newProfileListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all profiles",
		RunE:  runProfileList,
	}
}

func newProfileAddCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "add <profile-name>",
		Short: "Add a new profile",
		Args:  cobra.ExactArgs(1),
		RunE:  runProfileAdd,
	}
}

func runProfileAdd(cmd *cobra.Command, args []string) error {
	profileName := args[0]

	// Check if profile already exists
	mpc, _ := config.LoadMultiProfile()
	if mpc != nil {
		if _, exists := mpc.GetProfile(profileName); exists {
			return fmt.Errorf("profile already exists: %s", profileName)
		}
	}

	// Set the active profile and run init
	config.SetActiveProfile(profileName)
	return runInit(cmd, nil, "", "", "", "")
}

func newProfileUseCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "use <profile-name>",
		Short: "Switch to a profile",
		Args:  cobra.ExactArgs(1),
		RunE:  runProfileUse,
	}
}

func newProfileCurrentCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show the current active profile",
		RunE:  runProfileCurrent,
	}
}

func newProfileRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <profile-name>",
		Short: "Remove a profile",
		Args:  cobra.ExactArgs(1),
		RunE:  runProfileRemove,
	}
}

func runProfileList(cmd *cobra.Command, args []string) error {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return err
	}

	activeProfile := config.GetActiveProfileName(mpc)
	profiles := mpc.ListProfiles()

	if len(profiles) == 0 {
		styledOut.Muted("No profiles configured. Run 'sx init' to create one.")
		return nil
	}

	for _, name := range profiles {
		profile, _ := mpc.GetProfile(name)

		// Build description based on profile type
		desc := profile.RepositoryURL
		if desc == "" {
			desc = profile.ServerURL
		}

		if name == activeProfile {
			styledOut.SuccessItem(styledOut.BoldText(name) + " " + styledOut.MutedText(desc))
		} else {
			styledOut.ListItem(" ", name+" "+styledOut.MutedText(desc))
		}
	}

	return nil
}

func runProfileUse(cmd *cobra.Command, args []string) error {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
	profileName := args[0]

	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return err
	}

	if err := mpc.SetDefaultProfile(profileName); err != nil {
		return err
	}

	if err := config.SaveMultiProfile(mpc); err != nil {
		return err
	}

	styledOut.Success("Switched to profile: " + profileName)
	return nil
}

func runProfileCurrent(cmd *cobra.Command, args []string) error {
	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return err
	}

	activeProfile := config.GetActiveProfileName(mpc)
	fmt.Println(activeProfile)
	return nil
}

func runProfileRemove(cmd *cobra.Command, args []string) error {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
	profileName := args[0]

	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return err
	}

	// Don't allow removing the last profile
	if len(mpc.Profiles) <= 1 {
		return errors.New("cannot remove the last profile")
	}

	if err := mpc.DeleteProfile(profileName); err != nil {
		return err
	}

	if err := config.SaveMultiProfile(mpc); err != nil {
		return err
	}

	styledOut.Success("Removed profile: " + profileName)
	return nil
}
