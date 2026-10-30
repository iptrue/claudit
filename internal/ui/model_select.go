package ui

import (
	"image"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// selection is the text the left button drags over, kept within the text of the pane where the
// press was: the chat or a pane of the IDE.
type selection struct {
	box      image.Rectangle // the pane's text on screen; empty for none
	from, to image.Point     // where the press was, where the mouse is
	drag     bool            // the mouse moved with the button down: there is something to copy
	text     string          // what it covers, as the last frame drew it
}

// selectAt starts a selection at a press on (x, y), in the text of the topmost zone there.
func (m *Model) selectAt(x, y int) selection {
	p := image.Pt(x, y)
	for i := len(m.zones) - 1; i >= 0; i-- {
		z := m.zones[i]
		if !p.In(image.Rect(z.x, z.y, z.x+z.w, z.y+z.h)) {
			continue
		}
		if z.text == nil {
			return selection{}
		}
		box := z.text(x-z.x, y-z.y).Add(image.Pt(z.x, z.y))
		if !p.In(box) {
			return selection{}
		}
		return selection{box: box, from: p, to: p}
	}
	return selection{}
}

// dragTo moves the selection's end to (x, y), within its pane's text.
func (m *Model) dragTo(x, y int) {
	if m.sel.box.Empty() {
		return
	}
	b := m.sel.box
	to := image.Pt(min(max(x, b.Min.X), b.Max.X-1), min(max(y, b.Min.Y), b.Max.Y-1))
	if to != m.sel.from {
		m.sel.drag = true
	}
	m.sel.to = to
}

// copySelection ends the selection at the release of the button and copies what it covers.
func (m *Model) copySelection() tea.Cmd {
	text := m.sel.text
	m.sel = selection{}
	if text == "" {
		return nil
	}
	return copyText(text)
}

// copyText puts text on the clipboard through pbcopy, else through the terminal (OSC 52, which
// iTerm2 lets through only when allowed).
func copyText(text string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		if cmd.Run() == nil {
			return nil
		}
		return tea.SetClipboard(text)()
	}
}
