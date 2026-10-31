package ui

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/pixelbender/claudit/internal/ide"
	"github.com/pixelbender/claudit/internal/protocol"
)

func modelNames(cs []protocol.Command) []string {
	var names []string
	for _, c := range cs {
		names = append(names, c.Name)
	}
	return names
}

// modelSlashNew is a model whose session runs a few slash commands.
func modelSlashNew(t *testing.T) (*Model, *[]protocol.Outbound) {
	m, sent := modelNew(t, false)
	modelEvents(m, protocol.Init{Model: "m", Commands: []string{"compact", "context", "doctor", "clear", "model", "__internal", "heapdump"}})
	return m, sent
}

func TestModelSlashSuggest(t *testing.T) {
	m, sent := modelSlashNew(t)
	modelPress(m, inputTyped("/co")...)
	if want := []string{"copy", "compact", "context"}; !reflect.DeepEqual(modelNames(m.Suggest), want) {
		t.Fatalf("suggest %q, want %q", m.Suggest, want)
	}
	modelPress(m, inputKey("down"))
	if m.SuggestSel != 1 {
		t.Errorf("down: sel %d", m.SuggestSel)
	}
	modelPress(m, inputKeys("down down")...) // wraps
	if m.SuggestSel != 0 {
		t.Errorf("wrap: sel %d", m.SuggestSel)
	}
	modelPress(m, inputKeys("up tab")...)
	if got := string(m.Input.Text); got != "/context " || m.Suggest != nil {
		t.Errorf("tab completed %q, suggest %q", got, m.Suggest)
	}
	if m.PanelFocus {
		t.Error("tab went to the agents panel")
	}

	// Contains matches come after prefix matches; nothing but bare commands complete.
	m.Input = Input{}
	modelPress(m, inputTyped("/o")...)
	if want := []string{"compact", "context", "copy", "doctor", "export", "model"}; !reflect.DeepEqual(modelNames(m.Suggest), want) {
		t.Errorf("/o: %q", m.Suggest)
	}
	m.Input = Input{}
	modelPress(m, inputTyped("hi /co")...)
	if m.Suggest != nil {
		t.Errorf("a prompt that is not a command completes: %q", m.Suggest)
	}

	// Enter runs the highlighted command.
	m.Input = Input{}
	modelPress(m, inputTyped("/cle")...)
	modelPress(m, inputKey("enter"))
	if len(*sent) != 1 || modelUserText((*sent)[0]) != "/clear" {
		t.Errorf("sent %#v", *sent)
	}
}

func TestModelSlashLocal(t *testing.T) {
	m, sent := modelSlashNew(t)

	// A command of the interactive claude alone goes nowhere; a terminal-oriented one runs.
	modelPress(m, inputTyped("/memory")...)
	modelPress(m, inputKey("enter"))
	if len(*sent) != 0 || len(m.Chat) != 1 || !strings.Contains(m.Chat[0].Text, "/memory runs only in the interactive claude") {
		t.Errorf("interactive only: sent %#v chat %v", *sent, m.Chat)
	}
	m.Input = Input{}
	modelPress(m, inputTyped("/doctor")...)
	modelPress(m, inputKey("enter"))
	if len(*sent) != 1 || modelUserText((*sent)[0]) != "/doctor" {
		t.Errorf("/doctor: sent %#v", *sent)
	}
	*sent, m.Chat = nil, nil
	modelEvents(m, protocol.Result{})

	modelPress(m, inputTyped("/help")...)
	modelPress(m, inputKey("enter"))
	if !m.ShowHelp || len(*sent) != 0 {
		t.Fatalf("/help: help %v sent %#v", m.ShowHelp, *sent)
	}
	modelPress(m, inputKeys("up pgup")...) // the keys belong to the help
	if len(m.Input.Text) != 0 || !m.ShowHelp || m.ChatScroll != 0 {
		t.Errorf("a key leaked under the help: %q", string(m.Input.Text))
	}
	modelPress(m, inputKey("x")) // typing closes it and goes on to the prompt
	if string(m.Input.Text) != "x" || m.ShowHelp {
		t.Errorf("typing under the help: prompt %q help %v", string(m.Input.Text), m.ShowHelp)
	}
	m.Input = Input{}
	m.ShowHelp = true
	modelPress(m, inputKey("esc"))
	if m.ShowHelp {
		t.Error("esc does not close the help")
	}
	modelPress(m, inputKey("f1"))
	if !m.ShowHelp {
		t.Error("F1 does not open the help")
	}
	modelPress(m, inputKey("f1"))

	for _, cmd := range []string{"/exit", "/quit"} {
		modelPress(m, inputTyped(cmd)...)
		_, c := m.Update(inputKey("enter"))
		if c == nil {
			t.Fatalf("%s: no command", cmd)
		}
		if _, ok := c().(tea.QuitMsg); !ok {
			t.Errorf("%s does not quit", cmd)
		}
	}
	if len(*sent) != 0 {
		t.Errorf("local commands were sent: %#v", *sent)
	}
}

// /clear: claude answers with conversation_reset; the chat starts over, saying so.
func TestModelReset(t *testing.T) {
	m, _ := modelSlashNew(t)
	modelEvents(m, protocol.TextStart{}, protocol.TextDelta{Text: "old reply"})
	m.ChatScroll = 3
	modelEvents(m, protocol.Reset{}, protocol.CLIReply{Text: "## Context Usage"})
	want := []ChatEntry{{Kind: CommandEntry, Text: "Conversation cleared", At: modelT0}, {Kind: CommandEntry, Text: "## Context Usage", At: modelT0}}
	if !reflect.DeepEqual(m.Chat, want) || m.ChatScroll != 0 {
		t.Errorf("chat after reset %+v scroll %d", m.Chat, m.ChatScroll)
	}
}

func TestModelStatusAgents(t *testing.T) {
	m, _ := modelNew(t, false)
	st := m.Sim.State()
	a := m.Sim.Agent("w1", ide.WorkflowAgent)
	m.Sim.Agent("w2", ide.WorkflowAgent).Status = ide.Done
	m.Sim.Agent("wf", ide.Workflow)
	a.Status = ide.Running
	m.Update(tickMsg(modelT0))
	if m.Status.Agents != 1 {
		t.Errorf("agents at work %d (of %d)", m.Status.Agents, len(st.Agents))
	}
}

func TestModelElapsed(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0: "0s", 7*time.Second + 900*time.Millisecond: "7s", 65 * time.Second: "1m05s", 62 * time.Minute: "1h02m", -time.Second: "0s",
	} {
		if got := elapsed(d); got != want {
			t.Errorf("elapsed(%v) = %q, want %q", d, got, want)
		}
	}
	m, _ := modelNew(t, false)
	m.Update(tickMsg(modelT0))
	modelPress(m, inputTyped("go")...)
	modelPress(m, inputKey("enter"))
	m.Update(tickMsg(modelT0.Add(12 * time.Second)))
	if m.Status.Elapsed != "12s" {
		t.Errorf("elapsed %q", m.Status.Elapsed)
	}
	modelEvents(m, protocol.Result{})
	if m.Status.Elapsed != "" {
		t.Errorf("elapsed after the turn %q", m.Status.Elapsed)
	}
}

// A reset is to the minute within a day, to the hour past it; no unit is padded, a zero one goes.
func TestUntilReset(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0: "0m", -time.Minute: "0m", 42 * time.Minute: "42m", 4*time.Hour + 6*time.Minute: "4h6m", 4 * time.Hour: "4h",
		57*time.Hour + 6*time.Minute: "2d9h", 57*time.Hour + 40*time.Minute: "2d10h", 48 * time.Hour: "2d",
		24*time.Hour - 20*time.Second: "1d",
	} {
		if got := untilReset(d); got != want {
			t.Errorf("untilReset(%v) = %q, want %q", d, got, want)
		}
	}
}

// The status shows the rate-limit windows claude reports, until each one resets. Without any
// (an API key) it shows the cost.
func TestModelLimits(t *testing.T) {
	m, _ := modelNew(t, false)
	m.Update(tickMsg(modelT0))
	if m.Status.Usage != "" {
		t.Errorf("limits %q before any report", m.Status.Usage)
	}
	modelEvents(m, protocol.Result{CostUSD: 0.0123})
	if m.Status.Usage != "$0.0123" {
		t.Errorf("usage %q without limits", m.Status.Usage)
	}
	modelEvents(m, protocol.RateLimits{
		Session: &protocol.Window{Used: 0.095, ResetsAt: modelT0.Add(time.Hour)},
		Week:    &protocol.Window{Used: 0.58, ResetsAt: modelT0.Add(48 * time.Hour)},
		Fable:   &protocol.Window{Used: 1.2, ResetsAt: modelT0.Add(48 * time.Hour)},
	})
	if want := "5h 9% 1h · 7d 58% fable 120% 2d"; m.Status.Usage != want { // fable shares the week's reset
		t.Errorf("limits %q, want %q", m.Status.Usage, want)
	}
	m.Update(tickMsg(modelT0.Add(time.Hour)))
	if want := "7d 58% fable 120% 1d23h"; m.Status.Usage != want {
		t.Errorf("limits %q after the 5-hour reset, want %q", m.Status.Usage, want)
	}
}

// A window at its limit says until when, unless extra usage pays past it; the windows go with the
// usage, for its colours.
func TestModelLimitReached(t *testing.T) {
	m, _ := modelNew(t, false)
	m.Update(tickMsg(modelT0))
	session := &protocol.Window{Used: 1, ResetsAt: modelT0.Add(90 * time.Minute), Status: "rejected"}
	week := &protocol.Window{Used: 0.8, ResetsAt: modelT0.Add(48 * time.Hour), Status: "allowed_warning"}
	modelEvents(m, protocol.RateLimits{Session: session, Week: week, Overage: "rejected"})
	if want := "5h limit until 13:30 · 7d 80% 2d"; m.Status.Usage != want {
		t.Errorf("limits %q, want %q", m.Status.Usage, want)
	}
	modelEvents(m, protocol.RateLimits{Session: session, Week: week, Overage: "allowed"})
	if want := "5h 100% 1h30m · 7d 80% 2d · extra usage"; m.Status.Usage != want {
		t.Errorf("limits %q, want %q", m.Status.Usage, want)
	}
	modelEvents(m, protocol.RateLimits{Week: &protocol.Window{Used: 1, ResetsAt: modelT0.Add(48 * time.Hour), Status: "rejected"}})
	if want := "7d limit until Oct 31 12:00"; m.Status.Usage != want {
		t.Errorf("limits %q, want %q", m.Status.Usage, want)
	}
}

// The context gauge fills with main's model calls once a result tells the model's window;
// clearing the conversation empties it.
func TestModelContext(t *testing.T) {
	m, _ := modelNew(t, false)
	modelEvents(m, protocol.Context{Model: "haiku", Tokens: 30000})
	if m.Status.Context != 0 {
		t.Errorf("context %v before the window is known", m.Status.Context)
	}
	modelEvents(m, protocol.Result{ContextWindows: map[string]int{"haiku": 200000, "opus": 1000000}})
	if m.Status.Context != 0.15 {
		t.Errorf("context %v, want 0.15", m.Status.Context)
	}
	modelEvents(m, protocol.Result{}, protocol.Context{Model: "haiku", Tokens: 50000})
	if m.Status.Context != 0.25 {
		t.Errorf("context %v after a result without windows, want 0.25", m.Status.Context)
	}
	modelEvents(m, protocol.Reset{})
	if m.Status.Context != 0 {
		t.Errorf("context %v after /clear", m.Status.Context)
	}
}

// Initialize answers with descriptions before any Init; Init later drops the terminal-only ones.
func TestModelSlashDescriptions(t *testing.T) {
	m, _ := modelNew(t, false)
	modelEvents(m, protocol.Commands{List: []protocol.Command{
		{Name: "compact", Description: "Free up context", Hint: "<instructions>"},
		{Name: "doctor", Description: "Check the install"},
	}})
	modelPress(m, inputTyped("/")...)
	local := []string{"btw", "copy", "exit", "export", "help", "plugins", "quit", "resume", "status"}
	if got, want := modelNames(m.Suggest), slices.Concat([]string{"compact", "doctor"}, local); !reflect.DeepEqual(got, slices.Sorted(slices.Values(want))) {
		t.Fatalf("suggest %q", got)
	}
	modelEvents(m, protocol.Init{Commands: []string{"compact", "doctor", "model", "heapdump"}})
	m.Update(tickMsg(modelT0))
	if got, want := modelNames(m.Suggest), slices.Concat([]string{"compact", "doctor", "model"}, local); !reflect.DeepEqual(got, slices.Sorted(slices.Values(want))) {
		t.Errorf("after init %q", got) // the hidden heapdump stays out
	}
	if i := slices.IndexFunc(m.Suggest, func(c protocol.Command) bool { return c.Name == "compact" }); m.Suggest[i].Description != "Free up context" {
		t.Errorf("Init dropped the description: %+v", m.Suggest[i])
	}
}

// Fast ticks only while something moves on screen: a wipe while it plays, not an empty editor.
func TestModelAnimating(t *testing.T) {
	m, _ := modelNew(t, false)
	m.Update(tickMsg(modelT0))
	if !m.animating() {
		t.Error("the spinner while claude starts does not get frames")
	}
	m.Status.State = "idle"
	if m.animating() {
		t.Error("ticking for an empty editor")
	}
	m.Sim.Agent("s1", ide.Subagent).Focus = ide.Terminal
	m.Sim.Pin("s1") // a cut at the last tick
	if !m.animating() {
		t.Error("the wipe after a pin does not get frames")
	}
	m.Update(tickMsg(modelT0.Add(time.Second)))
	if m.animating() {
		t.Error("ticking after the wipe ended")
	}
}

// Errors go to the status, not the chat: what the CLI says otherwise stays in the chat.
func TestModelErrorOnce(t *testing.T) {
	m, _ := modelNew(t, false)
	modelEvents(m, protocol.CLIReply{Text: "API Error: 529 overloaded"},
		protocol.Result{IsError: true, Text: "API Error: 529 overloaded"})
	if len(m.Chat) != 0 || m.Status.State != "API error 529 · overloaded" {
		t.Errorf("chat %+v state %q", m.Chat, m.Status.State)
	}
	modelEvents(m, protocol.CLIReply{Text: "Total cost: $0.01"})
	if len(m.Chat) != 1 {
		t.Errorf("a command's output is not in the chat: %+v", m.Chat)
	}
}

// modelRaw runs cmd and returns what it writes to the terminal raw, "" if nothing.
func modelRaw(cmd tea.Cmd) string {
	if cmd == nil {
		return ""
	}
	switch msg := cmd().(type) {
	case tea.RawMsg:
		s, _ := msg.Msg.(string)
		return s
	case tea.BatchMsg:
		var out string
		for _, c := range msg {
			out += modelRaw(c)
		}
		return out
	}
	return ""
}

// While the terminal is away, a popup and a finished turn ring it with a notification.
func TestModelNotice(t *testing.T) {
	m, _ := modelNew(t, false)
	ring := func(ev protocol.Event) string {
		_, cmd := m.Update(AgentMsg{Events: []protocol.Event{ev}})
		return modelRaw(cmd)
	}
	if s := ring(protocol.PermissionRequest{RequestID: "p1", ToolName: "Bash", Input: map[string]any{"command": "ls"}}); s != "" {
		t.Errorf("rang in focus: %q", s)
	}
	m.Popup = nil
	m.Update(tea.BlurMsg{})
	if s := ring(protocol.PermissionRequest{RequestID: "p2", ToolName: "Bash", Input: map[string]any{"command": "ls"}}); s != "\x1b]9;claude wants to use Bash\x07" {
		t.Errorf("popup notice %q", s)
	}
	m.Status.Busy, m.Status.Elapsed = true, "1m 05s"
	if s := ring(protocol.Result{}); s != "\x1b]9;claude is done · 1m 05s\x07" {
		t.Errorf("done notice %q", s)
	}
	if s := ring(protocol.Result{IsError: true, Text: "API Error:\x1b[31m 529"}); s != "\x1b]9;claude stopped: API Error: 529\x07" {
		t.Errorf("error notice %q", s)
	}
	m.Update(tea.FocusMsg{})
	m.Status.Busy = true
	if s := ring(protocol.Result{}); s != "" {
		t.Errorf("rang in focus: %q", s)
	}
}

// A popup that comes up while the user types is not answered by the sentence, and the prompt
// takes nothing more: its letters go nowhere until the user pauses.
func TestModelPopupWhileTyping(t *testing.T) {
	m, sent := modelNew(t, false)
	modelPress(m, inputTyped("say")...)
	modelAt(m, 300*time.Millisecond)
	modelEvents(m, protocol.PermissionRequest{RequestID: "p1", ToolName: "Bash", Input: map[string]any{"command": "rm -rf build"}})
	modelAt(m, 500*time.Millisecond)
	modelPress(m, inputTyped("yes")...)
	modelPress(m, inputKey("enter"))
	if m.Popup == nil || len(*sent) != 0 || string(m.Input.Text) != "say" {
		t.Fatalf("popup %v, sent %v, prompt %q", m.Popup != nil, *sent, string(m.Input.Text))
	}
	modelPress(m, inputKey("right")) // not text: the popup has it
	if p := m.Popup.(*PermissionPopup); p.Allow {
		t.Error("right did not reach the popup")
	}
	modelAt(m, 500*time.Millisecond+popupGrace)
	modelPress(m, inputKey("n"))
	if m.Popup != nil || len(*sent) != 1 {
		t.Fatalf("after a pause: popup %v, sent %v", m.Popup != nil, *sent)
	}
	if _, ok := (*sent)[0].(protocol.Deny); !ok {
		t.Errorf("answer %#v", (*sent)[0])
	}
}

// The likely command comes first: the shortest that starts with the query, then the ones that
// contain it, then the ones whose description does.
func TestModelSlashRanking(t *testing.T) {
	m, _ := modelNew(t, false)
	modelEvents(m, protocol.Commands{List: []protocol.Command{
		{Name: "mobile-native", Description: "Make a web app feel native"},
		{Name: "model", Description: "Set the AI model"},
		{Name: "usage", Description: "Show session cost and plan usage"},
		{Name: "cost-report"},
	}})
	modelPress(m, inputTyped("/mo")...)
	if got := modelNames(m.Suggest); !reflect.DeepEqual(got, []string{"model", "mobile-native"}) {
		t.Errorf("/mo: %q", got)
	}
	m.Input = Input{}
	modelPress(m, inputTyped("/cost")...)
	if got := modelNames(m.Suggest); !reflect.DeepEqual(got, []string{"cost-report", "usage"}) {
		t.Errorf("/cost: %q", got)
	}
}

// A replay in a mode that would not have asked answers its permission popups itself, once the
// audience has read them.
func TestModelReplayAutoAllow(t *testing.T) {
	for _, tt := range []struct {
		mode, tool string
		allow      bool
	}{{"default", "Bash", false}, {"auto", "Bash", true}, {"acceptEdits", "Edit", true}, {"acceptEdits", "Bash", false}} {
		m, sent := modelNew(t, true)
		m.Status.Mode = tt.mode
		m.Update(tickMsg(modelT0))
		modelEvents(m, protocol.PermissionRequest{RequestID: "p1", ToolName: tt.tool, Input: map[string]any{}})
		m.Update(tickMsg(modelT0.Add(replayAutoAllow / 2)))
		if m.Popup == nil {
			t.Fatalf("%s %s: answered before the audience could read it", tt.mode, tt.tool)
		}
		m.Update(tickMsg(modelT0.Add(replayAutoAllow)))
		if got := m.Popup == nil && len(*sent) == 1; got != tt.allow {
			t.Errorf("%s %s: allowed %v, sent %v", tt.mode, tt.tool, got, *sent)
		}
	}
	// The mode the viewer picks outlasts the modes the recording reports.
	m, _ := modelNew(t, true)
	modelPress(m, inputKeys("shift+tab shift+tab shift+tab")...)
	modelEvents(m, protocol.ModeChanged{Mode: "default"})
	if m.Status.Mode != "auto" {
		t.Errorf("mode %q after the recording's", m.Status.Mode)
	}
}

// Prompts typed while claude works wait under the chat for the turn to end, then join the chat
// and go as one message. A slash command goes alone: the prompts after it wait for the turn it
// starts.
func TestModelQueued(t *testing.T) {
	m, sent := modelNew(t, false)
	m.Status.Busy = true
	for _, p := range []string{"Next", "And this", "/compact", "Later"} {
		modelPress(m, inputTyped(p)...)
		modelPress(m, inputKey("enter"))
	}
	modelEvents(m, protocol.TextDelta{Text: "done"}) // what claude says meanwhile goes above them
	drawn := func() (texts []string) {
		for _, e := range m.chatEntries() {
			switch {
			case e.Queued:
				e.Text = "~" + e.Text
			case e.Kind == CaretEntry:
				e.Text = "@"
			}
			texts = append(texts, e.Text)
		}
		return texts
	}
	if want := []string{"done", "@", "~Next", "~And this", "~/compact", "~Later"}; !slices.Equal(drawn(), want) || len(m.Chat) != 1 || len(*sent) != 0 {
		t.Fatalf("drawn %q chat %+v sent %v", drawn(), m.Chat, *sent)
	}
	if lines := drawEntry(m.chatEntries()[2], 60); len(lines) != 1 || lines[0] != styleDim.Render("› Next") {
		t.Errorf("drawn %q: a queued prompt is grey, with nothing more", lines)
	}
	for i, want := range []struct {
		text  string
		drawn []string
	}{
		{"Next\n\nAnd this", []string{"done", "Next", "And this", "@", "~/compact", "~Later"}},
		{"/compact", []string{"done", "Next", "And this", "@", "~Later"}}, // a command stays out of the chat
		{"Later", []string{"done", "Next", "And this", "Later", "@"}},
	} {
		modelEvents(m, protocol.Result{})
		if len(*sent) != i+1 || modelUserText((*sent)[i]) != want.text || !slices.Equal(drawn(), want.drawn) || !m.Status.Busy {
			t.Fatalf("turn %d: sent %q, drawn %q, busy %v", i, *sent, drawn(), m.Status.Busy)
		}
	}
	modelEvents(m, protocol.Result{})
	if len(*sent) != 3 || m.Status.Busy {
		t.Errorf("after the queue: sent %q, busy %v", *sent, m.Status.Busy)
	}
}

// The user's slash commands go to claude but stay out of the chat; what they print joins it. The
// turn of a command the user interrupts leaves the prompt before it as it was.
func TestModelCommandsOutOfChat(t *testing.T) {
	m, sent := modelLocalNew(t)
	modelLocalSay(m, "Fix it")
	modelEvents(m, protocol.Result{})
	modelLocalSay(m, "/context")
	if len(*sent) != 2 || modelUserText((*sent)[1]) != "/context" || len(m.Chat) != 1 {
		t.Fatalf("sent %#v chat %+v", *sent, m.Chat)
	}
	modelEvents(m, protocol.CLIReply{Text: "Context Usage"}, protocol.Result{})
	if len(m.Chat) != 2 || m.Chat[1].Kind != CommandEntry {
		t.Errorf("the output: chat %+v", m.Chat)
	}
	modelLocalSay(m, "/context")
	modelPress(m, inputKey("esc"))
	modelEvents(m, protocol.Result{IsError: true})
	if m.Chat[0].Cut || len(m.Chat) != 2 {
		t.Errorf("an interrupted command: chat %+v", m.Chat)
	}
}

// While claude works its caret spins white where its next words go, under the text of the prompt
// it got, then under what it says. Prompts queued meanwhile wait below: claude has not seen them.
func TestModelCaret(t *testing.T) {
	m, _ := modelNew(t, false)
	chat := func() []string {
		c, entries := new(chatCache), m.chatEntries()
		out, _ := renderChat(c, entries, 25, drawChatCount(c, entries, 25), 0)
		return drawPlainLines(out)
	}
	modelPress(m, inputTyped("Fix it")...)
	modelPress(m, inputKey("enter"))
	spin := drawSpinner[m.Status.Frame%len(drawSpinner)]
	if got, want := chat(), []string{"  › Fix it", "    " + spin}; !slices.Equal(got, want) {
		t.Fatalf("chat %q, want %q", got, want)
	}
	if e := m.chatEntries()[1]; !slices.Equal(drawEntry(e, 60), []string{"  " + styleReply.Render(spin)}) {
		t.Errorf("caret %q is not white", drawEntry(e, 60))
	}
	modelPress(m, inputTyped("And docs")...)
	modelPress(m, inputKey("enter"))
	modelEvents(m, protocol.TextDelta{Text: "on it"})
	if got, want := chat(), []string{"  › Fix it", "  › on it", "    " + spin, "  › And docs"}; !slices.Equal(got, want) {
		t.Errorf("chat %q, want %q", got, want)
	}
	m.pending = nil
	modelEvents(m, protocol.Result{})
	if got := chat(); slices.ContainsFunc(m.chatEntries(), func(e ChatEntry) bool { return e.Kind == CaretEntry }) || len(got) != 2 {
		t.Errorf("idle, chat %q", got)
	}
}
