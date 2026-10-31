package ide

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// mkTree creates files under root; names ending in "/" are directories.
func mkTree(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, n := range names {
		p := filepath.Join(root, n)
		if strings.HasSuffix(n, "/") {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func names(st *State) []string {
	var out []string
	for _, r := range st.Tree {
		out = append(out, strings.Repeat(" ", r.Depth)+r.Name)
	}
	return out
}

func TestExplorer(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, "b.txt", "A.go", "src/main.go", "src/util/x.go", "Zdir/", ".git/HEAD",
		"node_modules/m.js", "__pycache__/c.pyc", ".DS_Store", "target/", ".idea/", ".venv/")
	var st State
	st.SetRoot(root + "/")

	if want := []string{"src", "Zdir", "A.go", "b.txt"}; !slices.Equal(names(&st), want) || st.Root != root {
		t.Fatalf("initial rows %q root %q, want %q", names(&st), st.Root, want)
	}

	row := st.Reveal(filepath.Join(root, "src/util/x.go"))
	want := []string{"src", " util", "  x.go", " main.go", "Zdir", "A.go", "b.txt"}
	if !slices.Equal(names(&st), want) || row != 2 {
		t.Fatalf("after reveal rows %q row %d, want %q row 2", names(&st), row, want)
	}
	if !st.Tree[0].Expanded || !st.Tree[1].Expanded || st.Tree[4].Expanded {
		t.Fatal("wrong expanded flags")
	}
	if st.Reveal(filepath.Join(root, "src/util/x.go")) != 2 || len(st.Tree) != len(want) {
		t.Fatal("revealing twice must not add rows")
	}

	// A file that is not on disk yet gets its rows, folders included, in sorted order.
	row = st.Reveal(filepath.Join(root, "new/deep.go"))
	want = []string{"new", " deep.go", "src", " util", "  x.go", " main.go", "Zdir", "A.go", "b.txt"}
	if !slices.Equal(names(&st), want) || row != 1 || st.Tree[0].Path != filepath.Join(root, "new") {
		t.Fatalf("after insert rows %q row %d", names(&st), row)
	}
	if row := st.Reveal(filepath.Join(root, "B.md")); row != 8 || st.Tree[row].Name != "B.md" {
		t.Fatalf("B.md at %d in %q", row, names(&st))
	}

	for _, p := range []string{root, filepath.Dir(root), "/elsewhere/x.go", filepath.Join(root, ".git/HEAD"), filepath.Join(root, "b.txt/x")} {
		if row := st.Reveal(p); row != -1 {
			t.Errorf("Reveal(%q) = %d, want -1", p, row)
		}
	}
}

func TestTabs(t *testing.T) {
	var st State
	a := st.Open("/p/a/x.go", "one")
	if len(st.Tabs) != 1 || a.Title != "x.go" || a.Buf.Text() != "one" {
		t.Fatalf("open: %d tabs, title %q", len(st.Tabs), a.Title)
	}
	st.Open("/p/b/x.go", "two")
	if a.Title != "a/x.go" || st.Tabs[1].Title != "b/x.go" {
		t.Fatalf("titles %q %q", a.Title, st.Tabs[1].Title)
	}
	if st.Open("/p/a/x.go", "ignored") != a || a.Buf.Text() != "one" || st.FindTab("/p/a/x.go") != a {
		t.Fatal("reopening must return the open tab as it is")
	}
	if st.FindTab("/p/none") != nil {
		t.Fatal("FindTab of a closed file")
	}
	for i := range 6 {
		st.Open(fmt.Sprintf("/p/f%d", i), "")
	}

	// 8 tabs: the oldest one no agent works in is closed.
	st.Agents = []*Agent{{Key: "main", File: "/p/a/x.go"}, {Key: "sub", File: "/p/f0"}}
	st.Open("/p/new", "")
	if len(st.Tabs) != 8 || st.FindTab("/p/b/x.go") != nil || st.FindTab("/p/a/x.go") == nil || st.FindTab("/p/new") == nil {
		t.Fatalf("cap: tabs %q", titles(&st))
	}
	if a.Title != "x.go" {
		t.Fatalf("title after close %q", a.Title)
	}
	st.Open("/p/newer", "")
	if st.FindTab("/p/f0") == nil || st.FindTab("/p/f1") != nil {
		t.Fatalf("held tabs must stay: %q", titles(&st))
	}

	// Every tab held by an agent: none can close.
	st.Agents = nil
	for _, tb := range st.Tabs {
		st.Agents = append(st.Agents, &Agent{Key: tb.Path, File: tb.Path})
	}
	st.Open("/p/ninth", "")
	if len(st.Tabs) != 9 {
		t.Fatalf("%d tabs, want 9", len(st.Tabs))
	}
}

func titles(st *State) []string {
	var out []string
	for _, tb := range st.Tabs {
		out = append(out, tb.Path)
	}
	return out
}

// Forget undoes a Write of a file that never made it to disk.
// Toggle folds a folder with all below it and unfolds it as it is on disk; a file stays as it is.
// The rows git ignores are flagged: listed, unfolded, or revealed before they are on disk.
func TestExplorerIgnored(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("no git: %v %s", err, out)
	}
	mkTree(t, root, "out/a.bin", "src/x.go", "src/x.log", "main.go")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("/out/\n*.log\n/gen/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var st State
	st.SetRoot(root)
	st.Reveal(filepath.Join(root, "src/x.go"))
	st.Reveal(filepath.Join(root, "out/a.bin"))
	st.Reveal(filepath.Join(root, "gen/deep/y.go")) // not on disk
	st.Reveal(filepath.Join(root, "new.log"))       // not on disk
	want := map[string]bool{"out": true, "out/a.bin": true, "src/x.log": true, "gen": true, "gen/deep": true, "gen/deep/y.go": true, "new.log": true}
	for _, r := range st.Tree {
		rel, _ := filepath.Rel(root, r.Path)
		if r.Ignored != want[rel] {
			t.Errorf("%s ignored %v, want %v", rel, r.Ignored, want[rel])
		}
	}

	var plain State // outside a repository, nothing is ignored
	plain.SetRoot(t.TempDir())
	mkTree(t, plain.Root, "x.log")
	plain.Reveal(filepath.Join(plain.Root, "x.log"))
	if plain.Tree[0].Ignored {
		t.Error("a file outside a repository is ignored")
	}
}

func TestToggle(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, "src/util/x.go", "src/main.go", "b.txt")
	var st State
	st.SetRoot(root)
	st.Reveal(filepath.Join(root, "src/util/x.go"))
	st.Toggle(filepath.Join(root, "src"))
	if want := []string{"src", "b.txt"}; !slices.Equal(names(&st), want) || st.Tree[0].Expanded {
		t.Fatalf("folded: rows %q expanded %v", names(&st), st.Tree[0].Expanded)
	}
	mkTree(t, root, "src/new.go")
	st.Toggle(filepath.Join(root, "src"))
	if want := []string{"src", " util", " main.go", " new.go", "b.txt"}; !slices.Equal(names(&st), want) || !st.Tree[0].Expanded {
		t.Fatalf("unfolded: rows %q", names(&st))
	}
	st.Toggle(filepath.Join(root, "b.txt"))
	st.Toggle(filepath.Join(root, "gone"))
	if len(st.Tree) != 5 {
		t.Fatalf("a file or a missing path changed the rows: %q", names(&st))
	}
}

func TestForget(t *testing.T) {
	root := t.TempDir()
	mkTree(t, root, "on/disk.go", "z.txt")
	path := func(n string) string { return filepath.Join(root, n) }
	var st State
	st.SetRoot(root)
	for _, n := range []string{"on/disk.go", "a/x.go", "new/dir/x.go", "c.go"} {
		st.Reveal(path(n))
		st.Open(path(n), "")
	}
	main := &Agent{Key: "main", File: path("new/dir/x.go")}
	sub := &Agent{Key: "sub", File: path("on/disk.go")}
	st.Agents = []*Agent{main, sub}
	st.Touched = map[string]string{path("new/dir/x.go"): "main", path("on/disk.go"): "sub"}

	st.Forget(path("on/disk.go")) // on disk: kept
	st.Forget(path("new/dir/x.go"))
	want := []string{"a", " x.go", "on", " disk.go", "c.go", "z.txt"}
	if !slices.Equal(names(&st), want) {
		t.Fatalf("rows %q, want %q", names(&st), want)
	}
	if len(st.Tabs) != 3 || st.FindTab(path("new/dir/x.go")) != nil || st.FindTab(path("a/x.go")).Title != "x.go" {
		t.Fatalf("tabs %q", titles(&st))
	}
	if main.File != "" || sub.File != path("on/disk.go") {
		t.Fatalf("files %q %q", main.File, sub.File)
	}
	if _, ok := st.Touched[path("new/dir/x.go")]; ok || st.Touched[path("on/disk.go")] != "sub" {
		t.Fatalf("touched %v", st.Touched)
	}
}

func TestBuffer(t *testing.T) {
	var b Buffer
	b.SetText("hello, world\nsecond")
	c := b.Caret("a")
	if b.Caret("a") != c || len(b.Carets) != 1 || c.Pos != (Pos{}) {
		t.Fatal("Caret must add one caret at the top, once")
	}
	c.Pos = Pos{0, 7}
	b.Insert("a", "🙂\nnew ")
	if got := b.Text(); got != "hello, 🙂\nnew world\nsecond" || c.Pos != (Pos{1, 4}) {
		t.Fatalf("insert: %q at %v", got, c.Pos)
	}
	c.Sel = &Selection{To: Pos{1, 1}}
	b.Delete("a", Pos{1, 4}, Pos{0, 7}) // reversed range
	if got := b.Text(); got != "hello, world\nsecond" || c.Pos != (Pos{0, 7}) || c.Sel != nil {
		t.Fatalf("delete: %q at %v sel %v", got, c.Pos, c.Sel)
	}
	if b.End() != (Pos{1, 6}) || b.Clamp(Pos{9, 9}) != (Pos{1, 6}) || b.Clamp(Pos{-1, -1}) != (Pos{}) {
		t.Fatalf("end %v", b.End())
	}
	c.Pos = Pos{5, 50}
	b.Insert("a", "!") // out-of-range caret is clamped, not a panic
	if b.Text() != "hello, world\nsecond!" || c.Pos != (Pos{1, 7}) {
		t.Fatalf("clamped insert: %q at %v", b.Text(), c.Pos)
	}
	c.Sel = &Selection{To: Pos{1, 3}}
	b.SetText("x")
	if c.Pos != (Pos{0, 1}) || c.Sel != nil {
		t.Fatalf("SetText: caret %v sel %v", c.Pos, c.Sel)
	}

	var empty Buffer
	empty.Insert("k", "x")
	empty.Delete("k", Pos{}, Pos{0, 1})
	if empty.Text() != "" || empty.Caret("k").Pos != (Pos{}) {
		t.Fatalf("empty buffer: %q", empty.Text())
	}
	var none Buffer
	none.Delete("k", Pos{}, Pos{1, 1})
	if none.Carets != nil || none.End() != (Pos{}) || none.Clamp(Pos{3, 3}) != (Pos{}) {
		t.Fatal("a buffer with no lines")
	}
}

// Other agents' carets and selections keep their place in the text when an agent types or deletes.
func TestCaretsShift(t *testing.T) {
	sel := func(a, b, c, d int) *Selection { return &Selection{Pos{a, b}, Pos{c, d}} }
	tests := []struct {
		name string
		edit func(b *Buffer)
		want string
		// other carets: before, after on the same line, on a later line, selection across
		before, same, later Pos
		sel                 *Selection
	}{
		{
			name: "insert one line",
			edit: func(b *Buffer) { b.Caret("me").Pos = Pos{1, 2}; b.Insert("me", "XY") },
			want: "alpha\nbeXYta\ngamma",
			same: Pos{1, 6}, later: Pos{2, 3}, sel: sel(1, 1, 2, 2),
		},
		{
			name: "insert lines",
			edit: func(b *Buffer) { b.Caret("me").Pos = Pos{1, 2}; b.Insert("me", "X\nYZ\n") },
			want: "alpha\nbeX\nYZ\nta\ngamma",
			same: Pos{3, 2}, later: Pos{4, 3}, sel: sel(1, 1, 4, 2),
		},
		{
			name: "delete inside a line",
			edit: func(b *Buffer) { b.Delete("me", Pos{1, 1}, Pos{1, 3}) },
			want: "alpha\nba\ngamma",
			same: Pos{1, 2}, later: Pos{2, 3}, sel: sel(1, 1, 2, 2),
		},
		{
			name: "delete across lines",
			edit: func(b *Buffer) { b.Delete("me", Pos{0, 3}, Pos{1, 2}) },
			want: "alpta\ngamma",
			same: Pos{0, 5}, later: Pos{1, 3}, sel: sel(0, 3, 1, 2),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var b Buffer
			b.SetText("alpha\nbeta\ngamma")
			b.Caret("before").Pos = Pos{0, 2}
			b.Caret("same").Pos = Pos{1, 4}
			b.Caret("later").Pos = Pos{2, 3}
			b.Caret("sel").Sel = sel(1, 1, 2, 2)
			tc.edit(&b)
			if got := b.Text(); got != tc.want {
				t.Fatalf("text %q, want %q", got, tc.want)
			}
			if p := b.Carets["before"].Pos; p != (Pos{0, 2}) {
				t.Errorf("caret before the edit moved to %v", p)
			}
			if p := b.Carets["same"].Pos; p != tc.same {
				t.Errorf("caret after the edit on its line at %v, want %v", p, tc.same)
			}
			if p := b.Carets["later"].Pos; p != tc.later {
				t.Errorf("caret on a later line at %v, want %v", p, tc.later)
			}
			if s := b.Carets["sel"].Sel; *s != *tc.sel {
				t.Errorf("selection %v, want %v", *s, *tc.sel)
			}
		})
	}

	// A caret inside a deleted range lands at its start.
	var b Buffer
	b.SetText("one two three")
	b.Caret("in").Pos = Pos{0, 6}
	b.Delete("me", Pos{0, 4}, Pos{0, 8})
	if p := b.Carets["in"].Pos; b.Text() != "one three" || p != (Pos{0, 4}) {
		t.Fatalf("%q, caret %v", b.Text(), p)
	}
}

func TestScrollback(t *testing.T) {
	var term Term
	for i := range 1005 {
		term.Print(Output, fmt.Sprint(i))
	}
	if len(term.Lines) != 1000 || term.Lines[0].Text != "5" || term.Lines[999].Text != "1004" {
		t.Fatalf("%d lines from %q", len(term.Lines), term.Lines[0].Text)
	}
}

// Glows follow their text: an edit starting a line moves that line's glow with it.
func TestGlowFollowsLines(t *testing.T) {
	at := time.Unix(1000, 0)
	lit := func(b *Buffer) map[int]string {
		m := map[int]string{}
		for _, g := range b.Glow {
			m[g.Line] = b.Lines[g.Line]
		}
		return m
	}
	var b Buffer
	b.SetText("x\ny\nz")
	b.Light("a", 2, 2, at)
	b.Caret("a")
	b.Delete("a", Pos{Line: 0}, Pos{Line: 2}) // "x\ny\n" goes: z moves to line 0
	if got := lit(&b); len(got) != 1 || got[0] != "z" {
		t.Errorf("after deleting whole lines: lit %v in %q", got, b.Text())
	}

	b.SetText("x\ny")
	b.Glow = nil
	b.Light("a", 1, 1, at)
	b.Caret("a").Pos = Pos{Line: 1}
	b.Insert("a", "new\n") // at column 0: y moves down
	if got := lit(&b); len(got) != 1 || got[2] != "y" {
		t.Errorf("after inserting at column 0: lit %v in %q", got, b.Text())
	}

	b.SetText("abc\ndef")
	b.Glow = nil
	b.Light("a", 0, 1, at)
	b.Caret("a").Pos = Pos{Line: 0, Col: 1}
	b.Delete("a", Pos{Line: 0, Col: 1}, Pos{Line: 1, Col: 1}) // joins into "aef": line 0 keeps its glow
	if got := lit(&b); len(got) != 1 || got[0] != "aef" {
		t.Errorf("after joining lines: lit %v in %q", got, b.Text())
	}

	b.SetText("one")
	if len(b.Glow) != 1 {
		t.Errorf("SetText kept a glow past the end: %+v", b.Glow)
	}

	// A glow holds however old it is; once its agent has moved on, it goes when it has faded.
	b.SetText("one\ntwo")
	b.Glow = nil
	b.Light("a", 0, 0, at)
	b.Light("b", 1, 1, at.Add(time.Hour))
	if len(b.Glow) != 2 {
		t.Errorf("a held glow went: %+v", b.Glow)
	}
	st := State{Tabs: []*Tab{{Buf: b}}}
	st.MoveOn("a", at.Add(time.Minute))
	st.MoveOn("a", at.Add(2*time.Minute)) // fading already: it keeps the time it began
	if g := st.Tabs[0].Buf.Glow; !g[0].Off.Equal(at.Add(time.Minute)) || !g[1].Off.IsZero() {
		t.Errorf("after a moved on: %+v", g)
	}
	b = st.Tabs[0].Buf
	b.Light("b", 1, 1, at.Add(time.Minute+glowLife))
	if len(b.Glow) != 1 || b.Glow[0].Agent != "b" {
		t.Errorf("a faded glow stayed: %+v", b.Glow)
	}
}
