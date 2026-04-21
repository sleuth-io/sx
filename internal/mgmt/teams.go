// Package mgmt contains shared data types and serializers for sx
// management features: teams (interface types only — on-disk storage
// lives in the manifest package), identity resolution, usage events,
// and audit events.
package mgmt

import (
	"errors"
	"slices"
	"strings"
)

// ErrTeamNotFound is returned when a team lookup fails.
var ErrTeamNotFound = errors.New("team not found")

// ErrTeamExists is returned when attempting to create a team that already
// exists.
var ErrTeamExists = errors.New("team already exists")

// ErrLastAdmin is returned when a mutation would leave a team with zero
// admins, which would render the team permanently unmanageable.
var ErrLastAdmin = errors.New("team would be left without an admin")

// ErrEmptyTeamName is returned when a team name is blank or whitespace-
// only. Names are the primary key of teams in the manifest so an empty
// one would collide with any other empty-named team and hide from lookups.
var ErrEmptyTeamName = errors.New("team name cannot be empty")

// Team is a named grouping of members, admins, and repositories. It is
// the unit of targeted installation for git and path vaults and the
// shape returned by the Vault interface's team-management methods.
type Team struct {
	Name         string   `toml:"name"`
	Description  string   `toml:"description,omitempty"`
	Members      []string `toml:"members,omitempty"`
	Admins       []string `toml:"admins,omitempty"`
	Repositories []string `toml:"repositories,omitempty"`
}

// IsMember returns true if the given email is in the team's member list.
// Comparison is case-insensitive.
func (t *Team) IsMember(email string) bool {
	return slices.Contains(t.Members, NormalizeEmail(email))
}

// IsAdmin returns true if the given email is in the team's admin list.
// Comparison is case-insensitive.
func (t *Team) IsAdmin(email string) bool {
	return slices.Contains(t.Admins, NormalizeEmail(email))
}

// NormalizeEmail lowercases and trims an email for comparison.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
