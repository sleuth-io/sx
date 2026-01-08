package components

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/theme"
)

// inputModel is the bubbletea model for the input component.
type inputModel struct {
	textInput textinput.Model
	prompt    string
	done      bool
	cancelled bool
	theme     theme.Theme
}

func newInputModel(prompt, placeholder, defaultValue string) inputModel {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.SetValue(defaultValue)
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 50

	th := theme.Current()
	ti.PromptStyle = th.Styles().Emphasis
	ti.TextStyle = th.Styles().Value
	ti.PlaceholderStyle = th.Styles().Muted
	ti.Cursor.Style = th.Styles().Cursor

	return inputModel{
		textInput: ti,
		prompt:    prompt,
		theme:     th,
	}
}

func (m inputModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m inputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			m.done = true
			return m, tea.Quit
		case "ctrl+c", "esc":
			m.cancelled = true
			m.done = true
			return m, tea.Quit
		}
	}

	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m inputModel) View() string {
	if m.done {
		return ""
	}

	styles := m.theme.Styles()
	return styles.Bold.Render(m.prompt) + " " + m.textInput.View()
}

// Value returns the current input value.
func (m inputModel) Value() string {
	return m.textInput.Value()
}

// Input displays an interactive text input prompt.
// Falls back to simple readline for non-TTY environments.
func Input(prompt string) (string, error) {
	return InputWithDefault(prompt, "")
}

// InputWithDefault displays an interactive text input with a default value.
func InputWithDefault(prompt, defaultValue string) (string, error) {
	return InputWithIO(prompt, "", defaultValue, os.Stdin, os.Stdout)
}

// InputWithPlaceholder displays an interactive text input with placeholder text.
func InputWithPlaceholder(prompt, placeholder string) (string, error) {
	return InputWithIO(prompt, placeholder, "", os.Stdin, os.Stdout)
}

// InputWithIO displays an interactive text input using custom IO.
func InputWithIO(prompt, placeholder, defaultValue string, in io.Reader, out io.Writer) (string, error) {
	// Fall back to simple prompt for non-TTY
	if !ui.IsStdoutTTY() || !ui.IsStdinTTY() {
		return inputSimple(prompt, defaultValue, in, out)
	}

	m := newInputModel(prompt, placeholder, defaultValue)
	p := tea.NewProgram(m, tea.WithOutput(out))

	result, err := p.Run()
	if err != nil {
		return "", fmt.Errorf("input failed: %w", err)
	}

	final := result.(inputModel)
	if final.cancelled {
		return "", errors.New("input cancelled")
	}

	value := final.Value()
	if value == "" && defaultValue != "" {
		return defaultValue, nil
	}

	return value, nil
}

// inputSimple provides a simple readline fallback for non-TTY environments.
func inputSimple(prompt, defaultValue string, in io.Reader, out io.Writer) (string, error) {
	if defaultValue != "" {
		fmt.Fprintf(out, "%s [%s]: ", prompt, defaultValue)
	} else {
		fmt.Fprintf(out, "%s: ", prompt)
	}

	// Reuse existing bufio.Reader if provided, otherwise create new one
	reader, ok := in.(*bufio.Reader)
	if !ok {
		reader = bufio.NewReader(in)
	}
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(input)
	if input == "" && defaultValue != "" {
		return defaultValue, nil
	}

	return input, nil
}

// Password displays a password input (masked characters).
func Password(prompt string) (string, error) {
	return PasswordWithIO(prompt, os.Stdin, os.Stdout)
}

// PasswordWithIO displays a password input using custom IO.
func PasswordWithIO(prompt string, in io.Reader, out io.Writer) (string, error) {
	// Fall back to simple prompt for non-TTY
	if !ui.IsStdoutTTY() || !ui.IsStdinTTY() {
		return inputSimple(prompt, "", in, out)
	}

	ti := textinput.New()
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 50

	th := theme.Current()
	ti.PromptStyle = th.Styles().Emphasis
	ti.TextStyle = th.Styles().Value

	m := inputModel{
		textInput: ti,
		prompt:    prompt,
		theme:     th,
	}

	p := tea.NewProgram(m, tea.WithOutput(out))

	result, err := p.Run()
	if err != nil {
		return "", fmt.Errorf("password input failed: %w", err)
	}

	final := result.(inputModel)
	if final.cancelled {
		return "", errors.New("input cancelled")
	}

	return final.Value(), nil
}
