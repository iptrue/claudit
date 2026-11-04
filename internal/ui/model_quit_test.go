package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/protocol"
)

const quitTasks = `{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"b1","description":"Watch CI for MR 42"},{"task_id":"a2","description":"Review MR 43"}]}`

func TestEscConfirmsUnfinishedTasks(t *testing.T) {
	m, sent := modelMouseNew(t)
	activityRaw(m, quitTasks)
	modelEvents(m, protocol.Result{})
	if modelQuitsOn(m, "esc") || modelQuitsOn(m, "esc") {
		t.Fatal("Esc quit without confirming unfinished tasks")
	}
	if m.Popup == nil {
		t.Fatal("no exit confirmation")
	}
	got := ansi.Strip(renderPopup(m.Popup, 70, 20))
	for _, want := range []string{"Watch CI for MR 42", "Review MR 43", "Stay", "Exit"} {
		if !strings.Contains(got, want) {
			t.Errorf("confirmation lacks %q:\n%s", want, got)
		}
	}
	if modelQuitsOn(m, "enter") || m.Popup != nil {
		t.Fatal("the default choice did not keep the session open")
	}
	if modelQuitsOn(m, "esc") || m.Popup != nil {
		t.Fatal("cancelling confirmation left an armed exit")
	}
	if modelQuitsOn(m, "esc") || m.Popup == nil {
		t.Fatal("exit confirmation could not be reopened")
	}
	modelPress(m, inputKey("right"))
	if !modelQuitsOn(m, "enter") {
		t.Fatal("explicit Exit did not quit")
	}
	if len(*sent) != 0 {
		t.Errorf("confirmation interrupted tasks: %#v", *sent)
	}
}

func TestQuitPopupScrollAndMouse(t *testing.T) {
	m, sent := modelMouseNew(t)
	var tasks []protocol.BackgroundTask
	for i := range 30 {
		tasks = append(tasks, protocol.BackgroundTask{ID: fmt.Sprintf("b%d", i), Description: fmt.Sprintf("Review MR %02d", i)})
	}
	modelEvents(m, protocol.BackgroundTasks{Tasks: tasks}, protocol.Result{})
	modelPress(m, inputKey("esc"), inputKey("esc"))
	if m.Popup == nil {
		t.Fatal("no confirmation")
	}
	for _, size := range [][2]int{{40, 8}, {80, 24}, {100, 40}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		drawCheckSize(t, m.View().Content, size[0], size[1])
		modelMouseAt(t, m, "[ Stay ]")
		modelMouseAt(t, m, "[ Exit ]")
	}
	modelPress(m, inputKey("pgdown"))
	if got := ansi.Strip(renderPopup(m.Popup, 70, 12)); !strings.Contains(got, "Review MR 10") || strings.Contains(got, "Review MR 00") {
		t.Errorf("task list did not scroll:\n%s", got)
	}
	if cmd := m.clickPopup(m.Popup, 1); cmd == nil {
		t.Error("clicking Exit did not quit")
	} else if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("clicking Exit returned no QuitMsg")
	}
	modelMouseClick(t, m, "[ Stay ]")
	if m.Popup != nil || len(*sent) != 0 {
		t.Errorf("Stay did not preserve tasks: popup %T sent %#v", m.Popup, *sent)
	}
}

func TestQuitCancelRestoresQueuedPermission(t *testing.T) {
	m, _ := modelNew(t, false)
	activityRaw(m, quitTasks)
	modelEvents(m, protocol.Result{})
	modelPress(m, inputKey("esc"), inputKey("esc"))
	modelEvents(m, protocol.PermissionRequest{RequestID: "p1", ToolName: "Bash", Input: map[string]any{"command": "pwd"}})
	if modelQuitsOn(m, "ctrl+c") {
		t.Fatal("Ctrl+C bypassed the open confirmation")
	}
	p, ok := m.Popup.(*PermissionPopup)
	if !ok || p.RequestID != "p1" {
		t.Fatalf("queued permission was lost: %#v", m.Popup)
	}
}

func TestQuitCancelAfterQueuedPermissionAllowed(t *testing.T) {
	m, _ := modelNew(t, false)
	activityRaw(m, quitTasks)
	modelEvents(m, protocol.Result{})
	modelPress(m, inputKey("esc"), inputKey("esc"))
	modelEvents(m, protocol.PermissionRequest{RequestID: "p1", ToolName: "Edit", Input: map[string]any{"file_path": "/tmp/file.go", "old_string": "x", "new_string": "y"}})
	modelPress(m, inputKey("shift+tab"), inputKey("enter"))
	if m.Popup != nil || m.Status.State != "idle" {
		t.Errorf("cancel left a stale permission wait: popup %T state %q", m.Popup, m.Status.State)
	}
}

func TestEscInterruptsBeforeConfirmingExit(t *testing.T) {
	m, sent := modelNew(t, false)
	activityRaw(m, quitTasks)
	modelEvents(m, protocol.Thinking{})
	if modelQuitsOn(m, "esc") || m.Popup != nil || m.Status.State != "interrupting" {
		t.Fatalf("Esc did not interrupt: state %q popup %T", m.Status.State, m.Popup)
	}
	if len(*sent) != 1 {
		t.Fatalf("sent %#v", *sent)
	}
	if _, ok := (*sent)[0].(protocol.Interrupt); !ok {
		t.Fatalf("sent %#v, want Interrupt", (*sent)[0])
	}
	if modelQuitsOn(m, "esc") || m.Popup != nil || len(*sent) != 1 {
		t.Fatal("repeated Esc opened exit confirmation while interrupting")
	}
}

func TestInterruptEscDoesNotCountTowardExit(t *testing.T) {
	for _, armedBefore := range []bool{false, true} {
		t.Run(fmt.Sprint("previous exit press=", armedBefore), func(t *testing.T) {
			m, _ := modelNew(t, false)
			activityRaw(m, quitTasks)
			modelEvents(m, protocol.Result{})
			if armedBefore && modelQuitsOn(m, "esc") {
				t.Fatal("the first idle Esc quit")
			}
			modelEvents(m, protocol.Thinking{})
			if modelQuitsOn(m, "esc") || m.Popup != nil || m.Status.State != "interrupting" {
				t.Fatal("Esc did not cancel the running turn")
			}
			modelEvents(m, protocol.Result{IsError: true})
			if modelQuitsOn(m, "esc") || m.Popup != nil {
				t.Fatal("the cancel Esc counted toward exit confirmation")
			}
			if modelQuitsOn(m, "esc") || m.Popup == nil {
				t.Fatal("two fresh idle Esc presses did not open confirmation")
			}
		})
	}
}

func TestQuitTaskListStaysCurrent(t *testing.T) {
	m, _ := modelMouseNew(t)
	activityRaw(m, quitTasks)
	modelEvents(m, protocol.Result{})
	modelPress(m, inputKey("esc"), inputKey("esc"))
	if m.Popup == nil {
		t.Fatal("no confirmation")
	}
	activityRaw(m, `{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"a2","description":"Review MR 43"}]}`)
	got := ansi.Strip(renderPopup(m.Popup, 70, 20))
	if strings.Contains(got, "Watch CI") || !strings.Contains(got, "Review MR 43") {
		t.Errorf("stale task list:\n%s", got)
	}
	activityRaw(m, `{"type":"system","subtype":"background_tasks_changed","tasks":[]}`)
	if m.Popup == nil || modelQuitsOn(m, "esc") || m.Popup != nil {
		t.Fatal("task completion or Esc dismissed the confirmation by quitting")
	}
	if modelQuitsOn(m, "esc") || !modelQuitsOn(m, "esc") {
		t.Fatal("without unfinished tasks Esc no longer quits normally")
	}
}
