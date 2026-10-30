package sim

import (
	"time"

	"github.com/pixelbender/claudit/internal/ide"
)

// How long the camera stays on an agent before it may cut to a more interesting one, and how
// long the agent on camera may sit idle before the camera leaves it at once.
const (
	dwell     = 2500 * time.Millisecond
	idleDwell = 400 * time.Millisecond
)

// Pin keeps the camera on the agent with that key; "" lets it follow the action again.
func (s *Sim) Pin(key string) {
	s.st.Viewed = ""
	if key == "" {
		s.st.Pinned = false
		return
	}
	if s.FindAgent(key) == nil {
		return
	}
	if s.st.Camera != key {
		s.st.CameraAt = s.now // Pin has no clock of its own: the last Tick
	}
	s.st.Camera, s.st.Pinned, s.idleAt = key, true, time.Time{}
}

// Ask turns the camera to the agent with that key while it waits for the user's answer, unless
// the camera is pinned; "" when no agent waits.
func (s *Sim) Ask(key string) { s.asking = key }

// direct moves the camera to the most interesting agent, without cutting back and forth. It
// reports whether the camera moved.
func (s *Sim) direct(now time.Time) bool {
	cur := s.score(s.FindAgent(s.st.Camera))
	switch {
	case cur > 0:
		s.idleAt = time.Time{}
	case s.idleAt.IsZero():
		s.idleAt = now
	}
	if s.camAt.IsZero() {
		s.camAt = now
	}
	if s.st.Pinned {
		return false
	}
	if a := s.FindAgent(s.asking); a != nil { // who asks is the story now: cut at once
		if s.st.Camera == a.Key {
			return false
		}
		s.st.Camera, s.st.CameraAt, s.camAt, s.idleAt = a.Key, now, now, time.Time{}
		return true
	}
	var best *ide.Agent
	top := cur
	for _, a := range s.st.Agents {
		if sc := s.score(a); sc > top {
			best, top = a, sc
		}
	}
	// Nothing to watch, and the agent on camera is gone for good: back to main.
	if a := s.FindAgent(s.st.Camera); best == nil && cur == 0 && a != nil && a.Kind != ide.Main && (a.Status == ide.Done || a.Status == ide.Failed) {
		best = s.st.Agents[0]
	}
	if best == nil || now.Sub(s.camAt) < dwell && (cur > 0 || now.Sub(s.idleAt) < idleDwell) {
		return false
	}
	s.st.Camera, s.st.CameraAt, s.camAt, s.idleAt = best.Key, now, now, time.Time{}
	return true
}

// score is how interesting the lane of agent a (nil for none) is to watch right now: the score of
// the step it plays next, 0 when idle.
func (s *Sim) score(a *ide.Agent) int {
	if l := s.lanes[a]; l != nil && l.cur != nil {
		return l.cur.steps[0].score
	}
	return 0
}
