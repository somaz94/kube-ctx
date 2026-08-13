// Package render handles terminal output: colors that switch themselves off
// when the destination is not a terminal, and aligned tables.
package render

import (
	"io"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

// ANSI SGR sequences. Kept as constants rather than pulled from a color library
// so the dependency list stays at cobra + client-go + x/term.
const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiBlue   = "\033[34m"
	ansiCyan   = "\033[36m"
)

// Palette colorizes strings, or returns them untouched when color is off.
type Palette struct {
	enabled bool
}

// New decides whether color is appropriate for w.
//
// Color is suppressed when the caller asked for --no-color, when NO_COLOR is
// set (https://no-color.org), when TERM says the terminal cannot render it, or
// when w is a pipe — so `kctx list | grep` sees clean text.
func New(w io.Writer, forceNoColor bool) Palette {
	if forceNoColor {
		return Palette{}
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return Palette{}
	}
	if os.Getenv("TERM") == "dumb" {
		return Palette{}
	}
	return Palette{enabled: IsTerminal(w)}
}

// NewEnabled returns a palette with color forced on, for tests and for callers
// that already decided.
func NewEnabled(enabled bool) Palette { return Palette{enabled: enabled} }

// Enabled reports whether this palette emits escape sequences.
func (p Palette) Enabled() bool { return p.enabled }

func (p Palette) wrap(code, s string) string {
	if !p.enabled || s == "" {
		return s
	}
	return code + s + ansiReset
}

// Bold renders s in bold.
func (p Palette) Bold(s string) string { return p.wrap(ansiBold, s) }

// Dim renders s dimmed.
func (p Palette) Dim(s string) string { return p.wrap(ansiDim, s) }

// Red renders s in red.
func (p Palette) Red(s string) string { return p.wrap(ansiRed, s) }

// Green renders s in green.
func (p Palette) Green(s string) string { return p.wrap(ansiGreen, s) }

// Yellow renders s in yellow.
func (p Palette) Yellow(s string) string { return p.wrap(ansiYellow, s) }

// Blue renders s in blue.
func (p Palette) Blue(s string) string { return p.wrap(ansiBlue, s) }

// Cyan renders s in cyan.
func (p Palette) Cyan(s string) string { return p.wrap(ansiCyan, s) }

// IsTerminal reports whether w is an interactive terminal.
func IsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// ansiPattern matches SGR escape sequences so they can be excluded from width
// calculations.
var ansiPattern = regexp.MustCompile("\x1b\\[[0-9;]*m")

// VisibleWidth returns the printed width of s, ignoring color escapes.
func VisibleWidth(s string) int {
	return utf8.RuneCountInString(ansiPattern.ReplaceAllString(s, ""))
}

// Table writes headers and rows as an aligned, two-space-separated table.
//
// Columns are measured by visible width, so a cell that is already colorized
// still lines up — text/tabwriter counts the escape bytes and would push every
// following column out by that many characters.
func Table(w io.Writer, headers []string, rows [][]string) error {
	all := rows
	if len(headers) > 0 {
		all = append([][]string{headers}, rows...)
	}

	var widths []int
	for _, row := range all {
		for i, cell := range row {
			for len(widths) <= i {
				widths = append(widths, 0)
			}
			if n := VisibleWidth(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}

	var b strings.Builder
	for _, row := range all {
		var line strings.Builder
		for i, cell := range row {
			line.WriteString(cell)
			if i < len(row)-1 {
				line.WriteString(strings.Repeat(" ", widths[i]-VisibleWidth(cell)+2))
			}
		}
		// An empty trailing cell would otherwise leave the padding of the
		// column before it dangling at the end of the line.
		b.WriteString(strings.TrimRight(line.String(), " "))
		b.WriteByte('\n')
	}

	_, err := io.WriteString(w, b.String())
	return err
}
