package picker

import "unicode/utf8"

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

	r, size := utf8.DecodeRune(buf)
	if r == utf8.RuneError && size <= 1 {
		// Either an invalid byte or a multi-byte rune split across reads.
		if len(buf) < utf8.UTFMax {
			return Key{Type: KeyIgnore}, 0
		}
		return Key{Type: KeyIgnore}, 1
	}
	return Key{Type: KeyRune, Rune: r}, size
}

// decodeEscape handles ESC-prefixed sequences: arrows, page keys, and a bare
// ESC meaning "abort".
func decodeEscape(buf []byte) (Key, int) {
	if len(buf) == 1 {
		// A lone ESC byte is ambiguous until more input arrives. The caller
		// resolves it by reading again; on a real key press the rest of the
		// sequence is already in the same read.
		return Key{Type: KeyEscape}, 1
	}
	if buf[1] != keyBracket {
		return Key{Type: KeyIgnore}, 2
	}
	if len(buf) < 3 {
		return Key{Type: KeyIgnore}, 0
	}

	switch buf[2] {
	case 'A':
		return Key{Type: KeyUp}, 3
	case 'B':
		return Key{Type: KeyDown}, 3
	case 'C', 'D':
		return Key{Type: KeyIgnore}, 3
	case '5', '6':
		// Page Up / Page Down arrive as ESC [ 5 ~ and ESC [ 6 ~.
		if len(buf) < 4 {
			return Key{Type: KeyIgnore}, 0
		}
		if buf[2] == '5' {
			return Key{Type: KeyPageUp}, 4
		}
		return Key{Type: KeyPageDown}, 4
	}
	return Key{Type: KeyIgnore}, 3
}
