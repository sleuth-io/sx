package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/registry"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/components"
)

// browseCommunitySkills presents the featured skills browser loop.
// Returns true if any skills were added.
func browseCommunitySkills(cmd *cobra.Command) bool {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	skills, err := registry.FeaturedSkills()
	if err != nil || len(skills) == 0 {
		if err != nil {
			styledOut.Error(fmt.Sprintf("Failed to load community skills: %v", err))
		} else {
			styledOut.Info("No community skills available.")
		}
		return false
	}

	var addedAny bool
	for {
		styledOut.Newline()

		// Build options with done at the top, then skills
		options := make([]components.Option, len(skills)+1)
		options[0] = components.Option{
			Label: "Done",
			Value: "done",
		}
		for i, skill := range skills {
			options[i+1] = components.Option{
				Label:       skill.Name,
				Value:       skill.URL,
				Description: skill.Description,
			}
		}

		selected, err := components.SelectWithDefault("Select a skill to add:", options, 0)
		if err != nil || selected.Value == "done" {
			break
		}

		styledOut.Newline()

		// Run the add command with the skill URL (skip install prompt, we'll do it at the end)
		if err := runAddSkipInstall(cmd, selected.Value); err != nil {
			styledOut.Error(fmt.Sprintf("Failed to add asset: %v", err))
		} else {
			addedAny = true
		}
	}

	return addedAny
}
