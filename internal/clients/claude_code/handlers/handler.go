package handlers

import (
	"context"
	"fmt"

	"github.com/sleuth-io/skills/internal/asset"
	"github.com/sleuth-io/skills/internal/metadata"
)

// Handler defines the interface for artifact type handlers
type Handler interface {
	// Install installs the artifact from zip data to the target base directory
	Install(ctx context.Context, zipData []byte, targetBase string) error

	// Remove removes the artifact from the target base directory
	Remove(ctx context.Context, targetBase string) error

	// GetInstallPath returns the installation path relative to targetBase
	GetInstallPath() string

	// CanDetectInstalledState returns true if the handler can verify installation state
	CanDetectInstalledState() bool

	// VerifyInstalled checks if the artifact is properly installed
	// Returns (installed bool, message string)
	VerifyInstalled(targetBase string) (bool, string)
}

// NewHandler creates a handler for the given artifact type and metadata
func NewHandler(artifactType asset.Type, meta *metadata.Metadata) (Handler, error) {
	switch artifactType {
	case asset.TypeSkill:
		return NewSkillHandler(meta), nil
	case asset.TypeAgent:
		return NewAgentHandler(meta), nil
	case asset.TypeCommand:
		return NewCommandHandler(meta), nil
	case asset.TypeHook:
		return NewHookHandler(meta), nil
	case asset.TypeMCP:
		return NewMCPHandler(meta), nil
	case asset.TypeMCPRemote:
		return NewMCPRemoteHandler(meta), nil
	default:
		return nil, fmt.Errorf("unsupported artifact type: %s", artifactType.Key)
	}
}
