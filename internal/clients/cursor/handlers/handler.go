package handlers

import (
	"context"
	"fmt"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

// Handler defines the interface for asset type handlers
type Handler interface {
	// Install installs the asset from zip data to the target base directory
	Install(ctx context.Context, zipData []byte, targetBase string) error

	// Remove removes the asset from the target base directory
	Remove(ctx context.Context, targetBase string) error

	// VerifyInstalled checks if the asset is properly installed
	// Returns (installed bool, message string)
	VerifyInstalled(targetBase string) (bool, string)
}

// NewHandler creates a handler for the given asset type and metadata
func NewHandler(assetType asset.Type, meta *metadata.Metadata) (Handler, error) {
	switch assetType {
	case asset.TypeSkill:
		return NewSkillHandler(meta), nil
	case asset.TypeCommand:
		return NewCommandHandler(meta), nil
	case asset.TypeHook:
		return NewHookHandler(meta), nil
	case asset.TypeMCP:
		return NewMCPHandler(meta), nil
	case asset.TypeMCPRemote:
		return NewMCPRemoteHandler(meta), nil
	case asset.TypeInstruction:
		return NewInstructionHandler(meta, ""), nil
	default:
		return nil, fmt.Errorf("unsupported asset type: %s", assetType.Key)
	}
}
