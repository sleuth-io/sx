package handlers

import (
	"context"
	"fmt"

	"github.com/sleuth-io/sx/internal/asset"
	"github.com/sleuth-io/sx/internal/metadata"
)

// Handler is the interface implemented by all OpenCode asset handlers.
type Handler interface {
	Install(ctx context.Context, zipData []byte, targetBase string) error
	Remove(ctx context.Context, targetBase string) error
	VerifyInstalled(targetBase string) (bool, string)
}

// NewHandler returns the appropriate handler for the given asset type.
func NewHandler(assetType asset.Type, meta *metadata.Metadata) (Handler, error) {
	switch assetType {
	case asset.TypeSkill:
		return NewSkillHandler(meta), nil
	case asset.TypeCommand:
		return NewCommandHandler(meta), nil
	case asset.TypeMCP:
		return NewMCPHandler(meta), nil
	default:
		return nil, fmt.Errorf("unsupported asset type for OpenCode: %s", assetType.Key)
	}
}
