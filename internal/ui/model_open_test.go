package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/ide"
	"github.com/pixelbender/claudit/internal/ideview"
	"github.com/pixelbender/claudit/internal/openspec"
	"github.com/pixelbender/claudit/internal/protocol"
)

// openLong is a change whose name does not fit in 80 columns.
const openLong = "dev-125798-encoder-rejects-a-codec-it-cannot-carry-to-the-recording"

// openWrite puts text in root/rel, changed ago before modelT0.
func openWrite(t *testing.T, root, rel, text string, ago time.Duration) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	at := modelT0.Add(-ago)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
	return path
}

// modelOpenNew is a model whose explorer shows copies of a specs repository: two of cards' own,
// one of the main branch with a change and two specs.
func modelOpenNew(t *testing.T, w, h int) (*Model, string) {
	t.Helper()
	m, _ := modelNew(t, false)
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	root := m.Sim.State().Root
	main := "workspace-specs/openspec/"
	openWrite(t, root, main+"specs/chats/messages/spec.md", "# Messages\n", 48*time.Hour)
	openWrite(t, root, main+"specs/webinar/record-conversion/spec.md", "# Record\n", 48*time.Hour)
	openWrite(t, root, main+"changes/chats-17616-system-messages/proposal.md", "Tracker: CHATS-17616\n", 72*time.Hour)
	own := "_wt/specs-DEV-119816/openspec/changes/dev-119816-chat-send-time/"
	openWrite(t, root, own+"proposal.md", "# Proposal\n\nTracker: DEV-119816\n", 10*time.Hour)
	openWrite(t, root, own+"design.md", "a\nb\nc\n", 10*time.Hour)
	openWrite(t, root, own+"tasks.md", "- [ ] one\n", 10*time.Hour)
	openWrite(t, root, own+"specs/webinar/record-conversion/spec.md",
		"## ADDED Requirements\n### Requirement: A\n### Requirement: B\n## MODIFIED Requirements\n### Requirement: C\n", 10*time.Hour)
	openWrite(t, root, "_wt/specs-DEV-125798/openspec/changes/"+openLong+"/proposal.md", "Tracker: DEV-125798\n", 2*time.Minute)
	openWrite(t, root, "_wt/specs-DEV-555/openspec/changes/odd-name/proposal.md", "Tracker: DEV-555\n", 50*time.Hour)
	return m, root
}

// modelOpenRun runs /open with query: the list comes up, then the search fills it.
func modelOpenRun(t *testing.T, m *Model, query string) *OpenPopup {
	t.Helper()
	cmd := modelLocalSay(m, strings.TrimSpace("/open "+query))
	p, ok := m.Popup.(*OpenPopup)
	if !ok || !p.Loading || cmd == nil {
		t.Fatalf("popup %#v", m.Popup)
	}
	modelResumeRun(m, cmd)
	if p.Loading {
		t.Fatal("still looking")
	}
	return p
}

// modelOpenTicks ticks m from modelT0+from to modelT0+to, a frame each 50ms.
func modelOpenTicks(m *Model, from, to time.Duration) {
	for d := from; d <= to; d += 50 * time.Millisecond {
		m.Update(tickMsg(modelT0.Add(d)))
	}
}

// modelOpenScreen draws m and strips the colours.
func modelOpenScreen(m *Model) string {
	m.Now = m.clock()
	return ansi.Strip(m.View().Content)
}

// /open lists the changes of the cards' own copies, newest edit first, with how long ago and where;
// then the others by name and the main specs.
func TestModelOpenList(t *testing.T) {
	m, _ := modelOpenNew(t, 160, 50)
	modelOpenRun(t, m, "")
	out := modelOpenScreen(m)
	order := []string{"/open  4 changes · 2 main specs", "find", "type a name or a card number",
		"Yours  newest edit first · 3", "› dev-125798-encoder-rejects-a-codec-it-cannot-carry-to-the-recordi…", "2m ago", "specs-DEV-125798",
		"dev-119816-chat-send-time", "10h ago", "specs-DEV-119816", "odd-name", "2d ago", "specs-DEV-555",
		"Others  by name · 1", "chats-17616-system-messages", "3d ago", "workspace-specs",
		"Main specs  workspace-specs/openspec/specs · 2", "chats/messages", "webinar/record-conversion",
		"type to find · ↑/↓ choose · enter files · tab others · esc cancel"}
	at := 0
	for _, s := range order {
		i := strings.Index(out[at:], s)
		if i < 0 {
			t.Fatalf("%q missing or out of order after %d:\n%s", s, at, out)
		}
		at += i + len(s)
	}
}

// What is typed finds the changes by name or by the card their proposal tracks, and the specs by
// capability; the copy a change is in finds nothing.
func TestModelOpenFilter(t *testing.T) {
	m, _ := modelOpenNew(t, 160, 50)
	p := modelOpenRun(t, m, "119816")
	if string(p.Filter.Text) != "119816" || len(p.shown) != 1 || p.Index.Changes[p.shown[0].at].Name != "dev-119816-chat-send-time" {
		t.Fatalf("/open 119816: %+v", p.shown)
	}
	if out := modelOpenScreen(m); !strings.Contains(out, "No change matches") || !strings.Contains(out, "No main spec matches") {
		t.Errorf("empty groups say nothing:\n%s", out)
	}
	modelPress(m, inputKey("ctrl+u"))
	modelPress(m, inputTyped("555")...)
	if len(p.shown) != 1 || p.Index.Changes[p.shown[0].at].Name != "odd-name" {
		t.Errorf("by tracker: %+v", p.shown)
	}
	modelPress(m, inputKey("ctrl+u"))
	modelPress(m, inputTyped("WEBINAR/")...)
	if len(p.shown) != 2 || p.shown[0].group != openYours || p.shown[1].group != openSpecs {
		t.Errorf("by capability: %+v", p.shown) // the spec, and the change that has a delta of it
	}
	modelPress(m, inputKey("ctrl+u"))
	modelPress(m, inputTyped("record-conv")...)
	if len(p.shown) != 2 || p.Index.Changes[p.shown[0].at].Name != "dev-119816-chat-send-time" || p.shown[1].group != openSpecs {
		t.Errorf("by a delta's capability: %+v", p.shown)
	}
	modelPress(m, inputKey("ctrl+u"))
	modelPress(m, inputTyped("workspace")...)
	if len(p.shown) != 0 {
		t.Errorf("the copy's name finds %+v", p.shown)
	}
	modelPress(m, inputKey("backspace"))
	if string(p.Filter.Text) != "workspac" {
		t.Errorf("backspace: %q", string(p.Filter.Text))
	}
}

// enter on a change lists its deltas, with what they do to requirements, and its files; esc goes
// back to the list, and esc again closes it. A file chosen opens and stays on screen.
func TestModelOpenFiles(t *testing.T) {
	m, root := modelOpenNew(t, 160, 50)
	p := modelOpenRun(t, m, "chat-send")
	modelPress(m, inputKey("enter"))
	if p.Change == nil || p.Change.Name != "dev-119816-chat-send-time" {
		t.Fatalf("change %+v", p.Change)
	}
	out := modelOpenScreen(m)
	for _, s := range []string{"/open  ‹ dev-119816-chat-send-time", "edited 10h ago · _wt/specs-DEV-119816/openspec/changes/dev-119816-chat-send-time",
		"Spec deltas  1", "› webinar/record-conversion", "2 ADDED · 1 MODIFIED   5 lines", "Change files",
		"proposal.md", "3 lines", "design.md", "tasks.md", "↑/↓ choose · enter open and keep · esc back to the list"} {
		if !strings.Contains(out, s) {
			t.Fatalf("%q missing:\n%s", s, out)
		}
	}
	modelPress(m, inputKey("esc"))
	if p.Change != nil || m.Popup != p || string(p.Filter.Text) != "chat-send" {
		t.Fatal("esc did not go back to the list")
	}
	modelPress(m, inputKey("enter"), inputKey("down"), inputKey("down"), inputKey("down"), inputKey("down"), inputKey("up"))
	if p.File != 2 {
		t.Fatalf("file %d", p.File)
	}
	modelPress(m, inputKey("enter"))
	design := filepath.Join(root, "_wt/specs-DEV-119816/openspec/changes/dev-119816-chat-send-time/design.md")
	st := m.Sim.State()
	if m.Popup != nil || st.Viewed != design || !slices.ContainsFunc(st.Tree, func(r ide.TreeRow) bool { return r.Path == design }) {
		t.Fatalf("popup %v viewed %q", m.Popup, st.Viewed)
	}

	modelOpenRun(t, m, "")
	modelPress(m, inputKey("esc"))
	if m.Popup != nil {
		t.Error("esc did not close the list")
	}
}

// tab goes to the next group with rows, round to the first; enter on a main spec opens it.
func TestModelOpenTab(t *testing.T) {
	m, root := modelOpenNew(t, 160, 50)
	p := modelOpenRun(t, m, "")
	var groups []openGroup
	for range 4 {
		groups = append(groups, p.shown[p.Cursor].group)
		modelPress(m, inputKey("tab"))
	}
	if want := []openGroup{openYours, openOthers, openSpecs, openYours}; !slices.Equal(groups, want) {
		t.Fatalf("tab went %v", groups)
	}
	if !strings.Contains(modelOpenScreen(m), "enter files · tab main specs") {
		t.Error("the hint does not name where tab goes")
	}
	modelPress(m, inputKey("tab"))
	if out := modelOpenScreen(m); !strings.Contains(out, "enter open · tab yours") {
		t.Errorf("hint on a spec:\n%s", out)
	}
	modelPress(m, inputKey("down"), inputKey("enter"))
	if want := filepath.Join(root, "workspace-specs/openspec/specs/webinar/record-conversion/spec.md"); m.Popup != nil || m.Sim.State().Viewed != want {
		t.Errorf("viewed %q", m.Sim.State().Viewed)
	}
}

// Rows the list has no room for are counted, and the highlighted one stays in sight.
func TestModelOpenMore(t *testing.T) {
	m, root := modelOpenNew(t, 120, 24)
	for i := range 12 {
		openWrite(t, root, "workspace-specs/openspec/changes/zz-change-"+string(rune('a'+i))+"/proposal.md", "x\n", time.Hour)
	}
	p := modelOpenRun(t, m, "")
	out := modelOpenScreen(m)
	if !strings.Contains(out, "more") || strings.Contains(out, "zz-change-l") || !strings.Contains(out, "dev-119816-chat-send-time") || !strings.Contains(out, "odd-name") {
		t.Fatalf("the highlighted group does not show all its rows, or the others all theirs:\n%s", out)
	}
	modelPress(m, inputKey("tab"), inputKey("pgdown"))
	if out := modelOpenScreen(m); !strings.Contains(out, "› zz-change-j") || !strings.Contains(out, "more") {
		t.Fatalf("the highlighted row is out of sight (%d):\n%s", p.Cursor, out)
	}
	modelPress(m, inputKey("pgup"))
	if p.shown[p.Cursor].group != openOthers {
		t.Errorf("pgup went to %v", p.shown[p.Cursor].group)
	}

	// The file opened far down the explorer is scrolled into sight there.
	st := m.Sim.State()
	st.SetRoot(root) // the copies above it are listed, unfolded
	for _, c := range p.Index.Changes {
		st.Reveal(filepath.Join(c.Dir, "proposal.md"))
	}
	modelPress(m, inputKey("ctrl+u"))
	modelPress(m, inputTyped("zz-change-l")...)
	modelPress(m, inputKey("enter"), inputKey("enter"))
	m.inspection = nil
	inSight := false
	for _, l := range strings.Split(modelOpenScreen(m), "\n") {
		r := []rune(l)
		inSight = inSight || len(r) > 30 && strings.Contains(string(r[:30]), "proposal.md")
	}
	if !inSight || m.Sim.State().Viewed == "" {
		t.Errorf("the file is out of sight in the explorer:\n%s", modelOpenScreen(m))
	}
}

// In 80 columns the list and the files keep their frame; names too long end in "…".
func TestModelOpen80Columns(t *testing.T) {
	for _, h := range []int{24, 40} {
		m, _ := modelOpenNew(t, 80, h)
		p := modelOpenRun(t, m, "")
		check := func(what string) {
			t.Helper()
			lines := strings.Split(modelOpenScreen(m), "\n")
			top := slices.IndexFunc(lines, func(l string) bool { r := []rune(l); return len(r) > 2 && r[2] == '╭' })
			if top < 0 {
				t.Fatalf("%s at %d rows: no box:\n%s", what, h, strings.Join(lines, "\n"))
			}
			right := 3 + slices.Index([]rune(lines[top])[3:], '╮')
			for i, l := range lines[top:] {
				r := []rune(l)
				if ansi.StringWidth(l) != 80 || len(r) != 80 {
					t.Fatalf("%s: line %d is %d wide", what, i, ansi.StringWidth(l))
				}
				if i > 0 && r[2] == '╰' && r[right] == '╯' {
					return
				}
				if i > 0 && (r[2] != '│' || r[right] != '│') {
					t.Fatalf("%s at %d rows: the frame breaks at line %d:\n%s", what, h, i, strings.Join(lines, "\n"))
				}
			}
			t.Fatalf("%s: the box has no bottom", what)
		}
		check("list")
		if out := modelOpenScreen(m); !strings.Contains(out, "› dev-125798-encoder-rejects-a-codec-it-can…    2m ago  specs-DEV-125798 │") {
			t.Errorf("the long name is not cut:\n%s", out)
		}
		modelPress(m, inputKey("enter"))
		if p.Change == nil {
			t.Fatal("no files")
		}
		check("files")
	}
}

// A file opened with /open or clicked in the explorer stays on screen while the agent works on:
// the editor's title and the status line say how to go back. alt+0 goes back to following the
// action, alt+1 to the agent; esc goes back too, and only the next esc interrupts claude.
func TestModelOpenHeld(t *testing.T) {
	m, root := modelOpenNew(t, 160, 50)
	modelOpenRun(t, m, "119816")
	modelPress(m, inputKey("enter"), inputKey("enter"))
	spec := filepath.Join(root, "_wt/specs-DEV-119816/openspec/changes/dev-119816-chat-send-time/specs/webinar/record-conversion/spec.md")
	other := openWrite(t, root, "notes.txt", "notes\n", 0)
	modelEvents(m, protocol.ToolUse{ID: "r1", Name: "Read", Input: map[string]any{"file_path": other}},
		protocol.ToolResult{ID: "r1", Output: "notes"})
	modelOpenTicks(m, 0, 10*time.Second) // past the cooldown of the keys
	st := m.Sim.State()
	if st.Viewed != spec || m.Sim.FindAgent("main").File != other {
		t.Fatalf("viewed %q, main at %q", st.Viewed, m.Sim.FindAgent("main").File)
	}
	out := modelOpenScreen(m)
	if !strings.Contains(out, "record-conversion/spec.md opened by you · alt+0 back to agent") || !strings.Contains(out, "shift+tab mode · alt+0/esc back to agent") || strings.Contains(out, "esc interrupt") {
		t.Fatalf("no way back on screen:\n%s", out)
	}
	modelPress(m, inputKey("alt+0"))
	if out := modelOpenScreen(m); st.Viewed != "" || strings.Contains(out, "opened by you") || !strings.Contains(out, "F1 help") {
		t.Fatalf("alt+0: viewed %q\n%s", st.Viewed, out)
	}

	modelOpenRun(t, m, "119816")
	modelPress(m, inputKey("enter"), inputKey("enter"))
	var sent []protocol.Outbound
	m.send = func(o protocol.Outbound) { sent = append(sent, o) }
	m.Status.Busy, m.Status.State = true, "running"
	modelPress(m, inputKey("esc"))
	if st.Viewed != "" || len(sent) != 0 || m.Status.State != "running" {
		t.Fatalf("the first esc: viewed %q, sent %v, state %q", st.Viewed, sent, m.Status.State)
	}
	modelPress(m, inputKey("esc"))
	if len(sent) != 1 || m.Status.State != "interrupting" {
		t.Fatalf("the second esc: sent %v, state %q", sent, m.Status.State)
	}
	m.Status.Busy, m.Status.State = false, "idle"

	modelOpenRun(t, m, "119816")
	modelPress(m, inputKey("enter"), inputKey("enter"))
	modelPress(m, inputKey("alt+1"))
	if st.Viewed != "" || !st.Pinned || st.Camera != "main" {
		t.Fatalf("alt+1: viewed %q pinned %v", st.Viewed, st.Pinned)
	}

	m, root = modelOpenNew(t, 160, 50)
	clicked := openWrite(t, root, "zz-clicked.txt", "clicked\n", 0)
	m.Sim.State().SetRoot(root) // the explorer lists it
	modelMouseClick(t, m, "zz-clicked.txt")
	modelEvents(m, protocol.ToolUse{ID: "b1", Name: "Bash", Input: map[string]any{"command": "ls"}}, protocol.ToolResult{ID: "b1", Output: "x"})
	modelOpenTicks(m, 0, 20*time.Second)
	if st := m.Sim.State(); st.Viewed != clicked || m.Sim.Busy() {
		t.Errorf("the agent's call let the clicked file go: viewed %q", st.Viewed)
	}
}

// While the copies are looked for, the list says so, or shows the last search's; a search that
// stopped short says so; with no copy, the list says where it looked.
func TestModelOpenStates(t *testing.T) {
	m, root := modelOpenNew(t, 120, 40)
	modelLocalSay(m, "/open")
	if out := modelOpenScreen(m); !strings.Contains(out, "looking for openspec folders") {
		t.Errorf("looking:\n%s", out)
	}
	modelPress(m, inputKey("esc"))
	modelOpenRun(t, m, "")
	modelPress(m, inputKey("esc"))

	scan := openScan
	t.Cleanup(func() { openScan = scan })
	openScan = func(root string, _ openspec.Limits) openspec.Index {
		return openspec.Index{Root: root, Partial: "stopped after 5s"}
	}
	cmd := modelLocalSay(m, "/open")
	if out := modelOpenScreen(m); !strings.Contains(out, "4 changes · 2 main specs") {
		t.Errorf("the last search is not shown meanwhile:\n%s", out)
	}
	modelResumeRun(m, cmd)
	out := modelOpenScreen(m)
	if !strings.Contains(out, "the search stopped after 5s: some copies may be missing") || !strings.Contains(out, "No openspec folder within 3 levels of "+root[:20]) {
		t.Errorf("stopped short:\n%s", out)
	}
	modelPress(m, inputKey("enter"), inputKey("tab"), inputKey("down"))
	modelPress(m, inputKey("esc"))

	// The list closed before the search came back: it is kept for the next one only.
	cmd = modelLocalSay(m, "/open")
	modelPress(m, inputKey("esc"))
	modelResumeRun(m, cmd)
	if m.Popup != nil || m.openIndex.Partial != "stopped after 5s" {
		t.Errorf("popup %v", m.Popup)
	}
}

// A click on a row of the list chooses it as enter does; the wheel moves the highlight.
func TestModelOpenMouse(t *testing.T) {
	m, _ := modelOpenNew(t, 160, 50)
	p := modelOpenRun(t, m, "")
	m.Update(tea.MouseWheelMsg{X: 80, Y: 25, Button: tea.MouseWheelDown})
	if p.Cursor != 1 {
		t.Errorf("wheel: cursor %d", p.Cursor)
	}
	modelMouseClick(t, m, "chats-17616-system-messages")
	if p.Change == nil || p.Change.Name != "chats-17616-system-messages" {
		t.Fatalf("click: change %+v", p.Change)
	}
	m.Update(tea.MouseWheelMsg{X: 80, Y: 25, Button: tea.MouseWheelDown})
	if p.File != 0 { // one file: proposal.md
		t.Errorf("wheel in the files: %d", p.File)
	}
	modelMouseClick(t, m, "proposal.md")
	if m.Popup != nil || !strings.HasSuffix(m.Sim.State().Viewed, "chats-17616-system-messages/proposal.md") {
		t.Errorf("click on a file: viewed %q", m.Sim.State().Viewed)
	}
}

// /open and its key are in the help and the commands.
func TestModelOpenHelp(t *testing.T) {
	m, _ := modelOpenNew(t, 160, 50)
	modelPress(m, inputKey("f1"))
	out := modelOpenScreen(m)
	for _, s := range []string{"/open", "also back from a file you opened", "until alt+0"} {
		if !strings.Contains(out, s) {
			t.Errorf("%q not in the help:\n%s", s, out)
		}
	}
	if !slices.ContainsFunc(localCommands, func(c protocol.Command) bool { return c.Name == "open" }) {
		t.Error("/open is not a command")
	}
}

// With no copy of a card's own, the changes are all in one group; a change from outside the
// explorer's folder shows where it is in full; a long list of files scrolls; a file gone since the
// search says so.
func TestModelOpenEdges(t *testing.T) {
	m, root := modelOpenNew(t, 100, 24)
	elsewhere := filepath.Join(t.TempDir(), "x")
	var deltas []openspec.Delta
	for i := range 30 {
		deltas = append(deltas, openspec.Delta{Capability: "domain/cap-" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Path: filepath.Join(elsewhere, "gone.md"), Ops: [4]int{0, 0, 1, 2}})
	}
	scan := openScan
	t.Cleanup(func() { openScan = scan })
	openScan = func(string, openspec.Limits) openspec.Index {
		return openspec.Index{Root: root, Copies: 1, Changes: []openspec.Change{
			{Name: "far-change", Dir: elsewhere, Copy: openspec.Copy{Name: "x"}, Deltas: deltas},
			{Name: "empty-change", Dir: filepath.Join(root, "y"), Copy: openspec.Copy{Name: "y"}},
		}}
	}
	p := modelOpenRun(t, m, "")
	out := modelOpenScreen(m)
	if !strings.Contains(out, "Changes  by name · 2") || !strings.Contains(out, "enter files · esc cancel") {
		t.Errorf("one group:\n%s", out)
	}
	modelPress(m, inputKey("down"), inputKey("enter"))
	if out := modelOpenScreen(m); !strings.Contains(out, "No spec deltas") || !strings.Contains(out, "No Markdown files") || !strings.Contains(out, "‹ empty-change\n") && !strings.Contains(out, "│ y ") {
		t.Errorf("empty change:\n%s", out)
	}
	modelPress(m, inputKey("enter"), inputKey("esc"), inputKey("up"), inputKey("enter"))
	if p.Change == nil || p.Change.Name != "far-change" {
		t.Fatalf("change %+v", p.Change)
	}
	if out := modelOpenScreen(m); !strings.Contains(out, "│ "+filepath.ToSlash(elsewhere)+" ") || !strings.Contains(out, "1 REMOVED · 2 RENAMED") {
		t.Errorf("far change:\n%s", out)
	}
	for range 25 {
		modelPress(m, inputKey("down"))
	}
	if out := modelOpenScreen(m); !strings.Contains(out, "› domain/cap-za") || strings.Contains(out, "domain/cap-aa ") {
		t.Errorf("not scrolled to the highlighted file:\n%s", out)
	}
	modelPress(m, inputKey("enter"))
	if m.Popup != nil || m.Sim.State().Viewed != "" || !modelResumeSaid(m, "Error: /open: cannot read") {
		t.Errorf("a file gone: popup %v viewed %q", m.Popup, m.Sim.State().Viewed)
	}
}

// A change in several copies of a card's own is shown from the one edited last, and says how many
// more there are.
func TestModelOpenMoreCopies(t *testing.T) {
	m, root := modelOpenNew(t, 160, 50)
	openWrite(t, root, "_wt/specs-DEV-119816-retry/openspec/changes/dev-119816-chat-send-time/proposal.md", "Tracker: DEV-119816\n", time.Hour)
	p := modelOpenRun(t, m, "119816")
	if out := modelOpenScreen(m); !strings.Contains(out, "1h ago  specs-DEV-119816-retry · 1 more") {
		t.Errorf("list:\n%s", out)
	}
	modelPress(m, inputKey("enter"))
	if out := modelOpenScreen(m); p.Change == nil || !strings.Contains(out, "also in 1 more of your copies: the newest edit is shown") {
		t.Errorf("files:\n%s", out)
	}
}

// Where a change is loses the head of its path, which the copies share, when it does not fit.
func TestOpenEdited(t *testing.T) {
	c := &openspec.Change{Dir: filepath.FromSlash("/r/_wt/specs-DEV-1/openspec/changes/dev-1-x"), Mod: modelT0.Add(-time.Hour)}
	root := filepath.FromSlash("/r")
	for w, want := range map[int]string{
		80: "edited 1h ago · _wt/specs-DEV-1/openspec/changes/dev-1-x",
		30: "edited 1h ago · …anges/dev-1-x",
		16: "edited 1h ago · ",
	} {
		if got := openEdited(modelT0, c, root, w); got != want || ansi.StringWidth(got) > w {
			t.Errorf("%d cells: %q, want %q", w, got, want)
		}
	}
}

// openExplorerSees reports whether the first 30 columns of the screen of m show text: the explorer.
func openExplorerSees(m *Model, text string) bool {
	for _, l := range strings.Split(modelOpenScreen(m), "\n") {
		if r := []rune(l); len(r) > 30 && strings.Contains(string(r[:30]), text) {
			return true
		}
	}
	return false
}

// The explorer keeps the row of the file held in sight while an agent works on in a file far from
// it; going back to the agent takes the explorer to the agent's file.
func TestModelOpenExplorerFollowsHeld(t *testing.T) {
	m, root := modelOpenNew(t, 160, 50)
	for i := range 60 {
		openWrite(t, root, fmt.Sprintf("zz-files/f%02d.txt", i), "x\n", 0)
	}
	last := openWrite(t, root, "zz-files/f99.txt", "x\n", 0)
	st := m.Sim.State()
	st.SetRoot(root)
	modelOpenRun(t, m, "119816")
	modelPress(m, inputKey("enter"), inputKey("enter"))
	modelEvents(m, protocol.ToolUse{ID: "r1", Name: "Read", Input: map[string]any{"file_path": last}}, protocol.ToolResult{ID: "r1", Output: "x"})
	modelOpenTicks(m, 0, 10*time.Second)
	m.inspection = nil
	if st.Viewed == "" || !openExplorerSees(m, "proposal.md") {
		t.Fatalf("the held file is out of sight in the explorer:\n%s", modelOpenScreen(m))
	}
	modelPress(m, inputKey("alt+0"))
	m.inspection = nil
	if openExplorerSees(m, "proposal.md") || !openExplorerSees(m, "f99.txt") {
		t.Errorf("the explorer did not go to the agent's file:\n%s", modelOpenScreen(m))
	}
}

// Once the file the user opened lets go of the right column, by alt+0 or as it is gone from disk,
// the explorer follows the agent from where the agent is, not as far from it as it was scrolled
// from the file's row.
func TestModelOpenLetGoScroll(t *testing.T) {
	m, root := modelOpenNew(t, 160, 50)
	path := openWrite(t, root, "held.md", "x\n", 0)
	m.Sim.State().SetRoot(root)
	m.Sim.Agent("main", ide.Main)
	for i, letGo := range []func(){
		func() { modelPress(m, inputKey("alt+0")) },
		func() { modelPress(m, inputKey("esc")) },
		func() { modelPress(m, inputKey("alt+1")) },
		func() {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			modelOpenTicks(m, 0, 2*time.Second)
		},
	} {
		if !m.Sim.View(path) {
			t.Fatal("the file did not open")
		}
		m.scroll[ideview.ExplorerPane] = 8
		modelOpenTicks(m, 0, 0)
		if m.scroll[ideview.ExplorerPane] != 8 {
			t.Fatalf("%d: the scroll went while the file is held", i)
		}
		letGo()
		if _, kept := m.scroll[ideview.ExplorerPane]; kept || m.Sim.State().Viewed != "" {
			t.Errorf("%d: viewed %q, the explorer kept its scroll", i, m.Sim.State().Viewed)
		}
	}
	m.scroll[ideview.ExplorerPane] = 5 // the wheel, with no file held
	if modelOpenTicks(m, 3*time.Second, 4*time.Second); m.scroll[ideview.ExplorerPane] != 5 {
		t.Error("ticks undid the user's scroll of the explorer")
	}
}

// While the IDE the user is reading still shows the file they opened, its explorer keeps the scroll
// from the file's row; it goes once that IDE goes.
func TestModelOpenLetGoAfterInspection(t *testing.T) {
	m, root := modelOpenNew(t, 160, 50)
	path := openWrite(t, root, "held.md", "x\n", 0)
	m.Sim.State().SetRoot(root)
	if !m.Sim.View(path) {
		t.Fatal("the file did not open")
	}
	m.scroll[ideview.ExplorerPane] = 8
	m.inspection = &inspection{state: copyIDE(m.Sim.State()), until: modelT0.Add(time.Hour)}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if modelOpenTicks(m, 0, 2*time.Second); m.Sim.State().Viewed != "" || m.scroll[ideview.ExplorerPane] != 8 {
		t.Fatalf("viewed %q, scroll %d under the IDE the user reads", m.Sim.State().Viewed, m.scroll[ideview.ExplorerPane])
	}
	m.inspection.until = modelT0
	modelOpenTicks(m, 3*time.Second, 3*time.Second)
	if _, kept := m.scroll[ideview.ExplorerPane]; kept || m.inspection != nil {
		t.Error("the scroll from the file's row outlived the IDE the user read")
	}
}

// Folding the folder of the file the user opened keeps the explorer's top row where it was: the
// folder stays under the pointer.
func TestModelOpenFoldKeepsTop(t *testing.T) {
	m, root := modelOpenNew(t, 160, 50)
	for i := range 60 {
		openWrite(t, root, fmt.Sprintf("a%02d/x.txt", i), "x\n", 0)
		openWrite(t, root, fmt.Sprintf("c%02d/x.txt", i), "x\n", 0)
	}
	path := openWrite(t, root, "b/z.md", "z\n", 0)
	m.Sim.State().SetRoot(root)
	m.View() // the IDE's height
	if !m.viewFile(path) {
		t.Fatal("the file did not open")
	}
	m.inspection = nil
	top := ideview.ExplorerTop(m.ideState(), ideview.Options{Scroll: m.scroll}, m.ideHeight)
	if top == 0 {
		t.Fatalf("the tree fits: nothing to keep\n%s", modelOpenScreen(m))
	}
	modelMouseClick(t, m, "▾ b")
	m.inspection = nil
	if got := ideview.ExplorerTop(m.ideState(), ideview.Options{Scroll: m.scroll}, m.ideHeight); got != top || !openExplorerSees(m, "▸ b") {
		t.Errorf("top %d, was %d:\n%s", got, top, modelOpenScreen(m))
	}
}

// An agent that turns out a duplicate hands the camera pinned on it to the agent it stands for;
// the file the user opened stays, and so does the explorer's scroll.
func TestModelOpenDropAgentKeepsView(t *testing.T) {
	m, root := modelOpenNew(t, 160, 50)
	path := openWrite(t, root, "held.md", "x\n", 0)
	m.Sim.Agent("dup", ide.Subagent)
	m.Sim.Agent("real", ide.Subagent)
	m.Sim.Pin("dup")
	if !m.Sim.View(path) {
		t.Fatal("the file did not open")
	}
	m.scroll[ideview.ExplorerPane] = 8
	m.dropAgent("dup", "real")
	if st := m.Sim.State(); st.Viewed != path || st.Camera != "real" || !st.Pinned || m.scroll[ideview.ExplorerPane] != 8 {
		t.Errorf("viewed %q, camera %q pinned %v, scroll %d", st.Viewed, st.Camera, st.Pinned, m.scroll[ideview.ExplorerPane])
	}
}

// A search that a later /open overtook, or that looked in another folder than the explorer's now,
// shows nothing and is not kept.
func TestModelOpenStaleScan(t *testing.T) {
	m, root := modelOpenNew(t, 120, 40)
	scan := openScan
	t.Cleanup(func() { openScan = scan })
	found := func(name string) openspec.Index {
		return openspec.Index{Root: root, Changes: []openspec.Change{{Name: name, Copy: openspec.Copy{Name: "c"}}}}
	}
	n := 0
	openScan = func(r string, _ openspec.Limits) openspec.Index {
		n++
		idx := found("scan-" + strconv.Itoa(n))
		idx.Root = r
		return idx
	}
	first := modelLocalSay(m, "/open")
	msgFirst := first() // the search of the first /open, run but not yet delivered
	m.Popup = nil
	second := modelLocalSay(m, "/open")
	p := m.Popup.(*OpenPopup)
	m.Update(second())
	m.Update(msgFirst) // the older search arrives last
	if p.Loading || len(p.Index.Changes) != 1 || p.Index.Changes[0].Name != "scan-2" || m.openIndex.Changes[0].Name != "scan-2" {
		t.Errorf("an older search overwrote the newer: %+v", p.Index.Changes)
	}

	m.Popup = nil
	third := modelLocalSay(m, "/open")
	p = m.Popup.(*OpenPopup)
	msg := third()
	moved := t.TempDir()
	m.Sim.State().SetRoot(moved) // the explorer moved on meanwhile
	_, again := m.Update(msg)
	if !p.Loading || m.openIndex.Changes[0].Name != "scan-2" || again == nil {
		t.Fatalf("a search of another folder was taken: loading %v, kept %q", p.Loading, m.openIndex.Changes[0].Name)
	}
	m.Update(again())
	if p.Loading || p.Index.Root != moved || p.Index.Changes[0].Name != "scan-4" {
		t.Errorf("the list did not look in the explorer's folder: loading %v, %+v", p.Loading, p.Index)
	}
	m.Popup = nil
	msg = modelLocalSay(m, "/open")()
	m.Popup = nil
	m.Sim.State().SetRoot(t.TempDir())
	if _, cmd := m.Update(msg); cmd != nil {
		t.Error("a closed list looks again")
	}
}

// However many rows the list has and however little room, it never takes more lines than it has,
// its last line is the hint, and the highlighted row is on it.
func TestDrawOpenFitsRows(t *testing.T) {
	ix := openspec.Index{Root: "/r", Main: openspec.Copy{Root: "/r/main", Name: "main", Branch: "main"}}
	for i := range 30 {
		ix.Changes = append(ix.Changes,
			openspec.Change{Name: fmt.Sprintf("own-%02d-change", i), Own: true, Copy: openspec.Copy{Name: "specs-DEV-1"}},
			openspec.Change{Name: fmt.Sprintf("other-%02d-change", i), Copy: openspec.Copy{Name: "specs-main"}})
		ix.Specs = append(ix.Specs, openspec.Spec{Capability: fmt.Sprintf("domain/cap-%02d", i), Path: "/r/main/openspec/specs/x/spec.md"})
	}
	for _, partial := range []string{"", "stopped after 5s"} {
		for _, w := range []int{60, 76, 120} {
			for rows := 1; rows <= 40; rows++ {
				for _, at := range []int{0, 29, 30, 59, 60, 89} { // the first and last of each group
					p := &OpenPopup{Index: ix, Now: modelT0}
					p.Index.Partial = partial
					p.find()
					p.Cursor = at
					lines, hits := drawOpen(p, w, rows)
					what := fmt.Sprintf("rows %d, width %d, cursor %d, partial %q", rows, w, at, partial)
					plain := make([]string, len(lines))
					for i, l := range lines {
						plain[i] = ansi.Strip(l)
					}
					if len(lines) > rows || len(lines) == 0 || !strings.HasPrefix(plain[len(plain)-1], "type to find") {
						t.Fatalf("%s: %d lines, last %q:\n%s", what, len(lines), plain[len(plain)-1], strings.Join(plain, "\n"))
					}
					if rows < 6 { // too few lines for a row besides the hint
						continue
					}
					if !slices.ContainsFunc(plain, func(l string) bool { return strings.HasPrefix(l, "› ") }) {
						t.Fatalf("%s: the highlighted row is out of sight:\n%s", what, strings.Join(plain, "\n"))
					}
					if rows >= 15 && plain[len(plain)-2] != "" { // room for the blank lines: one sets the hint apart
						t.Fatalf("%s: the hint is not set apart:\n%s", what, strings.Join(plain, "\n"))
					}
					for _, h := range hits {
						if h.y >= len(lines)-1 || !strings.HasPrefix(plain[h.y], "  ") && !strings.HasPrefix(plain[h.y], "› ") {
							t.Fatalf("%s: a hit at line %d is not a row:\n%s", what, h.y, strings.Join(plain, "\n"))
						}
					}
				}
			}
		}
	}
}

// With no copy to list, the message and the hint still fit the rows.
func TestDrawOpenNoCopiesFitsRows(t *testing.T) {
	for rows := 1; rows <= 12; rows++ {
		p := &OpenPopup{Index: openspec.Index{Root: "/r"}, Now: modelT0}
		lines, _ := drawOpen(p, 60, rows)
		if last := ansi.Strip(lines[len(lines)-1]); len(lines) > rows || !strings.HasPrefix(last, "type to find") {
			t.Errorf("rows %d: %d lines, last %q", rows, len(lines), last)
		}
	}
}

// The main specs say which branch their copy is on.
func TestModelOpenMainBranch(t *testing.T) {
	m, root := modelOpenNew(t, 160, 50)
	openWrite(t, root, "workspace-specs/.git/HEAD", "ref: refs/heads/DEV-119462\n", 0)
	modelOpenRun(t, m, "")
	if out := modelOpenScreen(m); !strings.Contains(out, "Main specs  workspace-specs/openspec/specs @ DEV-119462 · 2") {
		t.Errorf("no branch in the header:\n%s", out)
	}
}
