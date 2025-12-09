package commands

import (
	"github.com/sleuth-io/skills/internal/claude"
)

// outputAdapter adapts outputHelper to claude.Output interface
type outputAdapter struct {
	out *outputHelper
}

func (a *outputAdapter) Println(msg string) {
	a.out.println(msg)
}

func (a *outputAdapter) PrintfErr(format string, args ...interface{}) {
	a.out.printfErr(format, args...)
}

// installClaudeCodeHooks installs all Claude Code hooks (usage tracking and auto-update)
func installClaudeCodeHooks(claudeDir string, out *outputHelper) error {
	// Note: claudeDir parameter is ignored, we use claude.InstallHooks which gets it internally
	adapter := &outputAdapter{out: out}
	return claude.InstallHooks(adapter)
}
