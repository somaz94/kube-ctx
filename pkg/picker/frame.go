package picker

import (
	"fmt"
	"strings"

	"github.com/somaz94/kube-ctx/pkg/render"
)

// frame renders the current state and reports how many lines it occupies.
//
// Every line is terminated with an erase-to-end-of-line so a shorter frame
// never leaves characters from the previous, longer one behind.
func (p *Picker) frame(m *Model) (string, int) {
	pal := p.Palette
	var b strings.Builder
	lines := 0

	writeLine := func(s string) {
		b.WriteString("\r\033[2K")
		b.WriteString(s)
		b.WriteString("\n")
		lines++
	}

	prompt := p.Prompt
	if prompt == "" {
		prompt = "❯"
	}
	writeLine(pal.Cyan(prompt) + " " + m.Query() + pal.Dim("▏"))

	visible := m.Visible()
	for i, match := range visible {
		absolute := m.Offset() + i
		writeLine(p.itemLine(m, match, absolute == m.Cursor()))
	}
	// Pad to a stable height so the frame does not jump as the list shrinks.
	for i := len(visible); i < m.Height(); i++ {
		writeLine("")
	}

	writeLine(pal.Dim(fmt.Sprintf("%d/%d  ↑↓ move  ⏎ select  esc cancel",
		len(m.Matches()), len(m.Items()))))

	return b.String(), lines
}

// itemLine renders one row: selection marker, highlighted label, badge, detail.
func (p *Picker) itemLine(m *Model, match Match, selected bool) string {
	pal := p.Palette
	item := m.Items()[match.Index]

	marker := "  "
	if selected {
		marker = pal.Cyan("▸ ")
	}

	label := highlight(pal, item.Label, match.Positions)
	if selected {
		label = pal.Bold(label)
	}

	var parts []string
	parts = append(parts, marker+label)
	if item.Badge != "" {
		parts = append(parts, badge(pal, item.Badge, item.BadgeStyle))
	}
	if item.Detail != "" {
		parts = append(parts, pal.Dim(item.Detail))
	}
	return strings.Join(parts, "  ")
}

// highlight underlines the query characters inside the label.
func highlight(pal render.Palette, label string, positions []int) string {
	if len(positions) == 0 || !pal.Enabled() {
		return label
	}

	marked := make(map[int]bool, len(positions))
	for _, p := range positions {
		marked[p] = true
	}

	var b strings.Builder
	for i, r := range []rune(label) {
		if marked[i] {
			b.WriteString(pal.Yellow(string(r)))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// badge renders a short marker in the color its style calls for.
func badge(pal render.Palette, text, style string) string {
	switch style {
	case "danger":
		return pal.Red(text)
	case "warn":
		return pal.Yellow(text)
	default:
		return pal.Dim(text)
	}
}
