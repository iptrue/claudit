package ui

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/theme"
)

var drawSpinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// styleYou is the colour of the user's prompts, as Claude Code labels them; claude's replies are white.
var styleYou = lipgloss.NewStyle().Foreground(theme.Blue)

// chatCache keeps the drawn lines of each chat entry, so a frame redraws only the entries that changed.
type chatCache struct {
	width   int
	entries []ChatEntry // each entry as it was when drawn
	lines   [][]string
}

// draw returns the lines of entry i, drawing it again only when it changed.
func (c *chatCache) draw(i int, e ChatEntry, w int) []string {
	if w != c.width {
		*c = chatCache{width: w}
	}
	for len(c.entries) <= i {
		c.entries = append(c.entries, ChatEntry{Kind: -1}) // never equal to a real entry
		c.lines = append(c.lines, nil)
	}
	if c.entries[i] != e {
		c.entries[i], c.lines[i] = e, drawEntry(e, w)
	}
	return c.lines[i]
}

// drawChatTime is the room at the right of the chat for the time of a message, "  15:04"; a chat
// narrower than drawChatTimeMin shows no times.
const (
	drawChatTime    = 7
	drawChatTimeMin = 30
)

// drawChatWidths are the cells of a width-wide chat row for the text of an entry and for its time.
func drawChatWidths(width int) (text, at int) {
	if width < drawChatTimeMin {
		return max(1, width-4), 0
	}
	return width - 4 - drawChatTime, drawChatTime
}

// drawChatCount is how many lines the conversation takes width cells wide.
func drawChatCount(cache *chatCache, entries []ChatEntry, width int) int {
	tw, _ := drawChatWidths(width)
	lines, _ := drawChatLines(cache, entries, tw, 0)
	return len(lines)
}

// drawChatLines are the lines of the conversation tw cells wide, and beside each the time its
// entry was said when it is the entry's first and aw leaves room for it. A blank line sets the
// user's prompts apart from what claude said before them; claude's caret stands in its place.
func drawChatLines(cache *chatCache, entries []ChatEntry, tw, aw int) (lines, times []string) {
	prev := UserEntry
	for i, e := range entries {
		drawn := cache.draw(i, e, tw)
		if len(drawn) == 0 {
			continue
		}
		if e.Kind == UserEntry && prev != UserEntry && prev != CaretEntry && len(lines) > 0 {
			lines, times = append(lines, ""), append(times, "")
		}
		prev = e.Kind
		for j, l := range drawn {
			at := ""
			if j == 0 && aw > 0 && !e.At.IsZero() {
				at = styleDim.Render(e.At.Format("15:04"))
			}
			lines, times = append(lines, l), append(times, at)
		}
	}
	return lines, times
}

// renderChat draws the tail of the conversation, scrolled up by scroll lines, into exactly width×height cells:
// each entry with the time it was said grey at the right of its first line. maxScroll is the largest
// scroll that still shows something new.
func renderChat(cache *chatCache, entries []ChatEntry, width, height, scroll int) (out string, maxScroll int) {
	tw, aw := drawChatWidths(width)
	lines, times := drawChatLines(cache, entries, tw, aw)

	maxScroll = max(0, len(lines)-height)
	scroll = min(max(scroll, 0), maxScroll)
	end := len(lines) - scroll
	from := max(0, end-height)
	rows := make([]string, height)
	top := height - (end - from) // tail-aligned: short logs sit right above the input
	for i := range rows {
		var l string
		if i >= top {
			l = "  " + lines[from+i-top]
			if at := times[from+i-top]; at != "" {
				l = "  " + drawFit(lines[from+i-top], tw) + "  " + at
			}
		}
		rows[i] = drawFit(l, width)
	}
	return strings.Join(rows, "\n"), maxScroll
}

// drawEntry renders one chat entry into styled lines of at most w cells: a prompt as typed (grey
// while queued), a reply as markdown, the CLI's own reply as laid out, all after a "›". No line is
// blank: every line of the chat says something.
func drawEntry(e ChatEntry, w int) []string {
	switch e.Kind {
	case UserEntry:
		st, arrow := styleYou, "› "
		switch {
		case e.Queued:
			st = styleDim
		case e.Cut:
			st, arrow = styleErr, "✗ "
		}
		lines := drawDense(drawWrap(e.Text, w-2))
		for i, l := range lines {
			prefix := "  "
			if i == 0 {
				prefix = arrow
			}
			lines[i] = st.Render(prefix + l)
		}
		return lines
	case AssistantEntry:
		if strings.TrimSpace(e.Text) == "" {
			return nil // a text block that has not streamed anything yet
		}
		lines := drawDense(drawMarkdown(e.Text, w-2))
		for i, l := range lines {
			gutter := "  "
			if i == 0 {
				gutter = styleReply.Render("› ")
			}
			lines[i] = gutter + l
		}
		return lines
	case CommandEntry:
		lines := drawDense(drawCommand(e.Text, w-2))
		for i, l := range lines {
			gutter := "  "
			if i == 0 {
				gutter = styleReply.Render("› ")
			}
			lines[i] = gutter + l
		}
		return lines
	case CaretEntry:
		return []string{" " + styleReply.Render(e.Text)} // under where claude's next words go
	}
	return nil
}

// drawDense drops the blank lines.
func drawDense(lines []string) []string {
	return slices.DeleteFunc(lines, func(l string) bool { return strings.TrimSpace(ansi.Strip(l)) == "" })
}

func drawStyled(lines []string, render func(...string) string) []string {
	for i, l := range lines {
		lines[i] = render(l)
	}
	return lines
}

// renderInput draws the prompt field, one line of exactly width cells. cursorX is the cursor column within it.
// A dimmed field does not have the keys.
func renderInput(in *Input, width int, placeholder string, dim bool) (out string, cursorX int) {
	if width <= 0 {
		return "", 0
	}
	prompt, text := styleYou.Render("› "), styleFg
	if dim {
		prompt, text = styleDim.Render("› "), styleDim
	}
	if len(in.Text) == 0 {
		return drawFit(prompt+styleDim.Render(placeholder), width), min(2, width-1)
	}
	disp := drawInputRunes(in.Text)
	cur := min(max(in.Cursor, 0), len(disp))
	start := drawScroll(disp, cur, width-2)
	shown := ansi.Truncate(string(disp[start:]), max(0, width-2), "")
	x := 2 + ansi.StringWidth(string(disp[start:cur]))
	return drawFit(prompt+text.Render(shown), width), min(x, width-1)
}

// drawInputRunes maps the field's runes one to one onto printable ones: '↵' for newlines, ' ' for other controls.
func drawInputRunes(text []rune) []rune {
	out := make([]rune, len(text))
	for i, r := range text {
		switch {
		case r == '\n':
			out[i] = '↵'
		case unicode.IsControl(r):
			out[i] = ' '
		default:
			out[i] = r
		}
	}
	return out
}

// drawScroll returns the first rune to show so that the text before cur and the caret cell fit in avail cells.
func drawScroll(r []rune, cur, avail int) int {
	caret := 1
	if cur < len(r) {
		caret = max(1, ansi.StringWidth(string(r[cur])))
	}
	// Walk back rune by rune, then correct for grapheme clusters, which StringWidth measures as a whole.
	start, w := cur, 0
	for start > 0 {
		w += ansi.StringWidth(string(r[start-1]))
		if w+caret > avail {
			break
		}
		start--
	}
	for start < cur && ansi.StringWidth(string(r[start:cur]))+caret > avail {
		start++
	}
	return start
}

// renderStatus draws the status line with the key hints that fit, one line of exactly width cells:
// the model, the mode, the limits, the context, then what changes size often — what claude does,
// the agents at work, the camera — so that the rest stays put.
func renderStatus(st Status, hints []string, width int) string {
	dot := styleLime.Render("●")
	switch {
	case st.State == "exited":
		dot = styleErr.Render("●")
	case st.Busy:
		dot = styleAccent.Render("●")
	}
	// The steady part, each with the turn it goes in when the screen is too narrow for it and
	// drawStatusMoving cells of the rest: what claude does says more. The mode never goes.
	type part struct {
		s    string
		drop int
	}
	var parts []part
	if st.Model != "" {
		parts = append(parts, part{styleFg.Render(drawOneLine(st.Model)), 1})
	}
	if mode := drawMode(st.Mode); mode != "" {
		parts = append(parts, part{mode, 0})
	}
	if st.Usage != "" {
		parts = append(parts, part{drawUsage(st.Usage), 3})
	}
	if st.Context > 0 {
		parts = append(parts, part{drawContext(st.Context), 2})
	}
	if st.Replay {
		parts = append(parts, part{styleFg.Render("replay"), 4})
	}
	var steady, moving []string
	for turn := 1; ; turn++ {
		steady = steady[:0]
		for _, p := range parts {
			steady = append(steady, p.s)
		}
		if turn > 4 || 2+ansi.StringWidth(strings.Join(steady, " · "))+3+drawStatusMoving <= width-4 {
			break
		}
		parts = slices.DeleteFunc(parts, func(p part) bool { return p.drop == turn })
	}
	spin := styleAccent.Render(drawSpinner[st.Frame%len(drawSpinner)]) + " "
	switch state := drawOneLine(st.State); {
	case st.Busy:
		s := spin + drawShimmer(state, st.Frame, theme.Fg)
		if st.Elapsed != "" {
			s += styleDim.Render(" " + st.Elapsed)
		}
		moving = append(moving, s)
	case state != "":
		moving = append(moving, styleFg.Render(state))
	}
	if st.Agents > 0 {
		agents := fmt.Sprintf("%d agents at work", st.Agents)
		if st.Agents == 1 {
			agents = "1 agent at work"
		}
		if st.Busy {
			moving = append(moving, styleFg.Render(agents))
		} else {
			moving = append(moving, spin+drawShimmer(agents, st.Frame, theme.Fg))
		}
	}
	if st.Camera != "" {
		moving = append(moving, drawCamera(st.Camera, st.CameraColor))
	}
	sep := styleDim.Render(" · ")
	join := func() string { return dot + " " + strings.Join(slices.Concat(steady, moving), sep) }

	// The hints that fit beside the steady part and drawStatusMoving cells of the rest: they do not
	// come and go as the rest changes size. The rest is cut to the room they leave.
	inner := width - 4
	budget := ansi.StringWidth(dot + " " + strings.Join(steady, sep))
	if len(moving) > 0 {
		budget += 3 + drawStatusMoving
	}
	for len(hints) > 0 && budget+2+ansi.StringWidth(strings.Join(hints, " · ")) > inner {
		hints = hints[1:]
	}
	room := inner
	if len(hints) > 0 {
		room -= 2 + ansi.StringWidth(strings.Join(hints, " · "))
	}
	// What does not fit goes whole, as a part cut short says nothing: what the camera's agent does,
	// then the camera, then the agents at work. What claude does stays, cut only as a last resort.
	line := join()
	if st.Camera != "" && ansi.StringWidth(line) > room {
		moving[len(moving)-1] = drawCamera(drawCameraOnly(st.Camera), st.CameraColor)
		line = join()
	}
	for len(moving) > 1 && ansi.StringWidth(line) > room {
		moving = moving[:len(moving)-1]
		line = join()
	}
	if ansi.StringWidth(line) > room {
		line = ansi.Truncate(line, max(0, room), "…")
	}
	if len(hints) > 0 {
		h := strings.Join(hints, " · ")
		line += strings.Repeat(" ", max(0, inner-ansi.StringWidth(line)-ansi.StringWidth(h))) + styleDim.Render(h)
	}
	return drawFit("  "+line, width)
}

// drawStatusMoving is the room the status line keeps for what changes size often.
const drawStatusMoving = 30

// drawUsage draws the use of each rate-limit window in Status.Usage ("5h 9% · 7d 58%"): the window
// grey, its use coloured by drawFull.
func drawUsage(usage string) string {
	parts := strings.Split(usage, " · ")
	for i, p := range parts {
		if j := strings.LastIndexByte(p, ' '); j >= 0 && strings.HasSuffix(p, "%") {
			pct, _ := strconv.Atoi(p[j+1 : len(p)-1])
			parts[i] = styleDim.Render(p[:j+1]) + drawFull(pct).Render(p[j+1:])
		} else {
			parts[i] = styleFg.Render(p)
		}
	}
	return strings.Join(parts, styleDim.Render(" · "))
}

// drawContext is how full main's context is, in few cells: "ctx" grey, the percentage coloured
// by drawFull.
func drawContext(f float64) string {
	pct := int(math.Round(min(max(f, 0), 1) * 100))
	return styleDim.Render("ctx ") + drawFull(pct).Render(fmt.Sprintf("%d%%", pct))
}

// drawFull is the colour of a share used up: as the auto mode from 80%, in Claude's own orange
// from 90%.
func drawFull(pct int) lipgloss.Style {
	switch {
	case pct >= 90:
		return styleAccent
	case pct >= 80:
		return styleModeAuto
	}
	return styleFg
}

// The colours of the permission modes in Claude Code's dark theme.
var (
	styleModePlan   = lipgloss.NewStyle().Foreground(theme.Teal)   // Claude Code's planMode
	styleModeAccept = lipgloss.NewStyle().Foreground(theme.Violet) // autoAccept
	styleModeAuto   = lipgloss.NewStyle().Foreground(theme.Yellow) // warning
	styleModeBypass = lipgloss.NewStyle().Foreground(theme.Red)    // error
)

// drawMode labels a permission mode as Claude Code does; the default mode shows nothing.
func drawMode(mode string) string {
	switch mode {
	case "", "default":
		return ""
	case "plan":
		return styleModePlan.Render("⏸ plan mode")
	case "acceptEdits":
		return styleModeAccept.Render("⏵⏵ accept edits")
	case "auto":
		return styleModeAuto.Render("⏵⏵ auto mode")
	case "bypassPermissions":
		return styleModeBypass.Render("⏵⏵ bypass permissions")
	case "dontAsk":
		return styleModeBypass.Render("⏵⏵ don't ask")
	}
	return styleAccent.Render("⏵⏵ " + drawOneLine(mode))
}

// drawCameraOnly is Status.Camera without what its agent does: "auto → tests".
func drawCameraOnly(cam string) string {
	if i := strings.LastIndex(cam, " ("); i >= 0 && strings.HasSuffix(cam, ")") {
		return cam[:i]
	}
	return cam
}

// drawCamera renders Status.Camera, e.g. "auto → tests (Edit calc.py)", with the agent's name in its colour.
func drawCamera(cam string, slot int) string {
	cam = drawOneLine(cam)
	var mode string
	for _, sep := range []string{"→ ", "· "} {
		if i := strings.Index(cam, sep); i >= 0 {
			mode, cam = cam[:i+len(sep)], cam[i+len(sep):]
			break
		}
	}
	name, activity := cam, ""
	if i := strings.Index(cam, " ("); i >= 0 && strings.HasSuffix(cam, ")") {
		name, activity = cam[:i], cam[i:]
	}
	return styleFg.Render("cam "+mode) + lipgloss.NewStyle().Foreground(drawAgentColor(slot)).Render(name) + styleDim.Render(activity)
}
