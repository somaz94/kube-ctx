package render

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestNewDisablesColorForNonTerminal(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	os.Unsetenv("NO_COLOR")

	if New(&bytes.Buffer{}, false).Enabled() {
		t.Error("color enabled while writing to a buffer")
	}
}

func TestNewRespectsOptOuts(t *testing.T) {
	tests := []struct {
		name    string
		noColor bool
		env     map[string]string
	}{
		{"--no-color flag", true, nil},
		{"NO_COLOR set", false, map[string]string{"NO_COLOR": "1"}},
		{"NO_COLOR empty still counts", false, map[string]string{"NO_COLOR": ""}},
		{"dumb terminal", false, map[string]string{"TERM": "dumb"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TERM", "xterm-256color")
			os.Unsetenv("NO_COLOR")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			// os.Stdout is used because it is the realistic "could be a
			// terminal" case; the opt-outs must win regardless.
			if New(os.Stdout, tt.noColor).Enabled() {
				t.Error("color should be disabled")
			}
		})
	}
}

func TestPaletteWrapsOnlyWhenEnabled(t *testing.T) {
	on := NewEnabled(true)
	off := NewEnabled(false)

	fns := map[string]func(Palette, string) string{
		"Bold":   Palette.Bold,
		"Dim":    Palette.Dim,
		"Red":    Palette.Red,
		"Green":  Palette.Green,
		"Yellow": Palette.Yellow,
		"Blue":   Palette.Blue,
		"Cyan":   Palette.Cyan,
	}
	for name, fn := range fns {
		t.Run(name, func(t *testing.T) {
			got := fn(on, "x")
			if !strings.HasPrefix(got, "\033[") || !strings.HasSuffix(got, ansiReset) {
				t.Errorf("enabled %s = %q, want an escape-wrapped string", name, got)
			}
			if got := fn(off, "x"); got != "x" {
				t.Errorf("disabled %s = %q, want plain x", name, got)
			}
			if got := fn(on, ""); got != "" {
				t.Errorf("%s on an empty string = %q, want empty", name, got)
			}
		})
	}
}

func TestIsTerminal(t *testing.T) {
	if IsTerminal(&bytes.Buffer{}) {
		t.Error("a buffer is not a terminal")
	}
	// A regular file has a valid fd but is not a terminal.
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	defer f.Close()
	if IsTerminal(f) {
		t.Error("a regular file is not a terminal")
	}
}

func TestTable(t *testing.T) {
	var buf bytes.Buffer
	err := Table(&buf, []string{"NAME", "NAMESPACE"}, [][]string{
		{"dev", "default"},
		{"production-cluster", "monitoring"},
	})
	if err != nil {
		t.Fatalf("Table: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3:\n%s", len(lines), buf.String())
	}
	// Every row's second column must start at the same offset.
	want := strings.Index(lines[0], "NAMESPACE")
	for _, line := range lines[1:] {
		if got := strings.Index(line, strings.Fields(line)[1]); got != want {
			t.Errorf("column not aligned in %q: got %d, want %d", line, got, want)
		}
	}
	for _, line := range lines {
		if strings.HasSuffix(line, " ") {
			t.Errorf("line has trailing whitespace: %q", line)
		}
	}
}

func TestTableAlignsColorizedCells(t *testing.T) {
	pal := NewEnabled(true)

	var buf bytes.Buffer
	err := Table(&buf, []string{"NAME", "NAMESPACE"}, [][]string{
		{pal.Bold("dev"), "default"},
		{"production-cluster", "monitoring"},
	})
	if err != nil {
		t.Fatalf("Table: %v", err)
	}

	// Escape sequences must not count toward the column width: stripping them
	// has to leave the same alignment a plain table would have had.
	plain := ansiPattern.ReplaceAllString(buf.String(), "")
	lines := strings.Split(strings.TrimRight(plain, "\n"), "\n")
	want := strings.Index(lines[0], "NAMESPACE")
	for _, line := range lines[1:] {
		if got := strings.Index(line, strings.Fields(line)[1]); got != want {
			t.Errorf("colorized row not aligned in %q: got %d, want %d", line, got, want)
		}
	}
}

func TestVisibleWidth(t *testing.T) {
	pal := NewEnabled(true)
	tests := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{pal.Bold("abc"), 3},
		{pal.Red(pal.Bold("ab")), 2},
	}
	for _, tt := range tests {
		if got := VisibleWidth(tt.in); got != tt.want {
			t.Errorf("VisibleWidth(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestTableTrimsEmptyTrailingCells(t *testing.T) {
	var buf bytes.Buffer
	err := Table(&buf, []string{"NAME", "NOTES"}, [][]string{
		{"dev", ""},
		{"production", "unreachable"},
	})
	if err != nil {
		t.Fatalf("Table: %v", err)
	}
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if strings.HasSuffix(line, " ") {
			t.Errorf("line has trailing whitespace: %q", line)
		}
	}
}

func TestTableRaggedRows(t *testing.T) {
	var buf bytes.Buffer
	// A row longer than the header must widen the table rather than panic.
	if err := Table(&buf, []string{"A"}, [][]string{{"a", "b", "c"}}); err != nil {
		t.Fatalf("Table: %v", err)
	}
	if !strings.Contains(buf.String(), "a  b  c") {
		t.Errorf("unexpected output:\n%s", buf.String())
	}
}

func TestTableWithoutHeaders(t *testing.T) {
	var buf bytes.Buffer
	if err := Table(&buf, nil, [][]string{{"a", "b"}}); err != nil {
		t.Fatalf("Table: %v", err)
	}
	if got := strings.Count(buf.String(), "\n"); got != 1 {
		t.Errorf("got %d lines, want 1", got)
	}
}

// errWriter fails every write, standing in for a closed pipe.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }

func TestTableSurfacesWriteErrors(t *testing.T) {
	if err := Table(errWriter{}, []string{"A"}, [][]string{{"a"}}); err == nil {
		t.Error("expected a write error")
	}
}
