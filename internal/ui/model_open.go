package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/openspec"
	"github.com/pixelbender/claudit/internal/theme"
)

// /open finds the OpenSpec changes and specs in the copies of a specs repository under the
// explorer's root and opens one of their files in the editor, where it stays until alt+0 or esc. The
// copies are looked for off the UI's way; the list of the last search shows meanwhile.

// openScan looks for the copies; tests put in one of their own.
var openScan = openspec.Scan

// openGroup is a group of the rows of /open.
type openGroup int

const (
	openYours  openGroup = iota // the changes of a card's own copies
	openOthers                  // the other changes
	openSpecs                   // the main copy's specs
)

// openRow is a row of /open that can be chosen: a change of Index.Changes or a spec of Index.Specs.
type openRow struct {
	group openGroup
	at    int
}

// OpenPopup is /open: a field that finds changes and specs, the ones it finds, and the files of
// the change chosen.
type OpenPopup struct {
	Index   openspec.Index
	Loading bool // the copies are being looked for; Index is the last search's
	Filter  Input
	Now     time.Time // for the ages
	Cursor  int       // the highlighted row of the list
	Change  *openspec.Change
	File    int       // the highlighted file of Change
	shown   []openRow // what Filter finds, by group
}

func (*OpenPopup) isPopup() {}

// openScannedMsg brings what the search number gen of the folder root found.
type openScannedMsg struct {
	gen   int
	root  string
	index openspec.Index
}

// openSpecs runs /open: the list comes up, with what follows /open in its field, and the copies
// are looked for under the explorer's root.
func (m *Model) openSpecs(query string) tea.Cmd {
	root := m.Sim.State().Root
	text := []rune(query)
	p := &OpenPopup{Loading: true, Filter: Input{Text: text, Cursor: len(text)}, Now: m.clock()}
	if m.openIndex.Root == root {
		p.Index = m.openIndex
	}
	p.find()
	m.Popup = p
	return m.openSearch(root)
}

// openSearch looks for the copies under root, overtaking any search still running.
func (m *Model) openSearch(root string) tea.Cmd {
	m.openGen++
	gen := m.openGen
	return func() tea.Msg { return openScannedMsg{gen: gen, root: root, index: openScan(root, openspec.Default)} }
}

// openScanned keeps what a search found for the next /open and shows it, unless the list was
// closed meanwhile; the highlighted row stays on what it was on. A search that a later /open has
// overtaken is dropped, and so is one of a folder that is not the explorer's any more: the list,
// when open, looks in the explorer's folder now.
func (m *Model) openScanned(msg openScannedMsg) tea.Cmd {
	if msg.gen != m.openGen {
		return nil
	}
	if root := m.Sim.State().Root; msg.root != root {
		if _, ok := m.Popup.(*OpenPopup); ok {
			return m.openSearch(root)
		}
		return nil
	}
	m.openIndex = msg.index
	if p, ok := m.Popup.(*OpenPopup); ok {
		key := p.key()
		p.Index, p.Loading, p.Now = msg.index, false, m.clock()
		p.find()
		for i, r := range p.shown {
			if p.rowKey(r) == key {
				p.Cursor = i
			}
		}
	}
	return nil
}

// key names the highlighted row, so that a new search finds it again; "" for none.
func (p *OpenPopup) key() string {
	if p.Cursor < len(p.shown) {
		return p.rowKey(p.shown[p.Cursor])
	}
	return ""
}

func (p *OpenPopup) rowKey(r openRow) string {
	if r.group == openSpecs {
		return "spec " + p.Index.Specs[r.at].Capability
	}
	return "change " + p.Index.Changes[r.at].Name
}

// find shows the rows Filter finds: changes by name or tracker id, specs by capability.
func (p *OpenPopup) find() {
	q := string(p.Filter.Text)
	p.shown = p.shown[:0]
	for i := range p.Index.Changes {
		if c := &p.Index.Changes[i]; c.Matches(q) {
			g := openOthers
			if c.Own {
				g = openYours
			}
			p.shown = append(p.shown, openRow{group: g, at: i})
		}
	}
	for i := range p.Index.Specs {
		if p.Index.Specs[i].Matches(q) {
			p.shown = append(p.shown, openRow{group: openSpecs, at: i})
		}
	}
	p.Cursor = min(max(p.Cursor, 0), max(0, len(p.shown)-1))
}

// groups are the groups the list has, found or not: the ones the search found anything for.
func (p *OpenPopup) groups() []openGroup {
	var yours, others bool
	for _, c := range p.Index.Changes {
		yours, others = yours || c.Own, others || !c.Own
	}
	var gs []openGroup
	if yours {
		gs = append(gs, openYours)
	}
	if others {
		gs = append(gs, openOthers)
	}
	if len(p.Index.Specs) > 0 {
		gs = append(gs, openSpecs)
	}
	return gs
}

// nextGroup is the first row of the group after the highlighted one's that has rows, round to the
// first; -1 for none.
func (p *OpenPopup) nextGroup() int {
	if len(p.shown) == 0 {
		return -1
	}
	g := p.shown[p.Cursor].group
	for i := range p.shown {
		if r := p.shown[(p.Cursor+i)%len(p.shown)]; r.group != g {
			return (p.Cursor + i) % len(p.shown)
		}
	}
	return -1
}

// files are the paths of Change's rows: its deltas, then its Markdown files.
func (p *OpenPopup) files() []string {
	var out []string
	for _, d := range p.Change.Deltas {
		out = append(out, d.Path)
	}
	for _, d := range p.Change.Docs {
		out = append(out, d.Path)
	}
	return out
}

func (m *Model) openKey(p *OpenPopup, k tea.KeyPressMsg) tea.Cmd {
	key := k.String()
	if p.Change != nil {
		files := p.files()
		switch key {
		case "esc":
			p.Change = nil
		case "up":
			p.File = max(p.File-1, 0)
		case "down":
			p.File = min(p.File+1, max(0, len(files)-1))
		case "enter":
			if p.File < len(files) {
				m.openFile(files[p.File])
			}
		}
		return nil
	}
	last := max(0, len(p.shown)-1)
	switch key {
	case "esc":
		m.closeResume()
	case "up":
		p.Cursor = max(p.Cursor-1, 0)
	case "down":
		p.Cursor = min(p.Cursor+1, last)
	case "pgup":
		p.Cursor = max(p.Cursor-10, 0)
	case "pgdown":
		p.Cursor = min(p.Cursor+10, last)
	case "tab":
		if i := p.nextGroup(); i >= 0 {
			p.Cursor = i
		}
	case "enter":
		if len(p.shown) == 0 {
			return nil
		}
		switch r := p.shown[p.Cursor]; r.group {
		case openSpecs:
			m.openFile(p.Index.Specs[r.at].Path)
		default:
			c := p.Index.Changes[r.at]
			p.Change, p.File = &c, 0
		}
	default:
		was := string(p.Filter.Text)
		if p.Filter.edit(k) && string(p.Filter.Text) != was {
			p.Cursor = 0
			p.find()
		}
	}
	return nil
}

// openFile closes /open and shows the file at path in the editor until alt+0, unfolded in the
// explorer, as a click on it there does.
func (m *Model) openFile(path string) {
	m.closeResume()
	if !m.viewFile(path) {
		m.local("Error: /open: cannot read " + path)
	}
}

// The colours of what a delta does to requirements, by openspec.Op.
var openOpStyles = [...]lipgloss.Style{
	lipgloss.NewStyle().Foreground(theme.Green), lipgloss.NewStyle().Foreground(theme.Yellow),
	lipgloss.NewStyle().Foreground(theme.Red), lipgloss.NewStyle().Foreground(theme.Cyan),
}

// drawOpen draws /open w cells wide in at most rows lines, the last of them the hint: the list, or
// the files of the change chosen.
func drawOpen(p *OpenPopup, w, rows int) ([]string, []drawHit) {
	if p.Change != nil {
		return drawOpenFiles(p, w, rows)
	}
	head := styleAccent.Render("/open")
	switch n, s := len(p.Index.Changes), len(p.Index.Specs); {
	case p.Loading && n+s == 0:
		head += styleDim.Render("  looking for openspec folders…")
	default:
		head += styleDim.Render(fmt.Sprintf("  %d changes · %d main specs", n, s))
	}
	lines := []string{ansi.Truncate(head, w, "…")}
	if p.Index.Partial != "" {
		lines = append(lines, styleDim.Render(ansi.Truncate("the search "+p.Index.Partial+": some copies may be missing", w, "…")))
	}
	field := styleDim.Render("find ") + drawField(&p.Filter, max(1, w-5))
	if len(p.Filter.Text) == 0 {
		field = ansi.Truncate(field+styleDim.Render(" type a name or a card number"), w, "…")
	}
	all := p.groups()
	hint := "type to find · ↑/↓ choose · esc cancel"
	if len(p.shown) > 0 {
		enter := "enter files"
		if p.shown[p.Cursor].group == openSpecs {
			enter = "enter open"
		}
		hint = "type to find · ↑/↓ choose · " + enter
		if i := p.nextGroup(); i >= 0 {
			hint += " · tab " + openGroupName(p.shown[i].group, all)
		}
		hint += " · esc cancel"
	}
	hint = styleDim.Render(ansi.Truncate(hint, w, "…"))
	if len(all) == 0 {
		msg := "No openspec folder within " + fmt.Sprint(openspec.Default.Depth) + " levels of " + p.Index.Root
		if p.Loading {
			msg = "Looking in the folders of the explorer…"
		}
		return openFit(append(lines, "", field, "", styleDim.Render(ansi.Truncate(msg, w, "…")), "", hint), nil, rows, hint)
	}

	// Blank lines set the parts apart while there is room for them and a row of each group; on a
	// shorter screen they go, and then every group but the highlighted one's.
	groups := all
	gap := rows >= len(lines)+4+3*len(groups)
	if !gap && rows < len(lines)+2+2*len(groups) {
		groups = []openGroup{all[0]}
		if len(p.shown) > 0 {
			groups[0] = p.shown[p.Cursor].group
		}
	}
	tail := []string{hint}
	blanks := 0
	if gap {
		lines, tail, blanks = append(lines, "", field, ""), []string{"", hint}, len(groups)-1
	} else {
		lines = append(lines, field)
	}
	room := max(len(groups), rows-len(lines)-len(tail)-len(groups)-blanks) // for the rows and "more" of every group
	alloc := openAlloc(p, groups, room)
	nameW, copyW := 0, 0 // the names leave the copies room, up to openCopyW, unless that cuts them short
	for _, r := range p.shown {
		if r.group != openSpecs {
			nameW = max(nameW, ansi.StringWidth(drawOneLine(p.Index.Changes[r.at].Name)))
			copyW = max(copyW, ansi.StringWidth(drawOneLine(openWhere(&p.Index.Changes[r.at]))))
		}
	}
	nameW = min(nameW, max(openNameW, w-2-2-8-2-min(copyW, openCopyW)), max(1, w-2-2-8))
	var hits []drawHit
	for gi, g := range groups {
		if gap && gi > 0 {
			lines = append(lines, "")
		}
		var rows []int // indices into shown
		for i, r := range p.shown {
			if r.group == g {
				rows = append(rows, i)
			}
		}
		lines = append(lines, ansi.Truncate(openGroupHead(p, g, all, len(rows)), w, "…"))
		if len(rows) == 0 {
			none := "  No change matches"
			if g == openSpecs {
				none = "  No main spec matches"
			}
			lines = append(lines, styleDim.Render(none))
			continue
		}
		n := min(alloc[gi], len(rows))
		more := n < len(rows) && n > 1 // a line says how many more there are, when there are two lines to share
		if more {
			n--
		}
		top := 0
		if at := slices.Index(rows, p.Cursor); at >= 0 {
			top = min(max(at-n/2, 0), len(rows)-n)
		}
		for _, i := range rows[top : top+n] {
			hits = append(hits, drawHit{y: len(lines), x1: w, at: i})
			lines = append(lines, drawOpenRow(p, p.shown[i], i == p.Cursor, nameW, w))
		}
		if more {
			lines = append(lines, styleDim.Render(fmt.Sprintf("  … %d more", len(rows)-n)))
		}
	}
	return openFit(append(lines, tail...), hits, rows, hint)
}

// openFit cuts lines to rows, the last one always the hint, and the hits with them.
func openFit(lines []string, hits []drawHit, rows int, hint string) ([]string, []drawHit) {
	if rows < 1 || len(lines) <= rows {
		return lines, hits
	}
	return append(lines[:rows-1], hint), slices.DeleteFunc(hits, func(h drawHit) bool { return h.y >= rows-1 })
}

// openAlloc shares room lines among the groups, a line of "more" included: the one with the
// highlighted row takes what the others leave, and they keep up to five each, fewer on a short
// screen.
func openAlloc(p *OpenPopup, groups []openGroup, room int) []int {
	need := make([]int, len(groups))
	focus := 0
	for gi, g := range groups {
		for i, r := range p.shown {
			if r.group == g {
				need[gi]++
				if i == p.Cursor {
					focus = gi
				}
			}
		}
		need[gi] = max(need[gi], 1) // "No … matches"
	}
	share := min(5, max(1, room/(2*len(groups))))
	alloc := make([]int, len(groups))
	left := room
	for gi := range groups {
		if gi != focus {
			alloc[gi] = min(need[gi], share)
			left -= alloc[gi]
		}
	}
	alloc[focus] = max(1, min(need[focus], left))
	left -= alloc[focus]
	for gi := range groups { // what the highlighted group leaves goes to the others
		if extra := min(max(left, 0), need[gi]-alloc[gi]); extra > 0 {
			alloc[gi] += extra
			left -= extra
		}
	}
	return alloc
}

// openGroupName is what tab says it goes to.
func openGroupName(g openGroup, groups []openGroup) string {
	switch g {
	case openYours:
		return "yours"
	case openOthers:
		if len(groups) > 0 && groups[0] == openOthers {
			return "changes"
		}
		return "others"
	}
	return "main specs"
}

// openGroupHead is the header of group g, n rows found.
func openGroupHead(p *OpenPopup, g openGroup, groups []openGroup, n int) string {
	switch g {
	case openYours:
		return styleStrong.Render("Yours") + styleDim.Render(fmt.Sprintf("  newest edit first · %d", n))
	case openOthers:
		title := "Others"
		if groups[0] == openOthers { // no copy of a card's own: these are all
			title = "Changes"
		}
		return styleStrong.Render(title) + styleDim.Render(fmt.Sprintf("  by name · %d", n))
	}
	where := openRel(p.Index.Root, filepath.Join(p.Index.Main.Root, "openspec", "specs"))
	if b := p.Index.Main.Branch; b != "" { // which branch the specs are on
		where += " @ " + b
	}
	return styleStrong.Render("Main specs") + styleDim.Render(fmt.Sprintf("  %s · %d", where, n))
}

// drawOpenRow draws row r of the list: a change, its name in nameW cells, how long ago it was
// edited and the copy it comes from; or a spec's capability.
func drawOpenRow(p *OpenPopup, r openRow, sel bool, nameW, w int) string {
	mark, st := "  ", styleFg
	if sel {
		mark, st = styleAccent.Render("› "), styleAccent
	}
	if r.group == openSpecs {
		return mark + st.Render(drawFitTrunc(drawOneLine(p.Index.Specs[r.at].Capability), w-2))
	}
	c := &p.Index.Changes[r.at]
	row := mark + st.Render(drawFitTrunc(drawOneLine(c.Name), nameW)) + "  " + styleDim.Render(fmt.Sprintf("%8s", openAge(p.Now, c.Mod)))
	if room := w - 2 - nameW - 2 - 8 - 2; room >= 6 {
		row += "  " + styleDim.Render(ansi.Truncate(drawOneLine(openWhere(c)), room, "…"))
	}
	return ansi.Truncate(row, w, "…")
}

// The widths a row of the list gives a change's name at least, if it has that many cells, and its
// copy at most.
const openNameW, openCopyW = 24, 28

// openWhere is the copy c comes from, and how many more own copies have it.
func openWhere(c *openspec.Change) string {
	if c.More > 0 {
		return fmt.Sprintf("%s · %d more", c.Copy.Name, c.More)
	}
	return c.Copy.Name
}

// drawOpenFiles draws the files of the change chosen: its deltas with what they do to the
// requirements, then its Markdown files, scrolled to keep the highlighted one in sight.
func drawOpenFiles(p *OpenPopup, w, rows int) ([]string, []drawHit) {
	c := p.Change
	lines := []string{
		styleAccent.Render("/open") + styleDim.Render("  ‹ ") + styleFg.Render(ansi.Truncate(drawOneLine(c.Name), max(1, w-9), "…")),
		styleDim.Render(openEdited(p.Now, c, p.Index.Root, w)),
	}
	if c.More > 0 {
		lines = append(lines, styleDim.Render(ansi.Truncate(fmt.Sprintf("also in %d more of your copies: the newest edit is shown", c.More), w, "…")))
	}
	lines = append(lines, "")
	tail := []string{"", styleDim.Render(ansi.Truncate("↑/↓ choose · enter open and keep · esc back to the list", w, "…"))}
	var body []string
	var owners []int // the file of each line of body, -1 for none
	add := func(owner int, l string) { body, owners = append(body, l), append(owners, owner) }
	row := func(i int, label, tail string) string {
		mark, st := "  ", styleFg
		if i == p.File {
			mark, st = styleAccent.Render("› "), styleAccent
		}
		room := w - 2 - ansi.StringWidth(tail) - 2
		if room < 8 { // too narrow for the tail
			return mark + st.Render(ansi.Truncate(drawOneLine(label), w-2, "…"))
		}
		return mark + st.Render(drawFitTrunc(drawOneLine(label), room)) + "  " + tail
	}
	add(-1, styleStrong.Render("Spec deltas")+styleDim.Render(fmt.Sprintf("  %d", len(c.Deltas))))
	if len(c.Deltas) == 0 {
		add(-1, styleDim.Render("  No spec deltas"))
	}
	for i, d := range c.Deltas {
		var ops []string
		for op, n := range d.Ops {
			if n > 0 {
				ops = append(ops, openOpStyles[op].Render(fmt.Sprintf("%d %s", n, openspec.Ops[op])))
			}
		}
		add(i, row(i, d.Capability, strings.Join(ops, styleDim.Render(" · "))+styleDim.Render(fmt.Sprintf("   %d lines", d.Lines))))
	}
	add(-1, "")
	add(-1, styleStrong.Render("Change files"))
	if len(c.Docs) == 0 {
		add(-1, styleDim.Render("  No Markdown files"))
	}
	for i, d := range c.Docs {
		at := len(c.Deltas) + i
		add(at, row(at, d.Name, styleDim.Render(fmt.Sprintf("%d lines", d.Lines))))
	}
	room := max(1, rows-len(lines)-len(tail))
	top := 0
	if len(body) > room {
		top = min(max(slices.Index(owners, p.File)-room/2, 0), len(body)-room)
		body, owners = body[top:top+room], owners[top:top+room]
	}
	var hits []drawHit
	for i, o := range owners {
		if o >= 0 {
			hits = append(hits, drawHit{y: len(lines) + i, x1: w, at: o})
		}
	}
	return append(append(lines, body...), tail...), hits
}

// openEdited says when c was edited last and where it is, w cells wide: a path too long loses its
// head, which the copies share.
func openEdited(now time.Time, c *openspec.Change, root string, w int) string {
	head, where := "", drawOneLine(openRel(root, c.Dir))
	if !c.Mod.IsZero() {
		head = "edited " + openAge(now, c.Mod) + " · "
	}
	if over := ansi.StringWidth(head+where) - w; over > 0 {
		where = ansi.TruncateLeft(where, min(over+1, ansi.StringWidth(where)), "…")
	}
	return ansi.Truncate(head+where, w, "…")
}

// openAge is how long ago t was, as of now; "" for no time.
func openAge(now, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return drawAge(now.Sub(t))
}

// openRel is path as the explorer's root sees it, with "/".
func openRel(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}
