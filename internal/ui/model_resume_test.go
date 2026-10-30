package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/protocol"
	"github.com/pixelbender/claudit/internal/sessions"
)

// modelResumeNew is a live model whose conversation is "current", with saved conversations
// where claude keeps those of its project: "a1" two hours old, "b2" three days old, and "current".
func modelResumeNew(t *testing.T) (*Model, *[]protocol.Outbound) {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	m, sent := modelNew(t, false)
	modelEvents(m, protocol.Init{SessionID: "current", Model: "m"})
	modelResumeWrite(t, m, "current", modelT0, `{"type":"ai-title","aiTitle":"This one"}`)
	modelResumeWrite(t, m, "a1", modelT0.Add(-2*time.Hour),
		`{"type":"user","message":{"role":"user","content":"add div"},"timestamp":"2026-10-28T09:15:00.000Z"}`,
		`{"type":"assistant","message":{"model":"m","content":[{"type":"text","text":"Done."}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user]"}]}}`,
		`{"type":"system","subtype":"local_command","content":"/usage"}`,
		`{"type":"system","subtype":"local_command","content":"<local-command-stdout>5h 9%</local-command-stdout>"}`,
		`{"type":"ai-title","aiTitle":"Add div"}`)
	modelResumeWrite(t, m, "b2", modelT0.Add(-72*time.Hour), `{"type":"last-prompt","lastPrompt":"Fix the tests"}`)
	return m, sent
}

// modelResumeWrite saves a conversation of lines for m's project, dated at.
func modelResumeWrite(t *testing.T, m *Model, id string, at time.Time, lines ...string) {
	t.Helper()
	dir := sessions.Dir(m.Sim.State().Root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// modelResumeSubmit sends prompt and plays the commands it starts as Bubble Tea would.
func modelResumeSubmit(m *Model, prompt string) {
	m.Input = Input{Text: []rune(prompt), Cursor: len([]rune(prompt))}
	_, cmd := m.Update(inputKey("enter"))
	modelResumeRun(m, cmd)
}

// modelResumeRun runs cmd, and the commands it leads to, feeding their messages to m.
func modelResumeRun(m *Model, cmd tea.Cmd) {
	for cmd != nil {
		_, cmd = m.Update(cmd())
	}
}

// modelResumeSaid reports whether claudit says s in the chat.
func modelResumeSaid(m *Model, s string) bool {
	return slices.ContainsFunc(m.Chat, func(e ChatEntry) bool { return e.Kind == CommandEntry && strings.Contains(e.Text, s) })
}

func TestModelResume(t *testing.T) {
	m, sent := modelResumeNew(t)
	m.Chat = []ChatEntry{{Kind: UserEntry, Text: "said before"}}

	modelResumeSubmit(m, "/resume")
	p, ok := m.Popup.(*ResumePopup)
	if !ok {
		t.Fatalf("popup %#v, chat %+v", m.Popup, m.Chat)
	}
	var ids []string
	for _, s := range p.Sessions {
		ids = append(ids, s.ID)
	}
	if !reflect.DeepEqual(ids, []string{"a1", "b2"}) { // the latest first, without the current one
		t.Errorf("listed %q", ids)
	}
	box := ansi.Strip(renderPopup(p, 60, 20))
	for _, want := range []string{"Resume a conversation  2", "› Add div", "2h ago", "Fix the tests", "3d ago"} {
		if !strings.Contains(box, want) {
			t.Errorf("popup lacks %q:\n%s", want, box)
		}
	}

	modelPress(m, inputKeys("down down up esc")...)
	if m.Popup != nil || p.Cursor != 0 || len(*sent) != 0 {
		t.Fatalf("esc: popup %#v, cursor %d, sent %#v", m.Popup, p.Cursor, *sent)
	}

	modelResumeSubmit(m, "/resume")
	_, cmd := m.Update(inputKey("enter"))
	modelResumeRun(m, cmd)
	if want := []protocol.Outbound{protocol.Resume{SessionID: "a1"}}; !reflect.DeepEqual(*sent, want) {
		t.Fatalf("sent %#v", *sent)
	}
	want := []ChatEntry{
		{Kind: UserEntry, Text: "add div", At: time.Date(2026, 10, 28, 9, 15, 0, 0, time.UTC), Cut: true}, // in the zone of the clock; interrupted
		{Kind: AssistantEntry, Text: "Done."},
		{Kind: CommandEntry, Text: "5h 9%"}, // the output of /usage: the command stays out, as live
		{Kind: CommandEntry, Text: "Resumed: Add div", At: modelT0},
	}
	if !reflect.DeepEqual(m.Chat, want) {
		t.Errorf("chat %+v", m.Chat)
	}
	if m.Popup != nil || m.Status.State != "starting" {
		t.Errorf("popup %#v, state %q, chat %+v", m.Popup, m.Status.State, m.Chat)
	}
	modelEvents(m, protocol.Commands{})
	if m.Status.State != "idle" {
		t.Errorf("state %q once claude answers", m.Status.State)
	}

	// The conversation resumed is the current one now: the other one and the old one are listed.
	modelResumeSubmit(m, "/resume")
	if p, ok := m.Popup.(*ResumePopup); !ok || len(p.Sessions) != 2 || p.Sessions[0].ID != "current" {
		t.Errorf("after resuming: %#v", m.Popup)
	}
}

func TestModelResumeQuery(t *testing.T) {
	m, sent := modelResumeNew(t)
	modelResumeSubmit(m, "/resume FIX") // one match by title: no list
	if want := []protocol.Outbound{protocol.Resume{SessionID: "b2"}}; m.Popup != nil || !reflect.DeepEqual(*sent, want) {
		t.Fatalf("popup %#v, sent %#v", m.Popup, *sent)
	}

	m, sent = modelResumeNew(t)
	modelResumeSubmit(m, "/resume A1") // by id
	if want := []protocol.Outbound{protocol.Resume{SessionID: "a1"}}; m.Popup != nil || !reflect.DeepEqual(*sent, want) {
		t.Fatalf("popup %#v, sent %#v", m.Popup, *sent)
	}

	// "Add div" and "Fix the tests" match, "This one" is the current conversation.
	m, sent = modelResumeNew(t)
	modelResumeSubmit(m, "/resume i")
	if p, ok := m.Popup.(*ResumePopup); !ok || len(p.Sessions) != 2 || len(*sent) != 0 {
		t.Errorf("popup %#v, sent %#v", m.Popup, *sent)
	}

	m, sent = modelResumeNew(t)
	modelResumeSubmit(m, "/resume nothing")
	if m.Popup != nil || len(*sent) != 0 || !modelResumeSaid(m, `no conversation matches "nothing"`) {
		t.Errorf("popup %#v, sent %#v, chat %+v", m.Popup, *sent, m.Chat)
	}
}

func TestModelResumeRefused(t *testing.T) {
	m, sent := modelResumeNew(t)
	modelResumeSubmit(m, "/resume current") // the one it is in
	if m.Popup != nil || len(*sent) != 0 || !modelResumeSaid(m, "no conversation matches") {
		t.Errorf("current: popup %#v, sent %#v, chat %+v", m.Popup, *sent, m.Chat)
	}

	m, sent = modelResumeNew(t)
	m.Status.Busy = true
	modelResumeSubmit(m, "/resume")
	if m.Popup != nil || len(*sent) != 0 || !modelResumeSaid(m, "claude is working") {
		t.Errorf("busy: popup %#v, sent %#v, chat %+v", m.Popup, *sent, m.Chat)
	}

	m, sent = modelResumeNew(t)
	m.Status.Replay = true
	modelResumeSubmit(m, "/resume")
	if m.Popup != nil || len(*sent) != 0 || !modelResumeSaid(m, "replay") || len(m.Chat) != 1 {
		t.Errorf("replay: popup %#v, sent %#v, chat %+v", m.Popup, *sent, m.Chat)
	}

	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir()) // a project without saved conversations
	m, sent = modelNew(t, false)
	modelResumeSubmit(m, "/resume")
	if m.Popup != nil || len(*sent) != 0 || !modelResumeSaid(m, "no other conversation") {
		t.Errorf("none: popup %#v, sent %#v, chat %+v", m.Popup, *sent, m.Chat)
	}
}

// A long list scrolls to keep the highlighted conversation in sight, within the rows it gets.
func TestDrawResumeScrolls(t *testing.T) {
	p := &ResumePopup{Now: modelT0, Cursor: 29}
	for i := range 30 {
		p.Sessions = append(p.Sessions, sessions.Info{ID: fmt.Sprint(i), Title: fmt.Sprintf("talk %d", i), Modified: modelT0.Add(-time.Duration(i) * time.Minute)})
	}
	lines, _ := drawResume(p, 40, 10)
	plain := ansi.Strip(strings.Join(lines, "\n"))
	if len(lines) != 10 || !strings.Contains(plain, "› talk 29") || !strings.Contains(plain, "talk 24") || strings.Contains(plain, "talk 23") {
		t.Errorf("%d lines:\n%s", len(lines), plain)
	}
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > 40 {
			t.Errorf("line %d cells wide: %q", w, ansi.Strip(l))
		}
	}
}
