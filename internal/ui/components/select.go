// Package components provides interactive UI components for the Skills CLI.
package components

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/theme"
)

// Option represents a selectable option.
type Option struct {
	Label       string
	Value       string
	Description string
}

// selectModel is the bubbletea model for the select component.
type selectModel struct {
	title    string
	options  []Option
	cursor   int
	selected int
	done     bool
	theme    theme.Theme
	width    int
}

// selectKeyMap defines the keybindings for the select component.
type selectKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Select key.Binding
	Quit   key.Binding
}

var selectKeys = selectKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Select: key.NewBinding(
		key.WithKeys("enter", " "),
		key.WithHelp("enter", "select"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c", "esc"),
		key.WithHelp("q", "quit"),
	),
}

func newSelectModel(title string, options []Option) selectModel {
	return selectModel{
		title:    title,
		options:  options,
		cursor:   0,
		selected: -1,
		theme:    theme.Current(),
		width:    60,
	}
}

func (m selectModel) Init() tea.Cmd {
	return nil
}

func (m selectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, selectKeys.Quit):
			m.selected = -1
			m.done = true
			return m, tea.Quit

		case key.Matches(msg, selectKeys.Up):
			if m.cursor > 0 {
				m.cursor--
			}

		case key.Matches(msg, selectKeys.Down):
			if m.cursor < len(m.options)-1 {
				m.cursor++
			}

		case key.Matches(msg, selectKeys.Select):
			m.selected = m.cursor
			m.done = true
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
	}

	return m, nil
}

func (m selectModel) View() string {
	if m.done {
		return ""
	}

	styles := m.theme.Styles()
	sym := m.theme.Symbols()

	var b strings.Builder

	// Title
	b.WriteString(styles.Header.Render(m.title))
	b.WriteString("\n\n")

	// Calculate max label width for column alignment
	maxLabelWidth := 0
	for _, opt := range m.options {
		if len(opt.Label) > maxLabelWidth {
			maxLabelWidth = len(opt.Label)
		}
	}

	// Check if any options have descriptions
	hasDescriptions := false
	for _, opt := range m.options {
		if opt.Description != "" {
			hasDescriptions = true
			break
		}
	}

	// Options
	for i, opt := range m.options {
		if i == m.cursor {
			// Selected row: arrow cursor, bright label, separator, muted description
			b.WriteString(styles.Cursor.Render(sym.Arrow + " "))
			paddedLabel := opt.Label + strings.Repeat(" ", maxLabelWidth-len(opt.Label))
			b.WriteString(styles.Selected.Render(paddedLabel))
			if hasDescriptions {
				b.WriteString(styles.Faint.Render("  │  "))
				if opt.Description != "" {
					b.WriteString(styles.Muted.Render(opt.Description))
				}
			}
		} else {
			// Unselected row: indent, normal label, separator, muted description
			b.WriteString("  ")
			paddedLabel := opt.Label + strings.Repeat(" ", maxLabelWidth-len(opt.Label))
			b.WriteString(paddedLabel)
			if hasDescriptions {
				b.WriteString(styles.Faint.Render("  │  "))
				if opt.Description != "" {
					b.WriteString(styles.Muted.Render(opt.Description))
				}
			}
		}
		b.WriteString("\n")
	}

	// Help
	b.WriteString("\n")
	b.WriteString(styles.Faint.Render("↑/↓ navigate • enter select"))

	return b.String()
}

// Select displays an interactive selection menu and returns the selected option.
// Falls back to numbered menu input for non-TTY environments.
func Select(title string, options []Option) (*Option, error) {
	return SelectWithIO(title, options, os.Stdin, os.Stdout)
}

// SelectWithIO displays an interactive selection menu using custom IO.
func SelectWithIO(title string, options []Option, in io.Reader, out io.Writer) (*Option, error) {
	if len(options) == 0 {
		return nil, errors.New("no options provided")
	}

	// Fall back to numbered menu for non-TTY
	if !ui.IsStdoutTTY() || !ui.IsStdinTTY() {
		return selectNumbered(title, options, in, out)
	}

	m := newSelectModel(title, options)
	p := tea.NewProgram(m, tea.WithOutput(out))

	result, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("select failed: %w", err)
	}

	final := result.(selectModel)
	if final.selected < 0 {
		return nil, errors.New("selection cancelled")
	}

	return &options[final.selected], nil
}

// selectNumbered provides a numbered fallback for non-TTY environments.
func selectNumbered(title string, options []Option, in io.Reader, out io.Writer) (*Option, error) {
	fmt.Fprintln(out, title)
	fmt.Fprintln(out)

	for i, opt := range options {
		if opt.Description != "" {
			fmt.Fprintf(out, "  %d) %s - %s\n", i+1, opt.Label, opt.Description)
		} else {
			fmt.Fprintf(out, "  %d) %s\n", i+1, opt.Label)
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "Enter choice [1-%d]: ", len(options))

	// Reuse existing bufio.Reader if provided, otherwise create new one
	reader, ok := in.(*bufio.Reader)
	if !ok {
		reader = bufio.NewReader(in)
	}
	input, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(input)
	if input == "" {
		// Default to first option
		return &options[0], nil
	}

	choice, err := strconv.Atoi(input)
	if err != nil || choice < 1 || choice > len(options) {
		return nil, fmt.Errorf("invalid choice: %s", input)
	}

	return &options[choice-1], nil
}

// SelectWithDefault selects with a default option highlighted.
func SelectWithDefault(title string, options []Option, defaultIndex int) (*Option, error) {
	return SelectWithDefaultAndIO(title, options, defaultIndex, os.Stdin, os.Stdout)
}

// SelectWithDefaultAndIO selects with a default option highlighted using custom IO.
func SelectWithDefaultAndIO(title string, options []Option, defaultIndex int, in io.Reader, out io.Writer) (*Option, error) {
	if len(options) == 0 {
		return nil, errors.New("no options provided")
	}

	if defaultIndex < 0 || defaultIndex >= len(options) {
		defaultIndex = 0
	}

	// Fall back to numbered menu for non-TTY
	if !ui.IsStdoutTTY() || !ui.IsStdinTTY() {
		return selectNumberedWithDefault(title, options, defaultIndex, in, out)
	}

	m := newSelectModel(title, options)
	m.cursor = defaultIndex

	p := tea.NewProgram(m, tea.WithOutput(out))

	result, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("select failed: %w", err)
	}

	final := result.(selectModel)
	if final.selected < 0 {
		return nil, errors.New("selection cancelled")
	}

	return &options[final.selected], nil
}

// selectNumberedWithDefault provides a numbered fallback with default.
func selectNumberedWithDefault(title string, options []Option, defaultIndex int, in io.Reader, out io.Writer) (*Option, error) {
	fmt.Fprintln(out, title)
	fmt.Fprintln(out)

	for i, opt := range options {
		marker := " "
		if i == defaultIndex {
			marker = "*"
		}
		if opt.Description != "" {
			fmt.Fprintf(out, " %s%d) %s - %s\n", marker, i+1, opt.Label, opt.Description)
		} else {
			fmt.Fprintf(out, " %s%d) %s\n", marker, i+1, opt.Label)
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "Enter choice [1-%d, default=%d]: ", len(options), defaultIndex+1)

	// Reuse existing bufio.Reader if provided, otherwise create new one
	reader, ok := in.(*bufio.Reader)
	if !ok {
		reader = bufio.NewReader(in)
	}
	input, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return &options[defaultIndex], nil
	}

	choice, err := strconv.Atoi(input)
	if err != nil || choice < 1 || choice > len(options) {
		return nil, fmt.Errorf("invalid choice: %s", input)
	}

	return &options[choice-1], nil
}
