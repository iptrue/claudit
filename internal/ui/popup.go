package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/ideview"
	"github.com/pixelbender/claudit/internal/theme"
)

var styleInverse = styleAccent.Reverse(true) // header chip, highlighted button

// renderPopup draws the popup box that View centres over the screen. It fits in maxWidth×maxHeight
// and draws its own cursor for text fields; the terminal cursor stays hidden while a popup is open.
func renderPopup(p Popup, maxWidth, maxHeight int) string {
	box, _ := drawPopup(p, maxWidth, maxHeight)
	return box
}

// drawHit is a part of a popup a click acts on: cells [x0, x1) of line y of its content, and the
// option, button or row there.
type drawHit struct{ y, x0, x1, at int }

// drawPopup is renderPopup with the parts of the box a click acts on, in cells of the box.
func drawPopup(p Popup, maxWidth, maxHeight int) (string, []drawHit) {
	inner, rows := max(1, maxWidth-4), max(0, maxHeight-2)
	var lines []string
	var hits []drawHit
	switch p := p.(type) {
	case *QuestionPopup:
		lines, hits = drawQuestion(p, inner, rows)
	case *PermissionPopup:
		lines, hits = drawPermission(p, inner, rows)
	case *ResumePopup:
		lines, hits = drawResume(p, inner, rows)
	case *PickPopup:
		lines, hits = drawPick(p, inner, rows)
	case *PluginsPopup:
		lines, hits = drawPlugins(p, inner, rows)
	case *BtwPopup:
		lines = drawBtw(p, inner, rows)
	}
	var box []drawHit
	for _, h := range hits {
		if h.y < rows { // inside the border and its padding
			box = append(box, drawHit{y: h.y + 1, x0: h.x0 + 2, x1: h.x1 + 2, at: h.at})
		}
	}
	return strings.Join(drawBox(lines[:min(len(lines), rows)], inner, theme.Bright), "\n"), box // white: it waits for the user
}

func drawQuestion(p *QuestionPopup, w, rows int) ([]string, []drawHit) {
	if p.Current < 0 || p.Current >= len(p.Questions) {
		return nil, nil // malformed input from the agent
	}
	q := &p.Questions[p.Current]
	head := styleDim.Render(fmt.Sprintf("Question %d/%d", p.Current+1, len(p.Questions)))
	if q.Header != "" {
		head = styleInverse.Render(" "+drawOneLine(q.Header)+" ") + "  " + head
	}
	head += drawAskedBy(p.AgentName, p.AgentColor)
	hints := "↑/↓ choose · enter select · esc dismiss"
	if q.Multi {
		hints = "↑/↓ choose · space toggle · enter submit · esc dismiss"
	}
	tail := []string{"", styleDim.Render(ansi.Truncate(hints, w, "…"))}

	// Descriptions go under their labels; when that is too tall, after them, and then the question shrinks.
	text := drawWrap(q.Text, w)
	opts, owners := drawOptions(q, w, false)
	if 2+len(text)+len(opts)+len(tail) > rows {
		opts, owners = drawOptions(q, w, true)
	}
	text = drawCut(text, max(1, rows-2-len(opts)-len(tail)), w)

	lines := []string{ansi.Truncate(head, w, "…"), ""}
	lines = append(lines, drawStyled(text, styleStrong.Render)...)
	var hits []drawHit
	for i, o := range owners {
		if o >= 0 {
			hits = append(hits, drawHit{y: len(lines) + i, x1: w, at: o})
		}
	}
	lines = append(lines, opts...)
	return append(lines, tail...), hits
}

// drawOptions renders a blank line, the option rows and the "Other" row; owners has the option of
// each line (len(q.Options) for "Other"), -1 for none.
func drawOptions(q *Question, w int, compact bool) (lines []string, owners []int) {
	add := func(owner int, l string) {
		lines, owners = append(lines, l), append(owners, owner)
	}
	add(-1, "")
	row := func(i int) (string, int) {
		mark := "  "
		if q.Cursor == i {
			mark = styleAccent.Render("› ")
		}
		if q.Multi {
			box := "[ ] "
			if i < len(q.Checked) && q.Checked[i] {
				box = "[x] "
			}
			return mark + styleFg.Render(box), 6
		}
		return mark, 2
	}
	for i, o := range q.Options {
		prefix, indent := row(i)
		label := styleFg.Render(drawOneLine(o.Label))
		if q.Cursor == i {
			label = styleAccent.Render(drawOneLine(o.Label))
		}
		switch {
		case o.Description == "":
			add(i, ansi.Truncate(prefix+label, w, "…"))
		case compact:
			add(i, ansi.Truncate(prefix+label+"  "+styleDim.Render(drawOneLine(o.Description)), w, "…"))
		default:
			add(i, ansi.Truncate(prefix+label, w, "…"))
			for _, d := range drawWrap(o.Description, w-indent-2) {
				add(i, strings.Repeat(" ", indent+2)+styleDim.Render(d))
			}
		}
	}

	other := len(q.Options)
	prefix, indent := row(other)
	label := "Other: "
	avail := max(1, w-indent-len(label))
	if q.Cursor == other {
		add(other, prefix+styleAccent.Render(label)+drawField(&q.Other, avail))
		return lines, owners
	}
	text := ansi.Truncate(string(drawInputRunes(q.Other.Text)), avail, "…")
	add(other, prefix+styleFg.Render(label+text))
	return lines, owners
}

// drawField draws an editable text field of at most avail cells with a reverse-video block cursor.
func drawField(in *Input, avail int) string {
	disp := drawInputRunes(in.Text)
	cur := min(max(in.Cursor, 0), len(disp))
	start := drawScroll(disp, cur, avail)
	at, rest := " ", ""
	if cur < len(disp) {
		at, rest = string(disp[cur]), ansi.Truncate(string(disp[cur+1:]), avail, "")
	}
	s := styleFg.Render(string(disp[start:cur])) + styleCaret.Render(at) + styleFg.Render(rest)
	return ansi.Truncate(s, avail, "")
}

// drawPermission draws a permission request: what the tool is about to do in full, scrolled
// when it does not fit (p.Scroll is kept in range here), then the buttons.
func drawPermission(p *PermissionPopup, w, rows int) ([]string, []drawHit) {
	head := styleAccent.Render("Allow "+drawOneLine(p.Tool)+"?") + drawAskedBy(p.AgentName, p.AgentColor)
	lines := []string{ansi.Truncate(head, w, "…"), ""}
	hints := "y allow · n/esc deny · ←/→ · enter · shift+tab mode"
	if body := drawDetail(p.Tool, p.Detail, w); len(body) > 0 {
		// Six fixed rows: title, blank, blank, buttons, blank, hints.
		body, scrolls := drawScrolled(body, &p.Scroll, max(1, rows-6), w)
		if scrolls {
			hints = "↑/↓ pgup/pgdn scroll · " + hints
		}
		lines = append(append(lines, body...), "")
	}
	allow, deny := styleDim.Render("[ Allow ]"), styleDim.Render("[ Deny ]")
	if p.Allow {
		allow = styleInverse.Render("[ Allow ]")
	} else {
		deny = styleInverse.Render("[ Deny ]")
	}
	hits := []drawHit{{y: len(lines), x0: 0, x1: 9, at: 0}, {y: len(lines), x0: 11, x1: 19, at: 1}}
	return append(lines, allow+"  "+deny, "", styleDim.Render(ansi.Truncate(hints, w, "…"))), hits
}

// drawScrolled shows room rows of body from line *scroll, which it keeps in range. A body taller
// than room gives a row to a rule saying which lines are on screen; scrolls reports that.
func drawScrolled(body []string, scroll *int, room, w int) (_ []string, scrolls bool) {
	if len(body) <= room {
		*scroll = 0
		return body, false
	}
	room = max(1, room-1)
	*scroll = min(max(*scroll, 0), len(body)-room)
	at := fmt.Sprintf("── %d–%d of %d lines ", *scroll+1, *scroll+room, len(body))
	return append(body[*scroll:*scroll+room:*scroll+room], styleRule.Render(ansi.Truncate(at+strings.Repeat("─", w), w, ""))), true
}

// drawBtw draws a side question and its answer as markdown, or what keeps it.
func drawBtw(p *BtwPopup, w, rows int) []string {
	question := drawCut(drawWrap(p.Question, max(1, w-5)), 3, w-5)
	lines := []string{styleAccent.Render("/btw ") + styleFg.Render(question[0])}
	for _, l := range question[1:] {
		lines = append(lines, "     "+styleFg.Render(l))
	}
	lines = append(lines, "")
	var body []string
	switch {
	case p.Failed:
		for _, l := range drawWrap(p.Answer, w) {
			body = append(body, styleErr.Render(l))
		}
	case p.Answer == "":
		body = []string{styleDim.Render("Thinking…")}
	default:
		body = drawMarkdown(p.Answer, w)
	}
	hints := "esc close"
	// Fixed rows: the question, blank, blank, hints.
	body, scrolls := drawScrolled(body, &p.Scroll, max(1, rows-len(lines)-2), w)
	if scrolls {
		hints = "↑/↓ pgup/pgdn scroll · " + hints
	}
	return append(append(lines, body...), "", styleDim.Render(ansi.Truncate(hints, w, "…")))
}

// drawResume draws the conversations to resume with their ages, the one claude is in marked as
// pickers mark the one in effect, scrolled to keep the highlighted one in sight.
func drawResume(p *ResumePopup, w, rows int) ([]string, []drawHit) {
	head := styleAccent.Render("Resume a conversation") + styleDim.Render(fmt.Sprintf("  %d", len(p.Sessions)))
	lines := []string{ansi.Truncate(head, w, "…"), ""}
	n := min(max(1, rows-4), len(p.Sessions)) // four fixed rows: title, blank, blank, hints
	top := min(max(p.Cursor-n/2, 0), len(p.Sessions)-n)
	var hits []drawHit
	for i, s := range p.Sessions[top : top+n] {
		hits = append(hits, drawHit{y: len(lines), x1: w, at: top + i})
		mark, st := "  ", styleFg
		if top+i == p.Cursor {
			mark, st = styleAccent.Render("› "), styleAccent
		}
		age := drawAge(p.Now.Sub(s.Modified))
		var current string
		if s.ID == p.Current {
			current = " ✓" // kept when the title is cut
		}
		room := max(1, w-4-ansi.StringWidth(age))
		title := drawOneLine(s.Title)
		if ansi.StringWidth(title+current) > room {
			title = ansi.Truncate(title, max(1, room-ansi.StringWidth(current)), "…")
		}
		lines = append(lines, mark+st.Render(drawFit(title+current, room))+"  "+styleDim.Render(age))
	}
	return append(lines, "", styleDim.Render(ansi.Truncate("↑/↓ choose · enter resume · esc cancel", w, "…"))), hits
}

// drawPick draws what a slash command takes, the one in effect marked, scrolled to keep the
// highlighted one in sight.
func drawPick(p *PickPopup, w, rows int) ([]string, []drawHit) {
	lines := []string{styleAccent.Render(ansi.Truncate("/"+p.Command, w, "…")), ""}
	n := min(max(1, rows-4), len(p.Options)) // four fixed rows: title, blank, blank, hints
	top := min(max(p.Cursor-n/2, 0), len(p.Options)-n)
	var hits []drawHit
	for i, o := range p.Options[top : top+n] {
		mark, st := "  ", styleFg
		if top+i == p.Cursor {
			mark, st = styleAccent.Render("› "), styleAccent
		}
		label := drawOneLine(o.Label)
		if top+i == p.Current {
			label += " ✓"
		}
		row := mark + st.Render(label)
		if o.Description != "" {
			row += "  " + styleDim.Render(drawOneLine(o.Description))
		}
		hits = append(hits, drawHit{y: len(lines), x1: w, at: top + i})
		lines = append(lines, ansi.Truncate(row, w, "…"))
	}
	return append(lines, "", styleDim.Render(ansi.Truncate("↑/↓ choose · enter set · esc cancel", w, "…"))), hits
}

// drawPlugins draws the manager of /plugins: its tabs, then the plugins installed, each on or off,
// or the field that finds offers and the ones it finds, scrolled to keep the highlighted one in sight.
func drawPlugins(p *PluginsPopup, w, rows int) ([]string, []drawHit) {
	tab := func(name string, n int, on bool) string {
		st := styleDim
		if on {
			st = styleInverse
		}
		return st.Render(fmt.Sprintf(" %s %d ", name, n))
	}
	head := styleAccent.Render("Plugins") + "  " + tab("Installed", len(p.Installed), !p.Discover) + " " + tab("Discover", len(p.Offers), p.Discover)
	lines := []string{ansi.Truncate(head, w, "…"), ""}
	hint := "↑/↓ choose · space on/off · u uninstall · tab discover · esc close"
	if p.Discover {
		lines = append(lines, styleDim.Render("find ")+drawField(&p.Filter, max(1, w-5)), "")
		hint = "type to find · ↑/↓ choose · enter install · tab installed · esc close"
	}
	if p.Confirm != "" {
		hint = "u again uninstalls " + p.Confirm + " · any other key keeps it"
	}
	var hits []drawHit
	switch {
	case p.Loading:
		lines = append(lines, styleDim.Render("Reading the plugins…"))
	case p.rows() == 0 && p.Discover:
		lines = append(lines, styleDim.Render("No plugin on offer matches"))
	case p.rows() == 0:
		lines = append(lines, styleDim.Render("No plugin installed: tab finds some"))
	default:
		n := min(max(1, rows-len(lines)-2), p.rows()) // two fixed rows below: blank, hints
		top := min(max(p.Cursor-n/2, 0), p.rows()-n)
		id := func(i int) string {
			if p.Discover {
				return drawOneLine(p.Offers[p.Shown[i]].ID)
			}
			return drawOneLine(p.Installed[i].ID)
		}
		idw := 0 // the notes stand in a column after the widest id on screen
		for i := top; i < top+n; i++ {
			idw = max(idw, ansi.StringWidth(id(i)))
		}
		for i := top; i < top+n; i++ {
			mark, st := "  ", styleFg
			if i == p.Cursor {
				mark, st = styleAccent.Render("› "), styleAccent
			}
			row, name, note := mark, id(i), ""
			if p.Discover {
				note = p.Offers[p.Shown[i]].Description
			} else {
				pl := p.Installed[i]
				dot := styleDim.Render("○ ")
				if pl.Enabled {
					dot = styleLime.Render("● ")
				}
				row, note = row+dot, pl.Scope
			}
			row += st.Render(name)
			if name == p.Busy {
				note = p.Doing + "…"
			}
			if note != "" {
				row += strings.Repeat(" ", idw-ansi.StringWidth(name)+2) + styleDim.Render(drawOneLine(note))
			}
			hits = append(hits, drawHit{y: len(lines), x1: w, at: i})
			lines = append(lines, ansi.Truncate(row, w, "…"))
		}
	}
	return append(lines, "", styleDim.Render(ansi.Truncate(hint, w, "…"))), hits
}

// drawAge says how long ago something was: "just now", "5m ago", "3h ago", "2d ago".
func drawAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", d/time.Minute)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", d/time.Hour)
	}
	return fmt.Sprintf("%dd ago", d/(24*time.Hour))
}

// drawDetail renders what a tool is about to do, w cells wide: a diff in its colours, code
// highlighted, a plan as markdown.
func drawDetail(tool, detail string, w int) []string {
	detail = strings.TrimRight(drawClean(detail), "\n")
	if strings.TrimSpace(detail) == "" {
		return nil
	}
	code := func(lines []string, lang string) []string {
		var out []string
		for _, l := range lines {
			for _, piece := range drawWrap(l, w) {
				out = append(out, ideview.Highlight(piece, lang))
			}
		}
		return out
	}
	lines := strings.Split(detail, "\n")
	switch tool {
	case "ExitPlanMode":
		return drawMarkdown(detail, w)
	case "Bash":
		return code(lines, "sh")
	case "Workflow":
		return code(lines, "js")
	case "Write":
		lang := strings.TrimPrefix(filepath.Ext(lines[0]), ".")
		return append([]string{styleStrong.Render(ansi.Truncate(lines[0], w, "…"))}, code(lines[1:], lang)...)
	case "Edit":
		out := []string{styleStrong.Render(ansi.Truncate(lines[0], w, "…"))}
		for _, l := range lines[1:] {
			st := styleDim // context
			switch {
			case strings.HasPrefix(l, "-"):
				st = styleFg.Background(theme.RemovedBg)
			case strings.HasPrefix(l, "+"):
				st = styleFg.Background(theme.AddedBg)
			}
			for _, piece := range drawWrap(l, w) {
				out = append(out, st.Render(piece))
			}
		}
		return out
	}
	return drawStyled(drawWrap(detail, w), styleFg.Render)
}

// drawAskedBy is the header chip naming the subagent that asks, in its colour; "" for the main agent.
func drawAskedBy(name string, slot int) string {
	if name == "" {
		return ""
	}
	chip := lipgloss.NewStyle().Foreground(drawAgentColor(slot)).Reverse(true)
	return "  " + chip.Render(" asked by "+drawOneLine(name)+" ")
}
