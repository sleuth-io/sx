package commands

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/mgmt"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/components"
)

// NewProfileCommand creates the profile command with subcommands
func NewProfileCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage configuration profiles",
		Long:  "Manage multiple configuration profiles for switching between different vaults. Multiple profiles can be active at the same time; sx install merges assets from every active profile.",
	}

	cmd.AddCommand(newProfileListCommand())
	cmd.AddCommand(newProfileAddCommand())
	cmd.AddCommand(newProfileUseCommand())
	cmd.AddCommand(newProfileCurrentCommand())
	cmd.AddCommand(newProfileRemoveCommand())
	cmd.AddCommand(newProfileActivateCommand())
	cmd.AddCommand(newProfileDeactivateCommand())
	cmd.AddCommand(newProfileDefaultCommand())
	cmd.AddCommand(newProfileEditCommand())

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
		Long: `Add a new configuration profile with the specified name.

Each profile can connect to a different vault (local path, Git repo, or Skills.new).
After creating a profile, use 'sx profile activate <name>' to add it to the active
set, or 'sx profile use <name>' to make it the only active profile.`,
		Example: `  sx profile add work
  sx profile add personal
  sx profile add local`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("missing profile name\n\nUsage: sx profile add <profile-name>\n\nExample: sx profile add work")
			}
			if len(args) > 1 {
				return fmt.Errorf("expected 1 argument, got %d", len(args))
			}
			return nil
		},
		RunE: runProfileAdd,
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
	if err := runInit(cmd, nil, "", "", "", ""); err != nil {
		return err
	}

	// Prompt for identity now that the profile is saved.
	return promptProfileIdentity(cmd, profileName)
}

// promptProfileIdentity asks the user which email should be used for
// team/user scope resolution in this profile and saves it. The default
// is the profile's existing identity, falling back to git config
// user.email.
func promptProfileIdentity(cmd *cobra.Command, profileName string) error {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return err
	}
	profile, ok := mpc.GetProfile(profileName)
	if !ok {
		return fmt.Errorf("profile not found after init: %s", profileName)
	}

	defaultIdentity := profile.Identity
	if defaultIdentity == "" {
		actor, gitErr := mgmt.CurrentGitActor(cmd.Context(), "")
		if gitErr == nil && !actor.Synthetic && !actor.IsBot() {
			defaultIdentity = actor.Email
		}
	}

	styledOut.Newline()
	styledOut.Muted("Used to resolve team and user-scoped installs in this profile.")
	identity, err := components.InputWithDefault("Identity email for profile "+profileName, defaultIdentity)
	if err != nil {
		// Non-interactive or cancelled — keep whatever was already there.
		return nil //nolint:nilerr // identity is optional; fall back to git config at use time.
	}
	identity = strings.TrimSpace(identity)
	if identity == "" || identity == profile.Identity {
		return nil
	}
	profile.Identity = identity
	mpc.SetProfile(profileName, profile)
	return config.SaveMultiProfile(mpc)
}

func newProfileUseCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "use <profile-name>",
		Short: "Switch to a profile exclusively (deactivates all others)",
		Long:  "Exclusive activation: makes the named profile the only active profile and sets it as the default. Use 'sx profile activate' to add to the active set without deactivating others.",
		Args:  cobra.ExactArgs(1),
		RunE:  runProfileUse,
	}
	cmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt when deactivating other active profiles")
	return cmd
}

func newProfileCurrentCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show the active profiles",
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

func newProfileActivateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "activate <profile-name>",
		Short: "Add a profile to the active set",
		Long:  "Adds the named profile to the active set without deactivating other profiles. sx install will then merge assets from every active profile.",
		Args:  cobra.ExactArgs(1),
		RunE:  runProfileActivate,
	}
}

func newProfileDeactivateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deactivate <profile-name>",
		Short: "Remove a profile from the active set",
		Long:  "Removes the profile from the active set. Cannot deactivate the last active profile. Assets installed from the deactivated profile are removed on the next sx install.",
		Args:  cobra.ExactArgs(1),
		RunE:  runProfileDeactivate,
	}
	cmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func newProfileDefaultCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "default <profile-name>",
		Short: "Set the default profile (target for writes and conflict tiebreaker)",
		Long:  "The default profile owns mutations when no --profile flag is given and wins conflicts when multiple active profiles publish an asset with the same name. Activates the profile if it isn't already active.",
		Args:  cobra.ExactArgs(1),
		RunE:  runProfileDefault,
	}
}

func newProfileEditCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit <profile-name>",
		Short: "Edit profile fields (identity email)",
		Args:  cobra.ExactArgs(1),
		RunE:  runProfileEdit,
	}
	cmd.Flags().String("identity", "", "Identity email for team/user scope resolution")
	return cmd
}

func runProfileList(cmd *cobra.Command, args []string) error {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return err
	}

	defaultProfile := mpc.DefaultProfile
	activeSet := make(map[string]bool, len(mpc.ActiveProfiles))
	for _, n := range mpc.ActiveProfiles {
		activeSet[n] = true
	}
	profiles := mpc.ListProfiles()

	if len(profiles) == 0 {
		styledOut.Muted("No profiles configured. Run 'sx init' to create one.")
		return nil
	}

	for _, name := range profiles {
		profile, _ := mpc.GetProfile(name)

		desc := profile.RepositoryURL
		if desc == "" {
			desc = profile.ServerURL
		}
		if profile.Identity != "" {
			desc += " (" + profile.Identity + ")"
		}

		marker := "  "
		switch {
		case activeSet[name] && name == defaultProfile:
			marker = "✓★"
		case activeSet[name]:
			marker = "✓ "
		case name == defaultProfile:
			marker = " ★"
		}

		label := name
		if name == defaultProfile {
			label = styledOut.BoldText(name)
		}
		styledOut.ListItem(marker, label+" "+styledOut.MutedText(desc))
	}

	styledOut.Newline()
	styledOut.Muted("✓ = active   ★ = default")
	return nil
}

func runProfileUse(cmd *cobra.Command, args []string) error {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
	profileName := args[0]

	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return err
	}

	if _, ok := mpc.GetProfile(profileName); !ok {
		return fmt.Errorf("profile not found: %s", profileName)
	}

	// If other profiles are currently active, warn before silently
	// deactivating them — sx install would otherwise clean up their
	// installed assets on the next run.
	var toDeactivate []string
	for _, n := range mpc.ActiveProfiles {
		if n != profileName {
			toDeactivate = append(toDeactivate, n)
		}
	}
	if len(toDeactivate) > 0 {
		skipConfirm, _ := cmd.Flags().GetBool("yes")
		if !skipConfirm {
			styledOut.Warning("This will deactivate: " + strings.Join(toDeactivate, ", "))
			styledOut.Muted("Their installed assets will be removed on the next sx install.")
			confirmed, err := components.Confirm("Continue?", true)
			if err != nil || !confirmed {
				return errors.New("profile use cancelled")
			}
		}
	}

	// Exclusive activation: shrink ActiveProfiles to just this one.
	mpc.ActiveProfiles = []string{profileName}
	if err := mpc.SetDefaultProfile(profileName); err != nil {
		return err
	}

	if err := config.SaveMultiProfile(mpc); err != nil {
		return err
	}

	styledOut.Success("Switched to profile: " + profileName + " (now the only active profile)")
	return nil
}

func runProfileCurrent(cmd *cobra.Command, args []string) error {
	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return err
	}

	names := config.GetActiveProfileNames(mpc)
	for _, name := range names {
		if name == mpc.DefaultProfile {
			fmt.Println(name + " (default)")
		} else {
			fmt.Println(name)
		}
	}
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

func runProfileActivate(cmd *cobra.Command, args []string) error {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
	profileName := args[0]

	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return err
	}

	if err := mpc.Activate(profileName); err != nil {
		return err
	}
	if err := config.SaveMultiProfile(mpc); err != nil {
		return err
	}

	styledOut.Success("Activated profile: " + profileName)
	active := strings.Join(mpc.ActiveProfiles, ", ")
	styledOut.Muted("Active profiles: " + active)
	return nil
}

func runProfileDeactivate(cmd *cobra.Command, args []string) error {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
	profileName := args[0]

	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return err
	}

	if !mpc.IsProfileActive(profileName) {
		return fmt.Errorf("profile not active: %s", profileName)
	}

	skipConfirm, _ := cmd.Flags().GetBool("yes")
	if !skipConfirm {
		styledOut.Warning("Deactivating " + profileName + " will remove its installed assets on the next sx install.")
		confirmed, err := components.Confirm("Continue?", true)
		if err != nil || !confirmed {
			return errors.New("deactivation cancelled")
		}
	}

	if err := mpc.Deactivate(profileName); err != nil {
		return err
	}
	if err := config.SaveMultiProfile(mpc); err != nil {
		return err
	}

	styledOut.Success("Deactivated profile: " + profileName)
	styledOut.Muted("Run 'sx install' to remove its installed assets.")
	return nil
}

func runProfileDefault(cmd *cobra.Command, args []string) error {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
	profileName := args[0]

	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return err
	}

	wasActive := mpc.IsProfileActive(profileName)
	if err := mpc.SetDefaultProfile(profileName); err != nil {
		return err
	}
	if err := config.SaveMultiProfile(mpc); err != nil {
		return err
	}

	if !wasActive {
		styledOut.Success("Activated " + profileName + " and set as default")
	} else {
		styledOut.Success("Default profile is now: " + profileName)
	}
	return nil
}

func runProfileEdit(cmd *cobra.Command, args []string) error {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())
	profileName := args[0]

	mpc, err := config.LoadMultiProfile()
	if err != nil {
		return err
	}
	profile, ok := mpc.GetProfile(profileName)
	if !ok {
		return fmt.Errorf("profile not found: %s", profileName)
	}

	identityFlag, _ := cmd.Flags().GetString("identity")
	if identityFlag != "" {
		profile.Identity = strings.TrimSpace(identityFlag)
		mpc.SetProfile(profileName, profile)
		if err := config.SaveMultiProfile(mpc); err != nil {
			return err
		}
		styledOut.Success("Updated identity for " + profileName + ": " + profile.Identity)
		return nil
	}

	// Interactive fallback
	if err := promptProfileIdentity(cmd, profileName); err != nil {
		return err
	}
	return nil
}
