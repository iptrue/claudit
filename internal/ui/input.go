package ui

import (
	"slices"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// Input is a one-line text field. Cursor is a rune index into Text.
type Input struct {
	Text   []rune
	Cursor int
}

// edit applies an editing key and reports whether the key was one.
func (in *Input) edit(k tea.KeyPressMsg) bool {
	switch k.String() {
	case "left":
		in.Cursor = max(in.Cursor-1, 0)
	case "right":
		in.Cursor = min(in.Cursor+1, len(in.Text))
	case "home", "ctrl+a":
		in.Cursor = 0
	case "end", "ctrl+e":
		in.Cursor = len(in.Text)
	case "backspace":
		before := string(in.Text[:in.Cursor])
		if loc := placeholderTail.FindStringIndex(before); loc != nil { // a placeholder goes whole
			n := utf8.RuneCountInString(before[loc[0]:])
			in.Text = slices.Delete(in.Text, in.Cursor-n, in.Cursor)
			in.Cursor -= n
		} else if in.Cursor > 0 {
			in.Text = slices.Delete(in.Text, in.Cursor-1, in.Cursor)
			in.Cursor--
		}
	case "delete":
		if in.Cursor < len(in.Text) {
			in.Text = slices.Delete(in.Text, in.Cursor, in.Cursor+1)
		}
	case "ctrl+u":
		in.Text = slices.Delete(in.Text, 0, in.Cursor)
		in.Cursor = 0
	case "ctrl+w":
		i := in.Cursor
		for i > 0 && unicode.IsSpace(in.Text[i-1]) {
			i--
		}
		for i > 0 && !unicode.IsSpace(in.Text[i-1]) {
			i--
		}
		in.Text = slices.Delete(in.Text, i, in.Cursor)
		in.Cursor = i
	default:
		if k.Text == "" {
			return false
		}
		in.insert(k.Text)
	}
	return true
}

// insert puts s at the cursor and moves the cursor past it.
func (in *Input) insert(s string) {
	r := []rune(s)
	in.Text = slices.Insert(in.Text, in.Cursor, r...)
	in.Cursor += len(r)
}
