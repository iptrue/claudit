package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/protocol"
	"github.com/pixelbender/claudit/internal/theme"
)

// modelMouseNew is a live model on a 100×30 screen.
func modelMouseNew(t *testing.T) (*Model, *[]protocol.Outbound) {
	t.Helper()
	m, sent := modelNew(t, false)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m, sent
}

// modelMouseAt draws m and finds text on the screen: the cell where it starts.
func modelMouseAt(t *testing.T, m *Model, text string) (x, y int) {
	t.Helper()
	for y, l := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if i := strings.Index(l, text); i >= 0 {
			return ansi.StringWidth(l[:i]), y
		}
	}
	t.Fatalf("%q is not on screen:\n%s", text, ansi.Strip(m.View().Content))
	return 0, 0
}

// modelMouseClick clicks where text starts on the screen and plays what the click starts.
func modelMouseClick(t *testing.T, m *Model, text string) {
	t.Helper()
	x, y := modelMouseAt(t, m, text)
	_, cmd := m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	modelResumeRun(m, cmd)
}

func modelMouseWheel(m *Model, up bool) {
	b := tea.MouseWheelDown
	if up {
		b = tea.MouseWheelUp
	}
	m.Update(tea.MouseWheelMsg{X: 50, Y: 20, Button: b})
}

// A click on a folder in the explorer folds or unfolds it.
func TestModelMouseToggleFolder(t *testing.T) {
	m, _ := modelMouseNew(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "pkg", "inner.go"), nil, 0o644)
	m.Sim.SetRoot(root)
	modelMouseClick(t, m, "▸ pkg")
	modelMouseAt(t, m, "inner.go")
	modelMouseClick(t, m, "▾ pkg")
	if strings.Contains(ansi.Strip(m.View().Content), "inner.go") {
		t.Fatal("pkg did not fold")
	}
}

// A click on a file in the explorer opens it in the editor; a binary one says why it does not.
func TestModelMouseOpensFile(t *testing.T) {
	m, _ := modelMouseNew(t)
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello from disk"), 0o644)
	os.WriteFile(filepath.Join(root, "blob.bin"), []byte("x\x00y"), 0o644)
	m.Sim.SetRoot(root)
	modelMouseClick(t, m, "notes.txt")
	modelMouseAt(t, m, "hello from disk")
	modelMouseClick(t, m, "blob.bin")
	if !modelResumeToast(m, "blob.bin is binary or unreadable") {
		t.Errorf("toasts %v", m.Toasts)
	}
}

func TestModelMouseScroll(t *testing.T) {
	m, _ := modelMouseNew(t)
	for i := range 40 {
		m.Chat = append(m.Chat, ChatEntry{Kind: AssistantEntry, Text: fmt.Sprint("line ", i)})
	}
	m.View()
	var scrolls []int
	for _, step := range []func(){
		func() { modelMouseWheel(m, true) },
		func() { modelMouseWheel(m, true) },
		func() { modelMouseWheel(m, false) },
		func() { modelPress(m, tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift}) }, // not a scroll key
	} {
		step()
		scrolls = append(scrolls, m.ChatScroll)
	}
	if want := []int{3, 6, 3, 3}; !reflect.DeepEqual(scrolls, want) {
		t.Errorf("scrolls %v, want %v", scrolls, want)
	}
	for range 50 {
		modelMouseWheel(m, true)
	}
	if m.ChatScroll != m.chatMax {
		t.Errorf("scrolled past the top: %d of %d", m.ChatScroll, m.chatMax)
	}

	// Under a popup the wheel scrolls the popup, not the chat.
	modelEvents(m, protocol.PermissionRequest{RequestID: "p", ToolName: "Bash", Input: map[string]any{"command": "ls"}})
	scroll := m.ChatScroll
	modelMouseWheel(m, false)
	if p := m.Popup.(*PermissionPopup); p.Scroll != wheelStep || m.ChatScroll != scroll {
		t.Errorf("popup scroll %d, chat %d", p.Scroll, m.ChatScroll)
	}
}

// Over the IDE the wheel scrolls the pane under it; a pane gone from the screen forgets it.
func TestModelMouseScrollIDE(t *testing.T) {
	m, _ := modelMouseNew(t)
	dir := t.TempDir()
	for i := range 40 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d.txt", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m.Sim.SetRoot(dir)
	modelMouseAt(t, m, "f00.txt")
	m.Update(tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})
	if _, y := modelMouseAt(t, m, "f03.txt"); y != 1 || m.ChatScroll != 0 {
		t.Errorf("f03.txt on row %d, chat scrolled %d", y, m.ChatScroll)
	}
}

// The wheel events that come while a scroll is in progress cost no frame: they add up and scroll at
// its next frame, as one. A frame with nothing to scroll ends the scroll.
func TestModelMouseWheelBurst(t *testing.T) {
	m, _ := modelMouseNew(t)
	dir := t.TempDir()
	for i := range 40 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%02d.txt", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m.Sim.SetRoot(dir)
	modelMouseAt(t, m, "f00.txt")
	down := tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown}
	if _, cmd := m.Update(down); cmd == nil {
		t.Fatal("the first notch starts no scroll")
	}
	for range 2 {
		if msg := Filter(m, down); msg != nil {
			t.Fatalf("a notch of a scroll in progress went through: %v", msg)
		}
	}
	if _, y := modelMouseAt(t, m, "f03.txt"); y != 1 {
		t.Errorf("before the frame f03.txt on row %d", y)
	}
	if _, cmd := m.Update(wheelMsg{}); cmd == nil {
		t.Error("the scroll ended while the wheel turned")
	}
	if _, y := modelMouseAt(t, m, "f09.txt"); y != 1 {
		t.Errorf("after the frame f09.txt on row %d", y)
	}
	if _, cmd := m.Update(wheelMsg{}); cmd != nil {
		t.Error("the scroll goes on with the wheel still")
	}
	if msg := Filter(m, down); msg == nil {
		t.Error("a notch after the scroll is held back")
	}
}

func TestModelMouseClickPopup(t *testing.T) {
	m, sent := modelMouseNew(t)
	modelEvents(m, protocol.PermissionRequest{RequestID: "p1", ToolName: "Bash", Input: map[string]any{"command": "ls"}})
	m.Update(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft}) // outside the popup: nothing
	if m.Popup == nil || len(*sent) != 0 {
		t.Fatalf("a click beside the popup: popup %v sent %#v", m.Popup != nil, *sent)
	}
	modelMouseClick(t, m, "[ Deny ]")
	if len(*sent) != 1 || m.Popup != nil {
		t.Fatalf("deny: popup %v sent %#v", m.Popup != nil, *sent)
	}
	if _, ok := (*sent)[0].(protocol.Deny); !ok {
		t.Errorf("deny sent %#v", (*sent)[0])
	}
	modelEvents(m, protocol.PermissionRequest{RequestID: "p2", ToolName: "Bash", Input: map[string]any{"command": "ls"}})
	modelMouseClick(t, m, "[ Allow ]")
	if a, ok := (*sent)[1].(protocol.Allow); !ok || a.RequestID != "p2" {
		t.Errorf("allow sent %#v", (*sent)[1])
	}

	*sent = nil
	modelAsk(t, m, "q1", modelFixtureQuestion)
	modelMouseClick(t, m, "Division") // the description is part of its option
	modelWantAllow(t, *sent, "q1", modelFixtureQuestion, map[string]string{"Which function should I add to calc.py: mul or div?": "div"})
}

func TestModelMouseClickPanel(t *testing.T) {
	m, _ := modelMouseNew(t)
	modelEvents(m,
		protocol.ToolUse{ID: "s1", Name: "Agent", Input: map[string]any{"description": "alpha"}},
		protocol.ToolUse{ID: "s2", Name: "Agent", Input: map[string]any{"description": "beta"}},
	)
	modelMouseClick(t, m, "Stuart")
	if st := m.Sim.State(); m.PanelSel != "s2" || st.Camera != "s2" || !st.Pinned {
		t.Errorf("sel %q camera %q pinned %v", m.PanelSel, st.Camera, st.Pinned)
	}
	modelPress(m, inputKey("tab"))
	modelMouseClick(t, m, "Ask claude…")
	if m.PanelFocus {
		t.Error("a click on the prompt left the keys with the panel")
	}
}

func TestModelMouseClickSuggest(t *testing.T) {
	m, _ := modelSlashNew(t)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	modelPress(m, inputTyped("/co")...)
	modelMouseClick(t, m, "/context")
	if got := string(m.Input.Text); got != "/context " {
		t.Errorf("prompt %q", got)
	}
}

// The help goes at a click anywhere.
func TestModelMouseClickHelp(t *testing.T) {
	m, _ := modelMouseNew(t)
	m.ShowHelp = true
	m.View()
	m.Update(tea.MouseClickMsg{X: 1, Y: 1, Button: tea.MouseLeft})
	if m.ShowHelp {
		t.Error("the help stays")
	}
}

// A drag selects the text of the pane where it began, lit as it goes, and the release copies it:
// in the chat its arrows and times stay out, and the rows it spans join with new lines.
func TestModelMouseSelect(t *testing.T) {
	m, _ := modelMouseNew(t)
	m.Chat = []ChatEntry{{Kind: UserEntry, Text: "fix the bug", At: modelT0}, {Kind: AssistantEntry, Text: "done, see calc.py"}}
	x, y := modelMouseAt(t, m, "fix the bug")
	m.Update(tea.MouseClickMsg{X: x + 4, Y: y, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: 99, Y: y, Button: tea.MouseLeft}) // past the time: it stays out
	if m.View(); m.sel.text != "the bug" {
		t.Errorf("one row: %q", m.sel.text)
	}
	m.Update(tea.MouseMotionMsg{X: x + 3, Y: y + 1, Button: tea.MouseLeft})
	r, g, b, _ := theme.SelBg.RGBA()
	if v := m.View(); m.sel.text != "the bug\ndone" || !strings.Contains(v.Content, fmt.Sprintf("48;2;%d;%d;%d", r>>8, g>>8, b>>8)) {
		t.Errorf("two rows: %q, lit %v", m.sel.text, strings.Contains(v.Content, "48;2"))
	}
	_, cmd := m.Update(tea.MouseReleaseMsg{X: x + 3, Y: y + 1, Button: tea.MouseLeft}) // not run: it would copy
	if n := len(m.Toasts); cmd == nil || n == 0 || m.Toasts[n-1].Text != "copied 2 lines" || m.sel != (selection{}) {
		t.Errorf("release: cmd %v toasts %+v selection %+v", cmd != nil, m.Toasts, m.sel)
	}

	// A click without a drag copies nothing.
	m.Update(tea.MouseClickMsg{X: x + 4, Y: y, Button: tea.MouseLeft})
	m.View()
	if _, cmd := m.Update(tea.MouseReleaseMsg{X: x + 4, Y: y, Button: tea.MouseLeft}); cmd != nil {
		t.Error("a click copied")
	}
}

// modelUnqueueNew is a busy model with prompts queued.
func modelUnqueueNew(t *testing.T, prompts ...string) (*Model, *[]protocol.Outbound) {
	m, sent := modelNew(t, false)
	m.Status.Busy, m.Status.State = true, "thinking"
	for _, p := range prompts {
		modelPress(m, inputTyped(p)...)
		modelPress(m, inputKey("enter"))
	}
	return m, sent
}

// ↑ on an empty prompt takes the last queued prompt into it to edit; enter puts it back in its
// place, as edited. The others stay queued: they are joined only as they go.
func TestModelUnqueue(t *testing.T) {
	m, sent := modelUnqueueNew(t, "Fix it", "And the docs")
	modelPress(m, inputKey("up"))
	if string(m.Input.Text) != "And the docs" || len(m.chatEntries()) != 2 { // the caret and "Fix it"
		t.Fatalf("prompt %q drawn %+v", string(m.Input.Text), m.chatEntries())
	}
	modelPress(m, inputTyped(" too")...)
	modelPress(m, inputKey("enter"))
	if !slices.Equal(m.pending, []string{"Fix it", "And the docs too"}) || len(m.Input.Text) != 0 {
		t.Errorf("pending %q prompt %q", m.pending, string(m.Input.Text))
	}
	modelEvents(m, protocol.Result{})
	if len(*sent) != 1 || modelUserText((*sent)[0]) != "Fix it\n\nAnd the docs too" {
		t.Errorf("sent %#v", *sent)
	}
	modelPress(m, inputKey("up")) // nothing queued: the history
	if string(m.Input.Text) != "And the docs too" || !m.browsing() {
		t.Errorf("history: prompt %q", string(m.Input.Text))
	}
}

// ↑ ↑ reaches the first queued prompt: the edit goes to it, the second stays as it was. ↓ walks
// back, past the newest to an empty prompt, and each keeps what it was given.
func TestModelUnqueueFirst(t *testing.T) {
	m, sent := modelUnqueueNew(t, "One", "Two")
	modelPress(m, inputKey("up"), inputKey("up"))
	modelPress(m, inputTyped(" more")...)
	modelPress(m, inputKey("enter"))
	if !slices.Equal(m.pending, []string{"One more", "Two"}) {
		t.Fatalf("pending %q", m.pending)
	}
	modelPress(m, inputKey("up"), inputKey("up"), inputKey("up")) // the oldest stays
	modelPress(m, inputKey("backspace"), inputKey("down"))
	if string(m.Input.Text) != "Two" || m.pending[0] != "One mor" {
		t.Fatalf("prompt %q pending %q", string(m.Input.Text), m.pending)
	}
	modelPress(m, inputKey("down"))
	if len(m.Input.Text) != 0 || m.queueAt != -1 || len(m.chatEntries()) != 3 { // the caret and both
		t.Fatalf("past the newest: prompt %q at %d", string(m.Input.Text), m.queueAt)
	}
	modelEvents(m, protocol.Result{})
	if len(*sent) != 1 || modelUserText((*sent)[0]) != "One mor\n\nTwo" {
		t.Errorf("sent %q", *sent)
	}
}

// A turn that ends while a queued prompt is edited sends those before it; it and those after it
// go once it is back, in their order. An emptied one leaves the queue; ctrl+c leaves it as it was.
func TestModelUnqueueTurnEnds(t *testing.T) {
	m, sent := modelUnqueueNew(t, "A", "B", "C")
	modelPress(m, inputKey("up"), inputKey("up")) // b
	modelEvents(m, protocol.Result{})
	if len(*sent) != 1 || modelUserText((*sent)[0]) != "A" || !slices.Equal(m.pending, []string{"B", "C"}) {
		t.Fatalf("sent %q pending %q", *sent, m.pending)
	}
	modelPress(m, inputKey("ctrl+c")) // b stays; the turn a started goes on
	if !slices.Equal(m.pending, []string{"B", "C"}) || len(*sent) != 1 {
		t.Fatalf("pending %q sent %q", m.pending, *sent)
	}
	modelPress(m, inputKey("up"), inputKey("up"))
	m.Input = Input{}
	modelEvents(m, protocol.Result{}) // the edited one is first: nothing goes
	if len(*sent) != 1 || m.Status.Busy {
		t.Fatalf("sent %q busy %v", *sent, m.Status.Busy)
	}
	modelPress(m, inputKey("enter")) // emptied: b leaves, c goes at once
	if len(*sent) != 2 || modelUserText((*sent)[1]) != "C" || len(m.pending) != 0 || !m.Status.Busy {
		t.Errorf("sent %q pending %q", *sent, m.pending)
	}
}

// With nothing said the IDE takes all the room above the prompt; the chat grows with what is said,
// up to 35% of the screen.
func TestViewChatGrows(t *testing.T) {
	m := drawTestModel(t, 80, 30)
	m.Chat = nil
	lines := drawPlainLines(m.View().Content)
	if !strings.HasPrefix(lines[25], "╰") || !strings.HasPrefix(lines[26], "╭") {
		t.Errorf("no chat: rows 25-26 %q %q", lines[25], lines[26])
	}
	m.Chat = []ChatEntry{{Kind: UserEntry, Text: "hi"}, {Kind: AssistantEntry, Text: "hello"}}
	lines = drawPlainLines(m.View().Content)
	if !strings.HasPrefix(lines[23], "╰") || lines[24] != "  › hi" || lines[25] != "  › hello" || !strings.HasPrefix(lines[26], "╭") {
		t.Errorf("two lines: rows 23-26 %q", lines[23:27])
	}
	for i := range 40 {
		m.Chat = append(m.Chat, ChatEntry{Kind: AssistantEntry, Text: fmt.Sprint("line ", i)})
	}
	lines = drawPlainLines(m.View().Content)
	if !strings.HasPrefix(lines[15], "╰") || lines[16] == "" {
		t.Errorf("a long chat takes more than 10 rows: rows 15-16 %q %q", lines[15], lines[16])
	}
}

// Behind a popup the screen fades; the popup does not.
func TestViewDimsUnderPopup(t *testing.T) {
	sgr := func(c interface{ RGBA() (r, g, b, a uint32) }) string {
		r, g, b, _ := c.RGBA()
		return fmt.Sprintf("38;2;%d;%d;%d", r>>8, g>>8, b>>8)
	}
	m := drawTestModel(t, 100, 30)
	status := func() string {
		lines := strings.Split(m.View().Content, "\n")
		return lines[len(lines)-1]
	}
	if s := status(); !strings.Contains(s, sgr(theme.Fg)) {
		t.Fatalf("status without a popup: %q", s)
	}
	m.Popup = &PermissionPopup{Tool: "Bash", Detail: "ls", Allow: true}
	if s := status(); strings.Contains(s, sgr(theme.Fg)) || !strings.Contains(s, sgr(theme.Mix(theme.Fg, theme.Bg, drawDimBy))) {
		t.Errorf("status under a popup: %q", s)
	}
	x, y := modelMouseAt(t, m, "Allow Bash?")
	line := strings.Split(m.View().Content, "\n")[y]
	if !strings.Contains(line, sgr(theme.Accent)) || x == 0 {
		t.Errorf("the popup faded: %q", line)
	}
}
