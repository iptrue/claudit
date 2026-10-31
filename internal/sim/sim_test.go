package sim

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/pixelbender/claudit/internal/ide"
)

var t0 = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

const frame = 16 * time.Millisecond

// play ticks every frame from now until the sim is idle or limit has passed, calling see after
// every tick. It returns the time of the last tick.
func play(s *Sim, now time.Time, limit time.Duration, see func()) time.Time {
	return playEvery(s, now, frame, limit, see)
}

// watch is play with a tick every millisecond, to see steps shorter than a frame.
func watch(s *Sim, now time.Time, limit time.Duration, see func()) time.Time {
	return playEvery(s, now, time.Millisecond, limit, see)
}

func playEvery(s *Sim, now time.Time, every, limit time.Duration, see func()) time.Time {
	for end := now.Add(limit); ; now = now.Add(every) {
		s.Tick(now)
		if see != nil {
			see()
		}
		if !s.Busy() || !now.Before(end) {
			return now
		}
	}
}

// setup writes files into a fresh root and starts a Sim there.
func setup(t *testing.T, files map[string]string) (*Sim, string) {
	t.Helper()
	root := t.TempDir()
	for name, text := range files {
		writeFile(t, filepath.Join(root, name), text)
	}
	return New(root), root
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func editInput(path, old, repl string, all bool) map[string]any {
	return map[string]any{"file_path": path, "old_string": old, "new_string": repl, "replace_all": all}
}

func tab(t *testing.T, s *Sim, path string) *ide.Tab {
	t.Helper()
	tb := s.State().FindTab(path)
	if tb == nil {
		t.Fatalf("no tab for %s", path)
	}
	return tb
}

// mainAgent is the main agent of s.
func mainAgent(s *Sim) *ide.Agent { return s.State().Agents[0] }

// sel is the selection of the agent key in the tab, nil for none.
func sel(tb *ide.Tab, key string) *ide.Selection {
	if c := tb.Buf.Carets[key]; c != nil {
		return c.Sel
	}
	return nil
}

func termTexts(a *ide.Agent, kind ide.LineKind) []string {
	var out []string
	for _, l := range a.Term.Lines {
		if l.Kind == kind {
			out = append(out, l.Text)
		}
	}
	return out
}

const calcPre = "\"\"\"Tiny calculator module.\"\"\"\n\n\ndef add(a, b):\n    return a + b\n\n\n" +
	"def sub(a, b):\n    return a - b\n\n\nif __name__ == \"__main__\":\n    print(add(2, 3))\n"

func TestEdit(t *testing.T) {
	subOld := "def sub(a, b):\n    return a - b\n"
	subNew := subOld + "\n\ndef div(a, b):\n    return a / b\n"
	calcPost := strings.Replace(calcPre, subOld, subNew, 1)
	goSrc := "func main() {\n\tfmt.Println(\"hello\")\n}\n"
	tests := []struct {
		name, disk, old, repl string
		all                   bool
		want                  string
	}{
		{"plain", calcPre, subOld, subNew, false, calcPost},
		{"already on disk, old inside new", calcPost, subOld, subNew, false, calcPost},
		{"already on disk", "x = 2\ny = 3\n", "x = 1", "x = 2", false, "x = 2\ny = 3\n"},
		{"replace_all", "a := 1\nb := a + a\n", "a", "alpha", true, "alpha := 1\nb := alpha + alpha\n"},
		{"replace_all already on disk", "alpha := 1\nb := alpha\n", "a", "alpha", true, "alpha := 1\nb := alpha\n"},
		{"unicode and tabs", goSrc, "\tfmt.Println(\"hello\")", "\tfmt.Println(\"hello, 世界 👋\")\n\treturn", false,
			"func main() {\n    fmt.Println(\"hello, 世界 👋\")\n    return\n}\n"},
		{"crlf", "one\r\ntwo\r\n", "two", "second 🚀", false, "one\nsecond 🚀\n"},
		{"delete", "keep\ndrop\nkeep\n", "drop\n", "", false, "keep\nkeep\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, root := setup(t, map[string]string{"other.txt": "", "pkg/calc.py": tc.disk})
			path := filepath.Join(root, "pkg/calc.py")
			s.ToolUse("main", "e1", "Edit", editInput(path, tc.old, tc.repl, tc.all))
			s.ToolResult("e1", "The file has been updated successfully.", false)
			m := mainAgent(s)
			var sawExplorer, sawSel, sawModified, sawActivity bool
			end := watch(s, t0, 10*time.Second, func() {
				sawExplorer = sawExplorer || m.Focus == ide.Explorer
				sawActivity = sawActivity || m.Activity == "Edit calc.py"
				if tb := s.State().FindTab(path); tb != nil {
					sawSel = sawSel || sel(tb, "main") != nil
					sawModified = sawModified || tb.Buf.Modified
				}
			})
			if s.Busy() {
				t.Fatal("still busy after 10s")
			}
			tb := tab(t, s, path)
			if got := tb.Buf.Text(); got != tc.want {
				t.Fatalf("buffer\n%q\nwant\n%q", got, tc.want)
			}
			if tb.Buf.Modified || sel(tb, "main") != nil || m.Focus != ide.Editor || tb.Title != "calc.py" {
				t.Fatalf("modified %v sel %v focus %v title %q", tb.Buf.Modified, sel(tb, "main"), m.Focus, tb.Title)
			}
			if m.TreeSel != path || m.File != path || s.State().Touched[path] != "main" || m.Activity != "" {
				t.Fatalf("explorer selection %q file %q touched %v activity %q", m.TreeSel, m.File, s.State().Touched, m.Activity)
			}
			if !sawExplorer || !sawSel || !sawActivity || !sawModified && tc.repl != "" {
				t.Fatalf("explorer %v selection %v activity %v modified %v", sawExplorer, sawSel, sawActivity, sawModified)
			}
			if d := end.Sub(t0); d > 4500*time.Millisecond {
				t.Fatalf("edit took %v", d)
			}
		})
	}
}

// The lines an edit typed stay lit while the edit is the agent's last call, and fade from the
// moment its next call starts to play.
func TestEditGlowHolds(t *testing.T) {
	s, root := setup(t, map[string]string{"pkg/calc.py": calcPre})
	path := filepath.Join(root, "pkg/calc.py")
	s.ToolUse("main", "e1", "Edit", editInput(path, "return a - b", "return a - b  # sub", false))
	s.ToolResult("e1", "The file has been updated successfully.", false)
	end := play(s, t0, 10*time.Second, nil)
	end = play(s, end.Add(time.Hour), time.Second, nil) // long after: nothing comes next
	glows := func() []ide.Glow { return tab(t, s, path).Buf.Glow }
	if g := glows(); len(g) == 0 || !g[0].Off.IsZero() {
		t.Fatalf("glows after the edit: %+v", g)
	}
	s.ToolUse("main", "r1", "Read", map[string]any{"file_path": path})
	s.Tick(end.Add(time.Second))
	for _, g := range glows() {
		if !g.Off.Equal(end.Add(time.Second)) {
			t.Errorf("glow %+v does not fade from the next call", g)
		}
	}
}

// In a live session the snapshot precedes the edit. When old sits inside new and new is in the
// text already, the edit must still be animated as pending, not taken for one already applied.
func TestEditOldInsideNewLive(t *testing.T) {
	tests := []struct{ name, disk, old, repl string }{
		{"indent", "for x in xs:\n    if x:\n    print(x)\n", "print(x)", "    print(x)"},
		{"blank line after", "def f(a, b):\n    return a + b\n\n\ndef g():\n    pass\n", "    return a + b\n", "    return a + b\n\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, root := setup(t, map[string]string{"a.py": tc.disk})
			path := filepath.Join(root, "a.py")
			s.ToolUse("main", "e1", "Edit", editInput(path, tc.old, tc.repl, false))
			now := play(s, t0, 300*time.Millisecond, nil)
			want := strings.Replace(tc.disk, tc.old, tc.repl, 1)
			writeFile(t, path, want) // the tool runs
			s.ToolResult("e1", "The file has been updated successfully.", false)
			var sawPre bool
			play(s, now, 10*time.Second, func() {
				if tb := s.State().FindTab(path); tb != nil && tb.Buf.Text() != tc.disk && tb.Buf.Text() != want && sel(tb, "main") == nil && !tb.Buf.Modified {
					sawPre = true // a text the file never held
				}
			})
			tb := tab(t, s, path)
			if s.Busy() || tb.Buf.Modified || tb.Buf.Text() != want {
				t.Fatalf("busy %v modified %v buffer %q, want %q", s.Busy(), tb.Buf.Modified, tb.Buf.Text(), want)
			}
			if sawPre {
				t.Error("the editor showed a pre-edit text the file never had")
			}
		})
	}
}

func TestEditResultAfterAnimation(t *testing.T) {
	for _, isErr := range []bool{false, true} {
		t.Run(fmt.Sprint("error=", isErr), func(t *testing.T) {
			s, root := setup(t, map[string]string{"a.go": "x := 1\n"})
			path := filepath.Join(root, "a.go")
			s.ToolUse("main", "e1", "Edit", editInput(path, "1", "2", false))
			play(s, t0, 10*time.Second, nil)
			tb := tab(t, s, path)
			if s.Busy() || !tb.Buf.Modified || tb.Buf.Text() != "x := 2\n" {
				t.Fatalf("before result: busy %v modified %v %q", s.Busy(), tb.Buf.Modified, tb.Buf.Text())
			}
			writeFile(t, path, "x := 3\n") // what the disk holds when the result comes
			s.ToolResult("e1", "result", isErr)
			want := "x := 2\n"
			if isErr {
				want = "x := 3\n"
			}
			if tb.Buf.Modified || tb.Buf.Text() != want {
				t.Fatalf("after result: modified %v %q, want %q", tb.Buf.Modified, tb.Buf.Text(), want)
			}
		})
	}
}

func TestEditNotFoundWaitsAndReloads(t *testing.T) {
	s, root := setup(t, map[string]string{"a.txt": "alpha\n"})
	path := filepath.Join(root, "a.txt")
	s.ToolUse("main", "e1", "Edit", editInput(path, "missing", "absent", false))
	now := play(s, t0, 5*time.Second, nil)
	if !s.Busy() || s.State().FindTab(path) != nil {
		t.Fatal("the edit must wait for its result before opening the file")
	}
	writeFile(t, path, "beta\n")
	s.ToolResult("e1", "String to replace not found in file.", true)
	play(s, now, 5*time.Second, nil)
	tb := tab(t, s, path)
	if s.Busy() || tb.Buf.Text() != "beta\n" || tb.Buf.Modified {
		t.Fatalf("busy %v modified %v %q", s.Busy(), tb.Buf.Modified, tb.Buf.Text())
	}
}

func TestWrite(t *testing.T) {
	content := "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"
	want := strings.ReplaceAll(content, "\t", "    ")

	t.Run("new file", func(t *testing.T) {
		s, root := setup(t, map[string]string{"a.txt": ""})
		path := filepath.Join(root, "cmd/app/main.go")
		s.ToolUse("main", "w1", "Write", map[string]any{"file_path": path, "content": content})
		s.Tick(t0)
		st := s.State()
		var rows []string
		for _, r := range st.Tree {
			rows = append(rows, r.Name)
		}
		if strings.Join(rows, " ") != "cmd app main.go a.txt" {
			t.Fatalf("rows %q", rows)
		}
		play(s, t0, 10*time.Second, nil)
		tb := tab(t, s, path)
		if tb.Buf.Text() != want || !tb.Buf.Modified || mainAgent(s).TreeSel != path {
			t.Fatalf("modified %v %q", tb.Buf.Modified, tb.Buf.Text())
		}
		s.ToolResult("w1", "File created successfully", false)
		if tb.Buf.Modified {
			t.Fatal("still modified after the result")
		}
	})

	t.Run("existing file", func(t *testing.T) {
		s, root := setup(t, map[string]string{"main.go": "old\ntext\n"})
		path := filepath.Join(root, "main.go")
		s.ToolUse("main", "w1", "Write", map[string]any{"file_path": path, "content": content})
		s.ToolResult("w1", "ok", false)
		var sawOld, sawAll bool
		play(s, t0, 10*time.Second, func() {
			if tb := s.State().FindTab(path); tb != nil {
				sawOld = sawOld || tb.Buf.Text() == "old\ntext\n"
				sawAll = sawAll || sel(tb, "main") != nil && *sel(tb, "main") == ide.Selection{To: ide.Pos{Line: 2}}
			}
		})
		tb := tab(t, s, path)
		if !sawOld || !sawAll || tb.Buf.Text() != want || tb.Buf.Modified {
			t.Fatalf("old %v select all %v modified %v %q", sawOld, sawAll, tb.Buf.Modified, tb.Buf.Text())
		}
	})
}

// A denied Write of a new file leaves nothing behind: no tab and no explorer rows, also for
// the new folders on the way. Rows of files on disk stay.
// Before old code goes the agent selects it, a line or a rune at a time, and holds the selection
// a moment: a Write the whole file from the top, an Edit the text it replaces.
func TestSelectBeforeDelete(t *testing.T) {
	for _, tt := range []struct {
		name, old string
		input     map[string]any
	}{
		{"write", "one\ntwo\nthree\nfour\n", map[string]any{"content": "new\n"}},
		{"edit", "keep\ndelete this\nkeep\n", map[string]any{"old_string": "delete this", "new_string": ""}},
	} {
		s, root := setup(t, map[string]string{"f.txt": tt.old})
		path := filepath.Join(root, "f.txt")
		tt.input["file_path"] = path
		name := map[string]string{"write": "Write", "edit": "Edit"}[tt.name]
		s.ToolUse("main", "x", name, tt.input)
		var sizes []int        // the selection as it grows, in runes, while the old text is whole
		var held time.Duration // how long the full selection shows before the text goes
		var last time.Time
		now := t0
		watch(s, t0, 10*time.Second, func() {
			now = now.Add(time.Millisecond)
			tb := s.State().FindTab(path)
			if tb == nil || tb.Buf.Text() != tt.old {
				return
			}
			c := tb.Buf.Carets["main"]
			if c == nil || c.Sel == nil {
				return
			}
			n := utf8.RuneCountInString(tb.Buf.Text()[offset(&tb.Buf, c.Sel.From):offset(&tb.Buf, c.Sel.To)])
			if len(sizes) == 0 || sizes[len(sizes)-1] != n {
				sizes, last = append(sizes, n), now
			}
			held = now.Sub(last)
		})
		if len(sizes) < 3 || !slices.IsSorted(sizes) || held < pause-10*time.Millisecond {
			t.Errorf("%s: selection grew %v, held %v", tt.name, sizes, held)
		}
	}
}

func TestWriteDeniedNewFile(t *testing.T) {
	s, root := setup(t, map[string]string{"a.txt": "", "z.txt": ""})
	path := filepath.Join(root, "new/dir/notes.txt")
	s.ToolUse("main", "w1", "Write", map[string]any{"file_path": path, "content": "pow"})
	now := play(s, t0, 10*time.Second, nil)
	if tab(t, s, path).Buf.Text() != "pow" {
		t.Fatal("the content was not typed")
	}
	s.ToolResult("w1", "The user denied this tool call.", true)
	play(s, now, time.Second, nil)
	st := s.State()
	var rows []string
	for _, r := range st.Tree {
		rows = append(rows, r.Name)
	}
	if strings.Join(rows, " ") != "a.txt z.txt" || st.FindTab(path) != nil || mainAgent(s).File != "" {
		t.Fatalf("rows %q, tab %v, file %q", rows, st.FindTab(path) != nil, mainAgent(s).File)
	}
	if _, ok := st.Touched[path]; ok {
		t.Fatal("a file that never made it to disk is not touched")
	}
}

func TestBash(t *testing.T) {
	s, root := setup(t, map[string]string{"a.txt": "hello\n"})
	s.ToolUse("main", "b1", "Bash", map[string]any{"command": "cd sub &&\n\tmake test"})
	s.ToolUse("main", "r1", "Read", map[string]any{"file_path": filepath.Join(root, "a.txt")})
	s.ToolResult("r1", "1\thello", false)

	m := mainAgent(s)
	var sawInput bool
	now := play(s, t0, 3*time.Second, func() { sawInput = sawInput || m.Term.Input == "cd sub &&" })
	st := s.State()
	// The prompt goes before the command's first line alone.
	if got, more := termTexts(m, ide.Command), termTexts(m, ide.More); !reflect.DeepEqual(got, []string{"cd sub &&"}) || !reflect.DeepEqual(more, []string{"    make test"}) {
		t.Fatalf("command lines %q, then %q", got, more)
	}
	if !sawInput || m.Term.Input != "" || !m.Term.Running || m.Focus != ide.Terminal || m.Activity != "$ cd sub &&" {
		t.Fatalf("input seen %v, input %q running %v focus %v activity %q", sawInput, m.Term.Input, m.Term.Running, m.Focus, m.Activity)
	}
	if !s.Busy() || len(st.Tabs) != 0 {
		t.Fatal("the next scene must wait for the command's result")
	}
	if ran := now.Sub(m.Term.RunAt); m.Term.RunAt.IsZero() || ran < 0 || ran > 3*time.Second {
		t.Fatalf("the command started at %v, now is %v", m.Term.RunAt, now)
	}
	spin := m.Term.Spinner
	now = now.Add(400 * time.Millisecond)
	if !s.Tick(now) || m.Term.Spinner-spin != 5 {
		t.Fatalf("spinner %d -> %d", spin, m.Term.Spinner)
	}

	s.ToolResult("b1", "\x1b[32mok\x1b[0m\r\n10%\r100%\nFAIL\tx\n", true)
	var sawRunningWithOutput bool
	play(s, now, 5*time.Second, func() {
		sawRunningWithOutput = sawRunningWithOutput || m.Term.Running && len(termTexts(m, ide.ErrorOutput)) > 0
	})
	if got := termTexts(mainAgent(s), ide.ErrorOutput); !reflect.DeepEqual(got, []string{"ok", "100%", "FAIL    x"}) {
		t.Fatalf("output %q", got)
	}
	if m.Term.Running || !sawRunningWithOutput {
		t.Fatalf("running %v", m.Term.Running)
	}
	if s.Busy() || tab(t, s, filepath.Join(root, "a.txt")).Buf.Text() != "hello\n" || m.Focus != ide.Editor {
		t.Fatal("the Read scene did not play after the command")
	}
}

func TestSearchTools(t *testing.T) {
	s, root := setup(t, nil)
	long := strings.Repeat("match\n", 100)
	calls := []struct {
		name  string
		input map[string]any
		cmd   string
	}{
		{"Grep", map[string]any{"pattern": "def add", "path": root}, "rg -n 'def add' ."},
		{"Grep", map[string]any{"pattern": "it's"}, `rg -n 'it'\''s'`},
		{"Glob", map[string]any{"pattern": "*.go", "path": filepath.Join(root, "src")}, "find src -name '*.go'"},
		{"Glob", map[string]any{"pattern": "*.md"}, "find . -name '*.md'"},
	}
	var want []string
	for i, c := range calls {
		id := fmt.Sprint("t", i)
		s.ToolUse("main", id, c.name, c.input)
		s.ToolResult(id, long, false)
		want = append(want, c.cmd)
	}
	s.ToolUse("main", "x", "TodoWrite", map[string]any{"todos": []any{}}) // no scene
	play(s, t0, 30*time.Second, nil)
	if s.Busy() {
		t.Fatal("still busy")
	}
	if got := termTexts(mainAgent(s), ide.Command); !reflect.DeepEqual(got, want) {
		t.Fatalf("commands\n%q\nwant\n%q", got, want)
	}
	out := termTexts(mainAgent(s), ide.Output)
	if len(out) != len(calls)*41 || out[39] != "match" || out[40] != "… 60 more lines" {
		t.Fatalf("%d output lines, %q", len(out), out[38:42])
	}
}

// A web tool types its address into the agent's browser, loads while it runs, then the page comes
// in: the links a search found, then its text. The page stays a while before the next call.
func TestBrowse(t *testing.T) {
	s, _ := setup(t, nil)
	s.ToolUse("main", "w", "WebSearch", map[string]any{"query": "bubble tea"})
	m := mainAgent(s)
	now := play(s, t0, 5*time.Second, nil) // blocked on the result
	if m.Focus != ide.Browser || m.Page.URL != "bubble tea" || !m.Page.Search || m.Page.LoadAt.IsZero() || len(m.Page.Lines) != 0 {
		t.Fatalf("while loading: focus %v page %+v", m.Focus, m.Page)
	}
	s.ToolResult("w", "Web search results for query: \"bubble tea\"\n\nLinks: [{\"title\":\"Bubble Tea\",\"url\":\"https://github.com/charmbracelet/bubbletea\"}]\n\nA **TUI** framework.", false)
	s.ToolUse("main", "b", "Bash", map[string]any{"command": "ls"}) // waiting: catch-up is on
	s.ToolResult("b", "", false)
	want := []ide.PageLine{{Text: "Bubble Tea", URL: "https://github.com/charmbracelet/bubbletea"}, {}, {Text: "A **TUI** framework."}}
	at := now.Add(-frame)
	var shown, left time.Time
	play(s, now, 5*time.Second, func() {
		at = at.Add(frame)
		if shown.IsZero() && len(m.Page.Lines) == len(want) {
			shown = at
		}
		if !shown.IsZero() && left.IsZero() && m.Focus != ide.Browser {
			left = at
		}
	})
	if !reflect.DeepEqual(m.Page.Lines, want) || !m.Page.LoadAt.IsZero() || m.Activity != "" {
		t.Fatalf("loaded: %+v", m.Page)
	}
	if left.IsZero() || left.Sub(shown) < pagePause-frame { // seen a frame late at most
		t.Errorf("the page stayed %v, want at least %v", left.Sub(shown), pagePause)
	}

	s.ToolUse("main", "f", "WebFetch", map[string]any{"url": "https://go.dev", "prompt": "x"})
	s.ToolResult("f", "<tool_use_error>Error: 404</tool_use_error>", true)
	play(s, now, 5*time.Second, nil)
	if m.Page.URL != "https://go.dev" || m.Page.Search || !m.Page.Failed || !reflect.DeepEqual(m.Page.Lines, []ide.PageLine{{Text: "Error: 404"}}) {
		t.Fatalf("failed fetch: %+v", m.Page)
	}
}

// Tool errors come wrapped in <tool_use_error> tags, which a terminal would never print.
func TestToolErrorTags(t *testing.T) {
	s, _ := setup(t, nil)
	s.ToolUse("main", "g", "Grep", map[string]any{"pattern": "def add"})
	s.ToolResult("g", "<tool_use_error>Error: No such tool available: Grep.\nUse grep via Bash.</tool_use_error>", true)
	play(s, t0, 10*time.Second, nil)
	want := []string{"Error: No such tool available: Grep.", "Use grep via Bash."}
	if got := termTexts(mainAgent(s), ide.ErrorOutput); !reflect.DeepEqual(got, want) {
		t.Fatalf("error lines %q, want %q", got, want)
	}
}

func TestRead(t *testing.T) {
	var fifty []string
	for i := range 50 {
		fifty = append(fifty, fmt.Sprint("line ", i+1))
	}
	text := strings.Join(fifty, "\n") + "\n"

	t.Run("outside root", func(t *testing.T) {
		s, _ := setup(t, map[string]string{"a.txt": ""})
		path := filepath.Join(t.TempDir(), "notes.txt")
		writeFile(t, path, text)
		s.ToolUse("main", "r1", "Read", map[string]any{"file_path": path})
		s.ToolUse("main", "r2", "Read", map[string]any{"file_path": path, "offset": 10.0, "limit": 5.0})
		var lines []int
		var sels []string // any selection on the way: there should be none
		watch(s, t0, 5*time.Second, func() {
			if tb := s.State().FindTab(path); tb != nil && (len(lines) == 0 || lines[len(lines)-1] != tb.Buf.Carets["main"].Pos.Line) {
				c := tb.Buf.Carets["main"]
				lines = append(lines, c.Pos.Line)
				if c.Sel != nil {
					sels = append(sels, fmt.Sprint(c.Sel.From, c.Sel.To))
				}
			}
		})
		st, m := s.State(), mainAgent(s)
		tb := tab(t, s, path)
		if s.Busy() || m.TreeSel != "" || m.Focus != ide.Editor || tb.Buf.Text() != text || len(st.Tabs) != 1 {
			t.Fatalf("busy %v sel %q focus %v tabs %d", s.Busy(), m.TreeSel, m.Focus, len(st.Tabs))
		}
		// The first read sweeps the whole file, the second jumps to line 9 and reads 5 lines.
		var want []int
		for i := range 50 {
			want = append(want, i)
		}
		want = append(want, 9, 10, 11, 12, 13)
		if !reflect.DeepEqual(lines, want) {
			t.Fatalf("cursor lines %v", lines)
		}
		// Reading moves the caret alone: nothing is selected.
		if len(sels) != 0 || lines[0] != 0 {
			t.Errorf("selections %q", sels)
		}
		if c := tb.Buf.Carets["main"]; c.Sel != nil || c.Pos != (ide.Pos{Line: 13, Col: 7}) {
			t.Errorf("after the read: caret %v, selection %v; want the end of the last line read, nothing selected", c.Pos, c.Sel)
		}
	})

	t.Run("unreadable", func(t *testing.T) {
		s, root := setup(t, map[string]string{"a.txt": ""})
		path := filepath.Join(root, "gone.txt")
		s.ToolUse("main", "r1", "Read", map[string]any{"file_path": path})
		now := play(s, t0, 2*time.Second, nil)
		if !s.Busy() || len(s.State().Tabs) != 0 {
			t.Fatal("an unreadable file must wait for the result")
		}
		s.ToolResult("r1", "1\tfirst\n2\t\tsetting 🙂\n3\t\n\n<system-reminder>note</system-reminder>", false)
		play(s, now, 5*time.Second, nil)
		if got := tab(t, s, path).Buf.Text(); got != "first\n    setting 🙂\n" {
			t.Fatalf("buffer %q", got)
		}
		if len(s.State().Tree) != 1 {
			t.Fatal("a file that is not on disk must not appear in the explorer")
		}

		s.ToolUse("main", "r2", "Read", map[string]any{"file_path": filepath.Join(root, "nope")})
		s.ToolResult("r2", "File does not exist.", true)
		play(s, now, 5*time.Second, nil)
		if s.Busy() || len(s.State().Tabs) != 1 {
			t.Fatal("a failed read opens nothing")
		}
	})

	// A picture comes back as an image block, which carries no text: the editor cannot show it.
	t.Run("picture", func(t *testing.T) {
		s, root := setup(t, map[string]string{"picture.png": "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"})
		path := filepath.Join(root, "picture.png")
		s.ToolUse("main", "r", "Read", map[string]any{"file_path": path})
		s.ToolResult("r", "", false)
		play(s, t0, 5*time.Second, nil)
		if s.Busy() || len(s.State().Tabs) != 0 {
			t.Fatalf("busy %v, %d tabs: a picture opens nothing", s.Busy(), len(s.State().Tabs))
		}
	})
}

// A long file is read to its end, a few lines per step, within readCap.
func TestReadLongFile(t *testing.T) {
	var many []string
	for i := range 3000 {
		many = append(many, fmt.Sprint("line ", i+1))
	}
	s, root := setup(t, map[string]string{"big.txt": strings.Join(many, "\n") + "\n"})
	path := filepath.Join(root, "big.txt")
	s.ToolUse("main", "r", "Read", map[string]any{"file_path": path})
	last := 0
	end := play(s, t0, 10*time.Second, func() {
		if tb := s.State().FindTab(path); tb != nil {
			last = max(last, tb.Buf.Carets["main"].Pos.Line)
		}
	})
	if last != 2999 || end.Sub(t0) > readCap+time.Second {
		t.Errorf("read to line %d in %v", last, end.Sub(t0))
	}
}

// Only an edit that succeeded marks its file as changed.
func TestEdited(t *testing.T) {
	s, root := setup(t, map[string]string{"a.go": "x := 1\n", "b.go": "y := 1\n"})
	a, b := filepath.Join(root, "a.go"), filepath.Join(root, "b.go")
	s.ToolUse("main", "r", "Read", map[string]any{"file_path": b})
	s.ToolUse("main", "ea", "Edit", editInput(a, "1", "2", false))
	s.ToolUse("main", "eb", "Edit", editInput(b, "1", "2", false))
	s.ToolResult("r", "", false)
	s.ToolResult("ea", "", false)
	s.ToolResult("eb", "String to replace not found", true)
	play(s, t0, 10*time.Second, nil)
	if st := s.State(); !st.Edited[a] || st.Edited[b] || st.Touched[b] != "main" {
		t.Errorf("edited %v, touched %v", st.Edited, st.Touched)
	}
}

func TestHugeJump(t *testing.T) {
	s, root := setup(t, map[string]string{"a.go": "x := 1\n"})
	a := filepath.Join(root, "a.go")
	s.ToolUse("main", "b", "Bash", map[string]any{"command": "go test ./..."})
	s.ToolUse("main", "e", "Edit", editInput(a, "1", "2", false))
	writeFile(t, a, "x := 2\n") // the Edit tool ran
	s.ToolUse("main", "w", "Write", map[string]any{"file_path": filepath.Join(root, "b.go"), "content": "package b\n"})
	s.ToolUse("main", "r", "Read", map[string]any{"file_path": a})
	for _, id := range []string{"b", "e", "w", "r"} {
		s.ToolResult(id, "ok", false)
	}
	if s.Tick(t0) != true || !s.Busy() {
		t.Fatal("first tick must start playing")
	}
	s.Tick(t0.Add(time.Hour))
	st, m := s.State(), mainAgent(s)
	if s.Busy() || m.Term.Running || len(m.Term.Lines) != 3 || len(st.Tabs) != 2 || m.Activity != "" { // command, output, gap
		t.Fatalf("busy %v running %v lines %d tabs %d activity %q", s.Busy(), m.Term.Running, len(m.Term.Lines), len(st.Tabs), m.Activity)
	}
	if tab(t, s, a).Buf.Text() != "x := 2\n" || m.File != a || st.Tabs[1].Buf.Modified {
		t.Fatal("wrong final editor state")
	}
	if s.Tick(t0.Add(2 * time.Hour)) {
		t.Fatal("an idle tick changes nothing")
	}
}

func TestResultBeforeTickAndUnknown(t *testing.T) {
	s, root := setup(t, map[string]string{"a.txt": "a\n"})
	s.ToolResult("unknown", "x", false)
	s.ToolResult("unknown", "x", true)
	if s.Busy() || s.Tick(t0) {
		t.Fatal("an unknown result must not start anything")
	}
	path := filepath.Join(root, "a.txt")
	s.ToolUse("main", "e1", "Edit", editInput(path, "a", "b", false))
	s.ToolResult("e1", "ok", false)
	s.ToolResult("other", "", true)
	play(s, t0.Add(time.Second), 5*time.Second, nil)
	if tb := tab(t, s, path); s.Busy() || tb.Buf.Text() != "b\n" || tb.Buf.Modified {
		t.Fatalf("busy %v %q modified %v", s.Busy(), tb.Buf.Text(), tb.Buf.Modified)
	}
	s.ToolResult("e1", "late duplicate", true) // already settled: ignored
	if tab(t, s, path).Buf.Text() != "b\n" {
		t.Fatal("a duplicate result must be ignored")
	}
}

// TestCatchUp: a backlog plays much faster than the same scenes one by one.
func TestCatchUp(t *testing.T) {
	const n = 12
	files := map[string]string{}
	for i := range n {
		var b strings.Builder
		for l := range 40 {
			fmt.Fprintf(&b, "line %d of file %d\n", l, i)
		}
		files[fmt.Sprintf("f%02d.txt", i)] = b.String()
	}
	edit := func(s *Sim, root string, i int) {
		id := fmt.Sprint("e", i)
		old := fmt.Sprintf("line 30 of file %d", i)
		s.ToolUse("main", id, "Edit", editInput(filepath.Join(root, fmt.Sprintf("f%02d.txt", i)), old, old+" // reviewed and changed by the agent", false))
		s.ToolResult(id, "ok", false)
	}

	var solo time.Duration
	for i := range n {
		s, root := setup(t, files)
		edit(s, root, i)
		solo += play(s, t0, time.Minute, nil).Sub(t0)
	}
	s, root := setup(t, files)
	for i := range n {
		edit(s, root, i)
	}
	queued := play(s, t0, time.Minute, nil).Sub(t0)
	if s.Busy() || queued > solo/2 {
		t.Fatalf("queued %v vs one by one %v", queued, solo)
	}
	for i := n - 8; i < n; i++ { // the 8 tabs still open
		if !strings.Contains(tab(t, s, filepath.Join(root, fmt.Sprintf("f%02d.txt", i))).Buf.Text(), "reviewed") {
			t.Fatalf("edit %d missing", i)
		}
	}
}

// TestCaps: huge inputs still play within the per-scene caps.
func TestCaps(t *testing.T) {
	s, root := setup(t, nil)
	s.ToolUse("main", "b", "Bash", map[string]any{"command": strings.Repeat("x", 5000)})
	s.ToolResult("b", strings.Repeat("out\n", 5000), false)
	end := play(s, t0, time.Minute, nil)
	if d := end.Sub(t0); s.Busy() || d > 2300*time.Millisecond {
		t.Fatalf("bash took %v", d)
	}
	if n := len(mainAgent(s).Term.Lines); n != 1000 {
		t.Fatalf("scrollback %d", n)
	}

	path := filepath.Join(root, "big.txt")
	s.ToolUse("main", "w", "Write", map[string]any{"file_path": path, "content": strings.Repeat("all 🙂\n", 4000)})
	s.ToolResult("w", "ok", false)
	start := end.Add(time.Second)
	end = play(s, start, time.Minute, nil)
	if d := end.Sub(start); s.Busy() || d > 3200*time.Millisecond {
		t.Fatalf("write took %v", d)
	}
}

func TestDeterministic(t *testing.T) {
	files := map[string]string{"a.go": "a\nb\nc\n"}
	run := func(root string, s *Sim) {
		s.ToolUse("main", "b", "Bash", map[string]any{"command": "ls -la"})
		s.ToolUse("main", "e", "Edit", editInput(filepath.Join(root, "a.go"), "b", "beta\ngamma", false))
		s.ToolUse("sub", "e2", "Edit", editInput(filepath.Join(root, "a.go"), "c", "charlie", false))
		s.ToolUse("sub", "b2", "Bash", map[string]any{"command": "go vet"})
		for _, id := range []string{"b", "e", "e2", "b2"} {
			s.ToolResult(id, "ok", false)
		}
	}
	s1, r1 := setup(t, files)
	s2, _ := setup(t, files)
	s2.SetRoot(r1) // same paths in both
	run(r1, s1)
	run(r1, s2)
	for now := t0; s1.Busy(); now = now.Add(7 * time.Millisecond) {
		c1, c2 := s1.Tick(now), s2.Tick(now)
		if c1 != c2 || !reflect.DeepEqual(s1.State(), s2.State()) {
			t.Fatalf("diverged at %v", now.Sub(t0))
		}
	}
	if got := tab(t, s1, filepath.Join(r1, "a.go")).Buf.Text(); got != "a\nbeta\ngamma\ncharlie\n" {
		t.Fatalf("buffer %q", got)
	}
}

// TestReplayDemo plays the tool calls of testdata/demo.jsonl against the recorded project, which
// already holds the result of every edit.
func TestReplayDemo(t *testing.T) {
	files := map[string]string{}
	for _, name := range []string{"calc.py", "test_calc.py", "README.md"} {
		data, err := os.ReadFile(filepath.Join("..", "testdata", "demo", name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(data)
	}
	s, root := setup(t, files)
	calc, test := filepath.Join(root, "calc.py"), filepath.Join(root, "test_calc.py")
	s.ToolUse("main", "q", "AskUserQuestion", map[string]any{})
	s.ToolUse("main", "g", "Grep", map[string]any{"pattern": "def add", "path": root})
	s.ToolResult("g", "Error: No such tool available: Grep.", true)
	s.ToolUse("main", "r", "Read", map[string]any{"file_path": calc})
	s.ToolResult("r", "1\t\"\"\"Tiny calculator module.\"\"\"", false)
	s.ToolUse("main", "e", "Edit", editInput(calc, "def sub(a, b):\n    return a - b\n",
		"def sub(a, b):\n    return a - b\n\n\ndef div(a, b):\n    return a / b\n", false))
	s.ToolResult("e", "updated", false)
	s.ToolUse("main", "w", "Write", map[string]any{"file_path": test, "content": files["test_calc.py"]})
	s.ToolResult("w", "created", false)
	s.ToolUse("main", "b", "Bash", map[string]any{"command": "python3 test_calc.py; echo \"exit=$?\""})
	s.ToolResult("b", "exit=0", false)

	var sawPre, sawEmptyWrite bool
	watch(s, t0, time.Minute, func() {
		if tb := s.State().FindTab(calc); tb != nil && tb.Buf.Text() == calcPre {
			sawPre = true
		}
		if tb := s.State().FindTab(test); tb != nil && tb.Buf.Text() == "" {
			sawEmptyWrite = true
		}
	})
	if s.Busy() {
		t.Fatal("still busy")
	}
	if got := tab(t, s, calc).Buf.Text(); got != files["calc.py"] || !sawPre {
		t.Fatalf("calc.py (saw pre-edit text: %v)\n%s", sawPre, got)
	}
	if got := tab(t, s, test).Buf.Text(); got != files["test_calc.py"] || !sawEmptyWrite {
		t.Fatalf("test_calc.py\n%s", got)
	}
	if got := termTexts(mainAgent(s), ide.ErrorOutput); len(got) != 1 {
		t.Fatalf("error output %q", got)
	}
	if got := termTexts(mainAgent(s), ide.Output); !reflect.DeepEqual(got, []string{"exit=0"}) {
		t.Fatalf("output %q", got)
	}
}

// TestIdleStart: a scene that arrives while idle starts at the Tick that finds it; the time spent
// idle is not played back in one jump.
func TestIdleStart(t *testing.T) {
	s, _ := setup(t, nil)
	s.Tick(t0)
	s.ToolUse("main", "b", "Bash", map[string]any{"command": "ls"})
	now := t0.Add(200 * time.Millisecond)
	s.Tick(now)
	s.Tick(now.Add(10 * time.Millisecond))
	if m := mainAgent(s); m.Focus != ide.Terminal || m.Term.Input != "" {
		t.Fatalf("focus %v input %q: typing started before the scene did", m.Focus, m.Term.Input)
	}
	s.Tick(now.Add(100 * time.Millisecond))
	if in := mainAgent(s).Term.Input; in != "ls" {
		t.Fatalf("input %q", in)
	}
}

// SetRoot points the explorer elsewhere; the same root again keeps it as it is.
func TestSetRoot(t *testing.T) {
	s, root := setup(t, map[string]string{"src/a.go": ""})
	st, m := s.State(), mainAgent(s)
	if m.File != "" || m.TreeSel != "" {
		t.Fatalf("file %q sel %q", m.File, m.TreeSel)
	}
	st.Reveal(filepath.Join(root, "src/a.go"))
	s.SetRoot(root + "/")
	if len(st.Tree) != 2 {
		t.Fatal("the same root must keep the expanded folders")
	}
	other := t.TempDir()
	s.SetRoot(other)
	if st.Root != other || len(st.Tree) != 0 {
		t.Fatalf("root %q rows %d", st.Root, len(st.Tree))
	}
}

// View puts a file the user clicked in the right column as it is on disk, until an agent's next
// call plays or the camera is pinned; a binary file is not shown.
func TestView(t *testing.T) {
	s, root := setup(t, map[string]string{"a.go": "package a\n", "b.bin": "x\x00y"})
	st, path := s.State(), filepath.Join(root, "a.go")
	if !s.View(path) || st.Viewed != path || st.FindTab(path).Buf.Text() != "package a\n" {
		t.Fatalf("viewed %q tab %v", st.Viewed, st.FindTab(path))
	}
	if s.View(filepath.Join(root, "b.bin")) || st.Viewed != path {
		t.Fatalf("a binary file shown: viewed %q", st.Viewed)
	}
	s.Pin("")
	if st.Viewed != "" {
		t.Fatal("a pin left the file on screen")
	}
	s.View(path)
	s.ToolUse("main", "c1", "Bash", map[string]any{"command": "ls"})
	if s.Tick(t0); st.Viewed != "" {
		t.Fatal("the agent's next call left the file on screen")
	}
}

// A blank line follows each command's output, so the next prompt stands apart; claude's note for
// an empty result shows as nothing.
func TestCommandGap(t *testing.T) {
	s, _ := setup(t, nil)
	s.ToolUse("main", "a", "Bash", map[string]any{"command": "true"})
	s.ToolResult("a", "(Bash completed with no output)", false)
	s.ToolUse("main", "b", "Bash", map[string]any{"command": "echo hi"})
	s.ToolResult("b", "hi", false)
	play(s, t0, 30*time.Second, nil)
	var got []string
	for _, l := range mainAgent(s).Term.Lines {
		got = append(got, fmt.Sprint(l.Kind, ":", l.Text))
	}
	want := []string{
		fmt.Sprint(ide.Command, ":true"), fmt.Sprint(ide.Gap, ":"),
		fmt.Sprint(ide.Command, ":echo hi"), fmt.Sprint(ide.Output, ":hi"), fmt.Sprint(ide.Gap, ":"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("lines %q, want %q", got, want)
	}
}

// A held call plays only once permitted; a denied one never does, and stops if it was playing.
func TestHold(t *testing.T) {
	s, root := setup(t, map[string]string{"calc.py": calcPre})
	path := filepath.Join(root, "calc.py")
	s.ToolUse("main", "e1", "Edit", editInput(path, "return a - b", "return a - b  # sub", false))
	s.Hold("e1")
	s.Hold("e2") // a call that comes later, from a transcript
	s.ToolUse("main", "e2", "Edit", editInput(path, "return a + b", "return a + b  # add", false))
	now := play(s, t0, 3*time.Second, nil)
	if s.State().FindTab(path) != nil || mainAgent(s).Activity != "" {
		t.Fatalf("a held call played: activity %q", mainAgent(s).Activity)
	}
	s.Release("e1")
	s.ToolResult("e1", "ok", false)
	s.Drop("e2")
	s.ToolResult("e2", "The user denied this tool call.", true)
	play(s, now, 10*time.Second, nil)
	if got := tab(t, s, path).Buf.Text(); !strings.Contains(got, "# sub") || strings.Contains(got, "# add") {
		t.Fatalf("text %q", got)
	}

	// Denied mid-scene: the typing stops and the file is as on disk again.
	s, root = setup(t, map[string]string{"calc.py": calcPre})
	path = filepath.Join(root, "calc.py")
	s.ToolUse("main", "e3", "Edit", editInput(path, "return a - b", "return a - b  # a long comment typed slowly", false))
	now = watch(s, t0, 10*time.Second, func() {
		if tb := s.State().FindTab(path); tb != nil && strings.Contains(tb.Buf.Text(), "# a l") {
			s.Hold("e3") // the request came late
		}
	})
	if got := tab(t, s, path).Buf.Text(); !strings.Contains(got, "# a l") || strings.Contains(got, "slowly") {
		t.Fatalf("held mid-scene: %q", got)
	}
	s.Drop("e3")
	play(s, now, time.Second, nil)
	if tb := tab(t, s, path); tb.Buf.Text() != calcPre || tb.Buf.Modified || s.Busy() {
		t.Fatalf("after the denial: %q modified %v busy %v", tb.Buf.Text(), tb.Buf.Modified, s.Busy())
	}

	// A command denied mid-typing leaves the terminal's prompt empty for the next one.
	s, _ = setup(t, nil)
	m := mainAgent(s)
	s.ToolUse("main", "b0", "Bash", map[string]any{"command": "rm -rf build &&\nmake all"})
	now = watch(s, t0, 10*time.Second, func() {
		if m.Term.Input == "ma" {
			s.Hold("b0")
		}
	})
	s.Drop("b0")
	s.ToolUse("main", "b2", "Bash", map[string]any{"command": "ls"})
	s.ToolResult("b2", "", false)
	play(s, now, 10*time.Second, nil)
	if got := termTexts(m, ide.Command); !reflect.DeepEqual(got, []string{"rm -rf build &&", "ls"}) || m.Term.Input != "" || m.Term.Running {
		t.Fatalf("after a denied command: commands %q, input %q, running %v", got, m.Term.Input, m.Term.Running)
	}
	// Denied once typed, while it waits to run: the spinner stops.
	s, _ = setup(t, nil)
	m = mainAgent(s)
	s.ToolUse("main", "b3", "Bash", map[string]any{"command": "make"})
	now = play(s, t0, 10*time.Second, func() {
		if m.Term.Running {
			s.Hold("b3")
		}
	})
	s.Drop("b3")
	if play(s, now, time.Second, nil); m.Term.Running || s.Busy() || m.Term.Lines[len(m.Term.Lines)-1].Kind != ide.Gap {
		t.Fatalf("after a denied running command: running %v busy %v lines %v", m.Term.Running, s.Busy(), m.Term.Lines)
	}

	// A result while held (an interrupt, an exit): the call never ran.
	s, _ = setup(t, nil)
	s.ToolUse("main", "b1", "Bash", map[string]any{"command": "make"})
	s.Hold("b1")
	s.ToolResult("b1", "", true)
	if play(s, t0, time.Second, nil); s.Busy() || len(mainAgent(s).Term.Lines) != 0 {
		t.Fatalf("busy %v, terminal %v", s.Busy(), mainAgent(s).Term.Lines)
	}
}
