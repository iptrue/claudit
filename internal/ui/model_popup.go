package ui

import (
	"maps"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/pixelbender/claudit/internal/ide"
	"github.com/pixelbender/claudit/internal/protocol"
)

// newPopup turns a permission request into the popup that answers it. asker is the subagent
// asking, nil for the main agent.
func newPopup(r protocol.PermissionRequest, root string, asker *ide.Agent) Popup {
	key, name, color := "main", "", 0
	if asker != nil {
		key, name, color = asker.Key, asker.Name, asker.Color
	}
	if r.ToolName == "AskUserQuestion" {
		if qs := parseQuestions(r.Input); len(qs) > 0 {
			return &QuestionPopup{Agent: key, AgentName: name, AgentColor: color, RequestID: r.RequestID, Input: r.Input, Questions: qs}
		}
	}
	return &PermissionPopup{
		Agent:      key,
		AgentName:  name,
		AgentColor: color,
		RequestID:  r.RequestID,
		ToolUseID:  r.ToolUseID,
		Tool:       r.ToolName,
		Input:      r.Input,
		Detail:     permissionDetail(r.ToolName, r.Input, root),
		Allow:      true,
	}
}

// parseQuestions reads AskUserQuestion input: {"questions":[{"question","header","options":[{"label","description"}],"multiSelect"}]}.
func parseQuestions(in map[string]any) []Question {
	raw, _ := in["questions"].([]any)
	var qs []Question
	for _, r := range raw {
		qm, ok := r.(map[string]any)
		if !ok {
			continue
		}
		q := Question{Header: inputString(qm, "header"), Text: inputString(qm, "question")}
		q.Multi, _ = qm["multiSelect"].(bool)
		opts, _ := qm["options"].([]any)
		for _, o := range opts {
			if om, ok := o.(map[string]any); ok {
				q.Options = append(q.Options, Option{Label: inputString(om, "label"), Description: inputString(om, "description")})
			}
		}
		q.Checked = make([]bool, len(q.Options)+1)
		qs = append(qs, q)
	}
	return qs
}

func (q *Question) onOther() bool { return q.Cursor == len(q.Options) }

// syncOther checks the "Other" row of a multi-select question exactly when it holds text.
func (q *Question) syncOther() {
	if q.Multi {
		q.Checked[len(q.Options)] = strings.TrimSpace(string(q.Other.Text)) != ""
	}
}

// answer is what the question currently answers; empty means it is not answerable yet.
func (q *Question) answer() string {
	other := strings.TrimSpace(string(q.Other.Text))
	if !q.Multi {
		if q.onOther() {
			return other
		}
		return q.Options[q.Cursor].Label
	}
	var picked []string
	for i, o := range q.Options {
		if q.Checked[i] {
			picked = append(picked, o.Label)
		}
	}
	if q.Checked[len(q.Options)] && other != "" {
		picked = append(picked, other)
	}
	return strings.Join(picked, ", ")
}

func (m *Model) questionKey(p *QuestionPopup, k tea.KeyPressMsg) {
	q := &p.Questions[p.Current]
	switch key := k.String(); {
	case key == "esc":
		m.closePopup(protocol.Deny{RequestID: p.RequestID, Message: "The user dismissed the question."}, "thinking")
	case key == "up":
		q.Cursor = max(q.Cursor-1, 0)
	case key == "down":
		q.Cursor = min(q.Cursor+1, len(q.Options))
	case key == "enter":
		m.answerQuestion(p)
	case key == "space" && q.Multi && !q.onOther():
		q.Checked[q.Cursor] = !q.Checked[q.Cursor]
	case q.onOther():
		if q.Other.edit(k) {
			q.syncOther()
		}
	}
}

// answerQuestion takes the answer to the question on screen and goes on to the next one, or sends
// all the answers after the last; a question not answered yet stays. The questions and their
// answers join the chat, as the agent's words and the user's.
func (m *Model) answerQuestion(p *QuestionPopup) {
	if p.Questions[p.Current].answer() == "" {
		return
	}
	if p.Current < len(p.Questions)-1 {
		p.Current++
		return
	}
	answers := make(map[string]string, len(p.Questions))
	now := m.clock()
	for i := range p.Questions {
		q := &p.Questions[i]
		answers[q.Text] = q.answer()
		asked := q.Text
		if p.AgentName != "" { // a subagent's: the chat's own words are main's
			asked = p.AgentName + " · " + asked
		}
		m.Chat = append(m.Chat, ChatEntry{Kind: AssistantEntry, Text: asked, At: now}, ChatEntry{Kind: UserEntry, Text: answers[q.Text], At: now})
	}
	in := make(map[string]any, len(p.Input)+1)
	maps.Copy(in, p.Input)
	in["answers"] = answers
	m.closePopup(protocol.Allow{RequestID: p.RequestID, Input: in}, "thinking")
}

func (m *Model) permissionKey(p *PermissionPopup, k tea.KeyPressMsg) {
	answer := func(ok bool) { m.answerPermission(p, ok) }
	switch k.String() {
	case "left", "right", "tab":
		p.Allow = !p.Allow
	case "up":
		p.Scroll--
	case "down":
		p.Scroll++
	case "pgup":
		p.Scroll -= 10
	case "pgdown":
		p.Scroll += 10
	case "y":
		answer(true)
	case "n", "esc":
		answer(false)
	case "enter":
		answer(p.Allow)
	}
}

// answerPermission allows or denies the request of p; the scene of the call plays or goes.
func (m *Model) answerPermission(p *PermissionPopup, ok bool) {
	if ok {
		m.Sim.Release(p.ToolUseID)
		m.closePopup(protocol.Allow{RequestID: p.RequestID, Input: p.Input}, "running")
		return
	}
	m.Sim.Drop(p.ToolUseID)
	m.crew.denied[p.ToolUseID] = true
	m.closePopup(protocol.Deny{RequestID: p.RequestID, Message: "The user denied this tool call."}, "thinking")
}

// replayAutoAllow is how long a replay shows a permission popup before it allows the call itself,
// in a permission mode that would not have asked: the audience reads it, nobody presses y.
const replayAutoAllow = 1200 * time.Millisecond

// replayAllow lets a replay play on through the permission popups its mode would not show.
func (m *Model) replayAllow(now time.Time) {
	p, ok := m.Popup.(*PermissionPopup)
	if !ok || !m.Status.Replay || m.popupAt.IsZero() || now.Sub(m.popupAt) < replayAutoAllow || !modeAllows(m.Status.Mode, p.Tool) {
		return
	}
	m.answerPermission(p, true)
}

// modeAllows reports whether permission mode lets tool run without asking: auto and bypass let
// everything through, accept edits the edits.
func modeAllows(mode, tool string) bool {
	switch mode {
	case "auto", "bypassPermissions":
		return true
	case "acceptEdits":
		return tool == "Edit" || tool == "Write" || tool == "NotebookEdit"
	}
	return false
}

// allowFor allows the permission requests on screen and waiting behind it that mode lets through.
// Auto mode lets a call through only once its classifier has judged it: what was asked already
// stays with the user.
func (m *Model) allowFor(mode string) {
	if mode == "auto" {
		return
	}
	var keep []Popup
	for _, q := range m.queue {
		if p, ok := q.(*PermissionPopup); ok && modeAllows(mode, p.Tool) {
			m.Sim.Release(p.ToolUseID)
			m.send(protocol.Allow{RequestID: p.RequestID, Input: p.Input})
			continue
		}
		keep = append(keep, q)
	}
	m.queue = keep
	if p, ok := m.Popup.(*PermissionPopup); ok && modeAllows(mode, p.Tool) {
		m.answerPermission(p, true)
	}
}

// closePopup sends the answer and shows the next queued popup, if any; otherwise the status becomes
// state. Another agent's answer does not change what main does: the status goes back to that.
func (m *Model) closePopup(o protocol.Outbound, state string) {
	m.send(o)
	if len(m.queue) > 0 {
		m.Popup, m.queue = m.queue[0], m.queue[1:]
		return
	}
	var asker string
	switch p := m.Popup.(type) {
	case *QuestionPopup:
		asker = p.AgentName
	case *PermissionPopup:
		asker = p.AgentName
	}
	if asker != "" {
		state = "idle"
		if m.Status.Busy {
			state = m.behind
		}
	}
	m.Popup = nil
	m.Status.State = state
}

// popupNotice is what to announce when p comes up: who asks what; "" for no popup.
func popupNotice(p Popup) string {
	who := func(name string) string {
		if name == "" {
			return "claude"
		}
		return name
	}
	switch p := p.(type) {
	case *PermissionPopup:
		return who(p.AgentName) + " wants to use " + p.Tool
	case *QuestionPopup:
		if len(p.Questions) > 0 {
			return who(p.AgentName) + " asks: " + p.Questions[0].Text
		}
		return who(p.AgentName) + " asks you"
	}
	return ""
}

// popupGrace is the pause after typing into the prompt before a popup that came up meanwhile
// takes keys: the letters of a sentence do not answer it ("y" allows).
const popupGrace = time.Second

// typing reports whether k belongs to what the user was typing when the popup came up: text,
// enter or backspace before a pause. Such keys go nowhere.
func (m *Model) typing(k tea.KeyPressMsg) bool {
	m.streak = m.streak && m.clock().Sub(m.typedAt) < popupGrace
	key := k.String()
	return m.streak && (k.Text != "" || key == "enter" || key == "backspace")
}

// popupAgent is the key of the agent asking in p; "" for no popup.
func popupAgent(p Popup) string {
	switch p := p.(type) {
	case *PermissionPopup:
		return p.Agent
	case *QuestionPopup:
		return p.Agent
	}
	return ""
}
