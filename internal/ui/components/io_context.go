package components

import (
	"io"
)

// IOContext holds IO streams and provides convenient methods for interactive components
// This avoids passing io.Reader and io.Writer to every component call
type IOContext struct {
	In  io.Reader
	Out io.Writer
}

// NewIOContext creates a new IO context
func NewIOContext(in io.Reader, out io.Writer) *IOContext {
	return &IOContext{
		In:  in,
		Out: out,
	}
}

// Confirm asks a yes/no question with a default answer
func (ioc *IOContext) Confirm(message string, defaultYes bool) (bool, error) {
	return ConfirmWithIO(message, defaultYes, ioc.In, ioc.Out)
}

// Input prompts for text input with optional default value
func (ioc *IOContext) Input(prompt, defaultValue string) (string, error) {
	return InputWithIO(prompt, "", defaultValue, ioc.In, ioc.Out)
}

// InputWithPlaceholder prompts for text input with placeholder text
func (ioc *IOContext) InputWithPlaceholder(prompt, placeholder string) (string, error) {
	return InputWithIO(prompt, placeholder, "", ioc.In, ioc.Out)
}

// Select displays an interactive selection menu
func (ioc *IOContext) Select(title string, options []Option) (*Option, error) {
	return SelectWithIO(title, options, ioc.In, ioc.Out)
}

// SelectWithDefault displays a selection menu with a default option
func (ioc *IOContext) SelectWithDefault(title string, options []Option, defaultIndex int) (*Option, error) {
	return SelectWithDefaultAndIO(title, options, defaultIndex, ioc.In, ioc.Out)
}

// MultiSelect displays an interactive multi-selection menu and returns the
// chosen options (those with Selected=true in the result).
func (ioc *IOContext) MultiSelect(title string, options []MultiSelectOption) ([]MultiSelectOption, error) {
	return MultiSelectWithIO(title, options, ioc.In, ioc.Out)
}
