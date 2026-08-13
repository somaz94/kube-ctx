package picker

import (
	"testing"
)

func items(labels ...string) []Item {
	out := make([]Item, len(labels))
	for i, l := range labels {
		out[i] = Item{Label: l}
	}
	return out
}

// typeKeys feeds a string to the model one rune at a time.
func typeKeys(m *Model, s string) {
	for _, r := range s {
		m.Update(Key{Type: KeyRune, Rune: r})
	}
}

func TestModelStartsOnFirstItem(t *testing.T) {
	m := NewModel(items("dev", "prod", "staging"), 10)

	if got := len(m.Matches()); got != 3 {
		t.Fatalf("got %d matches, want 3", got)
	}
	idx, ok := m.Selected()
	if !ok || idx != 0 {
		t.Errorf("Selected = %d, %v; want 0, true", idx, ok)
	}
	if done, _ := m.Done(); done {
		t.Error("a fresh model should not be done")
	}
}

func TestModelFiltersAsYouType(t *testing.T) {
	m := NewModel(items("dev", "prod-eks", "prod-gke", "staging"), 10)

	typeKeys(m, "prod")
	if got := len(m.Matches()); got != 2 {
		t.Fatalf("got %d matches, want 2", got)
	}
	if m.Query() != "prod" {
		t.Errorf("Query = %q", m.Query())
	}

	m.Update(Key{Type: KeyBackspace})
	if m.Query() != "pro" {
		t.Errorf("after backspace Query = %q, want pro", m.Query())
	}

	m.Update(Key{Type: KeyClearLine})
	if m.Query() != "" || len(m.Matches()) != 4 {
		t.Errorf("after clear: query %q, %d matches", m.Query(), len(m.Matches()))
	}
}

func TestModelBackspaceOnEmptyQuery(t *testing.T) {
	m := NewModel(items("dev"), 10)

	m.Update(Key{Type: KeyBackspace})
	m.Update(Key{Type: KeyClearLine})
	if m.Query() != "" {
		t.Errorf("Query = %q, want empty", m.Query())
	}
}

func TestModelClearWord(t *testing.T) {
	m := NewModel(items("dev"), 10)
	typeKeys(m, "abc def")

	m.Update(Key{Type: KeyClearWord})
	if m.Query() != "abc " {
		t.Errorf("Query = %q, want %q", m.Query(), "abc ")
	}
	m.Update(Key{Type: KeyClearWord})
	if m.Query() != "" {
		t.Errorf("Query = %q, want empty", m.Query())
	}
	// A no-op clear on an empty query must not panic or refilter oddly.
	m.Update(Key{Type: KeyClearWord})
	if m.Query() != "" {
		t.Errorf("Query = %q, want empty", m.Query())
	}
}

func TestModelCursorMovement(t *testing.T) {
	m := NewModel(items("a", "b", "c"), 10)

	m.Update(Key{Type: KeyDown})
	if m.Cursor() != 1 {
		t.Errorf("Cursor = %d, want 1", m.Cursor())
	}
	m.Update(Key{Type: KeyDown})
	m.Update(Key{Type: KeyDown}) // clamps at the end
	if m.Cursor() != 2 {
		t.Errorf("Cursor = %d, want 2 (clamped)", m.Cursor())
	}
	m.Update(Key{Type: KeyUp})
	m.Update(Key{Type: KeyUp})
	m.Update(Key{Type: KeyUp}) // clamps at the start
	if m.Cursor() != 0 {
		t.Errorf("Cursor = %d, want 0 (clamped)", m.Cursor())
	}
}

func TestModelScrolling(t *testing.T) {
	m := NewModel(items("a", "b", "c", "d", "e"), 3)

	for i := 0; i < 3; i++ {
		m.Update(Key{Type: KeyDown})
	}
	if m.Cursor() != 3 {
		t.Fatalf("Cursor = %d, want 3", m.Cursor())
	}
	if m.Offset() != 1 {
		t.Errorf("Offset = %d, want 1 — the window should follow the cursor", m.Offset())
	}
	if got := len(m.Visible()); got != 3 {
		t.Errorf("visible rows = %d, want 3", got)
	}

	m.Update(Key{Type: KeyPageUp})
	if m.Cursor() != 0 || m.Offset() != 0 {
		t.Errorf("after page up: cursor %d, offset %d", m.Cursor(), m.Offset())
	}
	m.Update(Key{Type: KeyPageDown})
	if m.Cursor() != 3 {
		t.Errorf("after page down: cursor %d, want 3", m.Cursor())
	}
}

func TestModelFilterResetsCursor(t *testing.T) {
	m := NewModel(items("dev", "prod", "staging"), 10)

	m.Update(Key{Type: KeyDown})
	typeKeys(m, "s")
	if m.Cursor() != 0 {
		t.Errorf("Cursor = %d; a new query must reset the cursor", m.Cursor())
	}
}

func TestModelEnterAccepts(t *testing.T) {
	m := NewModel(items("dev", "prod"), 10)

	m.Update(Key{Type: KeyDown})
	m.Update(Key{Type: KeyEnter})

	done, accepted := m.Done()
	if !done || !accepted {
		t.Fatalf("Done = %v, %v; want true, true", done, accepted)
	}
	idx, ok := m.Selected()
	if !ok || idx != 1 {
		t.Errorf("Selected = %d, %v; want 1, true", idx, ok)
	}
}

func TestModelEscapeAborts(t *testing.T) {
	m := NewModel(items("dev"), 10)
	m.Update(Key{Type: KeyEscape})

	done, accepted := m.Done()
	if !done || accepted {
		t.Errorf("Done = %v, %v; want true, false", done, accepted)
	}
}

func TestModelEnterWithNoMatchesDoesNothing(t *testing.T) {
	m := NewModel(items("dev", "prod"), 10)
	typeKeys(m, "zzzz")

	if len(m.Matches()) != 0 {
		t.Fatalf("got %d matches, want none", len(m.Matches()))
	}
	m.Update(Key{Type: KeyEnter})
	if done, _ := m.Done(); done {
		t.Error("Enter with nothing matching should not end the session")
	}
	if _, ok := m.Selected(); ok {
		t.Error("Selected should report false with no matches")
	}
	if m.Visible() != nil {
		t.Error("Visible should be empty with no matches")
	}

	// Movement with an empty match list must be a no-op, not a panic.
	m.Update(Key{Type: KeyDown})
	if m.Cursor() != 0 {
		t.Errorf("Cursor = %d, want 0", m.Cursor())
	}
}

func TestModelSelectedFollowsRanking(t *testing.T) {
	m := NewModel(items("my-prod-old", "prod"), 10)
	typeKeys(m, "prod")

	idx, ok := m.Selected()
	if !ok {
		t.Fatal("expected a selection")
	}
	if m.Items()[idx].Label != "prod" {
		t.Errorf("top match = %q, want prod", m.Items()[idx].Label)
	}
}

func TestModelIgnoresUnknownKeys(t *testing.T) {
	m := NewModel(items("dev"), 10)
	before := m.Query()

	m.Update(Key{Type: KeyIgnore})
	if m.Query() != before {
		t.Error("an ignored key changed the query")
	}
	if done, _ := m.Done(); done {
		t.Error("an ignored key ended the session")
	}
}

func TestNewModelClampsHeight(t *testing.T) {
	if got := NewModel(items("a"), 0).Height(); got != 1 {
		t.Errorf("Height = %d, want 1", got)
	}
}

func TestTrimLastWord(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"one", ""},
		{"one ", ""},
		{"one two", "one "},
		{"one two  ", "one "},
	}
	for _, tt := range tests {
		if got := trimLastWord(tt.in); got != tt.want {
			t.Errorf("trimLastWord(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
