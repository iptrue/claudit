package ide

import (
	"strings"
	"unicode/utf8"
)

const (
	scrollback      = 10000
	scrollbackBytes = 10 << 20
	termLineBytes   = 16 << 10
)

// Print appends a line of kind to the scrollback.
func (t *Term) Print(kind LineKind, text string) { t.PrintLine(TermLine{Kind: kind, Text: text}) }

// PrintLine keeps the newest 10000 lines within 10 MiB, with at most 16 KiB per line.
func (t *Term) PrintLine(l TermLine) {
	if t.kept != len(t.Lines) {
		t.bytes = 0
		for _, line := range t.Lines {
			t.bytes += len(line.Text)
		}
	}
	if len(l.Text) > termLineBytes {
		const more = "… [truncated]"
		n := termLineBytes - len(more)
		for n > 0 && !utf8.RuneStart(l.Text[n]) {
			n--
		}
		l.Text = l.Text[:n] + more
	} else {
		l.Text = strings.Clone(l.Text) // a retained line must not keep an entire tool result alive
	}
	t.bytes += len(l.Text)
	t.Lines = append(t.Lines, l)
	n := 0
	for len(t.Lines)-n > scrollback || t.bytes > scrollbackBytes {
		t.bytes -= len(t.Lines[n].Text)
		n++
	}
	clear(t.Lines[:n])
	t.Lines = t.Lines[n:]
	t.kept = len(t.Lines)
}
