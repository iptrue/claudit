package transcript

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pixelbender/claudit/internal/protocol"
)

const (
	testsAgent = "ac4272cdc31efcff1" // testdata/workflow: Read, Write, Bash
	docsAgent  = "ad85e07399d07f94d" // testdata/workflow: Read, Edit
)

func TestDir(t *testing.T) {
	tests := []struct {
		name, output, want string
		ok                 bool
	}{
		{"line in the middle", "Task ID: x\nTranscript dir: /tmp/wf_1\nRun ID: wf_1\n", "/tmp/wf_1", true},
		{"last line with CRLF", "Task ID: x\r\nTranscript dir: /tmp/wf 2\r\n", "/tmp/wf 2", true},
		{"no such line", "Workflow launched in background.", "", false},
		{"empty path", "Transcript dir: \nRun ID: wf_1", "", false},
		{"not on its own line", "see Transcript dir: /tmp/wf_1", "", false},
		{"empty", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Dir(tt.output)
			if got != tt.want || ok != tt.ok {
				t.Errorf("Dir(%q) = %q, %v; want %q, %v", tt.output, got, ok, tt.want, tt.ok)
			}
		})
	}
}

// parseMulti parses testdata/multi.jsonl, the stream that launched the testdata/workflow run.
func parseMulti(t *testing.T) []protocol.Event {
	t.Helper()
	data, err := os.ReadFile("../testdata/multi.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var evs []protocol.Event
	for line := range bytes.Lines(data) {
		evs = append(evs, protocol.Parse(line)...)
	}
	return evs
}

func TestDirOfRecordedWorkflow(t *testing.T) {
	var workflow string
	for _, ev := range parseMulti(t) {
		switch ev := ev.(type) {
		case protocol.ToolUse:
			if ev.Name == "Workflow" {
				workflow = ev.ID
			}
		case protocol.ToolResult:
			if ev.ID != workflow {
				continue
			}
			want := "/Users/pixelbender/.claude/projects/-Users-pixelbender-Projects-RustyChip-shell-fixtures-multi/" +
				"24d822ed-707f-42c1-8271-2ffe4508f180/subagents/workflows/wf_ed97e23c-fbb"
			if got, ok := Dir(ev.Output); got != want || !ok {
				t.Errorf("Dir = %q, %v; want %q", got, ok, want)
			}
			return
		}
	}
	t.Fatal("no Workflow tool result in testdata/multi.jsonl")
}

// copyFixture copies testdata/workflow into a fresh temp dir.
func copyFixture(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "wf")
	if err := os.CopyFS(dir, os.DirFS("../testdata/workflow")); err != nil {
		t.Fatal(err)
	}
	return dir
}

func appendFile(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
}

// call is the comparable part of an event.
type call struct {
	kind, id, name, parent string
}

func calls(evs []protocol.Event) []call {
	var cs []call
	for _, ev := range evs {
		switch ev := ev.(type) {
		case protocol.ToolUse:
			cs = append(cs, call{"use", ev.ID, ev.Name, ev.Parent})
		case protocol.ToolResult:
			cs = append(cs, call{"result", ev.ID, "", ""})
		default:
			cs = append(cs, call{kind: "unexpected"})
		}
	}
	return cs
}

func TestPollFixture(t *testing.T) {
	tl := New(copyFixture(t))
	evs := tl.Poll()

	// Counted from the fixture: tool_use and tool_result blocks of each agent file, files in name order.
	want := []call{
		{"use", "toolu_018RDap3nSVj9dHsfozBuRPM", "Read", testsAgent},
		{"result", "toolu_018RDap3nSVj9dHsfozBuRPM", "", ""},
		{"use", "toolu_01QhSLyCxRYU566eiSVpGkRs", "Write", testsAgent},
		{"result", "toolu_01QhSLyCxRYU566eiSVpGkRs", "", ""},
		{"use", "toolu_015srjqh2CMUmrNqtaw4J4q5", "Bash", testsAgent},
		{"result", "toolu_015srjqh2CMUmrNqtaw4J4q5", "", ""},
		{"use", "toolu_01H9W6RKhkXnzz7PTYonSWet", "Read", docsAgent},
		{"result", "toolu_01H9W6RKhkXnzz7PTYonSWet", "", ""},
		{"use", "toolu_01SFnFyC44Qk3rmpvcCVQK5X", "Edit", docsAgent},
		{"result", "toolu_01SFnFyC44Qk3rmpvcCVQK5X", "", ""},
	}
	if got := calls(evs); !reflect.DeepEqual(got, want) {
		t.Fatalf("Poll =\n%v\nwant\n%v", got, want)
	}

	if in := evs[0].(protocol.ToolUse).Input; !reflect.DeepEqual(in, map[string]any{
		"file_path": "/Users/pixelbender/Projects/RustyChip/shell/internal/testdata/multi/calc.py",
	}) {
		t.Errorf("Read input = %v", in)
	}
	if r := evs[5].(protocol.ToolResult); r.Output != "all tests passed\nEXIT CODE: 0" || r.IsError {
		t.Errorf("Bash result = %+v", r)
	}
	if out := evs[1].(protocol.ToolResult).Output; !strings.HasPrefix(out, "1\t\"\"\"Tiny calculator module.\"\"\"\n") {
		t.Errorf("Read output = %q", out)
	}

	if evs := tl.Poll(); len(evs) != 0 {
		t.Errorf("second Poll = %v, want nothing", calls(evs))
	}
}

// The agents of the recorded workflow asked for permission with their agent id; their transcripts hold
// those very tool calls, so permission popups and transcript events can be matched by agent.
func TestPollMatchesRecordedPermissions(t *testing.T) {
	byID := map[string]string{} // tool use id → agent id
	for _, ev := range New(copyFixture(t)).Poll() {
		if u, ok := ev.(protocol.ToolUse); ok {
			byID[u.ID] = u.Parent
		}
	}
	n := 0
	for _, ev := range parseMulti(t) {
		p, ok := ev.(protocol.PermissionRequest)
		if !ok || (p.AgentID != testsAgent && p.AgentID != docsAgent) {
			continue
		}
		n++
		if byID[p.ToolUseID] != p.AgentID {
			t.Errorf("%s %s asked by %s: transcript has it under %q", p.ToolName, p.ToolUseID, p.AgentID, byID[p.ToolUseID])
		}
	}
	if n != 3 { // Write and Bash by tests, Edit by docs
		t.Errorf("%d workflow permission requests, want 3", n)
	}
}

func TestPollIncremental(t *testing.T) {
	dir := copyFixture(t)
	tl := New(dir)
	tl.Poll()

	use := `{"type":"assistant","agentId":"` + docsAgent + `","message":{"role":"assistant","content":[` +
		`{"type":"text","text":"next"},{"type":"tool_use","id":"toolu_new","name":"Bash","input":{"command":"ls"}}]}}` + "\n"
	docs := filepath.Join(dir, "agent-"+docsAgent+".jsonl")
	half := len(use) / 2

	appendFile(t, docs, use[:half])
	if evs := tl.Poll(); len(evs) != 0 {
		t.Fatalf("Poll after half a line = %v, want nothing", calls(evs))
	}
	appendFile(t, docs, use[half:])
	evs := tl.Poll()
	want := []protocol.Event{protocol.ToolUse{ID: "toolu_new", Name: "Bash", Input: map[string]any{"command": "ls"}, Parent: docsAgent}}
	if !reflect.DeepEqual(evs, want) {
		t.Fatalf("Poll after the rest of the line =\n%#v\nwant\n%#v", evs, want)
	}

	// A new agent shows up; the journal and records without tool blocks are not events.
	appendFile(t, filepath.Join(dir, "journal.jsonl"), `{"type":"started","agentId":"a9","label":"lint","phase":"Check"}`+"\n")
	appendFile(t, filepath.Join(dir, "agent-a9.jsonl"),
		`{"type":"user","message":{"role":"user","content":"lint it"}}`+"\n"+
			`{"type":"system","subtype":"init","cwd":"/x"}`+"\n"+
			`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t9","name":"Grep","input":{"pattern":"TODO"}}]}}`+"\n"+
			`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t9","is_error":true,`+
			`"content":[{"type":"text","text":"no"},{"type":"text","text":"matches"}]}]}}`+"\n")
	evs = tl.Poll()
	want = []protocol.Event{
		protocol.ToolUse{ID: "t9", Name: "Grep", Input: map[string]any{"pattern": "TODO"}, Parent: "a9"},
		protocol.ToolResult{ID: "t9", Output: "no\nmatches", IsError: true},
	}
	if !reflect.DeepEqual(evs, want) {
		t.Fatalf("Poll after a new agent =\n%#v\nwant\n%#v", evs, want)
	}
	if evs := tl.Poll(); len(evs) != 0 {
		t.Errorf("idle Poll = %v, want nothing", calls(evs))
	}
}

func TestPollLargeLine(t *testing.T) {
	dir := t.TempDir()
	out := strings.Repeat("x", 3<<20)
	appendFile(t, filepath.Join(dir, "agent-big.jsonl"),
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"`+out+`"}]}}`+"\n")
	evs := New(dir).Poll()
	if len(evs) != 1 || evs[0].(protocol.ToolResult).Output != out {
		t.Errorf("Poll returned %d events, want one result with a %d-byte output", len(evs), len(out))
	}
}

func TestPollMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-yet")
	tl := New(dir)
	if evs := tl.Poll(); evs != nil {
		t.Errorf("Poll of a missing dir = %v", calls(evs))
	}
	// The dir may appear later.
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	appendFile(t, filepath.Join(dir, "agent-a1.jsonl"),
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Read","input":{}}]}}`+"\n")
	if got, want := calls(tl.Poll()), []call{{"use", "t1", "Read", "a1"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Poll = %v, want %v", got, want)
	}
}

// A file rewritten shorter is read again from its start.
func TestPollRewritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent-a1.jsonl")
	rec := func(id string) string {
		return `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"` + id + `","name":"Read","input":{}}]}}` + "\n"
	}
	appendFile(t, path, rec("t1")+rec("t2"))
	tl := New(dir)
	tl.Poll()
	if err := os.WriteFile(path, []byte(rec("t3")), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := calls(tl.Poll()), []call{{"use", "t3", "Read", "a1"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("Poll = %v, want %v", got, want)
	}
}
