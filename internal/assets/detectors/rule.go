package detectors

import (
	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

// RuleDetector detects rule assets
type RuleDetector struct{}

// Compile-time interface check
var _ AssetTypeDetector = (*RuleDetector)(nil)

// DetectType returns true if files indicate this is a rule asset
func (d *RuleDetector) DetectType(files []string) bool {
	for _, file := range files {
		if file == "RULE.md" || file == "rule.md" {
			return true
		}
	}
	return false
}

// GetType returns the asset type string
func (d *RuleDetector) GetType() string {
	return "rule"
}

// CreateDefaultMetadata creates default metadata for a rule
func (d *RuleDetector) CreateDefaultMetadata(name, version string) *metadata.Metadata {
	return &metadata.Metadata{
		MetadataVersion: metadata.CurrentMetadataVersion,
		Asset: metadata.Asset{
			Name:    name,
			Version: version,
			Type:    asset.TypeRule,
		},
		// RuleConfig is optional - defaults will be applied at install time
	}
}
