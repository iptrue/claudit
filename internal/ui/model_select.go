package ui

import (
	"image"
	"image/color"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/pixelbender/claudit/internal/theme"
)

// selection is the text the left button drags over, kept within the text of the pane where the
// press was: the chat or a pane of the IDE.
type selection struct {
	box      image.Rectangle // the pane's text on screen; empty for none
	from, to image.Point     // where the press was, where the mouse is
	drag     bool            // the mouse moved with the button down: there is something to copy
	text     string          // what it covers, as the last frame drew it
	released time.Time       // when the button let go and what it covers was copied
}

// How a copied selection goes out: a flash as the button lets go, lit a moment, so that the copy
// shows, then fading; the status line's note of the copy stays a while longer, then fades too.
const (
	selFlash  = 100 * time.Millisecond
	selHold   = 300 * time.Millisecond
	selFade   = 200 * time.Millisecond
	noteHold  = time.Second
	noteFade  = 300 * time.Millisecond
	drawNoted = "copied to clipboard"
)

// copiedColor is the light blue of a copy: the status line's note of it, the flash of what it took.
var copiedColor = theme.Mix(theme.Cyan, theme.Bright, 0.35)

// selFlashBg lights a selection as it is copied: in the light blue of the note that says so.
var selFlashBg = theme.Mix(copiedColor, theme.Bg, 0.45)

// out is how far the light of s has gone at now: 0 while it is dragged and for selHold after the
// release, then up to 1 over selFade.
func (s selection) out(now time.Time) float64 {
	if s.released.IsZero() {
		return 0
	}
	return min(max(float64(now.Sub(s.released)-selHold)/float64(selFade), 0), 1)
}

// lit is the colour s lights its cells in at now: the flash for selFlash after the release.
func (s selection) lit(now time.Time) color.Color {
	if !s.released.IsZero() && now.Sub(s.released) < selFlash {
		return selFlashBg
	}
	return theme.SelBg
}

// noted is how bright the status line's note of the copy of s is at now: 1 from the release, down
// to 0 over noteFade after noteHold; 0 for no copy.
func (s selection) noted(now time.Time) float64 {
	if s.released.IsZero() {
		return 0
	}
	return 1 - min(max(float64(now.Sub(s.released)-noteHold)/float64(noteFade), 0), 1)
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

// copySelection ends the selection at the release of the button and copies what it covers; its
// light goes out after it.
func (m *Model) copySelection() tea.Cmd {
	text := m.sel.text
	if !m.sel.released.IsZero() { // copied already: another button let go
		return nil
	}
	if strings.TrimSpace(text) == "" { // blank rows light nothing: nothing to copy
		m.sel = selection{}
		return nil
	}
	m.sel.released = m.clock()
	return copyText(text)
}

// copyText uses the system clipboard, falling back to the terminal's OSC 52 support.
func copyText(text string) tea.Cmd {
	return func() tea.Msg {
		if writeClipboardText(text) == nil {
			return nil
		}
		return tea.SetClipboard(text)()
	}
}
