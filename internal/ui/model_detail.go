package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// textLines splits s into lines; a final newline does not start another line, "" has none.
func textLines(s string) []string {
	if s = strings.TrimRight(s, "\n"); s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// permissionDetail describes what a tool is about to do, for the permission popup. Edit and Write
// details start with the file; then come diff lines (" " kept, "-" removed, "+" added) or the
// content.
func permissionDetail(name string, in map[string]any, root string) string {
	path := inputString(in, "file_path")
	switch name {
	case "Bash":
		return inputString(in, "command")
	case "Edit":
		diff := editDiff(path, inputString(in, "old_string"), inputString(in, "new_string"))
		return strings.Join(append([]string{relToRoot(root, path)}, diff...), "\n")
	case "Write":
		return relToRoot(root, path) + "\n" + strings.TrimRight(inputString(in, "content"), "\n")
	case "Read":
		d := relToRoot(root, path)
		if from, n := inputInt(in, "offset"), inputInt(in, "limit"); n > 0 {
			d += fmt.Sprintf(" · lines %d–%d", max(from, 1), max(from, 1)+n-1)
		}
		return d
	case "Grep", "Glob":
		d := inputString(in, "pattern")
		if p := inputString(in, "path"); p != "" {
			d += " in " + relToRoot(root, p)
		}
		return d
	case "WebFetch":
		return inputString(in, "url")
	case "WebSearch":
		return inputString(in, "query")
	case "ExitPlanMode":
		return inputString(in, "plan")
	case "Workflow":
		if script := inputString(in, "script"); script != "" {
			return script
		}
	}
	return jsonText(in, "  ")
}

// editContext is how many lines of the file a diff shows around an edit.
const editContext = 2

// editDiff is the change an edit of the file at path makes, as diff lines: the lines old and repl
// share at the start and the end stay as context, and so do editContext lines of the file around
// old, when the file holds it.
func editDiff(path, old, repl string) []string {
	o, r := textLines(old), textLines(repl)
	p := 0
	for p < len(o) && p < len(r) && o[p] == r[p] {
		p++
	}
	q := 0
	for q < len(o)-p && q < len(r)-p && o[len(o)-1-q] == r[len(r)-1-q] {
		q++
	}
	var before, after []string
	if data, err := os.ReadFile(path); err == nil && old != "" {
		text := strings.ReplaceAll(string(data), "\r\n", "\n")
		if i := strings.Index(text, old); i >= 0 {
			lines := strings.Split(text, "\n")
			first := strings.Count(text[:i], "\n")
			last := first + strings.Count(strings.TrimSuffix(old, "\n"), "\n")
			before = lines[max(0, first-editContext):first]
			after = lines[min(last+1, len(lines)):min(last+1+editContext, len(lines))]
		}
	}
	var out []string
	mark := func(m string, lines []string) {
		for _, l := range lines {
			out = append(out, m+l)
		}
	}
	mark(" ", before)
	mark(" ", o[:p])
	mark("-", o[p:len(o)-q])
	mark("+", r[p:len(r)-q])
	mark(" ", o[len(o)-q:])
	mark(" ", after)
	return out
}

func inputString(in map[string]any, key string) string {
	s, _ := in[key].(string)
	return s
}

// inputInt reads a number of a tool input (JSON numbers decode as float64); 0 when absent.
func inputInt(in map[string]any, key string) int {
	n, _ := in[key].(float64)
	return int(n)
}

// relToRoot shows p relative to root when it lies inside root, unchanged otherwise.
func relToRoot(root, p string) string {
	if root == "" || !filepath.IsAbs(p) {
		return p
	}
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return p
	}
	return rel
}

func firstLineOf(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// jsonText renders v as JSON without HTML escaping; indent "" means compact.
func jsonText(v any, indent string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", indent)
	_ = enc.Encode(v) // v comes from decoded JSON, so it always encodes
	return strings.TrimSuffix(b.String(), "\n")
}
