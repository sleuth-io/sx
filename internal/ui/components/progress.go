package components

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sleuth-io/sx/internal/ui"
	"github.com/sleuth-io/sx/internal/ui/theme"
)

// progressUpdateMsg updates the progress bar state.
type progressUpdateMsg struct {
	percent     float64
	description string
}

// progressDoneMsg signals that the progress is complete.
type progressDoneMsg struct{}

// progressModel is the bubbletea model for the progress bar.
type progressModel struct {
	progress    progress.Model
	description string
	percent     float64
	done        bool
	theme       theme.Theme
	width       int
}

func newProgressModel(description string, width int) progressModel {
	th := theme.Current()
	palette := th.Palette()

	p := progress.New(
		progress.WithWidth(width),
		progress.WithoutPercentage(),
		progress.WithSolidFill(string(palette.Primary.Dark)),
	)

	// Style the progress bar
	p.FullColor = string(palette.Primary.Dark)
	p.EmptyColor = string(palette.Border.Dark)

	return progressModel{
		progress:    p,
		description: description,
		theme:       th,
		width:       width,
	}
}

func (m progressModel) Init() tea.Cmd {
	return nil
}

func (m progressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case progressUpdateMsg:
		m.percent = msg.percent
		if msg.description != "" {
			m.description = msg.description
		}
		return m, nil

	case progressDoneMsg:
		m.done = true
		return m, tea.Quit

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.done = true
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.progress.Width = min(msg.Width-4, m.width)

	case progress.FrameMsg:
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		return m, cmd
	}

	return m, nil
}

func (m progressModel) View() string {
	if m.done {
		return ""
	}

	styles := m.theme.Styles()

	var b strings.Builder

	// Description
	if m.description != "" {
		b.WriteString(styles.Muted.Render(m.description))
		b.WriteString("\n")
	}

	// Progress bar
	b.WriteString(m.progress.ViewAs(m.percent))

	// Percentage
	b.WriteString(" ")
	b.WriteString(styles.Muted.Render(fmt.Sprintf("%3.0f%%", m.percent*100)))

	return b.String()
}

// ProgressBar represents an interactive progress bar.
type ProgressBar struct {
	program     *tea.Program
	description string
	width       int
	out         io.Writer
	noTTY       bool
	mu          sync.Mutex
	lastPercent float64
}

// ProgressBarOption configures a progress bar.
type ProgressBarOption func(*ProgressBar)

// WithWidth sets the progress bar width.
func WithWidth(width int) ProgressBarOption {
	return func(p *ProgressBar) {
		p.width = width
	}
}

// NewProgressBar creates a new progress bar.
func NewProgressBar(description string, opts ...ProgressBarOption) *ProgressBar {
	return NewProgressBarWithOutput(description, os.Stdout, opts...)
}

// NewProgressBarWithOutput creates a new progress bar with custom output.
func NewProgressBarWithOutput(description string, out io.Writer, opts ...ProgressBarOption) *ProgressBar {
	pb := &ProgressBar{
		description: description,
		width:       40,
		out:         out,
		noTTY:       !ui.IsTTY(out),
	}

	for _, opt := range opts {
		opt(pb)
	}

	return pb
}

// Start begins the progress bar.
func (p *ProgressBar) Start() {
	if p.noTTY {
		fmt.Fprintf(p.out, "%s: ", p.description)
		return
	}

	m := newProgressModel(p.description, p.width)
	p.program = tea.NewProgram(m, tea.WithOutput(p.out))

	go func() {
		_, _ = p.program.Run()
	}()
}

// Update updates the progress bar to the given percentage (0.0 to 1.0).
func (p *ProgressBar) Update(percent float64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.noTTY {
		// For non-TTY, print dots at intervals
		if percent-p.lastPercent >= 0.1 {
			fmt.Fprint(p.out, ".")
			p.lastPercent = percent
		}
		return
	}

	if p.program != nil {
		p.program.Send(progressUpdateMsg{percent: percent})
	}
}

// UpdateWithDescription updates both progress and description.
func (p *ProgressBar) UpdateWithDescription(percent float64, description string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.noTTY {
		if description != p.description {
			fmt.Fprintf(p.out, "\n%s: ", description)
			p.description = description
			p.lastPercent = 0
		}
		if percent-p.lastPercent >= 0.1 {
			fmt.Fprint(p.out, ".")
			p.lastPercent = percent
		}
		return
	}

	if p.program != nil {
		p.program.Send(progressUpdateMsg{percent: percent, description: description})
	}
}

// Done completes the progress bar.
func (p *ProgressBar) Done() {
	if p.noTTY {
		fmt.Fprintln(p.out, " done")
		return
	}

	if p.program != nil {
		p.program.Send(progressDoneMsg{})
	}
}

// MultiProgress manages multiple progress bars for concurrent downloads.
type MultiProgress struct {
	out     io.Writer
	noTTY   bool
	bars    map[string]*progressItem
	mu      sync.Mutex
	total   int
	current int
}

type progressItem struct {
	description string
	percent     float64
}

// NewMultiProgress creates a manager for multiple progress items.
func NewMultiProgress(out io.Writer) *MultiProgress {
	return &MultiProgress{
		out:   out,
		noTTY: !ui.IsTTY(out),
		bars:  make(map[string]*progressItem),
	}
}

// SetTotal sets the total number of items.
func (mp *MultiProgress) SetTotal(total int) {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	mp.total = total
}

// Start marks an item as started.
func (mp *MultiProgress) Start(id, description string) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	mp.bars[id] = &progressItem{
		description: description,
		percent:     0,
	}

	if mp.noTTY {
		fmt.Fprintf(mp.out, "[%d/%d] %s...\n", mp.current+1, mp.total, description)
	}
}

// Update updates an item's progress.
func (mp *MultiProgress) Update(id string, percent float64) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if item, ok := mp.bars[id]; ok {
		item.percent = percent
	}
}

// Complete marks an item as complete.
func (mp *MultiProgress) Complete(id string) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	mp.current++
	delete(mp.bars, id)
}

// Fail marks an item as failed.
func (mp *MultiProgress) Fail(id string, err error) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if mp.noTTY {
		if item, ok := mp.bars[id]; ok {
			fmt.Fprintf(mp.out, "[%d/%d] %s: failed - %v\n", mp.current+1, mp.total, item.description, err)
		}
	}

	mp.current++
	delete(mp.bars, id)
}
