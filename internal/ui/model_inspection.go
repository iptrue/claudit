package ui

import (
	"maps"
	"slices"
	"time"

	"github.com/pixelbender/claudit/internal/ide"
	"github.com/pixelbender/claudit/internal/ideview"
)

const inspectionCooldown = 5 * time.Second

// inspection keeps the visible IDE still while the live simulation continues underneath it.
type inspection struct {
	state  *ide.State
	until  time.Time
	frame  time.Time
	height int
	busy   bool
}

func (m *Model) interact() {
	now := m.clock()
	if m.Now.After(now) {
		now = m.Now
	}
	if m.inspection == nil || !now.Before(m.inspection.until) {
		m.inspection = &inspection{state: copyIDE(m.Sim.State()), frame: m.Now, height: m.ideHeight, busy: m.Status.Busy}
	}
	m.inspection.until = now.Add(inspectionCooldown)
}

func (m *Model) ideState() *ide.State {
	if m.inspection != nil {
		return m.inspection.state
	}
	return m.Sim.State()
}

func (m *Model) pinView(key string) {
	if key != "" && m.Sim.FindAgent(key) == nil {
		return
	}
	m.pin(key)
	if m.inspection != nil {
		if key != "" && !slices.ContainsFunc(m.inspection.state.Agents, func(a *ide.Agent) bool { return a.Key == key }) {
			m.refreshInspection()
		}
		st := m.inspection.state
		st.Viewed, st.Pinned = "", key != ""
		if key != "" {
			st.Camera = key
		}
		m.letGo(false) // the IDE the user is reading let go of the file too
	}
}

// viewFile shows the file at path in the right column until alt+0. The explorer follows its row
// from then on: it stays where it was when the row is in sight, else the row is scrolled into it.
func (m *Model) viewFile(path string) bool {
	top := ideview.ExplorerTop(m.ideState(), ideview.Options{Scroll: m.scroll}, m.ideHeight)
	if !m.Sim.View(path) {
		return false
	}
	m.refreshInspection()
	m.scrollIDE(ideview.ExplorerScroll(m.ideState(), m.ideHeight, top, path))
	return true
}

// pin is Sim.Pin, with the explorer following the agent from where it is again when that lets go of
// the file the user opened.
func (m *Model) pin(key string) {
	held := m.Sim.State().Viewed != ""
	m.Sim.Pin(key)
	m.letGo(held)
}

// letGo makes the explorer follow the agent from where the agent is once the file the user opened,
// held before, holds the right column no more: how far it was scrolled was from that file's row.
// While the IDE the user is reading still shows the file, that waits for it to go.
func (m *Model) letGo(held bool) {
	viewed := m.Sim.State().Viewed != ""
	m.unheld = !viewed && (m.unheld || held)
	if m.unheld && (m.inspection == nil || m.inspection.state.Viewed == "") {
		delete(m.scroll, ideview.ExplorerPane)
		m.unheld = false
	}
}

func (m *Model) refreshInspection() {
	if m.inspection != nil {
		m.inspection.state = copyIDE(m.Sim.State())
		m.inspection.frame, m.inspection.busy = m.Now, m.Status.Busy
	}
}

// copyIDE owns the mutable slices, maps and carets the simulation can change between frames.
func copyIDE(st *ide.State) *ide.State {
	copy := *st
	copy.Tree = slices.Clone(st.Tree)
	copy.Touched, copy.Edited = maps.Clone(st.Touched), maps.Clone(st.Edited)
	copy.Agents = make([]*ide.Agent, len(st.Agents))
	for i, a := range st.Agents {
		agent := *a
		agent.Term.Lines = slices.Clone(a.Term.Lines)
		agent.Page.Lines = slices.Clone(a.Page.Lines)
		copy.Agents[i] = &agent
	}
	copy.Tabs = make([]*ide.Tab, len(st.Tabs))
	for i, t := range st.Tabs {
		tab := *t
		tab.Buf.Lines, tab.Buf.Glow = slices.Clone(t.Buf.Lines), slices.Clone(t.Buf.Glow)
		tab.Buf.Carets = make(map[string]*ide.Caret, len(t.Buf.Carets))
		for key, c := range t.Buf.Carets {
			caret := *c
			if c.Sel != nil {
				sel := *c.Sel
				caret.Sel = &sel
			}
			tab.Buf.Carets[key] = &caret
		}
		copy.Tabs[i] = &tab
	}
	return &copy
}
