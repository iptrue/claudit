package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/protocol"
)

func TestViewSuggest(t *testing.T) {
	var names []protocol.Command
	for i := range 12 {
		names = append(names, protocol.Command{Name: fmt.Sprintf("cmd%02d", i), Description: strings.Repeat("description ", i)})
	}
	names[3].Hint = "<instructions>\nwith a break"

	box := renderSuggest(names, 10, "cmd", 60)
	lines := drawPlainLines(box)
	if len(lines) != drawSuggestMax+2 || !strings.Contains(lines[len(lines)-1], "+4") {
		t.Fatalf("box:\n%s", strings.Join(lines, "\n"))
	}
	if plain := strings.Join(lines, "\n"); !strings.Contains(plain, "/cmd10 ") || strings.Contains(plain, "cmd00") || !strings.Contains(lines[8], "description") {
		t.Errorf("the highlight is not in view:\n%s", strings.Join(lines, "\n"))
	}
	for _, mw := range []int{1, 5, 10, 30} {
		for _, l := range strings.Split(renderSuggest(names, 0, "c", mw), "\n") {
			if ansi.StringWidth(l) > max(mw, 5) {
				t.Errorf("max %d: line %q", mw, ansi.Strip(l))
			}
		}
	}
}

func TestViewHelp(t *testing.T) {
	for w := 10; w <= 120; w += 7 {
		for h := 3; h <= 40; h += 5 {
			lines := strings.Split(renderHelp(w, h), "\n")
			if len(lines) > h {
				t.Fatalf("%dx%d: %d lines", w, h, len(lines))
			}
			for _, l := range lines {
				if ansi.StringWidth(l) > max(w, 5) {
					t.Fatalf("%dx%d: line %q", w, h, ansi.Strip(l))
				}
			}
		}
	}
	if out := ansi.Strip(renderHelp(100, 40)); !strings.Contains(out, "claudit") || strings.Contains(out, "workflow") {
		t.Errorf("help:\n%s", out)
	}
	// A long description goes on under itself, never cut; cut short, the help still says how to close it.
	out := ansi.Strip(renderHelp(60, 60))
	for _, want := range []string{"what the new mode allows", "drop picture files to attach them"} {
		if !strings.Contains(strings.Join(strings.Fields(out), " "), want) {
			t.Errorf("help lacks %q:\n%s", want, out)
		}
	}
	if out := ansi.Strip(renderHelp(60, 12)); !strings.Contains(out, "esc close") {
		t.Errorf("short help:\n%s", out)
	}
}

// Overlays never change the size of the screen.
func TestViewOverlays(t *testing.T) {
	t0 := time.Unix(1000, 0)
	for _, w := range []int{40, 57, 80, 133} {
		for _, h := range []int{16, 24, 41} {
			m := drawTestModel(t, w, h)
			m.Now = t0
			m.Input = Input{Text: []rune("/c"), Cursor: 2}
			m.Suggest = []protocol.Command{{Name: "clear"}, {Name: "compact", Hint: "<instructions>", Description: "Free up context"}, {Name: "context"}}
			drawCheckSize(t, m.View().Content, w, h)
			m.ShowHelp = true
			v := m.View()
			drawCheckSize(t, v.Content, w, h)
			if v.Cursor != nil {
				t.Errorf("%dx%d: prompt caret under the help", w, h)
			}
			if !strings.Contains(ansi.Strip(v.Content), "esc close") && h >= 30 {
				t.Errorf("%dx%d: no help", w, h)
			}
		}
	}
}

func TestViewPopupDrop(t *testing.T) {
	if y := drawPopupY(40, 10, -1); y != 15 {
		t.Errorf("unknown age: y %d", y)
	}
	if y := drawPopupY(40, 10, 0); y != 11 {
		t.Errorf("new popup: y %d", y)
	}
	if y := drawPopupY(40, 10, drawPopupDrop); y != 15 {
		t.Errorf("settled popup: y %d", y)
	}
	if y := drawPopupY(10, 10, 0); y != 0 {
		t.Errorf("tall popup: y %d", y)
	}
}

func TestViewTitle(t *testing.T) {
	cases := []struct {
		st     Status
		asking bool
		title  string
		bar    bool
	}{
		{Status{State: "idle"}, false, "claudit", false},
		{Status{State: "thinking", Busy: true}, false, "claudit · thinking", true},
		{Status{State: "idle", Agents: 3}, false, "claudit · 3 agents", true},
		{Status{State: "waiting for you", Busy: true}, true, "claudit · waiting for you", true},
	}
	for _, c := range cases {
		title, bar := drawTitle(c.st, c.asking)
		if title != c.title || (bar != nil) != c.bar {
			t.Errorf("%+v: %q bar %v", c.st, title, bar)
		}
	}
	if _, bar := drawTitle(Status{}, true); bar.State != tea.ProgressBarWarning {
		t.Errorf("a waiting popup should warn: %+v", bar)
	}
}

// The typed part splits at a rune of the name, whatever case folding does to byte lengths.
func TestViewTypedPrefix(t *testing.T) {
	cases := []struct{ name, typed, head, rest string }{
		{"init", "in", "in", "it"},
		{"init", "İn", "in", "it"}, // U+0130 is 2 bytes, "i" 1: the split follows the name's runes
		{"init", "IN", "in", "it"},
		{"kill", "K", "k", "ill"}, // the Kelvin sign is 3 bytes, k is 1
		{"ab", "abc", "", "ab"},
		{"ab", "ab", "ab", ""},
		{"世界", "世", "世", "界"},
	}
	for _, c := range cases {
		if head, rest := drawTypedPrefix(c.name, c.typed); head != c.head || rest != c.rest {
			t.Errorf("drawTypedPrefix(%q, %q) = %q, %q", c.name, c.typed, head, rest)
		}
	}
	renderSuggest([]protocol.Command{{Name: "init"}, {Name: "k"}}, 0, "İnit", 60) // must not panic
	renderSuggest([]protocol.Command{{Name: "k"}}, 0, "K", 60)
}

// With the completions open, the hints are theirs.
func TestViewHintsCompletion(t *testing.T) {
	m := drawTestModel(t, 160, 30)
	m.Input = Input{Text: []rune("/c"), Cursor: 2}
	m.Suggest = []protocol.Command{{Name: "clear"}}
	if status := drawPlainLines(m.View().Content)[29]; !strings.HasSuffix(status, "esc clear") {
		t.Errorf("completion hints: %q", status)
	}
}

// What leaves the screen keeps its text whole: a notification has no cells to agree on.
func TestDrawNoticeKeepsText(t *testing.T) {
	if got := drawNotice("done ❤️ 👨‍👩‍👧\n\x1b[1mok\x1b[0m"); got != "\x1b]9;done ❤️ 👨‍👩‍👧 ok\x07" {
		t.Errorf("notice %q", got)
	}
}
