package picker

import "strings"

// Item is one selectable row.
type Item struct {
	// Label is the primary text and the only thing the query matches against.
	Label string
	// Detail is secondary text shown to the right, such as a namespace.
	Detail string
	// Badge is a short marker, such as a guard level.
	Badge string
	// BadgeStyle names the color to render the badge in: "danger", "warn", or
	// empty for none.
	BadgeStyle string
}

// Model is the picker's state machine. It holds no I/O, so a test can drive a
// whole session by feeding it keys.
type Model struct {
	items   []Item
	labels  []string
	query   string
	matches []Match

	cursor int // index into matches
	offset int // first visible match
	height int // visible rows

	accepted bool
	aborted  bool
}

// NewModel builds a model over items showing at most height rows at a time.
func NewModel(items []Item, height int) *Model {
	if height < 1 {
		height = 1
	}
	labels := make([]string, len(items))
	for i, item := range items {
		labels[i] = item.Label
	}

	m := &Model{items: items, labels: labels, height: height}
	m.refilter()
	return m
}

// Query returns the current search text.
func (m *Model) Query() string { return m.query }

// Matches returns the currently visible-eligible matches, best first.
func (m *Model) Matches() []Match { return m.matches }

// Cursor returns the index into Matches of the highlighted row.
func (m *Model) Cursor() int { return m.cursor }

// Offset returns the index of the first rendered match.
func (m *Model) Offset() int { return m.offset }

// Height returns how many rows are rendered at once.
func (m *Model) Height() int { return m.height }

// Items returns the full item list.
func (m *Model) Items() []Item { return m.items }

// Done reports whether the session ended, and how: accepted is false when the
// user aborted.
func (m *Model) Done() (done, accepted bool) {
	return m.accepted || m.aborted, m.accepted
}

// Selected returns the index into the original item slice of the highlighted
// row. It reports false when nothing matches the query.
func (m *Model) Selected() (int, bool) {
	if len(m.matches) == 0 || m.cursor >= len(m.matches) {
		return 0, false
	}
	return m.matches[m.cursor].Index, true
}

// Update applies one keystroke.
func (m *Model) Update(k Key) {
	switch k.Type {
	case KeyRune:
		m.query += string(k.Rune)
		m.refilter()
	case KeyBackspace:
		if r := []rune(m.query); len(r) > 0 {
			m.query = string(r[:len(r)-1])
			m.refilter()
		}
	case KeyClearLine:
		if m.query != "" {
			m.query = ""
			m.refilter()
		}
	case KeyClearWord:
		if trimmed := trimLastWord(m.query); trimmed != m.query {
			m.query = trimmed
			m.refilter()
		}
	case KeyUp:
		m.move(-1)
	case KeyDown:
		m.move(1)
	case KeyPageUp:
		m.move(-m.height)
	case KeyPageDown:
		m.move(m.height)
	case KeyEnter:
		if len(m.matches) > 0 {
			m.accepted = true
		}
	case KeyEscape:
		m.aborted = true
	}
}

// move shifts the cursor by delta, clamping at both ends, and scrolls the
// window to keep the cursor visible.
func (m *Model) move(delta int) {
	if len(m.matches) == 0 {
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > len(m.matches)-1 {
		m.cursor = len(m.matches) - 1
	}
	m.scrollToCursor()
}

// scrollToCursor adjusts the visible window so the cursor is inside it.
func (m *Model) scrollToCursor() {
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+m.height {
		m.offset = m.cursor - m.height + 1
	}
	if maxOffset := len(m.matches) - m.height; m.offset > maxOffset {
		m.offset = max(maxOffset, 0)
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// refilter recomputes the match list after a query change, keeping the cursor
// at the top since the ranking changed underneath it.
func (m *Model) refilter() {
	m.matches = Filter(m.query, m.labels)
	m.cursor = 0
	m.offset = 0
}

// Visible returns the matches currently on screen.
func (m *Model) Visible() []Match {
	end := min(m.offset+m.height, len(m.matches))
	if m.offset >= end {
		return nil
	}
	return m.matches[m.offset:end]
}

// trimLastWord removes the trailing word and any whitespace before it.
func trimLastWord(s string) string {
	s = strings.TrimRight(s, " ")
	if i := strings.LastIndex(s, " "); i >= 0 {
		return s[:i+1]
	}
	return ""
}
