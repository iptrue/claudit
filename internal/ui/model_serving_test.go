package ui

import (
	"testing"

	"github.com/pixelbender/claudit/internal/protocol"
)

func TestModelServerReportedModel(t *testing.T) {
	for _, tc := range []struct {
		name, line, want string
	}{
		{"response start", `{"type":"stream_event","event":{"type":"message_start","message":{"model":"claude-sonnet-5-5","usage":{"input_tokens":100}}}}`, "claude-sonnet-5-5"},
		{"CLI fallback", `{"type":"system","subtype":"model_fallback","original_model":"claude-opus-5-5","fallback_model":"claude-sonnet-5-5","trigger":"overloaded"}`, "claude-sonnet-5-5"},
		{"midstream fallback", `{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"fallback","from":{"model":"claude-opus-5-5"},"to":{"model":"claude-sonnet-5-5"},"trigger":{"type":"refusal"}}}}`, "claude-sonnet-5-5"},
		{"final iterations", `{"type":"stream_event","event":{"type":"message_delta","usage":{"iterations":[{"type":"message","model":"claude-opus-5-5"},{"type":"fallback_message","model":"claude-sonnet-5-5"},{"type":"fallback_message","model":"claude-haiku-5-5"}]}}}`, "claude-haiku-5-5"},
		{"final response", `{"type":"assistant","message":{"model":"claude-sonnet-5-5","content":[{"type":"text","text":"done"}]}}`, "claude-sonnet-5-5"},
		{"final response fallback", `{"type":"assistant","message":{"model":"claude-opus-5-5","content":[{"type":"fallback","from":{"model":"claude-opus-5-5"},"to":{"model":"claude-sonnet-5-5"}},{"type":"text","text":"done"}]}}`, "claude-sonnet-5-5"},
		{"subagent start", `{"type":"stream_event","parent_tool_use_id":"a1","event":{"type":"message_start","message":{"model":"claude-haiku-5-5"}}}`, "claude-opus-5-5"},
		{"subagent fallback", `{"type":"stream_event","parent_tool_use_id":"a1","event":{"type":"content_block_start","content_block":{"type":"fallback","to":{"model":"claude-haiku-5-5"}}}}`, "claude-opus-5-5"},
		{"subagent response", `{"type":"assistant","parent_tool_use_id":"a1","message":{"model":"claude-haiku-5-5","content":[{"type":"text","text":"done"}]}}`, "claude-opus-5-5"},
		{"synthetic reply", `{"type":"assistant","message":{"model":"<synthetic>","content":[{"type":"text","text":"Command output"}]}}`, "claude-opus-5-5"},
		{"missing fallback model", `{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"fallback","to":{}}}}`, "claude-opus-5-5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, sent := modelNew(t, false)
			modelEvents(m, protocol.Init{SessionID: "one", Model: "claude-opus-5-5"}, protocol.Context{Model: "claude-opus-5-5", Tokens: 100})
			activityRaw(m, tc.line)
			if m.Status.Model != tc.want {
				t.Errorf("status model %q, want %q", m.Status.Model, tc.want)
			}
			if m.Sim.FindAgent("main").Model != tc.want {
				t.Errorf("main model %q, want %q", m.Sim.FindAgent("main").Model, tc.want)
			}
			if m.context.Tokens != 100 {
				t.Errorf("a model report reset the context token count: %d", m.context.Tokens)
			}
			if len(*sent) != 0 {
				t.Errorf("displaying the serving model sent control requests: %#v", *sent)
			}
		})
	}
}
