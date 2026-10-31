package ideview

import (
	"fmt"
	"image/color"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/ide"
	"github.com/pixelbender/claudit/internal/theme"
)

// treeRow is one line of the agents panel: an agent, a phase header, or a count of queued agents.
type treeRow struct {
	agent       *ide.Agent
	depth       int
	header      string // phase title
	done, total int    // phase header: agents finished of all
	queued      int
}

// agentTree lays out the agents panel: every agent below its parent, in order of appearance; the
// agents of a workflow grouped by phase, its queued ones counted per phase instead of listed.
func agentTree(st *ide.State) []treeRow {
	var rows []treeRow
	seen := map[*ide.Agent]bool{}
	var walk func(a *ide.Agent, depth int)
	walk = func(a *ide.Agent, depth int) {
		if seen[a] {
			return
		}
		seen[a] = true
		rows = append(rows, treeRow{agent: a, depth: depth})
		var kids []*ide.Agent
		for _, k := range st.Agents {
			if k.Parent == a.Key && k != a {
				kids = append(kids, k)
			}
		}
		if a.Kind != ide.Workflow {
			for _, k := range kids {
				walk(k, depth+1)
			}
			return
		}
		var phases []string
		for _, k := range kids {
			if !slices.Contains(phases, k.Phase) {
				phases = append(phases, k.Phase)
			}
		}
		for _, p := range phases {
			d := depth + 1
			if p != "" {
				h := treeRow{depth: d, header: p}
				for _, k := range kids {
					if k.Phase == p {
						h.total++
						if k.Status == ide.Done || k.Status == ide.Failed {
							h.done++
						}
					}
				}
				rows = append(rows, h)
				d++
			}
			queued := 0
			for _, k := range kids {
				switch {
				case k.Phase != p:
				case k.Status == ide.Queued:
					queued++
				default:
					walk(k, d)
				}
			}
			if queued > 0 {
				rows = append(rows, treeRow{depth: d, queued: queued})
			}
		}
	}
	for _, a := range st.Agents {
		if a.Parent == "" || findAgent(st, a.Parent) == nil {
			walk(a, 0)
		}
	}
	return rows
}

// agentsView is the agents tree and its first row in view, ih rows high: the highlighted row is
// kept in view, else the camera's.
func agentsView(st *ide.State, cam *ide.Agent, o Options, ih int) ([]treeRow, int) {
	tree := agentTree(st)
	cur := slices.IndexFunc(tree, func(r treeRow) bool { return r.agent == cam })
	if i := slices.IndexFunc(tree, func(r treeRow) bool { return r.agent != nil && r.agent.Key == o.Sel }); o.Focused && i >= 0 {
		cur = i
	}
	return tree, scrollTop(cur, len(tree), ih)
}

func agentsPane(st *ide.State, cam *ide.Agent, o Options, w, h int) []string {
	iw, ih := inner(w, h)
	tree, top := agentsView(st, cam, o, ih)
	rows := make([]string, ih)
	for i := range rows {
		if top+i >= len(tree) {
			break
		}
		rows[i] = " " + agentRow(tree[top+i], o, textWidth(iw))
	}
	mode := "auto"
	if st.Pinned {
		mode = "pinned"
	}
	title := " " + fgStyle.Render("agents") + dimStyle.Render(" · "+mode) + " "
	return frame(w, h, theme.Border, title, rows)
}

// agentRow draws one line of the agents tree, w cells wide: the hotkey in a gutter, then the
// indented status and name, and at the right edge, grey, the model it runs on and the duration.
// The tree tells the agent's kind; what it does shows in its workplace.
func agentRow(r treeRow, o Options, w int) string {
	indent := strings.Repeat(" ", 2*r.depth)
	a := r.agent
	if a == nil {
		if r.queued > 0 {
			return dimStyle.Render(clip(fmt.Sprintf("  %s· %d queued", indent, r.queued), w, "…"))
		}
		return phaseRow(r, indent, w)
	}
	base := lipgloss.NewStyle()
	if o.Focused && a.Key == o.Sel {
		base = base.Background(theme.SelBg)
	}
	glyph, gc := status(a, o.Now)
	hot := " "
	if a.Hotkey != 0 {
		hot = strconv.Itoa(a.Hotkey)
	}
	nameStyle := base.Foreground(agentColor(a))
	dur := duration(a, o.Now)
	room := w - 4 - len(indent) // the hotkey, the status and a space after each
	if dur != "" {
		room -= 1 + len(dur)
	}
	name := clip(a.Name, room, "…")
	room -= ansi.StringWidth(name)
	var model string
	if f := family(a.Model); f != "" && room > len(f) {
		model = " " + f
		room -= len(model)
	}
	if dur != "" {
		dur = " " + dur
	}
	return base.Foreground(theme.Dim).Render(hot) + base.Render(" "+indent) + base.Foreground(gc).Render(glyph) + base.Render(" ") +
		nameStyle.Render(name) + base.Foreground(theme.Dim).Render(strings.Repeat(" ", max(0, room))+model+dur)
}

// family is the short name of a model: "haiku" for "claude-haiku-5-5", the id without "claude-"
// for one of no known family; "" for none.
func family(model string) string {
	for _, f := range []string{"opus", "sonnet", "haiku", "fable"} {
		if strings.Contains(model, f) {
			return f
		}
	}
	return strings.TrimPrefix(model, "claude-")
}

// phaseRow is a phase header: its title, and a bar of its agents finished of all when it fits.
func phaseRow(r treeRow, indent string, w int) string {
	head := "  " + indent + clip(r.header, w, "") // foreign text: no tabs or line breaks
	n := min(r.total, 8)                          // bar cells
	count := fmt.Sprintf(" %d/%d", r.done, r.total)
	if r.total == 0 || ansi.StringWidth(head)+1+n+len(count) > w {
		return dimStyle.Render(clip(head, w, "…"))
	}
	full := r.done * n / r.total
	bar := okStyle.Render(strings.Repeat("▰", full)) + dimStyle.Render(strings.Repeat("▱", n-full))
	return fgStyle.Render(head) + " " + bar + dimStyle.Render(count)
}

// status is the glyph of a's status and its colour; a running agent spins with now.
func status(a *ide.Agent, now time.Time) (string, color.Color) {
	switch a.Status {
	case ide.Done:
		return "✓", theme.OK
	case ide.Failed:
		return "✗", theme.Err
	case ide.Queued:
		return "·", theme.Dim
	}
	n := int64(len(spinner))
	return spinner[(now.UnixMilli()/80%n+n)%n], agentColor(a)
}

// duration is how long a has worked, as "12s", "3m" or "2h"; "" when it has not started.
func duration(a *ide.Agent, now time.Time) string {
	if a.Started.IsZero() {
		return ""
	}
	end := a.Ended
	if end.IsZero() {
		end = now
	}
	d := max(end.Sub(a.Started), 0)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", d/time.Second)
	case d < time.Hour:
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	return fmt.Sprintf("%dh", d/time.Hour)
}
