package ideview

import (
	"image"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/ide"
)

// The right column splits into the workplaces of the agents at work: at most maxColumns to a row,
// none narrower than minColumn cells; past that they go under one another, none lower than minRow.
// A workplace stays columnLinger after its agent last did something on screen.
const (
	maxColumns   = 3
	minColumn    = 32
	minRow       = 10
	columnLinger = 8 * time.Second
)

// column is one workplace: an agent's file, its terminal or its browser.
type column struct {
	agent *ide.Agent // whose view it shows: its caret leads the scroll, its name heads it
	pane  ide.Pane   // Editor, Terminal or Browser
}

// workPane is the workplace a's focus puts on screen: the explorer goes with the editor.
func workPane(a *ide.Agent) ide.Pane {
	if a.Focus == ide.Explorer {
		return ide.Editor
	}
	return a.Focus
}

// columns picks what the right column shows, rw×h cells: the workplaces of the agents at work or
// busy a moment ago, the camera's always among them, the most recently busy first up to the count
// that fits, in the order of the agents. A file several agents work in shows once, as the latest
// of them sees it. With nothing else at work, or the camera pinned, the camera's agent alone.
func columns(st *ide.State, cam *ide.Agent, o Options, rw, h int) []column {
	if st.Viewed != "" { // nobody's view: no caret of its own, every agent's labelled
		return []column{{agent: &ide.Agent{File: st.Viewed}, pane: ide.Editor}}
	}
	if st.Pinned {
		return alone(st, cam, rw)
	}
	type cand struct {
		col   column
		key   string
		at    time.Time
		order int
	}
	var cands []cand
	for i, a := range st.Agents {
		recent := !a.Active.IsZero() && o.Now.Sub(a.Active) < columnLinger
		if a.Kind == ide.Workflow || a != cam && a.Activity == "" && !recent {
			continue
		}
		c := cand{col: column{agent: a, pane: workPane(a)}, key: a.File, at: a.Active, order: i}
		switch {
		case c.col.pane != ide.Editor:
			c.key = "\x00" + a.Key // a terminal or browser of its own
		case a.File == "":
			continue
		}
		if a == cam {
			c.at = o.Now.Add(time.Hour) // first to stay
		}
		if j := slices.IndexFunc(cands, func(d cand) bool { return d.key == c.key }); j >= 0 {
			if c.at.After(cands[j].at) {
				cands[j].col, cands[j].at = c.col, c.at
			}
			continue
		}
		cands = append(cands, c)
	}
	if len(cands) <= 1 {
		return alone(st, cam, rw)
	}
	slices.SortStableFunc(cands, func(a, b cand) int { return b.at.Compare(a.at) })
	cands = cands[:min(len(cands), perRow(rw)*max(1, h/minRow))]
	slices.SortFunc(cands, func(a, b cand) int { return a.order - b.order })
	cols := make([]column, len(cands))
	for i, c := range cands {
		cols[i] = c.col
	}
	return cols
}

// alone is the workplace of agent a at work by itself: its file and, beside it, its terminal or,
// while it is on the web, its browser, once it has both and both fit. Else the one its focus is on.
func alone(st *ide.State, a *ide.Agent, rw int) []column {
	side := ide.Terminal
	if a.Focus == ide.Browser {
		side = ide.Browser
	}
	typed := len(a.Term.Lines) > 0 || a.Term.Input != "" || a.Focus == ide.Terminal
	if rw < 2*minColumn || st.FindTab(a.File) == nil || side == ide.Terminal && !typed {
		return []column{{agent: a, pane: workPane(a)}}
	}
	return []column{{agent: a, pane: ide.Editor}, {agent: a, pane: side}}
}

// perRow is how many workplaces a row of rw cells holds side by side.
func perRow(rw int) int { return min(maxColumns, max(1, rw/minColumn)) }

// tiles lays n workplaces out in rw×h cells, in reading order: in as few rows as hold them, as
// even as can be, the rows as high as one another and the panes of a row as wide; the last of
// each takes what is left over.
func tiles(n, rw, h int) []image.Rectangle {
	rows := (n + perRow(rw) - 1) / perRow(rw)
	per := (n + rows - 1) / rows
	var ts []image.Rectangle
	for r, y := 0, 0; r < rows; r++ {
		th := h / rows
		if r == rows-1 {
			th = h - y
		}
		k := min(per, n-r*per)
		for i, x := 0, 0; i < k; i++ {
			tw := rw / k
			if i == k-1 {
				tw = rw - x
			}
			ts = append(ts, image.Rect(x, y, x+tw, y+th))
			x += tw
		}
		y += th
	}
	return ts
}

// columnAt is the workplace at cell (x, y) of a view width×height cells, and where it is in the
// view; ok is false for none.
func columnAt(st *ide.State, o Options, width, height, x, y int) (c column, r image.Rectangle, ok bool) {
	ew := leftWidth(width)
	cols := columns(st, camera(st), o, width-ew, height)
	for i, r := range tiles(len(cols), width-ew, height) {
		if r = r.Add(image.Pt(ew, 0)); image.Pt(x, y).In(r) {
			return cols[i], r, true
		}
	}
	return column{}, image.Rectangle{}, false
}

// workplace draws column c, w×h cells.
func workplace(st *ide.State, c column, o Options, w, h int) []string {
	switch c.pane {
	case ide.Terminal:
		return terminalPane(st, c.agent, o, w, h)
	case ide.Browser:
		return browserPane(st, c.agent, o, w, h)
	}
	return editorPane(st, c.agent, o, w, h)
}

// title heads a pane of agent a: its name in its colour when several agents are about, then what
// the pane shows, cut to room cells.
func title(st *ide.State, a *ide.Agent, what func(room int) string, room int) string {
	if len(st.Agents) < 2 || a.Name == "" {
		return fgStyle.Render(what(room))
	}
	name := clip(a.Name, max(room/3, 1), "…")
	room -= ansi.StringWidth(name) + 3
	return lipgloss.NewStyle().Foreground(agentColor(a)).Render(name) + dimStyle.Render(" · ") + fgStyle.Render(what(max(room, 1)))
}

// shortPath is path relative to root (as it is outside root), at most w cells: folders from the
// middle give way to "…", the first folder and the file name stay; a file name too long keeps its
// end.
func shortPath(path, root string, w int) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		rel = path
	}
	rel = clip(rel, len(rel), "") // foreign text: one plain line
	if ansi.StringWidth(rel) <= w {
		return rel
	}
	parts := strings.Split(rel, "/")
	name := parts[len(parts)-1]
	join := func(tail []string) string { return parts[0] + "/…/" + strings.Join(tail, "/") }
	if len(parts) > 2 && ansi.StringWidth(join(parts[len(parts)-1:])) <= w {
		tail := parts[len(parts)-1:]
		for i := len(parts) - 2; i > 0 && ansi.StringWidth(join(parts[i:])) <= w; i-- {
			tail = parts[i:]
		}
		return join(tail)
	}
	if s := "…/" + name; ansi.StringWidth(s) <= w {
		return s
	}
	return "…" + tail(name, max(w-1, 0))
}
