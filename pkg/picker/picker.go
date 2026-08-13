package picker

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/somaz94/kube-ctx/pkg/render"
)

// ErrAborted is returned when the user cancels the picker.
var ErrAborted = errors.New("selection aborted")

// ErrNoTTY is returned when no interactive terminal is available, which is the
// signal for callers to fall back to plain listing.
var ErrNoTTY = errors.New("no interactive terminal available")

// defaultHeight is how many rows the picker shows when the terminal height is
// unknown.
const defaultHeight = 10

// readChunk is the read buffer size. One keystroke is at most a few bytes, but
// a paste or a fast key repeat delivers many at once.
const readChunk = 64

// Picker runs an interactive selection over a terminal.
//
// It is deliberately constructed from plain io.Reader/io.Writer plus an
// optional raw-mode hook, so tests drive a full session with a bytes.Reader
// and a buffer.
type Picker struct {
	// In supplies keystrokes.
	In io.Reader
	// Out receives the rendered frames.
	Out io.Writer
	// Prompt is shown before the query.
	Prompt string
	// Height is the number of item rows to render.
	Height int
	// Palette colorizes the frame.
	Palette render.Palette
	// EnterRaw switches the terminal to raw mode and returns a restore
	// function. When nil, no mode switching happens.
	EnterRaw func() (restore func() error, err error)
}

// NewTTY builds a Picker bound to the controlling terminal.
//
// /dev/tty is used rather than stdin and stdout so the picker still works when
// either is redirected — "kctx ctx | cat" must still be able to prompt.
func NewTTY(prompt string) (*Picker, func() error, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, ErrNoTTY
	}
	fd := int(tty.Fd())
	if !term.IsTerminal(fd) {
		_ = tty.Close()
		return nil, nil, ErrNoTTY
	}

	height := defaultHeight
	if _, rows, err := term.GetSize(fd); err == nil && rows > 0 {
		// Leave room for the prompt line, the counter line, and the shell
		// prompt that returns afterwards.
		if usable := rows - 4; usable < height {
			height = max(usable, 1)
		}
	}

	p := &Picker{
		In:      tty,
		Out:     tty,
		Prompt:  prompt,
		Height:  height,
		Palette: render.NewEnabled(true),
		EnterRaw: func() (func() error, error) {
			state, err := term.MakeRaw(fd)
			if err != nil {
				return nil, err
			}
			return func() error { return term.Restore(fd, state) }, nil
		},
	}
	return p, tty.Close, nil
}

// Run displays the picker and returns the index of the chosen item in items.
// It returns ErrAborted when the user cancels.
func (p *Picker) Run(items []Item) (int, error) {
	if len(items) == 0 {
		return 0, ErrAborted
	}
	height := p.Height
	if height < 1 {
		height = defaultHeight
	}

	if p.EnterRaw != nil {
		restore, err := p.EnterRaw()
		if err != nil {
			return 0, fmt.Errorf("switch terminal to raw mode: %w", err)
		}
		defer func() { _ = restore() }()
	}

	model := NewModel(items, height)
	drawn := 0
	// pending holds bytes of an escape sequence that arrived split across
	// reads, so the next read can complete it.
	var pending []byte

	for {
		frame, lines := p.frame(model)
		if err := p.paint(frame, drawn); err != nil {
			return 0, err
		}
		drawn = lines

		if done, accepted := model.Done(); done {
			if err := p.clear(drawn); err != nil {
				return 0, err
			}
			if !accepted {
				return 0, ErrAborted
			}
			index, ok := model.Selected()
			if !ok {
				return 0, ErrAborted
			}
			return index, nil
		}

		next, err := p.readKeys(model, pending)
		if err != nil {
			_ = p.clear(drawn)
			return 0, err
		}
		pending = next
	}
}

// readKeys blocks for one read and applies every complete keystroke in it.
// Bytes left over from an incomplete escape sequence are returned so the next
// read can finish them.
func (p *Picker) readKeys(model *Model, pending []byte) ([]byte, error) {
	buf := make([]byte, readChunk)
	n, err := p.In.Read(buf)
	if n == 0 {
		if err == nil || errors.Is(err, io.EOF) {
			// Input ended without a decision.
			model.Update(Key{Type: KeyEscape})
			return pending, nil
		}
		return pending, err
	}
	pending = append(pending, buf[:n]...)

	for len(pending) > 0 {
		key, consumed := DecodeKey(pending)
		if consumed == 0 {
			// An incomplete sequence at the end of the buffer: wait for more.
			return pending, nil
		}
		pending = pending[consumed:]
		model.Update(key)

		if done, _ := model.Done(); done {
			return pending, nil
		}
	}
	return pending, nil
}

// paint writes a frame, first moving back over the previously drawn lines.
func (p *Picker) paint(frame string, previous int) error {
	var b strings.Builder
	if previous > 0 {
		fmt.Fprintf(&b, "\r\033[%dA", previous)
	}
	b.WriteString(frame)
	_, err := io.WriteString(p.Out, b.String())
	return err
}

// clear erases the drawn frame so the picker leaves no residue behind.
func (p *Picker) clear(lines int) error {
	if lines == 0 {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\r\033[%dA", lines)
	for i := 0; i < lines; i++ {
		b.WriteString("\033[2K\n")
	}
	fmt.Fprintf(&b, "\033[%dA", lines)
	_, err := io.WriteString(p.Out, b.String())
	return err
}
