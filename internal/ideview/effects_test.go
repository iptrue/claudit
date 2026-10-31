package ideview

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/ide"
	"github.com/pixelbender/claudit/internal/theme"
)

// The camera's agent at work alone has its file and its terminal side by side at full height, as
// long as it has used both; one at a time where they do not fit.
func TestSoloPanes(t *testing.T) {
	st := sampleState()
	out := ansi.Strip(render(st, 100, 20))
	if !strings.Contains(out, "package main") || !strings.Contains(out, "$ go te") {
		t.Errorf("the editor and the terminal should be side by side:\n%s", out)
	}
	// Full height: the bottom borders of the explorer and both panes are the last line.
	lines := strings.Split(out, "\n")
	if strings.Count(lines[len(lines)-1], "╯") != 3 || !strings.Contains(lines[len(lines)-2], "$ go te") {
		t.Errorf("the panes are not full height:\n%s", out)
	}
	st.Agents[0].Focus = ide.Explorer
	if out := ansi.Strip(render(st, 100, 20)); !strings.Contains(out, "package main") || !strings.Contains(out, "$ go te") {
		t.Errorf("explorer focus should keep both:\n%s", out)
	}
	st.Agents[0].Focus = ide.Terminal
	if out := ansi.Strip(render(st, 80, 20)); strings.Contains(out, "package main") || !strings.Contains(out, "$ go te") {
		t.Errorf("too narrow for both, terminal focus should show only the terminal:\n%s", out)
	}
	st.Agents[0].Focus, st.Agents[0].Term = ide.Editor, ide.Term{}
	if out := ansi.Strip(render(st, 100, 20)); !strings.Contains(out, "package main") || strings.Contains(out, "terminal") {
		t.Errorf("no command run yet should show only the editor:\n%s", out)
	}
}

// A cut wipes the right column in behind a scanline in the camera's colour, then leaves it alone.
// Split into workplaces, each wipes in on its own when its agent turns.
func TestWipe(t *testing.T) {
	multi := multiState()
	multi.Agents[1].FocusAt = now.Add(-cutTime / 3)
	out := Render(multi, Options{Now: now}, 120, 30)
	if scan := lineWith(out, "━━━━"); !strings.Contains(scan, sgr("38", theme.Agents[1])) || strings.Contains(scan, sgr("38", theme.Agents[0])+"━") {
		t.Fatalf("not tests' workplace alone wipes:\n%s", ansi.Strip(out))
	}

	st := sampleState()
	st.CameraAt = now.Add(-cutTime / 3)
	out = Render(st, Options{Now: now}, 120, 30)
	checkSize(t, "wipe", out, 120, 30)
	scan := lineWith(out, "━━━━")
	if scan == "" || !strings.Contains(scan, sgr("38", theme.Agents[0])) {
		t.Fatalf("no scanline in the camera's colour:\n%s", ansi.Strip(out))
	}
	lines := strings.Split(ansi.Strip(out), "\n")
	if !strings.HasSuffix(strings.TrimRight(lines[len(lines)-2], " "), "│") {
		t.Errorf("the explorer must not wipe: %q", lines[len(lines)-2])
	}

	// The agent turning to its terminal plays the cut too where one pane shows at a time (80 cells),
	// not where its file and terminal stay side by side (120); a cut long gone does not.
	st.CameraAt = now.Add(-time.Second)
	st.Agents[0].FocusAt = now.Add(-cutTime / 2)
	if !strings.Contains(ansi.Strip(Render(st, Options{Now: now}, 80, 30)), "━━━━") {
		t.Error("no wipe after the agent turned to its terminal")
	}
	if strings.Contains(ansi.Strip(Render(st, Options{Now: now}, 120, 30)), "━━━━") {
		t.Error("a wipe as the agent turned between panes side by side")
	}
	st.Agents[0].FocusAt = now.Add(-cutTime)
	if strings.Contains(ansi.Strip(Render(st, Options{Now: now}, 80, 30)), "━━━━") {
		t.Error("the wipe outlives cutTime")
	}
}

// The camera's caret spins while its agent works: main by Options.Busy, others by their status.
func TestSpinningCaret(t *testing.T) {
	st := sampleState()
	mainBuf(st).Carets["main"].Sel = nil
	idle := Render(st, Options{Now: now}, 100, 20)
	busy := Render(st, Options{Now: now, Busy: true}, 100, 20)
	frame := spinFrame(now)
	if strings.Contains(ansi.Strip(idle), frame) || !strings.Contains(ansi.Strip(busy), frame) {
		t.Fatalf("editor caret: idle %v busy %v", strings.Contains(ansi.Strip(idle), frame), strings.Contains(ansi.Strip(busy), frame))
	}
	// No block: the bare frame in the agent's colour while it works, nothing while idle.
	if !styled(busy, frame, sgr("38", theme.Agents[0])) || reversed(busy) || reversed(idle) {
		t.Errorf("the caret is not a bare spin frame in the agent's colour")
	}
	// The frame takes the place of the character under the caret: the line keeps its width.
	if l := lineWith(busy, frame); ansi.StringWidth(l) != 100 || strings.Contains(ansi.Strip(l), "println") {
		t.Errorf("caret line %q", ansi.Strip(l))
	}

	st.Agents[0].Focus = ide.Terminal
	// The terminal's caret is lime, as its prompt.
	if out := Render(st, Options{Now: now, Busy: true}, 100, 20); !strings.Contains(ansi.Strip(out), "$ go te"+frame) ||
		!styled(out, frame, sgr("38", theme.Lime)) || reversed(out) {
		t.Errorf("terminal caret is not a bare lime spin frame:\n%s", ansi.Strip(out))
	}
	if out := Render(st, Options{Now: now}, 100, 20); !strings.Contains(ansi.Strip(out), "$ go te ") || reversed(out) {
		t.Errorf("idle terminal has a caret:\n%s", ansi.Strip(out))
	}
	sub := multiState()
	sub.Camera = "toolu_tests" // Done: idle
	if out := ansi.Strip(Render(sub, Options{Now: now, Busy: true}, 120, 30)); strings.Contains(out, "go test ./..."+frame) {
		t.Errorf("a finished agent's caret spins:\n%s", out)
	}
	sub.Agents[1].Status = ide.Running
	if out := ansi.Strip(Render(sub, Options{Now: now}, 120, 30)); !strings.Contains(out, frame) {
		t.Errorf("a running agent's caret does not spin:\n%s", out)
	}
}

// Typed lines flash green, as a diff's added lines, stay lit while their agent is on that tool
// call, and fade once it moves on. Only the background behind the text lights: no mark in the gutter, nothing past the
// end of the line.
func TestGlow(t *testing.T) {
	st := multiState()
	buf := mainBuf(st)
	buf.Glow = []ide.Glow{{Line: 0, Agent: "toolu_tests", At: now.Add(-100 * time.Millisecond)}}
	out := Render(st, Options{Now: now}, 120, 30)
	l := lineWith(out, "package main")
	lit := sgr("48", glow(buf.Glow[0], now))
	if strings.Contains(l, "▎") || !strings.Contains(l, lit) {
		t.Errorf("fresh line does not glow: %q", l)
	}
	c := lipgloss.NewCanvas(120, 1).Compose(lipgloss.NewLayer(l))
	plain := ansi.Strip(l)
	end := ansi.StringWidth(plain[:strings.Index(plain, "package main")+len("package main")])
	if cell := c.CellAt(end-1, 0); cell == nil || cell.Style.Bg == nil {
		t.Errorf("the last character does not glow: %q", l)
	}
	if cell := c.CellAt(end, 0); cell == nil || cell.Style.Bg != nil {
		t.Errorf("the glow goes on past the text: %q", l)
	}
	buf.Glow[0].At = now.Add(-time.Hour)
	held := theme.Mix(theme.Bg, theme.Green, glowHold)
	if l := lineWith(Render(st, Options{Now: now}, 120, 30), "package main"); !strings.Contains(l, sgr("48", held)) {
		t.Errorf("a line typed by the call on screen does not stay lit: %q", l)
	}
	buf.Glow[0].Off = now.Add(-glowLife)
	if l := lineWith(Render(st, Options{Now: now}, 120, 30), "package main"); strings.Contains(l, sgr("48", held)) {
		t.Errorf("faded line still glows: %q", l)
	}
	if glow(ide.Glow{At: now}, now.Add(-time.Second)) == nil {
		t.Error("a glow from the future (clock skew) should show at full strength")
	}
}

func TestOutputStyle(t *testing.T) {
	cases := map[string]string{
		"ok  	github.com/x/y	0.4s":          sgr("38", theme.OK),
		"--- FAIL: TestX (0.00s)":           sgr("38", theme.Err),
		"panic: runtime error":              sgr("38", theme.Err),
		"warning: unused variable":          sgr("38", theme.Warn),
		"all tests passed":                  sgr("38", theme.OK),
		"plain output":                      sgr("38", theme.Code),
		"Traceback (most recent call last)": sgr("38", theme.Err),
		"took 42 ms":                        sgr("38", theme.Number), // the rest by its tokens
		`name: "claudit"`:                   sgr("38", theme.String),
		"enabled: true":                     sgr("38", theme.Keyword),
		"Found 3 errors":                    sgr("38", theme.Err),
		"ValueError: bad value":             sgr("38", theme.Err),
		"./calc.go:12:2: undefined: x":      sgr("38", theme.Err),
		"calc.go:3:1: warning: unused x":    sgr("38", theme.Warn),
	}
	for line, want := range cases {
		if got := outputLine(sampleState(), line, line); !strings.Contains(got, want) {
			t.Errorf("%q: %q, want colour %s", line, got, want)
		}
	}
	// Saying there were none is no failure; a list is no diff, a clock no file.
	if got := outputLine(sampleState(), "x.py:12: DeprecationWarning: old", "x.py:12: DeprecationWarning: old"); !strings.Contains(got, sgr("38", theme.Warn)) {
		t.Errorf("a warning class is not yellow: %q", got)
	}
	for _, line := range []string{"Found 0 errors", "no failures, no warnings", "- a list item", "12:00:01: started",
		"Tests run: 5, Failures: 0, Errors: 0", "calc_test.go:12: computing"} {
		if got := outputLine(sampleState(), line, line); strings.Contains(got, sgr("38", theme.Err)) || strings.Contains(got, sgr("38", theme.Warn)) {
			t.Errorf("%q is coloured as a failure or a warning: %q", line, got)
		}
	}
}

// termWith renders sampleState's terminal holding lines.
func termWith(lines ...ide.TermLine) string {
	st := sampleState()
	st.Agents[0].Focus = ide.Terminal
	st.Agents[0].Term = ide.Term{Lines: lines}
	return Render(st, Options{}, 120, 30)
}

// A diff's changes go on green and red by their sign, their code in its colours, its hunks violet, the file's header in the
// colour of headings and the rest of its header grey; the lines around a change are code.
func TestTerminalDiff(t *testing.T) {
	out := termWith(
		ide.TermLine{Kind: ide.Meta, Text: "diff --git a/calc.go b/calc.go"},
		ide.TermLine{Kind: ide.Meta, Text: "index 1a2b3c4..5d6e7f8 100644"},
		ide.TermLine{Kind: ide.Hunk, Text: "@@ -1,4 +1,4 @@"},
		ide.TermLine{Kind: ide.Code, Text: " func Div(a, b int) (int, error) {", Lang: "calc.go"},
		ide.TermLine{Kind: ide.Removed, Text: "-    return a / b, nil"},
		ide.TermLine{Kind: ide.Added, Text: "+    return 0, errors.New(\"pass\")"},
	)
	for text, want := range map[string]string{
		"diff --git a/calc.go b/calc.go":      sgr("38", theme.Strong),
		"index 1a2b3c4..5d6e7f8 100644":       sgr("38", theme.Dim),
		"@@ -1,4 +1,4 @@":                     sgr("38", theme.Violet),
		"-    return a / b, nil":              sgr("48", theme.RemovedBg),
		"+    return 0, errors.New(\"pass\")": sgr("48", theme.AddedBg),
	} {
		if !styled(out, text, want) {
			t.Errorf("%q is not coloured %s:\n%q", text, want, lineWith(out, text))
		}
	}
	if l := lineWith(out, "func Div"); !strings.Contains(l, paint(" func Div(a, b int) (int, error) {", langOf("calc.go"))) {
		t.Errorf("the line around the change is not Go as the editor draws it: %q", l)
	}
}

// The text of a file is drawn as the editor draws its language, its errors no failure; a file of
// no language known shows as plain output.
func TestTerminalCode(t *testing.T) {
	code := `  return fmt.Errorf("x: %w", err) // fail` // short of a wrap
	out := termWith(ide.TermLine{Kind: ide.Code, Text: code, Lang: "internal/calc/calc.go"},
		ide.TermLine{Kind: ide.Code, Text: "FAIL TestDiv", Lang: "build.log"})
	if l := lineWith(out, "return fmt"); !strings.Contains(l, paint(code, langOf("calc.go"))) || strings.Contains(l, sgr("38", theme.Err)) {
		t.Errorf("code not drawn as the editor draws it: %q", l)
	}
	if !styled(out, "FAIL TestDiv", sgr("38", theme.Err)) {
		t.Errorf("a log's failure is not red: %q", lineWith(out, "FAIL"))
	}
}

// A search's hit says where it is, the file in its explorer colour and the line grey, then shows its
// line as code.
func TestTerminalHit(t *testing.T) {
	hit := "src/calc.go:12:func Div() {"
	out := termWith(ide.TermLine{Kind: ide.Match, Text: hit, Lang: "src/calc.go", Prefix: len("src/calc.go:12:")})
	l := lineWith(out, "func Div")
	if !styled(l, "src/calc.go", sgr("38", fileTint("src/calc.go"))) || !styled(l, ":12:", sgr("38", theme.Dim)) {
		t.Errorf("where the hit is: %q", l)
	}
	if !strings.Contains(l, paint("func Div() {", langOf("calc.go"))) {
		t.Errorf("the hit's line is not Go as the editor draws it: %q", l)
	}
	// A hit in a file of no language known says how things went, as plain output does.
	out = termWith(ide.TermLine{Kind: ide.Match, Text: "app.log:3:ERROR db down", Lang: "app.log", Prefix: len("app.log:3:")})
	if !styled(out, "ERROR db down", sgr("38", theme.Err)) {
		t.Errorf("a log's failure among hits is not red: %q", lineWith(out, "db down"))
	}
}

// Files named in plain output take their colour in the explorer, changed or not; a line that says
// how things went keeps its own colour.
func TestOutputFiles(t *testing.T) {
	st := sampleState()
	st.Edited = map[string]bool{root + "/src/main.go": true}
	line := `README.md  src/main.go  "docs/a.json". v1.2.3 0.4s file.go`
	got := outputLine(st, line, line)
	for name, path := range map[string]string{"README.md": root + "/README.md", "src/main.go": root + "/src/main.go",
		"docs/a.json": root + "/docs/a.json", "file.go": root + "/file.go"} {
		if !styled(got, name, sgr("38", fileTint(path))) {
			t.Errorf("%s not in its explorer colour: %q", name, got)
		}
	}
	if fileTint(root+"/src/main.go") != fileTint(root+"/x.go") {
		t.Error("a changed file changed colour")
	}
	if !styled(got, `"`, sgr("38", theme.String)) || !styled(got, "0.4s", sgr("38", theme.Number)) {
		t.Errorf("the rest lost its colours: %q", got)
	}
	if got := outputLine(st, "FAIL src/main.go", "FAIL src/main.go"); !styled(got, "FAIL src/main.go", sgr("38", theme.Err)) {
		t.Errorf("a failure is not red throughout: %q", got)
	}
}

// A terminal's text keeps a cell of air from its frame.
func TestTerminalAir(t *testing.T) {
	st := sampleState()
	st.Agents[0].Focus = ide.Terminal
	if l := ansi.Strip(lineWith(Render(st, Options{}, 100, 20), "go build")); !strings.Contains(l, "│ $ go build") {
		t.Errorf("terminal row %q", l)
	}
}

// In a terminal the prompt is lime, the commands typed after it white.
func TestTerminalCommandWhite(t *testing.T) {
	st := sampleState()
	st.Agents[0].Focus = ide.Terminal
	out := Render(st, Options{}, 100, 20)
	if !styled(out, "go build ./...", sgr("38", theme.Bright)) || !styled(out, "go te", sgr("38", theme.Bright)) || !styled(out, "$ ", sgr("38", theme.Lime)) {
		t.Errorf("prompt not lime or commands not white:\n%q", out)
	}
}

// The logo is at most 32×16 cells; the splash fits every size, and shows the whole logo or nothing.
func TestSplash(t *testing.T) {
	for _, l := range logo {
		if len(l) != len(logo[0]) || len(l) > 32 || (len(logo)+1)/2 > 16 {
			t.Fatalf("logo of %d×%d pixels, row %q", len(logo[0]), len(logo), l)
		}
	}
	for w := 0; w < 90; w += 3 {
		for h := 0; h < 14; h++ {
			lines := splash(w, h)
			if len(lines) != h {
				t.Fatalf("%dx%d: %d lines", w, h, len(lines))
			}
			rows := 0
			for _, l := range lines {
				if ansi.StringWidth(l) > w {
					t.Fatalf("%dx%d: line %q too wide", w, h, ansi.Strip(l))
				}
				if strings.ContainsAny(l, "▀▄") {
					rows++
				}
			}
			if fits := w >= len(logo[0]) && h >= 10; rows != 0 && rows != 10 || (rows == 10) != fits {
				t.Errorf("%dx%d: %d rows of the logo", w, h, rows)
			}
		}
	}
}

// A phase title is foreign text: tabs and line breaks must not break the row's width.
func TestPhaseRowClean(t *testing.T) {
	st := multiState()
	for _, a := range st.Agents {
		if a.Phase == "Check" {
			a.Phase = "Che\tck\nnow"
		}
	}
	out := Render(st, Options{Panel: Agents, Now: now}, 160, 30)
	checkSize(t, "phase", out, 160, 30)
}

// A long line wraps in the terminal as in a real one, up to maxWrap rows, then ends in "…".
func TestTerminalWraps(t *testing.T) {
	if got := wrap("abcdefghij", 3, 4); !reflect.DeepEqual(got, []string{"abc", "defg", "hij"}) {
		t.Errorf("wrap %q", got)
	}
	if got := wrap(strings.Repeat("y", 30), 4, 4); !reflect.DeepEqual(got, []string{"yyyy", "yyyy", "yyyy", "yyy…"}) {
		t.Errorf("long %q", got)
	}
	if got := wrap("世界世界", 1, 1); len(got) != maxWrap || got[0] != "世" { // a wide rune wider than the row still makes progress
		t.Errorf("wide %q", got)
	}
	if got := wrap("", 5, 5); !reflect.DeepEqual(got, []string{""}) {
		t.Errorf("empty %q", got)
	}
}
