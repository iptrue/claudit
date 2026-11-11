package ideview

import (
	"fmt"
	"image/color"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/ide"
	"github.com/pixelbender/claudit/internal/theme"
)

const root = "/home/dev/myproj"

func sampleState() *ide.State {
	main := &ide.Agent{
		Key: "main", Name: "main", Kind: ide.Main, Hotkey: 1,
		Term: ide.Term{
			Lines: []ide.TermLine{
				{Kind: ide.Command, Text: "go build ./..."},
				{Kind: ide.ErrorOutput, Text: "main.go:4: undefined: x"},
				{Kind: ide.Output, Text: "ok"},
			},
			Input: "go te",
		},
		File:    root + "/src/main.go",
		TreeSel: root + "/src/main.go",
		Focus:   ide.Editor,
	}
	return &ide.State{
		Root: root,
		Tree: []ide.TreeRow{
			{Depth: 0, Name: "docs", Path: root + "/docs", Dir: true},
			{Depth: 0, Name: "src", Path: root + "/src", Dir: true, Expanded: true},
			{Depth: 1, Name: "main.go", Path: root + "/src/main.go"},
			{Depth: 0, Name: "README.md", Path: root + "/README.md"},
		},
		Tabs: []*ide.Tab{
			{Path: root + "/src/main.go", Title: "main.go", Buf: ide.Buffer{
				Lines: []string{"package main", "", "func main() {", "\tprintln(\"hi\") // greet", "}"},
				Carets: map[string]*ide.Caret{"main": {
					Pos: ide.Pos{Line: 3, Col: 1},
					Sel: &ide.Selection{From: ide.Pos{Line: 3, Col: 1}, To: ide.Pos{Line: 3, Col: 8}},
				}},
				Modified: true,
			}},
			{Path: root + "/README.md", Title: "README.md", Buf: ide.Buffer{Lines: []string{"# hi"}}},
		},
		Agents: []*ide.Agent{main},
		Camera: "main",
	}
}

// mainBuf is the buffer of main.go in sampleState.
func mainBuf(st *ide.State) *ide.Buffer { return &st.FindTab(root + "/src/main.go").Buf }

func unicodeState() *ide.State {
	st := sampleState()
	cam := st.Agents[0]
	st.Root = "/tmp/project"
	st.Tree = []ide.TreeRow{
		{Depth: 0, Name: "documents", Path: "/tmp/project/documents", Dir: true, Expanded: true},
		{Depth: 1, Name: "文書ファイル.txt", Path: "/tmp/project/documents/文書ファイル.txt"},
		{Depth: 1, Name: "🎉party👨‍👩‍👧.md", Path: "/tmp/project/documents/🎉party👨‍👩‍👧.md"},
		{Depth: 1, Name: "tab\there\nnewline", Path: "/tmp/project/documents/tab"},
	}
	cam.TreeSel = st.Tree[1].Path
	st.Tabs = append(st.Tabs, &ide.Tab{Path: "/tmp/project/file.go", Title: "file.go 世界", Buf: ide.Buffer{
		Lines: []string{
			"package main",
			"\tfmt.Println(\"hello, 世界 🌍\") // comment",
			"a\tb\tc\t中\t👍🏽",
			"👨‍👩‍👧 family e\u0301 \x1b[31mesc\x1b[0m\r",
			"\xff\xfe broken utf8 \xe4\xb8",
			strings.Repeat("中", 100) + "END",
		},
		Carets: map[string]*ide.Caret{"main": {
			Pos: ide.Pos{Line: 1, Col: 16},
			Sel: &ide.Selection{From: ide.Pos{Line: 2, Col: 3}, To: ide.Pos{Line: 1, Col: 14}},
		}},
	}})
	cam.File = "/tmp/project/file.go"
	cam.Term.Lines = append(cam.Term.Lines,
		ide.TermLine{Kind: ide.Command, Text: "echo 'hello 世界 🎉'"},
		ide.TermLine{Kind: ide.Output, Text: "hello 世界 🎉\ttab"},
		ide.TermLine{Kind: ide.Output, Text: "\x1b[31mred\x1b[0m and\rcarriage\nnewline"},
		ide.TermLine{Kind: ide.ErrorOutput, Text: strings.Repeat("error 错误 ", 50)},
	)
	cam.Term.Input = "grep -rn 'test' 世界 " + strings.Repeat("z", 200)
	cam.Name = "main 🎉"
	cam.Activity = "Edit file.go\n世界"
	return st
}

// keycapState holds "1️⃣", which ansi.Truncate measures 1 cell wide and ansi.StringWidth 2.
func keycapState() *ide.State {
	st := multiState()
	st.Root = "/tmp/1️⃣proj"
	st.Tree[3].Name = "1️⃣ README.md"
	buf := mainBuf(st)
	buf.Lines = []string{"1️⃣ Install the dependencies 1️⃣ and configure 1️⃣ the virtual environment 1️⃣"}
	buf.Carets["main"].Pos, buf.Carets["main"].Sel = ide.Pos{}, nil
	st.Agents[0].Term.Lines = append(st.Agents[0].Term.Lines, ide.TermLine{Kind: ide.Output, Text: strings.Repeat("1️⃣ step ", 30)})
	for _, a := range st.Agents {
		a.Name = "1️⃣" + a.Name + "1️⃣"
		a.Activity = strings.Repeat("1️⃣ x", 10)
	}
	return st
}

var now = time.Unix(1000, 0) // spinner frame 0: UnixMilli/80 = 12500

// multiState is sampleState with a subagent editing main.go next to main, a workflow with phases
// and queued agents, and touched files.
func multiState() *ide.State {
	st := sampleState()
	st.Agents[0].Started = now.Add(-12 * time.Second)
	st.Agents[0].Activity = "Edit main.go"
	tests := &ide.Agent{
		Key: "toolu_tests", Name: "tests", Kind: ide.Subagent, Parent: "main", Hotkey: 2, Color: 1,
		Status: ide.Done, Activity: "Read main.go", Started: now.Add(-5 * time.Minute), Ended: now.Add(-2*time.Minute + 5*time.Second),
		Term: ide.Term{Lines: []ide.TermLine{{Kind: ide.Command, Text: "go test ./..."}}},
		File: root + "/src/main.go", TreeSel: root + "/README.md", Focus: ide.Terminal,
	}
	wf := &ide.Agent{Key: "toolu_wf", Name: "build", Kind: ide.Workflow, Parent: "main", Color: 2, Started: now.Add(-45 * time.Second)}
	lint := &ide.Agent{
		Key: "a1", Name: "lint", Kind: ide.WorkflowAgent, Parent: "toolu_wf", Phase: "Check", Hotkey: 3, Color: 3,
		Activity: "Bash go vet", Started: now.Add(-3 * time.Second),
		Term: ide.Term{Running: true},
	}
	docs := &ide.Agent{Key: "a2", Name: "docs", Kind: ide.WorkflowAgent, Parent: "toolu_wf", Phase: "Write", Hotkey: 4, Color: 4, Status: ide.Failed}
	q1 := &ide.Agent{Key: "toolu_wf#3", Name: "fmt", Kind: ide.WorkflowAgent, Parent: "toolu_wf", Phase: "Check", Color: 5, Status: ide.Queued}
	q2 := &ide.Agent{Key: "toolu_wf#4", Name: "race", Kind: ide.WorkflowAgent, Parent: "toolu_wf", Phase: "Check", Color: 6, Status: ide.Queued}
	vet := &ide.Agent{Key: "a5", Name: "vet", Kind: ide.WorkflowAgent, Parent: "toolu_wf", Phase: "Check", Hotkey: 5, Color: 7}
	st.Agents = append(st.Agents, tests, wf, lint, docs, q1, q2, vet)
	buf := mainBuf(st)
	buf.Carets["toolu_tests"] = &ide.Caret{
		Pos: ide.Pos{Line: 2, Col: 4},
		Sel: &ide.Selection{From: ide.Pos{Line: 2, Col: 0}, To: ide.Pos{Line: 2, Col: 4}},
	}
	buf.Carets["a2"] = &ide.Caret{Pos: ide.Pos{Line: 4, Col: 1}}
	docs.File = root + "/src/main.go"
	st.Touched = map[string]string{root + "/src/main.go": "toolu_tests", root + "/README.md": "main"}
	return st
}

// checkSize asserts that out is exactly h lines of exactly w cells.
func checkSize(t *testing.T, name string, out string, w, h int) {
	t.Helper()
	if h == 0 {
		if out != "" {
			t.Fatalf("%s %dx%d: want empty output, got %q", name, w, h, out)
		}
		return
	}
	lines := strings.Split(out, "\n")
	if len(lines) != h {
		t.Fatalf("%s %dx%d: got %d lines", name, w, h, len(lines))
	}
	for i, l := range lines {
		if got := ansi.StringWidth(l); got != w {
			t.Fatalf("%s %dx%d: line %d is %d cells wide: %q", name, w, h, i, got, ansi.Strip(l))
		}
		// The renderer counts by wcwidth where the terminal does not report its own (iTerm2).
		if got := ansi.StringWidthWc(l); got != w {
			t.Fatalf("%s %dx%d: line %d is %d cells by wcwidth: %q", name, w, h, i, got, ansi.Strip(l))
		}
	}
}

// sgr is the SGR parameter of c as a foreground ("38") or background ("48") colour.
func sgr(layer string, c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("%s;2;%d;%d;%d", layer, r>>8, g>>8, b>>8)
}

var sgrRe = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// styled reports whether out prints text right after an SGR sequence that sets every one of params.
func styled(out, text string, params ...string) bool {
	for _, m := range sgrRe.FindAllStringSubmatchIndex(out, -1) {
		if !strings.HasPrefix(out[m[1]:], text) {
			continue
		}
		have := ";" + out[m[2]:m[3]] + ";"
		if !slices.ContainsFunc(params, func(p string) bool { return !strings.Contains(have, ";"+p+";") }) {
			return true
		}
	}
	return false
}

// reversed reports whether out turns reverse video on anywhere.
func reversed(out string) bool {
	for _, m := range sgrRe.FindAllStringSubmatch(out, -1) {
		ps := strings.Split(m[1], ";")
		for i := 0; i < len(ps); i++ {
			switch ps[i] {
			case "38", "48", "58": // an extended colour: skip its arguments
				if i+1 < len(ps) && ps[i+1] == "5" {
					i += 2
				} else {
					i += 4
				}
			case "7":
				return true
			}
		}
	}
	return false
}

// lineWith returns the first line of out whose plain text contains s.
func lineWith(out, s string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(ansi.Strip(l), s) {
			return l
		}
	}
	return ""
}

var trailingSGR = regexp.MustCompile(`(\x1b\[[0-9;]*m)+$`)

// left returns the left pane of a w-wide render, with its styles.
func left(out string, w int) string {
	ew := min(max(w/5, 18), 32, w/2)
	// The agents panel is wider than the explorer: the left pane ends at its first top corner.
	if top := ansi.Strip(strings.SplitN(out, "\n", 2)[0]); strings.Contains(top, "╮") {
		ew = ansi.StringWidth(top[:strings.Index(top, "╮")]) + 1
	}
	var rows []string
	for _, l := range strings.Split(out, "\n") {
		// ansi.Cut keeps the escape sequences of the cut-off part.
		rows = append(rows, trailingSGR.ReplaceAllString(ansi.Cut(l, 0, ew), ""))
	}
	return strings.Join(rows, "\n")
}

// leftRows returns the plain rows inside the left pane's border.
func leftRows(out string, w int) []string {
	var rows []string
	for _, l := range strings.Split(ansi.Strip(left(out, w)), "\n") {
		rows = append(rows, strings.TrimSuffix(strings.TrimPrefix(l, "│"), "│"))
	}
	return rows[1 : len(rows)-1]
}

func render(st *ide.State, w, h int) string { return Render(st, Options{}, w, h) }

func states() map[string]*ide.State {
	running := sampleState()
	running.Agents[0].Term.Running = true
	running.Agents[0].Term.Spinner = 7
	running.Agents[0].Focus = ide.Terminal
	noFile := sampleState()
	noFile.Tabs = nil
	noFile.Agents[0].Focus = ide.Explorer
	unknownCam := multiState()
	unknownCam.Camera = "gone"
	browsing := browserState()
	browsing.Agents[0].Page.LoadAt = now.Add(-time.Second)
	return map[string]*ide.State{
		"browser":     browserState(),
		"loading":     browsing,
		"zero":        {},
		"sample":      sampleState(),
		"unicode":     unicodeState(),
		"running":     running,
		"no file":     noFile,
		"multi":       multiState(),
		"keycap":      keycapState(),
		"unknown cam": unknownCam,
	}
}

func options() map[string]Options {
	return map[string]Options{
		"files":          {},
		"files focused":  {Focused: true, Now: now},
		"agents":         {Panel: Agents, Now: now},
		"agents focused": {Panel: Agents, Focused: true, Sel: "a1", Now: now},
	}
}

func TestRenderSize(t *testing.T) {
	sizes := [][2]int{{0, 0}, {1, 1}, {0, 5}, {5, 0}, {2, 2}, {3, 3}, {10, 3}, {17, 4}, {40, 10}, {80, 24}, {120, 40}, {200, 60}, {1, 60}, {300, 1}}
	for name, st := range states() {
		for oname, o := range options() {
			for _, s := range sizes {
				checkSize(t, name+"/"+oname, Render(st, o, s[0], s[1]), s[0], s[1])
			}
		}
	}
}

func TestRenderSizeSweep(t *testing.T) {
	sts := map[string]*ide.State{"sample": sampleState(), "unicode": unicodeState(), "keycap": keycapState(), "multi": multiState()}
	opts := map[string]Options{"files": {}, "agents": options()["agents focused"]}
	for name, st := range sts {
		for oname, o := range opts {
			for w := 0; w <= 70; w++ {
				for h := 0; h <= 16; h++ {
					checkSize(t, name+"/"+oname, Render(st, o, w, h), w, h)
				}
			}
		}
	}
}

func TestExplorer(t *testing.T) {
	out := ansi.Strip(render(sampleState(), 100, 20))
	for _, want := range []string{"myproj", "▸ docs", "▾ src", "    main.go", "  README.md"} {
		if !strings.Contains(out, want) {
			t.Errorf("explorer lacks %q:\n%s", want, out)
		}
	}
}

func TestExplorerSelection(t *testing.T) {
	st := sampleState()
	st.Tree = nil
	for i := range 300 {
		st.Tree = append(st.Tree, ide.TreeRow{Name: fmt.Sprintf("file%03d.go", i), Path: fmt.Sprintf("/p/file%03d.go", i)})
	}
	cam := st.Agents[0]
	cam.TreeSel = "/p/file250.go"
	cam.Focus = ide.Explorer
	out := render(st, 100, 20)
	if s := ansi.Strip(out); !strings.Contains(s, "file250.go") || strings.Contains(s, "file000.go") {
		t.Fatalf("explorer is not scrolled to the selection:\n%s", s)
	}
	if l := lineWith(out, "file250.go"); !strings.Contains(l, sgr("48", theme.SelBg)) {
		t.Errorf("selected row is not on SelBg: %q", l)
	}

	// Working elsewhere, the agent's row keeps a dimmer blue.
	cam.Focus = ide.Editor
	l := lineWith(render(st, 100, 20), "file250.go")
	if strings.Contains(l, sgr("48", theme.SelBg)) || !strings.Contains(l, sgr("48", theme.SelBgDim)) {
		t.Errorf("unfocused selection should be on SelBgDim, not SelBg: %q", l)
	}
}

// File names take the colour of their type as a pastel, with no icon, never bold; once an agent
// changed a file its name keeps that colour and a yellow asterisk follows it.
func TestFileColors(t *testing.T) {
	st := sampleState()
	goC, mdC := fileColor("main.go"), fileColor("README.md")
	if goC == nil || mdC == nil || goC == mdC || fileColor("notes.txt") != nil {
		t.Fatalf("file colours: go %v md %v", goC, mdC)
	}
	if fileColor("sub/Dockerfile") == nil || fileColor("A.GO") != goC {
		t.Errorf("by name or upper case: no colour")
	}
	pastel := func(c color.Color) color.Color { return theme.Mix(c, theme.Bright, 0.4) }
	out := left(render(st, 100, 20), 100)
	if s := ansi.Strip(out); strings.Contains(s, "▪") || strings.Contains(s, "*") || !styled(out, "main.go", sgr("38", pastel(goC))) ||
		!styled(out, "README.md", sgr("38", pastel(mdC))) || !styled(out, "src", sgr("38", theme.Fg)) || !styled(out, "▾ ", sgr("38", theme.Dim)) {
		t.Errorf("files not in their type's colour, dirs not neutral, or an asterisk on nothing changed:\n%s", out)
	}
	// A changed file gets a yellow asterisk, every folder above it a yellow name and no asterisk;
	// a folder whose name only starts the same stays as it is.
	st.Edited = map[string]bool{st.Root + "/src/main.go": true, st.Root + "/srcx.go": true}
	out = left(render(st, 100, 20), 100)
	if !strings.Contains(ansi.Strip(lineWith(out, "main.go")), "main.go *") || strings.Contains(ansi.Strip(lineWith(out, "▾ src")), "*") ||
		!styled(out, " *", sgr("38", theme.Yellow)) || strings.Count(ansi.Strip(out), "*") != 1 || !styled(out, "src", sgr("38", theme.Yellow)) ||
		!styled(out, "main.go", sgr("38", pastel(goC))) || !styled(out, "README.md", sgr("38", pastel(mdC))) {
		t.Errorf("a changed file has no yellow asterisk, its folder no yellow name, or an unchanged one changed:\n%s", out)
	}
	// Squeezed, the name gives way to the asterisk.
	st.Tree[2] = ide.TreeRow{Depth: 1, Name: "a_rather_long_file_name.go", Path: st.Root + "/src/main.go"}
	if l := ansi.Strip(lineWith(left(render(st, 100, 20), 100), "a_rather")); !strings.Contains(l, "… *│") {
		t.Errorf("a long changed name hides its asterisk: %q", l)
	}
	// Hidden and ignored folders and files step back, a changed file keeping its asterisk; a changed
	// folder is yellow all the same.
	st.Tree = append(st.Tree, ide.TreeRow{Name: ".git", Path: st.Root + "/.git", Dir: true},
		ide.TreeRow{Name: ".env", Path: st.Root + "/.env"}, ide.TreeRow{Name: ".gitignore", Path: st.Root + "/.gitignore"},
		ide.TreeRow{Name: "dist", Path: st.Root + "/dist", Dir: true, Ignored: true}, ide.TreeRow{Name: "app.log", Path: st.Root + "/app.log", Ignored: true},
		ide.TreeRow{Name: "build", Path: st.Root + "/build", Dir: true, Ignored: true})
	st.Edited[st.Root+"/.gitignore"], st.Edited[st.Root+"/build/out.bin"] = true, true
	out = left(render(st, 100, 20), 100)
	if !styled(out, ".git", sgr("38", theme.Dim)) || !styled(out, ".env", sgr("38", theme.Dim)) || !styled(out, "README.md", sgr("38", pastel(mdC))) ||
		!styled(out, ".gitignore", sgr("38", theme.Dim)) || !strings.Contains(ansi.Strip(lineWith(out, ".gitignore")), ".gitignore *") ||
		!styled(out, "dist", sgr("38", theme.Dim)) || !styled(out, "app.log", sgr("38", theme.Dim)) || !styled(out, "build", sgr("38", theme.Yellow)) {
		t.Errorf("hidden or ignored rows not dim, a plain one dim, or a changed one unmarked:\n%s", out)
	}
}

// TreeAt finds the explorer row under a click, scrolled or not; nothing outside the tree.
func TestTreeAt(t *testing.T) {
	st := sampleState()
	st.Tree = []ide.TreeRow{{Name: "dir", Path: "/p/dir", Dir: true}}
	for i := range 300 {
		st.Tree = append(st.Tree, ide.TreeRow{Name: fmt.Sprintf("file%03d.go", i), Path: fmt.Sprintf("/p/file%03d.go", i)})
	}
	if path, dir := TreeAt(st, Options{}, 100, 20, 3, 1); path != "/p/dir" || !dir {
		t.Fatalf("folder row: %q dir %v", path, dir)
	}
	st.Agents[0].TreeSel = "/p/file250.go"
	for _, o := range []Options{{}, {Scroll: Scroll{filesPane: 7}}} {
		lines := strings.Split(ansi.Strip(Render(st, o, 100, 20)), "\n")
		for y := 1; y < 19; y++ {
			got, dir := TreeAt(st, o, 100, 20, 3, y)
			if want := "/p/" + strings.TrimSpace(lines[y][3:16]); got != want || dir {
				t.Fatalf("scroll %d row %d: %q dir %v, want %q", o.Scroll[filesPane], y, got, dir, want)
			}
		}
	}
	for _, at := range [][2]int{{3, 0}, {3, 19}, {60, 5}, {-1, 5}} {
		if got, _ := TreeAt(st, Options{}, 100, 20, at[0], at[1]); got != "" {
			t.Errorf("(%d, %d): %q", at[0], at[1], got)
		}
	}
	if got, _ := TreeAt(st, Options{Panel: Agents}, 100, 20, 3, 5); got != "" {
		t.Errorf("agents panel: %q", got)
	}
}

// A file the user opened fills the right column alone, headed by its path and by who opened it and
// how to go back, in as much as the column has room for.
func TestViewedFile(t *testing.T) {
	st := multiState()
	st.Viewed = root + "/README.md"
	top := strings.Split(ansi.Strip(render(st, 160, 30)), "\n")[0]
	if strings.Count(top, "╭") != 2 || !strings.Contains(top, "╭─ README.md opened by you · alt+0 back to agent ─") {
		t.Errorf("right column is not the viewed file alone: %q", top)
	}
	for _, c := range []struct {
		room int
		want string
	}{{60, "opened by you · alt+0 back to agent "}, {59, "opened by you "}, {26, "opened by you "}, {25, ""}} {
		if got := ansi.Strip(heldLabel(c.room)); got != c.want {
			t.Errorf("room %d: %q, want %q", c.room, got, c.want)
		}
	}
}

// The file the user opened is the explorer's selection, in focus, instead of the camera's row.
func TestViewedFileSelected(t *testing.T) {
	st := sampleState()
	st.Viewed = root + "/README.md"
	out := left(render(st, 100, 20), 100)
	if l := lineWith(out, "README.md"); !strings.Contains(l, sgr("48", theme.SelBg)) {
		t.Errorf("viewed file is not on SelBg: %q", l)
	}
	if l := lineWith(out, "main.go"); strings.Contains(l, sgr("48", theme.SelBgDim)) || strings.Contains(l, sgr("48", theme.SelBg)) {
		t.Errorf("the camera's row is still selected: %q", l)
	}
}

// The explorer marks no file with a dot: an asterisk after its name tells an agent changed it.
func TestExplorerHasNoDots(t *testing.T) {
	if out := ansi.Strip(left(render(multiState(), 120, 24), 120)); strings.Contains(out, "●") {
		t.Errorf("a dot in the explorer:\n%s", out)
	}
}

func TestEditor(t *testing.T) {
	out := ansi.Strip(render(sampleState(), 100, 20))
	for _, want := range []string{
		" 1 package main",
		" 3 func main() {",
		" 4     println(\"hi\") // greet",
		"╭─ src/main.go ─",
		"README.md",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("editor lacks %q:\n%s", want, out)
		}
	}
}

func TestEditorShowsCameraFile(t *testing.T) {
	st := sampleState()
	// The camera's file, not the last tab.
	if out := ansi.Strip(render(st, 100, 20)); !strings.Contains(out, "package main") {
		t.Fatalf("camera's file is not shown:\n%s", out)
	}
	// No file: the last tab.
	st.Agents[0].File = ""
	if out := ansi.Strip(render(st, 100, 20)); !strings.Contains(out, " 1 # hi") || strings.Contains(out, "package main") {
		t.Fatalf("last tab is not shown:\n%s", out)
	}
}

func TestEditorCursorFarDown(t *testing.T) {
	st := sampleState()
	var lines []string
	for i := range 5000 {
		lines = append(lines, fmt.Sprintf("row%d", i))
	}
	*mainBuf(st) = ide.Buffer{Lines: lines, Carets: map[string]*ide.Caret{"main": {Pos: ide.Pos{Line: 4321, Col: 2}}}}
	out := ansi.Strip(render(st, 100, 30))
	if !strings.Contains(out, "4322 row4321") || strings.Contains(out, " 1 row0 ") {
		t.Fatalf("cursor line is not visible:\n%s", out)
	}
}

func TestEditorCursorFarRight(t *testing.T) {
	cases := []struct {
		name string
		line string
		col  int
		want string // around the caret; %s is the spin frame that replaces the cell under it
	}{
		{"ascii", strings.Repeat("x", 300) + "TARGET" + strings.Repeat("y", 300), 300, "%sARGET"},
		{"wide", strings.Repeat("中", 200) + "END", 200, "%sND"},
		{"tabs", strings.Repeat("\t", 60) + "deep", 60, "%seep"},
		{"end of line", strings.Repeat("z", 300) + "tail", 304, "tail %s"}, // a space apart from the text
	}
	frame := spinFrame(now)
	for _, c := range cases {
		st := sampleState()
		*mainBuf(st) = ide.Buffer{Lines: []string{"first", c.line}, Carets: map[string]*ide.Caret{"main": {Pos: ide.Pos{Line: 1, Col: c.col}}}}
		out := Render(st, Options{Now: now, Busy: true}, 100, 20)
		checkSize(t, c.name, out, 100, 20)
		if want, s := fmt.Sprintf(c.want, frame), ansi.Strip(out); !strings.Contains(s, want) {
			t.Errorf("%s: %q is not visible:\n%s", c.name, want, s)
		}
		if !styled(out, frame, sgr("38", theme.Agents[0])) {
			t.Errorf("%s: no caret in the agent's colour", c.name)
		}
	}
}

// A hex dump is not code, whatever the file's name says.
func TestEditorHexPlain(t *testing.T) {
	st := sampleState()
	tb := st.FindTab(root + "/src/main.go")
	tb.Buf = ide.Buffer{Lines: []string{"00000000  66 75 6e 63  |func|"}}
	if out := render(st, 100, 20); !styled(out, "func", sgr("38", theme.Keyword)) {
		t.Fatalf("the Go file's keyword is not coloured: %q", lineWith(out, "func"))
	}
	tb.Hex = true
	if out := render(st, 100, 20); styled(out, "func", sgr("38", theme.Keyword)) || styled(out, "66", sgr("38", theme.Number)) {
		t.Errorf("the hex dump is coloured as Go: %q", lineWith(out, "func"))
	}
}

func TestEditorUnicode(t *testing.T) {
	out := ansi.Strip(render(unicodeState(), 160, 30))
	for _, want := range []string{
		"╭─ file.go ─",
		"fmt.Println(\"hello, 世界 🌍\") // comment",
		"a    b    c    中    👍",  // without the skin tone, which terminals measure apart
		"👨👩👧 family e\u0301 esc", // without the joiners
		"\uFFFD\uFFFD broken utf8 \uFFFD",
		"文書ファイル.txt",
		"🎉party👨👩👧.md",
		"tab    herenewline",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
}

func TestCarets(t *testing.T) {
	st := multiState()
	findAgent(st, "a2").Activity = "Edit main.go" // failed, but still playing its edit
	out := render(st, 120, 30)
	plain := ansi.Strip(out)
	if strings.Contains(plain, "◂") {
		t.Errorf("a caret has a name beside it:\n%s", plain)
	}
	tests, docs := theme.Agents[1], theme.Agents[4]
	checks := []struct {
		what, text string
		params     []string
	}{
		{"tests' selection, red as what goes", "func", []string{sgr("48", theme.RemovedBg)}},
		{"main's selection, red as what goes", "println", []string{sgr("48", theme.RemovedBg)}},
	}
	for _, c := range checks {
		if !styled(out, c.text, c.params...) {
			t.Errorf("no %s (%q with %v)", c.what, c.text, c.params)
		}
	}
	frame := spinFrame(time.Time{})
	if reversed(out) || strings.Contains(plain, frame) ||
		styled(out, " ", sgr("48", tests)) || styled(out, " ", sgr("48", docs)) {
		t.Errorf("a caret is a block or spins while idle:\n%s", plain)
	}
	// A working agent's caret is the spin frame in its colour, over the character under it.
	findAgent(st, "toolu_tests").Status = ide.Running
	out = render(st, 120, 30)
	if !strings.Contains(ansi.Strip(out), " 3 func"+frame+"main() {") || !styled(out, frame, sgr("38", tests)) {
		t.Errorf("a working agent's caret does not spin:\n%s", ansi.Strip(out))
	}
	findAgent(st, "toolu_tests").Status = ide.Done

	// The camera on tests (idle: no caret of its own); main's caret spins while it works.
	st.Camera = "toolu_tests"
	st.Agents[1].Focus = ide.Editor
	out = render(st, 120, 30)
	busy := Render(st, Options{Busy: true}, 120, 30)
	if reversed(out) || strings.Contains(ansi.Strip(out), frame) || !styled(busy, frame, sgr("38", theme.Agents[0])) ||
		!strings.Contains(ansi.Strip(busy), frame+"rintln") || !styled(busy, "rintln", sgr("48", theme.RemovedBg)) {
		t.Errorf("camera switch does not swap the carets:\n%s", ansi.Strip(busy))
	}

	// An agent that has finished and played everything leaves no caret behind; main stays.
	red := sgr("48", theme.RemovedBg)
	mainBuf(st).Carets["a2"].Sel = &ide.Selection{From: ide.Pos{Line: 4, Col: 0}, To: ide.Pos{Line: 4, Col: 1}}
	if out = render(st, 120, 30); !styled(out, "}", red) {
		t.Errorf("docs, still playing, has no selection:\n%s", ansi.Strip(out))
	}
	findAgent(st, "a2").Activity = ""
	st.Agents[0].Status = ide.Done
	out = render(st, 120, 30)
	if styled(out, "}", red) || !styled(out, "println", red) {
		t.Errorf("finished docs still has a caret, or main lost its:\n%s", ansi.Strip(out))
	}
}

// A pane is headed by what it shows: the file's path from the root, or "terminal"; with several
// agents about, the name of the agent in its colour first.
func TestTitles(t *testing.T) {
	st := sampleState()
	if top := ansi.Strip(strings.Split(render(st, 120, 30), "\n")[0]); !strings.Contains(top, "╭─ src/main.go ─") {
		t.Errorf("one agent's editor: %q", top)
	}
	st.Agents[0].Focus = ide.Terminal
	if top := ansi.Strip(strings.Split(render(st, 120, 30), "\n")[0]); !strings.Contains(top, "╭─ terminal ─") {
		t.Errorf("one agent's terminal: %q", top)
	}
	multi := multiState()
	top := strings.Split(render(multi, 140, 30), "\n")[0]
	if s := ansi.Strip(top); !strings.Contains(s, "╭─ main · src/main.go ─") || !strings.Contains(s, "╭─ tests · terminal ─") {
		t.Errorf("several agents: %q", s)
	}
	if !styled(top, "tests", sgr("38", theme.Agents[1])) || !styled(top, " · ", sgr("38", theme.Dim)) {
		t.Errorf("the name is not in its agent's colour: %q", top)
	}
}

func TestShortPath(t *testing.T) {
	const root = "/w"
	for _, tt := range []struct {
		path string
		w    int
		want string
	}{
		{"/w/main.go", 40, "main.go"},
		{"/w/internal/ui/view.go", 40, "internal/ui/view.go"},
		{"/w/internal/ui/deep/er/model_mouse.go", 30, "internal/…/er/model_mouse.go"},
		{"/w/internal/ui/deep/er/model_mouse.go", 25, "internal/…/model_mouse.go"},
		{"/w/internal/ui/model_mouse.go", 18, "…/model_mouse.go"},
		{"/w/internal/a_very_long_file_name.go", 12, "…ile_name.go"},
		{"/elsewhere/x.go", 40, "/elsewhere/x.go"},
	} {
		got := shortPath(tt.path, root, tt.w)
		if got != tt.want || ansi.StringWidth(got) > tt.w {
			t.Errorf("shortPath(%q, %d) = %q, want %q", tt.path, tt.w, got, tt.want)
		}
	}
}

func TestTerminal(t *testing.T) {
	st := sampleState()
	st.Agents[0].Focus = ide.Terminal
	term := &st.Agents[0].Term
	for i := range 50 {
		term.Lines = append(term.Lines, ide.TermLine{Kind: ide.Output, Text: fmt.Sprintf("out%02d", i)})
	}
	out := ansi.Strip(render(st, 100, 30))
	for _, want := range []string{"main", "out49", "$ go te"} {
		if !strings.Contains(out, want) {
			t.Errorf("terminal lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "out00") {
		t.Errorf("terminal is not tail-aligned:\n%s", out)
	}

	st = sampleState()
	st.Agents[0].Focus = ide.Terminal
	term = &st.Agents[0].Term
	out = ansi.Strip(render(st, 100, 30))
	for _, want := range []string{"$ go build ./...", "main.go:4: undefined: x", "ok"} {
		if !strings.Contains(out, want) {
			t.Errorf("terminal lacks %q:\n%s", want, out)
		}
	}
	// Tail-aligned: the live prompt line sits right above the bottom border.
	lines := strings.Split(out, "\n")
	if !strings.Contains(lines[len(lines)-2], "$ go te") {
		t.Errorf("live line is not at the bottom: %q", lines[len(lines)-2])
	}

	term.Running = true
	term.Spinner = 12
	out = ansi.Strip(render(st, 100, 30))
	lines = strings.Split(out, "\n")
	if last := lines[len(lines)-2]; !strings.Contains(last, spinner[2]) || strings.Contains(last, "go te") {
		t.Errorf("running terminal shows %q", last)
	}
	if !styled(render(st, 100, 30), spinner[2], sgr("38", theme.Fg)) {
		t.Errorf("the spinner of a running command is not light grey")
	}
}

// A command running for a while shows beside its spinner how long, as the status line counts;
// the first seconds, and a command whose start is unknown, show none.
func TestTerminalRunTime(t *testing.T) {
	st := sampleState()
	st.Agents[0].Focus = ide.Terminal
	term := &st.Agents[0].Term
	term.Running = true
	live := func(start time.Time) string {
		term.RunAt = start
		lines := strings.Split(ansi.Strip(Render(st, Options{Now: now}, 100, 30)), "\n")
		return lines[len(lines)-2]
	}
	for _, start := range []time.Time{now.Add(-2 * time.Second), {}} {
		if l := live(start); strings.ContainsAny(l, "0123456789") {
			t.Errorf("started at %v, the live line shows %q", start, l)
		}
	}
	for ran, want := range map[time.Duration]string{3 * time.Second: "3s", 72 * time.Second: "1m12s", 2*time.Hour + 3*time.Minute: "2h03m"} {
		if l := live(now.Add(-ran)); !strings.Contains(l, spinner[0]+" "+want) {
			t.Errorf("after %v the live line is %q, want %q", ran, l, spinner[0]+" "+want)
		}
	}
	term.RunAt = now.Add(-2*time.Hour - 3*time.Minute)
	if !styled(Render(st, Options{Now: now}, 100, 30), "2h03m", sgr("38", theme.Dim)) {
		t.Error("the time is not grey")
	}
}

// A command of several lines has the prompt before its first alone: the lines after it, and the
// input that goes on with it, stand under its text.
func TestTerminalMultiline(t *testing.T) {
	st := sampleState()
	st.Agents[0].Focus = ide.Terminal
	term := &st.Agents[0].Term
	term.Lines = []ide.TermLine{{Kind: ide.Command, Text: "cat <<EOF"}, {Kind: ide.More, Text: "hi"}}
	term.Input = "EOF"
	lines := strings.Split(ansi.Strip(render(st, 100, 30)), "\n")
	col := func(s, sub string) int {
		if i := strings.Index(s, sub); i >= 0 {
			return ansi.StringWidth(s[:i])
		}
		return -1
	}
	cat, hi, live := lines[len(lines)-4], lines[len(lines)-3], lines[len(lines)-2]
	x := col(cat, "cat <<EOF")
	if col(cat, "$ cat") != x-2 || col(hi, "hi") != x || col(live, "EOF") != x || strings.Contains(hi+live, "$") {
		t.Errorf("multiline command:\n%s\n%s\n%s", cat, hi, live)
	}
	term.Lines = append(term.Lines, ide.TermLine{Kind: ide.Gap}) // done: the next input is a command of its own
	if live := strings.Split(ansi.Strip(render(st, 100, 30)), "\n"); !strings.Contains(live[len(live)-2], "$ EOF") {
		t.Errorf("a new command has no prompt: %q", live[len(live)-2])
	}
}

func TestTerminalUnicode(t *testing.T) {
	st := unicodeState()
	st.Agents[0].Focus = ide.Terminal
	out := ansi.Strip(render(st, 160, 40))
	for _, want := range []string{
		"$ echo 'hello 世界 🎉'",
		"hello 世界 🎉    tab",
		"red andcarriagenewline",
		"error 错误",
		strings.Repeat("z", 20), // the end of a long input stays visible
	} {
		if !strings.Contains(out, want) {
			t.Errorf("terminal lacks %q:\n%s", want, out)
		}
	}
}

// termTitle is the top line of a render whose right column shows the terminal: its title is there.
func termTitle(out string) string { return strings.Split(out, "\n")[0] }

// The terminal shows the session of the agent whose workplace it is.
func TestTerminalFollowsItsAgent(t *testing.T) {
	st := multiState()
	st.Camera = "toolu_tests"
	if s := ansi.Strip(render(st, 160, 30)); !strings.Contains(s, "$ go test ./...") {
		t.Errorf("tests' terminal does not show its session:\n%s", s)
	}
}

// Every frame is the same dim grey, in focus or not, as the prompt's; the agent's colour is its
// name's, never the frame's.
func TestFocusBorder(t *testing.T) {
	st := multiState()
	st.Camera = "toolu_tests" // Focus: Terminal
	for _, o := range []Options{{}, {Focused: true}, {Focused: true, Panel: Agents}} {
		out := Render(st, o, 120, 30)
		for _, l := range strings.Split(out, "\n") {
			for _, c := range append([]color.Color{theme.Fg, theme.Dim}, theme.Agents...) {
				if styled(l, "╭", sgr("38", c)) || styled(l, "│", sgr("38", c)) || styled(l, "╰", sgr("38", c)) {
					t.Fatalf("%+v: a frame not in Border: %q", o, l)
				}
			}
		}
		if top := strings.Split(out, "\n")[0]; !styled(top, "╭", sgr("38", theme.Border)) {
			t.Errorf("%+v: the top frame: %q", o, top)
		}
	}
	if top := strings.Split(render(st, 120, 30), "\n")[0]; !styled(top, "tests", sgr("38", theme.Agents[1])) {
		t.Errorf("the terminal's name is not in its agent's colour: %q", top)
	}
}

func TestAgentsPanel(t *testing.T) {
	st := multiState()
	o := Options{Panel: Agents, Now: now}
	out := Render(st, o, 160, 30)
	rows := leftRows(out, 160)
	want := []string{
		" 1 ⠋ main                 12s", // what an agent does shows in its workplace, not here
		" 2   ✓ tests               3m",
		"     ⠋ build              45s",
		"       Check ▱▱▱▱ 0/4",
		" 3       ⠋ lint            3s",
		" 5       ⠋ vet",
		"         · 2 queued",
		"       Write ▰ 1/1",
		" 4       ✗ docs",
	}
	for i, w := range want {
		if got := strings.TrimRight(rows[i], " "); got != w {
			t.Errorf("row %d:\n got %q\nwant %q", i, got, w)
		}
	}
	if s := ansi.Strip(strings.Split(out, "\n")[0]); !strings.Contains(s, "agents · auto") {
		t.Errorf("title: %q", s)
	}
	if !styled(out, "main", sgr("38", theme.Agents[0])) || styled(out, "main", "1") || !styled(out, "lint", sgr("38", theme.Agents[3])) {
		t.Errorf("names are not in their colours")
	}
	if l := lineWith(out, "queued"); !styled(l, "        · 2 queued", sgr("38", theme.Dim)) {
		t.Errorf("queued row is not dim: %q", l)
	}
	if strings.Contains(left(out, 160), sgr("48", theme.SelBg)) {
		t.Errorf("selection shown without focus")
	}

	// Focused: the selected row is highlighted, the border lit.
	st.Pinned = true
	o.Focused, o.Sel = true, "a1"
	out = left(Render(st, o, 160, 30), 160)
	if !strings.Contains(lineWith(out, "lint"), sgr("48", theme.SelBg)) || strings.Contains(lineWith(out, "main"), sgr("48", theme.SelBg)) {
		t.Errorf("selected row is not highlighted:\n%s", out)
	}
	if l := strings.Split(out, "\n")[0]; !strings.Contains(ansi.Strip(l), "agents · pinned") || !styled(l, "╭", sgr("38", theme.Border)) {
		t.Errorf("title or border: %q", l)
	}

}

// Every agent shows, grey before its duration, the family of the model it runs on, when known.
func TestAgentsPanelModel(t *testing.T) {
	st := multiState()
	findAgent(st, "main").Model = "claude-opus-5-5"
	findAgent(st, "toolu_tests").Model = "claude-haiku-5-5"
	findAgent(st, "a1").Model = "claude-opus-5-5"
	out := Render(st, Options{Panel: Agents, Now: now}, 160, 30)
	rows := leftRows(out, 160)
	if got := strings.TrimRight(rows[1], " "); got != " 2   ✓ tests         haiku 3m" {
		t.Errorf("subagent row %q", got)
	}
	if !strings.HasSuffix(strings.TrimRight(rows[0], " "), "opus 12s") || !strings.HasSuffix(strings.TrimRight(rows[4], " "), "opus 3s") {
		t.Errorf("main's model, or one on main's model, not shown: %q / %q", rows[0], rows[4])
	}
	if strings.Contains(rows[2], "opus") || strings.Contains(rows[2], "haiku") {
		t.Errorf("a model shown for an agent of no known model: %q", rows[2])
	}
	if !styled(lineWith(out, "haiku"), "         haiku 3m", sgr("38", theme.Dim)) {
		t.Error("the model is not grey, as the duration")
	}
}

func TestAgentsPanelScroll(t *testing.T) {
	st := sampleState()
	for i := range 40 {
		st.Agents = append(st.Agents, &ide.Agent{Key: fmt.Sprint("s", i), Name: fmt.Sprintf("sub%02d", i), Kind: ide.Subagent, Parent: "main"})
	}
	st.Camera = "s30"
	out := ansi.Strip(left(Render(st, Options{Panel: Agents}, 120, 20), 120))
	if !strings.Contains(out, "sub30") || strings.Contains(out, "sub00") {
		t.Errorf("panel does not scroll to the camera:\n%s", out)
	}
	out = ansi.Strip(left(Render(st, Options{Panel: Agents, Focused: true, Sel: "s5"}, 120, 20), 120))
	if !strings.Contains(out, "sub05") || strings.Contains(out, "sub30") {
		t.Errorf("panel does not scroll to the selection:\n%s", out)
	}
}

func TestAgentRows(t *testing.T) {
	st := multiState()
	nested := &ide.Agent{Key: "nested", Name: "deep", Kind: ide.Subagent, Parent: "toolu_tests"}
	late := &ide.Agent{Key: "late", Name: "late", Kind: ide.Subagent, Parent: "main"}
	orphan := &ide.Agent{Key: "orphan", Name: "orphan", Kind: ide.Subagent, Parent: "unknown"}
	st.Agents = append(st.Agents, nested, late, orphan)
	got := AgentRows(st)
	want := []string{"main", "toolu_tests", "nested", "toolu_wf", "a1", "a5", "a2", "late", "orphan"}
	if !slices.Equal(got, want) {
		t.Fatalf("AgentRows = %v, want %v", got, want)
	}
	if rows := AgentRows(&ide.State{}); len(rows) != 0 {
		t.Errorf("no agents: %v", rows)
	}
}

func TestCameraFallback(t *testing.T) {
	st := multiState()
	st.Camera = "gone"
	st.Agents[0].Focus = ide.Terminal
	out := Render(st, Options{Panel: Agents, Now: now}, 160, 30)
	plain := ansi.Strip(out)
	for _, want := range []string{"main", "$ go te"} {
		if !strings.Contains(plain, want) {
			t.Errorf("fallback to the first agent lacks %q:\n%s", want, plain)
		}
	}
	if !strings.Contains(termTitle(out), sgr("38", theme.Agents[0])) {
		t.Errorf("terminal border is not main's colour")
	}
}

func TestNoAgents(t *testing.T) {
	st := sampleState()
	st.Agents, st.Camera = nil, ""
	st.Tabs = nil
	for _, o := range []Options{{}, {Panel: Agents, Focused: true}} {
		out := Render(st, o, 100, 20)
		checkSize(t, "no agents", out, 100, 20)
	}
	if out := ansi.Strip(render(st, 100, 20)); !strings.Contains(out, "▾ src") {
		t.Errorf("explorer is empty:\n%s", out)
	}
}

func TestDuration(t *testing.T) {
	cases := []struct {
		started, ended time.Time
		want           string
	}{
		{time.Time{}, time.Time{}, ""},
		{now.Add(-12 * time.Second), time.Time{}, "12s"},
		{now.Add(-3*time.Minute - 59*time.Second), time.Time{}, "3m"},
		{now.Add(-2 * time.Hour), time.Time{}, "2h"},
		{now.Add(-time.Hour), now.Add(-time.Hour + 5*time.Second), "5s"},
		{now.Add(time.Second), time.Time{}, "0s"},
	}
	for _, c := range cases {
		if got := duration(&ide.Agent{Started: c.started, Ended: c.ended}, now); got != c.want {
			t.Errorf("duration(%v, %v) = %q, want %q", c.started, c.ended, got, c.want)
		}
	}
	// A zero clock does not break the spinner.
	if g, _ := status(&ide.Agent{}, time.Time{}); !slices.Contains(spinner, g) {
		t.Errorf("spinner frame %q", g)
	}
}

func TestHighlight(t *testing.T) {
	cases := []struct {
		path, line string
		want       map[string]kind // first occurrence of a substring → kind of its runes
	}{
		{"a.go", `func main() { x := "s" + 42 // done`, map[string]kind{"func": keyword, "main": fn, "x": plain, `"s"`: str, "42": number, "// done": comment}},
		{"a.py", `def fn(a): return 'q#' # note`, map[string]kind{"def": keyword, "fn": fn, "return": keyword, `'q#'`: str, "# note": comment}},
		{"a.sh", `echo $# done # c`, map[string]kind{"$#": plain, "done": keyword, "# c": comment}},
		{"a.ts", "const t = `x` /* c */ + y", map[string]kind{"const": keyword, "`x`": str, "/* c */": comment, "y": plain}},
		{"a.js", "client.reader(`sudo ${name} -Fc`).pipe(file)", map[string]kind{"client.": plain, "reader": fn, "`sudo ": str, "${name}": interp, " -Fc`": str, "pipe": fn}},
		{"a.js", "log(`\\${x}`, y)", map[string]kind{"${x}`": str, "y": plain}},
		{"a.rs", `fn main() { let v = "a\"b"; }`, map[string]kind{"fn": keyword, "main": fn, "let": keyword, `"a\"b"`: str}},
		{"a.c", `int x = 0x1F; /* open`, map[string]kind{"int": keyword, "0x1F": number, "/* open": comment}},
		{"a.txt", `func "s" 42`, map[string]kind{"func": plain, `"s"`: plain, "42": plain}},
		{"q.sql", `SELECT id from t -- note`, map[string]kind{"SELECT": keyword, "id": plain, "from": keyword, "-- note": comment}},
		{"a.lua", `local s = "x" -- c`, map[string]kind{"local": keyword, `"x"`: str, "-- c": comment}},
		{"a.html", `<a href="x"><!-- c --> b`, map[string]kind{`"x"`: str, "<!-- c -->": comment, " b": plain}},
		{"a.json", `{"k": true, "n": 1.5}`, map[string]kind{`"k"`: keyword, "true": keyword, `"n"`: keyword, "1.5": number}},
		// A JSON object's keys as keywords, wherever on the line; the strings it holds are strings.
		{"a.json", `{"a": "b", "c": {"d" : ["e"]}}`, map[string]kind{`"a"`: keyword, `"b"`: str, `"c"`: keyword, `"d"`: keyword, `"e"`: str}},
		{"a.json5", `{k: 1, // c`, map[string]kind{"k": keyword, "// c": comment}},
		// A YAML mapping's key, whole, as a keyword.
		{"a.yml", `on: [push] # c`, map[string]kind{"on": keyword, "push": plain, "# c": comment}},
		{"a.yaml", `debug: on`, map[string]kind{"debug": keyword, "on": keyword}},
		{"a.yml", `  - runs-on: ubuntu-latest # c`, map[string]kind{"-": plain, "runs-on": keyword, "ubuntu": plain, "# c": comment}},
		{"a.yml", `"a b": 1`, map[string]kind{`"a b"`: keyword, "1": number}},
		{"a.yml", `url: http://x:80`, map[string]kind{"url": keyword, "http": plain}},
		{"a.yml", `- item: "x"`, map[string]kind{"item": keyword, `"x"`: str}},
		{"a.yml", `- plain`, map[string]kind{"plain": plain}},
		{"a.yml", `# key: no`, map[string]kind{"# key: no": comment}},
		{"a.css", `a { color: #fff; } /* c */`, map[string]kind{"#fff;": plain, "/* c */": comment}},
		{"a.java", `public void f() { return; } // c`, map[string]kind{"public": keyword, "f": fn, "// c": comment}},
		{"a.kt", `fun area(x: Int) = when (x) {}`, map[string]kind{"fun": keyword, "area": fn, "when": keyword}},
		{"a.swift", `guard let v = f() else { return }`, map[string]kind{"guard": keyword, "let": keyword, "f": fn}},
		{"a.rb", `def f; puts "#{x}" end # c`, map[string]kind{"def": keyword, `"#{x}"`: str, "end": keyword, "# c": comment}},
		{"a.cs", `public string Name { get; set; }`, map[string]kind{"public": keyword, "string": keyword, "get": keyword}},
		{"a.php", `function greet($x) { echo 'y'; }`, map[string]kind{"function": keyword, "greet": fn, "echo": keyword, "'y'": str}},
		{"Dockerfile", `FROM golang AS build # c`, map[string]kind{"FROM": keyword, "golang": plain, "AS": keyword, "# c": comment}},
		{"sub/Makefile", `build: ; go build # c`, map[string]kind{"# c": comment}},
		// Types and classes: the built-in ones as keywords, a declared one, and by the language's habits.
		{"a.go", `type State struct { Root string; Open []*Tab; Cam *ide.Agent; M map[Key]Val }`,
			map[string]kind{"State": typ, "struct": keyword, "Root": plain, "string": keyword, "Tab": typ, "ide.": plain, "Agent": typ, "Key": typ, "Val": typ}},
		{"a.go", `func (m *Model) Draw(w int, xs ...Item) tea.Cmd { return m.sel }`,
			map[string]kind{"Model": typ, "Draw": fn, "int": keyword, "Item": typ, "tea.": plain, "Cmd": typ, "sel": plain}},
		{"a.go", `xs = append(xs, cell{a}); y := v.(Pos); var p, q Point; case *ide.Status:`,
			map[string]kind{"cell": typ, "Pos": typ, "q": plain, "Point": typ, "Status": typ}},
		{"a.go", `d := 2*time.Second + n * time.Hour; x := *p; e := ide.Editor; b := []byte(s); k := ide.Pane(v)`,
			map[string]kind{"Second": plain, "Hour": plain, "p;": plain, "Editor": plain, "byte": keyword, "Pane": fn}},
		{"a.go", "\tFocus Pane // c", map[string]kind{"Focus": plain, "Pane": typ}},
		{"a.go", "\tsome prose in a raw string goes on", map[string]kind{"prose": plain, "raw": plain, "string": keyword, "goes": plain, "on": plain}},
		{"a.py", `class Foo(Base): n: int = Bar(); MAX_N = 1`, map[string]kind{"Foo": typ, "Base": typ, "int": keyword, "Bar": fn, "MAX_N": plain}},
		{"a.java", `List<String> xs = new ArrayList<>(); long n = java.util.Objects.hash(xs);`,
			map[string]kind{"List": typ, "String": typ, "ArrayList": typ, "long": keyword, "java.util.": plain, "Objects": typ, "hash": fn}},
		{"a.rs", `impl fmt::Display for Point { fn f(v: Vec<u8>) -> Option<Self> {} }`,
			map[string]kind{"impl": keyword, "fmt::": plain, "Display": typ, "Point": typ, "Vec": typ, "u8": keyword, "Option": typ, "Self": keyword}},
		{"a.c", `struct point *p; size_t n;`, map[string]kind{"struct": keyword, "point": typ, "size_t": keyword}},
		{"a.ts", `let n: number = parseInt(s)`, map[string]kind{"number": keyword, "parseInt": fn}},
		// Tags in the colour of types; in JSX only where no expression ends before "<".
		{"a.html", `<div class="x"></div>`, map[string]kind{"div": typ, "class": plain, `"x"`: str, "</": plain}},
		{"a.xml", `<xsl:value-of select="."/>`, map[string]kind{"xsl:value-of": typ, "select": plain}},
		{"a.jsx", `return <div>{a<b}</div>`, map[string]kind{"div": typ, "b": plain}},
		{"a.ts", `if (i<n) x = a < b`, map[string]kind{"n": plain, "b": plain}},
		{"a.jsonl", `{"k": 1}`, map[string]kind{`"k"`: keyword, "1": number}},
		// Markdown as the chat draws it.
		{"a.md", "# Title `x`", map[string]kind{"# Title `x`": heading}},
		{"a.md", "- item **bold** `code` [link](http://x) and _em_",
			map[string]kind{"-": heading, "item": plain, "**bold**": strong, "`code`": literal, "[link](http://x)": link, "_em_": plain}},
		{"a.md", "1. first", map[string]kind{"1.": heading, "first": plain}},
		{"a.md", "> quoted", map[string]kind{"> quoted": faint}},
		{"a.md", "```go", map[string]kind{"```go": literal}},
		{"a.md", "| a | b |", map[string]kind{"|": faint, "a": plain}},
		{"a.md", "|---|:-:|", map[string]kind{"|---|:-:|": faint}},
		{"a.md", "---", map[string]kind{"---": faint}},
		// Ignore lists: comments, negations and globs.
		{".gitignore", "# build output", map[string]kind{"# build output": comment}},
		{"sub/.dockerignore", "!keep/*.log", map[string]kind{"!": keyword, "keep/": plain, "*": keyword, ".log": plain}},
		{".gitattributes", "*.go text eol=lf", map[string]kind{"*": keyword, "text": plain}},
		// Diffs, as git colours them.
		{"a.diff", "diff --git a/x b/x", map[string]kind{"diff --git a/x b/x": strong}},
		{"a.patch", "+++ b/x", map[string]kind{"+++ b/x": faint}},
		{"a.patch", "@@ -1 +1 @@ f", map[string]kind{"@@ -1 +1 @@ f": literal}},
		{"a.patch", "+added", map[string]kind{"+added": added}},
		{"a.patch", "-gone", map[string]kind{"-gone": removed}},
		{"a.patch", " kept", map[string]kind{" kept": plain}},
		// Settings: env files, ini, sections.
		// A setting's key, whole, as a keyword; its value as it is.
		{".env", `KEY="v ${X}" # c`, map[string]kind{"KEY": keyword, `"v `: str, "${X}": interp, "# c": comment}},
		{".env.local", "export PORT=8080", map[string]kind{"export": plain, "PORT": keyword, "8080": number}},
		{".editorconfig", "root = true", map[string]kind{"root": keyword, "true": keyword}},
		{"a.toml", "[server.http] # c", map[string]kind{"[server.http]": typ, "# c": comment}},
		{"a.toml", `bin-dir.path = "a=b" # c`, map[string]kind{"bin-dir.path": keyword, `"a=b"`: str, "# c": comment}},
		{"a.toml", `"quoted key" = 1`, map[string]kind{`"quoted key"`: keyword, "1": number}},
		{"a.toml", `  "item",`, map[string]kind{`"item"`: str}},
		{"app.properties", "a.b=1", map[string]kind{"a.b": keyword, "1": number}},
		{"app.properties", "log.level: debug", map[string]kind{"log.level": keyword, "debug": plain}},
		{"nginx.conf", "proxy_pass http://backend;", map[string]kind{"proxy_pass": plain, "http": plain}},
		// Schemas and infrastructure.
		{"a.proto", "message User { repeated string name = 1; }", map[string]kind{"message": keyword, "User": typ, "repeated": keyword, "string": keyword}},
		{"a.graphql", "query Q($id: ID!) { user(id: $id) } # c", map[string]kind{"query": keyword, "ID": keyword, "user": fn, "# c": comment}},
		{"main.tf", `resource "aws_s3_bucket" "b" { count = 2 }`, map[string]kind{"resource": keyword, `"aws_s3_bucket"`: str, "count": keyword, "2": number}},
		{"go.mod", "require golang.org/x/sys v0.30.0 // indirect", map[string]kind{"require": keyword, "// indirect": comment}},
		{"a.csv", `"name",42`, map[string]kind{`"name"`: str, "42": number}},
		{"a.xaml", `<Grid Margin="4"/>`, map[string]kind{"Grid": typ, `"4"`: str}},
		{"a.mdx", "# T", map[string]kind{"# T": heading}},
		{"x.ipynb", `{"cells": []}`, map[string]kind{`"cells"`: keyword}},
		{"Vagrantfile", `Vagrant.configure("2") do |config|`, map[string]kind{"do": keyword}},
		{".bash_profile", "export PATH", map[string]kind{"export": keyword}},
	}
	// A chat's fence names the language as markdown does.
	if out := Highlight("+x", "diff"); !styled(out, "+x", sgr("38", theme.Green)) {
		t.Errorf("a diff fence: %q", out)
	}
	for _, c := range cases {
		rs := []rune(c.line)
		ks := highlight(rs, langOf(c.path))
		for sub, want := range c.want {
			at := len([]rune(c.line[:strings.Index(c.line, sub)]))
			for i := range []rune(sub) {
				if ks[at+i] != want {
					t.Errorf("%s %q: %q rune %d is %d, want %d", c.path, c.line, sub, i, ks[at+i], want)
					break
				}
			}
		}
	}
}

// A click on a row of the agents panel finds its agent, on the right the agent of that workplace.
func TestAgentAt(t *testing.T) {
	st := multiState()
	o := Options{Panel: Agents, Now: now}
	lines := strings.Split(ansi.Strip(Render(st, o, 120, 12)), "\n")
	found := map[string]bool{}
	for y, l := range lines {
		if key := AgentAt(st, o, 120, 12, 3, y); key != "" {
			if a := findAgent(st, key); a == nil || !strings.Contains(l, a.Name) {
				t.Errorf("row %d %q: click finds %q", y, l, key)
			}
			found[key] = true
		}
	}
	if !found["main"] || !found["a1"] || len(found) < 5 || AgentAt(st, o, 120, 12, 3, 0) != "" {
		t.Errorf("clicks found %v", found)
	}
	if key := AgentAt(st, o, 120, 12, 40, 2); key != st.Camera {
		t.Errorf("right column: %q, want the camera's %q", key, st.Camera)
	}
	if key := AgentAt(st, Options{Now: now}, 120, 12, 3, 3); key != "" {
		t.Errorf("explorer row: %q", key)
	}
}
