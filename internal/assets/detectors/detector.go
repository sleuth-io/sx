package detectors

import (
	"github.com/sleuth-io/sx/internal/metadata"
)

// AssetTypeDetector detects asset types from file structures
type AssetTypeDetector interface {
	// DetectType returns true if the file list matches this asset type
	DetectType(files []string) bool

	// GetType returns the asset type string
	GetType() string

	// CreateDefaultMetadata creates default metadata for this type
	CreateDefaultMetadata(name, version string) *metadata.Metadata
}

// UsageDetector provides methods to detect asset usage from tool calls
type UsageDetector interface {
	// DetectUsageFromToolCall checks if this handler's asset type was used in a tool call
	// Returns (asset_name, detected)
	DetectUsageFromToolCall(toolName string, toolInput map[string]any) (string, bool)
}

// detectorRegistry holds all registered detectors
var detectorRegistry []func() AssetTypeDetector

// RegisterDetector registers a detector factory function
func RegisterDetector(factory func() AssetTypeDetector) {
	detectorRegistry = append(detectorRegistry, factory)
}

// DetectAssetType detects the asset type from a list of files
func DetectAssetType(files []string, name, version string) *metadata.Metadata {
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
	RegisterDetector(func() AssetTypeDetector { return &SkillDetector{} })
	RegisterDetector(func() AssetTypeDetector { return &AgentDetector{} })
	RegisterDetector(func() AssetTypeDetector { return &CommandDetector{} })
	RegisterDetector(func() AssetTypeDetector { return &HookDetector{} })
	RegisterDetector(func() AssetTypeDetector { return &MCPDetector{} })
}
