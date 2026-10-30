package sim

import (
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pixelbender/claudit/internal/ide"
)

func TestAgents(t *testing.T) {
	s, _ := setup(t, nil)
	m := mainAgent(s)
	want := ide.Agent{Key: "main", Kind: ide.Main, Name: "main", Status: ide.Running, Hotkey: 1, Color: 0, Term: m.Term}
	if len(s.State().Agents) != 1 || !reflect.DeepEqual(*m, want) || s.State().Camera != "main" || s.State().Pinned {
		t.Fatalf("main %+v camera %q", *m, s.State().Camera)
	}
	sub := s.Agent("toolu_1", ide.Subagent)
	wf := s.Agent("toolu_2", ide.Workflow)
	w1 := s.Agent("toolu_2#1", ide.WorkflowAgent)
	if s.Agent("toolu_1", ide.WorkflowAgent) != sub || s.FindAgent("toolu_2") != wf || s.FindAgent("nope") != nil {
		t.Fatal("Agent must return the known agent, FindAgent nil for an unknown key")
	}
	got := func(a *ide.Agent) string { return fmt.Sprint(a.Kind, a.Status, a.Hotkey, a.Color, a.Started.IsZero()) }
	for _, c := range []struct {
		a    *ide.Agent
		want string
	}{{sub, "1 0 2 1 true"}, {wf, "2 0 0 2 true"}, {w1, "3 0 3 3 true"}} {
		if g := got(c.a); g != c.want {
			t.Errorf("%s: kind status hotkey color = %s, want %s", c.a.Key, g, c.want)
		}
	}
	for i := range 7 {
		s.Agent(fmt.Sprint("a", i), ide.Subagent)
	}
	var keys []int
	for _, a := range s.State().Agents {
		keys = append(keys, a.Hotkey)
	}
	if want := []int{1, 2, 0, 3, 4, 5, 6, 7, 8, 9, 0}; !reflect.DeepEqual(keys, want) {
		t.Fatalf("hotkeys %v, want %v", keys, want)
	}

	// A tool call of an unknown agent adds it as a subagent.
	s.ToolUse("toolu_9", "x", "TodoWrite", nil)
	if a := s.FindAgent("toolu_9"); a == nil || a.Kind != ide.Subagent || a.Color != 11 {
		t.Fatalf("unknown agent %+v", a)
	}
}

// The ui drops a queued placeholder whose agent its transcript showed first: agents added later
// still get a hotkey and a colour of their own.
func TestAgentsAfterRemoval(t *testing.T) {
	s, _ := setup(t, nil)
	s.Agent("wf", ide.Workflow)
	s.Agent("wf#1.1", ide.WorkflowAgent)
	a1 := s.Agent("a1", ide.WorkflowAgent)
	st := s.State()
	st.Agents = slices.DeleteFunc(st.Agents, func(a *ide.Agent) bool { return a.Key == "wf#1.1" })
	late := s.Agent("toolu_9", ide.Subagent)
	if late.Hotkey == a1.Hotkey || late.Color == a1.Color {
		t.Fatalf("late agent hotkey %d color %d, the same as %+v", late.Hotkey, late.Color, *a1)
	}
	if late.Hotkey != 2 {
		t.Fatalf("late agent hotkey %d, want 2: the one the dropped agent had", late.Hotkey)
	}
}

// A queued workflow agent gets its id: everything that knew it by the old key follows.
func TestRekey(t *testing.T) {
	s, root := setup(t, map[string]string{"a.txt": "one\ntwo\n"})
	path := filepath.Join(root, "a.txt")
	w := s.Agent("wf#1", ide.WorkflowAgent)
	child := s.Agent("c", ide.Subagent)
	child.Parent = "wf#1"
	s.Pin("wf#1")
	s.ToolUse("wf#1", "e", "Edit", editInput(path, "two", "deux", false))
	s.ToolResult("e", "ok", false)
	now := play(s, t0, 150*time.Millisecond, nil) // the file is open, the edit not typed yet
	if tab(t, s, path).Buf.Carets["wf#1"] == nil {
		t.Fatal("no caret before the rekey")
	}

	s.Rekey("wf#1", "a1b2c3")
	play(s, now, 10*time.Second, nil)
	st, tb := s.State(), tab(t, s, path)
	if w.Key != "a1b2c3" || child.Parent != "a1b2c3" || st.Camera != "a1b2c3" || !st.Pinned || st.Touched[path] != "a1b2c3" {
		t.Fatalf("key %q parent %q camera %q touched %v", w.Key, child.Parent, st.Camera, st.Touched)
	}
	if _, old := tb.Buf.Carets["wf#1"]; old || tb.Buf.Carets["a1b2c3"] == nil || tb.Buf.Text() != "one\ndeux\n" {
		t.Fatalf("carets %v text %q", tb.Buf.Carets, tb.Buf.Text())
	}

	s.Rekey("nope", "x")      // unknown: nothing
	s.Rekey("a1b2c3", "main") // taken: nothing
	if s.FindAgent("x") != nil || w.Key != "a1b2c3" || len(st.Agents) != 3 {
		t.Fatal("a rekey of an unknown key or onto a known one must do nothing")
	}
}

func TestTouched(t *testing.T) {
	s, root := setup(t, map[string]string{"a.txt": "a\n", "b.txt": "b\n"})
	a, b, c := filepath.Join(root, "a.txt"), filepath.Join(root, "b.txt"), filepath.Join(root, "new/c.txt")
	s.ToolUse("main", "r", "Read", map[string]any{"file_path": a})
	s.ToolUse("sub", "e", "Edit", editInput(b, "b", "B", false))
	s.ToolUse("sub", "w", "Write", map[string]any{"file_path": c, "content": "c"})
	s.ToolUse("sub", "g", "Grep", map[string]any{"pattern": "x", "path": b}) // a search touches nothing
	for _, id := range []string{"r", "e", "w", "g"} {
		s.ToolResult(id, "", false)
	}
	now := play(s, t0, 10*time.Second, nil)
	if want := map[string]string{a: "main", b: "sub", c: "sub"}; !reflect.DeepEqual(s.State().Touched, want) {
		t.Fatalf("touched %v, want %v", s.State().Touched, want)
	}
	s.ToolUse("main", "e2", "Edit", editInput(b, "B", "b", false))
	s.ToolResult("e2", "", false)
	play(s, now, 10*time.Second, nil)
	if got := s.State().Touched[b]; got != "main" {
		t.Fatalf("b.txt last touched by %q", got)
	}
}

// Two agents edit the same file at once: each one's caret and selection are moved by the other's
// typing and both edits land where they belong.
func TestTwoAgentsOneFile(t *testing.T) {
	var rows []string
	for i := 1; i <= 50; i++ {
		rows = append(rows, fmt.Sprintf("row %02d", i))
	}
	pre := strings.Join(rows, "\n") + "\n"
	var added []string
	for i := 1; i <= 10; i++ {
		added = append(added, fmt.Sprint("added ", i))
	}
	subNew := "ROW 02\n" + strings.Join(added, "\n") // lines above main's edit
	mainNew := "ROW 40\nROW 40b"
	post := strings.Replace(strings.Replace(pre, "row 02", subNew, 1), "row 40", mainNew, 1)

	tests := []struct {
		name   string
		onDisk bool          // both tools ran already, as when a workflow transcript is read
		subAt  time.Duration // when sub's call comes
	}{
		{"live", false, 0},
		{"already on disk", true, 0},
		{"already on disk, sub while main selects", true, 400 * time.Millisecond},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			disk := pre
			if tc.onDisk {
				disk = post
			}
			s, root := setup(t, map[string]string{"f.txt": disk})
			path := filepath.Join(root, "f.txt")
			s.ToolUse("main", "m", "Edit", editInput(path, "row 40", mainNew, false))
			s.ToolResult("m", "ok", false)
			var stale, overlap bool
			see := func() {
				tb := s.State().FindTab(path)
				if tb == nil {
					return
				}
				text, mc, sc := tb.Buf.Text(), tb.Buf.Carets["main"], tb.Buf.Carets["sub"]
				// main has planned its edit but not reached it while sub adds lines above it
				stale = stale || mc != nil && mc.Sel == nil && sc != nil && strings.Contains(text, "added 1\n") && !strings.Contains(text, "ROW 40")
				// main selects while sub's edit is under way
				overlap = overlap || mc != nil && mc.Sel != nil && sc != nil && !strings.Contains(text, "added 10")
			}
			now := watch(s, t0, tc.subAt, see)
			s.ToolUse("sub", "s", "Edit", editInput(path, "row 02", subNew, false))
			s.ToolResult("s", "ok", false)
			watch(s, now, 10*time.Second, see)
			if s.Busy() {
				t.Fatal("still busy")
			}
			tb := tab(t, s, path)
			if got := tb.Buf.Text(); got != post {
				t.Fatalf("text\n%s\nwant\n%s", got, post)
			}
			if !overlap || !tc.onDisk && !stale {
				t.Fatalf("the edits did not overlap: main selecting %v, main's target moved %v", overlap, stale)
			}
			mc, sc := tb.Buf.Carets["main"], tb.Buf.Carets["sub"]
			if mc.Pos != (ide.Pos{Line: 50, Col: 7}) || sc.Pos != (ide.Pos{Line: 11, Col: 8}) || mc.Sel != nil || sc.Sel != nil {
				t.Fatalf("carets main %+v sub %+v", *mc, *sc)
			}
			if tb.Buf.Modified || s.State().Touched[path] == "" {
				t.Fatal("not saved")
			}
		})
	}
}

// Another agent deletes the very text an agent is selecting: the selection stops there, nothing
// else is deleted, and the caret stays inside the buffer.
func TestSelectionTextGone(t *testing.T) {
	s, root := setup(t, map[string]string{"f.txt": "aaa\nbbb\nccc\nddd\neee\nfff"})
	path := filepath.Join(root, "f.txt")
	s.ToolUse("x", "e", "Edit", editInput(path, "ccc\nddd", "Z", false))
	s.ToolResult("e", "ok", false)
	var buf *ide.Buffer
	now := t0
	for ; buf == nil; now = now.Add(time.Millisecond) {
		if !s.Tick(now) && !s.Busy() {
			t.Fatal("x never selected")
		}
		if tb := s.State().FindTab(path); tb != nil && sel(tb, "x") != nil && sel(tb, "x").To != sel(tb, "x").From {
			buf = &tb.Buf
		}
	}
	buf.Delete("main", ide.Pos{Line: 2}, ide.Pos{Line: 4}) // "ccc\nddd\n"
	watch(s, now, 10*time.Second, func() {
		if c := buf.Carets["x"]; c.Pos != buf.Clamp(c.Pos) {
			t.Fatalf("x at %+v outside %q", c.Pos, buf.Text())
		}
	})
	if got := buf.Text(); got != "aaa\nbbb\nZeee\nfff" {
		t.Fatalf("text %q: x deleted what was not its old_string", got)
	}
}

// A lane waits for its command; the other lanes go on meanwhile.
func TestLaneBlockedOnBash(t *testing.T) {
	s, root := setup(t, map[string]string{"a.go": "x := 1\n"})
	path := filepath.Join(root, "a.go")
	s.ToolUse("sub", "b", "Bash", map[string]any{"command": "make"})
	s.ToolUse("main", "r", "Read", map[string]any{"file_path": path})
	s.ToolUse("main", "e", "Edit", editInput(path, "1", "2", false))
	s.ToolResult("r", "", false)
	s.ToolResult("e", "", false)

	now := play(s, t0, 5*time.Second, nil)
	m, sub := mainAgent(s), s.FindAgent("sub")
	if !s.Busy() || !sub.Term.Running || sub.Activity != "$ make" || sub.Focus != ide.Terminal {
		t.Fatalf("sub: busy %v running %v activity %q", s.Busy(), sub.Term.Running, sub.Activity)
	}
	if tab(t, s, path).Buf.Text() != "x := 2\n" || m.Activity != "" || m.Focus != ide.Editor || len(m.Term.Lines) != 0 {
		t.Fatalf("main did not go on: %q activity %q focus %v", tab(t, s, path).Buf.Text(), m.Activity, m.Focus)
	}
	if got := termTexts(sub, ide.Command); !reflect.DeepEqual(got, []string{"make"}) {
		t.Fatalf("sub terminal %q", got)
	}

	s.ToolResult("b", "built", false)
	play(s, now, 5*time.Second, nil)
	if s.Busy() || sub.Term.Running || sub.Activity != "" || !reflect.DeepEqual(termTexts(sub, ide.Output), []string{"built"}) {
		t.Fatalf("busy %v running %v activity %q", s.Busy(), sub.Term.Running, sub.Activity)
	}
}

// The sim sets Activity while a lane plays and clears it when the lane goes idle, but leaves
// alone what the ui wrote for an agent whose lane is idle.
func TestActivity(t *testing.T) {
	s, root := setup(t, map[string]string{"a.txt": "a\n"})
	w := s.Agent("w", ide.WorkflowAgent)
	w.Activity = "Bash go test"
	s.ToolUse("main", "b", "Bash", map[string]any{"command": "ls\n-la"})
	s.ToolUse("main", "r", "Read", map[string]any{"file_path": filepath.Join(root, "a.txt")})
	s.ToolUse("main", "g", "Grep", map[string]any{"pattern": "def add", "path": root})
	for _, id := range []string{"b", "r", "g"} {
		s.ToolResult(id, "", false)
	}
	m := mainAgent(s)
	var seen []string
	now := play(s, t0, 10*time.Second, func() {
		if len(seen) == 0 || seen[len(seen)-1] != m.Activity {
			seen = append(seen, m.Activity)
		}
	})
	if want := []string{"$ ls", "Read a.txt", "rg def add", ""}; !reflect.DeepEqual(seen, want) {
		t.Fatalf("activities %q, want %q", seen, want)
	}
	m.Activity = "thinking"
	play(s, now, time.Second, nil)
	if w.Activity != "Bash go test" || m.Activity != "thinking" {
		t.Fatalf("activities written by the ui: %q %q", w.Activity, m.Activity)
	}
}

// Tabs other agents work in stay open when the 8-tab cap closes one.
func TestTabCap(t *testing.T) {
	files := map[string]string{"s1.txt": "s1\n", "s2.txt": "s2\n"}
	for i := range 10 {
		files[fmt.Sprintf("f%02d.txt", i)] = "f\n"
	}
	s, root := setup(t, files)
	p := func(n string) string { return filepath.Join(root, n) }
	s.ToolUse("s1", "s1", "Read", map[string]any{"file_path": p("s1.txt")})
	s.ToolUse("s2", "s2", "Read", map[string]any{"file_path": p("s2.txt")})
	for i := range 10 {
		s.ToolUse("main", fmt.Sprint("r", i), "Read", map[string]any{"file_path": p(fmt.Sprintf("f%02d.txt", i))})
	}
	var most int
	play(s, t0, time.Minute, func() { most = max(most, len(s.State().Tabs)) })
	st := s.State()
	if most != 8 || len(st.Tabs) != 8 || s.Busy() {
		t.Fatalf("%d tabs at most, %d at the end", most, len(st.Tabs))
	}
	for _, key := range []string{"s1", "s2"} {
		if tab(t, s, p(key+".txt")).Buf.Carets[key] == nil {
			t.Fatalf("%s lost its agent's caret", key)
		}
	}
	if st.FindTab(p("f00.txt")) != nil || tab(t, s, p("f09.txt")).Buf.Carets["main"] == nil {
		t.Fatal("main's old files must close, its last one must hold its caret")
	}
	for _, tb := range st.Tabs {
		if c := tb.Buf.Carets["main"]; c != nil && tb.Path != p("f09.txt") {
			t.Fatalf("main's caret left behind in %s", tb.Title)
		}
	}
}

// Catch-up is per lane: a backlog on one lane does not hurry another.
func TestLaneCatchUp(t *testing.T) {
	idle := func(s *Sim) time.Time { // when the lane of sub has played its command
		l := s.lanes[s.FindAgent("sub")]
		for now := t0; now.Before(t0.Add(time.Minute)); now = now.Add(frame) {
			s.Tick(now)
			if l.cur == nil && len(l.queue) == 0 {
				return now
			}
		}
		return time.Time{}
	}
	alone, _ := setup(t, nil)
	alone.ToolUse("sub", "x", "Bash", map[string]any{"command": "ls -la"})
	alone.ToolResult("x", "out", false)

	s, _ := setup(t, nil)
	s.ToolUse("sub", "x", "Bash", map[string]any{"command": "ls -la"}) // first: the same jitter
	s.ToolResult("x", "out", false)
	for i := range 6 {
		id := fmt.Sprint("m", i)
		s.ToolUse("main", id, "Bash", map[string]any{"command": "go test ./..."})
		s.ToolResult(id, "ok", false)
	}
	if want, got := idle(alone), idle(s); want.IsZero() || !got.Equal(want) {
		t.Fatalf("sub done after %v with main's backlog, %v alone", got.Sub(t0), want.Sub(t0))
	}
}
