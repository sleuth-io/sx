package lockfile

import (
	"fmt"
	"time"
)

// LockFile represents the complete lock file structure
type LockFile struct {
	LockVersion string     `toml:"lock-version"`
	Version     string     `toml:"version"`
	CreatedBy   string     `toml:"created-by"`
	Artifacts   []Artifact `toml:"artifacts"`
}

// Artifact represents an artifact with its metadata, source, and installation configurations
type Artifact struct {
	Name         string       `toml:"name"`
	Version      string       `toml:"version"`
	Type         ArtifactType `toml:"type"`
	Clients      []string     `toml:"clients,omitempty"`
	Dependencies []Dependency `toml:"dependencies,omitempty"`

	// Source (one of these will be present)
	SourceHTTP *SourceHTTP `toml:"source-http,omitempty"`
	SourcePath *SourcePath `toml:"source-path,omitempty"`
	SourceGit  *SourceGit  `toml:"source-git,omitempty"`

	// Installation configurations - array of repository installations
	// If empty, artifact is installed globally
	Repositories []Repository `toml:"repositories,omitempty"`
}

// Repository represents where an artifact is installed within a repository
type Repository struct {
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

// GetScope returns the scope type for this repository entry
// - If paths is empty/nil, it's repo-scoped (entire repository)
// - If paths has entries, it's path-scoped (specific paths within repository)
func (r *Repository) GetScope() ScopeType {
	if len(r.Paths) > 0 {
		return ScopePath
	}
	return ScopeRepo
}

// IsGlobal returns true if artifact is installed globally (no repository restrictions)
func (a *Artifact) IsGlobal() bool {
	return len(a.Repositories) == 0
}

// MatchesClient returns true if the artifact is compatible with the given client
func (a *Artifact) MatchesClient(clientName string) bool {
	// If no clients specified, matches all clients
	if len(a.Clients) == 0 {
		return true
	}

	// Check if client is in the list
	for _, client := range a.Clients {
		if client == clientName {
			return true
		}
	}

	return false
}

// ArtifactType represents the type of artifact
type ArtifactType string

const (
	ArtifactTypeMCP       ArtifactType = "mcp"
	ArtifactTypeMCPRemote ArtifactType = "mcp-remote"
	ArtifactTypeSkill     ArtifactType = "skill"
	ArtifactTypeAgent     ArtifactType = "agent"
	ArtifactTypeCommand   ArtifactType = "command"
	ArtifactTypeHook      ArtifactType = "hook"
)

// IsValid checks if the artifact type is valid
func (t ArtifactType) IsValid() bool {
	switch t {
	case ArtifactTypeMCP, ArtifactTypeMCPRemote, ArtifactTypeSkill,
		ArtifactTypeAgent, ArtifactTypeCommand, ArtifactTypeHook:
		return true
	}
	return false
}

// SourceHTTP represents an HTTP source for an artifact
type SourceHTTP struct {
	URL        string            `toml:"url"`
	Hashes     map[string]string `toml:"hashes"`
	Size       int64             `toml:"size,omitempty"`
	UploadedAt *time.Time        `toml:"uploaded-at,omitempty"`
}

// SourcePath represents a local path source for an artifact
type SourcePath struct {
	Path string `toml:"path"`
}

// SourceGit represents a Git repository source for an artifact
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

// GetSourceType returns the type of source for this artifact
func (a *Artifact) GetSourceType() string {
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
func (a *Artifact) GetSourceConfig() map[string]interface{} {
	config := make(map[string]interface{})

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

// String returns a string representation of the artifact
func (a *Artifact) String() string {
	return fmt.Sprintf("%s@%s (%s)", a.Name, a.Version, a.Type)
}

// Key returns a unique key for the artifact (name@version)
func (a *Artifact) Key() string {
	return fmt.Sprintf("%s@%s", a.Name, a.Version)
}
