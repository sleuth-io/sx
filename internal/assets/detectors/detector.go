package detectors

import (
	"github.com/sleuth-io/skills/internal/metadata"
)

// ArtifactTypeDetector detects artifact types from file structures
type ArtifactTypeDetector interface {
	// DetectType returns true if the file list matches this artifact type
	DetectType(files []string) bool

	// GetType returns the artifact type string
	GetType() string

	// CreateDefaultMetadata creates default metadata for this type
	CreateDefaultMetadata(name, version string) *metadata.Metadata
}

// UsageDetector provides methods to detect artifact usage from tool calls
type UsageDetector interface {
	// DetectUsageFromToolCall checks if this handler's artifact type was used in a tool call
	// Returns (artifact_name, detected)
	DetectUsageFromToolCall(toolName string, toolInput map[string]interface{}) (string, bool)
}

// detectorRegistry holds all registered detectors
var detectorRegistry []func() ArtifactTypeDetector

// RegisterDetector registers a detector factory function
func RegisterDetector(factory func() ArtifactTypeDetector) {
	detectorRegistry = append(detectorRegistry, factory)
}

// DetectArtifactType detects the artifact type from a list of files
func DetectArtifactType(files []string, name, version string) *metadata.Metadata {
	for _, factory := range detectorRegistry {
		detector := factory()
		if detector.DetectType(files) {
			return detector.CreateDefaultMetadata(name, version)
		}
	}

	// Default to skill if nothing detected
	return (&SkillDetector{}).CreateDefaultMetadata(name, version)
}

func init() {
	// Register all detectors
	RegisterDetector(func() ArtifactTypeDetector { return &SkillDetector{} })
	RegisterDetector(func() ArtifactTypeDetector { return &AgentDetector{} })
	RegisterDetector(func() ArtifactTypeDetector { return &CommandDetector{} })
	RegisterDetector(func() ArtifactTypeDetector { return &HookDetector{} })
	RegisterDetector(func() ArtifactTypeDetector { return &MCPDetector{} })
}
