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

// MultiSelectOption represents a selectable option with a checked state.
type MultiSelectOption struct {
	Label       string
	Value       string
	Description string
	Selected    bool
}

// multiSelectModel is the bubbletea model for the multi-select component.
type multiSelectModel struct {
	title     string
	options   []MultiSelectOption
	cursor    int
	done      bool
	cancelled bool
	theme     theme.Theme
	width     int
}

// multiSelectKeyMap defines the keybindings for the multi-select component.
type multiSelectKeyMap struct {
	Up          key.Binding
	Down        key.Binding
	Toggle      key.Binding
	SelectAll   key.Binding
	DeselectAll key.Binding
	Confirm     key.Binding
	Quit        key.Binding
}

var multiSelectKeys = multiSelectKeyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Toggle: key.NewBinding(
		key.WithKeys(" "),
		key.WithHelp("space", "toggle"),
	),
	SelectAll: key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "select all"),
	),
	DeselectAll: key.NewBinding(
		key.WithKeys("n"),
		key.WithHelp("n", "deselect all"),
	),
	Confirm: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "confirm"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c", "esc"),
		key.WithHelp("q", "quit"),
	),
}

func newMultiSelectModel(title string, options []MultiSelectOption) multiSelectModel {
	// Make a copy of options to avoid modifying the original
	optsCopy := make([]MultiSelectOption, len(options))
	copy(optsCopy, options)

	return multiSelectModel{
		title:   title,
		options: optsCopy,
		cursor:  0,
		theme:   theme.Current(),
		width:   60,
	}
}

func (m multiSelectModel) Init() tea.Cmd {
	return nil
}

func (m multiSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, multiSelectKeys.Quit):
			m.cancelled = true
			m.done = true
			return m, tea.Quit

		case key.Matches(msg, multiSelectKeys.Up):
			if m.cursor > 0 {
				m.cursor--
			}

		case key.Matches(msg, multiSelectKeys.Down):
			if m.cursor < len(m.options)-1 {
				m.cursor++
			}

		case key.Matches(msg, multiSelectKeys.Toggle):
			m.options[m.cursor].Selected = !m.options[m.cursor].Selected

		case key.Matches(msg, multiSelectKeys.SelectAll):
			for i := range m.options {
				m.options[i].Selected = true
			}

		case key.Matches(msg, multiSelectKeys.DeselectAll):
			for i := range m.options {
				m.options[i].Selected = false
			}

		case key.Matches(msg, multiSelectKeys.Confirm):
			m.done = true
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
	}

	return m, nil
}

func (m multiSelectModel) View() string {
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
		// Checkbox
		checkbox := "[ ]"
		if opt.Selected {
			checkbox = "[" + sym.Success + "]"
		}

		if i == m.cursor {
			// Selected row: arrow cursor, bright label
			b.WriteString(styles.Cursor.Render(sym.Arrow + " "))
			b.WriteString(styles.Selected.Render(checkbox + " "))
			paddedLabel := opt.Label + strings.Repeat(" ", maxLabelWidth-len(opt.Label))
			b.WriteString(styles.Selected.Render(paddedLabel))
			if hasDescriptions {
				b.WriteString(styles.Faint.Render("  │  "))
				if opt.Description != "" {
					b.WriteString(styles.Muted.Render(opt.Description))
				}
			}
		} else {
			// Unselected row: indent, normal label
			b.WriteString("  ")
			b.WriteString(checkbox + " ")
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
	b.WriteString(styles.Faint.Render("↑/↓ navigate • space toggle • a all • n none • enter confirm"))

	return b.String()
}

// MultiSelect displays an interactive multi-selection menu and returns the selected options.
// Falls back to numbered menu input for non-TTY environments.
func MultiSelect(title string, options []MultiSelectOption) ([]MultiSelectOption, error) {
	return MultiSelectWithIO(title, options, os.Stdin, os.Stdout)
}

// MultiSelectWithIO displays an interactive multi-selection menu using custom IO.
func MultiSelectWithIO(title string, options []MultiSelectOption, in io.Reader, out io.Writer) ([]MultiSelectOption, error) {
	if len(options) == 0 {
		return nil, errors.New("no options provided")
	}

	// Fall back to numbered menu for non-TTY
	if !ui.IsStdoutTTY() || !ui.IsStdinTTY() {
		return multiSelectNumbered(title, options, in, out)
	}

	m := newMultiSelectModel(title, options)
	p := tea.NewProgram(m, tea.WithOutput(out))

	result, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("multi-select failed: %w", err)
	}

	final := result.(multiSelectModel)
	if final.cancelled {
		return nil, errors.New("selection cancelled")
	}
	return final.options, nil
}

// multiSelectNumbered provides a numbered fallback for non-TTY environments.
func multiSelectNumbered(title string, options []MultiSelectOption, in io.Reader, out io.Writer) ([]MultiSelectOption, error) {
	fmt.Fprintln(out, title)
	fmt.Fprintln(out)

	// Show options with current selection state
	for i, opt := range options {
		marker := "[ ]"
		if opt.Selected {
			marker = "[x]"
		}
		if opt.Description != "" {
			fmt.Fprintf(out, "  %d) %s %s - %s\n", i+1, marker, opt.Label, opt.Description)
		} else {
			fmt.Fprintf(out, "  %d) %s %s\n", i+1, marker, opt.Label)
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Enter numbers to toggle (comma-separated, e.g., '1,3'), 'all', 'none', or press Enter to confirm:")
	fmt.Fprint(out, "> ")

	reader, ok := in.(*bufio.Reader)
	if !ok {
		reader = bufio.NewReader(in)
	}
	input, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(input)

	// Make a copy of options
	result := make([]MultiSelectOption, len(options))
	copy(result, options)

	if input == "" {
		// Confirm current selection
		return result, nil
	}

	if strings.ToLower(input) == "all" {
		for i := range result {
			result[i].Selected = true
		}
		return result, nil
	}

	if strings.ToLower(input) == "none" {
		for i := range result {
			result[i].Selected = false
		}
		return result, nil
	}

	// Parse comma-separated numbers
	parts := strings.SplitSeq(input, ",")
	for part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		num, err := strconv.Atoi(part)
		if err != nil || num < 1 || num > len(result) {
			return nil, fmt.Errorf("invalid choice: %s", part)
		}
		// Toggle the option
		result[num-1].Selected = !result[num-1].Selected
	}

	return result, nil
}
