package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/pixelbender/claudit/internal/protocol"
)

// QuitPopup asks before closing a session with unfinished background work. Stay is the default.
type QuitPopup struct {
	Tasks  []protocol.BackgroundTask
	Scroll int
	Exit   bool
}

func (*QuitPopup) isPopup() {}

func (m *Model) quitPopupKey(p *QuitPopup, k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "left", "right", "tab":
		p.Exit = !p.Exit
	case "up":
		p.Scroll--
	case "down":
		p.Scroll++
	case "pgup":
		p.Scroll -= 10
	case "pgdown":
		p.Scroll += 10
	case "enter":
		if p.Exit {
			return tea.Quit
		}
		m.cancelQuit()
	case "y":
		return tea.Quit
	case "n", "esc":
		m.cancelQuit()
	}
	return nil
}

func (m *Model) cancelQuit() {
	m.Popup, m.quitKey = nil, ""
	if len(m.queue) > 0 {
		m.Popup, m.queue = m.queue[0], m.queue[1:]
	} else if m.Status.State == "waiting for you" {
		m.Status.State = m.behind
	}
}
