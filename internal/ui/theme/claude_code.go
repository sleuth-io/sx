package theme

import "github.com/charmbracelet/lipgloss"

// claudeCodeTheme implements the Claude Code visual style.
// Clean, minimal aesthetic with cyan/blue accents.
type claudeCodeTheme struct {
	palette Palette
	styles  Styles
	symbols Symbols
}

// Palette is an alias for ColorPalette (for internal use).
type Palette = ColorPalette

// NewClaudeCodeTheme creates a new Claude Code theme instance.
func NewClaudeCodeTheme() Theme {
	// Use AdaptiveColor for automatic light/dark terminal support
	palette := Palette{
		Primary:   lipgloss.AdaptiveColor{Light: "#0891b2", Dark: "#22d3ee"}, // Cyan
		Secondary: lipgloss.AdaptiveColor{Light: "#2563eb", Dark: "#60a5fa"}, // Blue

		Success: lipgloss.AdaptiveColor{Light: "#16a34a", Dark: "#4ade80"}, // Green
		Error:   lipgloss.AdaptiveColor{Light: "#dc2626", Dark: "#ef4444"}, // Red
		Warning: lipgloss.AdaptiveColor{Light: "#ca8a04", Dark: "#facc15"}, // Yellow
		Info:    lipgloss.AdaptiveColor{Light: "#2563eb", Dark: "#60a5fa"}, // Blue

		Text:         lipgloss.AdaptiveColor{Light: "#1f2937", Dark: "#f9fafb"}, // Gray-800/Gray-50
		TextMuted:    lipgloss.AdaptiveColor{Light: "#6b7280", Dark: "#9ca3af"}, // Gray-500/Gray-400
		TextFaint:    lipgloss.AdaptiveColor{Light: "#9ca3af", Dark: "#6b7280"}, // Gray-400/Gray-500 (more subtle)
		TextEmphasis: lipgloss.AdaptiveColor{Light: "#111827", Dark: "#ffffff"}, // Gray-900/White

		Border:    lipgloss.AdaptiveColor{Light: "#d1d5db", Dark: "#4b5563"}, // Gray-300/Gray-600
		Highlight: lipgloss.AdaptiveColor{Light: "#e0f2fe", Dark: "#0c4a6e"}, // Sky-100/Sky-900
	}

	symbols := Symbols{
		Success:    "\u2713", // checkmark
		Error:      "\u2717", // X mark
		Warning:    "!",
		Info:       "\u2192", // arrow
		Arrow:      "\u2192", // arrow
		Bullet:     "\u2022", // bullet
		Pending:    "\u25cb", // empty circle
		InProgress: "\u25d0", // half circle
	}

	t := &claudeCodeTheme{
		palette: palette,
		symbols: symbols,
	}

	// Build styles from palette
	t.styles = Styles{
		// Message styles
		Success: lipgloss.NewStyle().
			Foreground(palette.Success).
			Bold(true),
		Error: lipgloss.NewStyle().
			Foreground(palette.Error).
			Bold(true),
		Warning: lipgloss.NewStyle().
			Foreground(palette.Warning),
		Info: lipgloss.NewStyle().
			Foreground(palette.Info),

		// Layout styles
		Header: lipgloss.NewStyle().
			Foreground(palette.TextEmphasis).
			Bold(true),
		SubHeader: lipgloss.NewStyle().
			Foreground(palette.Primary).
			Bold(true),

		// Text styles
		Bold: lipgloss.NewStyle().
			Foreground(palette.TextEmphasis).
			Bold(true),
		Muted: lipgloss.NewStyle().
			Foreground(palette.TextMuted),
		Faint: lipgloss.NewStyle().
			Foreground(palette.TextFaint),
		Emphasis: lipgloss.NewStyle().
			Foreground(palette.Primary),

		// List styles
		ListItem: lipgloss.NewStyle().
			Foreground(palette.Text),
		ListBullet: lipgloss.NewStyle().
			Foreground(palette.Primary),
		Selected: lipgloss.NewStyle().
			Foreground(palette.Primary).
			Bold(true),
		Cursor: lipgloss.NewStyle().
			Foreground(palette.Primary).
			Bold(true),

		// Key-Value styles
		Key: lipgloss.NewStyle().
			Foreground(palette.TextMuted),
		Value: lipgloss.NewStyle().
			Foreground(palette.Text),
		Separator: lipgloss.NewStyle().
			Foreground(palette.TextMuted),

		// Progress/status styles
		Spinner: lipgloss.NewStyle().
			Foreground(palette.Primary),
		Progress: lipgloss.NewStyle().
			Foreground(palette.Primary),
	}

	return t
}

func (t *claudeCodeTheme) Name() string {
	return "claude-code"
}

func (t *claudeCodeTheme) Palette() ColorPalette {
	return t.palette
}

func (t *claudeCodeTheme) Styles() Styles {
	return t.styles
}

func (t *claudeCodeTheme) Symbols() Symbols {
	return t.symbols
}
