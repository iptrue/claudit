// Package ideview draws the simulated IDE: the explorer or the agents tree on the left; on the
// right, at full height, the workplace of the camera's agent (its terminal while it runs commands,
// its browser while it searches or fetches the web, the editor otherwise), side by side with those
// of the other agents at work.
package ideview

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/ide"
	"github.com/pixelbender/claudit/internal/theme"
)

var (
	fgStyle   = lipgloss.NewStyle().Foreground(theme.Fg)
	dimStyle  = lipgloss.NewStyle().Foreground(theme.Dim)
	okStyle   = lipgloss.NewStyle().Foreground(theme.OK)
	limeStyle = lipgloss.NewStyle().Foreground(theme.Lime)
	cmdStyle  = lipgloss.NewStyle().Foreground(theme.Bright) // what is typed in a terminal
	errStyle  = lipgloss.NewStyle().Foreground(theme.Err)
	hunkStyle = lipgloss.NewStyle().Foreground(theme.Violet) // a diff's "@@ -1,2 +1,3 @@"

	spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
)

// Render draws st into exactly width×height cells (height lines, each width cells wide), following
// the camera's agent.
func Render(st *ide.State, o Options, width, height int) string {
	if height <= 0 {
		return ""
	}
	cam := camera(st)
	ew := leftWidth(width)
	rw := width - ew
	var left []string
	if o.Panel == Agents {
		left = agentsPane(st, cam, o, ew, height)
	} else {
		left = explorerPane(st, cam, o, ew, height)
	}
	lines := slices.Clone(left)
	cols := columns(st, cam, o, rw)
	solo := len(cols) > 1 && cols[0].agent == cols[1].agent // one agent's file beside its terminal
	for i, cw := range columnWidths(rw, len(cols)) {
		c := cols[i]
		pane := workplace(st, c, o, cw, height)
		switch {
		case len(cols) == 1:
			pane = wipe(pane, cutAt(st, cam), o.Now, agentColor(cam), cw)
		case solo: // both stay on screen as it turns between them: only a cut wipes them
			pane = wipe(pane, st.CameraAt, o.Now, agentColor(cam), cw)
		default:
			pane = wipe(pane, c.agent.FocusAt, o.Now, agentColor(c.agent), cw)
		}
		for j := range lines {
			lines[j] += pane[j]
		}
	}
	return strings.Join(lines, "\n")
}

// leftWidth is the width of the left column, the same for the files and the agents panels.
func leftWidth(width int) int {
	return min(max(width/5, 18), 32, width/2)
}

// AgentAt is the agent a click at cell (x, y) of Render(st, o, width, height) falls on: a row of
// the agents panel, or the workplace of an agent on the right; "" for none.
func AgentAt(st *ide.State, o Options, width, height, x, y int) string {
	ew := leftWidth(width)
	switch {
	case y < 0 || y >= height || x < 0 || x >= width:
		return ""
	case x >= ew:
		if c, _, _, ok := columnAt(st, o, width, x); ok {
			return c.agent.Key
		}
		return ""
	case o.Panel != Agents || y == 0 || y == height-1:
		return ""
	}
	_, ih := inner(0, height)
	tree, top := agentsView(st, camera(st), o, ih)
	if i := top + y - 1; i < len(tree) && tree[i].agent != nil {
		return tree[i].agent.Key
	}
	return ""
}

// TreeAt is the explorer row a click at cell (x, y) of Render(st, o, width, height) falls on: its
// path, "" for none, and whether it is a folder.
func TreeAt(st *ide.State, o Options, width, height, x, y int) (path string, dir bool) {
	if o.Panel != Files || x < 0 || x >= leftWidth(width) || y < 1 || y >= height-1 {
		return "", false
	}
	_, ih := inner(0, height)
	if r := explorerTop(st, o, ih) + y - 1; r < len(st.Tree) {
		return st.Tree[r].Path, st.Tree[r].Dir
	}
	return "", false
}

// TextAt is the text of the pane at cell (x, y) of Render(st, o, width, height): the cells inside
// its frame, less the editor's line numbers and the air of a terminal, a browser or the agents
// panel. It is empty where there is no text: on a frame, in the gutter, in an empty editor.
func TextAt(st *ide.State, o Options, width, height, x, y int) image.Rectangle {
	ew := leftWidth(width)
	x0, w := 1, ew-2 // the explorer
	if o.Panel == Agents {
		x0, w = 2, textWidth(ew-2)
	}
	if c, cx, cw, ok := columnAt(st, o, width, x); ok {
		iw, _ := inner(cw, height)
		x0, w = cx+2, textWidth(iw)
		if c.pane == ide.Editor {
			tab := shownTab(st, c.agent)
			if tab == nil {
				return image.Rectangle{}
			}
			_, _, tw := editorText(tab, c.agent, iw)
			x0, w = cx+1+iw-tw, tw
		}
	}
	r := image.Rect(x0, 1, x0+max(w, 0), max(height-1, 1))
	if !image.Pt(x, y).In(r) {
		return image.Rectangle{}
	}
	return r
}

// cutAt is when the right column last changed what it shows: the camera cut to another agent, or
// its agent turned between the editor and the terminal.
func cutAt(st *ide.State, cam *ide.Agent) time.Time {
	if cam.FocusAt.After(st.CameraAt) {
		return cam.FocusAt
	}
	return st.CameraAt
}

// camera is the agent the view follows: the one keyed st.Camera, else the first agent, else an
// idle stand-in so that a state without agents still draws.
func camera(st *ide.State) *ide.Agent {
	if a := findAgent(st, st.Camera); a != nil {
		return a
	}
	if len(st.Agents) > 0 {
		return st.Agents[0]
	}
	return &ide.Agent{}
}

func findAgent(st *ide.State, key string) *ide.Agent {
	for _, a := range st.Agents {
		if a.Key == key {
			return a
		}
	}
	return nil
}

// agentColor is the colour of a's palette slot; Dim for an unknown agent.
func agentColor(a *ide.Agent) color.Color {
	if a == nil {
		return theme.Dim
	}
	n := len(theme.Agents)
	return theme.Agents[(a.Color%n+n)%n]
}

// border is the frame colour of pane: BorderFocus where the agent works, Border elsewhere. Whose
// pane it is, its title says in the agent's colour.
func border(cam *ide.Agent, pane ide.Pane) color.Color {
	if cam.Focus == pane {
		return theme.BorderFocus
	}
	return theme.Border
}

func explorerPane(st *ide.State, cam *ide.Agent, o Options, w, h int) []string {
	iw, ih := inner(w, h)
	sel, lit := treeSel(st, cam), cam.Focus == ide.Explorer
	if st.Viewed != "" { // the user's pick while it fills the right column; the scroll stays put
		sel, lit = slices.IndexFunc(st.Tree, func(r ide.TreeRow) bool { return r.Path == st.Viewed }), true
	}
	rows := make([]string, ih)
	top := explorerTop(st, o, ih)
	for i := range rows {
		r := top + i
		if r >= len(st.Tree) {
			break
		}
		row := st.Tree[r]
		base := lipgloss.NewStyle()
		switch {
		case r == sel && lit:
			base = base.Background(theme.SelBg)
		case r == sel:
			base = base.Background(theme.SelBgDim)
		}
		indent := strings.Repeat(" ", 2*max(row.Depth, 0))
		// A directory's marker is its fold state; a file has none: a changed one, a yellow asterisk after it.
		marker, nc := "  ", fileTint(row.Path)
		if row.Dir {
			marker, nc = "▸ ", theme.Fg
			if row.Expanded {
				marker = "▾ "
			}
		}
		if strings.HasPrefix(row.Name, ".") {
			nc = theme.Dim // hidden: .git, .env and the like
		}
		room := iw - ansi.StringWidth(indent+marker)
		mark := ""
		if st.Edited[row.Path] {
			mark = " *" // an agent changed it
		}
		name := clip(row.Name, room-len(mark), "…") // the name gives way to the mark
		mark = clip(mark, room-ansi.StringWidth(name), "")
		pad := strings.Repeat(" ", max(0, room-ansi.StringWidth(name+mark)))
		rows[i] = base.Render(indent) + base.Foreground(theme.Dim).Render(marker) +
			base.Foreground(nc).Render(name) + base.Foreground(theme.Yellow).Render(mark) + base.Render(pad)
	}
	title := " " + fgStyle.Render(clip(filepath.Base(st.Root), iw, "")) + " "
	bc := border(cam, ide.Explorer)
	if o.Focused {
		bc = theme.BorderFocus
	}
	return frame(w, h, bc, title, rows)
}

// treeSel is the explorer row cam points at; -1 for none.
// explorerTop is the first row of the tree in an explorer ih rows high: near the camera's
// selection, or where the user scrolled.
func explorerTop(st *ide.State, o Options, ih int) int {
	return scrolled(scrollTop(treeSel(st, camera(st)), len(st.Tree), ih), o.Scroll[filesPane], max(0, len(st.Tree)-ih))
}

func treeSel(st *ide.State, cam *ide.Agent) int {
	if cam.TreeSel == "" {
		return -1
	}
	return slices.IndexFunc(st.Tree, func(r ide.TreeRow) bool { return r.Path == cam.TreeSel })
}

// fileTint is the colour of a file's name in the explorer: its type's colour as a pastel,
// changed or not; an unknown type is as plain as a folder.
func fileTint(path string) color.Color {
	if t := fileColor(path); t != nil {
		return theme.Mix(t, theme.Bright, 0.4)
	}
	return theme.Fg
}

// termPrompt is every terminal's prompt: the pane's title tells whose it is.
const termPrompt = "$ "

func terminalPane(st *ide.State, cam *ide.Agent, o Options, w, h int) []string {
	fw, ih := inner(w, h)
	iw := textWidth(fw)
	t := &cam.Term
	prompt := limeStyle.Render(clip(termPrompt, iw, ""))
	back := o.Scroll[termKey(cam.Key)]
	// A row of a line, styled only once it is in the window: scrolled far back, styling every row
	// below it would cost a frame its time.
	type row struct {
		line *ide.TermLine // nil for the live prompt line
		j    int           // the row's place among its line's
		text string
	}
	var rows []row
	// The last row belongs to the live prompt line; scrolled back, rows below the window are made too.
	lines := t.Lines[max(0, len(t.Lines)-max(0, ih-1+back)):]
	for i := range lines {
		l := &lines[i]
		pw := 0
		if l.Kind == ide.Command || l.Kind == ide.More {
			pw = ansi.StringWidth(prompt)
		}
		for j, text := range wrap(l.Text, iw-pw, iw) {
			rows = append(rows, row{l, j, text})
		}
	}
	live := fgStyle.Render(spinner[t.Spinner%len(spinner)])
	if ran := runTime(t, o.Now); ran != "" { // claude brings the output only once the command is over
		live += " " + dimStyle.Render(ran)
	}
	if !t.Running {
		var c string
		if working(cam, o) { // the caret is a spin frame while its agent works, else none; lime as the prompt
			c = limeStyle.Render(spinFrame(o.Now))
		}
		part := ide.Command
		if n := len(t.Lines); n > 0 && (t.Lines[n-1].Kind == ide.Command || t.Lines[n-1].Kind == ide.More) {
			part = ide.More // the input goes on with the command above
		}
		live = lead(prompt, part) + cmdStyle.Render(tail(t.Input, iw-ansi.StringWidth(prompt)-1)) + c
	}
	rows = append(rows, row{})
	back = min(back, max(0, len(rows)-ih)) // as far as the scrollback goes
	var shown []string
	for _, r := range rows[max(0, len(rows)-ih-back) : len(rows)-back] {
		text := r.text
		switch {
		case r.line == nil:
			text = live
		case r.line.Kind == ide.Command || r.line.Kind == ide.More:
			text = cmdStyle.Render(text)
			if r.j == 0 {
				text = lead(prompt, r.line.Kind) + text
			}
		case r.line.Kind == ide.ErrorOutput:
			text = errStyle.Render(text)
		case r.line.Kind == ide.Added:
			text = paintOn(text, langOf(r.line.Lang), theme.AddedBg)
		case r.line.Kind == ide.Removed:
			text = paintOn(text, langOf(r.line.Lang), theme.RemovedBg)
		case r.line.Kind == ide.Hunk:
			text = hunkStyle.Render(text)
		case r.line.Kind == ide.Meta && (strings.HasPrefix(r.line.Text, "diff ") || strings.HasPrefix(r.line.Text, "commit ")):
			text = headStyle.Render(text)
		case r.line.Kind == ide.Meta:
			text = dimStyle.Render(text)
		case r.line.Kind == ide.Code:
			text = codeLine(st, text, r.line)
		case r.line.Kind == ide.Match:
			text = hitLine(st, text, r.j == 0, r.line)
		default:
			text = outputLine(st, text, r.line.Text)
		}
		shown = append(shown, text)
	}
	// Tail-aligned: blank rows on top, newest line at the bottom.
	out := append(make([]string, ih-len(shown), ih), airy(shown)...)
	head := title(st, cam, func(int) string { return "terminal" }, fw-3)
	return frame(w, h, border(cam, ide.Terminal), " "+head+" ", out)
}

// runShown is how long a command runs before the terminal shows for how long.
const runShown = 3 * time.Second

// runTime is how long the running command of t has run by now, as the status line counts it:
// "7s", "1m05s", "1h02m"; "" for the first runShown, or when its start is unknown.
func runTime(t *ide.Term, now time.Time) string {
	d := now.Sub(t.RunAt).Truncate(time.Second)
	switch {
	case !t.Running || t.RunAt.IsZero() || d < runShown:
		return ""
	case d < time.Minute:
		return fmt.Sprintf("%ds", d/time.Second)
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", d/time.Minute, d%time.Minute/time.Second)
	}
	return fmt.Sprintf("%dh%02dm", d/time.Hour, d%time.Hour/time.Minute)
}

// lead is what goes before a command's line of kind k: the prompt on its first, as much room on
// those after it.
func lead(prompt string, k ide.LineKind) string {
	if k == ide.More {
		return strings.Repeat(" ", ansi.StringWidth(prompt))
	}
	return prompt
}

// inner is the space inside a w×h frame.
func inner(w, h int) (int, int) { return max(0, w-2), max(0, h-2) }

// textWidth is the room for the text of a pane iw cells wide inside its frame that has no gutter
// of its own (a terminal, a browser, the agents): a cell of air on either side keeps it off the
// frame. airy puts the left one before rows; the frame pads the right.
func textWidth(iw int) int { return max(0, iw-2) }

func airy(rows []string) []string {
	for i := range rows {
		rows[i] = " " + rows[i]
	}
	return rows
}

// frame draws body in a rounded border of colour bc, exactly w×h cells, with title on the top edge.
func frame(w, h int, bc color.Color, title string, body []string) []string {
	if h <= 0 {
		return nil
	}
	bs := lipgloss.NewStyle().Foreground(bc)
	b := lipgloss.RoundedBorder()
	iw, ih := inner(w, h)
	top := bs.Render(b.TopLeft + strings.Repeat(b.Top, iw) + b.TopRight)
	if iw > 0 {
		title = truncate(title, iw-1)
		top = bs.Render(b.TopLeft+b.Top) + title + bs.Render(strings.Repeat(b.Top, iw-1-ansi.StringWidth(title))+b.TopRight)
	}
	out := []string{fit(top, w)}
	for i := range ih {
		var line string
		if i < len(body) {
			line = body[i]
		}
		out = append(out, fit(bs.Render(b.Left)+fit(line, iw)+bs.Render(b.Right), w))
	}
	if h > 1 {
		out = append(out, fit(bs.Render(b.BottomLeft+strings.Repeat(b.Bottom, iw)+b.BottomRight), w))
	}
	return out
}

// fit pads or truncates s to exactly w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = truncate(s, w)
	return s + strings.Repeat(" ", w-ansi.StringWidth(s))
}

// truncate cuts s to at most w cells. ansi.Truncate alone can leave a cluster such as the keycap
// "1️⃣" one cell wider than ansi.StringWidth allows, so it cuts again until the text fits.
func truncate(s string, w int) string {
	for n := w; ansi.StringWidth(s) > w; n-- {
		s = ansi.Truncate(s, n, "")
	}
	return s
}

// scrollTop is the first visible row of n rows in a window of vis rows that keeps cur in its upper third.
func scrollTop(cur, n, vis int) int {
	return min(max(cur-vis/3, 0), max(0, n-vis))
}

// cell is one grapheme cluster as drawn.
type cell struct {
	s      string // what to print
	w      int    // width in terminal cells
	r0, r1 int    // rune range [r0, r1) in the source text
}

// layout splits s into cells: tabs become 4 spaces; escape sequences and other control characters
// vanish; invalid UTF-8 shows as U+FFFD. It stops once the cells are wider than maxW and cover more
// than minRunes runes, so huge lines stay cheap, and returns how many bytes of s it consumed.
func layout(s string, maxW, minRunes int) ([]cell, int) {
	var cs []cell
	var state byte
	x, r, n := 0, 0, 0
	for n < len(s) && (x <= maxW || r <= minRunes) {
		g, w, size, next := ansi.DecodeSequence(s[n:], state, nil)
		state = next
		c := cell{s: g, w: w, r0: r, r1: r + utf8.RuneCountInString(g)}
		switch first, _ := utf8.DecodeRuneInString(g); {
		case first == '\t':
			c.s, c.w = "    ", 4
		case unicode.IsControl(first):
			c.s, c.w = "", 0
		case !utf8.ValidString(g):
			c.s = strings.ToValidUTF8(g, "�")
			c.w = ansi.StringWidth(c.s)
		}
		cs = append(cs, c)
		x, r, n = x+c.w, c.r1, n+max(size, 1)
	}
	return cs, n
}

// clip renders s as plain one-line text at most w cells wide; text that does not fit ends with more.
func clip(s string, w int, more string) string {
	cs, n := layout(s, w, 0)
	total := 0
	for _, c := range cs {
		total += c.w
	}
	if n == len(s) && total <= w {
		return join(cs)
	}
	room := w - ansi.StringWidth(more)
	if room < 0 {
		return ""
	}
	x, i := 0, 0
	for ; i < len(cs) && x+cs[i].w <= room; i++ {
		x += cs[i].w
	}
	return join(cs[:i]) + more
}

// maxWrap is how many rows one terminal line takes at most.
const maxWrap = 4

// wrap cuts s into plain rows as a terminal wraps a long line: the first row first cells wide,
// the others w. Past maxWrap rows the text is cut with "…".
func wrap(s string, first, w int) []string {
	cs, _ := layout(s, math.MaxInt, 0)
	var rows []string
	room, x, start := max(first, 1), 0, 0
	for i, c := range cs {
		if x+c.w > room && i > start {
			if len(rows) == maxWrap-1 {
				return append(rows, clip(join(cs[start:]), room, "…"))
			}
			rows = append(rows, join(cs[start:i]))
			start, x, room = i, 0, max(w, 1)
		}
		x += c.w
	}
	return append(rows, join(cs[start:]))
}

// tail renders s as plain one-line text, dropping cells on the left until it is at most w cells wide.
func tail(s string, w int) string {
	cs, _ := layout(s, math.MaxInt, 0)
	x := 0
	for _, c := range cs {
		x += c.w
	}
	i := 0
	for ; i < len(cs) && x > w; i++ {
		x -= cs[i].w
	}
	return join(cs[i:])
}

func join(cs []cell) string {
	var b strings.Builder
	for _, c := range cs {
		b.WriteString(c.s)
	}
	return b.String()
}
