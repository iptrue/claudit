package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/protocol"
)

// modelLocalNew is a live model whose claude told its commands, models and output styles.
func modelLocalNew(t *testing.T) (*Model, *[]protocol.Outbound) {
	t.Helper()
	m, sent := modelNew(t, false)
	modelEvents(m, protocol.Commands{
		List: []protocol.Command{{Name: "context"}, {Name: "model"}, {Name: "effort"}, {Name: "output-style"}, {Name: "config", Aliases: []string{"settings"}}},
		Models: []protocol.Model{
			{Value: "default", Name: "Default", Resolved: "claude-opus-5-5", Efforts: []string{"low", "high"}},
			{Value: "haiku", Name: "Haiku 5.5", Description: "Fast", Resolved: "claude-haiku-5-5", Efforts: []string{"low", "medium"}},
		},
		OutputStyles: []string{"default", "Concise"},
		Version:      "2.1.314",
		Account:      "a@b.c · Claude Max",
	}, protocol.Init{SessionID: "s1", Model: "claude-haiku-5-5", PermissionMode: "default", OutputStyle: "Concise",
		Commands: []string{"context", "model", "effort", "output-style", "config"},
		MCP:      []protocol.MCPServer{{Name: "docs", Status: "connected"}}})
	return m, sent
}

// Until the first turn the status names the model claude starts with; the turn tells the real one.
func TestModelStartModel(t *testing.T) {
	models := []protocol.Model{{Value: "default", Resolved: "claude-opus-5-5"}, {Value: "haiku", Resolved: "claude-haiku-5-5"}}
	m, _ := modelNew(t, false)
	m.StartModel = "haiku"
	modelEvents(m, protocol.Commands{Models: models})
	if m.Status.Model != "claude-haiku-5-5" {
		t.Errorf("alias: %q", m.Status.Model)
	}
	modelEvents(m, protocol.Init{SessionID: "s1", Model: "claude-opus-5-5"}, protocol.Commands{Models: models}) // a /resume answers initialize again
	if m.Status.Model != "claude-opus-5-5" {
		t.Errorf("after the turn: %q", m.Status.Model)
	}

	m, _ = modelNew(t, false)
	m.StartModel = "opus[1m]"
	modelEvents(m, protocol.Commands{Models: models})
	if m.Status.Model != "opus[1m]" {
		t.Errorf("unlisted: %q", m.Status.Model)
	}
}

// modelLocalSay types text into the prompt, sends it and plays what that starts.
func modelLocalSay(m *Model, text string) tea.Cmd {
	m.Input = Input{Text: []rune(text), Cursor: len([]rune(text))}
	_, cmd := m.Update(inputKey("enter"))
	return cmd
}

// A command claude does not know is held back once: claude -p would make a paid prompt of a typo.
func TestModelSlashTypo(t *testing.T) {
	m, sent := modelLocalNew(t)
	modelLocalSay(m, "/contxt")
	if len(*sent) != 0 || string(m.Input.Text) != "/contxt" || !modelResumeSaid(m, "unknown command /contxt · /context?") {
		t.Fatalf("typo: sent %#v prompt %q chat %+v", *sent, string(m.Input.Text), m.Chat)
	}
	modelLocalSay(m, "/contxt") // enter again: as asked
	if len(*sent) != 1 || modelUserText((*sent)[0]) != "/contxt" {
		t.Errorf("again: sent %#v", *sent)
	}

	// Aliases and paths are no typos.
	*sent = nil
	m.Status.Busy = false
	modelLocalSay(m, "/settings")
	m.Status.Busy = false
	modelLocalSay(m, "/tmp/notes.txt what is in it?")
	if len(*sent) != 2 {
		t.Errorf("alias and path: sent %#v", *sent)
	}

	// Before claude tells its commands there is nothing to check against.
	m, sent = modelNew(t, false)
	modelLocalSay(m, "/whatever")
	if len(*sent) != 1 {
		t.Errorf("unknown list: sent %#v", *sent)
	}
}

// /model, /effort and /output-style without an argument open a list; the choice goes as the command.
func TestModelSlashPickers(t *testing.T) {
	m, sent := modelLocalNew(t)
	modelLocalSay(m, "/model")
	p, ok := m.Popup.(*PickPopup)
	if !ok || len(p.Options) != 2 || p.Current != 1 || p.Cursor != 1 || len(*sent) != 0 {
		t.Fatalf("/model: popup %#v sent %#v", m.Popup, *sent)
	}
	if box := renderPopup(p, 60, 20); !strings.Contains(box, "haiku ✓") || !strings.Contains(box, "Haiku 5.5 · Fast") {
		t.Errorf("/model list:\n%s", box)
	}
	modelPress(m, inputKeys("up enter")...)
	if m.Popup != nil || len(*sent) != 1 || (*sent)[0] != (protocol.SetModel{RequestID: "model-1", Model: "default"}) {
		t.Fatalf("picked: popup %#v sent %#v", m.Popup, *sent)
	}

	*sent = nil
	m.Status.Busy = false
	modelLocalSay(m, "/effort")
	if p, ok := m.Popup.(*PickPopup); !ok || !reflect.DeepEqual(modelLocalLabels(p), []string{"low", "medium", "auto"}) {
		t.Errorf("/effort: %#v", m.Popup)
	}
	modelPress(m, inputKey("esc"))
	modelLocalSay(m, "/output-style")
	if p, ok := m.Popup.(*PickPopup); !ok || p.Current != 1 || !reflect.DeepEqual(modelLocalLabels(p), []string{"default", "Concise"}) {
		t.Errorf("/output-style: %#v", m.Popup)
	}
	modelPress(m, inputKey("esc"))
	modelLocalSay(m, "/model haiku") // with an argument: straight to claude
	if m.Popup != nil || len(*sent) != 1 || (*sent)[0] != (protocol.SetModel{RequestID: "model-2", Model: "haiku"}) {
		t.Errorf("/model haiku: popup %#v sent %#v", m.Popup, *sent)
	}
}

// /model with a model switches claude to it at once, even mid-turn, and out of the chat: claude -p
// would take the command as text and name the new model only with the turn after. Its answer
// names the model in the chat, above a reply still streaming; a refusal leaves the old one.
func TestModelSetModel(t *testing.T) {
	m, sent := modelLocalNew(t)
	m.Status.Busy = true
	modelLocalSay(m, "/model sonnet")
	if len(*sent) != 1 || (*sent)[0] != (protocol.SetModel{RequestID: "model-1", Model: "sonnet"}) || len(m.Chat) != 0 || len(m.pending) != 0 {
		t.Fatalf("sent %#v chat %+v pending %q", *sent, m.Chat, m.pending)
	}
	modelEvents(m, protocol.TextStart{}, protocol.TextDelta{Text: "Hal"}, protocol.ModelChanged{Model: "claude-sonnet-5-5"}, protocol.TextDelta{Text: "f"})
	if m.Status.Model != "claude-sonnet-5-5" {
		t.Errorf("model %q", m.Status.Model)
	}
	want := []ChatEntry{{Kind: CommandEntry, Text: "Set model to claude-sonnet-5-5", At: modelT0}, {Kind: AssistantEntry, Text: "Half", At: modelT0}}
	if !reflect.DeepEqual(m.Chat, want) {
		t.Errorf("chat %+v", m.Chat)
	}
	modelLocalSay(m, "/model nonsense")
	modelEvents(m, protocol.ControlError{RequestID: "model-2", Message: "Model 'nonsense' not found"})
	if m.Status.Model != "claude-sonnet-5-5" || !modelResumeSaid(m, "Error: Model 'nonsense' not found") {
		t.Errorf("refused: model %q chat %+v", m.Status.Model, m.Chat)
	}
}

func modelLocalLabels(p *PickPopup) []string {
	var ls []string
	for _, o := range p.Options {
		ls = append(ls, o.Label)
	}
	return ls
}

func TestModelSlashStatus(t *testing.T) {
	m, sent := modelLocalNew(t)
	modelLocalSay(m, "/status")
	if len(*sent) != 0 || len(m.Chat) != 1 || m.Chat[0].Kind != CommandEntry {
		t.Fatalf("sent %#v chat %+v", *sent, m.Chat)
	}
	for _, want := range []string{"Version      claude 2.1.314", "Session      s1", "Account      a@b.c · Claude Max",
		"Model        claude-haiku-5-5", "Output style Concise", "MCP servers  docs (connected)"} {
		if !strings.Contains(m.Chat[0].Text, want) {
			t.Errorf("status lacks %q:\n%s", want, m.Chat[0].Text)
		}
	}
}

func TestModelSlashExportAndCopy(t *testing.T) {
	m, _ := modelLocalNew(t)
	if cmd := modelLocalSay(m, "/copy"); cmd != nil || !modelResumeSaid(m, "no reply to copy yet") {
		t.Errorf("/copy with nothing said: %v %+v", cmd, m.Chat)
	}
	m.Chat = []ChatEntry{
		{Kind: UserEntry, Text: "add div", At: modelT0},
		{Kind: AssistantEntry, Text: "Added `div`.", At: modelT0},
		{Kind: CommandEntry, Text: "5h 9%", At: modelT0},
	}
	cmd := modelLocalSay(m, "/copy")
	if cmd == nil || fmt.Sprint(cmd()) != "Added `div`." || m.Chat[len(m.Chat)-1].Text != "Copied claude's last reply" {
		t.Errorf("/copy: %v, chat %+v", cmd, m.Chat)
	}

	cmd = modelLocalSay(m, "/export notes.md")
	if cmd == nil {
		t.Fatal("/export: no command")
	}
	m.Update(cmd())
	path := filepath.Join(m.Sim.State().Root, "notes.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"**12:00 ›** add div", "Added `div`.", "```\n5h 9%\n```"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("export lacks %q:\n%s", want, data)
		}
	}
	if e := m.Chat[len(m.Chat)-1]; e.Kind != CommandEntry || e.Text != "Exported to "+path {
		t.Errorf("chat %+v", m.Chat)
	}
}

// shift+tab over a permission request switches the mode and answers what the new mode allows,
// first: claude takes the switch only once it has its answers.
func TestModelModeOverRequests(t *testing.T) {
	m, sent := modelLocalNew(t)
	modelEvents(m,
		protocol.PermissionRequest{RequestID: "e1", ToolName: "Edit", ToolUseID: "t1", Input: map[string]any{"file_path": "a"}},
		protocol.PermissionRequest{RequestID: "b1", ToolName: "Bash", ToolUseID: "t2", Input: map[string]any{"command": "ls"}},
		protocol.PermissionRequest{RequestID: "w1", ToolName: "Write", ToolUseID: "t3", Input: map[string]any{"file_path": "b"}},
	)
	modelPress(m, inputKey("shift+tab")) // default → acceptEdits
	var got []string
	for _, o := range *sent {
		switch o := o.(type) {
		case protocol.Allow:
			got = append(got, "allow "+o.RequestID)
		case protocol.SetMode:
			got = append(got, "mode "+o.Mode)
		}
	}
	if want := []string{"allow w1", "allow e1", "mode acceptEdits"}; !reflect.DeepEqual(got, want) {
		t.Errorf("sent %v, want %v", got, want)
	}
	if p, ok := m.Popup.(*PermissionPopup); !ok || p.RequestID != "b1" || len(m.queue) != 0 || m.Status.Mode != "acceptEdits" {
		t.Errorf("popup %#v queue %d mode %q", m.Popup, len(m.queue), m.Status.Mode)
	}

	// Into auto mode nothing is allowed: its classifier judges the calls to come, the user those asked.
	*sent = nil
	m.Status.Mode = "plan"
	modelPress(m, inputKey("shift+tab"))
	if len(*sent) != 1 || (*sent)[0] != (protocol.SetMode{RequestID: "mode-2", Mode: "auto"}) || m.Popup == nil {
		t.Errorf("auto: sent %#v popup %#v", *sent, m.Popup)
	}

	// Into bypass everything goes.
	*sent = nil
	m.extraMode = "bypassPermissions"
	modelPress(m, inputKey("shift+tab"))
	if a, ok := (*sent)[0].(protocol.Allow); len(*sent) != 2 || !ok || a.RequestID != "b1" || m.Popup != nil {
		t.Errorf("bypass: sent %#v popup %#v", *sent, m.Popup)
	}
}

// /btw asks beside the conversation while claude works: the popup shows the answer, which joins
// neither the chat nor the turn, and goes along with the next side question.
func TestModelBtw(t *testing.T) {
	m, sent := modelLocalNew(t)
	m.Status.Busy = true
	modelPress(m, inputTyped("/btw what runs?")...)
	modelPress(m, inputKey("enter"))
	want := []protocol.Outbound{protocol.SideQuestion{RequestID: "btw-1", Question: "what runs?"}}
	if !reflect.DeepEqual(*sent, want) || len(m.pending) != 0 || len(m.Chat) != 0 {
		t.Fatalf("sent %v, pending %q, chat %+v", *sent, m.pending, m.Chat)
	}
	p, ok := m.Popup.(*BtwPopup)
	if !ok || !strings.Contains(ansi.Strip(renderPopup(p, 60, 20)), "Thinking…") {
		t.Fatalf("popup %#v", m.Popup)
	}
	modelEvents(m, protocol.SideAnswer{RequestID: "btw-1", Text: "**ping**, still running"})
	if box := ansi.Strip(renderPopup(p, 60, 20)); !strings.Contains(box, "/btw what runs?") || !strings.Contains(box, "ping, still running") {
		t.Fatalf("answer not shown:\n%s", box)
	}
	modelPress(m, inputKey("esc"))
	if m.Popup != nil || !m.Status.Busy || len(m.Chat) != 0 {
		t.Fatalf("esc: popup %v, busy %v, chat %+v", m.Popup, m.Status.Busy, m.Chat)
	}

	// A follow-up carries what was said; an answer to a popup closed meanwhile is dropped.
	*sent = nil
	modelPress(m, inputTyped("/btw and then?")...)
	modelPress(m, inputKey("enter"), inputKey("esc"))
	want = []protocol.Outbound{protocol.SideQuestion{RequestID: "btw-2", Question: "and then?",
		History: []protocol.SideQA{{Question: "what runs?", Response: "**ping**, still running"}}}}
	if !reflect.DeepEqual(*sent, want) {
		t.Fatalf("follow-up sent %v", *sent)
	}
	modelEvents(m, protocol.SideAnswer{RequestID: "btw-2", Text: "late"})
	if m.Popup != nil || len(m.btwSaid) != 1 {
		t.Fatalf("late answer: popup %v, said %v", m.Popup, m.btwSaid)
	}

	// A refusal shows in the popup, not in the chat; /clear forgets what was said.
	modelPress(m, inputTyped("/btw x")...)
	modelPress(m, inputKey("enter"))
	modelEvents(m, protocol.ControlError{RequestID: "btw-3", Message: "side_question timed out"})
	if p := m.Popup.(*BtwPopup); !p.Failed || p.Answer != "side_question timed out" || modelResumeSaid(m, "timed out") {
		t.Fatalf("refusal: popup %+v, chat %+v", p, m.Chat)
	}
	modelEvents(m, protocol.Reset{})
	if m.btwSaid != nil {
		t.Errorf("/clear kept %v", m.btwSaid)
	}
}

// /btw needs a question and a live claude.
func TestModelBtwRefused(t *testing.T) {
	for _, tc := range []struct {
		name   string
		replay bool
		exited bool
		typed  string
	}{
		{"no question", false, false, "/btw"},
		{"replay", true, false, "/btw why?"},
		{"exited", false, true, "/btw why?"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, sent := modelNew(t, tc.replay)
			if tc.exited {
				modelEvents(m, protocol.Exited{})
			}
			modelPress(m, inputTyped(tc.typed)...)
			modelPress(m, inputKey("enter"))
			if m.Popup != nil || len(*sent) != 0 || !modelResumeSaid(m, "/btw") {
				t.Fatalf("popup %v, sent %v, chat %+v", m.Popup, *sent, m.Chat)
			}
		})
	}
}
