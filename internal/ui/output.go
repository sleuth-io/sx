package ui

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/muesli/reflow/wordwrap"
	"golang.org/x/term"

	"github.com/sleuth-io/sx/internal/ui/theme"
)

// Output provides styled terminal output.
type Output struct {
	out    io.Writer
	err    io.Writer
	theme  theme.Theme
	silent bool
	noTTY  bool
	width  int
}

// NewOutput creates a new styled output instance.
func NewOutput(out, err io.Writer) *Output {
	width := 80 // default
	if w, _, e := term.GetSize(int(os.Stdout.Fd())); e == nil && w > 0 {
		width = w
	}
	return &Output{
		out:   out,
		err:   err,
		theme: theme.Current(),
		noTTY: !IsTTY(out) || NoColor(),
		width: width,
	}
}

// Width returns the terminal width.
func (o *Output) Width() int {
	return o.width
}

// Wrap wraps text to fit the terminal width.
func (o *Output) Wrap(text string) string {
	if o.width <= 0 {
		return text
	}
	return wordwrap.String(text, o.width)
}

// SetSilent enables or disables silent mode (suppresses stdout).
func (o *Output) SetSilent(silent bool) {
	o.silent = silent
}

// IsSilent returns whether silent mode is enabled.
func (o *Output) IsSilent() bool {
	return o.silent
}

// render applies styling if TTY is available, otherwise returns plain text.
func (o *Output) render(s fmt.Stringer, text string) string {
	if o.noTTY {
		return text
	}
	return s.String()
}

// Success prints a success message with checkmark.
func (o *Output) Success(msg string) {
	if o.silent {
		return
	}
	sym := o.theme.Symbols().Success
	style := o.theme.Styles().Success
	styled := style.Render(sym + " " + msg)
	fmt.Fprintln(o.out, o.render(style.SetString(sym+" "+msg), sym+" "+msg))
	_ = styled // avoid unused
}

// Error prints an error message with X mark to stderr.
func (o *Output) Error(msg string) {
	sym := o.theme.Symbols().Error
	style := o.theme.Styles().Error
	text := sym + " " + msg
	if o.noTTY {
		fmt.Fprintln(o.err, text)
	} else {
		fmt.Fprintln(o.err, style.Render(text))
	}
}

// Warning prints a warning message to stderr.
func (o *Output) Warning(msg string) {
	sym := o.theme.Symbols().Warning
	style := o.theme.Styles().Warning
	text := sym + " " + msg
	if o.noTTY {
		fmt.Fprintln(o.err, text)
	} else {
		fmt.Fprintln(o.err, style.Render(text))
	}
}

// Info prints an info message with arrow.
func (o *Output) Info(msg string) {
	if o.silent {
		return
	}
	sym := o.theme.Symbols().Info
	style := o.theme.Styles().Info
	text := sym + " " + msg
	if o.noTTY {
		fmt.Fprintln(o.out, text)
	} else {
		fmt.Fprintln(o.out, style.Render(text))
	}
}

// Header prints a bold header.
func (o *Output) Header(text string) {
	if o.silent {
		return
	}
	style := o.theme.Styles().Header
	if o.noTTY {
		fmt.Fprintln(o.out, text)
	} else {
		fmt.Fprintln(o.out, style.Render(text))
	}
}

// SubHeader prints a styled sub-header.
func (o *Output) SubHeader(text string) {
	if o.silent {
		return
	}
	style := o.theme.Styles().SubHeader
	if o.noTTY {
		fmt.Fprintln(o.out, text)
	} else {
		fmt.Fprintln(o.out, style.Render(text))
	}
}

// Println prints a line to stdout.
func (o *Output) Println(args ...any) {
	if o.silent {
		return
	}
	fmt.Fprintln(o.out, args...)
}

// Printf prints formatted output to stdout.
func (o *Output) Printf(format string, args ...any) {
	if o.silent {
		return
	}
	fmt.Fprintf(o.out, format, args...)
}

// PrintlnAlways prints even in silent mode.
func (o *Output) PrintlnAlways(args ...any) {
	fmt.Fprintln(o.out, args...)
}

// Muted prints muted/dim text.
func (o *Output) Muted(msg string) {
	if o.silent {
		return
	}
	style := o.theme.Styles().Muted
	if o.noTTY {
		fmt.Fprintln(o.out, msg)
	} else {
		fmt.Fprintln(o.out, style.Render(msg))
	}
}

// Bold prints bold text.
func (o *Output) Bold(msg string) {
	if o.silent {
		return
	}
	style := o.theme.Styles().Bold
	if o.noTTY {
		fmt.Fprintln(o.out, msg)
	} else {
		fmt.Fprintln(o.out, style.Render(msg))
	}
}

// Emphasis prints emphasized text (primary color).
func (o *Output) Emphasis(msg string) {
	if o.silent {
		return
	}
	style := o.theme.Styles().Emphasis
	if o.noTTY {
		fmt.Fprintln(o.out, msg)
	} else {
		fmt.Fprintln(o.out, style.Render(msg))
	}
}

// KeyValue prints a key-value pair.
func (o *Output) KeyValue(key, value string) {
	if o.silent {
		return
	}
	styles := o.theme.Styles()
	if o.noTTY {
		fmt.Fprintf(o.out, "%s: %s\n", key, value)
	} else {
		fmt.Fprintln(o.out, styles.Key.Render(key+":")+
			" "+styles.Value.Render(value))
	}
}

// List prints a bulleted list.
func (o *Output) List(items []string) {
	if o.silent {
		return
	}
	styles := o.theme.Styles()
	sym := o.theme.Symbols().Bullet
	for _, item := range items {
		if o.noTTY {
			fmt.Fprintf(o.out, "  %s %s\n", sym, item)
		} else {
			fmt.Fprintf(o.out, "  %s %s\n",
				styles.ListBullet.Render(sym), item)
		}
	}
}

// ListItem prints a single list item with custom prefix.
func (o *Output) ListItem(prefix, item string) {
	if o.silent {
		return
	}
	styles := o.theme.Styles()
	if o.noTTY {
		fmt.Fprintf(o.out, "  %s %s\n", prefix, item)
	} else {
		fmt.Fprintf(o.out, "  %s %s\n",
			styles.ListBullet.Render(prefix), item)
	}
}

// SuccessItem prints a success list item.
func (o *Output) SuccessItem(item string) {
	if o.silent {
		return
	}
	sym := o.theme.Symbols().Success
	styles := o.theme.Styles()
	if o.noTTY {
		fmt.Fprintf(o.out, "  %s %s\n", sym, item)
	} else {
		fmt.Fprintf(o.out, "  %s %s\n",
			styles.Success.Render(sym), item)
	}
}

// ErrorItem prints an error list item to stderr.
func (o *Output) ErrorItem(item string) {
	sym := o.theme.Symbols().Error
	styles := o.theme.Styles()
	if o.noTTY {
		fmt.Fprintf(o.err, "  %s %s\n", sym, item)
	} else {
		fmt.Fprintf(o.err, "  %s %s\n",
			styles.Error.Render(sym), item)
	}
}

// Section prints a section header with underline.
func (o *Output) Section(title string) {
	if o.silent {
		return
	}
	styles := o.theme.Styles()
	if o.noTTY {
		fmt.Fprintln(o.out, title)
		fmt.Fprintln(o.out, strings.Repeat("-", len(title)))
	} else {
		fmt.Fprintln(o.out, styles.SubHeader.Render(title))
		fmt.Fprintln(o.out, styles.Muted.Render(strings.Repeat("─", len(title))))
	}
}

// Newline prints an empty line.
func (o *Output) Newline() {
	if o.silent {
		return
	}
	fmt.Fprintln(o.out)
}

// StyledText returns styled text without printing.
func (o *Output) StyledText(style func(theme.Styles) fmt.Stringer, text string) string {
	if o.noTTY {
		return text
	}
	return style(o.theme.Styles()).String()
}

// BoldText returns bold-styled text.
func (o *Output) BoldText(text string) string {
	if o.noTTY {
		return text
	}
	return o.theme.Styles().Bold.Render(text)
}

// MutedText returns muted-styled text.
func (o *Output) MutedText(text string) string {
	if o.noTTY {
		return text
	}
	return o.theme.Styles().Muted.Render(text)
}

// EmphasisText returns emphasis-styled text.
func (o *Output) EmphasisText(text string) string {
	if o.noTTY {
		return text
	}
	return o.theme.Styles().Emphasis.Render(text)
}

// SuccessText returns success-styled text.
func (o *Output) SuccessText(text string) string {
	if o.noTTY {
		return text
	}
	return o.theme.Styles().Success.Render(text)
}

// ErrorText returns error-styled text.
func (o *Output) ErrorText(text string) string {
	if o.noTTY {
		return text
	}
	return o.theme.Styles().Error.Render(text)
}

// Theme returns the current theme.
func (o *Output) Theme() theme.Theme {
	return o.theme
}
