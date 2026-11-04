package ideview

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/ide"
)

func BenchmarkTerminalOutput(b *testing.B) {
	for _, tc := range []struct {
		name         string
		lines, width int
	}{{"1000x80", 1000, 80}, {"1000x1024", 1000, 1024}, {"10000x80", 10000, 80}, {"10000x1024", 10000, 1024}, {"1x1MiB", 1, 1 << 20}} {
		b.Run(tc.name, func(b *testing.B) {
			a := &ide.Agent{Key: "main", Focus: ide.Terminal}
			for range tc.lines {
				a.Term.Print(ide.Output, strings.Repeat("x", tc.width))
			}
			st := &ide.State{Agents: []*ide.Agent{a}, Camera: "main"}
			o := Options{Terms: TermCache{}}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				RenderWithScrollbars(st, o, 120, 30)
			}
		})
	}
}

func TestWrapHugeLineAllocations(t *testing.T) {
	line := strings.Repeat("x", 1<<20)
	result := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			rows := wrap(line, 40, 40)
			if len(rows) != 4 || rows[3] != strings.Repeat("x", 39)+"…" {
				b.Fatal("the visible part of the long line changed")
			}
		}
	})
	if n := result.AllocedBytesPerOp(); n > 64<<10 {
		t.Fatalf("wrapping a visible fragment allocated %d bytes", n)
	}
}

// Row counting must agree with the rendered wraps, including narrow panes and Unicode.
func TestWrappedRows(t *testing.T) {
	for _, s := range []string{"", "abcdefghijk", "a\tb\tc", "界界界界界界", "a\x1b[31mred\x1b[0m", "1️⃣ step 👩‍💻", strings.Repeat("x", 1000)} {
		for _, w := range []int{0, 1, 2, 5, 40} {
			for _, first := range []int{0, 1, w} {
				if got, want := wrappedRows(s, first, w), len(wrap(s, first, w)); got != want {
					t.Errorf("%q first=%d width=%d: rows=%d want=%d", s, first, w, got, want)
				}
			}
		}
	}
}

func TestTerminalWrappedScrollWindow(t *testing.T) {
	a := &ide.Agent{Key: "main", Focus: ide.Terminal}
	for i := range 1000 {
		var line strings.Builder
		for _, row := range "ABCD" {
			fmt.Fprintf(&line, "%04d-%c%s", i, row, strings.Repeat("x", 86))
		}
		a.Term.Print(ide.Output, line.String())
	}
	st := &ide.State{Agents: []*ide.Agent{a}, Camera: "main"}
	for _, tc := range []struct {
		back        int
		top, bottom string
	}{{0, "0993-B", ""}, {1, "0993-A", "0999-D"}, {2000, "0493-B", "0500-A"}, {10000, "0000-A", "0006-D"}} {
		rows := strings.Split(ansi.Strip(Render(st, Options{Scroll: Scroll{termKey("main"): tc.back}}, 120, 30)), "\n")
		if !strings.Contains(rows[1], tc.top) || tc.bottom != "" && !strings.Contains(rows[28], tc.bottom) {
			t.Errorf("back=%d top=%q bottom=%q", tc.back, rows[1], rows[28])
		}
	}
}

func BenchmarkTerminalScrolled(b *testing.B) {
	a := &ide.Agent{Key: "main", Focus: ide.Terminal}
	for range 1000 {
		a.Term.Print(ide.Output, strings.Repeat("строка ", 100))
	}
	st := &ide.State{Agents: []*ide.Agent{a}, Camera: "main"}
	o := Options{Scroll: Scroll{termKey("main"): 2000}, Terms: TermCache{}}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		RenderWithScrollbars(st, o, 120, 30)
	}
}

func TestTerminalCacheInvalidation(t *testing.T) {
	var term ide.Term
	term.Print(ide.Output, "abcdefghij")
	o := Options{Terms: TermCache{}}
	if got := o.terminalLayout(&term, 6).total; got != 3 {
		t.Fatalf("initial rows=%d, want 3", got)
	}
	term.Print(ide.Output, "abcdefg")
	if got := o.terminalLayout(&term, 6).total; got != 5 {
		t.Fatalf("after append rows=%d, want 5", got)
	}
	term.Lines[0].Text = "x"
	if got := o.terminalLayout(&term, 6).total; got != 4 {
		t.Fatalf("after replacement rows=%d, want 4", got)
	}
	if got := o.terminalLayout(&term, 3).total; got != 5 {
		t.Fatalf("after resize rows=%d, want 5", got)
	}
	term.Lines = nil
	if got := o.terminalLayout(&term, 3).total; got != 1 {
		t.Fatalf("after clear rows=%d, want 1", got)
	}
}

func TestTerminalCachePrunesHiddenTerminals(t *testing.T) {
	st := sampleState()
	st.Agents[0].Focus = ide.Terminal
	o := Options{Terms: TermCache{}}
	Render(st, o, 100, 20)
	if len(o.Terms) != 1 {
		t.Fatal("the visible terminal has no cached layout")
	}
	st.Agents[0].Focus = ide.Browser
	Prune(st, o, 100, 20)
	if len(o.Terms) != 0 {
		t.Fatal("the cache retains a hidden terminal")
	}
}
