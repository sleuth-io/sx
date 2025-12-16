package dirasset

import (
	"github.com/sleuth-io/skills/internal/asset"
)

// InstalledArtifactInfo represents information about an installed artifact
type InstalledArtifactInfo struct {
	Name        string
	Description string
	Version     string
	Type        asset.Type
	InstallPath string
}
