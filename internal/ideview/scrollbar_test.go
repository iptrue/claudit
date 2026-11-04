package ideview

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/ide"
	"github.com/pixelbender/claudit/internal/theme"
)

func TestScrollbarFrames(t *testing.T) {
	st := sampleState()
	st.Tree = nil
	for i := range 60 {
		st.Tree = append(st.Tree, ide.TreeRow{Name: fmt.Sprintf("f%02d", i)})
		st.Agents[0].Term.Print(ide.Output, fmt.Sprintf("out%02d", i))
	}
	mainBuf(st).Lines = strings.Split(strings.Repeat("code\n", 99)+"end", "\n")
	out := Render(st, Options{}, 100, 20)
	checkSize(t, "scrollbars", out, 100, 20)
	rows := strings.Split(out, "\n")
	for _, x := range []int{19, 59, 99} {
		if x != 99 && !styled(ansi.Cut(rows[1], x, x+1), "│", sgr("38", theme.Fg)) {
			t.Errorf("no thumb at the top of column %d: %s", x, rows[1])
		}
		if x == 99 && !styled(ansi.Cut(rows[18], x, x+1), "│", sgr("38", theme.Fg)) {
			t.Errorf("no terminal thumb at its tail: %s", rows[18])
		}
	}
	if !styled(out, "│", sgr("38", theme.Fg)) {
		t.Error("the thumb is not brighter than the border")
	}
	if !styled(ansi.Cut(rows[18], 19, 20), "│", sgr("38", theme.Border)) {
		t.Error("the scrollbar track is brighter than the border")
	}
	if styled(Render(sampleState(), Options{}, 100, 20), "│", sgr("38", theme.Fg)) {
		t.Error("a pane without overflow has a scrollbar")
	}
}

func TestScrollbarPanes(t *testing.T) {
	for _, pane := range []string{"editor", "terminal", "web", "agents"} {
		t.Run(pane, func(t *testing.T) {
			st := sampleState()
			o := Options{Scroll: Scroll{}}
			key := fileKey(root + "/src/main.go")
			switch pane {
			case "editor":
				mainBuf(st).Lines = strings.Split(strings.Repeat("code\n", 99)+"last line", "\n")
			case "terminal":
				key = termKey("main")
				for i := range 60 {
					st.Agents[0].Term.Print(ide.Output, fmt.Sprintf("out%02d", i))
				}
			case "web":
				key = webKey("main")
				st.Agents[0].Focus = ide.Browser
				for i := range 60 {
					st.Agents[0].Page.Lines = append(st.Agents[0].Page.Lines, ide.PageLine{Text: fmt.Sprintf("page%02d", i)})
				}
			case "agents":
				key, o.Panel = agentsPaneKey, Agents
				for i := range 60 {
					st.Agents = append(st.Agents, &ide.Agent{Key: fmt.Sprintf("a%02d", i), Name: fmt.Sprintf("agent%02d", i)})
				}
			}
			var bar *Scrollbar
			for _, b := range Scrollbars(st, o, 100, 20) {
				if b.Key == key {
					bar = &b
				}
			}
			if bar == nil {
				t.Fatal("overflowing pane has no scrollbar")
			}
			for _, edge := range []struct {
				y    int
				text string
			}{
				{-100, map[string]string{"editor": " 1 code", "terminal": "$ go build", "web": "page00", "agents": "main"}[pane]},
				{100, map[string]string{"editor": "last line", "terminal": "out59", "web": "page59", "agents": "agent59"}[pane]},
			} {
				o.Scroll[key] = bar.Scroll(edge.y, 0)
				out := Render(st, o, 100, 20)
				checkSize(t, pane, out, 100, 20)
				if !strings.Contains(ansi.Strip(out), edge.text) {
					t.Fatalf("edge %d lacks %q:\n%s", edge.y, edge.text, ansi.Strip(out))
				}
			}
		})
	}
}

// A single source line can overflow an entire wrapped editor.
func TestScrollbarWrappedLine(t *testing.T) {
	st := sampleState()
	mainBuf(st).Lines = []string{strings.Repeat("words ", 300) + "THE_END"}
	o := Options{Wrap: true, Scroll: Scroll{}}
	key := fileKey(root + "/src/main.go")
	for _, bar := range Scrollbars(st, o, 100, 20) {
		if bar.Key != key {
			continue
		}
		o.Scroll[key] = bar.Scroll(100, 0)
		if out := ansi.Strip(Render(st, o, 100, 20)); !strings.Contains(out, "THE_END") {
			t.Fatalf("wrapped scrollbar did not reach the end:\n%s", out)
		}
		return
	}
	t.Fatal("the wrapped line overflows but has no scrollbar")
}
