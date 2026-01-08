package components

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/theme"
)

// spinnerDoneMsg signals that the spinner task is complete.
type spinnerDoneMsg struct {
	err error
}

// spinnerModel is the bubbletea model for the spinner component.
type spinnerModel struct {
	spinner spinner.Model
	message string
	done    bool
	err     error
	theme   theme.Theme
}

func newSpinnerModel(message string) spinnerModel {
	th := theme.Current()

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = th.Styles().Spinner

	return spinnerModel{
		spinner: s,
		message: message,
		theme:   th,
	}
}

func (m spinnerModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinnerDoneMsg:
		m.done = true
		m.err = msg.err
		return m, tea.Quit

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.done = true
			m.err = errors.New("cancelled")
			return m, tea.Quit
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m spinnerModel) View() string {
	if m.done {
		return ""
	}

	styles := m.theme.Styles()
	return m.spinner.View() + " " + styles.Muted.Render(m.message)
}

// Spinner displays a spinner with a message.
// The spinner runs until Stop() is called on the returned Spinner.
type Spinner struct {
	program *tea.Program
	model   spinnerModel
	out     io.Writer
	noTTY   bool
}

// NewSpinner creates a new spinner with the given message.
func NewSpinner(message string) *Spinner {
	return NewSpinnerWithOutput(message, os.Stdout)
}

// NewSpinnerWithOutput creates a new spinner with custom output.
func NewSpinnerWithOutput(message string, out io.Writer) *Spinner {
	noTTY := !ui.IsTTY(out)

	return &Spinner{
		model: newSpinnerModel(message),
		out:   out,
		noTTY: noTTY,
	}
}

// Start begins the spinner animation.
func (s *Spinner) Start() {
	if s.noTTY {
		fmt.Fprintf(s.out, "%s...\n", s.model.message)
		return
	}

	s.program = tea.NewProgram(s.model, tea.WithOutput(s.out))
	go func() {
		_, _ = s.program.Run()
	}()

	// Give the program time to start
	time.Sleep(10 * time.Millisecond)
}

// Stop stops the spinner and optionally shows a result message.
func (s *Spinner) Stop() {
	if s.noTTY || s.program == nil {
		return
	}

	s.program.Send(spinnerDoneMsg{})
	// Give the program time to quit
	time.Sleep(10 * time.Millisecond)
}

// StopWithError stops the spinner and records an error.
func (s *Spinner) StopWithError(err error) {
	if s.noTTY || s.program == nil {
		return
	}

	s.program.Send(spinnerDoneMsg{err: err})
	time.Sleep(10 * time.Millisecond)
}

// RunWithSpinner runs a function while showing a spinner.
// Returns the function's result and any error.
func RunWithSpinner[T any](message string, fn func() (T, error)) (T, error) {
	return RunWithSpinnerOutput(message, os.Stdout, fn)
}

// RunWithSpinnerOutput runs a function while showing a spinner on custom output.
func RunWithSpinnerOutput[T any](message string, out io.Writer, fn func() (T, error)) (T, error) {
	var result T
	var fnErr error

	// For non-TTY, just run the function
	if !ui.IsTTY(out) {
		fmt.Fprintf(out, "%s...\n", message)
		return fn()
	}

	m := newSpinnerModel(message)
	p := tea.NewProgram(m, tea.WithOutput(out))

	// Run the function in a goroutine
	go func() {
		result, fnErr = fn()
		p.Send(spinnerDoneMsg{err: fnErr})
	}()

	// Run the spinner until done
	if _, err := p.Run(); err != nil {
		return result, fmt.Errorf("spinner failed: %w", err)
	}

	return result, fnErr
}

// SpinnerMessage updates the spinner message (for long-running operations).
type spinnerMsgUpdate struct {
	message string
}

// UpdateMessage sends a message update to the spinner.
func (s *Spinner) UpdateMessage(message string) {
	if s.noTTY || s.program == nil {
		fmt.Fprintf(s.out, "%s...\n", message)
		return
	}
	s.program.Send(spinnerMsgUpdate{message: message})
}
