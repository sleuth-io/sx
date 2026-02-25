package components

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/reflow/wordwrap"
	"golang.org/x/term"

	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/theme"
)

// confirmModel is the bubbletea model for the confirm component.
type confirmModel struct {
	message    string
	confirmed  bool
	defaultYes bool
	done       bool
	cancelled  bool
	theme      theme.Theme
	width      int
}

// confirmKeyMap defines the keybindings for the confirm component.
type confirmKeyMap struct {
	Yes    key.Binding
	No     key.Binding
	Toggle key.Binding
	Submit key.Binding
	Quit   key.Binding
}

var confirmKeys = confirmKeyMap{
	Yes: key.NewBinding(
		key.WithKeys("y", "Y"),
		key.WithHelp("y", "yes"),
	),
	No: key.NewBinding(
		key.WithKeys("n", "N"),
		key.WithHelp("n", "no"),
	),
	Toggle: key.NewBinding(
		key.WithKeys("left", "right", "h", "l", "tab"),
		key.WithHelp("←/→", "toggle"),
	),
	Submit: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "confirm"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c", "esc"),
		key.WithHelp("q", "quit"),
	),
}

func newConfirmModel(message string, defaultYes bool) confirmModel {
	width := 80 // default
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		width = w
	}
	return confirmModel{
		message:    message,
		confirmed:  defaultYes,
		defaultYes: defaultYes,
		theme:      theme.Current(),
		width:      width,
	}
}

func (m confirmModel) Init() tea.Cmd {
	return nil
}

func (m confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, confirmKeys.Quit):
			m.confirmed = false
			m.cancelled = true
			m.done = true
			return m, tea.Quit

		case key.Matches(msg, confirmKeys.Yes):
			m.confirmed = true
			m.done = true
			return m, tea.Quit

		case key.Matches(msg, confirmKeys.No):
			m.confirmed = false
			m.done = true
			return m, tea.Quit

		case key.Matches(msg, confirmKeys.Toggle):
			m.confirmed = !m.confirmed

		case key.Matches(msg, confirmKeys.Submit):
			m.done = true
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m confirmModel) View() string {
	if m.done {
		return ""
	}

	styles := m.theme.Styles()

	var yes, no string
	if m.confirmed {
		yes = styles.Selected.Render("[Yes]")
		no = styles.Muted.Render(" No ")
	} else {
		yes = styles.Muted.Render(" Yes ")
		no = styles.Selected.Render("[No]")
	}

	// Calculate available width for message (leave room for buttons)
	buttonWidth := 12 // "[Yes]  No " is about 12 chars
	msgWidth := max(m.width-buttonWidth-1, 20)

	// Wrap message if needed
	wrappedMsg := wordwrap.String(m.message, msgWidth)

	return fmt.Sprintf("%s %s %s", wrappedMsg, yes, no)
}

// Confirm displays an interactive confirmation prompt.
// Returns true for yes, false for no.
// Falls back to Y/n prompt for non-TTY environments.
func Confirm(message string, defaultYes bool) (bool, error) {
	return ConfirmWithIO(message, defaultYes, os.Stdin, os.Stdout)
}

// ConfirmWithIO displays an interactive confirmation prompt using custom IO.
func ConfirmWithIO(message string, defaultYes bool, in io.Reader, out io.Writer) (bool, error) {
	// Fall back to simple prompt for non-TTY
	if !ui.IsStdoutTTY() || !ui.IsStdinTTY() {
		return confirmSimple(message, defaultYes, in, out)
	}

	m := newConfirmModel(message, defaultYes)
	p := tea.NewProgram(m, tea.WithOutput(out))

	result, err := p.Run()
	if err != nil {
		return false, fmt.Errorf("confirm failed: %w", err)
	}

	final := result.(confirmModel)
	if final.cancelled {
		return false, errors.New("cancelled")
	}
	return final.confirmed, nil
}

// confirmSimple provides a simple Y/n fallback for non-TTY environments.
func confirmSimple(message string, defaultYes bool, in io.Reader, out io.Writer) (bool, error) {
	hint := "(y/N)"
	if defaultYes {
		hint = "(Y/n)"
	}

	fmt.Fprintf(out, "%s %s: ", message, hint)

	// Reuse existing bufio.Reader if provided, otherwise create new one
	reader, ok := in.(*bufio.Reader)
	if !ok {
		reader = bufio.NewReader(in)
	}
	input, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(strings.ToLower(input))

	if input == "" {
		return defaultYes, nil
	}

	switch input {
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		return defaultYes, nil
	}
}
