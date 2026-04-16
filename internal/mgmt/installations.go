package mgmt

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// InstallationsFileName is the path relative to the vault root where team
// and user installations are persisted.
const InstallationsFileName = ".sx/installations.toml"

// InstallationsLockVersion is the current schema version for installations.toml.
const InstallationsLockVersion = "1"

// InstallKind identifies the type of an installation target.
type InstallKind string

const (
	// InstallKindTeam is an installation scoped to every member of a team.
	// The team is resolved from .sx/teams.toml at read time; caller membership
	// determines visibility and the team's repositories determine the
	// flattened repo scope list.
	InstallKindTeam InstallKind = "team"

	// InstallKindUser is an installation scoped to a single user, identified
	// by email.
	InstallKindUser InstallKind = "user"
)

// ErrInvalidInstallationKind is returned when an installation row carries a
// kind that is not recognized.
var ErrInvalidInstallationKind = errors.New("invalid installation kind")

// Installation is one row in .sx/installations.toml. For kind=team, Team is
// set; for kind=user, User is set.
type Installation struct {
	Asset string      `toml:"asset"`
	Kind  InstallKind `toml:"kind"`
	Team  string      `toml:"team,omitempty"`
	User  string      `toml:"user,omitempty"`
}

// InstallationsFile is the on-disk representation of .sx/installations.toml.
type InstallationsFile struct {
	LockVersion   string         `toml:"lock-version"`
	Installations []Installation `toml:"installation,omitempty"`
}

// LoadInstallations reads .sx/installations.toml from the given vault root.
// Returns (nil, false, nil) when the file does not exist — the fast path so
// GetLockFile can skip the overlay step entirely for vaults that have never
// used team/user installs.
func LoadInstallations(vaultRoot string) (*InstallationsFile, bool, error) {
	path := filepath.Join(vaultRoot, InstallationsFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to read installations file: %w", err)
	}

	var ifile InstallationsFile
	if err := toml.Unmarshal(data, &ifile); err != nil {
		return nil, false, fmt.Errorf("failed to parse installations file: %w", err)
	}
	if ifile.LockVersion == "" {
		ifile.LockVersion = InstallationsLockVersion
	}
	return &ifile, true, nil
}

// SaveInstallations writes .sx/installations.toml to the given vault root,
// creating parent directories as needed. Installations are normalized and
// deduplicated before writing.
func SaveInstallations(vaultRoot string, ifile *InstallationsFile) error {
	if ifile.LockVersion == "" {
		ifile.LockVersion = InstallationsLockVersion
	}
	ifile.Installations = normalizeInstallations(ifile.Installations)

	path := filepath.Join(vaultRoot, InstallationsFileName)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create .sx directory: %w", err)
	}

	buf := new(bytes.Buffer)
	if err := toml.NewEncoder(buf).Encode(ifile); err != nil {
		return fmt.Errorf("failed to encode installations file: %w", err)
	}
	return writeFileAtomic(path, buf.Bytes(), 0644)
}

// Validate checks that each installation has a legal kind and the matching
// non-empty targeting field.
func (i *Installation) Validate() error {
	if strings.TrimSpace(i.Asset) == "" {
		return errors.New("installation missing asset name")
	}
	switch i.Kind {
	case InstallKindTeam:
		if strings.TrimSpace(i.Team) == "" {
			return errors.New("team installation missing team name")
		}
	case InstallKindUser:
		if strings.TrimSpace(i.User) == "" {
			return errors.New("user installation missing user email")
		}
	default:
		return fmt.Errorf("%w: %q", ErrInvalidInstallationKind, string(i.Kind))
	}
	return nil
}

// ForAsset returns all installations targeting the given asset name.
func (ifile *InstallationsFile) ForAsset(assetName string) []Installation {
	var out []Installation
	for _, ins := range ifile.Installations {
		if ins.Asset == assetName {
			out = append(out, ins)
		}
	}
	return out
}

// Upsert adds an installation row. If a row with the same (asset, kind,
// team, user) tuple already exists, it is left untouched. Returns true if
// the row was added (false if it was a duplicate).
func (ifile *InstallationsFile) Upsert(ins Installation) bool {
	norm := normalizeInstallation(ins)
	for _, existing := range ifile.Installations {
		if installationsEqual(existing, norm) {
			return false
		}
	}
	ifile.Installations = append(ifile.Installations, norm)
	return true
}

// Remove deletes installation rows matching the given filter. Empty string
// fields match any value. Returns the number of rows removed.
func (ifile *InstallationsFile) Remove(filter Installation) int {
	needle := normalizeInstallation(filter)
	kept := ifile.Installations[:0]
	removed := 0
	for _, ins := range ifile.Installations {
		if installationMatches(ins, needle) {
			removed++
			continue
		}
		kept = append(kept, ins)
	}
	ifile.Installations = kept
	return removed
}

// RemoveForAsset deletes every installation row for the given asset.
func (ifile *InstallationsFile) RemoveForAsset(assetName string) int {
	return ifile.Remove(Installation{Asset: assetName})
}

func normalizeInstallations(in []Installation) []Installation {
	out := make([]Installation, 0, len(in))
	seen := make(map[Installation]struct{}, len(in))
	for _, ins := range in {
		n := normalizeInstallation(ins)
		if err := n.Validate(); err != nil {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

func normalizeInstallation(ins Installation) Installation {
	out := Installation{
		Asset: strings.TrimSpace(ins.Asset),
		Kind:  ins.Kind,
		Team:  strings.TrimSpace(ins.Team),
		User:  NormalizeEmail(ins.User),
	}
	return out
}

func installationsEqual(a, b Installation) bool {
	return a.Asset == b.Asset && a.Kind == b.Kind && a.Team == b.Team && a.User == b.User
}

// installationMatches returns true when every non-empty field of needle
// matches the corresponding field in ins. Used by Remove.
func installationMatches(ins, needle Installation) bool {
	if needle.Asset != "" && ins.Asset != needle.Asset {
		return false
	}
	if needle.Kind != "" && ins.Kind != needle.Kind {
		return false
	}
	if needle.Team != "" && ins.Team != needle.Team {
		return false
	}
	if needle.User != "" && ins.User != needle.User {
		return false
	}
	return true
}
