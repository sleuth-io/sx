package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sleuth-io/sx/internal/gitutil"
	"github.com/sleuth-io/sx/internal/logger"
)

// ToolDef represents an MCP tool with its handler
type ToolDef struct {
	Tool    *mcp.Tool
	Handler func(context.Context, *mcp.CallToolRequest, QueryInput) (*mcp.CallToolResult, any, error)
}

// QueryInput is the input type for query tool
type QueryInput struct {
	Query       string `json:"query" jsonschema:"A simple, focused natural language query. Keep queries atomic - ask for one specific thing (e.g., 'Get PR comments', 'Get failed CI checks', 'Get open issues assigned to me'). Avoid complex multi-part queries."`
	Integration string `json:"integration" jsonschema:"which integration to query (github, circleci, or linear)"`
}

// GetMCPTools returns the query tool for Sleuth vault
func (s *SleuthVault) GetMCPTools() any {
	return []ToolDef{
		{
			Tool: &mcp.Tool{
				Name:        "query",
				Description: "Query integrated services (GitHub, CircleCI, Linear) using natural language. Context (repo, branch, commit) is automatically detected from git. For best performance, use simple atomic queries that ask for one specific thing - complex queries take longer and may timeout.",
			},
			Handler: s.handleQueryTool,
		},
	}
}

// handleQueryTool handles the query tool invocation
func (s *SleuthVault) handleQueryTool(ctx context.Context, req *mcp.CallToolRequest, input QueryInput) (*mcp.CallToolResult, any, error) {
	log := logger.Get()
	log.Debug("query tool invoked", "query", input.Query, "integration", input.Integration, "sleuth_url", s.serverURL)

	if input.Query == "" {
		return nil, nil, errors.New("query is required")
	}
	if input.Integration == "" {
		return nil, nil, errors.New("integration is required")
	}

	// Detect git context using existing gitutil
	gitCtx, err := gitutil.DetectContext(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to detect git context: %w", err)
	}

	if !gitCtx.IsRepo {
		return nil, nil, errors.New("not in a git repository")
	}

	// Get current branch
	branch, err := gitutil.GetCurrentBranch(ctx, gitCtx.RepoRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get current branch: %w", err)
	}

	// Get current commit
	commit, err := gitutil.GetCurrentCommit(ctx, gitCtx.RepoRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get current commit: %w", err)
	}

	// Build context for API call (matches AiQueryContextInput schema)
	apiContext := map[string]any{
		"repoUrl":   gitCtx.RepoURL,
		"branch":    branch,
		"commitSha": commit,
	}

	log.Debug("calling sleuth query API with SSE streaming", "repoUrl", gitCtx.RepoURL, "branch", branch, "commit", commit)

	// Create event callback that sends MCP log notifications to keep connection alive
	onEvent := func(eventType, content string) {
		log.Debug("query progress", "type", eventType, "content", content)

		// Send log notification to Claude Code via MCP
		// This writes to stdio, keeping the connection alive and preventing timeout
		logData, _ := json.Marshal(map[string]string{
			"event":   eventType,
			"message": content,
		})
		_ = req.Session.Log(ctx, &mcp.LoggingMessageParams{
			Level:  "info",
			Logger: "sleuth-query",
			Data:   logData,
		})
	}

	// Call Sleuth API with SSE streaming
	result, err := s.QueryIntegrationStream(ctx, input.Query, input.Integration, apiContext, onEvent)
	if err != nil {
		log.Warn("sleuth query API failed", "error", err)
		return nil, nil, fmt.Errorf("sleuth query failed: %w", err)
	}

	log.Debug("sleuth query API succeeded", "result_length", len(result))

	// Return result as plain text
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: result},
		},
	}, nil, nil
}
