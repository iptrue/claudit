package ide

const scrollback = 1000

// Print appends a line to the scrollback, dropping the oldest beyond 1000 lines.
func (t *Term) Print(kind LineKind, text string) {
	t.Lines = append(t.Lines, TermLine{Kind: kind, Text: text})
	if n := len(t.Lines) - scrollback; n > 0 {
		t.Lines = t.Lines[n:]
	}
}
