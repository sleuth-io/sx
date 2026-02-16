package singlefile

import (
	"context"
	"os"
	"path/filepath"

	"github.com/sleuth-io/sx/internal/metadata"
	"github.com/sleuth-io/sx/internal/utils"
)

// Config configures a single-file asset handler
type Config struct {
	Dir       string                                               // Target directory (e.g., "commands", "prompts", "agents")
	Extension string                                               // File extension (e.g., ".md", ".prompt.md")
	SrcFiles  []string                                             // Source files to try in order (e.g., ["COMMAND.md", "command.md"])
	Transform func(meta *metadata.Metadata, content []byte) []byte // Optional content transform
}

// Handler is a generic handler for single-file assets
type Handler struct {
	metadata *metadata.Metadata
	config   Config
}

// New creates a new single-file handler
func New(meta *metadata.Metadata, config Config) *Handler {
	return &Handler{
		metadata: meta,
		config:   config,
	}
}

// Install extracts and writes the asset file
func (h *Handler) Install(ctx context.Context, zipData []byte, targetBase string) error {
	destPath := filepath.Join(targetBase, h.config.Dir, h.metadata.Asset.Name+h.config.Extension)

	var transform func([]byte) []byte
	if h.config.Transform != nil {
		transform = func(content []byte) []byte {
			return h.config.Transform(h.metadata, content)
		}
	}

	return utils.ExtractFileFromZipWithFallback(zipData, h.config.SrcFiles, destPath, transform)
}

// Remove removes the asset file
func (h *Handler) Remove(ctx context.Context, targetBase string) error {
	return utils.RemoveFileIfExists(filepath.Join(targetBase, h.config.Dir, h.metadata.Asset.Name+h.config.Extension))
}

// VerifyInstalled checks if the asset file exists
func (h *Handler) VerifyInstalled(targetBase string) (bool, string) {
	filePath := filepath.Join(targetBase, h.config.Dir, h.metadata.Asset.Name+h.config.Extension)
	if _, err := os.Stat(filePath); err == nil {
		return true, "Found at " + filePath
	}
	return false, "not found"
}

// GetInstallPath returns the relative installation path
func (h *Handler) GetInstallPath() string {
	return filepath.Join(h.config.Dir, h.metadata.Asset.Name+h.config.Extension)
}
