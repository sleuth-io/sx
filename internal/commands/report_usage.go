package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
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

// CopilotPostToolUseEvent represents the JSON payload from GitHub Copilot postToolUse hook
type CopilotPostToolUseEvent struct {
	ToolName string         `json:"toolName"`
	ToolArgs map[string]any `json:"toolArgs"`
}

// CodexNotifyEvent represents the JSON payload from Codex notify hook
type CodexNotifyEvent struct {
	Type                 string   `json:"type"`
	TurnID               string   `json:"turn-id"`
	InputMessages        []string `json:"input-messages"`
	LastAssistantMessage string   `json:"last-assistant-message"`
}

// KiroPostToolUseEvent represents the JSON payload from Kiro postToolUse hook
type KiroPostToolUseEvent struct {
	ToolName   string `json:"toolName"`
	ToolResult string `json:"toolResult"`
}

// kiroSkillPathRegex matches skill file paths in Kiro's readFile tool result
// Captures the top-level skill name (handles both single-file and multi-file skills)
// e.g., .kiro/skills/my-skill.md -> my-skill
// e.g., .kiro/skills/my-skill/index.md -> my-skill
var kiroSkillPathRegex = regexp.MustCompile(`<file name="\.kiro/skills/([^/".]+)(?:\.md|/)`)

// extractKiroSkillNames extracts all skill names from Kiro's readFile tool result
// Returns all unique skill names found in the tool result
func extractKiroSkillNames(toolResult string) []string {
	matches := kiroSkillPathRegex.FindAllStringSubmatch(toolResult, -1)
	if len(matches) == 0 {
		return nil
	}

	// Deduplicate skill names
	seen := make(map[string]bool)
	var names []string
	for _, match := range matches {
		if len(match) >= 2 && !seen[match[1]] {
			seen[match[1]] = true
			names = append(names, match[1])
		}
	}
	return names
}

// extractKiroSkillName extracts the first skill name from Kiro's readFile tool result
// Kept for backwards compatibility
func extractKiroSkillName(toolResult string) string {
	names := extractKiroSkillNames(toolResult)
	if len(names) > 0 {
		return names[0]
	}
	return ""
}

// runReportUsage executes the report-usage command
func runReportUsage(cmd *cobra.Command, args []string) error {
	// Initialize logger early to capture all errors
	log := logger.Get()

	clientID, _ := cmd.Flags().GetString("client")

	var data []byte
	var err error

	// Try different input methods based on client
	if len(args) > 0 {
		// Codex format: JSON as first argument
		data = []byte(args[0])
	} else if userPrompt := os.Getenv("USER_PROMPT"); userPrompt != "" {
		// Kiro format: JSON in USER_PROMPT env var
		data = []byte(userPrompt)
	} else {
		// Claude Code/Cursor format: JSON from stdin
		data, err = io.ReadAll(os.Stdin)
		if err != nil {
			log.Error("report-usage: failed to read stdin", "error", err)
			return fmt.Errorf("failed to read stdin: %w", err)
		}
	}

	// uncomment for debugging
	// 	log.Debug("report-usage: received", "data", string(data), "client", clientID)

	// Empty input is not an error - just nothing to do
	if len(data) == 0 {
		log.Debug("report-usage: no data received, skipping")
		return nil
	}

	// Try Codex format first (check for agent-turn-complete type)
	// Codex's agent-turn-complete doesn't contain tool usage data, so skip it
	var codexEvent CodexNotifyEvent
	if err := json.Unmarshal(data, &codexEvent); err == nil && codexEvent.Type == "agent-turn-complete" {
		return nil
	}

	// Try Claude Code/Cursor format (snake_case)
	var event PostToolUseEvent
	if err := json.Unmarshal(data, &event); err != nil {
		log.Error("report-usage: failed to parse hook event JSON", "error", err, "data_length", len(data), "client", clientID)
		return nil
	}

	// If no tool name from Claude Code format, try Copilot/Kiro format (camelCase)
	if event.ToolName == "" {
		var copilotEvent CopilotPostToolUseEvent
		if err := json.Unmarshal(data, &copilotEvent); err == nil && copilotEvent.ToolName != "" {
			event.ToolName = copilotEvent.ToolName
			event.ToolInput = copilotEvent.ToolArgs
		}
	}

	// If no tool name, nothing to detect
	if event.ToolName == "" {
		return nil
	}

	// Kiro-specific detection: extract skill name from readFile tool result
	if event.ToolName == "readFile" && clientID == "kiro" {
		var kiroEvent KiroPostToolUseEvent
		if err := json.Unmarshal(data, &kiroEvent); err == nil {
			if skillName := extractKiroSkillName(kiroEvent.ToolResult); skillName != "" {
				// Found a skill - set up for tracking
				event.ToolName = "Skill"
				event.ToolInput = map[string]any{"skill": skillName}
			}
		}
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

	// Try to flush queue asynchronously to avoid blocking Kiro hooks
	// The event is already persisted to disk, so a failed flush is recoverable
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Load config to get repository
		cfg, err := config.Load()
		if err != nil {
			log.Error("report-usage: failed to load config", "error", err)
			return
		}

		// Create vault instance
		vault, err := vaultpkg.NewFromConfig(cfg)
		if err != nil {
			log.Error("report-usage: failed to create vault", "error", err)
			return
		}

		// Try to flush queue
		if err := stats.FlushQueue(ctx, vault); err != nil {
			log.Error("report-usage: failed to flush usage stats", "error", err)
		}
	}()

	return nil
}
