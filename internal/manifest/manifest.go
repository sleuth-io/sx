// Package manifest defines sx.toml, the source-of-truth file stored at the
// vault root. It holds the set of assets managed by the vault, their install
// scopes (org/repo/path/team/user), and the teams (with members, admins, and
// repositories) those scopes reference.
//
// This package is format-only: parse, marshal, read, write, plus small helper
// methods for idempotent mutation. I/O locking, git commit/push, and
// identity resolution are the caller's responsibility.
package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/scope"
)

// FileName is the path, relative to the vault root, where the manifest lives.
const FileName = "sx.toml"

// CurrentSchemaVersion is the schema version written by this build. A
// future build that bumps the version will migrate v1 files forward on
// first write; this build rejects files whose version exceeds the one it
// understands.
const CurrentSchemaVersion = 1

// Errors returned by the parser and mutators.
var (
	// ErrUnsupportedSchema is returned when the on-disk file has a schema
	// version this build does not know how to read.
	ErrUnsupportedSchema = errors.New("unsupported manifest schema version")

	// ErrInvalidScopeKind is returned for scope rows with an unrecognized
	// kind field.
	ErrInvalidScopeKind = errors.New("invalid scope kind")

	// ErrEmptyTeamName is returned by team mutators when the normalized team
	// name is blank.
	ErrEmptyTeamName = errors.New("team name cannot be empty")

	// ErrTeamNotFound is returned when a team lookup fails.
	ErrTeamNotFound = errors.New("team not found")

	// ErrTeamExists is returned when CreateTeam finds the team already
	// exists.
	ErrTeamExists = errors.New("team already exists")

	// ErrEmptyBotName is returned by bot mutators when the normalized bot
	// name is blank.
	ErrEmptyBotName = errors.New("bot name cannot be empty")

	// ErrBotNotFound is returned when a bot lookup fails.
	ErrBotNotFound = errors.New("bot not found")

	// ErrBotExists is returned when CreateBot finds the bot already exists.
	ErrBotExists = errors.New("bot already exists")
)

// Manifest is the on-disk structure of sx.toml.
type Manifest struct {
	// SchemaVersion gates on-disk compatibility. Bumped for breaking
	// schema changes so newer builds can recognize and migrate older
	// files.
	SchemaVersion int `toml:"schema_version"`

	// CreatedBy records the sx build that last wrote the file. Purely
	// informational — used for diagnostics, not gating.
	CreatedBy string `toml:"created_by,omitempty"`

	// Assets are the assets managed by this vault, including their
	// install scopes.
	Assets []Asset `toml:"assets,omitempty"`

	// Teams are group definitions (members, admins, repositories)
	// referenced by team-scoped installs.
	Teams []Team `toml:"teams,omitempty"`

	// Bots are non-human service identities. A bot can be a member of one
	// or more teams (gaining repo context the same way a human team
	// member does) and can also be a direct install target via
	// ScopeKindBot.
	Bots []Bot `toml:"bots,omitempty"`
}

// Asset is one managed asset: its identity, source, and install scopes.
type Asset struct {
	Name         string       `toml:"name"`
	Version      string       `toml:"version"`
	Type         asset.Type   `toml:"type"`
	Clients      []string     `toml:"clients,omitempty"`
	Dependencies []Dependency `toml:"dependencies,omitempty"`

	SourceHTTP *SourceHTTP `toml:"source-http,omitempty"`
	SourcePath *SourcePath `toml:"source-path,omitempty"`
	SourceGit  *SourceGit  `toml:"source-git,omitempty"`

	// Scopes enumerates every install target for this asset. An empty
	// slice means org-wide / global — the asset is available to every
	// caller regardless of identity or repo. See ScopeKind for the set of
	// permitted kinds.
	Scopes []Scope `toml:"scopes,omitempty"`
}

// Dependency is a reference to another asset.
type Dependency struct {
	Name    string `toml:"name"`
	Version string `toml:"version,omitempty"`
}

// SourceHTTP describes an HTTP-hosted asset archive.
type SourceHTTP struct {
	URL    string            `toml:"url"`
	Hashes map[string]string `toml:"hashes"`
	Size   int64             `toml:"size,omitempty"`
}

// SourcePath describes a local path source.
type SourcePath struct {
	Path string `toml:"path"`
}

// SourceGit describes a git repository source.
type SourceGit struct {
	URL          string `toml:"url"`
	Ref          string `toml:"ref"`
	Subdirectory string `toml:"subdirectory,omitempty"`
}

// ScopeKind identifies the type of an install scope. The manifest
// represents all five kinds uniformly.
type ScopeKind string

const (
	// ScopeKindOrg means the asset is available org-wide. Equivalent to
	// an asset with no scopes at all; writing it explicitly produces a
	// row in the file instead of an empty slice.
	ScopeKindOrg ScopeKind = "org"

	// ScopeKindRepo means the asset is available for a single repository.
	// The Repo field must be set.
	ScopeKindRepo ScopeKind = "repo"

	// ScopeKindPath means the asset is available for specific paths within
	// a repository. Both Repo and Paths must be set.
	ScopeKindPath ScopeKind = "path"

	// ScopeKindTeam means the asset is available to every member of the
	// named team. The team is defined in Manifest.Teams; the vault layer
	// resolves it against the caller's identity when producing a lock
	// file.
	ScopeKindTeam ScopeKind = "team"

	// ScopeKindUser means the asset is available to a single user,
	// identified by email.
	ScopeKindUser ScopeKind = "user"

	// ScopeKindBot means the asset is available to a single bot identity,
	// identified by name. The bot is defined in Manifest.Bots; the vault
	// layer resolves it against the caller's identity (typically SX_BOT)
	// when producing a lock file.
	ScopeKindBot ScopeKind = "bot"
)

// Scope is one install target. Which fields are significant depends on Kind;
// unused fields are omitted from the TOML output.
type Scope struct {
	Kind  ScopeKind `toml:"kind"`
	Repo  string    `toml:"repo,omitempty"`
	Paths []string  `toml:"paths,omitempty"`
	Team  string    `toml:"team,omitempty"`
	User  string    `toml:"user,omitempty"`
	Bot   string    `toml:"bot,omitempty"`
}

// Validate returns nil if this scope row has the fields required by its Kind.
func (s *Scope) Validate() error {
	switch s.Kind {
	case ScopeKindOrg:
		return nil
	case ScopeKindRepo:
		if strings.TrimSpace(s.Repo) == "" {
			return errors.New("repo scope requires repo field")
		}
	case ScopeKindPath:
		if strings.TrimSpace(s.Repo) == "" {
			return errors.New("path scope requires repo field")
		}
		if len(s.Paths) == 0 {
			return errors.New("path scope requires non-empty paths")
		}
	case ScopeKindTeam:
		if strings.TrimSpace(s.Team) == "" {
			return errors.New("team scope requires team field")
		}
	case ScopeKindUser:
		if strings.TrimSpace(s.User) == "" {
			return errors.New("user scope requires user field")
		}
	case ScopeKindBot:
		if strings.TrimSpace(s.Bot) == "" {
			return errors.New("bot scope requires bot field")
		}
	default:
		return fmt.Errorf("%w: %q", ErrInvalidScopeKind, string(s.Kind))
	}
	return nil
}

// Team is a named group with a member list, admin list, and repositories.
// Description is optional. Members and Admins are email lists; Admins is
// expected to be a subset of Members (enforced by callers, not the parser).
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

// FindTeam returns the team with the given name, or ErrTeamNotFound.
func (m *Manifest) FindTeam(name string) (*Team, error) {
	for i := range m.Teams {
		if m.Teams[i].Name == name {
			return &m.Teams[i], nil
		}
	}
	return nil, ErrTeamNotFound
}

// UpsertTeam inserts or replaces a team keyed by name. Returns the pointer
// into the manifest's own slice, or ErrEmptyTeamName if the normalized name
// is blank.
func (m *Manifest) UpsertTeam(t Team) (*Team, error) {
	normalizeTeamInPlace(&t)
	if t.Name == "" {
		return nil, ErrEmptyTeamName
	}
	for i := range m.Teams {
		if m.Teams[i].Name == t.Name {
			m.Teams[i] = t
			return &m.Teams[i], nil
		}
	}
	m.Teams = append(m.Teams, t)
	return &m.Teams[len(m.Teams)-1], nil
}

// DeleteTeam removes the team by name, returning ErrTeamNotFound if missing.
func (m *Manifest) DeleteTeam(name string) error {
	for i := range m.Teams {
		if m.Teams[i].Name == name {
			m.Teams = append(m.Teams[:i], m.Teams[i+1:]...)
			return nil
		}
	}
	return ErrTeamNotFound
}

// TeamsForMember returns all teams containing the given email as a member.
func (m *Manifest) TeamsForMember(email string) []*Team {
	needle := NormalizeEmail(email)
	if needle == "" {
		return nil
	}
	var out []*Team
	for i := range m.Teams {
		if slices.Contains(m.Teams[i].Members, needle) {
			out = append(out, &m.Teams[i])
		}
	}
	return out
}

// Bot is a non-human service identity. Bots gain repository context by
// being members of one or more teams; assets can also be installed
// directly to a bot via ScopeKindBot. File-based vaults treat bots as
// identity-only — the trust boundary is "vault read access ⇒ asset
// access", so anyone with vault access can claim any bot identity.
type Bot struct {
	Name        string   `toml:"name"`
	Description string   `toml:"description,omitempty"`
	Teams       []string `toml:"teams,omitempty"`
}

// IsOnTeam returns true if the bot lists the given team in its Teams
// slice.
func (b *Bot) IsOnTeam(name string) bool {
	return slices.Contains(b.Teams, strings.TrimSpace(name))
}

// FindBot returns the bot with the given name, or ErrBotNotFound.
func (m *Manifest) FindBot(name string) (*Bot, error) {
	for i := range m.Bots {
		if m.Bots[i].Name == name {
			return &m.Bots[i], nil
		}
	}
	return nil, ErrBotNotFound
}

// UpsertBot inserts or replaces a bot keyed by name. Returns the pointer
// into the manifest's own slice, or ErrEmptyBotName if the normalized
// name is blank.
func (m *Manifest) UpsertBot(b Bot) (*Bot, error) {
	normalizeBotInPlace(&b)
	if b.Name == "" {
		return nil, ErrEmptyBotName
	}
	for i := range m.Bots {
		if m.Bots[i].Name == b.Name {
			m.Bots[i] = b
			return &m.Bots[i], nil
		}
	}
	m.Bots = append(m.Bots, b)
	return &m.Bots[len(m.Bots)-1], nil
}

// DeleteBot removes the bot by name, returning ErrBotNotFound if missing.
func (m *Manifest) DeleteBot(name string) error {
	for i := range m.Bots {
		if m.Bots[i].Name == name {
			m.Bots = append(m.Bots[:i], m.Bots[i+1:]...)
			return nil
		}
	}
	return ErrBotNotFound
}

// FindAsset returns the first asset with the given name, or nil.
func (m *Manifest) FindAsset(name string) *Asset {
	for i := range m.Assets {
		if m.Assets[i].Name == name {
			return &m.Assets[i]
		}
	}
	return nil
}

// UpsertAsset replaces an asset by (name, version) or appends if missing.
// Returns the pointer into the manifest's slice.
func (m *Manifest) UpsertAsset(a Asset) *Asset {
	for i := range m.Assets {
		if m.Assets[i].Name == a.Name && m.Assets[i].Version == a.Version {
			m.Assets[i] = a
			return &m.Assets[i]
		}
	}
	m.Assets = append(m.Assets, a)
	return &m.Assets[len(m.Assets)-1]
}

// RemoveAsset removes every entry matching name. If version is non-empty,
// only matching versions are removed. Returns the number of rows removed.
func (m *Manifest) RemoveAsset(name, version string) int {
	kept := m.Assets[:0]
	removed := 0
	for _, a := range m.Assets {
		if a.Name == name && (version == "" || a.Version == version) {
			removed++
			continue
		}
		kept = append(kept, a)
	}
	m.Assets = kept
	return removed
}

// NormalizeEmail lowercases and trims an email for comparison and storage.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// normalizeTeamInPlace trims and deduplicates a team's string slices. The
// name and description are trimmed; members, admins, and repositories are
// normalized (lowercase emails, url-normalized repos) and sorted for
// deterministic serialization.
func normalizeTeamInPlace(t *Team) {
	t.Name = strings.TrimSpace(t.Name)
	t.Description = strings.TrimSpace(t.Description)
	t.Members = dedupeSorted(normalizeEmails(t.Members))
	t.Admins = dedupeSorted(normalizeEmails(t.Admins))
	t.Repositories = dedupeSorted(normalizeRepos(t.Repositories))
}

// normalizeBotInPlace trims a bot's name and description and normalizes
// its team list (trimmed, deduped, sorted) so the on-disk encoding is
// deterministic regardless of insertion order.
func normalizeBotInPlace(b *Bot) {
	b.Name = strings.TrimSpace(b.Name)
	b.Description = strings.TrimSpace(b.Description)
	teams := make([]string, 0, len(b.Teams))
	for _, t := range b.Teams {
		if t = strings.TrimSpace(t); t != "" {
			teams = append(teams, t)
		}
	}
	b.Teams = dedupeSorted(teams)
}

func normalizeScopeInPlace(s *Scope) {
	s.Kind = ScopeKind(strings.ToLower(strings.TrimSpace(string(s.Kind))))
	s.Repo = strings.TrimSpace(s.Repo)
	s.Team = strings.TrimSpace(s.Team)
	s.User = NormalizeEmail(s.User)
	s.Bot = strings.TrimSpace(s.Bot)

	if len(s.Paths) > 0 {
		cleaned := make([]string, 0, len(s.Paths))
		for _, p := range s.Paths {
			p = strings.TrimSpace(p)
			if p != "" {
				cleaned = append(cleaned, p)
			}
		}
		s.Paths = dedupeSorted(cleaned)
	}
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

// writeFileAtomic writes data to path via a tmp file in the same directory
// plus rename. On POSIX filesystems the rename is atomic — readers never see
// a partial write.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}
	return nil
}

// Parse parses a manifest from TOML bytes.
func Parse(data []byte) (*Manifest, error) {
	var m Manifest
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}
	if m.SchemaVersion == 0 {
		// Treat an unspecified version as v1 for forgiving reads. Newer
		// files without a version will still produce warnings in
		// validation, but parse succeeds.
		m.SchemaVersion = CurrentSchemaVersion
	}
	if m.SchemaVersion > CurrentSchemaVersion {
		return nil, fmt.Errorf("%w: file uses schema %d, this build understands up to %d", ErrUnsupportedSchema, m.SchemaVersion, CurrentSchemaVersion)
	}
	return &m, nil
}

// ReadFile reads and parses the manifest file at the given absolute path.
func ReadFile(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest %s: %w", path, err)
	}
	return Parse(data)
}

// Load reads the manifest at vaultRoot/sx.toml. Returns (nil, false, nil)
// when the file does not exist; callers can use the bool to distinguish
// "never initialized" from "parse error".
func Load(vaultRoot string) (*Manifest, bool, error) {
	path := filepath.Join(vaultRoot, FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("failed to read manifest: %w", err)
	}
	m, err := Parse(data)
	if err != nil {
		return nil, false, err
	}
	return m, true, nil
}

// Marshal encodes the manifest to TOML bytes with the current schema version
// written at the top. Fields are normalized (trimmed, deduped, sorted)
// before encoding so the output is deterministic.
func Marshal(m *Manifest) ([]byte, error) {
	out := normalized(m)

	buf := new(bytes.Buffer)
	enc := toml.NewEncoder(buf)
	if err := enc.Encode(out); err != nil {
		return nil, fmt.Errorf("failed to encode manifest: %w", err)
	}
	return buf.Bytes(), nil
}

// Write writes the manifest to the given absolute path atomically.
func Write(m *Manifest, path string) error {
	data, err := Marshal(m)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0644)
}

// Save writes the manifest to vaultRoot/sx.toml atomically. Creates the
// vault root directory if needed.
func Save(vaultRoot string, m *Manifest) error {
	path := filepath.Join(vaultRoot, FileName)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create vault root: %w", err)
	}
	return Write(m, path)
}

// normalized returns a copy of m with every field normalized for stable
// output. Pure — does not mutate the input.
func normalized(m *Manifest) *Manifest {
	out := *m
	if out.SchemaVersion == 0 {
		out.SchemaVersion = CurrentSchemaVersion
	}

	if len(m.Assets) > 0 {
		out.Assets = make([]Asset, len(m.Assets))
		copy(out.Assets, m.Assets)
		for i := range out.Assets {
			normalizeAssetInPlace(&out.Assets[i])
		}
	}

	if len(m.Teams) > 0 {
		out.Teams = make([]Team, len(m.Teams))
		copy(out.Teams, m.Teams)
		for i := range out.Teams {
			normalizeTeamInPlace(&out.Teams[i])
		}
	}

	if len(m.Bots) > 0 {
		out.Bots = make([]Bot, len(m.Bots))
		copy(out.Bots, m.Bots)
		for i := range out.Bots {
			normalizeBotInPlace(&out.Bots[i])
		}
	}

	return &out
}

func normalizeAssetInPlace(a *Asset) {
	a.Name = strings.TrimSpace(a.Name)
	a.Version = strings.TrimSpace(a.Version)
	if len(a.Scopes) > 0 {
		scopes := make([]Scope, 0, len(a.Scopes))
		type scopeKey struct {
			kind                 ScopeKind
			repo, team, usr, bot string
			paths                string
		}
		seen := make(map[scopeKey]struct{}, len(a.Scopes))
		for _, s := range a.Scopes {
			normalizeScopeInPlace(&s)
			if err := s.Validate(); err != nil {
				continue
			}
			key := scopeKey{
				kind:  s.Kind,
				repo:  s.Repo,
				team:  s.Team,
				usr:   s.User,
				bot:   s.Bot,
				paths: strings.Join(s.Paths, "\x00"),
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			scopes = append(scopes, s)
		}
		a.Scopes = scopes
	}
}
