package sim

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/pixelbender/claudit/internal/ide"
)

// Pace of the simulated developer, before catch-up.
const (
	termLo, termHi = 15 * time.Millisecond, 25 * time.Millisecond // typing a command, per rune
	codeLo, codeHi = 5 * time.Millisecond, 8 * time.Millisecond   // typing code, per rune
	selLo, selHi   = 12 * time.Millisecond, 18 * time.Millisecond // selecting code to delete, per rune
	outLo, outHi   = 10 * time.Millisecond, 20 * time.Millisecond // printing output, per step
	rowLo, rowHi   = 20 * time.Millisecond, 30 * time.Millisecond // explorer selection, per row
	lineLo, lineHi = 12 * time.Millisecond, 18 * time.Millisecond // editor caret, per line

	cmdCap  = 1200 * time.Millisecond
	codeCap = 2500 * time.Millisecond
	outCap  = 800 * time.Millisecond
	rowCap  = 400 * time.Millisecond
	lineCap = 400 * time.Millisecond
	selCap  = 800 * time.Millisecond

	scanStep   = 12 * time.Millisecond // reading a file, per line
	readCap    = 1500 * time.Millisecond
	colStep    = 40 * time.Millisecond  // caret jump along the line
	click      = 80 * time.Millisecond  // open the file picked in the explorer
	pause      = 350 * time.Millisecond // look at the selection before deleting it
	enterPause = 150 * time.Millisecond // look at the command before pressing enter
	pagePause  = time.Second            // look at the page that came before moving on
	spinFrame  = 80 * time.Millisecond

	maxOutput = 40   // lines printed for search tools
	maxPage   = 200  // lines of a web page
	maxLines  = 5000 // lines kept from a file
	maxBytes  = 1 << 20
)

// How interesting a step is to watch, for the camera.
const (
	scoreLook  = 1 // walking the explorer, scanning a file, waiting for a command
	scoreShell = 2 // typing a command, printing its output
	scoreCode  = 3 // selecting or typing code
)

// file is the file a tool works on, as it was on disk when the call was announced.
type file struct {
	path string
	text string // content as the editor shows it
	ok   bool   // the file could be read
}

func (s *Sim) file(input map[string]any) file {
	f := file{path: s.abs(str(input, "file_path"))}
	f.text, f.ok = snapshot(f.path)
	return f
}

// noOutput is what claude says in place of a tool's empty result: the terminal shows nothing.
var noOutput = regexp.MustCompile(`^\(\w+ completed with no output\)$`)

// command types cmd into the agent's terminal under note, what it does as a "# " line when there
// is one, runs it and prints the tool output, then a blank line before the next prompt; limit > 0
// caps the printed lines.
func (s *Sim) command(sc *scene, cmd, note string, limit int) {
	l := sc.l
	t := &l.a.Term
	keys := []rune(cmd)
	sc.score = scoreShell
	sc.add(0, func() {
		l.look(ide.Terminal)
		if note = oneLine(note); note != "" {
			t.PrintLine(ide.TermLine{Kind: ide.Comment, Text: "# " + note, Call: sc.id})
		}
	})
	part := ide.Command // the command's first line has the prompt, the lines after it none
	langs, n := lineLangs(cmd), 0
	sc.paced(len(keys), termLo, termHi, cmdCap, func(i, j int) {
		sc.typed()
		for _, r := range keys[i:j] {
			if r == '\n' {
				t.PrintLine(ide.TermLine{Kind: part, Text: t.Input, Lang: langs[n], Call: sc.id})
				n++
				t.Input, t.InputLang, part = "", langs[n], ide.More
			} else {
				t.Input += printable(string(r))
			}
		}
	})
	sc.add(enterPause, func() {
		t.PrintLine(ide.TermLine{Kind: part, Text: t.Input, Lang: langs[n], Call: sc.id})
		t.Input, t.InputLang, t.Running, t.Spinner, t.RunAt = "", "", true, 0, l.clock
	})
	sc.score = scoreLook
	sc.await(func(r result) {
		out := r.out
		if r.isErr {
			out = strings.NewReplacer("<tool_use_error>", "", "</tool_use_error>", "").Replace(out)
		}
		if noOutput.MatchString(strings.TrimSpace(out)) {
			out = ""
		}
		lines := classify(cmd, termLines(out), r.isErr)
		if limit > 0 && len(lines) > limit {
			lines = append(lines[:limit:limit], ide.TermLine{Kind: ide.Output, Text: fmt.Sprintf("… %d more lines", len(lines)-limit)})
		}
		sc.score = scoreShell
		sc.paced(len(lines), outLo, outHi, outCap, func(i, j int) {
			for _, l := range lines[i:j] {
				t.PrintLine(l)
			}
		})
		sc.add(0, func() {
			t.Running = false
			t.Print(ide.Gap, "")
		})
	})
}

// browse types address (a query when search) into the agent's browser and loads the page the
// tool brings once it is in.
func (s *Sim) browse(sc *scene, address string, search bool) {
	l := sc.l
	p := &l.a.Page
	keys := []rune(address)
	sc.score = scoreShell
	sc.add(0, func() {
		l.look(ide.Browser)
		*p = ide.Page{Search: search}
	})
	sc.paced(len(keys), termLo, termHi, cmdCap, func(i, j int) { sc.typed(); p.URL += printable(string(keys[i:j])) })
	sc.add(enterPause, func() { p.LoadAt = l.clock })
	sc.score = scoreLook
	sc.await(func(r result) {
		lines := pageLines(r.out)
		sc.score = scoreShell
		sc.add(0, func() { p.LoadAt, p.Failed = time.Time{}, r.isErr })
		sc.paced(len(lines), outLo, outHi, outCap, func(i, j int) { p.Lines = append(p.Lines, lines[i:j]...) })
		sc.hold(pagePause)
	})
}

// pageLines is the page a web tool's output makes, at most maxPage lines: the links a search
// found ("Links: [{title, url}, …]"), and the text.
func pageLines(out string) []ide.PageLine {
	out = strings.NewReplacer("<tool_use_error>", "", "</tool_use_error>", "").Replace(out)
	var page []ide.PageLine
	for _, l := range termLines(out) {
		// The address bar says the query; the page starts with what it has to say.
		if strings.HasPrefix(l, "Web search results for query:") || len(page) == 0 && strings.TrimSpace(l) == "" {
			continue
		}
		var links []struct {
			Title string `json:"title"`
			URL   string `json:"url"`
		}
		if rest, ok := strings.CutPrefix(l, "Links: "); ok && json.Unmarshal([]byte(rest), &links) == nil {
			for _, k := range links {
				page = append(page, ide.PageLine{Text: printable(k.Title), URL: printable(k.URL)})
			}
			continue
		}
		page = append(page, ide.PageLine{Text: l})
	}
	return page[:min(len(page), maxPage)]
}

// read opens the file and scans it from line start; an unreadable file waits for the tool output.
func (s *Sim) read(sc *scene, f file, start, limit int) {
	if !f.ok {
		sc.score = scoreLook
		sc.await(func(r result) {
			// A picture, a PDF or a notebook comes back without numbered lines: nothing to show.
			if text := numbered(r.out); !r.isErr && text != "" {
				s.show(sc, f.path, text, start, limit)
			}
		})
		return
	}
	s.walk(sc, f.path)
	s.show(sc, f.path, f.text, start, limit)
}

// show opens text in the editor and reads the lines the tool reads, from line start, limit of them
// (0: to the end): the caret sweeps down them at the end of each line, selecting nothing.
func (s *Sim) show(sc *scene, path, text string, start, limit int) {
	if t := s.st.FindTab(path); t != nil && t.Buf.Modified {
		text = t.Buf.Text() // an edit is waiting for its result: keep it
	}
	sc.score = scoreLook
	sc.add(click, func() {
		if t := s.enter(sc, path, text); !t.Buf.Modified {
			patch(sc.buf, text)
		}
	})
	sc.add(0, func() {
		c := sc.caret()
		c.Pos, c.Sel = sc.buf.Clamp(ide.Pos{Line: start, Col: math.MaxInt}), nil
	})
	n := len(strings.Split(strings.TrimSuffix(text, "\n"), "\n")) - start
	if limit > 0 {
		n = min(n, limit)
	}
	sc.paced(n, scanStep, scanStep, readCap, func(_, j int) {
		sc.caret().Pos = sc.buf.Clamp(ide.Pos{Line: start + j - 1, Col: math.MaxInt})
	})
}

// edit replaces old with repl: travel to it, select it, delete it, type the new text.
func (s *Sim) edit(sc *scene, f file, old, repl string, all bool) {
	if f.ok {
		s.walk(sc, f.path)
	}
	base, ok := f.text, f.ok
	if t := s.st.FindTab(f.path); t != nil {
		base, ok = t.Buf.Text(), true
	}
	sc.score = scoreLook
	switch {
	case !ok:
		sc.await(func(result) { s.reload(sc, f.path) })
	case repl != "" && strings.Contains(repl, old) && strings.Contains(base, old) && strings.Contains(base, repl):
		// old sits inside new and both are in the text: the edit may be on disk already (a replay)
		// or not (a live session). The file after the tool tells: unchanged means it was.
		sc.await(func(r result) {
			if r.isErr {
				s.reload(sc, f.path)
				return
			}
			disk, _ := snapshot(f.path)
			s.replace(sc, f.path, base, old, repl, all, disk == base)
		})
	default:
		s.replace(sc, f.path, base, old, repl, all, false)
	}
}

// replace animates the edit of base (which already holds it when applied). The places are found
// when the steps run: other agents may be typing in the same file meanwhile.
func (s *Sim) replace(sc *scene, path, base, old, repl string, all, applied bool) {
	sc.score = scoreCode
	sc.add(click, func() {
		text := base
		if t := s.st.FindTab(path); t != nil {
			text = t.Buf.Text()
		}
		pre, at, found := locate(text, old, repl, all, applied)
		if !found {
			sc.score = scoreLook
			sc.await(func(result) { s.reload(sc, path) })
			return
		}
		s.enter(sc, path, pre)
		patch(sc.buf, pre)
		sc.score = scoreCode
		s.retype(sc, path, old, repl, at, all)
	})
}

// retype adds the steps of an edit whose old text starts at byte offset at of the buffer.
func (s *Sim) retype(sc *scene, path, old, repl string, at int, all bool) {
	from := advance(ide.Pos{}, sc.buf.Text()[:at])
	sc.travel(sc.caret().Pos, from)
	sc.add(colStep, func() {
		if old != "" { // other agents may have typed above it meanwhile
			text := sc.buf.Text()
			if i, ok := seek(text, old, at); ok {
				from = advance(ide.Pos{}, text[:i])
			}
		}
		c := sc.caret()
		c.Pos, c.Sel = sc.buf.Clamp(from), nil
	})
	sel := []rune(old)
	sc.paced(len(sel), selLo, selHi, selCap, func(_, j int) {
		sc.typed()
		c := sc.caret()
		start := c.Pos
		if c.Sel != nil {
			start = c.Sel.From // kept in place while other agents type
		}
		start = sc.buf.Clamp(start)
		if !sc.holds(start, string(sel[:j])) { // another agent changed the text: select none of it
			c.Pos, c.Sel = start, nil
			return
		}
		c.Pos = advance(start, string(sel[:j]))
		c.Sel = &ide.Selection{From: start, To: c.Pos}
	})
	sc.add(pause, func() {
		sc.typed()
		c := sc.caret()
		if c.Sel != nil && sc.holds(c.Sel.From, old) {
			sc.buf.Delete(sc.key(), c.Sel.From, advance(c.Sel.From, old))
		}
		c.Sel = nil
		sc.buf.Modified = true
	})
	sc.typeText(repl)
	sc.add(0, func() {
		if all && old != "" { // the remaining occurrences, at once
			text := sc.buf.Text()
			i := offset(sc.buf, sc.caret().Pos)
			patch(sc.buf, text[:i]+strings.ReplaceAll(text[i:], old, repl))
		}
		s.settle(sc, path)
	})
}

// write replaces the whole file: select all, delete, type the content.
func (s *Sim) write(sc *scene, f file, content string) {
	s.walk(sc, f.path)
	old := f.text
	if t := s.st.FindTab(f.path); t != nil {
		old = t.Buf.Text()
	}
	if old == content {
		old = "" // the file already holds it (a replay): type it from scratch
	}
	sc.score = scoreCode
	s.open(sc, f.path, old)
	if old != "" { // the old text goes: select it line by line from the top, then delete it
		n := len(strings.Split(strings.TrimSuffix(old, "\n"), "\n"))
		sc.add(click, func() { sc.caret().Pos, sc.caret().Sel = ide.Pos{}, nil })
		sc.paced(n, lineLo, lineHi, selCap, func(_, j int) {
			sc.typed()
			c := sc.caret()
			c.Pos = sc.buf.Clamp(ide.Pos{Line: j})
			if j == n {
				c.Pos = sc.buf.End()
			}
			c.Sel = &ide.Selection{To: c.Pos}
		})
		sc.add(pause, func() { sc.typed(); sc.buf.Delete(sc.key(), ide.Pos{}, sc.buf.End()) })
	}
	sc.add(0, func() {
		sc.caret().Pos = ide.Pos{}
		sc.buf.Modified = true
	})
	sc.typeText(content)
	sc.add(0, func() {
		patch(sc.buf, content)
		s.settle(sc, f.path)
	})
}

// walk moves the agent's explorer selection to path row by row; folders on the way open at once.
func (s *Sim) walk(sc *scene, path string) {
	to := s.st.Reveal(path)
	if to < 0 {
		return
	}
	a := sc.l.a
	from := max(0, slices.IndexFunc(s.st.Tree, func(r ide.TreeRow) bool { return r.Path == a.TreeSel }))
	row := func(i int) {
		if n := len(s.st.Tree); n > 0 { // other agents may have changed the tree meanwhile
			a.TreeSel = s.st.Tree[min(i, n-1)].Path
		}
	}
	sc.score = scoreLook
	sc.add(0, func() {
		sc.l.look(ide.Explorer)
		row(from)
	})
	dir := 1
	if to < from {
		dir = -1
	}
	sc.paced((to-from)*dir, rowLo, rowHi, rowCap, func(_, j int) { row(from + dir*j) })
	sc.add(0, func() { a.TreeSel = path })
}

// open adds the step that brings path up in the agent's editor showing text.
func (s *Sim) open(sc *scene, path, text string) {
	sc.add(click, func() {
		s.enter(sc, path, text)
		patch(sc.buf, text)
	})
}

// enter makes path the agent's file: its tab opens (with text when it is new), the agent's caret
// moves into it from the previous file, and the agent looks at the editor.
func (s *Sim) enter(sc *scene, path, text string) *ide.Tab {
	a := sc.l.a
	t := s.st.Open(path, text)
	t.Hex = false // an agent works on text
	if a.File != path {
		if o := s.st.FindTab(a.File); o != nil {
			delete(o.Buf.Carets, a.Key)
		}
		a.File = path
	}
	sc.buf = &t.Buf
	t.Buf.Caret(a.Key)
	sc.l.look(ide.Editor)
	s.st.Touched[path] = a.Key
	return t
}

// reload shows the file as it is on disk now.
func (s *Sim) reload(sc *scene, path string) {
	text, ok := snapshot(path)
	if !ok && s.st.FindTab(path) == nil {
		return
	}
	sc.score = scoreLook
	s.open(sc, path, text)
	sc.add(0, func() { sc.buf.Modified = false })
}

// settle ends an edit once the tool result is in: success saves the buffer, an error puts the
// file back as it is on disk.
func (s *Sim) settle(sc *scene, path string) {
	done := func(r result) {
		if r.isErr {
			s.st.Forget(path) // a new file that never made it to disk
		} else {
			s.st.Edited[path] = true
		}
		t := s.st.FindTab(path)
		if t == nil {
			return
		}
		if r.isErr {
			text, _ := snapshot(path)
			t.Buf.SetText(text)
		}
		t.Buf.Modified = false
	}
	if sc.res != nil {
		done(*sc.res)
		return
	}
	sc.after = done
}

// travel moves the caret from a toward b line by line.
func (sc *scene) travel(a, b ide.Pos) {
	dir := 1
	if b.Line < a.Line {
		dir = -1
	}
	sc.paced((b.Line-a.Line)*dir, lineLo, lineHi, lineCap, func(_, j int) {
		sc.caret().Pos = sc.buf.Clamp(ide.Pos{Line: a.Line + dir*j, Col: a.Col})
	})
}

// typeText types text at the agent's caret at code pace.
func (sc *scene) typeText(text string) {
	keys := []rune(text)
	sc.paced(len(keys), codeLo, codeHi, codeCap, func(i, j int) {
		sc.typed()
		from := sc.buf.Caret(sc.key()).Pos.Line
		sc.buf.Insert(sc.key(), string(keys[i:j]))
		sc.buf.Light(sc.key(), from, sc.buf.Carets[sc.key()].Pos.Line, sc.l.clock)
	})
}

// typed notes that the scene's agent types, selects or deletes text now.
func (sc *scene) typed() { sc.l.a.TypedAt = sc.l.clock }

// look turns the agent to pane.
func (l *lane) look(p ide.Pane) {
	l.a.Focus = p
}

// holds reports whether the scene's buffer has s at p.
func (sc *scene) holds(p ide.Pos, s string) bool {
	return strings.HasPrefix(sc.buf.Text()[offset(sc.buf, sc.buf.Clamp(p)):], s)
}

// key is the key of the scene's agent, read when a step runs: the agent may have been rekeyed.
func (sc *scene) key() string { return sc.l.a.Key }

// caret is the agent's caret in the scene's buffer.
func (sc *scene) caret() *ide.Caret { return sc.buf.Caret(sc.key()) }
