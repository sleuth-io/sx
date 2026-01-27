package lockfile

import (
	"fmt"
	"slices"
	"time"

	"github.com/sleuth-io/sx/internal/asset"
)

// LockFile represents the complete lock file structure
type LockFile struct {
	LockVersion string         `toml:"lock-version"`
	Version     string         `toml:"version"`
	CreatedBy   string         `toml:"created-by"`
	Install     *InstallConfig `toml:"install,omitempty"`
	Assets      []Asset        `toml:"assets"`
}

// InstallConfig contains per-asset-type installation settings
type InstallConfig struct {
	Instruction *InstructionInstallConfig `toml:"instruction,omitempty"`
}

// InstructionInstallConfig contains settings for instruction injection
type InstructionInstallConfig struct {
	Heading   string `toml:"heading,omitempty"`    // Default: "## Shared Instructions"
	EndMarker string `toml:"end-marker,omitempty"` // Default: "---"
}

// DefaultInstructionHeading is the default section heading for injected instructions
const DefaultInstructionHeading = "## Shared Instructions"

// DefaultInstructionEndMarker is the default end marker for the managed section
const DefaultInstructionEndMarker = "---"

// GetInstructionConfig returns the instruction install config with defaults applied
func (lf *LockFile) GetInstructionConfig() *InstructionInstallConfig {
	config := &InstructionInstallConfig{
		Heading:   DefaultInstructionHeading,
		EndMarker: DefaultInstructionEndMarker,
	}

	if lf.Install != nil && lf.Install.Instruction != nil {
		if lf.Install.Instruction.Heading != "" {
			config.Heading = lf.Install.Instruction.Heading
		}
		if lf.Install.Instruction.EndMarker != "" {
			config.EndMarker = lf.Install.Instruction.EndMarker
		}
	}

	return config
}

// Asset represents an asset with its metadata, source, and installation configurations
// (formerly Artifact)
type Asset struct {
	Name         string       `toml:"name"`
	Version      string       `toml:"version"`
	Type         asset.Type   `toml:"type"`
	Clients      []string     `toml:"clients,omitempty"`
	Dependencies []Dependency `toml:"dependencies,omitempty"`

	// Source (one of these will be present)
	SourceHTTP *SourceHTTP `toml:"source-http,omitempty"`
	SourcePath *SourcePath `toml:"source-path,omitempty"`
	SourceGit  *SourceGit  `toml:"source-git,omitempty"`

	// Installation configurations - array of scope installations
	// If empty, asset is installed globally
	Scopes []Scope `toml:"scopes,omitempty"`
}

// Scope represents where an asset is installed within a repository
// (formerly Repository)
type Scope struct {
	Repo  string   `toml:"repo"`            // Repository URL
	Paths []string `toml:"paths,omitempty"` // Specific paths within repo (if empty, entire repo)
}

// ScopeType represents the scope of an installation
type ScopeType string

const (
	ScopeGlobal ScopeType = "global"
	ScopeRepo   ScopeType = "repo"
	ScopePath   ScopeType = "path"
)

// GetScopeType returns the scope type for this scope entry
// - If paths is empty/nil, it's repo-scoped (entire repository)
// - If paths has entries, it's path-scoped (specific paths within repository)
func (s *Scope) GetScopeType() ScopeType {
	if len(s.Paths) > 0 {
		return ScopePath
	}
	return ScopeRepo
}

// IsGlobal returns true if asset is installed globally (no scope restrictions)
func (a *Asset) IsGlobal() bool {
	return len(a.Scopes) == 0
}

// MatchesClient returns true if the asset is compatible with the given client
func (a *Asset) MatchesClient(clientName string) bool {
	// If no clients specified, matches all clients
	if len(a.Clients) == 0 {
		return true
	}

	// Check if client is in the list
	return slices.Contains(a.Clients, clientName)
}

// SourceHTTP represents an HTTP source for an asset
type SourceHTTP struct {
	URL        string            `toml:"url"`
	Hashes     map[string]string `toml:"hashes"`
	Size       int64             `toml:"size,omitempty"`
	UploadedAt *time.Time        `toml:"uploaded-at,omitempty"`
}

// SourcePath represents a local path source for an asset
type SourcePath struct {
	Path string `toml:"path"`
}

// SourceGit represents a Git repository source for an asset
type SourceGit struct {
	URL          string `toml:"url"`
	Ref          string `toml:"ref"`
	Subdirectory string `toml:"subdirectory,omitempty"`
}

// Dependency represents a dependency reference
type Dependency struct {
	Name    string `toml:"name"`
	Version string `toml:"version,omitempty"`
}

// GetSourceType returns the type of source for this asset
func (a *Asset) GetSourceType() string {
	if a.SourceHTTP != nil {
		return "http"
	}
	if a.SourcePath != nil {
		return "path"
	}
	if a.SourceGit != nil {
		return "git"
	}
	return "unknown"
}

// GetSourceConfig returns the source configuration as a map for generic handling
func (a *Asset) GetSourceConfig() map[string]any {
	config := make(map[string]any)

	if a.SourceHTTP != nil {
		config["type"] = "http"
		config["url"] = a.SourceHTTP.URL
		config["hashes"] = a.SourceHTTP.Hashes
		if a.SourceHTTP.Size > 0 {
			config["size"] = a.SourceHTTP.Size
		}
		if a.SourceHTTP.UploadedAt != nil {
			config["uploaded-at"] = a.SourceHTTP.UploadedAt
		}
	} else if a.SourcePath != nil {
		config["type"] = "path"
		config["path"] = a.SourcePath.Path
	} else if a.SourceGit != nil {
		config["type"] = "git"
		config["url"] = a.SourceGit.URL
		config["ref"] = a.SourceGit.Ref
		if a.SourceGit.Subdirectory != "" {
			config["subdirectory"] = a.SourceGit.Subdirectory
		}
	}

	return config
}

// String returns a string representation of the asset
func (a *Asset) String() string {
	return fmt.Sprintf("%s@%s (%s)", a.Name, a.Version, a.Type)
}

// Key returns a unique key for the asset (name@version)
func (a *Asset) Key() string {
	return fmt.Sprintf("%s@%s", a.Name, a.Version)
}

// ScopedAsset represents an asset with its scope information
// (formerly ScopedArtifact)
type ScopedAsset struct {
	Asset     *Asset
	ScopeDesc string // "Global", repo URL, or "repo:path"
}

// GroupByScope returns all assets grouped by their scope
// An asset can appear in multiple scopes
func (lf *LockFile) GroupByScope() map[string][]*Asset {
	result := make(map[string][]*Asset)

	for i := range lf.Assets {
		ast := &lf.Assets[i]

		if ast.IsGlobal() {
			result["Global"] = append(result["Global"], ast)
		} else {
			for _, scope := range ast.Scopes {
				if len(scope.Paths) == 0 {
					// Repo-scoped
					result[scope.Repo] = append(result[scope.Repo], ast)
				} else {
					// Path-scoped
					for _, path := range scope.Paths {
						scopeKey := fmt.Sprintf("%s:%s", scope.Repo, path)
						result[scopeKey] = append(result[scopeKey], ast)
					}
				}
			}
		}
	}

	return result
}
