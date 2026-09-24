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

	"github.com/sleuth-io/sx/v2/internal/assets"
	"github.com/sleuth-io/sx/v2/internal/assets/detectors"
	"github.com/sleuth-io/sx/v2/internal/config"
	"github.com/sleuth-io/sx/v2/internal/logger"
	"github.com/sleuth-io/sx/v2/internal/stats"
	vaultpkg "github.com/sleuth-io/sx/v2/internal/vault"
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

// kiroSkillPathRegex spots skill reads in Kiro's readFile tool result.
//
// It keys on the "skills/<name>" path segment alone and ignores whatever prefix
// the path carries, because Kiro reports the path verbatim as the model asked
// for it: the IDE agent's read_file returns its readPath unchanged on the happy
// path, so one installed skill legitimately arrives as any of
//
//	.kiro/skills/my-skill/SKILL.md      repo-local, relative to the workspace
//	/home/me/.kiro/skills/my-skill.md   absolute, default config root
//	~/.kiro/skills/my-skill.md          absolute, spelled with a tilde
//	/opt/profile/skills/my-skill.md     KIRO_HOME root, no .kiro segment at all
//
// Keying on any particular root drops the forms it did not predict, and it
// cannot predict them all: KIRO_HOME *is* the Kiro config root, so a relocated
// home has no .kiro segment anywhere, and even under the default root the tilde
// and relative spellings never equal a reconstructed absolute path.
//
// Matching the segment and leaving the decision to the installed-asset check in
// runReportUsage is also what every other client already does. Claude, Copilot,
// Codex and Cursor skill reads reach detectors.SkillDetector as a plain skill
// NAME carrying no path at all, so a relocated CLAUDE_CONFIG_DIR or COPILOT_HOME
// cannot affect them; the only other path-keyed recognition in the codebase,
// detectors.DetectAssetTypeFromPath, likewise tests for a bare "/skills/"
// segment and ignores the prefix. Kiro needs a path only because it has no Skill
// tool to name the skill for us, so the name is all this recovers.
//
// The optional prefix must end in "/", so "skills" has to be a whole path
// segment: "noskills/my-skill/SKILL.md" is not a skill read. The match is rooted
// at the <file name=" attribute so a path merely mentioned in file content is
// not counted as a read.
//
// Consequence accepted deliberately: an sx vault keeps its own authored skills
// in a top-level skills/ directory, and a vault author has those same skills
// installed, so reading one in the vault repo does report usage. That is a real
// read of a genuinely installed skill, and no root-based filter could reject it
// without also rejecting the relocated-home read above.
var kiroSkillPathRegex = regexp.MustCompile(`<file name="(?:[^"]*/)?skills/([^/".]+)(?:\.md|/)`)

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

	// Flush queue synchronously. This used to be wrapped in `go func()` to avoid
	// blocking Kiro hooks, but `report-usage` is a short-lived CLI command — when
	// runReportUsage returns, main() exits and the goroutine is killed before the
	// network call can complete, so events accumulate on disk and never reach the
	// server. Block on the flush with a bounded timeout instead.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := flushUsageQueue(ctx); err != nil {
		log.Error("report-usage: failed to flush usage stats", "error", err)
	}

	return nil
}

// flushUsageQueue loads the config, builds a vault, and flushes the on-disk
// usage queue to the server. It is a package-level var so tests can replace it
// with a mock that records when (and whether) the flush happened — the goroutine
// regression that broke usage stats on 3/28/26 only manifests if the flush runs
// after runReportUsage has returned, so the test must be able to assert call
// ordering.
var flushUsageQueue = defaultFlushUsageQueue

func defaultFlushUsageQueue(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	vault, err := vaultpkg.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create vault: %w", err)
	}
	return stats.FlushQueue(ctx, vault)
}
