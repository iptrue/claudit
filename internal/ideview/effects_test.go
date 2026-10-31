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
	// The terminal's caret is in the agent's colour too; 80 cells show the terminal alone.
	if out := Render(st, Options{Now: now, Busy: true}, 80, 20); !strings.Contains(ansi.Strip(out), "$ go te"+frame) ||
		!styled(out, frame, sgr("38", theme.Agents[0])) || reversed(out) {
		t.Errorf("terminal caret is not a bare spin frame in the agent's colour:\n%s", ansi.Strip(out))
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

// A diff's changes show their sign green or red and their code in its colours, on no background
// of their own; its hunks violet, the file's header in the colour of headings and the rest of its
// header grey; the lines around a change are code.
func TestTerminalDiff(t *testing.T) {
	out := termWith(
		ide.TermLine{Kind: ide.Meta, Text: "diff --git a/calc.go b/calc.go"},
		ide.TermLine{Kind: ide.Meta, Text: "index 1a2b3c4..5d6e7f8 100644"},
		ide.TermLine{Kind: ide.Hunk, Text: "@@ -1,4 +1,4 @@"},
		ide.TermLine{Kind: ide.Code, Text: " func Div(a, b int) (int, error) {", Lang: "calc.go"},
		ide.TermLine{Kind: ide.Removed, Text: "-    return a / b, nil", Lang: "calc.go"},
		ide.TermLine{Kind: ide.Added, Text: "+    return 0, errors.New(\"pass\")", Lang: "calc.go"},
	)
	for text, want := range map[string]string{
		"diff --git a/calc.go b/calc.go": sgr("38", theme.Strong),
		"index 1a2b3c4..5d6e7f8 100644":  sgr("38", theme.Dim),
		"@@ -1,4 +1,4 @@":                sgr("38", theme.Violet),
	} {
		if !styled(out, text, want) {
			t.Errorf("%q is not coloured %s:\n%q", text, want, lineWith(out, text))
		}
	}
	for sign, code := range map[string]string{"-": "    return a / b, nil", "+": "    return 0, errors.New(\"pass\")"} {
		c := theme.Red
		if sign == "+" {
			c = theme.Green
		}
		l := lineWith(out, code)
		if want := lipgloss.NewStyle().Foreground(c).Render(sign) + paint(code, langOf("calc.go")); !strings.Contains(l, want) {
			t.Errorf("%s%s: want its sign in %v, its code as the editor draws it:\n%q", sign, code, c, l)
		}
		if strings.Contains(l, "48;2;") {
			t.Errorf("%s%s has a background: %q", sign, code, l)
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

// What a row draws of its line (no runes terminals measure apart, tabs widened, broken bytes as
// one mark) moves none of its colours: a hit's place and code, a diffstat's bar, a row cut short.
func TestTerminalDrawnMarks(t *testing.T) {
	out := termWith(ide.TermLine{Kind: ide.Match, Text: "docs/❤️.go:3:func main() {}", Lang: "docs/❤️.go", Prefix: len([]rune("docs/❤️.go:3:"))})
	if l := lineWith(out, "❤.go:3:"); !styled(l, ":3:", sgr("38", theme.Dim)) || !strings.Contains(l, paint("func main() {}", goLang)) {
		t.Errorf("a hit in a file with an emoji in its name: %q", l)
	}
	green, red := sgr("38", theme.Green), sgr("38", theme.Red)
	out = termWith(ide.TermLine{Kind: ide.Stat, Text: " docs/❤️.md | 3 +++"}, ide.TermLine{Kind: ide.Stat, Text: " a\tb.go | 4 ++--"})
	if l := lineWith(out, "❤.md"); !styled(l, "+++", green) {
		t.Errorf("an emoji before the bar: %q", l)
	}
	if l := lineWith(out, "b.go"); !styled(l, "++", green) || !styled(l, "--", red) || styled(l, " 4", green) {
		t.Errorf("a tab before the bar: %q", l)
	}
	line := " calc.go | 42 " + strings.Repeat("+", 22) + strings.Repeat("-", 20)
	if l := statLine(sampleState(), "++++++---…", 30, line); !styled(l, "---", red) || styled(l, "++++++---", green) {
		t.Errorf("a row cut short: %q", l)
	}
	if cs, _ := layout("a\xff\ufe0fb", 10, 0); join(cs) != "a\ufffdb" {
		t.Errorf("a broken byte and a selector: %q", join(cs))
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

// git's marks are in git's colours: of a short status, the staged column green, the unstaged one
// and the untracked red; of a diffstat, the "+" green and the "-" red, wrapped rows too.
func TestTerminalGitMarks(t *testing.T) {
	st := sampleState()
	cam := st.Agents[0]
	cam.Focus = ide.Terminal
	long := " " + strings.Repeat("deep/", 12) + "calc.go | 42 " + strings.Repeat("+", 30) + strings.Repeat("-", 12)
	cam.Term.Lines = append(cam.Term.Lines,
		ide.TermLine{Kind: ide.Status, Text: "A  added.go"},
		ide.TermLine{Kind: ide.Status, Text: " D deleted.go"},
		ide.TermLine{Kind: ide.Status, Text: "?? new.txt"},
		ide.TermLine{Kind: ide.Stat, Text: " calc.go | 7 ++++++-"},
		ide.TermLine{Kind: ide.Stat, Text: long},
	)
	out := Render(st, Options{}, 100, 30)
	green, red := sgr("38", theme.Green), sgr("38", theme.Red)
	for _, c := range []struct{ line, text, color string }{
		{"added.go", "A", green}, {"deleted.go", "D", red}, {"new.txt", "?", red},
		{"| 7", "++++++", green}, {"| 7", "-", red},
	} {
		if l := lineWith(out, c.line); !styled(l, c.text, c.color) {
			t.Errorf("%q in %q: not in its colour: %q", c.text, c.line, l)
		}
	}
	// The long line wraps, its bar too: every row of it is in the bar's colours.
	if strings.Count(ansi.Strip(out), "deep/") < 12 || lineWith(out, "deep/") == lineWith(out, "+++") {
		t.Fatalf("the long diffstat line does not wrap before its bar:\n%s", ansi.Strip(out))
	}
	for _, l := range strings.Split(out, "\n") {
		plain := ansi.Strip(l)
		if strings.Contains(plain, "++") && !styled(l, "++", green) || strings.Contains(plain, "--") && !styled(l, "--", red) {
			t.Errorf("a row of a diffstat bar: %q", plain)
		}
	}
}

// A command shows in the shell's colours, a here-document's body in those of its language; what
// has no colour of its own is white, as typed, the line being typed too.
func TestTerminalCommandColours(t *testing.T) {
	st := sampleState()
	cam := st.Agents[0]
	cam.Focus = ide.Terminal
	cam.Term = ide.Term{Lines: []ide.TermLine{
		{Kind: ide.Command, Text: `for f in *.go; do echo "$f"; done`},
		{Kind: ide.Command, Text: "python3 - <<'EOF'"},
		{Kind: ide.More, Text: "def edit(p):", Lang: ".py"},
		{Kind: ide.More, Text: "EOF"},
	}, Input: `git commit -m "fix"`}
	out := Render(st, Options{}, 100, 30)
	kw, str, white := sgr("38", theme.Keyword), sgr("38", theme.String), sgr("38", theme.Bright)
	for _, c := range []struct{ text, color string }{
		{"for", kw}, {`"$f"`, str}, {" echo ", white}, {"python3 - <<", white}, {"'EOF'", str},
		{"def", kw}, {"edit", sgr("38", theme.Func)}, {"EOF", white}, {"git commit -m ", white}, {`"fix"`, str},
	} {
		if !styled(out, c.text, c.color) {
			t.Errorf("%q is not in its colour:\n%s", c.text, lineWith(out, c.text))
		}
	}
}

// While its agent types, selects or deletes text, a caret is a solid block of the agent's colour,
// the cell's own text dark on it, right after the text at the end of a line: in the editor, the
// terminal and the address bar. Once the agent has stopped for longer than an edit pauses, it spins.
func TestTypingCaret(t *testing.T) {
	st := sampleState()
	c := mainBuf(st).Carets["main"]
	c.Sel = nil
	st.Agents[0].TypedAt = now.Add(-100 * time.Millisecond)
	o := Options{Now: now, Busy: true}
	frame, block := spinFrame(now), sgr("48", theme.Agents[0])
	out := Render(st, o, 100, 20)
	if l := lineWith(out, "rintln"); strings.Contains(ansi.Strip(out), frame) || !styled(l, "p", sgr("38", theme.Bg), block) ||
		!strings.Contains(ansi.Strip(l), "println") {
		t.Errorf("the editor's caret is not a block over its cell: %q", l)
	}
	c.Pos.Col = 23 // the end of "\tprintln("hi") // greet"
	l := lineWith(Render(st, o, 100, 20), "greet")
	var before string
	for _, m := range sgrRe.FindAllStringIndex(l, -1) {
		if strings.Contains(l[m[0]:m[1]], block) {
			before = ansi.Strip(l[:m[0]])
			break
		}
	}
	if !strings.HasSuffix(before, "// greet") {
		t.Errorf("the block is not right after the end of the line: %q", l)
	}

	st.Agents[0].Focus = ide.Terminal
	if out := Render(st, o, 80, 20); strings.Contains(ansi.Strip(out), frame) || !styled(lineWith(out, "$ go te"), " ", block) {
		t.Errorf("the terminal's caret is not a block: %q", lineWith(out, "$ go te"))
	}
	br := browserState()
	br.Agents[0].Page, br.Agents[0].TypedAt = ide.Page{URL: "https://go.dev"}, st.Agents[0].TypedAt
	if out := Render(br, o, 100, 20); strings.Contains(ansi.Strip(out), frame) || !styled(lineWith(out, "go.dev"), " ", sgr("48", theme.OK)) {
		t.Errorf("the address bar's caret is not a block: %q", lineWith(out, "go.dev"))
	}

	st.Agents[0].Focus, st.Agents[0].TypedAt = ide.Editor, now.Add(-typeHold)
	if out := Render(st, o, 100, 20); !strings.Contains(ansi.Strip(out), frame) || strings.Contains(out, block) {
		t.Errorf("a caret that has stopped typing does not spin:\n%s", ansi.Strip(out))
	}
}

// What a command does goes grey above its prompt.
func TestTerminalComment(t *testing.T) {
	out := termWith(ide.TermLine{Kind: ide.Comment, Text: "# Run the tests"}, ide.TermLine{Kind: ide.Command, Text: "go test ./..."})
	if !styled(out, "# Run the tests", sgr("38", theme.Dim)) || strings.Contains(lineWith(out, "# Run"), "$") {
		t.Errorf("the comment is not grey, or has a prompt: %q", lineWith(out, "# Run"))
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

// In a terminal the prompt is in its agent's colour, the commands typed after it white.
func TestTerminalCommandWhite(t *testing.T) {
	st := sampleState()
	st.Agents[0].Focus = ide.Terminal
	out := Render(st, Options{}, 100, 20)
	if !styled(out, "go build ./...", sgr("38", theme.Bright)) || !styled(out, "go te", sgr("38", theme.Bright)) || !styled(out, "$ ", sgr("38", agentColor(st.Agents[0]))) {
		t.Errorf("prompt not in the agent's colour or commands not white:\n%q", out)
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
