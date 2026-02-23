package commands

import (
	"context"
	"fmt"

	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/theme"
	"github.com/spf13/cobra"
)

// Context key for test prompter injection
type prompterKeyType struct{}

var prompterKey = prompterKeyType{}

// outputHelper wraps a cobra.Command to provide convenient output methods
type outputHelper struct {
	cmd      *cobra.Command
	prompter Prompter
	silent   bool // suppress stdout output (for hook mode)
}

// newOutputHelper creates an output helper for the given command
func newOutputHelper(cmd *cobra.Command) *outputHelper {
	// Check if a test prompter was injected via context
	var prompter Prompter
	if cmd.Context() != nil {
		if p, ok := cmd.Context().Value(prompterKey).(Prompter); ok {
			prompter = p
		}
	}

	// Use standard prompter by default
	if prompter == nil {
		prompter = NewStdPrompter(cmd.InOrStdin(), cmd.ErrOrStderr())
	}

	return &outputHelper{
		cmd:      cmd,
		prompter: prompter,
	}
}

// WithPrompter returns a context with the given prompter (for testing)
func WithPrompter(ctx context.Context, prompter Prompter) context.Context {
	return context.WithValue(ctx, prompterKey, prompter)
}

// println writes a line to the command's output
func (o *outputHelper) println(args ...any) {
	if !o.silent {
		fmt.Fprintln(o.cmd.OutOrStdout(), args...)
	}
}

// printlnAlways writes a line to the command's output (even in silent mode)
func (o *outputHelper) printlnAlways(args ...any) {
	fmt.Fprintln(o.cmd.OutOrStdout(), args...)
}

// printf writes formatted output to the command's output
func (o *outputHelper) printf(format string, args ...any) {
	if !o.silent {
		fmt.Fprintf(o.cmd.OutOrStdout(), format, args...)
	}
}

// printErr writes a line to the command's error output with error styling
func (o *outputHelper) printErr(args ...any) {
	msg := fmt.Sprint(args...)
	if ui.IsTTY(o.cmd.ErrOrStderr()) && !ui.NoColor() {
		style := theme.Current().Styles().Error
		fmt.Fprintln(o.cmd.ErrOrStderr(), style.Render(msg))
	} else {
		fmt.Fprintln(o.cmd.ErrOrStderr(), msg)
	}
}

// printfErr writes formatted output to the command's error output with error styling
func (o *outputHelper) printfErr(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if ui.IsTTY(o.cmd.ErrOrStderr()) && !ui.NoColor() {
		style := theme.Current().Styles().Error
		fmt.Fprint(o.cmd.ErrOrStderr(), style.Render(msg))
	} else {
		fmt.Fprint(o.cmd.ErrOrStderr(), msg)
	}
}

// prompt prompts the user for input and returns the trimmed response
// Delegates to the prompter interface for flexibility and testability
func (o *outputHelper) prompt(message string) (string, error) {
	return o.prompter.Prompt(message)
}
