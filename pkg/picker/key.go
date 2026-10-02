package picker

import (
	"strings"
	"unicode/utf8"
)

// KeyType classifies one decoded keystroke.
type KeyType int

const (
	// KeyIgnore is an input the picker does not act on.
	KeyIgnore KeyType = iota
	// KeyRune is a printable character to append to the query.
	KeyRune
	// KeyUp moves the selection up.
	KeyUp
	// KeyDown moves the selection down.
	KeyDown
	// KeyEnter accepts the highlighted item.
	KeyEnter
	// KeyEscape aborts.
	KeyEscape
	// KeyBackspace deletes the last query character.
	KeyBackspace
	// KeyClearLine clears the whole query.
	KeyClearLine
	// KeyClearWord deletes the last query word.
	KeyClearWord
	// KeyPageUp scrolls up by a screenful.
	KeyPageUp
	// KeyPageDown scrolls down by a screenful.
	KeyPageDown
)

// Key is one decoded keystroke.
type Key struct {
	Type KeyType
	Rune rune
}

// Control byte values the picker understands.
const (
	ctrlA      = 0x01
	ctrlC      = 0x03
	ctrlE      = 0x05
	ctrlN      = 0x0e
	ctrlP      = 0x10
	ctrlU      = 0x15
	ctrlW      = 0x17
	keyEsc     = 0x1b
	keyEnter   = 0x0d
	keyLF      = 0x0a
	keyDel     = 0x7f
	keyBS      = 0x08
	keyBracket = '['
)

// DecodeKey reads one keystroke from the front of buf.
//
// It returns the key and how many bytes it consumed. A consumption of 0 means
// buf holds an incomplete sequence and the caller should read more input — a
// terminal can split an arrow-key escape sequence across two reads.
func DecodeKey(buf []byte) (Key, int) {
	if len(buf) == 0 {
		return Key{Type: KeyIgnore}, 0
	}

	switch buf[0] {
	case ctrlC:
		return Key{Type: KeyEscape}, 1
	case keyEnter, keyLF:
		return Key{Type: KeyEnter}, 1
	case keyDel, keyBS:
		return Key{Type: KeyBackspace}, 1
	case ctrlU:
		return Key{Type: KeyClearLine}, 1
	case ctrlW:
		return Key{Type: KeyClearWord}, 1
	case ctrlN:
		return Key{Type: KeyDown}, 1
	case ctrlP:
		return Key{Type: KeyUp}, 1
	case ctrlA, ctrlE:
		return Key{Type: KeyIgnore}, 1
	case keyEsc:
		return decodeEscape(buf)
	}

	if buf[0] < 0x20 {
		return Key{Type: KeyIgnore}, 1
	}

	if !utf8.FullRune(buf) {
		// A multi-byte rune split across reads: wait for the rest.
		return Key{Type: KeyIgnore}, 0
	}
	r, size := utf8.DecodeRune(buf)
	if r == utf8.RuneError && size <= 1 {
		return Key{Type: KeyIgnore}, 1
	}
	return Key{Type: KeyRune, Rune: r}, size
}

// maxCSIParams caps how many parameter bytes a stray "ESC [" can swallow.
const maxCSIParams = 16

// decodeEscape handles ESC-prefixed sequences: arrows, page keys, and a bare
// ESC meaning "abort".
func decodeEscape(buf []byte) (Key, int) {
	if len(buf) == 1 {
		// A lone ESC aborts instead of waiting for more input: an arrow key's bytes
		// normally arrive in one read, so an ESC on its own is the Escape key.
		return Key{Type: KeyEscape}, 1
	}
	// ESC O is SS3 (F1-F4, application-mode arrows), with CSI's byte classes.
	if buf[1] == keyBracket || buf[1] == 'O' {
		return decodeCSI(buf)
	}
	return Key{Type: KeyIgnore}, 2
}

// decodeCSI consumes an ESC [ or ESC O sequence through its final byte, so the
// parameters of a key the picker ignores (Delete is ESC [ 3 ~) never reach the
// query. A modifier keeps the key's meaning: Ctrl-Up still moves up.
func decodeCSI(buf []byte) (Key, int) {
	for i := 2; i < len(buf); i++ {
		b := buf[i]
		if b >= 0x40 && b <= 0x7e {
			if b == '~' {
				return Key{Type: pageKey(string(buf[2:i]))}, i + 1
			}
			return Key{Type: arrowKey(b)}, i + 1
		}
		if b < 0x20 || b > 0x3f {
			// Not a control sequence after all: drop what was read of it and
			// decode this byte on its own.
			return Key{Type: KeyIgnore}, i
		}
	}
	if len(buf)-2 > maxCSIParams {
		return Key{Type: KeyIgnore}, len(buf)
	}
	return Key{Type: KeyIgnore}, 0
}

// arrowKey maps the final byte of an arrow sequence; left and right are unused.
func arrowKey(final byte) KeyType {
	switch final {
	case 'A':
		return KeyUp
	case 'B':
		return KeyDown
	}
	return KeyIgnore
}

// pageKey maps the key number of an ESC [ n ~ sequence, ignoring any modifier
// after it.
func pageKey(params string) KeyType {
	if i := strings.IndexByte(params, ';'); i >= 0 {
		params = params[:i]
	}
	switch params {
	case "5":
		return KeyPageUp
	case "6":
		return KeyPageDown
	}
	return KeyIgnore
}
