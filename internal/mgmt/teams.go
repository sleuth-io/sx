// Package mgmt contains shared data types and serializers for sx management
// features: teams, installations, usage events, and audit events. These are
// used by file-backed vaults (git, path) to persist management state under
// the .sx/ subtree at the vault root.
package mgmt

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/sleuth-io/sx/internal/scope"
)

// TeamsFileName is the path relative to the vault root where team definitions
// are persisted.
const TeamsFileName = ".sx/teams.toml"

// TeamsLockVersion is the current schema version for teams.toml.
const TeamsLockVersion = "1"

// ErrTeamNotFound is returned when a team lookup fails.
var ErrTeamNotFound = errors.New("team not found")

// ErrTeamExists is returned when attempting to create a team that already
// exists.
var ErrTeamExists = errors.New("team already exists")

// ErrLastAdmin is returned when a mutation would leave a team with zero
// admins, which would render the team permanently unmanageable (no actor
// could later recover admin rights through the admin-gated helpers).
var ErrLastAdmin = errors.New("team would be left without an admin")

// ErrEmptyTeamName is returned when a team name is blank or whitespace-
// only. Names are the primary key of .sx/teams.toml so an empty one would
// collide with any other empty-named team and hide from lookups.
var ErrEmptyTeamName = errors.New("team name cannot be empty")

// Team is a named grouping of members, admins, and repositories. It is the
// unit of targeted installation for git and path vaults.
type Team struct {
	Name         string   `toml:"name"`
	Description  string   `toml:"description,omitempty"`
	Members      []string `toml:"members,omitempty"`
	Admins       []string `toml:"admins,omitempty"`
	Repositories []string `toml:"repositories,omitempty"`
}

// TeamsFile is the on-disk representation of .sx/teams.toml.
type TeamsFile struct {
	LockVersion string `toml:"lock-version"`
	Teams       []Team `toml:"team,omitempty"`
}

// LoadTeams reads .sx/teams.toml from the given vault root. A missing file
// returns an empty TeamsFile with the current lock version, not an error.
func LoadTeams(vaultRoot string) (*TeamsFile, error) {
	path := filepath.Join(vaultRoot, TeamsFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &TeamsFile{LockVersion: TeamsLockVersion}, nil
		}
		return nil, fmt.Errorf("failed to read teams file: %w", err)
	}

	var tf TeamsFile
	if err := toml.Unmarshal(data, &tf); err != nil {
		return nil, fmt.Errorf("failed to parse teams file: %w", err)
	}
	if tf.LockVersion == "" {
		tf.LockVersion = TeamsLockVersion
	}
	return &tf, nil
}

// SaveTeams writes .sx/teams.toml to the given vault root, creating parent
// directories as needed. Team members, admins, and repositories are
// normalized (trimmed, lowercased for emails, url-normalized for repos) and
// deduplicated before writing so equality comparisons downstream stay cheap.
func SaveTeams(vaultRoot string, tf *TeamsFile) error {
	if tf.LockVersion == "" {
		tf.LockVersion = TeamsLockVersion
	}
	for i := range tf.Teams {
		normalizeTeamInPlace(&tf.Teams[i])
	}

	path := filepath.Join(vaultRoot, TeamsFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create .sx directory: %w", err)
	}

	buf := new(bytes.Buffer)
	if err := toml.NewEncoder(buf).Encode(tf); err != nil {
		return fmt.Errorf("failed to encode teams file: %w", err)
	}
	return writeFileAtomic(path, buf.Bytes(), 0644)
}

// FindTeam returns the team with the given name, or ErrTeamNotFound if no
// such team exists.
func (tf *TeamsFile) FindTeam(name string) (*Team, error) {
	for i := range tf.Teams {
		if tf.Teams[i].Name == name {
			return &tf.Teams[i], nil
		}
	}
	return nil, ErrTeamNotFound
}

// UpsertTeam adds or replaces a team with the given team's name. Returns the
// inserted/updated team pointer from the file's slice, or nil alongside
// ErrEmptyTeamName when the normalized name is blank.
func (tf *TeamsFile) UpsertTeam(t Team) (*Team, error) {
	normalizeTeamInPlace(&t)
	if t.Name == "" {
		return nil, ErrEmptyTeamName
	}
	for i := range tf.Teams {
		if tf.Teams[i].Name == t.Name {
			tf.Teams[i] = t
			return &tf.Teams[i], nil
		}
	}
	tf.Teams = append(tf.Teams, t)
	return &tf.Teams[len(tf.Teams)-1], nil
}

// DeleteTeam removes the team with the given name. Returns ErrTeamNotFound
// if no such team exists.
func (tf *TeamsFile) DeleteTeam(name string) error {
	for i := range tf.Teams {
		if tf.Teams[i].Name == name {
			tf.Teams = append(tf.Teams[:i], tf.Teams[i+1:]...)
			return nil
		}
	}
	return ErrTeamNotFound
}

// TeamsForMember returns all teams that include the given email as a member.
// Email comparison is case-insensitive and trimmed.
func (tf *TeamsFile) TeamsForMember(email string) []*Team {
	needle := NormalizeEmail(email)
	if needle == "" {
		return nil
	}
	var matches []*Team
	for i := range tf.Teams {
		if slices.Contains(tf.Teams[i].Members, needle) {
			matches = append(matches, &tf.Teams[i])
		}
	}
	return matches
}

// TeamsOwningRepo returns all teams whose repositories list contains the
// given repository URL (compared after normalization).
func (tf *TeamsFile) TeamsOwningRepo(repoURL string) []*Team {
	needle := scope.NormalizeRepoURL(repoURL)
	if needle == "" {
		return nil
	}
	var matches []*Team
	for i := range tf.Teams {
		for _, r := range tf.Teams[i].Repositories {
			if scope.NormalizeRepoURL(r) == needle {
				matches = append(matches, &tf.Teams[i])
				break
			}
		}
	}
	return matches
}

// IsMember returns true if the given email is in the team's member list.
func (t *Team) IsMember(email string) bool {
	return slices.Contains(t.Members, NormalizeEmail(email))
}

// IsAdmin returns true if the given email is in the team's admin list.
func (t *Team) IsAdmin(email string) bool {
	return slices.Contains(t.Admins, NormalizeEmail(email))
}

// NormalizeEmail lowercases and trims an email for comparison.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// normalizeTeamInPlace normalizes email and repo fields on a team so the
// persisted file is stable.
func normalizeTeamInPlace(t *Team) {
	t.Name = strings.TrimSpace(t.Name)
	t.Description = strings.TrimSpace(t.Description)
	t.Members = dedupeSorted(normalizeEmails(t.Members))
	t.Admins = dedupeSorted(normalizeEmails(t.Admins))
	t.Repositories = dedupeSorted(normalizeRepos(t.Repositories))
}

func normalizeEmails(in []string) []string {
	out := make([]string, 0, len(in))
	for _, e := range in {
		n := NormalizeEmail(e)
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

func normalizeRepos(in []string) []string {
	out := make([]string, 0, len(in))
	for _, r := range in {
		n := scope.NormalizeRepoURL(strings.TrimSpace(r))
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

func dedupeSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}
