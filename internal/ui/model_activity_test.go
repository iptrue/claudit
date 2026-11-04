package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/protocol"
)

func activityRaw(m *Model, line string) {
	modelEvents(m, protocol.Parse([]byte(line))...)
}

func activityStatus(m *Model, width int) string {
	return ansi.Strip(renderStatus(m.Status, nil, width))
}

func TestModelBackgroundTasksStatus(t *testing.T) {
	m, sent := modelNew(t, false)
	modelEvents(m, protocol.Init{SessionID: "one"})
	activityRaw(m, `{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"b1","task_type":"local_bash"},{"task_id":"b2","task_type":"local_bash"},{"task_id":"a1","task_type":"local_agent"}]}`)
	modelEvents(m, protocol.Result{})
	for _, width := range []int{40, 80, 160} {
		got := activityStatus(m, width)
		if !strings.Contains(got, "3 tasks") || !strings.Contains(got, "waiting") || strings.Contains(got, "idle") {
			t.Errorf("width %d: %q", width, got)
		}
	}
	// Bookends can arrive on either side of the snapshot; they must not change its count.
	modelEvents(m, protocol.TaskStarted{TaskID: "b1", Type: "local_bash"}, protocol.TaskDone{TaskID: "b2", Status: "completed"})
	if got := activityStatus(m, 80); !strings.Contains(got, "3 tasks") {
		t.Errorf("bookends changed the snapshot: %q", got)
	}
	modelEvents(m, protocol.Thinking{})
	if got := activityStatus(m, 80); !strings.Contains(got, "thinking") || !strings.Contains(got, "3 tasks") || strings.Contains(got, "waiting") {
		t.Errorf("active main: %q", got)
	}
	activityRaw(m, `{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"b1","task_type":"local_bash"}]}`)
	if got := activityStatus(m, 80); !strings.Contains(got, "1 task") || strings.Contains(got, "3 tasks") {
		t.Errorf("replacement: %q", got)
	}
	activityRaw(m, `{"type":"system","subtype":"background_tasks_changed","tasks":[]}`)
	modelEvents(m, protocol.Result{})
	if got := activityStatus(m, 80); strings.Contains(got, "task") || !strings.Contains(got, "idle") {
		t.Errorf("finished: %q", got)
	}
	if len(*sent) != 0 {
		t.Errorf("background tracking sent messages: %#v", *sent)
	}
}

func TestModelWakeupCountdown(t *testing.T) {
	m, _ := modelNew(t, false)
	m.Now = modelT0
	modelEvents(m, protocol.Init{SessionID: "one"})
	deadline := modelT0.Add(425 * time.Second).UnixMilli()
	activityRaw(m, fmt.Sprintf(`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"w1","content":"Next wakeup scheduled"}]},"tool_use_result":{"scheduledFor":%d,"clampedDelaySeconds":425,"wasClamped":false}}`, deadline))
	modelEvents(m, protocol.Result{})
	if got := activityStatus(m, 80); !strings.Contains(got, "waiting 7m5s") {
		t.Fatalf("scheduled: %q", got)
	}
	m.Update(tickMsg(modelT0.Add(1100 * time.Millisecond)))
	if got := activityStatus(m, 80); !strings.Contains(got, "waiting 7m4s") {
		t.Errorf("countdown: %q", got)
	}
	// An older queued fire must not erase a newer pending wakeup.
	activityRaw(m, fmt.Sprintf(`{"type":"command_lifecycle","state":"started","wakeup_source":"loop_wakeup","scheduled_for":%d}`, deadline-1000))
	modelEvents(m, protocol.Result{})
	if got := activityStatus(m, 80); !strings.Contains(got, "waiting 7m4s") {
		t.Errorf("old fire erased countdown: %q", got)
	}
	m.Update(tickMsg(modelT0.Add(426 * time.Second)))
	if got := activityStatus(m, 80); !strings.Contains(got, "waiting 0s") || strings.Contains(got, "-1s") {
		t.Errorf("overdue: %q", got)
	}
	activityRaw(m, fmt.Sprintf(`{"type":"command_lifecycle","state":"started","wakeup_source":"loop_wakeup","scheduled_for":%d}`, deadline))
	if got := activityStatus(m, 80); strings.Contains(got, "waiting") {
		t.Errorf("fired countdown remains: %q", got)
	}
}

func TestModelActivityCleared(t *testing.T) {
	for _, end := range []string{"stop", "exit", "session"} {
		t.Run(end, func(t *testing.T) {
			m, _ := modelNew(t, false)
			m.Now = modelT0
			modelEvents(m, protocol.Init{SessionID: "one"})
			activityRaw(m, `{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"b1","task_type":"local_bash"}]}`)
			activityRaw(m, fmt.Sprintf(`{"type":"user","message":{"content":[]},"tool_use_result":{"scheduledFor":%d}}`, modelT0.Add(time.Minute).UnixMilli()))
			modelEvents(m, protocol.Result{})
			switch end {
			case "stop":
				activityRaw(m, `{"type":"user","message":{"content":[]},"tool_use_result":{"scheduledFor":0,"stopped":true}}`)
			case "exit":
				modelEvents(m, protocol.Exited{})
			case "session":
				modelEvents(m, protocol.Init{SessionID: "two"})
			}
			got := activityStatus(m, 80)
			if strings.Contains(got, "waiting 1m") || end != "stop" && strings.Contains(got, "task") {
				t.Errorf("stale activity after %s: %q", end, got)
			}
			if end == "stop" && (!strings.Contains(got, "waiting") || !strings.Contains(got, "1 task")) {
				t.Errorf("stopping a loop hid background work: %q", got)
			}
		})
	}
}

func TestModelAutonomousTurnStarts(t *testing.T) {
	m, sent := modelNew(t, false)
	modelEvents(m, protocol.Init{SessionID: "one"}, protocol.Result{})
	activityRaw(m, `{"type":"command_lifecycle","command_uuid":"auto-1","state":"queued"}`)
	if m.Status.Busy {
		t.Fatal("a queued command marked main busy before it started")
	}
	activityRaw(m, `{"type":"command_lifecycle","command_uuid":"auto-1","state":"started","wakeup_source":"schedule_wakeup"}`)
	if !m.Status.Busy || !strings.Contains(activityStatus(m, 80), "thinking") {
		t.Errorf("autonomous turn remained idle: %q", activityStatus(m, 80))
	}
	modelEvents(m, protocol.ToolUse{ID: "b1", Name: "Bash", Input: map[string]any{"command": "pwd"}})
	activityRaw(m, `{"type":"command_lifecycle","command_uuid":"auto-1","state":"started"}`)
	if m.Status.State != "running" {
		t.Errorf("a repeated start overwrote activity: %q", m.Status.State)
	}
	if len(*sent) != 0 {
		t.Errorf("tracking an autonomous turn sent messages: %#v", *sent)
	}
}

func TestActivityStatusWidth(t *testing.T) {
	st := Status{Model: "claude-opus-5-5", Mode: "auto", State: "idle", Tasks: 3, Wakeup: "7m5s", Usage: "5h 9%", Context: 0.3}
	for width := 0; width <= 200; width++ {
		drawCheckSize(t, renderStatus(st, drawHintsPrompt, width), width, 1)
	}
	got := ansi.Strip(renderStatus(st, drawHintsPrompt, 80))
	if !strings.Contains(got, "3 tasks") || !strings.Contains(got, "waiting 7m5s") {
		t.Errorf("compact status: %q", got)
	}
}
