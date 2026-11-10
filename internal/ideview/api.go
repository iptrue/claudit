package ideview

import (
	"time"

	"github.com/pixelbender/claudit/internal/ide"
)

// Panel is what the left column shows.
type Panel int

const (
	Files Panel = iota
	Agents
)

// Options are the view settings that are not part of the IDE state.
type Options struct {
	Panel   Panel
	Focused bool      // the left column has keyboard focus
	Sel     string    // agents panel: key of the highlighted agent
	Now     time.Time // for durations, spinners and effects
	Busy    bool      // the main agent is at work (its ide.Agent stays Running between turns)
	Wrap    bool      // the editor wraps long lines instead of scrolling sideways
	Raw     bool      // the editor shows OpenSpec specs as written, not structured
	Specs   Specs     // how the user folded the specs shown structured
	Scroll  Scroll    // how far the user scrolled the panes away from their agents
	Terms   TermCache // wrapped row counts shared by rendering and mouse handling
}

// AgentRows lists the keys of the selectable rows of the agents panel, top to bottom.
func AgentRows(st *ide.State) []string {
	var keys []string
	for _, r := range agentTree(st) {
		if r.agent != nil {
			keys = append(keys, r.agent.Key)
		}
	}
	return keys
}
