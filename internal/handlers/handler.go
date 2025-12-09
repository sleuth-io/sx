package handlers

import (
	"context"
	"fmt"

	"github.com/sleuth-io/skills/internal/metadata"
)

// ArtifactHandler handles installation and removal of artifacts
type ArtifactHandler interface {
	// Install extracts and installs the artifact
	// targetBase is the base directory (e.g., ~/.claude/ or {repo}/.claude/)
	Install(ctx context.Context, zipData []byte, targetBase string) error

	// Remove uninstalls the artifact
	Remove(ctx context.Context, targetBase string) error

	// GetInstallPath returns the installation path relative to targetBase
	GetInstallPath() string

	// Validate checks if the zip structure is valid for this artifact type
	Validate(zipData []byte) error
}

// ArtifactTypeDetector extends ArtifactHandler with type detection capability
type ArtifactTypeDetector interface {
	// DetectType returns true if the file list matches this artifact type
	DetectType(files []string) bool

	// GetType returns the artifact type string
	GetType() string

	// CreateDefaultMetadata creates default metadata for this type
	CreateDefaultMetadata(name, version string) *metadata.Metadata
}

// MetadataHelper provides metadata-related helper methods
type MetadataHelper interface {
	// GetPromptFile returns the prompt file path, or empty string if not applicable
	GetPromptFile(meta *metadata.Metadata) string

	// GetScriptFile returns the script file path, or empty string if not applicable
	GetScriptFile(meta *metadata.Metadata) string

	// ValidateMetadata validates the metadata for this artifact type
	ValidateMetadata(meta *metadata.Metadata) error
}

// InstalledStateDetector provides methods to detect installed artifacts from the filesystem
type InstalledStateDetector interface {
	// CanDetectInstalledState returns true if this handler can read
	// version info from filesystem (metadata.toml is preserved)
	CanDetectInstalledState() bool

	// ScanInstalled scans targetBase for installed artifacts of this type
	// Returns slice of found artifacts with name, version, type, path
	ScanInstalled(targetBase string) ([]InstalledArtifactInfo, error)
}

// UsageDetector provides methods to detect artifact usage from tool calls
type UsageDetector interface {
	// DetectUsageFromToolCall checks if this handler's artifact type was used in a tool call
	// Returns (artifact_name, detected)
	DetectUsageFromToolCall(toolName string, toolInput map[string]interface{}) (string, bool)
}

// InstalledArtifactInfo represents information about an installed artifact
type InstalledArtifactInfo struct {
	Name        string
	Version     string
	Type        string
	InstallPath string
}

// NewHandler creates an appropriate handler for the given artifact type
func NewHandler(meta *metadata.Metadata) (ArtifactHandler, error) {
	switch meta.Artifact.Type {
	case "skill":
		return NewSkillHandler(meta), nil
	case "agent":
		return NewAgentHandler(meta), nil
	case "command":
		return NewCommandHandler(meta), nil
	case "hook":
		return NewHookHandler(meta), nil
	case "mcp":
		return NewMCPHandler(meta), nil
	case "mcp-remote":
		return NewMCPRemoteHandler(meta), nil
	default:
		return nil, fmt.Errorf("unsupported artifact type: %s", meta.Artifact.Type)
	}
}

// handlerRegistry holds all registered handlers
var handlerRegistry []func() ArtifactTypeDetector

// RegisterHandler registers a handler factory function
func RegisterHandler(factory func() ArtifactTypeDetector) {
	handlerRegistry = append(handlerRegistry, factory)
}

// DetectArtifactType detects the artifact type from a list of files
func DetectArtifactType(files []string, name, version string) *metadata.Metadata {
	for _, factory := range handlerRegistry {
		detector := factory()
		if detector.DetectType(files) {
			return detector.CreateDefaultMetadata(name, version)
		}
	}

	// Default to skill if nothing detected
	return (&SkillHandler{}).CreateDefaultMetadata(name, version)
}

// GetPromptFile returns the prompt file path for the given metadata
func GetPromptFile(meta *metadata.Metadata) string {
	handler, err := NewHandler(meta)
	if err != nil {
		return ""
	}

	if helper, ok := handler.(MetadataHelper); ok {
		return helper.GetPromptFile(meta)
	}
	return ""
}

// GetScriptFile returns the script file path for the given metadata
func GetScriptFile(meta *metadata.Metadata) string {
	handler, err := NewHandler(meta)
	if err != nil {
		return ""
	}

	if helper, ok := handler.(MetadataHelper); ok {
		return helper.GetScriptFile(meta)
	}
	return ""
}

// ValidateMetadata validates the metadata using the appropriate handler
func ValidateMetadata(meta *metadata.Metadata) error {
	handler, err := NewHandler(meta)
	if err != nil {
		return err
	}

	if helper, ok := handler.(MetadataHelper); ok {
		return helper.ValidateMetadata(meta)
	}
	return fmt.Errorf("handler does not support metadata validation")
}

// GetRequiredFiles returns a list of files that must exist in the artifact
func GetRequiredFiles(meta *metadata.Metadata) []string {
	var files []string

	// Add type-specific files
	if promptFile := GetPromptFile(meta); promptFile != "" {
		files = append(files, promptFile)
	}
	if scriptFile := GetScriptFile(meta); scriptFile != "" {
		files = append(files, scriptFile)
	}

	// Add readme if specified
	if meta.Artifact.Readme != "" {
		files = append(files, meta.Artifact.Readme)
	}

	return files
}

func init() {
	// Register all handlers
	RegisterHandler(func() ArtifactTypeDetector { return &SkillHandler{} })
	RegisterHandler(func() ArtifactTypeDetector { return &AgentHandler{} })
	RegisterHandler(func() ArtifactTypeDetector { return &CommandHandler{} })
	RegisterHandler(func() ArtifactTypeDetector { return &HookHandler{} })
	RegisterHandler(func() ArtifactTypeDetector { return &MCPHandler{} })
}
