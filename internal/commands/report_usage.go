package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/skills/internal/artifacts"
	"github.com/sleuth-io/skills/internal/config"
	"github.com/sleuth-io/skills/internal/gitutil"
	"github.com/sleuth-io/skills/internal/handlers"
	"github.com/sleuth-io/skills/internal/logger"
	"github.com/sleuth-io/skills/internal/repository"
	"github.com/sleuth-io/skills/internal/scope"
	"github.com/sleuth-io/skills/internal/stats"
	"github.com/sleuth-io/skills/internal/utils"
)

// NewReportUsageCommand creates the report-usage command
func NewReportUsageCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report-usage",
		Short: "Report artifact usage from tool calls (PostToolUse hook)",
		Long: `Parse PostToolUse hook JSON from stdin, detect artifact usage,
and report it to the repository. Intended to be called from Claude Code hooks.`,
		Hidden: true, // Hide from help output as it's for internal use
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReportUsage(cmd, args)
		},
	}

	return cmd
}

// PostToolUseEvent represents the JSON payload from Claude Code PostToolUse hook
type PostToolUseEvent struct {
	ToolName  string                 `json:"tool_name"`
	ToolInput map[string]interface{} `json:"tool_input"`
}

// runReportUsage executes the report-usage command
func runReportUsage(cmd *cobra.Command, args []string) error {
	// Read JSON from stdin
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("failed to read stdin: %w", err)
	}

	// Parse hook event
	var event PostToolUseEvent
	if err := json.Unmarshal(data, &event); err != nil {
		// Silently exit on parse error (not a valid hook event)
		return nil
	}

	// Create all handlers for detection
	allHandlers := []handlers.UsageDetector{
		&handlers.SkillHandler{},
		&handlers.AgentHandler{},
		&handlers.CommandHandler{},
		&handlers.MCPHandler{},
		&handlers.MCPRemoteHandler{},
		&handlers.HookHandler{},
	}

	// Try to detect artifact usage from each handler
	var artifactName string
	var artifactType string
	var detected bool

	for _, handler := range allHandlers {
		artifactName, detected = handler.DetectUsageFromToolCall(event.ToolName, event.ToolInput)
		if detected {
			// Get artifact type from handler
			if typedHandler, ok := handler.(handlers.ArtifactTypeDetector); ok {
				artifactType = typedHandler.GetType()
			}
			break
		}
	}

	// If no handler detected usage, exit silently
	if !detected || artifactName == "" {
		return nil
	}

	// Detect Git context to determine scope
	ctx := context.Background()
	gitContext, err := gitutil.DetectContext(ctx)
	if err != nil {
		// If we can't detect Git context, use global scope
		gitContext = &gitutil.GitContext{}
	}

	// Determine scope from git context
	var currentScope *scope.Scope
	if gitContext.IsRepo {
		if gitContext.RelativePath == "." {
			currentScope = &scope.Scope{
				Type:     "repo",
				RepoURL:  gitContext.RepoURL,
				RepoPath: "",
			}
		} else {
			currentScope = &scope.Scope{
				Type:     "path",
				RepoURL:  gitContext.RepoURL,
				RepoPath: gitContext.RelativePath,
			}
		}
	} else {
		currentScope = &scope.Scope{
			Type: "global",
		}
	}

	// Determine tracking base for this scope
	var trackingBase string
	if currentScope.Type == "global" {
		trackingBase, _ = utils.GetClaudeDir()
	} else {
		trackingBase = filepath.Join(gitContext.RepoRoot, ".claude")
	}

	// Load tracker to check if artifact is installed
	tracker, err := artifacts.LoadInstalledArtifacts(trackingBase)
	if err != nil {
		// Tracker doesn't exist, exit silently
		return nil
	}

	// Check if artifact is in tracker
	var artifactVersion string
	found := false
	for _, installed := range tracker.Artifacts {
		if installed.Name == artifactName {
			artifactVersion = installed.Version
			found = true
			break
		}
	}

	if !found {
		// Artifact not installed by us, exit silently
		return nil
	}

	// Create usage event
	usageEvent := stats.UsageEvent{
		ArtifactName:    artifactName,
		ArtifactVersion: artifactVersion,
		ArtifactType:    artifactType,
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
	}

	// Enqueue event
	if err := stats.EnqueueEvent(usageEvent); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to enqueue usage event: %v\n", err)
		return nil // Don't fail the hook
	}

	// Log successful usage tracking
	log := logger.Get()
	log.Info("artifact usage tracked", "name", artifactName, "version", artifactVersion, "type", artifactType)

	// Try to flush queue
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Load config to get repository
	cfg, err := config.Load()
	if err != nil {
		// Config not initialized, queue will be flushed later
		return nil
	}

	// Create repository instance
	repo, err := repository.NewFromConfig(cfg)
	if err != nil {
		// Unknown repo type, queue will be flushed later
		return nil
	}

	// Try to flush queue
	if err := stats.FlushQueue(ctx, repo); err != nil {
		// Flush failed, queue preserved for next attempt
		fmt.Fprintf(os.Stderr, "Warning: failed to flush usage stats: %v\n", err)
	}

	return nil
}
