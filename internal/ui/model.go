// Package ui is the Bubble Tea program: a simulated IDE on top, the conversation and the prompt
// below it, and popups when the agent needs an answer.
package ui

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/pixelbender/claudit/internal/ide"
	"github.com/pixelbender/claudit/internal/ideview"
	"github.com/pixelbender/claudit/internal/protocol"
	"github.com/pixelbender/claudit/internal/sessions"
	"github.com/pixelbender/claudit/internal/sim"
)

// AgentMsg delivers the agent events that arrived since the last one, oldest first.
type AgentMsg struct{ Events []protocol.Event }

// Model is the whole UI state. View and Update share it through a pointer.
type Model struct {
	Sim        *sim.Sim
	Chat       []ChatEntry
	Input      Input
	Popup      Popup // nil when none is open
	Status     Status
	ChatScroll int // chat lines scrolled up from the bottom; 0 follows the tail
	Width      int
	Height     int
	Now        time.Time // time of the last tick, for durations and spinners in the view

	Panel      ideview.Panel // what the IDE's left column shows
	PanelFocus bool          // keys go to the left column instead of the prompt
	PanelSel   string        // key of the agent highlighted in the agents panel
	ShowHelp   bool          // F1: the key reference over everything
	Wrap       bool          // the editor wraps long lines (alt+z)

	Suggest    []protocol.Command // slash commands completing the prompt, shown above it
	SuggestSel int                // highlighted one
	Toasts     []Toast            // notices in the corner of the IDE, oldest first
	Away       bool               // the terminal lost the focus: what needs the user is announced
	StartModel string             // the model claude starts with as args or settings name it ("opus"), shown until a turn tells

	chatMax   int            // largest useful ChatScroll, written by View
	chatLines chatCache      // drawn chat entries, kept by View
	zones     []zone         // what clicks act on, written by View
	sel       selection      // the text the mouse drags over; View writes what it covers
	scroll    ideview.Scroll // how far the wheel scrolled the IDE's panes; View forgets those it no longer draws
	burst     wheelBurst     // the wheel events Filter held back since the last frame of a scroll
	send      func(protocol.Outbound)
	queue     []Popup          // requests waiting behind the open popup
	behind    string           // the state of main while "waiting for you" covers it
	clock     func() time.Time // the time of agent events; ticks bring their own
	crew      crew             // what the model tracks about the agents beyond ide.Agent
	agentMode string           // the permission mode the agent last reported; Status.Mode runs ahead of it
	extraMode string           // a reported permission mode beyond modeCycle; shift+tab reaches it too
	modeReqs  int              // SetMode requests sent, for their ids
	modelReqs int              // SetModel requests sent, for their ids

	btwReqs int               // side questions asked, for their ids
	btwSaid []protocol.SideQA // the side questions answered in this conversation, for follow-ups

	commands     []protocol.Command // slash commands to complete, sorted by name
	known        map[string]bool    // every slash command and alias claude runs, hidden ones too
	cli          cliInfo            // what claude tells about itself, for /status and the pickers
	unknown      string             // a prompt whose command claude does not know, typed once: again sends it
	suggestFor   string             // the prompt Suggest was made for
	shownPopup   Popup              // the popup popupAt is about
	popupAt      time.Time          // when the open popup came up, for its drop-in
	notice       string             // what to announce after this message, if Away
	typedAt      time.Time          // when a key last went to the prompt
	streak       bool               // the open popup came up mid-typing: keys go on to the prompt until a pause
	history      []string           // prompts sent, oldest first
	histAt       int                // the history entry in the prompt while browsing it; len(history) when not
	histDraft    Input              // the prompt as typed before browsing the history
	interrupting bool               // an Interrupt was sent: the error Result it brings ends the turn as asked
	interrupts   int                // Interrupts sent, for their ids
	cmdTurn      bool               // a slash command started the turn: it is not in the chat to be cut
	quitAt       time.Time          // when quitKey last asked to be pressed again to quit
	quitKey      string             // ctrl+c or esc
	stderr       []string           // the last lines claude wrote to stderr, for the report of its exit
	exitReport   string             // why claude exited, when it failed; "" while it runs or after a clean exit
	turnAt       time.Time          // when the current turn began
	session      string             // id of the conversation, once claude reports it

	limits  protocol.RateLimits // the rate-limit use claude reported last
	cost    float64             // the session's cost in USD, as of the last turn
	context protocol.Context    // how full main's context was at its last model call
	windows map[string]int      // the context window of each model, from the turns' results
	pending []string            // prompts typed while claude worked, sent when its turn ends
	queueAt int                 // index in pending of the prompt taken into the prompt to edit; -1 for none

	attached map[string]attachment // what the placeholders of long pastes and pictures stand for, by placeholder
}

type EntryKind int

const (
	UserEntry EntryKind = iota
	AssistantEntry
	CommandEntry // the CLI's own reply: a slash command's output or an API error
	CaretEntry   // where claude is while it works: a spin frame under what it got or said last
)

// ChatEntry is one item of the conversation: a prompt or a reply. What the agents do shows in
// the IDE, not here.
type ChatEntry struct {
	Kind   EntryKind
	Text   string
	Queued bool      // a prompt typed while claude works, drawn under the chat until it goes to claude
	Cut    bool      // a prompt whose turn the user interrupted: red, a ✗ for its arrow
	At     time.Time // when it was said, shown grey at the right; zero for not known
}

// Status feeds the status line.
type Status struct {
	Model   string
	State   string // "starting", "idle", "thinking", "responding", "running", "waiting for you", "exited"
	Busy    bool   // the agent is working: show a spinner
	Frame   int    // spinner frame
	Usage   string // rate-limit use, "5h 9% · 7d 58% · fable 3%", or the cost, "$0.0123", without limits
	Elapsed string // how long the current turn has run, e.g. "12s"; "" between turns
	Agents  int    // subagents and workflow agents at work
	Replay  bool
	Mode    string // permission mode: "default", "acceptEdits", "plan", …; "" until known

	Camera      string // e.g. "auto → tests" or "pinned · tests"
	CameraColor int    // palette slot of the agent the camera is on

	Context float64 // fraction of main's context window in use; 0 until the window is known
}

// Popup is a modal question from the agent, *QuestionPopup or *PermissionPopup, or a list the
// user opened: *ResumePopup of /resume, *PickPopup of /model and the like, *PluginsPopup of /plugins;
// or *BtwPopup, the answer to /btw.
type Popup interface{ isPopup() }

// QuestionPopup answers an AskUserQuestion call, one question at a time.
type QuestionPopup struct {
	Agent      string // key of the agent asking
	AgentName  string // the subagent asking; "" for the main agent
	AgentColor int
	RequestID  string
	Input      map[string]any // the tool input, echoed back with "answers" added
	Questions  []Question
	Current    int // index of the question on screen
}

type Question struct {
	Header  string
	Text    string
	Options []Option
	Multi   bool
	Cursor  int    // highlighted row: 0..len(Options)-1 are options, len(Options) is "Other"
	Checked []bool // multi-select marks, len(Options)+1
	Other   Input  // free text for the "Other" row
}

type Option struct{ Label, Description string }

// PermissionPopup asks whether a tool may run.
type PermissionPopup struct {
	Agent      string // key of the agent asking
	AgentName  string // the subagent asking; "" for the main agent
	AgentColor int
	RequestID  string
	ToolUseID  string // the call asking, whose scene waits for the answer
	Tool       string
	Input      map[string]any
	Detail     string // what the tool is about to do, ready to show; may span lines
	Scroll     int    // first line of Detail on screen
	Allow      bool   // highlighted button
}

// ResumePopup lists the saved conversations of the project, the latest first.
type ResumePopup struct {
	Sessions []sessions.Info
	Cursor   int
	Now      time.Time // when the list was read, for the ages
}

func (*QuestionPopup) isPopup()   {}
func (*PermissionPopup) isPopup() {}
func (*ResumePopup) isPopup()     {}

// New builds the model. send delivers messages to the agent; cwd seeds the explorer until the session reports its own.
func New(send func(protocol.Outbound), cwd string, replay bool) *Model {
	m := &Model{
		Sim:    sim.New(cwd),
		Status: Status{State: "starting", Replay: replay},
		Wrap:   true,
		Now:    time.Now(),
		send:   send,
		clock:  time.Now,
		crew:   newCrew(),

		known:    map[string]bool{},
		scroll:   ideview.Scroll{},
		attached: map[string]attachment{},
		queueAt:  -1,
	}
	m.addCommands(nil)
	m.sync()
	return m
}

func (m *Model) Init() tea.Cmd {
	return m.nextTick()
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width, m.Height = msg.Width, msg.Height
	case tickMsg:
		m.tick(time.Time(msg))
		cmd = m.nextTick()
	case AgentMsg:
		for _, ev := range msg.Events {
			m.handleEvent(ev)
		}
	case sessionsMsg:
		cmd = m.listed(msg)
	case historyMsg:
		m.resumed(msg)
	case pluginsMsg:
		m.pluginsRead(msg)
	case pluginChangedMsg:
		cmd = m.pluginChanged(msg)
	case exportedMsg:
		if msg.err != nil {
			m.toast(ToastErr, -1, "/export: %v", msg.err)
		} else {
			m.local("Exported to " + msg.path)
		}
	case tea.PasteMsg:
		cmd = m.paste(msg.Content)
	case imagesMsg:
		m.pasted(msg)
	case tea.KeyPressMsg:
		cmd = m.handleKey(msg)
	case tea.MouseWheelMsg:
		if d := wheelNotch(msg); !m.tooSmall() && d != 0 {
			m.wheel(msg.X, msg.Y, d)
			cmd = m.holdWheel()
		}
	case wheelMsg:
		cmd = m.flushWheel()
	case tea.MouseClickMsg:
		if !m.tooSmall() && msg.Button == tea.MouseLeft {
			m.sel = m.selectAt(msg.X, msg.Y)
			cmd = m.click(msg.X, msg.Y)
		}
	case tea.MouseMotionMsg:
		if msg.Button == tea.MouseLeft {
			m.dragTo(msg.X, msg.Y)
		}
	case tea.MouseReleaseMsg:
		cmd = m.copySelection()
	case tea.FocusMsg:
		m.Away = false
	case tea.BlurMsg:
		m.Away = true
	}
	m.sync()
	if m.notice != "" {
		if m.Away {
			cmd = tea.Batch(cmd, tea.Raw(drawNotice(m.notice)))
		}
		m.notice = ""
	}
	return m, cmd
}

type tickMsg time.Time

const (
	tickBusy    = 33 * time.Millisecond // 30 fps: smooth typing at half the CPU of 60
	tickAmbient = 50 * time.Millisecond // effects that move by themselves
	tickIdle    = 200 * time.Millisecond
)

func (m *Model) tickInterval() time.Duration {
	switch {
	case m.Sim.Busy() || m.Status.Busy || len(m.crew.tailers) > 0:
		return tickBusy
	case m.animating():
		return tickAmbient
	}
	return tickIdle
}

// animating reports whether something on screen moves by itself: a toast, a wipe, a popup
// dropping in.
func (m *Model) animating() bool {
	cut := ideview.Cutting(m.Sim.State(), m.Now)
	return len(m.Toasts) > 0 || cut || !m.popupAt.IsZero() && m.Now.Sub(m.popupAt) < drawPopupDrop
}

func (m *Model) nextTick() tea.Cmd {
	return tea.Tick(m.tickInterval(), func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) handleEvent(ev protocol.Event) {
	root, now := m.Sim.State().Root, m.clock()
	switch ev := ev.(type) {
	case protocol.Init:
		m.Status.Model, m.session = ev.Model, ev.SessionID
		if ev.Cwd != "" {
			m.Sim.SetRoot(ev.Cwd)
		}
		if ev.PermissionMode != "" {
			m.setMode(ev.PermissionMode)
		}
		m.cli.outputStyle, m.cli.mcp = ev.OutputStyle, ev.MCP
		m.cli.version = cmp.Or(ev.Version, m.cli.version)
		var list []protocol.Command
		for _, c := range ev.Commands {
			list = append(list, protocol.Command{Name: c})
		}
		m.addCommands(list)
		// Live sessions report init after the first prompt; keep "thinking" then.
		if !m.Status.Busy {
			m.Status.State = "idle"
		}
	case protocol.ModeChanged:
		m.setMode(ev.Mode)
	case protocol.ModelChanged:
		m.Status.Model = ev.Model
		m.local("Set model to " + ev.Model)
	case protocol.SideAnswer:
		m.btwAnswered(ev.RequestID, ev.Text, false)
	case protocol.ControlError:
		// The CLI answers requests in order, so after a refused switch the agent is in the mode it reported last.
		if strings.HasPrefix(ev.RequestID, "mode-") {
			m.Status.Mode = m.agentMode
		}
		if strings.HasPrefix(ev.RequestID, "btw-") {
			m.btwAnswered(ev.RequestID, ev.Message, true)
			break
		}
		m.toast(ToastErr, -1, "%s", ev.Message)
	case protocol.Commands:
		m.addCommands(ev.List)
		m.cli.models, m.cli.outputStyles, m.cli.account = ev.Models, ev.OutputStyles, ev.Account
		m.cli.version = cmp.Or(ev.Version, m.cli.version)
		if m.Status.Model == "" && m.StartModel != "" { // claude -p tells its model only with a turn
			m.Status.Model = m.StartModel
			for _, md := range ev.Models {
				if md.Value == m.StartModel {
					m.Status.Model = md.Resolved
				}
			}
		}
		if m.Status.State == "starting" { // claude answered: it is ready
			m.Status.State = "idle"
		}
	case protocol.Reset:
		m.Chat, m.chatLines, m.ChatScroll, m.btwSaid = nil, chatCache{}, 0, nil
		m.context = protocol.Context{}
		m.local("Conversation cleared")
	case protocol.Thinking:
		m.working("thinking")
	case protocol.TextStart:
		m.Chat = append(m.Chat, ChatEntry{Kind: AssistantEntry, At: now})
		m.working("responding")
	case protocol.TextDelta:
		if n := len(m.Chat); n == 0 || m.Chat[n-1].Kind != AssistantEntry {
			m.Chat = append(m.Chat, ChatEntry{Kind: AssistantEntry, At: now})
		}
		m.Chat[len(m.Chat)-1].Text += ev.Text
	case protocol.CLIReply:
		m.Chat = append(m.Chat, ChatEntry{Kind: CommandEntry, Text: ev.Text, At: now})
	case protocol.ToolUse:
		m.toolUse(ev, now)
		if ev.Parent == "" { // the status line is about main
			m.working("running")
		}
	case protocol.ToolResult:
		m.toolResult(ev, now)
	case protocol.TaskStarted:
		m.taskStarted(ev, now)
	case protocol.TaskProgress:
		m.taskProgress(ev, now)
	case protocol.TaskDone:
		m.taskDone(ev, now)
	case protocol.PermissionRequest:
		var asker *ide.Agent
		if a := m.agentByID(ev.AgentID); a != nil && a.Kind != ide.Main {
			asker = a
		}
		m.Sim.Hold(ev.ToolUseID) // no edit on screen before the user permits it
		p := newPopup(ev, root, asker)
		if m.Popup == nil {
			m.Popup, m.behind = p, m.Status.State
		} else {
			m.queue = append(m.queue, p)
		}
		m.Status.State = "waiting for you"
	case protocol.RateLimits:
		m.limits = ev
	case protocol.Context:
		m.context = ev
	case protocol.Result:
		interrupted := m.interrupting && ev.IsError
		m.interrupting = false
		switch {
		case interrupted:
			if !m.cmdTurn {
				m.cut()
			}
		case ev.IsError:
			m.notice = "claude stopped: " + ev.Text
		case m.Status.Busy && m.Status.Elapsed != "":
			m.notice = "claude is done · " + m.Status.Elapsed
		case m.Status.Busy:
			m.notice = "claude is done"
		}
		m.Status.State, m.Status.Busy = "idle", false
		m.cost = ev.CostUSD
		if m.windows == nil {
			m.windows = map[string]int{}
		}
		maps.Copy(m.windows, ev.ContextWindows)
		if n := len(m.Chat); ev.IsError && !interrupted && !(n > 0 && strings.TrimSpace(m.Chat[n-1].Text) == strings.TrimSpace(ev.Text)) {
			m.toast(ToastErr, -1, "%s", ev.Text) // unless the CLI said it in the conversation already
		}
		if len(m.pending) > 0 { // claude goes on with what was typed meanwhile: it is not done
			m.notice = ""
			m.flush()
		}
	case protocol.Stderr:
		m.toast(ToastErr, -1, "%s", ev.Line)
		m.stderr = append(m.stderr[max(0, len(m.stderr)-maxStderr+1):], ev.Line)
	case protocol.Exited:
		first := m.Status.State != "exited"
		m.Status.State, m.Status.Busy = "exited", false
		// The toasts go; the reason stays in the conversation.
		report := "claude exited"
		if ev.Err != nil {
			report = "Error: claude exited (" + ev.Err.Error() + ")"
		}
		for _, l := range m.stderr {
			report += "\n  " + l
		}
		if first {
			m.Chat = append(m.Chat, ChatEntry{Kind: CommandEntry, Text: report, At: now})
			if ev.Err != nil || len(m.stderr) > 0 {
				m.exitReport = strings.TrimPrefix(report, "Error: ")
			}
		}
		// Nobody is left to answer the popups or to finish the tool calls: end their scenes.
		m.Popup, m.queue = nil, nil
		m.exited(now)
		// Last, so the agents' toasts do not push it out.
		if ev.Err != nil {
			m.toast(ToastErr, -1, "claude exited: %v", ev.Err)
		} else {
			m.toast(ToastErr, -1, "claude exited")
		}
	}
}

// maxStderr is how many of claude's last stderr lines the report of its exit keeps.
const maxStderr = 6

// ExitReport says why claude exited, when it failed while the UI ran; "" otherwise. claudit
// prints it once the screen is gone.
func (m *Model) ExitReport() string { return m.exitReport }

// modeCycle is the order shift+tab walks the permission modes in, as in Claude Code.
var modeCycle = []string{"default", "acceptEdits", "plan", "auto"}

// setMode shows the permission mode the agent reports. A mode beyond modeCycle (one the session
// was started in, such as bypassPermissions) joins the cycle, so shift+tab can come back to it.
func (m *Model) setMode(mode string) {
	m.agentMode = mode
	if !slices.Contains(modeCycle, mode) {
		m.extraMode = mode
	}
	if m.Status.Replay && m.modeReqs > 0 {
		return // a replay plays on in the mode its viewer chose, whatever the recording says
	}
	m.Status.Mode = mode
}

// cycleMode switches the agent to the next permission mode. The status shows it at once;
// ModeChanged confirms it, ControlError takes it back. claude takes the switch only once it has
// the answers it waits for, and does not ask again what it asked already: the requests the new
// mode lets through are allowed first.
func (m *Model) cycleMode() {
	if m.Status.State == "exited" {
		return
	}
	cycle := modeCycle
	if m.extraMode != "" {
		cycle = append(slices.Clone(modeCycle), m.extraMode)
	}
	next := cycle[(slices.Index(cycle, cmp.Or(m.Status.Mode, "default"))+1)%len(cycle)]
	m.allowFor(next)
	m.modeReqs++
	m.send(protocol.SetMode{RequestID: fmt.Sprintf("mode-%d", m.modeReqs), Mode: next})
	m.Status.Mode = next
}

// working marks main busy with state, unless a popup is waiting for the user: that says more.
func (m *Model) working(state string) {
	if m.Popup == nil {
		m.Status.State = state
	} else {
		m.behind = state
	}
	m.Status.Busy = true
}

func (m *Model) handleKey(k tea.KeyPressMsg) tea.Cmd {
	if k.String() == "ctrl+c" {
		return m.ctrlC()
	}
	if m.Width > 0 && m.tooSmall() { // nothing on screen to aim the keys at
		return nil
	}
	// shift+tab switches the mode even over a popup: the questions the new mode would not have
	// asked are answered with it.
	if k.String() == "shift+tab" && !m.ShowHelp {
		m.cycleMode()
		return nil
	}
	// A popup takes every key; what was being typed when it came up goes nowhere, so that a
	// sentence never answers it.
	switch p := m.Popup.(type) {
	case *ResumePopup: // the user opened it: no typing runs on into it
		return m.resumeKey(p, k.String())
	case *PickPopup:
		return m.pickKey(p, k.String())
	case *BtwPopup:
		m.btwKey(p, k.String())
		return nil
	case *PluginsPopup:
		return m.pluginsKey(p, k)
	case *QuestionPopup:
		if !m.typing(k) {
			m.questionKey(p, k)
		}
		return nil
	case *PermissionPopup:
		if !m.typing(k) {
			m.permissionKey(p, k)
		}
		return nil
	}
	if m.ShowHelp {
		if k.Text == "" {
			if key := k.String(); key == "esc" || key == "f1" || key == "enter" {
				m.ShowHelp = false
			}
			return nil
		}
		m.ShowHelp = false // typing goes on to the prompt
	}
	if m.PanelFocus && k.Text != "" {
		m.PanelFocus = false // typing goes back to the prompt
	}
	if !m.PanelFocus && !m.browsing() && m.queueAt < 0 && m.suggestKey(k.String()) {
		return nil
	}
	if m.globalKey(k.String()) {
		return nil
	}
	if m.PanelFocus {
		m.panelKey(k.String())
		return nil
	}
	m.typedAt = m.clock()
	typed := string(m.Input.Text)
	defer func() {
		if string(m.Input.Text) != typed && k.String() != "up" && k.String() != "down" {
			m.histAt = len(m.history) // an edit ends browsing the history
		}
	}()
	switch k.String() {
	case "enter":
		if c := m.Input.Cursor; c > 0 && m.Input.Text[c-1] == '\\' { // "\" + enter: a new line
			m.Input.Text[c-1] = '\n'
			return nil
		}
		if m.queueAt >= 0 {
			if text := tidy(string(m.Input.Text)); text != "" {
				m.remember(text)
			}
			m.keepQueued()
			m.release()
			return nil
		}
		return m.submit()
	case "ctrl+j", "alt+enter", "shift+enter":
		m.Input.insert("\n")
	case "ctrl+v":
		return pasteClipboard
	case "up":
		if m.queueAt >= 0 || len(m.Input.Text) == 0 && !m.browsing() && len(m.pending) > 0 {
			m.walkQueue(-1)
		} else {
			m.recall(-1)
		}
	case "down":
		if m.queueAt >= 0 {
			m.walkQueue(+1)
		} else {
			m.recall(+1)
		}
	case "esc":
		switch {
		case m.canInterrupt():
			m.interrupt()
		case len(m.Input.Text) > 0 || m.queueAt >= 0:
			m.clearPrompt()
		default: // nothing left to cancel
			return m.quitTwice("esc")
		}
	case "pgup":
		m.scrollChat(10)
	case "pgdown":
		m.scrollChat(-10)
	default:
		m.Input.edit(k)
	}
	return nil
}

func (m *Model) submit() tea.Cmd {
	text := tidy(string(m.Input.Text))
	if text == "" {
		return nil
	}
	m.Input = Input{}
	return m.prompt(text)
}

// tidy cleans a prompt up as typed: no blanks around it or at the ends of its lines, no runs of
// spaces inside a line (its indentation stays), at most one blank line in a row, and a capital first
// letter when the first word is a plain lowercase one ("fix", not "calc.py" or "iOS").
func tidy(text string) string {
	var lines []string
	for l := range strings.Lines(strings.TrimSpace(text)) {
		l = strings.TrimRight(l, " \t\n")
		if l == "" && len(lines) > 0 && lines[len(lines)-1] == "" {
			continue
		}
		body := strings.TrimLeft(l, " \t")
		words := strings.FieldsFunc(body, func(r rune) bool { return r == ' ' })
		lines = append(lines, l[:len(l)-len(body)]+strings.Join(words, " "))
	}
	s := strings.Join(lines, "\n")
	word := s
	if i := strings.IndexFunc(s, unicode.IsSpace); i >= 0 {
		word = s[:i]
	}
	word = strings.TrimRight(word, ",.!?:;")
	r, n := utf8.DecodeRuneInString(word)
	if unicode.IsLower(r) && !strings.ContainsFunc(word[n:], func(r rune) bool { return !unicode.IsLower(r) && r != '\'' && r != '-' }) {
		s = string(unicode.ToUpper(r)) + s[n:]
	}
	return s
}

// prompt handles text sent from the prompt: a slash command claudit runs itself, one claude
// cannot run, or else a message for claude, sent at once or queued behind its turn. A command
// claude runs stays out of the chat; what it prints joins it.
func (m *Model) prompt(text string) tea.Cmd {
	m.ChatScroll = 0
	m.remember(text)
	cmd, isCmd := slash(text)
	if isCmd {
		if done, run := m.slashCommand(text, cmd); done {
			return run
		}
	}
	m.unknown = ""
	if m.Status.State == "exited" {
		m.toast(ToastErr, -1, "claude has exited: prompts are not sent")
		return nil
	}
	if m.Status.Busy && !m.Status.Replay { // it joins the chat once it goes to claude
		m.pending = append(m.pending, text)
		return nil
	}
	if !isCmd {
		m.Chat = append(m.Chat, ChatEntry{Kind: UserEntry, Text: text, At: m.clock()})
	}
	if m.Status.Replay {
		m.toast(ToastInfo, -1, "replay: prompts are not sent")
		return nil
	}
	m.send(m.message(text))
	m.Status.State, m.Status.Busy, m.cmdTurn = "thinking", true, isCmd
	return nil
}

// cut marks the last prompt in the chat as one whose turn the user interrupted.
func (m *Model) cut() {
	for i := len(m.Chat) - 1; i >= 0; i-- {
		if m.Chat[i].Kind == UserEntry {
			m.Chat[i].Cut = true
			return
		}
	}
}

// chatEntries is the conversation as drawn: the chat, claude's caret while it works, then the
// prompts waiting for claude's turn: it has not seen those yet.
func (m *Model) chatEntries() []ChatEntry {
	entries := slices.Clip(m.Chat)
	if m.Status.Busy {
		entries = append(entries, ChatEntry{Kind: CaretEntry, Text: drawSpinner[m.Status.Frame%len(drawSpinner)]})
	}
	for i, p := range m.pending {
		if i != m.queueAt { // that one is in the prompt
			entries = append(entries, ChatEntry{Kind: UserEntry, Text: p, Queued: true})
		}
	}
	return entries
}

// remember adds a prompt to the history, unless it repeats the last one.
func (m *Model) remember(text string) {
	if n := len(m.history); n == 0 || m.history[n-1] != text {
		m.history = append(m.history, text)
	}
	m.histAt = len(m.history)
}

// walkQueue walks the prompts waiting for claude's turn as ↑ and ↓ walk the history, each taken
// into the prompt to be edited in its place: d = -1 to an older one, +1 to a newer one, past the
// newest to an empty prompt. The one it leaves keeps what the prompt says.
func (m *Model) walkQueue(d int) {
	i := len(m.pending)
	if m.queueAt >= 0 {
		i = m.queueAt
		if m.keepQueued() && d > 0 {
			i-- // the newer ones moved up into its place
		}
	}
	if i = max(i+d, 0); i >= len(m.pending) {
		m.release()
		return
	}
	text := []rune(m.pending[i])
	m.queueAt, m.Input = i, Input{Text: text, Cursor: len(text)}
}

// keepQueued puts what the prompt says in place of the queued prompt it holds and empties the
// prompt; with nothing said, that one leaves the queue. It reports whether it left.
func (m *Model) keepQueued() bool {
	text := tidy(string(m.Input.Text))
	if text == "" {
		m.pending = slices.Delete(m.pending, m.queueAt, m.queueAt+1)
	} else {
		m.pending[m.queueAt] = text
	}
	m.queueAt, m.Input = -1, Input{}
	return text == ""
}

// clearPrompt empties the prompt; a queued prompt taken into it to edit stays as it was.
func (m *Model) clearPrompt() {
	m.Input, m.histAt, m.queueAt = Input{}, len(m.history), -1
	m.release()
}

// release sends the queue held back while one of its prompts was being edited, if claude's turn
// ended meanwhile.
func (m *Model) release() {
	if m.queueAt < 0 && len(m.pending) > 0 && !m.Status.Busy && m.Status.State != "exited" {
		m.flush()
	}
}

// flush sends the prompts typed while claude worked as one message. A slash command goes alone,
// since claude reads only a whole message as a command; what follows it waits for the next turn.
// A prompt being edited holds itself and those after it back, so they go in their order.
func (m *Model) flush() {
	end := len(m.pending)
	if m.queueAt >= 0 {
		end = m.queueAt
	}
	if end == 0 {
		return
	}
	n := 1
	if _, ok := slash(m.pending[0]); !ok {
		for n < end {
			if _, ok := slash(m.pending[n]); ok {
				break
			}
			n++
		}
	}
	m.send(m.message(strings.Join(m.pending[:n], "\n\n")))
	_, m.cmdTurn = slash(m.pending[0])
	for _, p := range m.pending[:n] {
		if !m.cmdTurn { // a command goes alone and stays out of the chat
			m.Chat = append(m.Chat, ChatEntry{Kind: UserEntry, Text: p, At: m.clock()})
		}
	}
	m.pending = m.pending[n:]
	if m.queueAt >= 0 {
		m.queueAt -= n
	}
	m.Status.State, m.Status.Busy = "thinking", true
}

// paste puts what the terminal pasted in the prompt: the pictures whose paths it is, a picture
// pasted as base64, a long text as a placeholder.
func (m *Model) paste(s string) tea.Cmd {
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(s)
	switch p := m.Popup.(type) {
	case nil:
		if paths := imagePaths(s); paths != nil {
			return loadImages(paths)
		}
		if data := pastedImage(s); data != nil {
			return func() tea.Msg { return loadImageData(data) }
		}
		if isLongPaste(s) {
			s = m.attach(attachment{text: s})
		}
		m.Input.insert(s)
	case *QuestionPopup:
		if q := &p.Questions[p.Current]; q.onOther() {
			q.Other.insert(strings.ReplaceAll(s, "\n", " "))
			q.syncOther()
		}
	}
	return nil
}
