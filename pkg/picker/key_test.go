package picker

import "testing"

func TestDecodeKey(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantType KeyType
		wantRune rune
		wantN    int
	}{
		{"empty buffer", "", KeyIgnore, 0, 0},
		{"printable rune", "a", KeyRune, 'a', 1},
		{"multi-byte rune", "한", KeyRune, '한', 3},
		{"enter", "\r", KeyEnter, 0, 1},
		{"line feed", "\n", KeyEnter, 0, 1},
		{"ctrl-c", "\x03", KeyEscape, 0, 1},
		{"delete", "\x7f", KeyBackspace, 0, 1},
		{"backspace", "\x08", KeyBackspace, 0, 1},
		{"ctrl-u", "\x15", KeyClearLine, 0, 1},
		{"ctrl-w", "\x17", KeyClearWord, 0, 1},
		{"ctrl-n", "\x0e", KeyDown, 0, 1},
		{"ctrl-p", "\x10", KeyUp, 0, 1},
		{"ctrl-a ignored", "\x01", KeyIgnore, 0, 1},
		{"other control ignored", "\x02", KeyIgnore, 0, 1},
		{"arrow up", "\x1b[A", KeyUp, 0, 3},
		{"arrow down", "\x1b[B", KeyDown, 0, 3},
		{"arrow right ignored", "\x1b[C", KeyIgnore, 0, 3},
		{"page up", "\x1b[5~", KeyPageUp, 0, 4},
		{"page down", "\x1b[6~", KeyPageDown, 0, 4},
		{"unknown csi", "\x1b[Z", KeyIgnore, 0, 3},
		{"esc + non-bracket", "\x1bx", KeyIgnore, 0, 2},
		{"lone esc aborts", "\x1b", KeyEscape, 0, 1},
		{"incomplete csi waits", "\x1b[", KeyIgnore, 0, 0},
		{"incomplete page key waits", "\x1b[5", KeyIgnore, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, n := DecodeKey([]byte(tt.in))
			if key.Type != tt.wantType {
				t.Errorf("type = %v, want %v", key.Type, tt.wantType)
			}
			if key.Rune != tt.wantRune {
				t.Errorf("rune = %q, want %q", key.Rune, tt.wantRune)
			}
			if n != tt.wantN {
				t.Errorf("consumed %d bytes, want %d", n, tt.wantN)
			}
		})
	}
}

func TestDecodeKeySplitRune(t *testing.T) {
	// The leading byte of a 3-byte rune on its own: wait for the rest.
	partial := []byte("한")[:1]
	if _, n := DecodeKey(partial); n != 0 {
		t.Errorf("consumed %d bytes on a split rune, want 0", n)
	}

	// An invalid byte in a full-width buffer is skipped rather than stalling.
	invalid := []byte{0xff, 'a', 'b', 'c', 'd'}
	key, n := DecodeKey(invalid)
	if n != 1 || key.Type != KeyIgnore {
		t.Errorf("DecodeKey(invalid) = %v, %d; want ignore, 1", key.Type, n)
	}
}

func TestDecodeKeySequence(t *testing.T) {
	// A burst of input decodes into successive keystrokes.
	buf := []byte("ab\x1b[B\r")
	var got []KeyType
	for len(buf) > 0 {
		key, n := DecodeKey(buf)
		if n == 0 {
			t.Fatal("stalled on a complete buffer")
		}
		got = append(got, key.Type)
		buf = buf[n:]
	}

	want := []KeyType{KeyRune, KeyRune, KeyDown, KeyEnter}
	if len(got) != len(want) {
		t.Fatalf("decoded %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("key %d = %v, want %v", i, got[i], want[i])
		}
	}
}
