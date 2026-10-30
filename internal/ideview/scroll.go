package ideview

import (
	"maps"

	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/ide"
)

// Scroll is how far the user scrolled each pane back (up) from where it follows its agent: in rows,
// in lines for a file. It is keyed by pane: the explorer, a file in the editor, an agent's terminal.
type Scroll map[string]int

const filesPane = "files"

func fileKey(path string) string  { return "file:" + path }
func termKey(agent string) string { return "term:" + agent }
func webKey(agent string) string  { return "web:" + agent }

// paneKey is the key of column c's pane; "" for an editor without a file.
func paneKey(st *ide.State, c column) string {
	switch c.pane {
	case ide.Terminal:
		return termKey(c.agent.Key)
	case ide.Browser:
		return webKey(c.agent.Key)
	}
	if tab := shownTab(st, c.agent); tab != nil {
		return fileKey(tab.Path)
	}
	return ""
}

// scrolled is the first row of a window that follows at top, scrolled back d rows, within [0, last].
func scrolled(top, d, last int) int { return min(max(top-d, 0), last) }

// Wheel scrolls the pane at cell (x, y) of Render(st, o, width, height) n rows back (forward when
// n < 0), as far as there is to see. It returns the pane's key, "" for none, and its new scroll.
// Scrolled back to where it follows its agent, a pane stops there: 0, it follows again.
func Wheel(st *ide.State, o Options, width, height, x, y, n int) (string, int) {
	if y < 0 || y >= height || x < 0 || x >= width {
		return "", 0
	}
	_, ih := inner(0, height)
	c, _, cw, ok := columnAt(st, o, width, x)
	iw, _ := inner(cw, height)
	switch {
	case !ok && o.Panel == Files:
		top := scrollTop(treeSel(st, camera(st)), len(st.Tree), ih)
		return filesPane, step(o.Scroll[filesPane], n, top-max(0, len(st.Tree)-ih), top)
	case !ok:
		return "", 0
	case c.pane == ide.Terminal:
		key := termKey(c.agent.Key)
		return key, step(o.Scroll[key], n, 0, max(0, termRows(&c.agent.Term, textWidth(iw))-ih))
	case c.pane == ide.Browser: // it follows the top of the page
		key := webKey(c.agent.Key)
		return key, step(o.Scroll[key], n, -max(0, len(pageRows(&c.agent.Page, textWidth(iw), -1))-pageHeight(ih)), 0)
	}
	tab := shownTab(st, c.agent)
	if tab == nil {
		return "", 0
	}
	key := fileKey(tab.Path)
	lines, cur, tw := editorText(tab, c.agent, iw)
	top, _ := editorTop(lines, cur, tw, ih, o.Wrap)
	return key, step(o.Scroll[key], n, min(0, top-editorLast(lines, tw, ih, o.Wrap)), max(0, top))
}

// step is scroll d moved by n within [lo, hi], the scrolls that show the end and the start. Moving
// across 0, where the pane follows its agent, it stops there.
func step(d, n, lo, hi int) int {
	d = min(max(d, lo), hi)
	next := min(max(d+n, lo), hi)
	if d > 0 && next < 0 || d < 0 && next > 0 {
		return 0
	}
	return next
}

// Prune forgets the scroll of the panes Render(st, o, width, …) does not draw, so that one coming
// back follows its agent.
func Prune(st *ide.State, o Options, width int) {
	shown := map[string]bool{filesPane: o.Panel == Files}
	for _, c := range columns(st, camera(st), o, width-leftWidth(width)) {
		shown[paneKey(st, c)] = true
	}
	maps.DeleteFunc(o.Scroll, func(k string, _ int) bool { return !shown[k] })
}

// termRows is how many rows terminal t takes iw cells wide, its live line included.
func termRows(t *ide.Term, iw int) int {
	pw, n := ansi.StringWidth(clip(termPrompt, iw, "")), 1
	for _, l := range t.Lines {
		first := iw
		if l.Kind == ide.Command || l.Kind == ide.More {
			first -= pw
		}
		n += len(wrap(l.Text, first, iw))
	}
	return n
}
