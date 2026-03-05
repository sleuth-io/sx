package commands

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/registry"
	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/components"
)

const browsePageSize = 5

// browseCommunitySkills presents the skills.sh browser with search and pagination.
// Returns true if any skills were added.
func browseCommunitySkills(cmd *cobra.Command) bool {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	status := components.NewStatus(cmd.OutOrStdout())
	status.Start("Loading skills from skills.sh")

	allSkills, err := registry.FetchSkills(ctx)
	if err != nil {
		status.Fail("Failed to load skills")
		styledOut.Error(fmt.Sprintf("Failed to load skills from skills.sh: %v", err))
		return false
	}
	status.Done(fmt.Sprintf("Loaded %d skills", len(allSkills)))

	var addedAny bool
	query := ""
	offset := 0

	for {
		// Filter by current search query
		filtered := registry.Search(allSkills, query)

		if len(filtered) == 0 {
			styledOut.Newline()
			if query != "" {
				styledOut.Info(fmt.Sprintf("No skills matching \"%s\".", query))
			} else {
				styledOut.Info("No skills available.")
			}
		}

		// Clamp offset
		if offset >= len(filtered) {
			offset = 0
		}

		// Get current page
		end := offset + browsePageSize
		if end > len(filtered) {
			end = len(filtered)
		}
		page := filtered[offset:end]

		// Build options
		var options []components.Option

		// Search option always first
		searchLabel := "Search skills..."
		if query != "" {
			searchLabel = fmt.Sprintf("Search skills... (current: \"%s\")", query)
		}
		options = append(options, components.Option{
			Label: searchLabel,
			Value: "search",
		})

		// Done option
		options = append(options, components.Option{
			Label: "Done",
			Value: "done",
		})

		// Skill options
		for _, s := range page {
			label := fmt.Sprintf("%s (%s)", s.Name, s.Source)
			options = append(options, components.Option{
				Label:       label,
				Value:       fmt.Sprintf("%s/%s", s.Source, s.SkillID),
				Description: fmt.Sprintf("%s installs", s.FormatInstalls()),
			})
		}

		// Show more option if there are more results
		if end < len(filtered) {
			remaining := len(filtered) - end
			options = append(options, components.Option{
				Label: fmt.Sprintf("Show more (%d remaining)", remaining),
				Value: "more",
			})
		}

		styledOut.Newline()

		// Show result count context
		if query != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Showing %d-%d of %d results for \"%s\"\n",
				offset+1, end, len(filtered), query)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Showing %d-%d of %d skills\n",
				offset+1, end, len(filtered))
		}

		selected, err := components.SelectWithDefault("Browse skills.sh:", options, 0)
		if err != nil {
			break
		}

		switch selected.Value {
		case "done":
			goto done
		case "search":
			styledOut.Newline()
			newQuery, err := components.InputWithPlaceholder("Search:", "e.g. react, testing, python...")
			if err != nil {
				goto done
			}
			query = newQuery
			offset = 0
		case "more":
			offset = end
		default:
			// User selected a skill — add it
			styledOut.Newline()
			if err := runAddSkipInstall(cmd, selected.Value); err != nil {
				styledOut.Error(fmt.Sprintf("Failed to add skill: %v", err))
			} else {
				addedAny = true
			}
		}
	}

done:
	return addedAny
}
