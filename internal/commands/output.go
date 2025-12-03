package commands

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

// outputHelper wraps a cobra.Command to provide convenient output methods
type outputHelper struct {
	cmd *cobra.Command
}

// newOutputHelper creates an output helper for the given command
func newOutputHelper(cmd *cobra.Command) *outputHelper {
	return &outputHelper{cmd: cmd}
}

// println writes a line to the command's output
func (o *outputHelper) println(args ...interface{}) {
	fmt.Fprintln(o.cmd.OutOrStdout(), args...)
}

// printf writes formatted output to the command's output
func (o *outputHelper) printf(format string, args ...interface{}) {
	fmt.Fprintf(o.cmd.OutOrStdout(), format, args...)
}

// printErr writes a line to the command's error output
func (o *outputHelper) printErr(args ...interface{}) {
	fmt.Fprintln(o.cmd.ErrOrStderr(), args...)
}

// printfErr writes formatted output to the command's error output
func (o *outputHelper) printfErr(format string, args ...interface{}) {
	fmt.Fprintf(o.cmd.ErrOrStderr(), format, args...)
}

// prompt prompts the user for input and returns the trimmed response
func (o *outputHelper) prompt(message string) (string, error) {
	fmt.Fprint(o.cmd.ErrOrStderr(), message)
	reader := bufio.NewReader(o.cmd.InOrStdin())
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(input), nil
}

// promptWithDefault prompts the user with a default value
func (o *outputHelper) promptWithDefault(message, defaultValue string) (string, error) {
	fullMessage := fmt.Sprintf("%s [%s]: ", message, defaultValue)
	response, err := o.prompt(fullMessage)
	if err != nil {
		return "", err
	}
	if response == "" {
		return defaultValue, nil
	}
	return response, nil
}

// getOutput returns the command's output writer
func (o *outputHelper) getOutput() io.Writer {
	return o.cmd.OutOrStdout()
}
