package commands

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/skills/internal/config"
	"github.com/sleuth-io/skills/internal/lockfile"
	"github.com/sleuth-io/skills/internal/metadata"
	"github.com/sleuth-io/skills/internal/repository"
	"github.com/sleuth-io/skills/internal/utils"
)

// NewAddCommand creates the add command
func NewAddCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add [zip-file]",
		Short: "Add a local zip file artifact to the repository",
		Long: `Take a local zip file, detect metadata from its contents, prompt for
confirmation/edits, install it to the repository, and update the lock file.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var zipFile string
			if len(args) > 0 {
				zipFile = args[0]
			}
			return runAdd(cmd, zipFile)
		},
	}

	return cmd
}

// runAdd executes the add command
func runAdd(cmd *cobra.Command, zipFile string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	reader := bufio.NewReader(os.Stdin)

	// Prompt for zip file if not provided
	if zipFile == "" {
		fmt.Fprint(os.Stderr, "Enter path to artifact zip file: ")
		input, _ := reader.ReadString('\n')
		zipFile = strings.TrimSpace(input)
	}

	if zipFile == "" {
		return fmt.Errorf("zip file path is required")
	}

	// Expand path
	zipFile, err := utils.NormalizePath(zipFile)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	// Check if file exists
	if !utils.FileExists(zipFile) {
		return fmt.Errorf("file not found: %s", zipFile)
	}

	// Read zip file
	fmt.Println()
	fmt.Println("Reading artifact...")
	zipData, err := os.ReadFile(zipFile)
	if err != nil {
		return fmt.Errorf("failed to read zip file: %w", err)
	}

	// Verify it's a valid zip
	if !utils.IsZipFile(zipData) {
		return fmt.Errorf("file is not a valid zip archive")
	}

	// Extract metadata
	fmt.Println("Extracting metadata...")
	var meta *metadata.Metadata
	metadataBytes, err := utils.ReadZipFile(zipData, "metadata.toml")
	if err == nil {
		// Metadata exists, parse it
		meta, err = metadata.Parse(metadataBytes)
		if err != nil {
			return fmt.Errorf("failed to parse metadata: %w", err)
		}
	} else {
		// No metadata, need to create it
		fmt.Println("No metadata.toml found in zip. Creating new metadata...")
		meta, err = promptForMetadata(reader, zipData)
		if err != nil {
			return fmt.Errorf("failed to create metadata: %w", err)
		}

		// Add metadata to zip
		metadataBytes, err = metadata.Marshal(meta)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}

		zipData, err = utils.AddFileToZip(zipData, "metadata.toml", metadataBytes)
		if err != nil {
			return fmt.Errorf("failed to add metadata to zip: %w", err)
		}
	}

	// Display metadata and confirm
	fmt.Println()
	fmt.Println("Artifact metadata:")
	fmt.Printf("  Name:    %s\n", meta.Artifact.Name)
	fmt.Printf("  Version: %s\n", meta.Artifact.Version)
	fmt.Printf("  Type:    %s\n", meta.Artifact.Type)
	if meta.Artifact.Description != "" {
		fmt.Printf("  Description: %s\n", meta.Artifact.Description)
	}
	fmt.Println()

	fmt.Fprint(os.Stderr, "Add this artifact to repository? (y/N): ")
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))
	if response != "y" && response != "yes" {
		return fmt.Errorf("cancelled by user")
	}

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w\nRun 'skills init' to configure", err)
	}

	// Create repository instance
	var repo repository.Repository
	switch cfg.Type {
	case config.RepositoryTypeSleuth:
		repo = repository.NewSleuthRepository(cfg.GetServerURL(), cfg.AuthToken)
	case config.RepositoryTypeGit:
		repo, err = repository.NewGitRepository(cfg.RepositoryURL)
		if err != nil {
			return fmt.Errorf("failed to create git repository: %w", err)
		}
	default:
		return fmt.Errorf("unsupported repository type: %s", cfg.Type)
	}

	// Create artifact entry for lock file
	artifact := &lockfile.Artifact{
		Name:    meta.Artifact.Name,
		Version: meta.Artifact.Version,
		Type:    lockfile.ArtifactType(meta.Artifact.Type),
	}

	// Upload artifact
	fmt.Println()
	fmt.Println("Adding artifact to repository...")
	if err := repo.AddArtifact(ctx, artifact, zipData); err != nil {
		return fmt.Errorf("failed to add artifact: %w", err)
	}

	fmt.Println()
	fmt.Printf("✓ Successfully added %s@%s\n", meta.Artifact.Name, meta.Artifact.Version)

	return nil
}

// promptForMetadata prompts the user to enter metadata
func promptForMetadata(reader *bufio.Reader, zipData []byte) (*metadata.Metadata, error) {
	// List files in zip
	files, err := utils.ListZipFiles(zipData)
	if err != nil {
		return nil, fmt.Errorf("failed to list zip files: %w", err)
	}

	// Try to detect artifact type
	artifactType := detectArtifactType(files)

	meta := &metadata.Metadata{
		Artifact: metadata.Artifact{},
	}

	// Prompt for name
	fmt.Fprint(os.Stderr, "Artifact name: ")
	name, _ := reader.ReadString('\n')
	meta.Artifact.Name = strings.TrimSpace(name)

	// Prompt for version
	fmt.Fprint(os.Stderr, "Version (e.g., 1.0.0): ")
	version, _ := reader.ReadString('\n')
	meta.Artifact.Version = strings.TrimSpace(version)

	// Prompt for type
	fmt.Fprintf(os.Stderr, "Type (detected: %s): ", artifactType)
	typeInput, _ := reader.ReadString('\n')
	typeInput = strings.TrimSpace(typeInput)
	if typeInput == "" {
		typeInput = artifactType
	}
	meta.Artifact.Type = typeInput

	// Create type-specific sections
	switch meta.Artifact.Type {
	case "skill":
		meta.Skill = &metadata.SkillConfig{
			PromptFile: "SKILL.md",
		}
	case "agent":
		meta.Agent = &metadata.AgentConfig{
			PromptFile: "AGENT.md",
		}
	case "command":
		meta.Command = &metadata.CommandConfig{
			PromptFile: "COMMAND.md",
		}
	case "hook":
		fmt.Fprint(os.Stderr, "Hook event (e.g., pre-commit): ")
		event, _ := reader.ReadString('\n')
		meta.Hook = &metadata.HookConfig{
			Event:      strings.TrimSpace(event),
			ScriptFile: "hook.sh",
		}
	case "mcp", "mcp-remote":
		fmt.Fprint(os.Stderr, "Command (e.g., node): ")
		command, _ := reader.ReadString('\n')
		fmt.Fprint(os.Stderr, "Args (comma-separated, e.g., dist/index.js): ")
		argsInput, _ := reader.ReadString('\n')
		args := strings.Split(strings.TrimSpace(argsInput), ",")
		for i := range args {
			args[i] = strings.TrimSpace(args[i])
		}
		meta.MCP = &metadata.MCPConfig{
			Command: strings.TrimSpace(command),
			Args:    args,
		}
	}

	// Validate
	if err := meta.Validate(); err != nil {
		return nil, fmt.Errorf("metadata validation failed: %w", err)
	}

	return meta, nil
}

// detectArtifactType tries to detect the artifact type from file list
func detectArtifactType(files []string) string {
	for _, file := range files {
		switch strings.ToUpper(file) {
		case "SKILL.MD":
			return "skill"
		case "AGENT.MD":
			return "agent"
		case "COMMAND.MD":
			return "command"
		case "HOOK.SH", "HOOK.PY", "HOOK.JS":
			return "hook"
		case "PACKAGE.JSON":
			return "mcp"
		}
	}
	return "skill" // Default
}
