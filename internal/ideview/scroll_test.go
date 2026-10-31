package ideview

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/ide"
)

// scrollRows is the plain rows of the view of st with o, 100×20.
func scrollRows(st *ide.State, o Options) []string {
	return strings.Split(ansi.Strip(Render(st, o, 100, 20)), "\n")
}

// The wheel scrolls the pane under it as far as there is to see, and stops where it follows its agent.
func TestWheel(t *testing.T) {
	st := sampleState()
	st.Tree = nil
	for i := range 50 {
		st.Tree = append(st.Tree, ide.TreeRow{Name: fmt.Sprintf("f%02d", i), Path: fmt.Sprintf("/f%02d", i)})
	}
	st.Agents[0].TreeSel = "/f25" // followed at row 19 of 32 at most
	var lines []string
	for i := range 100 {
		lines = append(lines, fmt.Sprintf("l%02d", i))
	}
	*mainBuf(st) = ide.Buffer{Lines: lines, Carets: map[string]*ide.Caret{"main": {Pos: ide.Pos{Line: 50}}}} // followed at line 44
	o := Options{Scroll: Scroll{}}
	src := fileKey(root + "/src/main.go")

	for _, tt := range []struct {
		x, from, n int
		key        string
		want       int
	}{
		{5, 0, 3, filesPane, 3},
		{5, 3, 100, filesPane, 19},   // the first row on top
		{5, 0, -100, filesPane, -13}, // the last row at the bottom
		{5, 2, -3, filesPane, 0},     // back where it follows
		{50, 0, 3, src, 3},
		{50, 0, 100, src, 44},
		{50, 0, -100, src, -38},
		{50, -2, 5, src, 0},
	} {
		o.Scroll[tt.key] = tt.from
		if key, got := Wheel(st, o, 100, 20, tt.x, 10, tt.n); key != tt.key || got != tt.want {
			t.Errorf("at x=%d from %d by %d: %q %d, want %q %d", tt.x, tt.from, tt.n, key, got, tt.key, tt.want)
		}
	}

	o.Scroll = Scroll{filesPane: 3, src: 3}
	rows := scrollRows(st, o)
	if !strings.Contains(rows[1], "f16") || !strings.Contains(rows[1], " 42 l41") {
		t.Errorf("scrolled back 3: %q", rows[1])
	}
	o.Wrap = true
	if rows := scrollRows(st, o); !strings.Contains(rows[1], " 42 l41") {
		t.Errorf("wrapping, scrolled back 3: %q", rows[1])
	}

	// The terminal, beside the file, scrolls back from its tail, over its scrollback.
	st.Agents[0].Focus = ide.Terminal
	for i := range 50 {
		st.Agents[0].Term.Print(ide.Output, fmt.Sprintf("out%02d", i))
	}
	term := termKey("main")
	if key, got := Wheel(st, o, 100, 20, 80, 10, -3); key != term || got != 0 {
		t.Errorf("terminal past its tail: %q %d", key, got)
	}
	if key, got := Wheel(st, o, 100, 20, 80, 10, 100); key != term || got != 36 { // 54 rows in 18
		t.Errorf("terminal back to its start: %q %d", key, got)
	}
	o.Scroll[term] = 3
	rows = scrollRows(st, o)
	if bottom := rows[len(rows)-2]; !strings.Contains(bottom, "out47") || strings.Contains(strings.Join(rows, "\n"), "go te") {
		t.Errorf("terminal scrolled back 3 ends with %q", bottom)
	}
	o.Scroll[term] = 36
	if rows := scrollRows(st, o); !strings.Contains(rows[1], "$ go build") {
		t.Errorf("terminal scrolled to its start: %q", rows[1])
	}

	// What is not drawn forgets its scroll: the browser takes the terminal's place.
	o.Scroll[src] = 3
	st.Agents[0].Focus = ide.Browser
	Prune(st, o, 100, 20)
	if _, ok := o.Scroll[term]; ok || o.Scroll[src] != 3 || o.Scroll[filesPane] != 3 {
		t.Errorf("pruned: %v", o.Scroll)
	}
}

// A Bash call's lines come to the top of its agent's terminal however far back they are; at its
// end, the terminal follows its agent. A call the terminal holds no line of, or a terminal not on
// screen, scrolls nothing.
func TestTermScroll(t *testing.T) {
	st := sampleState()
	st.Pinned = true // main alone: its file beside its terminal
	main := st.Agents[0]
	main.Term.Lines = nil
	out := func(what string) {
		for i := range 30 {
			main.Term.Lines = append(main.Term.Lines, ide.TermLine{Kind: ide.Output, Text: fmt.Sprintf("%s %02d", what, i)})
		}
	}
	out("before")
	main.Term.Lines = append(main.Term.Lines,
		ide.TermLine{Kind: ide.Comment, Text: "# Run the tests", Call: "b1"},
		ide.TermLine{Kind: ide.Command, Text: "go test ./...", Call: "b1"})
	out("after")
	main.Term.Lines = append(main.Term.Lines, ide.TermLine{Kind: ide.Command, Text: "pwd", Call: "b2"})

	o := Options{Scroll: Scroll{}}
	key, back := TermScroll(st, o, 100, 20, "main", "b1")
	if key != termKey("main") || back <= 0 {
		t.Fatalf("b1: %q %d", key, back)
	}
	o.Scroll[key] = back
	if rows := scrollRows(st, o); !strings.Contains(rows[1], "# Run the tests") || !strings.Contains(rows[2], "go test") ||
		strings.Contains(strings.Join(rows, "\n"), "before 29") {
		t.Errorf("scrolled to b1:\n%s", strings.Join(rows, "\n"))
	}
	if key, back := TermScroll(st, o, 100, 20, "main", "b2"); key != termKey("main") || back != 0 {
		t.Errorf("b2, the last: %q %d", key, back)
	}
	if key, back := TermScroll(st, o, 100, 20, "main", "b3"); key != "" || back != 0 {
		t.Errorf("a call it holds no line of: %q %d", key, back)
	}
	main.Focus = ide.Browser // its file beside its browser: no terminal on screen
	if key, back := TermScroll(st, o, 100, 20, "main", "b1"); key != "" || back != 0 {
		t.Errorf("no terminal on screen: %q %d", key, back)
	}
}
