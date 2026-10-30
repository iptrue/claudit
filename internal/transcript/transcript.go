// Package transcript follows the transcripts a workflow run writes to disk. A workflow agent's tool
// calls never reach the stream-json output; its transcript is the only place to see them.
package transcript

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pixelbender/claudit/internal/protocol"
)

// Dir finds the "Transcript dir: <path>" line in a Workflow tool result.
func Dir(output string) (string, bool) {
	for line := range strings.Lines(output) {
		if dir, ok := strings.CutPrefix(line, "Transcript dir: "); ok {
			dir = strings.TrimSpace(dir)
			return dir, dir != ""
		}
	}
	return "", false
}

// Tailer reads a workflow transcript dir incrementally.
type Tailer struct {
	dir     string
	offsets map[string]int64 // file name → bytes already consumed
}

func New(dir string) *Tailer { return &Tailer{dir: dir} }

// Poll returns what the workflow's agents did since the last call: protocol.ToolUse events whose
// Parent is the agent id, and protocol.ToolResult events, in file order per agent. A line still
// being written is left for the next call. Missing or unreadable files yield nothing.
func (t *Tailer) Poll() []protocol.Event {
	entries, err := os.ReadDir(t.dir)
	if err != nil {
		return nil
	}
	if t.offsets == nil {
		t.offsets = map[string]int64{}
	}
	var evs []protocol.Event
	for _, e := range entries {
		id, ok := agentID(e.Name())
		if !ok || e.IsDir() {
			continue
		}
		data := t.readNew(e.Name())
		for line := range bytes.Lines(data) {
			// Transcript records share the stream-json shapes of assistant and user messages.
			for _, ev := range protocol.Parse(line) {
				switch ev := ev.(type) {
				case protocol.ToolUse:
					ev.Parent = id
					evs = append(evs, ev)
				case protocol.ToolResult:
					evs = append(evs, ev)
				}
			}
		}
	}
	return evs
}

// agentID extracts <id> from a file name "agent-<id>.jsonl".
func agentID(name string) (string, bool) {
	id, ok := strings.CutPrefix(name, "agent-")
	if !ok {
		return "", false
	}
	id, ok = strings.CutSuffix(id, ".jsonl")
	return id, ok && id != ""
}

// readNew returns the complete lines appended to name since the last call and consumes them.
func (t *Tailer) readNew(name string) []byte {
	f, err := os.Open(filepath.Join(t.dir, name))
	if err != nil {
		return nil
	}
	defer f.Close()
	off := t.offsets[name]
	if fi, err := f.Stat(); err == nil && fi.Size() < off {
		off = 0 // rewritten shorter: read it again from the start
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil
	}
	end := bytes.LastIndexByte(data, '\n') + 1 // a partial last line waits for its newline
	t.offsets[name] = off + int64(end)
	return data[:end]
}
