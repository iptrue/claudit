package ide

const scrollback = 1000

// Print appends a line of kind to the scrollback.
func (t *Term) Print(kind LineKind, text string) { t.PrintLine(TermLine{Kind: kind, Text: text}) }

// PrintLine appends l to the scrollback, dropping the oldest beyond 1000 lines.
func (t *Term) PrintLine(l TermLine) {
	t.Lines = append(t.Lines, l)
	if n := len(t.Lines) - scrollback; n > 0 {
		t.Lines = t.Lines[n:]
	}
}
