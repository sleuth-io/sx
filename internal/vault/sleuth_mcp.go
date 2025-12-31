package vault

import (
	"context"
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
	Query       string `json:"query" jsonschema:"natural language query (e.g., 'Get PR comments from Claude Code bot', 'Get failed CI checks')"`
	Integration string `json:"integration" jsonschema:"which integration to query (github, circleci, or linear)"`
}

// GetMCPTools returns the query tool for Sleuth vault
func (s *SleuthVault) GetMCPTools() interface{} {
	return []ToolDef{
		{
			Tool: &mcp.Tool{
				Name:        "query",
				Description: "Query integrated services (GitHub, CircleCI, Linear) using natural language. Context (repo, branch, commit) is automatically detected from git.",
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
		return nil, nil, fmt.Errorf("query is required")
	}
	if input.Integration == "" {
		return nil, nil, fmt.Errorf("integration is required")
	}

	// Detect git context using existing gitutil
	gitCtx, err := gitutil.DetectContext(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to detect git context: %w", err)
	}

	if !gitCtx.IsRepo {
		return nil, nil, fmt.Errorf("not in a git repository")
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

	// Parse repo name from URL
	repoName := gitutil.ParseRepoFromURL(gitCtx.RepoURL)

	// Build context for API call (matches AiQueryContextInput schema)
	apiContext := map[string]interface{}{
		"repo":      repoName,
		"branch":    branch,
		"commitSha": commit,
	}

	log.Debug("calling sleuth query API", "repo", repoName, "branch", branch, "commit", commit)

	// Call Sleuth API
	result, err := s.QueryIntegration(ctx, input.Query, input.Integration, apiContext)
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
