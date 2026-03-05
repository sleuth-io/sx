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

const browseTopLimit = 50

const searchResultLimit = 20

// browseCommunitySkills presents the skills.sh browser with search and pagination.
// Returns true if any skills were added.
func browseCommunitySkills(cmd *cobra.Command) bool {
	styledOut := ui.NewOutput(cmd.OutOrStdout(), cmd.ErrOrStderr())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	status := components.NewStatus(cmd.OutOrStdout())
	status.Start("Loading skills from skills.sh")

	topSkills, err := registry.FetchTopSkills(ctx, browseTopLimit)
	if err != nil {
		status.Fail("Failed to load skills")
		styledOut.Error(fmt.Sprintf("Could not reach skills.sh: %v", err))
		return false
	}
	status.Done("")

	var addedAny bool
	query := ""
	offset := 0
	// current holds the active skill list: top skills initially, search results after a query
	current := topSkills

	for {
		if len(current) == 0 {
			styledOut.Newline()
			if query != "" {
				styledOut.Info(fmt.Sprintf("No skills found for \"%s\".", query))
			} else {
				styledOut.Info("No skills available.")
			}
		}

		// Clamp offset
		if offset >= len(current) {
			offset = 0
		}

		// Get current page
		end := offset + browsePageSize
		if end > len(current) {
			end = len(current)
		}
		page := current[offset:end]

		// Build options
		var options []components.Option

		// Skill options first — the primary content
		for _, s := range page {
			label := fmt.Sprintf("%s (%s)", s.Name, s.Source)
			options = append(options, components.Option{
				Label:       label,
				Value:       fmt.Sprintf("%s/%s", s.Source, s.SkillID),
				Description: fmt.Sprintf("%s installs", s.FormatInstalls()),
			})
		}

		// Show more option if there are more results
		if end < len(current) {
			remaining := len(current) - end
			options = append(options, components.Option{
				Label:       fmt.Sprintf("Show more (%d remaining)", remaining),
				Value:       "more",
				Description: "Load next page",
			})
		}

		// Search option
		searchLabel := "Search skills.sh"
		searchDesc := "Search across all skills"
		if query != "" {
			searchLabel = fmt.Sprintf("New search (current: \"%s\")", query)
			searchDesc = "Search again or clear"
		}
		options = append(options, components.Option{
			Label:       searchLabel,
			Value:       "search",
			Description: searchDesc,
		})

		// Clear search option — only show when a search is active
		if query != "" {
			options = append(options, components.Option{
				Label:       "Back to top skills",
				Value:       "clear",
				Description: "Clear search",
			})
		}

		// Done option last
		options = append(options, components.Option{
			Label:       "Done",
			Value:       "done",
			Description: "Exit browser",
		})

		styledOut.Newline()

		// Build the select title with context
		var title string
		if len(current) == 0 {
			title = "Browse skills.sh:"
		} else if query != "" {
			title = fmt.Sprintf("Results for \"%s\" (%d-%d of %d):",
				query, offset+1, end, len(current))
		} else {
			title = fmt.Sprintf("Popular skills (%d-%d of %d):",
				offset+1, end, len(current))
		}

		selected, err := components.SelectWithDefault(title, options, 0)
		if err != nil {
			break
		}

		switch selected.Value {
		case "done":
			goto done
		case "clear":
			query = ""
			offset = 0
			current = topSkills
		case "search":
			styledOut.Newline()
			newQuery, err := components.InputWithPlaceholder("Search:", "e.g. react, testing, python...")
			if err != nil {
				goto done
			}
			offset = 0
			if newQuery == "" {
				query = ""
				current = topSkills
			} else {
				query = newQuery
				if len(query) < 2 {
					// API requires min 2 chars; fall back to local filtering
					current = registry.Search(topSkills, query)
				} else {
					searchCtx, searchCancel := context.WithTimeout(context.Background(), 15*time.Second)
					searchStatus := components.NewStatus(cmd.OutOrStdout())
					searchStatus.Start(fmt.Sprintf("Searching for \"%s\"", query))
					results, searchErr := registry.SearchSkills(searchCtx, query, searchResultLimit)
					searchCancel()
					if searchErr != nil {
						searchStatus.Fail(fmt.Sprintf("Search failed: %v", searchErr))
						current = topSkills
						query = ""
					} else {
						searchStatus.Done(fmt.Sprintf("Found %d results", len(results)))
						current = results
					}
				}
			}
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
