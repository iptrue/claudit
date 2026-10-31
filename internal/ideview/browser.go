package ideview

import (
	"math"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/ide"
	"github.com/pixelbender/claudit/internal/theme"
)

var (
	linkStyle = lipgloss.NewStyle().Foreground(theme.Sky)
	urlStyle  = lipgloss.NewStyle().Foreground(theme.Mix(theme.OK, theme.Dim, 0.3))
	headStyle = lipgloss.NewStyle().Foreground(theme.Strong)
	ruleStyle = lipgloss.NewStyle().Foreground(theme.Border)
)

// browserPane draws a's browser: the address bar, a rule, and the page from its top.
func browserPane(st *ide.State, a *ide.Agent, o Options, w, h int) []string {
	fw, ih := inner(w, h)
	iw := textWidth(fw)
	p := &a.Page
	ph := pageHeight(ih)
	rows := pageRows(p, iw, ph)
	if back := o.Scroll[webKey(a.Key)]; back != 0 {
		rows = pageRows(p, iw, -1)
		rows = rows[scrolled(0, back, max(0, len(rows)-ph)):]
	}
	body := airy(append([]string{addressBar(a, o, iw), ruleStyle.Render(strings.Repeat("─", max(iw, 0)))}, rows...))
	head := title(st, a, func(int) string { return "web" }, fw-3)
	return frame(w, h, theme.Border, " "+head+" ", body)
}

// pageHeight is how many rows of the page a browser ih rows high shows under its two bars.
func pageHeight(ih int) int { return max(0, ih-2) }

// addressBar is the first row of a's browser: the address, its scheme grey, on the pane's own
// background. The caret is a solid block while a types the address, a spin frame while a works
// otherwise, and a spin frame a space after it while the page loads.
func addressBar(a *ide.Agent, o Options, iw int) string {
	p := &a.Page
	var caret string
	switch {
	case !p.LoadAt.IsZero():
		caret = okStyle.Render(" " + spinFrame(o.Now))
	case len(p.Lines) == 0 && typing(a, o):
		caret = lipgloss.NewStyle().Background(theme.OK).Render(" ")
	case len(p.Lines) == 0 && working(a, o):
		caret = okStyle.Render(spinFrame(o.Now))
	}
	room := iw - ansi.StringWidth(caret)
	text := tail(p.URL, max(room, 0))
	var field string
	switch i := strings.Index(text, "://"); {
	case p.Search:
		field = dimStyle.Render("search ") + fgStyle.Render(tail(p.URL, max(room-7, 0)))
	case i >= 0:
		field = dimStyle.Render(text[:i+3]) + fgStyle.Render(text[i+3:])
	default:
		field = fgStyle.Render(text)
	}
	field += caret
	return field + strings.Repeat(" ", max(0, iw-ansi.StringWidth(field)))
}

// pageRows draws page p iw cells wide, at least need rows of it (all of it when need < 0): a link
// as its title over its address, text wrapped at spaces with headings lit and markdown marks gone.
func pageRows(p *ide.Page, iw, need int) []string {
	var rows []string
	for _, l := range p.Lines {
		if need >= 0 && len(rows) >= need {
			break
		}
		if l.URL != "" {
			rows = append(rows, linkStyle.Render(clip(l.Text, iw, "…")), urlStyle.Render(clip(l.URL, iw, "…")), "")
			continue
		}
		st, text := fgStyle, strings.NewReplacer("**", "", "`", "").Replace(l.Text)
		trimmed := strings.TrimLeft(text, " ")
		switch {
		case p.Failed:
			st = errStyle
		case strings.HasPrefix(trimmed, "#"):
			st, text = headStyle, strings.TrimLeft(trimmed, "# ")
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			text = text[:len(text)-len(trimmed)] + "• " + trimmed[2:]
		}
		for _, r := range wrapWords(text, iw) {
			rows = append(rows, st.Render(r))
		}
	}
	return rows
}

// wrapWords cuts s into rows of at most w cells, after the last space that fits where it can.
func wrapWords(s string, w int) []string {
	cs, _ := layout(s, math.MaxInt, 0)
	segs := segments(s, w)
	rows := make([]string, len(segs))
	var b strings.Builder
	x, i := 0, 0
	for _, c := range cs {
		for i < len(segs)-1 && x >= segs[i][1] {
			rows[i] = b.String()
			b.Reset()
			i++
		}
		b.WriteString(c.s)
		x += c.w
	}
	rows[i] = b.String()
	return rows
}
