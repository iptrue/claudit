package ide

import (
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const maxTabs = 8

// FindTab returns the open tab of path, or nil.
func (st *State) FindTab(path string) *Tab {
	for _, t := range st.Tabs {
		if t.Path == path {
			return t
		}
	}
	return nil
}

// Open returns the tab of path; a tab that is not open yet starts with text. Beyond 8 tabs the
// oldest one no agent works in, and the user does not hold, is closed.
func (st *State) Open(path, text string) *Tab {
	if t := st.FindTab(path); t != nil {
		return t
	}
	if len(st.Tabs) >= maxTabs {
		if i := slices.IndexFunc(st.Tabs, func(t *Tab) bool { return !st.inUse(t.Path) }); i >= 0 {
			st.Tabs = slices.Delete(st.Tabs, i, i+1)
		}
	}
	t := &Tab{Path: path}
	t.Buf.SetText(text)
	st.Tabs = append(st.Tabs, t)
	st.retitle()
	return t
}

// inUse reports whether an agent works in the file at path, or the user opened it and holds it.
func (st *State) inUse(path string) bool {
	return st.Viewed != "" && path == st.Viewed || slices.ContainsFunc(st.Agents, func(a *Agent) bool { return a.File == path })
}

// retitle names tabs by base name, adding the parent directory when two tabs share it.
func (st *State) retitle() {
	for _, t := range st.Tabs {
		base := filepath.Base(t.Path)
		t.Title = base
		for _, o := range st.Tabs {
			if o != t && filepath.Base(o.Path) == base {
				t.Title = filepath.Base(filepath.Dir(t.Path)) + "/" + base
				break
			}
		}
	}
}

// Text is the content of the buffer.
func (b *Buffer) Text() string { return strings.Join(b.Lines, "\n") }

// SetText replaces the content. Selections are dropped, carets stay inside the text, glows on
// lines that are gone go.
func (b *Buffer) SetText(s string) {
	b.Lines = strings.Split(s, "\n")
	for _, c := range b.Carets {
		c.Pos, c.Sel = b.Clamp(c.Pos), nil
	}
	b.Glow = slices.DeleteFunc(b.Glow, func(g Glow) bool { return g.Line >= len(b.Lines) })
}

// Caret returns the caret of the agent key, adding one at the top of the buffer.
func (b *Buffer) Caret(key string) *Caret {
	if b.Carets == nil {
		b.Carets = map[string]*Caret{}
	}
	c := b.Carets[key]
	if c == nil {
		c = &Caret{}
		b.Carets[key] = c
	}
	return c
}

// End is the position after the last rune.
func (b *Buffer) End() Pos {
	if len(b.Lines) == 0 {
		return Pos{}
	}
	n := len(b.Lines) - 1
	return Pos{Line: n, Col: utf8.RuneCountInString(b.Lines[n])}
}

// Clamp returns p moved inside the buffer.
func (b *Buffer) Clamp(p Pos) Pos {
	if len(b.Lines) == 0 {
		return Pos{}
	}
	p.Line = max(0, min(p.Line, len(b.Lines)-1))
	p.Col = max(0, min(p.Col, utf8.RuneCountInString(b.Lines[p.Line])))
	return p
}

// Insert types s at the caret of key; newlines split the line and the caret ends after s.
// The other carets keep their place in the text.
func (b *Buffer) Insert(key, s string) {
	if len(b.Lines) == 0 {
		b.Lines = []string{""}
	}
	c := b.Caret(key)
	at := b.Clamp(c.Pos)
	line := b.Lines[at.Line]
	i := byteIndex(line, at.Col)
	parts := strings.Split(line[:i]+s+line[i:], "\n")
	b.Lines = slices.Replace(b.Lines, at.Line, at.Line+1, parts...)
	end := Pos{Line: at.Line + len(parts) - 1, Col: utf8.RuneCountInString(parts[len(parts)-1]) - utf8.RuneCountInString(line[i:])}
	b.others(key, func(p Pos) Pos {
		switch {
		case before(p, at):
			return p
		case p.Line == at.Line:
			return Pos{Line: end.Line, Col: end.Col + p.Col - at.Col}
		}
		return Pos{Line: p.Line + end.Line - at.Line, Col: p.Col}
	})
	b.shiftGlow(glowAnchor(at), end.Line-at.Line)
	c.Pos = end
}

// Delete removes the text in [from, to) and puts the caret of key at from without a selection.
// The other carets keep their place in the text.
func (b *Buffer) Delete(key string, from, to Pos) {
	if len(b.Lines) == 0 {
		return
	}
	from, to = b.Clamp(from), b.Clamp(to)
	if before(to, from) {
		from, to = to, from
	}
	head := b.Lines[from.Line][:byteIndex(b.Lines[from.Line], from.Col)]
	tail := b.Lines[to.Line][byteIndex(b.Lines[to.Line], to.Col):]
	b.Lines = slices.Replace(b.Lines, from.Line, to.Line+1, head+tail)
	b.others(key, func(p Pos) Pos {
		switch {
		case !before(from, p):
			return p
		case before(p, to):
			return from
		case p.Line == to.Line:
			return Pos{Line: from.Line, Col: from.Col + p.Col - to.Col}
		}
		return Pos{Line: p.Line - (to.Line - from.Line), Col: p.Col}
	})
	b.shiftGlow(glowAnchor(from), -(to.Line - from.Line))
	c := b.Caret(key)
	c.Pos, c.Sel = from, nil
}

// glowLife is how long a glow takes to fade once its agent has moved on.
const glowLife = 2 * time.Second

// Light marks lines [from, to] as just typed by the agent key at time at. Glows that have faded
// go, so the list stays short.
func (b *Buffer) Light(key string, from, to int, at time.Time) {
	b.Glow = slices.DeleteFunc(b.Glow, func(g Glow) bool {
		return !g.Off.IsZero() && at.Sub(g.Off) >= glowLife || g.Line >= from && g.Line <= to
	})
	for l := from; l <= to; l++ {
		b.Glow = append(b.Glow, Glow{Line: l, Agent: key, At: at})
	}
}

// MoveOn notes that the agent key went on to its next tool call at time at: the lines it typed
// before stop holding their light and fade.
func (s *State) MoveOn(key string, at time.Time) {
	for _, t := range s.Tabs {
		for i := range t.Buf.Glow {
			if g := &t.Buf.Glow[i]; g.Agent == key && g.Off.IsZero() {
				g.Off = at
			}
		}
	}
}

// glowAnchor is the last line whose glow an edit at p leaves in place: p's line, unless p starts it
// (then that line's text moves with the edit).
func glowAnchor(p Pos) int {
	if p.Col == 0 {
		return p.Line - 1
	}
	return p.Line
}

// shiftGlow moves the glows below line at by n lines (n < 0: up, dropping those that fall
// inside the removed lines).
func (b *Buffer) shiftGlow(at, n int) {
	if n == 0 {
		return
	}
	b.Glow = slices.DeleteFunc(b.Glow, func(g Glow) bool { return n < 0 && g.Line > at && g.Line <= at-n })
	for i := range b.Glow {
		if b.Glow[i].Line > at {
			b.Glow[i].Line += n
		}
	}
}

// others moves the carets and selections of every agent but key through f.
func (b *Buffer) others(key string, f func(Pos) Pos) {
	for k, c := range b.Carets {
		if k == key {
			continue
		}
		c.Pos = f(c.Pos)
		if c.Sel != nil {
			c.Sel = &Selection{From: f(c.Sel.From), To: f(c.Sel.To)}
		}
	}
}

// before reports whether a comes before b.
func before(a, b Pos) bool { return a.Line < b.Line || a.Line == b.Line && a.Col < b.Col }

// byteIndex is the byte offset of rune col in s.
func byteIndex(s string, col int) int {
	for i := range s {
		if col == 0 {
			return i
		}
		col--
	}
	return len(s)
}
