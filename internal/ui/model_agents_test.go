package ui

import (
	"bytes"
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/pixelbender/claudit/internal/ide"
	"github.com/pixelbender/claudit/internal/ideview"
	"github.com/pixelbender/claudit/internal/protocol"
)

const modelScript = "export const meta = {\n  name: 'calc-tests-and-docs',\n  description: 'Tests and docs',\n}\nphase('Build')\n"

// modelWorkflow starts the workflow "wf" (task id "w1") at modelT0.
func modelWorkflow(t *testing.T, m *Model) *ide.Agent {
	t.Helper()
	modelEvents(m,
		protocol.ToolUse{ID: "wf", Name: "Workflow", Input: map[string]any{"script": modelScript}},
		protocol.TaskStarted{TaskID: "w1", ToolUseID: "wf", Type: "local_workflow", WorkflowName: "calc-tests-and-docs"},
	)
	return modelAgent(t, m, "wf")
}

// modelProgress reports workflow agents of the workflow "wf" in phase 1.
func modelProgress(m *Model, items ...protocol.WorkflowItem) {
	for i := range items {
		items[i].PhaseIndex, items[i].PhaseTitle = cmp.Or(items[i].PhaseIndex, 1), cmp.Or(items[i].PhaseTitle, "Build")
	}
	all := append([]protocol.WorkflowItem{{Phase: true, Index: 1, Title: "Build"}}, items...)
	modelEvents(m, protocol.TaskProgress{TaskID: "w1", ToolUseID: "wf", Workflow: all})
}

// modelOpen lists the calls waiting for their result as "agent:tool:id", sorted.
func modelOpen(m *Model) []string {
	var open []string
	for id, c := range m.crew.calls {
		open = append(open, cmp.Or(c.Parent, "main")+":"+c.Name+":"+id)
	}
	slices.Sort(open)
	return open
}

func modelChildren(m *Model, parent string) []string {
	var keys []string
	for _, a := range m.Sim.State().Agents {
		if a.Parent == parent {
			keys = append(keys, a.Key)
		}
	}
	return keys
}

func modelAppend(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// modelCall and modelReturn are transcript records of a tool call and of its result.
func modelCall(id, name string) string {
	return `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"` + id + `","name":"` + name + `","input":{"command":"ls"}}]}}` + "\n"
}

func modelReturn(id string) string {
	return `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"` + id + `","content":"ok"}]}}` + "\n"
}

func TestModelSubagent(t *testing.T) {
	m, _ := modelNew(t, false)
	modelEvents(m, protocol.ToolUse{ID: "call1", Name: "Agent", Input: map[string]any{"description": "Add mul", "subagent_type": "general-purpose"}})
	sub := modelAgent(t, m, "call1")
	// A subagent gets a name of its own.
	if sub.Kind != ide.Subagent || sub.Name != "Kevin" || sub.Parent != "main" || sub.Status != ide.Running || sub.Started != modelT0 {
		t.Fatalf("subagent %+v", sub)
	}

	modelAt(m, time.Second)
	modelEvents(m, protocol.TaskStarted{TaskID: "a0b8", ToolUseID: "call1", Type: "local_agent", Description: "something else"})
	if sub.Name != "Kevin" || sub.Started != modelT0 {
		t.Errorf("TaskStarted changed the known subagent: %+v", sub)
	}

	modelAt(m, 2*time.Second)
	modelEvents(m,
		protocol.ToolUse{ID: "r1", Name: "Read", Input: map[string]any{"file_path": "/x/calc.py"}, Parent: "call1"},
		protocol.PermissionRequest{RequestID: "p1", ToolName: "Edit", Input: map[string]any{"file_path": "/x/calc.py"}, ToolUseID: "e1", AgentID: "a0b8"},
	)
	if sub.ToolCalls != 1 {
		t.Errorf("subagent call: %d calls", sub.ToolCalls)
	}
	if got, want := modelOpen(m), []string{"call1:Read:r1", "main:Agent:call1"}; !slices.Equal(got, want) {
		t.Errorf("open calls %q, want %q", got, want)
	}
	if p, ok := m.Popup.(*PermissionPopup); !ok || p.AgentName != "Kevin" || p.AgentColor != sub.Color {
		t.Fatalf("popup %#v", m.Popup)
	}
	modelPress(m, inputKey("y"))

	// The Agent call returns at once for a background agent: only TaskDone ends it.
	modelEvents(m, protocol.ToolResult{ID: "r1", Output: "1\tx\n"}, protocol.ToolResult{ID: "call1", Output: "Async agent launched"})
	if sub.Status != ide.Running || !sub.Ended.IsZero() {
		t.Fatalf("subagent ended by its Agent result: %+v", sub)
	}
	modelAt(m, 3*time.Second)
	modelEvents(m, protocol.TaskDone{TaskID: "a0b8", Status: "completed"})
	modelAt(m, 4*time.Second)
	modelEvents(m, protocol.TaskDone{TaskID: "a0b8", ToolUseID: "call1", Status: "completed"})
	if sub.Status != ide.Done || sub.Ended != modelT0.Add(3*time.Second) {
		t.Errorf("after TaskDone: %+v", sub)
	}
}

func TestModelSubagentNamesAndEnds(t *testing.T) {
	m, _ := modelNew(t, false)
	modelEvents(m,
		protocol.ToolUse{ID: "c1", Name: "Task", Input: map[string]any{"subagent_type": "Explore"}},
		protocol.ToolUse{ID: "c2", Name: "Agent", Input: map[string]any{}},
		// A subagent whose Agent call was not seen, and a background shell, which is no agent.
		protocol.TaskStarted{TaskID: "a3", ToolUseID: "c3", Type: "local_agent", Description: "Review"},
		protocol.TaskStarted{TaskID: "b4", ToolUseID: "c4", Type: "local_bash", Description: "sleep"},
		protocol.TaskDone{TaskID: "b4", ToolUseID: "c4", Status: "completed"},
		// A denied Agent call never runs; a killed one fails.
		protocol.ToolResult{ID: "c2", Output: "The user denied this tool call.", IsError: true},
		protocol.TaskDone{TaskID: "a3", Status: "killed"},
	)
	for key, want := range map[string]struct {
		name   string
		status ide.AgentStatus
	}{"c1": {"Kevin", ide.Running}, "c2": {"Stuart", ide.Failed}, "c3": {"Bob", ide.Failed}} {
		a := modelAgent(t, m, key)
		if a.Name != want.name || a.Status != want.status || a.Parent != "main" || a.Started != modelT0 {
			t.Errorf("%s: %+v", key, a)
		}
	}
	if m.Sim.FindAgent("c4") != nil {
		t.Error("a background shell became an agent")
	}
	// The main agent's requests carry no agent; unknown agents show none either.
	modelEvents(m,
		protocol.PermissionRequest{RequestID: "p1", ToolName: "Bash", Input: map[string]any{"command": "ls"}},
		protocol.PermissionRequest{RequestID: "p2", ToolName: "Bash", Input: map[string]any{"command": "ls"}, AgentID: "nobody"},
	)
	for range 2 {
		if p := m.Popup.(*PermissionPopup); p.AgentName != "" || p.AgentColor != 0 {
			t.Errorf("popup %s names %q", p.RequestID, p.AgentName)
		}
		modelPress(m, inputKey("y"))
	}
}

func TestModelWorkflowProgress(t *testing.T) {
	m, _ := modelNew(t, false)
	modelEvents(m, protocol.ToolUse{ID: "wf", Name: "Workflow", Input: map[string]any{"script": modelScript}})
	wf := modelAgent(t, m, "wf")
	if wf.Kind != ide.Workflow || wf.Name != "calc-tests-and-docs" || wf.Parent != "main" || !wf.Started.IsZero() {
		t.Fatalf("workflow node %+v", wf)
	}
	modelAt(m, time.Second)
	modelEvents(m, protocol.TaskStarted{TaskID: "w1", ToolUseID: "wf", Type: "local_workflow", WorkflowName: "calc-tests-and-docs"})
	if wf.Status != ide.Running || wf.Started != modelT0.Add(time.Second) {
		t.Fatalf("started workflow %+v", wf)
	}

	modelAt(m, 2*time.Second)
	modelProgress(m,
		protocol.WorkflowItem{Index: 1, Title: "tests", AgentID: "ac42", State: "start"},
		protocol.WorkflowItem{Index: 2, Title: "docs", State: "start"},
		// Agent indexes may restart in every phase.
		protocol.WorkflowItem{Index: 1, Title: "review", PhaseIndex: 2, PhaseTitle: "Check"},
	)
	tests := modelAgent(t, m, "ac42")
	if tests.Kind != ide.WorkflowAgent || tests.Parent != "wf" || tests.Name != "tests" || tests.Phase != "Build" ||
		tests.Status != ide.Running || tests.Started != modelT0.Add(2*time.Second) {
		t.Errorf("tests %+v", tests)
	}
	docs, review := modelAgent(t, m, "wf#1.2"), modelAgent(t, m, "wf#2.1")
	if docs.Status != ide.Queued || docs.Name != "docs" || !docs.Started.IsZero() || review.Status != ide.Queued || review.Phase != "Check" {
		t.Errorf("queued docs %+v review %+v", docs, review)
	}
	color := docs.Color

	// The id of a queued agent arrives: it keeps its row under the new key.
	modelAt(m, 3*time.Second)
	modelProgress(m, protocol.WorkflowItem{Index: 2, Title: "docs", AgentID: "ad85", State: "progress",
		LastTool: "Bash", LastToolSummary: "go test ./...\nmore", Tokens: 10, ToolCalls: 2})
	if m.Sim.FindAgent("wf#1.2") != nil {
		t.Error("the placeholder is still there")
	}
	docs = modelAgent(t, m, "ad85")
	if docs.Status != ide.Running || docs.Started != modelT0.Add(3*time.Second) || docs.Color != color ||
		docs.Tokens != 10 || docs.ToolCalls != 2 || docs.Activity != "Bash go test ./..." {
		t.Errorf("docs %+v", docs)
	}
	modelProgress(m, protocol.WorkflowItem{Index: 2, Title: "docs", AgentID: "ad85", State: "progress", LastTool: "Read", LastToolSummary: "x.go"})
	if docs.Activity != "Read x.go" {
		t.Errorf("activity %q", docs.Activity)
	}
	docs.Activity = "Edit calc.py" // the sim plays its transcript: progress must not overwrite that
	modelProgress(m, protocol.WorkflowItem{Index: 2, Title: "docs", AgentID: "ad85", State: "progress", LastTool: "Grep"})
	if docs.Activity != "Edit calc.py" {
		t.Errorf("progress overwrote the sim's activity: %q", docs.Activity)
	}

	modelAt(m, 4*time.Second)
	modelProgress(m, protocol.WorkflowItem{Index: 1, Title: "tests", AgentID: "ac42", State: "done", LastTool: "Bash"})
	if tests.Status != ide.Done || tests.Ended != modelT0.Add(4*time.Second) || tests.Activity != "" {
		t.Errorf("done tests %+v", tests)
	}

	modelAt(m, 5*time.Second)
	modelEvents(m, protocol.TaskDone{TaskID: "w1", Status: "completed"})
	for _, a := range []*ide.Agent{wf, docs, review} {
		if a.Status != ide.Done || a.Ended != modelT0.Add(5*time.Second) {
			t.Errorf("after TaskDone: %+v", a)
		}
	}
	if tests.Ended != modelT0.Add(4*time.Second) {
		t.Errorf("TaskDone moved the end of a finished agent: %v", tests.Ended)
	}
}

func TestModelWorkflowName(t *testing.T) {
	tests := map[string]string{
		modelScript: "calc-tests-and-docs",
		"export const meta = {\n  name: \"double\",\n}":                      "double",
		"const meta={name:`tick`, phases: [{ title: 'Build' }]}":             "tick",
		"const meta = {\n  phases: [{ name: 'phase' }],\n  name: 'late',\n}": "",
		"phase('Build')\nagent({ name: 'not meta' })":                        "",
		"": "",
	}
	for script, want := range tests {
		if got := workflowName(script); got != want {
			t.Errorf("workflowName(%q) = %q, want %q", script, got, want)
		}
	}
}

func TestModelWorkflowAgentStates(t *testing.T) {
	tests := []struct {
		id, state string
		want      ide.AgentStatus
	}{
		{"", "start", ide.Queued},
		{"x", "start", ide.Running},
		{"x", "progress", ide.Running},
		{"x", "something new", ide.Running},
		{"x", "done", ide.Done},
		{"x", "error", ide.Failed},
		{"x", "failed", ide.Failed},
		{"x", "killed", ide.Failed},
		{"x", "cancelled", ide.Failed},
	}
	for _, tt := range tests {
		m, _ := modelNew(t, false)
		modelWorkflow(t, m)
		modelProgress(m, protocol.WorkflowItem{Index: 1, Title: "a", AgentID: tt.id, State: tt.state})
		a := modelAgent(t, m, cmp.Or(tt.id, "wf#1.1"))
		if a.Status != tt.want || a.Ended.IsZero() != (tt.want == ide.Running || tt.want == ide.Queued) {
			t.Errorf("%q %q: %+v", tt.id, tt.state, a)
		}
	}

	// A failed workflow fails what still runs in it.
	m, _ := modelNew(t, false)
	wf := modelWorkflow(t, m)
	modelProgress(m, protocol.WorkflowItem{Index: 1, Title: "a", AgentID: "x", State: "start"})
	modelEvents(m, protocol.TaskDone{TaskID: "w1", ToolUseID: "wf", Status: "failed"})
	if x := modelAgent(t, m, "x"); wf.Status != ide.Failed || x.Status != ide.Failed {
		t.Errorf("failed workflow %+v, agent %+v", wf, x)
	}
}

func TestModelWorkflowTranscript(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "agent-ab12cd34ef.jsonl")
	ret := modelReturn("x1")
	modelAppend(t, first, modelCall("x1", "Bash")+ret[:20])

	m, _ := modelNew(t, false)
	modelWorkflow(t, m)
	modelEvents(m,
		protocol.ToolResult{ID: "wf", Output: "Workflow launched in background. Task ID: w1\nTranscript dir: " + dir + "\nRun ID: r1"},
		protocol.Result{},
	)
	if m.tickInterval() != tickBusy {
		t.Error("the tick slowed down while a transcript is followed")
	}
	t1 := modelT0.Add(time.Second)
	m.Update(tickMsg(t1))
	a := modelAgent(t, m, "ab12cd34ef")
	if a.Kind != ide.WorkflowAgent || a.Parent != "wf" || a.Name != "ab12cd3" || a.Status != ide.Running || a.Started != t1 {
		t.Fatalf("agent from the transcript %+v", a)
	}
	if got, want := modelOpen(m), []string{"ab12cd34ef:Bash:x1"}; !slices.Equal(got, want) {
		t.Errorf("open calls %q, want %q", got, want)
	}

	// The tailers are read every 250 ms.
	modelAppend(t, first, ret[20:]+modelCall("x2", "Read"))
	m.Update(tickMsg(t1.Add(100 * time.Millisecond)))
	if got, want := modelOpen(m), []string{"ab12cd34ef:Bash:x1"}; !slices.Equal(got, want) {
		t.Fatalf("polled before 250 ms: open calls %q", got)
	}
	m.Update(tickMsg(t1.Add(250 * time.Millisecond)))
	if got, want := modelOpen(m), []string{"ab12cd34ef:Read:x2"}; !slices.Equal(got, want) {
		t.Errorf("open calls %q, want %q", got, want)
	}

	// Progress names the agent; a new agent file is found on the next poll.
	modelProgress(m, protocol.WorkflowItem{Index: 1, Title: "tests", AgentID: "ab12cd34ef", State: "progress"})
	modelAppend(t, filepath.Join(dir, "agent-ff00ff00ff.jsonl"), modelCall("y1", "Grep"))
	m.Update(tickMsg(t1.Add(500 * time.Millisecond)))
	if a.Name != "tests" || modelAgent(t, m, "ff00ff00ff").Name != "ff00ff0" {
		t.Errorf("names %q %q", a.Name, m.Sim.FindAgent("ff00ff00ff").Name)
	}

	// TaskDone reads the transcripts one last time (an agent that came at the end), then stops
	// following them; the calls left open end with the workflow.
	modelAppend(t, filepath.Join(dir, "agent-0123456789.jsonl"), modelCall("x3", "Edit"))
	modelAt(m, 2*time.Second)
	modelEvents(m, protocol.TaskDone{TaskID: "w1", Status: "completed"})
	if m.Sim.FindAgent("0123456789") == nil {
		t.Error("final poll: the last agent was not read")
	}
	if len(m.crew.tailers) != 0 {
		t.Fatal("the tailer outlived its workflow")
	}
	if got := modelOpen(m); len(got) != 0 {
		t.Errorf("open calls %q after the workflow", got)
	}
	modelAppend(t, first, modelCall("x4", "Edit"))
	m.Update(tickMsg(t1.Add(5 * time.Second)))
	if got := modelOpen(m); len(got) != 0 {
		t.Errorf("a finished workflow's transcript is still read: open calls %q", got)
	}
}

// The transcript can show an agent before the progress reports its id: the queued placeholder
// of that agent must not stay behind as a second row.
func TestModelWorkflowPlaceholderMeetsTranscript(t *testing.T) {
	dir := t.TempDir()
	modelAppend(t, filepath.Join(dir, "agent-bbbbbbbbbb.jsonl"), modelCall("z1", "Bash"))
	m, _ := modelNew(t, false)
	modelWorkflow(t, m)
	modelProgress(m, protocol.WorkflowItem{Index: 1, Title: "a"})
	hot := modelAgent(t, m, "wf#1.1").Hotkey
	modelPress(m, inputKey("alt+"+string(rune('0'+hot)))) // the user pins the placeholder
	modelEvents(m, protocol.ToolResult{ID: "wf", Output: "Transcript dir: " + dir})
	m.Update(tickMsg(modelT0))
	if got := modelChildren(m, "wf"); !reflect.DeepEqual(got, []string{"wf#1.1", "bbbbbbbbbb"}) {
		t.Fatalf("children %v", got)
	}
	modelProgress(m, protocol.WorkflowItem{Index: 1, Title: "a", AgentID: "bbbbbbbbbb", State: "progress"})
	if got := modelChildren(m, "wf"); !reflect.DeepEqual(got, []string{"bbbbbbbbbb"}) {
		t.Fatalf("children %v", got)
	}
	a := modelAgent(t, m, "bbbbbbbbbb")
	if a.Name != "a" || a.Status != ide.Running {
		t.Errorf("agent %+v", a)
	}
	// The agent takes over what the user knew the placeholder by.
	if st := m.Sim.State(); st.Camera != a.Key || !st.Pinned || m.PanelSel != a.Key || a.Hotkey != hot {
		t.Errorf("camera %q pinned %v sel %q hotkey %d, want %d", st.Camera, st.Pinned, m.PanelSel, a.Hotkey, hot)
	}
	modelEvents(m, protocol.ToolUse{ID: "s1", Name: "Agent", Input: map[string]any{"description": "late"}})
	if h := modelAgent(t, m, "s1").Hotkey; h != hot+1 {
		t.Errorf("next hotkey %d, want %d: no hole", h, hot+1)
	}
}

// The transcript shows the agent even before the progress queues it: the agent keeps its own,
// lower hotkey, and the placeholder's is free again.
func TestModelTranscriptBeforePlaceholder(t *testing.T) {
	dir := t.TempDir()
	modelAppend(t, filepath.Join(dir, "agent-bbbbbbbbbb.jsonl"), modelCall("z1", "Bash"))
	m, _ := modelNew(t, false)
	modelWorkflow(t, m)
	modelEvents(m, protocol.ToolResult{ID: "wf", Output: "Transcript dir: " + dir})
	m.Update(tickMsg(modelT0))
	hot := modelAgent(t, m, "bbbbbbbbbb").Hotkey
	modelProgress(m, protocol.WorkflowItem{Index: 1, Title: "a"})
	modelProgress(m, protocol.WorkflowItem{Index: 1, Title: "a", AgentID: "bbbbbbbbbb", State: "progress"})
	modelEvents(m, protocol.ToolUse{ID: "s1", Name: "Agent", Input: map[string]any{"description": "late"}})
	if a, s1 := modelAgent(t, m, "bbbbbbbbbb"), modelAgent(t, m, "s1"); a.Hotkey != hot || s1.Hotkey != hot+1 {
		t.Errorf("hotkeys %d and %d, want %d and %d", a.Hotkey, s1.Hotkey, hot, hot+1)
	}
}

// A subagent and a workflow agent stopped in the middle of a call: the result never comes, so
// TaskDone ends the call. The lanes play on, the calls end, the chat line stops waiting.
func TestModelTaskDoneEndsOpenCalls(t *testing.T) {
	dir := t.TempDir()
	modelAppend(t, filepath.Join(dir, "agent-ab12cd34ef.jsonl"), modelCall("x1", "Bash"))
	m, _ := modelNew(t, false)
	modelEvents(m,
		protocol.ToolUse{ID: "s1", Name: "Agent", Input: map[string]any{"description": "bg"}},
		protocol.TaskStarted{TaskID: "a1", ToolUseID: "s1", Type: "local_agent"},
		protocol.ToolResult{ID: "s1", Output: "launched"},
		protocol.ToolUse{ID: "b1", Name: "Bash", Input: map[string]any{"command": "sleep 100"}, Parent: "s1"},
	)
	modelWorkflow(t, m)
	modelEvents(m, protocol.ToolResult{ID: "wf", Output: "Transcript dir: " + dir}, protocol.Result{})
	m.Update(tickMsg(modelT0))
	if got := modelOpen(m); !slices.Equal(got, []string{"ab12cd34ef:Bash:x1", "s1:Bash:b1"}) {
		t.Fatalf("open calls %q", got)
	}
	modelAt(m, time.Second)
	modelEvents(m, protocol.TaskDone{TaskID: "a1", Status: "killed"}, protocol.TaskDone{TaskID: "w1", Status: "killed"})
	for i := range 100 {
		m.Update(tickMsg(modelT0.Add(time.Duration(i) * 100 * time.Millisecond)))
	}
	if m.Sim.Busy() || m.tickInterval() == tickBusy {
		t.Error("a lane still waits for a result that never comes")
	}
	if got := modelOpen(m); len(got) != 0 {
		t.Errorf("open calls %q after the agents ended", got)
	}

	// A foreground subagent that fails: its Agent result ends its open calls too.
	modelEvents(m,
		protocol.ToolUse{ID: "s2", Name: "Agent", Input: map[string]any{"description": "fg"}},
		protocol.ToolUse{ID: "b2", Name: "Bash", Input: map[string]any{"command": "make"}, Parent: "s2"},
		protocol.ToolResult{ID: "s2", Output: "interrupted by the user", IsError: true},
	)
	if got := modelOpen(m); len(got) != 0 || modelAgent(t, m, "s2").Status != ide.Failed {
		t.Errorf("open calls %q, subagent %v", got, modelAgent(t, m, "s2").Status)
	}
}

// The workflow ends before its launch result arrives: its transcripts are read once, and no
// tailer keeps polling them.
func TestModelWorkflowResultAfterTaskDone(t *testing.T) {
	dir := t.TempDir()
	modelAppend(t, filepath.Join(dir, "agent-ab12cd34ef.jsonl"), modelCall("x1", "Bash")+modelReturn("x1"))
	m, _ := modelNew(t, false)
	modelWorkflow(t, m)
	modelEvents(m, protocol.TaskDone{TaskID: "w1", Status: "completed"}, protocol.ToolResult{ID: "wf", Output: "Transcript dir: " + dir})
	if len(m.crew.tailers) != 0 {
		t.Errorf("%d tailers after the workflow ended", len(m.crew.tailers))
	}
	if a := m.Sim.FindAgent("ab12cd34ef"); a == nil || len(modelOpen(m)) != 0 {
		t.Errorf("transcript not read: agent %v, open calls %q", a, modelOpen(m))
	}
}

// The status line is about main: the calls of a subagent and the answers to its popups do not
// change it.
func TestModelSubagentKeepsMainStatus(t *testing.T) {
	m, _ := modelNew(t, false)
	main := modelAgent(t, m, "main")
	modelEvents(m,
		protocol.ToolUse{ID: "s1", Name: "Agent", Input: map[string]any{"description": "bg"}},
		protocol.TaskStarted{TaskID: "a1", ToolUseID: "s1", Type: "local_agent"},
	)
	if main.Status != ide.Running {
		t.Errorf("main at work: %v", main.Status)
	}
	modelEvents(m,
		protocol.ToolResult{ID: "s1", Output: "launched"},
		protocol.Result{},
		protocol.ToolUse{ID: "e1", Name: "Edit", Input: map[string]any{"file_path": "/x/a.go"}, Parent: "s1"},
	)
	if m.Status.State != "idle" || m.Status.Busy || main.Status != ide.Done {
		t.Errorf("subagent call: status %+v, main %v", m.Status, main.Status)
	}
	modelEvents(m, protocol.PermissionRequest{RequestID: "p1", ToolName: "Edit", Input: map[string]any{"file_path": "/x/a.go"}, AgentID: "a1"})
	if m.Status.State != "waiting for you" {
		t.Errorf("popup: %+v", m.Status)
	}
	modelPress(m, inputKey("y"))
	if m.Status.State != "idle" || m.Status.Busy {
		t.Errorf("answered while main is idle: %+v", m.Status)
	}

	// Main at work: the answer goes back to what main does by then.
	modelEvents(m,
		protocol.ToolUse{ID: "b1", Name: "Bash", Input: map[string]any{"command": "ls"}},
		protocol.PermissionRequest{RequestID: "p2", ToolName: "Bash", Input: map[string]any{"command": "ls"}, AgentID: "a1"},
		protocol.Thinking{},
	)
	modelPress(m, inputKey("n"))
	if m.Status.State != "thinking" || !m.Status.Busy || main.Status != ide.Running {
		t.Errorf("answered while main works: %+v, main %v", m.Status, main.Status)
	}
}

func TestModelExitEndsAgents(t *testing.T) {
	dir := t.TempDir()
	modelAppend(t, filepath.Join(dir, "agent-cccccccccc.jsonl"), modelCall("z1", "Bash"))
	m, _ := modelNew(t, false)
	modelEvents(m, protocol.ToolUse{ID: "s1", Name: "Agent", Input: map[string]any{"description": "bg"}}, protocol.ToolResult{ID: "s1", Output: "launched"})
	modelWorkflow(t, m)
	modelProgress(m, protocol.WorkflowItem{Index: 2, Title: "later"})
	modelEvents(m, protocol.ToolResult{ID: "wf", Output: "Transcript dir: " + dir})
	m.Update(tickMsg(modelT0))
	if got := modelOpen(m); !slices.Contains(got, "cccccccccc:Bash:z1") {
		t.Fatalf("open calls %q", got)
	}
	end := modelAt(m, time.Second)
	modelEvents(m, protocol.Exited{})
	for _, a := range m.Sim.State().Agents[1:] {
		if a.Status != ide.Failed || a.Ended != end {
			t.Errorf("after exit: %+v", a)
		}
	}
	if got := modelOpen(m); len(got) != 0 {
		t.Errorf("open calls %q after exit", got)
	}
	if len(m.crew.tailers) != 0 {
		t.Error("tailers outlived claude")
	}
}

func TestModelCameraStatus(t *testing.T) {
	m, _ := modelNew(t, false)
	if m.Status.Camera != "" {
		t.Errorf("a camera with main alone: %q", m.Status.Camera)
	}
	modelEvents(m, protocol.ToolUse{ID: "s1", Name: "Agent", Input: map[string]any{"description": "tests"}})
	sub := modelAgent(t, m, "s1")
	modelPress(m, inputKey("alt+"+string(rune('0'+sub.Hotkey))))
	m.Update(tickMsg(modelT0))
	if want := "pinned · Kevin"; m.Status.Camera != want || m.Status.CameraColor != sub.Color {
		t.Errorf("pinned camera %q %d, want %q %d", m.Status.Camera, m.Status.CameraColor, want, sub.Color)
	}
	sub.Activity = "Edit calc.py"
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	if want := "pinned · Kevin (Edit calc.py)"; m.Status.Camera != want {
		t.Errorf("camera %q, want %q", m.Status.Camera, want)
	}
	modelPress(m, inputKey("alt+0"))
	if want := "auto → Kevin (Edit calc.py)"; m.Status.Camera != want || m.Sim.State().Pinned {
		t.Errorf("camera %q pinned %v, want %q", m.Status.Camera, m.Sim.State().Pinned, want)
	}
}

// The left column shows the agents while others than main work, the files otherwise; the keys
// leave the panel when it goes.
func TestModelPanelFollowsTheAgents(t *testing.T) {
	m, _ := modelNew(t, false)
	if m.Panel != ideview.Files {
		t.Fatal("the panel does not start on the files")
	}
	modelPress(m, inputKey("tab"))
	if m.PanelFocus {
		t.Error("tab focused the files")
	}
	modelEvents(m, protocol.ToolUse{ID: "s1", Name: "Agent", Input: map[string]any{"description": "a"}})
	if m.Panel != ideview.Agents {
		t.Fatal("a subagent at work did not switch the panel to the agents")
	}
	modelPress(m, inputKey("tab"))
	modelEvents(m, protocol.TaskDone{ToolUseID: "s1", Status: "completed"})
	if m.Panel != ideview.Files || m.PanelFocus {
		t.Errorf("all done: panel %v focus %v", m.Panel, m.PanelFocus)
	}
}

func TestModelPanelKeys(t *testing.T) {
	m, _ := modelNew(t, false)
	modelEvents(m,
		protocol.ToolUse{ID: "s1", Name: "Agent", Input: map[string]any{"description": "a"}},
		protocol.ToolUse{ID: "s2", Name: "Agent", Input: map[string]any{"description": "b"}},
	)
	st := m.Sim.State()
	modelPress(m, inputKey("tab"))
	if !m.PanelFocus || m.Panel != ideview.Agents || m.PanelSel != "main" {
		t.Fatalf("focus %v panel %v sel %q", m.PanelFocus, m.Panel, m.PanelSel)
	}
	var sels []string
	for _, k := range inputKeys("down down down up") {
		m.Update(k)
		sels = append(sels, m.PanelSel)
	}
	if want := []string{"s1", "s2", "s2", "s1"}; !reflect.DeepEqual(sels, want) {
		t.Errorf("selection %v, want %v", sels, want)
	}
	modelPress(m, inputKey("enter"))
	if st.Camera != "s1" || !st.Pinned {
		t.Errorf("enter: camera %q pinned %v", st.Camera, st.Pinned)
	}
	modelPress(m, inputKey("alt+0"))
	if st.Pinned {
		t.Error("alt+0 did not give the camera back")
	}
	modelPress(m, inputKey("alt+"+string(rune('0'+modelAgent(t, m, "s2").Hotkey))))
	if st.Camera != "s2" || !st.Pinned || m.PanelSel != "s2" {
		t.Errorf("hotkey: camera %q pinned %v sel %q", st.Camera, st.Pinned, m.PanelSel)
	}
	modelPress(m, inputKey("esc"))
	if m.PanelFocus {
		t.Fatal("esc did not leave the panel")
	}
	modelPress(m, inputTyped("hi")...)
	if string(m.Input.Text) != "hi" {
		t.Errorf("prompt got %q", string(m.Input.Text))
	}

	// Typing while the panel has the keys gives them back to the prompt, letters and digits alike.
	m.Input = Input{}
	modelPress(m, inputKey("tab"))
	modelPress(m, inputTyped("e1 x")...)
	if m.PanelFocus || string(m.Input.Text) != "e1 x" || m.Panel != ideview.Agents {
		t.Errorf("typing in the panel: focus %v prompt %q panel %v", m.PanelFocus, string(m.Input.Text), m.Panel)
	}
	m.Input = Input{}

	// Focusing again keeps a selection that still exists, and replaces one that is gone.
	modelPress(m, inputKey("tab"))
	if m.PanelSel != "s2" || m.Panel != ideview.Agents {
		t.Errorf("refocus: sel %q panel %v", m.PanelSel, m.Panel)
	}
	modelPress(m, inputKey("tab"))
	m.PanelSel = "gone"
	modelPress(m, inputKey("tab"))
	if m.PanelSel != st.Camera {
		t.Errorf("sel %q, want the camera %q", m.PanelSel, st.Camera)
	}
}

func TestModelPanelEnterOnWorkflow(t *testing.T) {
	m, _ := modelNew(t, false)
	modelWorkflow(t, m)
	modelProgress(m,
		protocol.WorkflowItem{Index: 1, Title: "a", AgentID: "x1", State: "done"},
		protocol.WorkflowItem{Index: 2, Title: "b", AgentID: "x2", State: "progress"},
		protocol.WorkflowItem{Index: 3, Title: "c"},
	)
	if rows := ideview.AgentRows(m.Sim.State()); !slices.Contains(rows, "wf") {
		t.Fatalf("rows %v", rows)
	}
	modelPress(m, inputKey("tab"))
	m.PanelSel = "wf"
	modelPress(m, inputKey("enter"))
	if st := m.Sim.State(); st.Camera != "x2" || !st.Pinned {
		t.Errorf("camera %q pinned %v, want the running agent", st.Camera, st.Pinned)
	}
	modelProgress(m, protocol.WorkflowItem{Index: 2, Title: "b", AgentID: "x2", State: "done"})
	modelPress(m, inputKey("alt+0"))
	modelPress(m, inputKey("enter"))
	if m.Sim.State().Pinned {
		t.Error("enter on a workflow with nothing running pinned the camera")
	}
}

// modelTranscriptDir matches the transcript dir in a raw stream line.
var modelTranscriptDir = regexp.MustCompile(`Transcript dir: ([^\\"]+)`)

// TestModelReplaysMultiFixture feeds a real session with a subagent and a two-agent workflow
// through the model, with the workflow's transcripts read from a copy of testdata/workflow.
func TestModelReplaysMultiFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "testdata", "multi.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	files, err := filepath.Glob(filepath.Join("..", "testdata", "workflow", "*.jsonl"))
	if err != nil || len(files) != 3 {
		t.Fatalf("workflow fixture: %v %v", files, err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(f)), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	quoted, _ := json.Marshal(dir)
	dirJSON := string(quoted[1 : len(quoted)-1])

	m, sent := modelNew(t, true)
	type ask struct {
		tool, agent string
		color       int
	}
	var asks []ask
	at := func(line int) time.Time { return modelT0.Add(time.Duration(line+1) * 100 * time.Millisecond) }
	for i, line := range bytes.Split(data, []byte("\n")) {
		if loc := modelTranscriptDir.FindSubmatchIndex(line); loc != nil {
			line = slices.Concat(line[:loc[2]], []byte(dirJSON), line[loc[3]:])
		}
		now := modelAt(m, at(i).Sub(modelT0))
		modelEvents(m, protocol.Parse(line)...)
		for m.Popup != nil {
			p, ok := m.Popup.(*PermissionPopup)
			if !ok {
				t.Fatalf("line %d: popup %#v", i, m.Popup)
			}
			asks = append(asks, ask{p.Tool, p.AgentName, p.AgentColor})
			modelPress(m, inputKey("y"))
		}
		m.Update(tickMsg(now))
	}

	const (
		subKey = "toolu_01P5HQRgrYB9L6rKi69RVaUL"
		wfKey  = "toolu_01SYyRMgjWVknSVXCK8VrFCu"
		tests  = "ac4272cdc31efcff1"
		docs   = "ad85e07399d07f94d"
	)
	type row struct {
		key, name, parent, phase string
		kind                     ide.AgentKind
		status                   ide.AgentStatus
	}
	var got []row
	for _, a := range m.Sim.State().Agents {
		got = append(got, row{a.Key, a.Name, a.Parent, a.Phase, a.Kind, a.Status})
	}
	want := []row{
		{"main", "main", "", "", ide.Main, ide.Done}, // its last turn is over
		{subKey, "Kevin", "main", "", ide.Subagent, ide.Done},
		{wfKey, "calc-tests-and-docs", "main", "", ide.Workflow, ide.Done},
		{tests, "tests", wfKey, "Build", ide.WorkflowAgent, ide.Done},
		{docs, "docs", wfKey, "Build", ide.WorkflowAgent, ide.Done},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("agents\n%+v\nwant\n%+v", got, want)
	}
	sub, wf, a1, a2 := modelAgent(t, m, subKey), modelAgent(t, m, wfKey), modelAgent(t, m, tests), modelAgent(t, m, docs)
	if sub.Started != at(71) || sub.Ended != at(85) || sub.ToolCalls != 2 {
		t.Errorf("subagent %+v", sub)
	}
	if wf.Started != at(282) || wf.Ended != at(340) {
		t.Errorf("workflow %+v", wf)
	}
	if a1.Started.IsZero() || a1.Ended != at(338) || a1.Tokens != 22228 || a1.ToolCalls != 3 ||
		a2.Started.IsZero() || a2.Ended != at(339) || a2.Tokens != 22003 || a2.ToolCalls != 2 {
		t.Errorf("workflow agents %+v\n%+v", a1, a2)
	}
	if main := modelAgent(t, m, "main"); main.ToolCalls != 5 {
		t.Errorf("main made %d calls, want 5", main.ToolCalls)
	}

	wantAsks := []ask{
		{"Edit", sub.Name, sub.Color}, {"Workflow", "", 0}, {"Write", "tests", a1.Color},
		{"Bash", "tests", a1.Color}, {"Edit", "docs", a2.Color}, {"Bash", "", 0},
	}
	if !reflect.DeepEqual(asks, wantAsks) || len(*sent) != len(wantAsks) {
		t.Errorf("popups %+v\nwant %+v, sent %d", asks, wantAsks, len(*sent))
	}

	if got := modelOpen(m); len(got) != 0 {
		t.Errorf("open calls %q", got)
	}

	for _, e := range m.Chat {
		if e.Kind != AssistantEntry {
			t.Errorf("chat entry %+v", e)
		}
	}
	// All agents done: the left column is back on the files.
	if len(m.crew.tailers) != 0 || m.Panel != ideview.Files || m.Status.State != "idle" || m.Status.Busy {
		t.Errorf("tailers %d panel %v status %+v", len(m.crew.tailers), m.Panel, m.Status)
	}
}
