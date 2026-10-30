package sim

import (
	"testing"

	"github.com/pixelbender/claudit/internal/ide"
)

// patch replaces only what differs: carets keep their place in the text.
func TestPatch(t *testing.T) {
	tests := []struct {
		from, to string
		caret    ide.Pos // of another agent, before
		want     ide.Pos // after
	}{
		{"abc\ndef", "abc\nXdef", ide.Pos{Line: 1, Col: 1}, ide.Pos{Line: 1, Col: 2}},
		{"one\ntwo\nthree", "one\nthree", ide.Pos{Line: 2, Col: 3}, ide.Pos{Line: 1, Col: 3}},
		{"one\ntwo\nthree", "one\nthree", ide.Pos{Line: 1, Col: 2}, ide.Pos{Line: 1, Col: 1}}, // inside the cut "wo\nt"
		{"aa", "aaa", ide.Pos{Line: 0, Col: 1}, ide.Pos{Line: 0, Col: 1}},
		{"xéy", "xèy", ide.Pos{Line: 0, Col: 3}, ide.Pos{Line: 0, Col: 3}},
		{"é", "è", ide.Pos{}, ide.Pos{Line: 0, Col: 1}}, // at the cut: after the new text, as Insert does
		{"", "abc\nd", ide.Pos{}, ide.Pos{Line: 1, Col: 1}},
		{"abc", "", ide.Pos{Line: 0, Col: 2}, ide.Pos{}},
		{"same", "same", ide.Pos{Line: 0, Col: 2}, ide.Pos{Line: 0, Col: 2}},
	}
	for _, tc := range tests {
		var b ide.Buffer
		b.SetText(tc.from)
		b.Caret("other").Pos = tc.caret
		patch(&b, tc.to)
		if b.Text() != tc.to || b.Carets["other"].Pos != tc.want || len(b.Carets) != 1 {
			t.Errorf("%q → %q: got %q, caret %v want %v, %d carets", tc.from, tc.to, b.Text(), b.Carets["other"].Pos, tc.want, len(b.Carets))
		}
	}
}

func TestSeek(t *testing.T) {
	text := "x ab y ab z"
	for _, tc := range []struct {
		old       string
		near, at  int
		wantFound bool
	}{
		{"ab", 7, 7, true}, {"ab", 3, 2, true}, {"ab", 0, 2, true}, {"ab", 99, 7, true}, {"zz", 3, -1, false},
	} {
		if at, ok := seek(text, tc.old, tc.near); at != tc.at || ok != tc.wantFound {
			t.Errorf("seek(%q, %d) = %d %v, want %d", tc.old, tc.near, at, ok, tc.at)
		}
	}
}

func TestOffset(t *testing.T) {
	var b ide.Buffer
	b.SetText("hello🙂\nyes")
	text := b.Text()
	for _, p := range []ide.Pos{{}, {Line: 0, Col: 6}, {Line: 1, Col: 2}, {Line: 1, Col: 3}} {
		got := offset(&b, p)
		if advance(ide.Pos{}, text[:got]) != p {
			t.Errorf("offset(%v) = %d", p, got)
		}
	}
}
