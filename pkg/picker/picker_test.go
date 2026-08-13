package picker

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/somaz94/kube-ctx/pkg/render"
)

// newTestPicker returns a picker driven by the given keystrokes.
func newTestPicker(keys string, out io.Writer) *Picker {
	return &Picker{
		In:      strings.NewReader(keys),
		Out:     out,
		Prompt:  "❯",
		Height:  3,
		Palette: render.NewEnabled(false),
	}
}

func TestPickerSelectsWithEnter(t *testing.T) {
	var out bytes.Buffer
	p := newTestPicker("\r", &out)

	got, err := p.Run(items("dev", "prod"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != 0 {
		t.Errorf("selected %d, want 0", got)
	}
	if !strings.Contains(out.String(), "dev") {
		t.Errorf("frame did not render the items:\n%q", out.String())
	}
}

func TestPickerArrowThenEnter(t *testing.T) {
	var out bytes.Buffer
	p := newTestPicker("\x1b[B\r", &out)

	got, err := p.Run(items("dev", "prod"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != 1 {
		t.Errorf("selected %d, want 1", got)
	}
}

func TestPickerTypeToFilter(t *testing.T) {
	var out bytes.Buffer
	p := newTestPicker("prod\r", &out)

	got, err := p.Run(items("dev", "prod-eks", "staging"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != 1 {
		t.Errorf("selected %d, want 1 (prod-eks)", got)
	}
}

func TestPickerEscapeAborts(t *testing.T) {
	var out bytes.Buffer
	p := newTestPicker("\x03", &out) // Ctrl-C

	if _, err := p.Run(items("dev")); !errors.Is(err, ErrAborted) {
		t.Errorf("err = %v, want ErrAborted", err)
	}
}

func TestPickerEmptyInputAborts(t *testing.T) {
	var out bytes.Buffer
	p := newTestPicker("", &out)

	// A closed input stream means nothing chose anything.
	if _, err := p.Run(items("dev")); !errors.Is(err, ErrAborted) {
		t.Errorf("err = %v, want ErrAborted", err)
	}
}

func TestPickerNoItems(t *testing.T) {
	var out bytes.Buffer
	p := newTestPicker("\r", &out)

	if _, err := p.Run(nil); !errors.Is(err, ErrAborted) {
		t.Errorf("err = %v, want ErrAborted", err)
	}
}

func TestPickerEnterWithNoMatchIsIgnored(t *testing.T) {
	var out bytes.Buffer
	// Type something that matches nothing, press Enter (ignored), then abort.
	p := newTestPicker("zzz\r\x03", &out)

	if _, err := p.Run(items("dev", "prod")); !errors.Is(err, ErrAborted) {
		t.Errorf("err = %v, want ErrAborted", err)
	}
}

func TestPickerRunsRawModeHook(t *testing.T) {
	var out bytes.Buffer
	p := newTestPicker("\r", &out)

	entered, restored := false, false
	p.EnterRaw = func() (func() error, error) {
		entered = true
		return func() error { restored = true; return nil }, nil
	}

	if _, err := p.Run(items("dev")); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !entered || !restored {
		t.Errorf("raw mode entered=%v restored=%v; want both true", entered, restored)
	}
}

func TestPickerRawModeFailure(t *testing.T) {
	var out bytes.Buffer
	p := newTestPicker("\r", &out)
	p.EnterRaw = func() (func() error, error) { return nil, errors.New("not a terminal") }

	if _, err := p.Run(items("dev")); err == nil {
		t.Error("expected the raw-mode failure to surface")
	}
}

// errReader fails after returning nothing.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestPickerReadError(t *testing.T) {
	var out bytes.Buffer
	p := newTestPicker("", &out)
	p.In = errReader{}

	if _, err := p.Run(items("dev")); err == nil || errors.Is(err, ErrAborted) {
		t.Errorf("err = %v, want the read failure", err)
	}
}

// errWriter fails every write.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestPickerWriteError(t *testing.T) {
	p := newTestPicker("\r", errWriter{})

	if _, err := p.Run(items("dev")); err == nil {
		t.Error("expected the write failure to surface")
	}
}

func TestPickerSplitEscapeSequence(t *testing.T) {
	var out bytes.Buffer
	// A terminal can split an arrow key across reads. An incomplete CSI must
	// be held until the rest arrives rather than acted on or dropped.
	// (A lone ESC byte is deliberately *not* held: it is how the user aborts,
	// and nothing distinguishes it from a truncated sequence.)
	p := newTestPicker("", &out)
	p.In = &chunkReader{chunks: []string{"\x1b[", "B", "\r"}}

	got, err := p.Run(items("dev", "prod"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != 1 {
		t.Errorf("selected %d, want 1", got)
	}
}

// chunkReader returns one queued chunk per Read call, simulating a terminal
// that splits an escape sequence across reads.
type chunkReader struct {
	chunks []string
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if len(c.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(p, c.chunks[0])
	c.chunks = c.chunks[1:]
	return n, nil
}

func TestFrameRendersBadgesAndDetails(t *testing.T) {
	var out bytes.Buffer
	p := newTestPicker("\r", &out)
	p.Palette = render.NewEnabled(true)

	list := []Item{
		{Label: "prod-eks", Detail: "monitoring", Badge: "PROD", BadgeStyle: "danger"},
		{Label: "staging", Detail: "default", Badge: "STG", BadgeStyle: "warn"},
		{Label: "dev", Detail: "default", Badge: "plain", BadgeStyle: ""},
	}
	if _, err := p.Run(list); err != nil {
		t.Fatalf("Run: %v", err)
	}

	frame := out.String()
	for _, want := range []string{"prod-eks", "PROD", "monitoring", "STG", "3/3"} {
		if !strings.Contains(frame, want) {
			t.Errorf("frame missing %q:\n%q", want, frame)
		}
	}
}

func TestFrameHighlightsMatchedCharacters(t *testing.T) {
	m := NewModel(items("prod-eks"), 5)
	typeKeys(m, "pe")

	p := newTestPicker("", nil)
	p.Palette = render.NewEnabled(true)
	frame, lines := p.frame(m)

	if want := 1 + m.Height() + 1; lines != want {
		t.Errorf("frame lines = %d, want %d (prompt + rows + footer)", lines, want)
	}
	// With color on, matched runes are wrapped individually, so the plain
	// substring no longer appears contiguously.
	if strings.Contains(frame, "prod-eks") {
		t.Errorf("expected per-rune highlighting:\n%q", frame)
	}
	if !strings.Contains(render.NewEnabled(false).Bold(stripANSI(frame)), "prod-eks") {
		t.Errorf("label lost after stripping color:\n%q", frame)
	}
}

func TestFrameWithoutColorLeavesLabelIntact(t *testing.T) {
	m := NewModel(items("prod-eks"), 5)
	typeKeys(m, "pe")

	p := newTestPicker("", nil)
	frame, _ := p.frame(m)
	if !strings.Contains(frame, "prod-eks") {
		t.Errorf("frame missing the plain label:\n%q", frame)
	}
}

func TestFrameDefaultPrompt(t *testing.T) {
	p := newTestPicker("", nil)
	p.Prompt = ""

	frame, _ := p.frame(NewModel(items("dev"), 2))
	if !strings.Contains(frame, "❯") {
		t.Errorf("frame missing the default prompt:\n%q", frame)
	}
}

// stripANSI removes escape sequences so assertions can look at the text.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' && s[i] != 'K' && s[i] != 'A' {
				i++
			}
			i++
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
