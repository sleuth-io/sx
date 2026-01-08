# SX MCP Server Specification

## Overview

sx provides a built-in MCP (Model Context Protocol) server that exposes tools to AI coding assistants. When you run `sx serve`, it starts an MCP server over stdio that provides:

1. **read_skill** - Read installed skill content with resolved file references
2. **query** - Query integrated services (GitHub, CircleCI, Linear) using natural language

The MCP server is automatically configured when you install sx assets, allowing AI assistants to access skills and query external services without additional setup.

## Starting the MCP Server

```bash
sx serve
```

This starts the MCP server over stdio, ready to accept tool calls from AI clients.

## Built-in Tools

### read_skill

Read a skill's full instructions and content.

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `name` | string | Yes | Name of the skill to read |

**Returns:** The skill content as markdown with `@file` references resolved to absolute paths.

**Example:**

```json
{
  "name": "read_skill",
  "arguments": {
    "name": "code-reviewer"
  }
}
```

**File Reference Resolution:**

Skills can reference local files using `@filename` syntax. When read through the MCP server, these references are automatically resolved to absolute paths if the file exists:

```markdown
<!-- In skill content -->
See the coding standards at @coding-standards.md

<!-- Resolved output -->
See the coding standards at @/home/user/project/.claude/skills/my-skill/coding-standards.md
```

### query

Query integrated services using natural language. This is the primary tool for AI assistants to interact with external development services.

**Parameters:**

| Name | Type | Required | Description |
|------|------|----------|-------------|
| `query` | string | Yes | Natural language query describing what you want to know |
| `integration` | string | Yes | Service to query: `github`, `circleci`, or `linear` |

**Automatic Context Detection:**

The query tool automatically detects git context from the current working directory:
- Repository URL (from git remote)
- Current branch
- Current commit SHA

This context is passed to the query, so you don't need to specify which repository you're asking about.

**Returns:** Plain text response with the query results.

## Query Tool Usage Guide

The query tool is designed for AI assistants to gather information about the current development context. It works best with simple, focused queries.

### Supported Integrations

#### GitHub

Query pull requests, issues, reviews, and repository information.

**Example queries:**
- "Get comments on this PR"
- "List open pull requests"
- "Get review status for PR #123"
- "Show failing checks on this branch"
- "Get issues assigned to me"

#### CircleCI

Query build status, pipeline information, and test results.

**Example queries:**
- "Get failed CI checks"
- "Show recent pipeline runs"
- "What tests failed in the last build?"
- "Get build status for this branch"

#### Linear

Query issues, projects, and sprint information.

**Example queries:**
- "Get my assigned issues"
- "Show issues in the current sprint"
- "List high priority bugs"
- "Get project status"

### Best Practices

**Keep queries atomic:**

```
Good: "Get PR comments"
Bad:  "Get PR comments and also check if CI passed and list any related issues"
```

**Be specific:**

```
Good: "Get review comments on PR #42"
Bad:  "Tell me about the PR"
```

**Let context do the work:**

The tool auto-detects your repository, branch, and commit. Don't include these in your query unless you need to override them:

```
Good: "Get open PRs"  (uses current repo)
Bad:  "Get open PRs for github.com/user/repo on branch main"
```

### Example Tool Calls

**Check PR review status:**

```json
{
  "name": "query",
  "arguments": {
    "query": "Get review comments and approval status",
    "integration": "github"
  }
}
```

**Get CI failures:**

```json
{
  "name": "query",
  "arguments": {
    "query": "What checks failed on this branch?",
    "integration": "circleci"
  }
}
```

**Find related issues:**

```json
{
  "name": "query",
  "arguments": {
    "query": "Get issues related to authentication",
    "integration": "linear"
  }
}
```

## Using MCP Tools in Skills

Skills can leverage MCP tools to enhance their capabilities. When creating skills that use these tools, document the dependency clearly.

### Skill Example: PR Review Helper

```markdown
# PR Review Helper

This skill helps review pull requests by gathering context from GitHub and CI.

## Required Tools

This skill uses:
- `mcp__sx__query` - Query GitHub and CircleCI for PR context

## Workflow

1. First, gather PR context:
   - Use the query tool with GitHub to get PR comments and review status
   - Use the query tool with CircleCI to check build status

2. Then provide review feedback based on the gathered context
```

### Tool Naming Convention

When skills reference MCP tools, they follow the pattern: `mcp__<server>__<tool>`

For sx tools:
- `mcp__sx__read_skill` - Read skill content
- `mcp__sx__query` - Query integrations

### Automatic Dependency Detection

sx automatically detects when skills use MCP tools based on tool call patterns in conversation history. This helps track which skills depend on which MCP capabilities.

## Streaming and Progress

The query tool uses Server-Sent Events (SSE) for streaming responses. During long-running queries, the tool sends progress notifications to keep the connection alive and inform the user of status:

```
event: progress
message: Querying GitHub API...

event: progress
message: Processing 15 comments...

event: complete
message: Query completed
```

These notifications appear as log messages in the AI client, providing visibility into query progress.

## Error Handling

**Not in a git repository:**

```
Error: not in a git repository
```

The query tool requires git context. Run it from within a git repository.

**Missing required parameters:**

```
Error: query is required
Error: integration is required
```

Both `query` and `integration` parameters must be provided.

**API errors:**

If the underlying service API fails, the error message will include details about what went wrong. Common issues:
- Authentication failures (check API tokens)
- Rate limiting (wait and retry)
- Invalid queries (rephrase the question)

## Configuration

The MCP server uses the vault configuration from `sx init`. For the query tool to work, you must be using the Sleuth vault:

```bash
sx init --type sleuth
```

The query tool is only available with Sleuth vault, as it connects to Sleuth's AI query service which integrates with your configured GitHub, CircleCI, and Linear accounts.

## Architecture

```
┌─────────────────┐     stdio      ┌─────────────────┐
│   AI Client     │◄──────────────►│   sx serve      │
│ (Claude Code)   │                │   MCP Server    │
└─────────────────┘                └────────┬────────┘
                                            │
                                   ┌────────┴────────┐
                                   │                 │
                              ┌────▼────┐     ┌──────▼──────┐
                              │read_skill│     │   query     │
                              │  Tool    │     │   Tool      │
                              └────┬────┘     └──────┬──────┘
                                   │                 │
                              ┌────▼────┐     ┌──────▼──────┐
                              │ Local   │     │ Sleuth API  │
                              │ Skills  │     │ (SSE)       │
                              └─────────┘     └─────────────┘
                                                     │
                                              ┌──────┴──────┐
                                              │             │
                                         ┌────▼───┐  ┌──────▼────┐
                                         │ GitHub │  │ CircleCI  │
                                         └────────┘  └───────────┘
                                              │
                                         ┌────▼───┐
                                         │ Linear │
                                         └────────┘
```

## Future Enhancements

Potential additions for future versions:

- **Additional integrations** - Jira, Slack, PagerDuty
- **Custom tool registration** - Allow skills to register custom MCP tools
- **Tool composition** - Chain multiple tool calls in a single request
- **Caching** - Cache query results for frequently accessed data
