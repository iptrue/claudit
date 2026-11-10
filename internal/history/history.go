// Package history reads and adds to the prompt history Claude Code keeps for ↑ and ↓:
// <config dir>/history.jsonl, one JSON line a prompt, with the project it was typed in. claude -p
// writes none of it, so claudit adds its own prompts there, in the same form, for Claude Code and
// claudit to recall each other's.
package history

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// Limit is how many prompts of a project are recalled, as in Claude Code.
const Limit = 100

// Entry is a prompt as typed, its pasted texts by the number of their placeholder.
type Entry struct {
	Text   string
	Pasted map[int]string
}

// line is an entry as Claude Code writes it.
type line struct {
	Display string                 `json:"display"`
	Pasted  map[string]pastedEntry `json:"pastedContents"`
	At      int64                  `json:"timestamp"`
	Project string                 `json:"project"`
	Session string                 `json:"sessionId,omitempty"`
}

// pastedEntry is a pasted text: in the line, or in the paste cache under its hash.
type pastedEntry struct {
	ID      int    `json:"id"`
	Type    string `json:"type"`
	Content string `json:"content,omitempty"`
	Hash    string `json:"contentHash,omitempty"`
}

// File is the history file: in CLAUDE_CONFIG_DIR, else ~/.claude.
func File() string {
	config := os.Getenv("CLAUDE_CONFIG_DIR")
	if config == "" {
		home, _ := os.UserHomeDir()
		config = filepath.Join(home, ".claude")
	}
	return filepath.Join(config, "history.jsonl")
}

// Project is how the history names a project: the real path of its directory.
func Project(dir string) string {
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		return real
	}
	return dir
}

// Load returns the last limit prompts typed in project, oldest first, a prompt repeated right
// after itself, with the same pasted texts, once. A missing file is an empty history; lines it cannot read are skipped.
func Load(file, project string, limit int) ([]Entry, error) {
	f, err := os.Open(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []line
	r := bufio.NewReader(f)
	for {
		b, err := r.ReadBytes('\n')
		var l line
		if len(b) > 0 && json.Unmarshal(b, &l) == nil && l.Project == project && l.Display != "" {
			if n := len(lines); n == 0 || lines[n-1].Display != l.Display || !maps.Equal(lines[n-1].Pasted, l.Pasted) {
				lines = append(lines, l)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	lines = lines[max(0, len(lines)-limit):]
	cache := filepath.Join(filepath.Dir(file), "paste-cache")
	out := make([]Entry, len(lines))
	for i, l := range lines {
		out[i] = Entry{Text: l.Display, Pasted: pasted(l.Pasted, cache)}
	}
	return out, nil
}

// pasted is the texts of a line's pasted contents, from the line or the paste cache; one the
// cache no longer has is left out.
func pasted(contents map[string]pastedEntry, cache string) map[int]string {
	var out map[int]string
	for _, p := range contents {
		if p.Type != "text" {
			continue
		}
		text := p.Content
		if text == "" && p.Hash != "" && filepath.Base(p.Hash) == p.Hash {
			b, err := os.ReadFile(filepath.Join(cache, p.Hash+".txt"))
			if err != nil {
				continue
			}
			text = string(b)
		}
		if text == "" {
			continue
		}
		if out == nil {
			out = map[int]string{}
		}
		out[p.ID] = text
	}
	return out
}

// Append adds a prompt typed in project to the history, as one line written at once at the end
// of the file, as the Claude Code sessions running beside it do.
func Append(file, project, session string, e Entry, at time.Time) error {
	l := line{Display: e.Text, Pasted: map[string]pastedEntry{}, At: at.UnixMilli(), Project: project, Session: session}
	ids := make([]int, 0, len(e.Pasted))
	for id := range e.Pasted {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		l.Pasted[strconv.Itoa(id)] = pastedEntry{ID: id, Type: "text", Content: e.Pasted[id]}
	}
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return errors.Join(err, f.Close())
}
