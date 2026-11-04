package ideview

import (
	"slices"

	"github.com/pixelbender/claudit/internal/ide"
)

// TermCache keeps wrapped row counts for visible terminals between frames.
type TermCache map[*ide.Term]termLayout

type termLayout struct {
	width int
	lines []ide.TermLine
	rows  []int
	total int
}

func (o Options) terminalLayout(t *ide.Term, width int) termLayout {
	if cached, ok := o.Terms[t]; ok && cached.width == width && slices.Equal(cached.lines, t.Lines) {
		return cached
	}
	layout := termLayout{width: width, rows: make([]int, len(t.Lines)), total: 1}
	for i, l := range t.Lines {
		first := width
		if l.Kind == ide.Command || l.Kind == ide.More {
			first -= min(len(termPrompt), max(width, 0))
		}
		layout.rows[i] = wrappedRows(l.Text, first, width)
		layout.total += layout.rows[i]
	}
	if o.Terms != nil {
		layout.lines = slices.Clone(t.Lines)
		o.Terms[t] = layout
	}
	return layout
}
