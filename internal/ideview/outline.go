package ideview

import (
	"fmt"
	"strconv"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/ide"
	"github.com/pixelbender/claudit/internal/theme"
)

// outlineKey is the scroll key of the outline in the left column.
const outlineKey = "outline"

// outlined is the spec the left column outlines in place of the files: the one the first editor
// on the right shows structured; nil for none, or while the left column shows the agents.
func outlined(st *ide.State, o Options, width, height int) *specView {
	if o.Panel != Files {
		return nil
	}
	ew := leftWidth(width)
	cols := columns(st, camera(st), o, width-ew, height)
	for i, r := range tiles(len(cols), width-ew, height) {
		if cols[i].pane != ide.Editor {
			continue
		}
		iw, ih := inner(r.Dx(), r.Dy())
		if v := structured(st, cols[i].agent, o, iw, ih); v != nil {
			return v
		}
	}
	return nil
}

// outlineRow is a row of the outline: the label of a section, a requirement of it, or the blank
// between two sections.
type outlineRow struct{ sec, req int } // -1 for none

// outline is the rows of v's outline: per section with requirements its label, then a row per
// requirement; the row of the requirement at the top of the editor; and the outline's first row
// in ih rows that keeps that one in sight, and the first row it shows, scrolled as o says.
func (v *specView) outline(o Options, ih int) (rows []outlineRow, sel, follow, top int) {
	sec, req := v.current()
	sel = -1
	for i, s := range v.doc.sections {
		if len(s.reqs) == 0 {
			continue
		}
		if len(rows) > 0 {
			rows = append(rows, outlineRow{-1, -1})
		}
		if sectionLabel(s) != "" {
			rows = append(rows, outlineRow{i, -1})
		}
		for j := range s.reqs {
			if i == sec && j == req {
				sel = len(rows)
			}
			rows = append(rows, outlineRow{i, j})
		}
	}
	follow = scrollTop(sel, len(rows), ih)
	return rows, sel, follow, scrolled(follow, o.Scroll[outlineKey], max(0, len(rows)-ih))
}

// outlinePane draws the outline of v in the left column, w×h cells: the sections' labels in their
// colours; a row per requirement, its name after the bar of its section, the count of its
// scenarios at the right; the requirement at the top of the editor lit as the viewed file is in
// the explorer.
func outlinePane(v *specView, o Options, w, h int) []string {
	iw, ih := inner(w, h)
	rows, sel, _, top := v.outline(o, ih)
	out := make([]string, ih)
	for i := range out {
		r := top + i
		if r >= len(rows) || rows[r].sec < 0 {
			continue
		}
		s := v.doc.sections[rows[r].sec]
		c := opColor(s.op)
		if rows[r].req < 0 {
			out[i] = lipgloss.NewStyle().Foreground(c).Bold(true).Render(clip(sectionLabel(s), iw, "…"))
			continue
		}
		req := s.reqs[rows[r].req]
		base := lipgloss.NewStyle()
		if r == sel {
			base = base.Background(theme.SelBg)
		}
		n := strconv.Itoa(len(req.scenarios))
		room := max(0, iw-3-len(n)) // the bar and a space before the name, a space before the count
		name := clip(req.name, room, "…")
		pad := fmt.Sprintf("%*s", room-ansi.StringWidth(name)+1, "")
		out[i] = base.Foreground(c).Render("▌") + base.Render(" ") + base.Foreground(theme.Bright).Render(name) +
			base.Render(pad) + base.Foreground(theme.Dim).Render(n)
	}
	reqs, scenarios := v.doc.counts()
	head := count(reqs, "requirement") + " · " + count(scenarios, "scenario")
	if ansi.StringWidth(head) > iw-3 { // the scenarios give way rather than be cut
		head = count(reqs, "requirement")
	}
	title := " " + fgStyle.Render(head) + " "
	return frame(w, h, theme.Border, title, out)
}

// SpecAt is what a click at cell (x, y) of Render(st, o, width, height) does to a spec shown
// structured: on a row of the editor that heads a section, a requirement or a scenario, or on a
// requirement of the outline. It is false elsewhere.
func SpecAt(st *ide.State, o Options, width, height, x, y int) (SpecHit, bool) {
	ew := leftWidth(width)
	if y < 1 || y >= height-1 || x < 0 || x >= width {
		return SpecHit{}, false
	}
	if x < ew {
		v := outlined(st, o, width, height)
		if v == nil || x < 1 || x >= ew-1 {
			return SpecHit{}, false
		}
		_, ih := inner(ew, height)
		rows, _, _, top := v.outline(o, ih)
		if i := top + y - 1; i < len(rows) && rows[i].req >= 0 {
			return v.hit(rowReq, rows[i].sec, rows[i].req, -1, true)
		}
		return SpecHit{}, false
	}
	c, t, ok := columnAt(st, o, width, height, x, y)
	if !ok || c.pane != ide.Editor || x < t.Min.X+1 || x >= t.Max.X-1 || y < t.Min.Y+1 || y >= t.Max.Y-1 {
		return SpecHit{}, false
	}
	iw, ih := inner(t.Dx(), t.Dy())
	v := structured(st, c.agent, o, iw, ih)
	if v == nil || v.top+y-t.Min.Y-1 >= len(v.rows) {
		return SpecHit{}, false
	}
	r := v.rows[v.top+y-t.Min.Y-1]
	return v.hit(r.kind, r.sec, r.req, r.scen, false)
}

// hit is what a click on the row of kind at section sec, requirement req, scenario scen does:
// it turns the fold the row heads; from the outline, it opens the requirement and brings it to
// the top of the editor.
func (v *specView) hit(kind rowKind, sec, req, scen int, outline bool) (SpecHit, bool) {
	h := SpecHit{Path: v.path}
	switch s := v.doc.sections[max(sec, 0)]; {
	case kind == rowText:
		return SpecHit{}, false
	case kind == rowSection: // they all open, unless they are all open: then they all fold
		h.Open = false
		for j, r := range s.reqs {
			h.Folds = append(h.Folds, reqKey(s, r))
			h.Open = h.Open || !reqOpen(v.folds, s, j)
		}
	case kind == rowScenario:
		r := s.reqs[req]
		key := scenarioKey(s, r, r.scenarios[scen])
		h.Folds, h.Open = []string{key}, !v.folds[key]
	case outline:
		h.Folds, h.Open = []string{reqKey(s, s.reqs[req])}, true
		h.Key = specKey(v.path)
		for i, r := range v.rows {
			if r.kind == rowReq && r.sec == sec && r.req == req {
				h.Back = -i
				break
			}
		}
	default:
		h.Folds, h.Open = []string{reqKey(s, s.reqs[req])}, !reqOpen(v.folds, s, req)
	}
	return h, true
}
