package sim

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/ide"
)

// locate finds the byte offset in pre where old is replaced. When text already holds the edit
// (applied, or old is missing: the tool ran before the file was read, as in a replay) pre is text
// as it was before the edit. ok is false when neither string is in text.
func locate(text, old, repl string, all, applied bool) (pre string, at int, ok bool) {
	i, j := strings.Index(text, old), strings.Index(text, repl)
	if repl != "" && j >= 0 && (i < 0 || applied) {
		if all {
			return strings.ReplaceAll(text, repl, old), j, true
		}
		return text[:j] + old + text[j+len(repl):], j, true
	}
	return text, i, i >= 0
}

// seek finds old in text at the occurrence nearest to byte offset near; ok is false when there is
// none. old must not be empty.
func seek(text, old string, near int) (at int, ok bool) {
	at = -1
	for i := 0; ; {
		j := strings.Index(text[i:], old)
		if j < 0 {
			break
		}
		j += i
		if at < 0 || max(j-near, near-j) < max(at-near, near-at) {
			at = j
		}
		if j >= near { // the next ones are farther
			break
		}
		i = j + 1
	}
	return at, at >= 0
}

// nobody is a caret key no agent has.
const nobody = "\x00"

// patch turns the buffer into text by replacing only the part that differs, as if the file
// changed by itself: every agent's caret keeps its place in the text.
func patch(b *ide.Buffer, text string) {
	old := b.Text()
	if old == text {
		return
	}
	p := 0
	for p < len(old) && p < len(text) && old[p] == text[p] {
		p++
	}
	for p > 0 && p < len(old) && !utf8.RuneStart(old[p]) {
		p--
	}
	q := 0
	for q < len(old)-p && q < len(text)-p && old[len(old)-1-q] == text[len(text)-1-q] {
		q++
	}
	for q > 0 && !utf8.RuneStart(old[len(old)-q]) {
		q--
	}
	from := advance(ide.Pos{}, old[:p])
	b.Delete(nobody, from, advance(from, old[p:len(old)-q]))
	b.Insert(nobody, text[p:len(text)-q])
	delete(b.Carets, nobody)
}

// offset is the byte offset of p in the text of b.
func offset(b *ide.Buffer, p ide.Pos) int {
	n := 0
	for _, l := range b.Lines[:p.Line] {
		n += len(l) + 1
	}
	for i := range b.Lines[p.Line] {
		if p.Col == 0 {
			return n + i
		}
		p.Col--
	}
	return n + len(b.Lines[p.Line])
}

// advance is where the caret ends after typing s at p.
func advance(p ide.Pos, s string) ide.Pos {
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return ide.Pos{Line: p.Line + strings.Count(s, "\n"), Col: utf8.RuneCountInString(s[i+1:])}
	}
	return ide.Pos{Line: p.Line, Col: p.Col + utf8.RuneCountInString(s)}
}

// snapshot reads a file as the editor shows it; ok is false when it cannot be read or is binary.
func snapshot(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes))
	if err != nil || bytes.IndexByte(data, 0) >= 0 {
		return "", false
	}
	return headLines(fileText(string(data)), maxLines), true
}

// numbered turns Read output ("<n>\t<text>" per line) back into file text. Lines without a
// number (notes appended by the tool) are dropped.
func numbered(out string) string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		num, text, ok := strings.Cut(strings.TrimLeft(l, " "), "\t")
		n, err := strconv.Atoi(num)
		if !ok || err != nil || n < 1 || n > maxLines {
			continue
		}
		for len(lines) < n {
			lines = append(lines, "")
		}
		lines[n-1] = text
	}
	return fileText(strings.Join(lines, "\n"))
}

// fileText is text as the editor shows it: valid UTF-8, no "\r", tabs as 4 spaces.
func fileText(s string) string {
	s = strings.ReplaceAll(strings.ToValidUTF8(s, "�"), "\r", "")
	return strings.ReplaceAll(s, "\t", "    ")
}

// headLines keeps the first n lines of s.
func headLines(s string, n int) string {
	for i, c := 0, 0; i < len(s); i++ {
		if s[i] == '\n' {
			if c++; c == n {
				return s[:i]
			}
		}
	}
	return s
}

// termLines splits command output into printable lines. Of a line rewritten with "\r" (a progress
// bar) only the last version stays.
func termLines(s string) []string {
	s = strings.TrimRight(strings.ReplaceAll(ansi.Strip(s), "\r\n", "\n"), "\n")
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = printable(l[strings.LastIndexByte(l, '\r')+1:])
	}
	return lines
}

// printable drops control characters and turns tabs into 4 spaces.
func printable(s string) string {
	s = strings.ReplaceAll(strings.ToValidUTF8(s, "�"), "\t", "    ")
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// oneLine is the first line of s, printable.
func oneLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return printable(line)
}

// quote single-quotes s for a shell command line.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// abs resolves a tool path against the root.
func (s *Sim) abs(p string) string {
	if !filepath.IsAbs(p) {
		p = filepath.Join(s.st.Root, p)
	}
	return filepath.Clean(p)
}

// rel shortens a path inside the root for a command line.
func (s *Sim) rel(p string) string {
	if r, err := filepath.Rel(s.st.Root, p); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	return p
}

func str(in map[string]any, key string) string {
	s, _ := in[key].(string)
	return s
}

// num reads a number that JSON decoding left as float64 (or a model sent as a string).
func num(in map[string]any, key string) int {
	switch v := in[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}
