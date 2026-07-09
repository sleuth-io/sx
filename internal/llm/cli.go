package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// cliProvider runs an installed coding-agent CLI (claude, codex,
// gemini) headless and returns its final answer. This reuses whatever
// auth the user already set up in that CLI — a user-initiated
// convenience, never something an extension can reach without the
// llm:use grant and the user having picked this provider in Settings.
//
// TRUST BOUNDARY: the prompt is extension-authored, and these CLIs are
// agents that can normally run tools. Each invocation is therefore
// pinned to its most restricted mode — claude gets `--tools ""` (no
// tools at all), codex gets `--sandbox read-only`, gemini gets
// `--approval-mode default` (headless has no approver, so mutating
// tools can't run) — and the working directory is a fresh empty
// scratch dir, never a real project. Residual risk on codex/gemini:
// their restricted modes still permit READS, so a hostile prompt could
// try to exfiltrate file contents through the reply. That's why the
// consent line for llm:use names prompt-sending explicitly and CLI
// providers are an explicit user choice, never a default.
type cliProvider struct {
	id      string
	binPath string
	model   string
}

func (p *cliProvider) ID() string { return p.id }

// cliEnv returns the child process environment with PATH extended by
// the GUI extra dirs — the CLIs are mostly node scripts whose shebang
// interpreter must also resolve, and a GUI app's PATH misses Homebrew.
func cliEnv() []string {
	env := os.Environ()
	path := os.Getenv("PATH")
	for _, dir := range guiExtraDirs() {
		if !strings.Contains(path, dir) {
			path += string(os.PathListSeparator) + dir
		}
	}
	return append(env, "PATH="+path)
}

func (p *cliProvider) Complete(ctx context.Context, req Request) (Response, error) {
	if err := validateRequest(req); err != nil {
		return Response{}, err
	}
	prompt := flattenPrompt(req.Messages)
	if len(req.Schema) > 0 {
		prompt += "\n\n" + schemaInstruction(req.Schema)
	}
	model := req.Model
	if model == "" {
		model = p.model
	}
	ctx, cancel := context.WithTimeout(ctx, completionTimeout)
	defer cancel()

	var resp Response
	var err error
	switch p.id {
	case ProviderClaudeCLI:
		resp, err = p.runClaude(ctx, prompt, model)
	case ProviderCodexCLI:
		resp, err = p.runCodex(ctx, prompt, model)
	case ProviderGeminiCLI:
		resp, err = p.runGemini(ctx, prompt, model)
	default:
		return Response{}, fmt.Errorf("unknown CLI provider %q", p.id)
	}
	if err != nil {
		return Response{}, err
	}
	resp.Text, err = finishStructured(req, resp.Text)
	if err != nil {
		return Response{}, err
	}
	resp.Provider = p.id
	return resp, nil
}

// run executes the CLI with the prompt on stdin and returns stdout.
func (p *cliProvider) run(ctx context.Context, stdin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, p.binPath, args...) // #nosec G204 -- binPath comes from provider detection, args are fixed flags
	cmd.Env = cliEnv()
	// Run in a fresh empty scratch dir: these CLIs are
	// workspace-oriented, and neither the app bundle's cwd nor the
	// user's home may be treated as the project an extension-authored
	// prompt gets to look at.
	if scratch, err := os.MkdirTemp("", "sx-llm-*"); err == nil {
		cmd.Dir = scratch
		defer func() { _ = os.RemoveAll(scratch) }()
	}
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if len(detail) > 2000 {
			detail = detail[:2000]
		}
		return "", fmt.Errorf("%s failed: %s", filepath.Base(p.binPath), detail)
	}
	return stdout.String(), nil
}

func (p *cliProvider) runClaude(ctx context.Context, prompt, model string) (Response, error) {
	// --tools "" removes every built-in tool: the agent CLI degrades to
	// a pure completion endpoint, which is all llm:use grants.
	args := []string{"-p", "--output-format", "json", "--tools", ""}
	if model != "" {
		args = append(args, "--model", model)
	}
	out, err := p.run(ctx, prompt, args...)
	if err != nil {
		return Response{}, err
	}
	var parsed struct {
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
		Usage   struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &parsed); jsonErr != nil {
		// Older CLI or changed envelope — fall back to raw output.
		return Response{Text: strings.TrimSpace(out), Model: model}, nil
	}
	if parsed.IsError {
		return Response{}, fmt.Errorf("claude CLI reported an error: %s", parsed.Result)
	}
	return Response{
		Text:  parsed.Result,
		Model: model,
		Usage: Usage{InputTokens: parsed.Usage.InputTokens, OutputTokens: parsed.Usage.OutputTokens},
	}, nil
}

func (p *cliProvider) runCodex(ctx context.Context, prompt, model string) (Response, error) {
	// --output-last-message isolates the final answer from the session
	// log codex prints to stdout.
	lastMsg, err := os.CreateTemp("", "sx-codex-*.txt")
	if err != nil {
		return Response{}, err
	}
	lastPath := lastMsg.Name()
	_ = lastMsg.Close()
	defer func() { _ = os.Remove(lastPath) }()

	// codex has no no-tools mode; read-only is its most restricted
	// sandbox (shell commands can't write or leave side effects).
	args := []string{"exec", "--skip-git-repo-check", "--sandbox", "read-only", "--output-last-message", lastPath}
	if model != "" {
		args = append(args, "-m", model)
	}
	args = append(args, "-") // read the prompt from stdin
	out, err := p.run(ctx, prompt, args...)
	if err != nil {
		return Response{}, err
	}
	final, readErr := os.ReadFile(lastPath) // #nosec G304 -- temp file we created above
	if readErr == nil && len(bytes.TrimSpace(final)) > 0 {
		return Response{Text: strings.TrimSpace(string(final)), Model: model}, nil
	}
	return Response{Text: strings.TrimSpace(out), Model: model}, nil
}

func (p *cliProvider) runGemini(ctx context.Context, prompt, model string) (Response, error) {
	// Pin approval-mode to default (never yolo/auto_edit): headless has
	// no one to approve, so approval-gated tools cannot execute.
	args := []string{"--output-format", "json", "--approval-mode", "default"}
	if model != "" {
		args = append(args, "--model", model)
	}
	out, err := p.run(ctx, prompt, args...)
	if err != nil {
		return Response{}, err
	}
	var parsed struct {
		Response string `json:"response"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &parsed); jsonErr != nil || parsed.Response == "" {
		return Response{Text: strings.TrimSpace(out), Model: model}, nil
	}
	return Response{Text: parsed.Response, Model: model}, nil
}
