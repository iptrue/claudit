package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/ide"
	"github.com/pixelbender/claudit/internal/protocol"
)

func inspectionTop(m *Model, height int) string {
	return strings.Join(strings.Split(ansi.Strip(m.View().Content), "\n")[:height], "\n")
}

func inspectionFile(t *testing.T) (*Model, int) {
	t.Helper()
	m, _ := modelMouseNew(t)
	var lines []string
	for i := range 80 {
		lines = append(lines, fmt.Sprintf("reading line %02d", i))
	}
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	m.Sim.SetRoot(filepath.Dir(path))
	modelEvents(m, protocol.Init{SessionID: "one"})
	modelMouseClick(t, m, "notes.txt")
	ideH, _ := drawLayout(m.Height, drawChatCount(&m.chatLines, m.chatEntries(), m.Width))
	return m, ideH
}

func TestInspectionShowsSubmittedPromptImmediately(t *testing.T) {
	for _, busy := range []bool{false, true} {
		t.Run(fmt.Sprintf("busy=%v", busy), func(t *testing.T) {
			m, sent := modelMouseNew(t)
			m.Status.Busy = busy
			m.View()
			modelPress(m, inputTyped("hello")...)
			m.View()
			held := m.ideState()
			modelPress(m, inputKey("enter"))
			out := ansi.Strip(m.View().Content)
			if !strings.Contains(out, "› Hello") {
				t.Fatalf("submitted prompt is hidden during inspection:\n%s", out)
			}
			if m.inspection == nil || m.ideState() != held {
				t.Fatal("submitting a prompt released the inspected IDE content")
			}
			if busy {
				if len(*sent) != 0 || len(m.pending) != 1 || m.pending[0] != "Hello" {
					t.Fatal("the queued prompt was sent early or lost")
				}
			} else if len(*sent) != 1 || modelUserText((*sent)[0]) != "Hello" {
				t.Fatal("the prompt was not sent immediately")
			}
		})
	}
}

func TestInspectionFileSurvivesAgentActivity(t *testing.T) {
	m, height := inspectionFile(t)
	before := inspectionTop(m, height)
	modelEvents(m,
		protocol.ToolUse{ID: "b1", Name: "Bash", Input: map[string]any{"command": "pwd", "description": "Background command"}},
		protocol.ToolResult{ID: "b1", Output: "background output"},
	)
	for _, d := range []time.Duration{time.Second, 4999 * time.Millisecond} {
		m.Update(tickMsg(modelT0.Add(d)))
		if got := inspectionTop(m, height); got != before {
			t.Fatalf("agent replaced the inspected file at %v:\n%s", d, got)
		}
	}
	if m.Sim.State().Viewed == "" || m.Sim.FindAgent("main").Focus != ide.Terminal {
		t.Fatal("protecting the view paused the live simulation, or the agent's call let the file go")
	}
	m.Update(tickMsg(modelT0.Add(5 * time.Second)))
	if got := inspectionTop(m, height); !strings.Contains(got, "reading line 00") || strings.Contains(got, "background output") {
		t.Errorf("the file the user clicked gave way after five seconds:\n%s", got)
	}
	modelPress(m, inputKey("alt+0"))
	if got := inspectionTop(m, height); !strings.Contains(got, "background output") {
		t.Errorf("alt+0 did not go back to the agent:\n%s", got)
	}
}

func TestInspectionScrollExtendsCooldown(t *testing.T) {
	m, height := inspectionFile(t)
	modelEvents(m, protocol.ToolUse{ID: "b1", Name: "Bash", Input: map[string]any{"command": "pwd"}})
	m.Update(tickMsg(modelT0.Add(4 * time.Second)))
	m.View()
	m.Update(tea.MouseWheelMsg{X: 50, Y: 8, Button: tea.MouseWheelDown})
	afterScroll := inspectionTop(m, height)
	if !strings.Contains(afterScroll, "reading line 03") {
		t.Fatalf("manual scrolling did not move the held file:\n%s", afterScroll)
	}
	m.Update(tickMsg(modelT0.Add(8999 * time.Millisecond)))
	if got := inspectionTop(m, height); got != afterScroll {
		t.Errorf("scrolling did not extend the cooldown:\n%s", got)
	}
	m.Update(tickMsg(modelT0.Add(9 * time.Second)))
	if got := inspectionTop(m, height); got == afterScroll {
		t.Error("view stayed held after the renewed cooldown expired")
	}
}

func TestInspectionSelectionAndKeyboard(t *testing.T) {
	m, height := inspectionFile(t)
	x, y := modelMouseAt(t, m, "reading line 00")
	m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: x + 5, Y: y, Button: tea.MouseLeft})
	before := inspectionTop(m, height)
	modelEvents(m, protocol.ToolUse{ID: "b1", Name: "Bash", Input: map[string]any{"command": "pwd"}})
	m.Update(tickMsg(modelT0.Add(6 * time.Second)))
	if got := inspectionTop(m, height); got != before {
		t.Fatal("view moved while the mouse button was held")
	}
	m.Update(tea.MouseReleaseMsg{X: x + 5, Y: y, Button: tea.MouseLeft})
	m.Update(tickMsg(modelT0.Add(10 * time.Second)))
	modelPress(m, inputKey("alt+z"))
	before = inspectionTop(m, height)
	m.Update(tickMsg(modelT0.Add(14999 * time.Millisecond)))
	if got := inspectionTop(m, height); got != before {
		t.Fatal("keyboard interaction did not extend the cooldown")
	}
	m.Update(tickMsg(modelT0.Add(15 * time.Second)))
	if got := inspectionTop(m, height); got == before {
		t.Fatal("automatic view did not resume after keyboard cooldown")
	}
}

func TestInspectionBashClickKeepsTerminal(t *testing.T) {
	m, _ := modelMouseNew(t)
	var output []string
	for i := range 60 {
		output = append(output, fmt.Sprintf("output %02d", i))
	}
	modelEvents(m,
		protocol.ToolUse{ID: "b1", Name: "Bash", Input: map[string]any{"command": "pwd", "description": "Inspect this command"}},
		protocol.ToolResult{ID: "b1", Output: strings.Join(output, "\n")},
	)
	for d := time.Duration(0); d <= 10*time.Second; d += 100 * time.Millisecond {
		m.Update(tickMsg(modelT0.Add(d)))
	}
	modelEvents(m, protocol.Result{})
	modelMouseClick(t, m, "Inspect this command")
	height, _ := drawLayout(m.Height, drawChatCount(&m.chatLines, m.chatEntries(), m.Width))
	before := inspectionTop(m, height)
	if !strings.Contains(before, "# Inspect this command") {
		t.Fatalf("the clicked command is not visible:\n%s", before)
	}
	path := filepath.Join(t.TempDir(), "new.txt")
	if err := os.WriteFile(path, []byte("new file opened by the agent"), 0o644); err != nil {
		t.Fatal(err)
	}
	modelEvents(m,
		protocol.ToolUse{ID: "r1", Name: "Read", Input: map[string]any{"file_path": path}},
		protocol.ToolResult{ID: "r1", Output: "new file opened by the agent"},
	)
	for _, d := range []time.Duration{11 * time.Second, 14999 * time.Millisecond} {
		m.Update(tickMsg(modelT0.Add(d)))
		if got := inspectionTop(m, height); got != before {
			t.Errorf("the agent replaced the inspected terminal at %v:\n%s", d, got)
		}
	}
	// Explicit auto-follow takes effect immediately, without waiting for the cooldown.
	modelPress(m, inputKey("alt+0"))
	if got := inspectionTop(m, height); got == before || !strings.Contains(got, "new file opened by the agent") {
		t.Errorf("explicit auto-follow did not resume:\n%s", got)
	}
}

func TestInspectionProtectsFileContentAndCaret(t *testing.T) {
	m, height := inspectionFile(t)
	before := inspectionTop(m, height)
	path := filepath.Join(m.Sim.State().Root, "notes.txt")
	modelEvents(m,
		protocol.ToolUse{ID: "e1", Name: "Edit", Input: map[string]any{"file_path": path, "old_string": "reading line 00", "new_string": "changed by the agent"}},
		protocol.ToolResult{ID: "e1", Output: "ok"},
	)
	for d := 100 * time.Millisecond; d < 5*time.Second; d += 100 * time.Millisecond {
		m.Update(tickMsg(modelT0.Add(d)))
	}
	if got := m.Sim.State().FindTab(path).Buf.Text(); !strings.Contains(got, "changed by the agent") {
		t.Fatalf("the live edit did not run: %q", got)
	}
	if got := inspectionTop(m, height); got != before {
		t.Errorf("live buffer changes moved the inspected file:\n%s", got)
	}
}

func TestInspectionCanOpenNewBashCall(t *testing.T) {
	m, _ := inspectionFile(t)
	modelPress(m, inputKey("alt+0"))
	m.Chat = append(m.Chat, ChatEntry{Kind: AssistantEntry, Text: strings.Repeat("earlier reply\n", 5)})
	modelMouseClick(t, m, "notes.txt")
	height := m.ideHeight
	modelEvents(m,
		protocol.ToolUse{ID: "b2", Name: "Bash", Input: map[string]any{"command": "pwd", "description": "New Bash link"}},
		protocol.ToolResult{ID: "b2", Output: "new result"},
	)
	for _, d := range []time.Duration{time.Second, 4 * time.Second} {
		m.Update(tickMsg(modelT0.Add(d)))
	}
	modelMouseClick(t, m, "New Bash link")
	if got := inspectionTop(m, height); !strings.Contains(got, "# New Bash link") || !strings.Contains(got, "pwd") {
		t.Errorf("a new Bash call could not be opened during cooldown:\n%s", got)
	}
}

func TestInspectionCanSelectNewWorkflowAgent(t *testing.T) {
	for _, tc := range []struct {
		queued bool
		key    string
	}{{false, "enter"}, {true, "enter"}, {true, "alt+2"}} {
		t.Run(fmt.Sprintf("queued=%v/%s", tc.queued, tc.key), func(t *testing.T) {
			m, _ := modelMouseNew(t)
			modelWorkflow(t, m)
			item := protocol.WorkflowItem{Index: 1, Title: "next agent", AgentID: "old", State: "progress"}
			if tc.queued {
				item.AgentID = ""
			}
			modelProgress(m, item)
			modelPress(m, inputKey("tab"))
			m.View()
			if tc.queued {
				m.PanelSel = modelChildren(m, "wf")[0]
			} else {
				m.PanelSel = "wf"
				item.State = "done"
				modelProgress(m, item)
				item.Index = 2
			}
			item.AgentID, item.State = "next", "progress"
			modelProgress(m, item)
			a := m.Sim.FindAgent("next")
			a.Focus = ide.Terminal
			a.Term.Print(ide.Output, "new agent terminal")
			modelPress(m, inputKey(tc.key))
			if got := inspectionTop(m, m.ideHeight); !strings.Contains(got, "new agent terminal") {
				t.Errorf("the newly assigned agent could not be selected:\n%s", got)
			}
		})
	}
}
