package ideview

import (
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/ide"
)

// The workplaces of the agents at work share the right column: the camera's always, the others
// while busy or a moment after, a file several work in once, at most three, as many as fit. An
// agent at work alone has its file and, beside it, its terminal or its browser.
func TestColumns(t *testing.T) {
	keys := func(cols []column) string {
		var s []string
		for _, c := range cols {
			k := c.agent.Key
			switch c.pane {
			case ide.Terminal:
				k += ":term"
			case ide.Browser:
				k += ":web"
			}
			s = append(s, k)
		}
		return strings.Join(s, " ")
	}
	st := sampleState()
	if got := keys(columns(st, camera(st), Options{Now: now}, 200)); got != "main main:term" {
		t.Errorf("main alone: %q", got)
	}
	cam := camera(st)
	cam.Focus = ide.Browser
	if got := keys(columns(st, camera(st), Options{Now: now}, 200)); got != "main main:web" {
		t.Errorf("main alone on the web: %q", got)
	}
	cam.Focus = ide.Editor
	if got := keys(columns(st, camera(st), Options{Now: now}, 2*minColumn-1)); got != "main" {
		t.Errorf("main alone, too narrow for two: %q", got)
	}
	term := cam.Term
	cam.Term = ide.Term{}
	if got := keys(columns(st, camera(st), Options{Now: now}, 200)); got != "main" {
		t.Errorf("main alone, no command run yet: %q", got)
	}
	cam.Term, cam.File, cam.Focus = term, "", ide.Terminal
	if got := keys(columns(st, camera(st), Options{Now: now}, 200)); got != "main:term" {
		t.Errorf("main alone, no file yet: %q", got)
	}

	st = multiState() // tests works in its terminal; lint has no workplace yet; docs is done
	if got := keys(columns(st, camera(st), Options{Now: now}, 200)); got != "main toolu_tests:term" {
		t.Errorf("multi: %q", got)
	}
	if got := keys(columns(st, camera(st), Options{Now: now}, 50)); got != "main" {
		t.Errorf("too narrow for two: %q", got)
	}

	// docs busy a moment ago in main's file shares main's column; later it is gone for good.
	docs := findAgent(st, "a2")
	docs.Active = now.Add(-time.Second)
	if got := keys(columns(st, camera(st), Options{Now: now}, 200)); got != "main toolu_tests:term" {
		t.Errorf("a file worked in by two: %q", got)
	}
	docs.File = root + "/README.md"
	if got := keys(columns(st, camera(st), Options{Now: now}, 200)); got != "main toolu_tests:term a2" {
		t.Errorf("docs a moment ago: %q", got)
	}
	if got := keys(columns(st, camera(st), Options{Now: now.Add(columnLinger)}, 200)); got != "main toolu_tests:term" {
		t.Errorf("docs long ago: %q", got)
	}

	// A fourth workplace pushes out the one idle longest; the camera's stays.
	lint := findAgent(st, "a1")
	lint.File, lint.Active = root+"/docs/x.md", now
	st.Agents[1].Activity, st.Agents[1].Active = "", now.Add(-5*time.Second)
	if got := keys(columns(st, camera(st), Options{Now: now}, 200)); got != "main a1 a2" {
		t.Errorf("four: %q", got)
	}
}

// The text under a cell is that of its pane, inside the frame, without the editor's line numbers
// or a terminal's air; there is none on a frame or in an empty editor.
func TestTextAt(t *testing.T) {
	st := sampleState()
	o := Options{Now: now}
	rows := strings.Split(ansi.Strip(Render(st, o, 100, 20)), "\n")
	cut := func(r image.Rectangle, y int) string { return ansi.Cut(rows[y], r.Min.X, r.Max.X) }
	if r := TextAt(st, o, 100, 20, 40, 5); !strings.HasPrefix(cut(r, 1), "package main") || r.Dy() != 18 {
		t.Errorf("editor text %v: %q", r, cut(r, 1))
	}
	if r := TextAt(st, o, 100, 20, 80, 5); !strings.HasPrefix(cut(r, 18), "$ go te") || ansi.Cut(rows[18], r.Max.X, r.Max.X+2) != " │" {
		t.Errorf("terminal text %v: %q", r, cut(r, 18))
	}
	if r := TextAt(st, o, 100, 20, 5, 3); !strings.HasPrefix(cut(r, 1), "▸ docs") {
		t.Errorf("explorer text %v: %q", r, cut(r, 1))
	}
	for _, p := range [][2]int{{60, 5}, {22, 5}, {40, 0}, {40, 19}} { // a frame, the gutter, the top, the bottom
		if r := TextAt(st, o, 100, 20, p[0], p[1]); !r.Empty() {
			t.Errorf("text at %v: %v", p, r)
		}
	}
	st.Tabs = nil
	if r := TextAt(st, o, 100, 20, 40, 5); !r.Empty() {
		t.Errorf("text in an empty editor: %v", r)
	}
}

// Wrapped, a long line breaks after its last space that fits; its rows past the first have no
// line number. Every row keeps the width.
func TestWrap(t *testing.T) {
	st := sampleState()
	words := strings.TrimSpace(strings.Repeat("alpha beta gamma ", 12))
	buf := mainBuf(st)
	buf.Lines = []string{"package main", words, "}"}
	buf.Carets["main"] = &ide.Caret{Pos: ide.Pos{Line: 1, Col: len(words)}}
	out := Render(st, Options{Now: now, Wrap: true}, 80, 20)
	checkSize(t, "wrap", out, 80, 20)
	plain := ansi.Strip(out)
	rows := strings.Split(plain, "\n")
	var mine []string
	for _, r := range rows {
		if strings.Contains(r, "alpha") || strings.Contains(r, "gamma") {
			mine = append(mine, r)
		}
	}
	if len(mine) < 3 || !strings.Contains(mine[0], " 2 alpha beta") || !strings.Contains(mine[1], "│   ") || strings.Contains(plain, " 3 }") == false {
		t.Fatalf("the long line does not wrap:\n%s", plain)
	}
	for _, r := range mine {
		if text := strings.TrimRight(r[strings.Index(r, "││")+len("││"):], "│ "); strings.HasSuffix(text, "alph") || strings.HasSuffix(text, "gam") {
			t.Errorf("a word is cut: %q", r)
		}
	}
	if strings.Contains(plain, "alpha beta gamma alpha beta gamma alpha beta gamma alpha beta gamma alpha") {
		t.Errorf("rows wider than the editor:\n%s", plain)
	}

	// Unwrapped, it scrolls sideways to the caret instead.
	if out := ansi.Strip(Render(st, Options{Now: now}, 80, 20)); strings.Contains(out, " 2 alpha beta") {
		t.Errorf("unwrapped, the line does not scroll to the caret:\n%s", out)
	}

	// A file of long lines keeps the caret's row in view.
	var many []string
	for i := range 40 {
		many = append(many, fmt.Sprint(i, " ", words))
	}
	buf.Lines = many
	buf.Carets["main"] = &ide.Caret{Pos: ide.Pos{Line: 30, Col: 3}}
	if out := ansi.Strip(Render(st, Options{Now: now, Wrap: true}, 80, 20)); !strings.Contains(out, " 31 30 alpha") {
		t.Errorf("the caret's line is out of view:\n%s", out)
	}
}
