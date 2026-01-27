package detectors

import (
	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

// InstructionDetector detects instruction assets
type InstructionDetector struct{}

// Compile-time interface check
var _ AssetTypeDetector = (*InstructionDetector)(nil)

// DetectType returns true if files indicate this is an instruction asset
func (d *InstructionDetector) DetectType(files []string) bool {
	for _, file := range files {
		if file == "INSTRUCTION.md" || file == "instruction.md" {
			return true
		}
	}
	return false
}

// GetType returns the asset type string
func (d *InstructionDetector) GetType() string {
	return "instruction"
}

// CreateDefaultMetadata creates default metadata for an instruction
func (d *InstructionDetector) CreateDefaultMetadata(name, version string) *metadata.Metadata {
	return &metadata.Metadata{
		MetadataVersion: metadata.CurrentMetadataVersion,
		Asset: metadata.Asset{
			Name:    name,
			Version: version,
			Type:    asset.TypeInstruction,
		},
		// InstructionConfig is optional - defaults will be applied at install time
	}
}
