package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/sleuth-io/sx/internal/assets"
	"github.com/sleuth-io/sx/internal/assets/detectors"
	"github.com/sleuth-io/sx/internal/config"
	"github.com/sleuth-io/sx/internal/logger"
	"github.com/sleuth-io/sx/internal/stats"
	vaultpkg "github.com/sleuth-io/sx/internal/vault"
)

// NewReportUsageCommand creates the report-usage command
func NewReportUsageCommand() *cobra.Command {
	var clientID string

	cmd := &cobra.Command{
		Use:   "report-usage",
		Short: "Report asset usage from tool calls (PostToolUse hook)",
		Long: `Parse PostToolUse hook JSON from stdin, detect asset usage,
and report it to the vault. Intended to be called from Claude Code hooks.`,
		Hidden: true, // Hide from help output as it's for internal use
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReportUsage(cmd, args)
		},
	}

	cmd.Flags().StringVar(&clientID, "client", "", "Client ID that triggered the hook (informational only)")

	return cmd
}

// PostToolUseEvent represents the JSON payload from Claude Code PostToolUse hook
type PostToolUseEvent struct {
	ToolName  string         `json:"tool_name"`
	ToolInput map[string]any `json:"tool_input"`
}

// CodexNotifyEvent represents the JSON payload from Codex notify hook
type CodexNotifyEvent struct {
	Type                 string   `json:"type"`
	TurnID               string   `json:"turn-id"`
	InputMessages        []string `json:"input-messages"`
	LastAssistantMessage string   `json:"last-assistant-message"`
}

// runReportUsage executes the report-usage command
func runReportUsage(cmd *cobra.Command, args []string) error {
	// Initialize logger early to capture all errors
	log := logger.Get()

	clientID, _ := cmd.Flags().GetString("client")

	var data []byte
	var err error

	// Codex passes JSON as command-line argument, others use stdin
	if len(args) > 0 {
		// Codex format: JSON as first argument
		data = []byte(args[0])
	} else {
		// Claude Code/Cursor format: JSON from stdin
		data, err = io.ReadAll(os.Stdin)
		if err != nil {
			log.Error("report-usage: failed to read stdin", "error", err)
			return fmt.Errorf("failed to read stdin: %w", err)
		}
	}

	// Try Codex format first (check for agent-turn-complete type)
	var codexEvent CodexNotifyEvent
	if err := json.Unmarshal(data, &codexEvent); err == nil && codexEvent.Type == "agent-turn-complete" {
		// Codex event parsed successfully - log it but no asset detection possible
		// Codex's agent-turn-complete doesn't contain tool usage data
		log.Debug("report-usage: received Codex notify event", "type", codexEvent.Type, "turn_id", codexEvent.TurnID)
		return nil
	}

	// Try Claude Code/Cursor format
	var event PostToolUseEvent
	if err := json.Unmarshal(data, &event); err != nil {
		log.Error("report-usage: failed to parse hook event JSON", "error", err, "data_length", len(data), "client", clientID)
		return nil
	}

	// If no tool name, nothing to detect
	if event.ToolName == "" {
		return nil
	}

	// Create all handlers for detection
	allHandlers := []detectors.UsageDetector{
		&detectors.SkillDetector{},
		&detectors.AgentDetector{},
		&detectors.CommandDetector{},
		&detectors.MCPDetector{},
		&detectors.HookDetector{},
		&detectors.ClaudeCodePluginDetector{},
	}

	// Try to detect asset usage from each handler
	var assetName string
	var assetType string
	var detected bool

	for _, handler := range allHandlers {
		assetName, detected = handler.DetectUsageFromToolCall(event.ToolName, event.ToolInput)
		if detected {
			// Get asset type from handler
			if typedHandler, ok := handler.(detectors.AssetTypeDetector); ok {
				assetType = typedHandler.GetType()
			}
			break
		}
	}

	// If no handler detected usage, exit
	if !detected || assetName == "" {
		return nil
	}

	// Load tracker to check if asset is installed
	tracker, err := assets.LoadTracker()
	if err != nil {
		log.Error("report-usage: failed to load tracker", "error", err, "asset", assetName)
		return nil
	}

	// Check if asset is in tracker
	var assetVersion string
	found := false
	for _, installed := range tracker.Assets {
		if installed.Name == assetName {
			assetVersion = installed.Version
			found = true
			break
		}
	}

	if !found {
		return nil
	}

	// Create usage event
	usageEvent := stats.UsageEvent{
		AssetName:    assetName,
		AssetVersion: assetVersion,
		AssetType:    assetType,
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
	}

	// Enqueue event
	if err := stats.EnqueueEvent(usageEvent); err != nil {
		log.Error("report-usage: failed to enqueue usage event", "error", err, "asset", assetName)
		return nil // Don't fail the hook
	}

	// Log successful usage tracking
	log.Info("report-usage: asset usage tracked", "name", assetName, "version", assetVersion, "type", assetType)

	// Try to flush queue
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Load config to get repository
	cfg, err := config.Load()
	if err != nil {
		log.Error("report-usage: failed to load config", "error", err)
		return nil
	}

	// Create vault instance
	vault, err := vaultpkg.NewFromConfig(cfg)
	if err != nil {
		log.Error("report-usage: failed to create vault", "error", err)
		return nil
	}

	// Try to flush queue
	if err := stats.FlushQueue(ctx, vault); err != nil {
		log.Error("report-usage: failed to flush usage stats", "error", err)
	}

	return nil
}
