package protocol

import (
	"reflect"
	"testing"
	"time"
)

func TestParseBackgroundActivity(t *testing.T) {
	for _, tt := range []struct {
		name, line string
		want       []Event
	}{
		{"snapshot", `{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"b1","task_type":"local_bash","description":"Watch CI"}]}`, []Event{BackgroundTasks{Tasks: []BackgroundTask{{ID: "b1", Description: "Watch CI"}}}}},
		{"empty snapshot", `{"type":"system","subtype":"background_tasks_changed","tasks":[]}`, []Event{BackgroundTasks{Tasks: []BackgroundTask{}}}},
		{"missing snapshot", `{"type":"system","subtype":"background_tasks_changed"}`, nil},
		{"queued", `{"type":"command_lifecycle","state":"queued"}`, nil},
		{"started", `{"type":"command_lifecycle","state":"started","wakeup_source":"loop_wakeup","scheduled_for":1793311200000}`, []Event{CommandStarted{WakeupSource: "loop_wakeup", ScheduledFor: time.UnixMilli(1793311200000)}}},
		{"user started", `{"type":"command_lifecycle","state":"started"}`, []Event{CommandStarted{}}},
		{"wakeup", `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"w1","content":"Next wakeup scheduled"}]},"tool_use_result":{"scheduledFor":1793311200000,"clampedDelaySeconds":425,"wasClamped":false}}`, []Event{ToolResult{ID: "w1", Output: "Next wakeup scheduled"}, WakeupScheduled{At: time.UnixMilli(1793311200000)}}},
		{"stop", `{"type":"user","message":{"content":[]},"tool_use_result":{"scheduledFor":0,"stopped":true}}`, []Event{WakeupScheduled{}}},
		{"other tool", `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"b1","content":"ok"}]},"tool_use_result":"ok"}`, []Event{ToolResult{ID: "b1", Output: "ok"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := Parse([]byte(tt.line)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}
