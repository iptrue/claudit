package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

var inputSpecialKeys = map[string]rune{
	"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab,
	"backspace": tea.KeyBackspace, "delete": tea.KeyDelete,
	"left": tea.KeyLeft, "right": tea.KeyRight, "up": tea.KeyUp, "down": tea.KeyDown,
	"home": tea.KeyHome, "end": tea.KeyEnd, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "f1": tea.KeyF1, "f2": tea.KeyF2,
}

// inputKey builds the KeyPressMsg the terminal delivers for a key name as tea spells it ("enter", "ctrl+w", "alt+1", "space", "y").
func inputKey(name string) tea.KeyPressMsg {
	if c, ok := inputSpecialKeys[name]; ok {
		return tea.KeyPressMsg{Code: c}
	}
	if name == "space" {
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	if name == "shift+tab" {
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	}
	if c, ok := strings.CutPrefix(name, "ctrl+"); ok {
		return tea.KeyPressMsg{Code: []rune(c)[0], Mod: tea.ModCtrl}
	}
	if c, ok := strings.CutPrefix(name, "alt+"); ok {
		return tea.KeyPressMsg{Code: []rune(c)[0], Mod: tea.ModAlt}
	}
	return tea.KeyPressMsg{Code: []rune(name)[0], Text: name}
}

// inputKeys splits a script like "a b ctrl+w" into keys; a single space is spelled "space".
func inputKeys(script string) []tea.KeyPressMsg {
	var ks []tea.KeyPressMsg
	for _, f := range strings.Fields(script) {
		ks = append(ks, inputKey(f))
	}
	return ks
}

// inputTyped returns the keys that type s rune by rune.
func inputTyped(s string) []tea.KeyPressMsg {
	var ks []tea.KeyPressMsg
	for _, r := range s {
		if r == ' ' {
			ks = append(ks, inputKey("space"))
		} else {
			ks = append(ks, inputKey(string(r)))
		}
	}
	return ks
}

func TestInputKeyNames(t *testing.T) {
	for _, name := range []string{"enter", "esc", "tab", "backspace", "delete", "left", "right", "up", "down",
		"home", "end", "pgup", "pgdown", "space", "ctrl+c", "ctrl+w", "ctrl+u", "ctrl+a", "ctrl+e", "y",
		"f2", "alt+1", "alt+0"} {
		if got := inputKey(name).String(); got != name {
			t.Errorf("inputKey(%q).String() = %q", name, got)
		}
	}
}

func TestInputEditing(t *testing.T) {
	tests := []struct {
		name   string
		typed  string // typed first, rune by rune
		keys   string // then these keys
		want   string
		cursor int
	}{
		{"type latin", "hello", "", "hello", 5},
		{"type words", "hello world", "", "hello world", 11},
		{"insert in the middle", "plnet", "left left left a", "planet", 3},
		{"emoji", "a🙂b", "left backspace", "ab", 1},
		{"backspace at start", "abc", "home backspace", "abc", 0},
		{"delete", "abc", "home delete", "bc", 0},
		{"delete at end", "abc", "delete", "abc", 3},
		{"right clamps", "ab", "right right", "ab", 2},
		{"left clamps", "ab", "left left left", "ab", 0},
		{"ctrl+a ctrl+e", "ab", "ctrl+a x ctrl+e y", "xaby", 4},
		{"home end", "ab", "home x end y", "xaby", 4},
		{"ctrl+u deletes to start", "one two", "left left ctrl+u", "wo", 0},
		{"ctrl+w deletes a word", "foo bar", "ctrl+w", "foo ", 4},
		{"ctrl+w skips trailing spaces", "foo bar  ", "ctrl+w", "foo ", 4},
		{"ctrl+w mid-text", "one two six", "left left left left ctrl+w", "one  six", 4},
		{"ctrl+w at start", "abc", "home ctrl+w", "abc", 0},
		{"non-editing key", "ab", "up down tab", "ab", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var in Input
			for _, k := range append(inputTyped(tt.typed), inputKeys(tt.keys)...) {
				in.edit(k)
			}
			if string(in.Text) != tt.want || in.Cursor != tt.cursor {
				t.Errorf("got %q cursor %d, want %q cursor %d", string(in.Text), in.Cursor, tt.want, tt.cursor)
			}
		})
	}
}

func TestInputEditReportsUnhandledKeys(t *testing.T) {
	var in Input
	for _, name := range []string{"enter", "esc", "up", "down", "tab", "pgup"} {
		if in.edit(inputKey(name)) {
			t.Errorf("edit(%q) = true, want false", name)
		}
	}
	if !in.edit(inputKey("x")) || !in.edit(inputKey("backspace")) {
		t.Error("editing keys must report true")
	}
}

func TestInputPasteKeepsNewlines(t *testing.T) {
	m, sent := modelNew(t, false)
	modelPress(m, inputTyped("ab")...)
	modelPress(m, inputKey("left"))
	m.Update(tea.PasteMsg{Content: "1\r\n2\r3"})
	if got := string(m.Input.Text); got != "a1\n2\n3b" || m.Input.Cursor != 6 {
		t.Fatalf("after paste: %q cursor %d", got, m.Input.Cursor)
	}
	modelPress(m, inputKey("enter"))
	if len(*sent) != 1 || modelUserText((*sent)[0]) != "a1\n2\n3b" {
		t.Fatalf("sent %#v", *sent)
	}
}
