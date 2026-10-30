// Package sim turns the agents' tool calls into developers at work: they type commands into their
// terminals, print the output, open files and edit them with moving carets.
package sim

import (
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/pixelbender/claudit/internal/ide"
)

// Sim plays tool calls on an ide.State as time advances through Tick: every agent has a lane that
// plays its calls one after another, and all lanes play at once.
type Sim struct {
	st ide.State

	lanes  map[*ide.Agent]*lane
	calls  map[string]*scene // scenes by tool call id, until they no longer need the result
	held   map[string]bool   // tool calls waiting for the user's permission: their scenes wait too
	denied map[string]bool   // tool calls the user denied before their scene came
	asking string            // key of the agent waiting for the user's answer; "" when none is
	seq    int               // scenes created, seeds the jitter
	added  int               // agents ever added: the next colour (the ui may drop agents)
	camAt  time.Time         // when the camera moved to its agent
	now    time.Time         // the last Tick
	idleAt time.Time         // since when the camera's agent is idle; zero while it is busy
}

// lane is the work queue of one agent.
type lane struct {
	a     *ide.Agent
	queue []*scene  // scenes waiting to play
	cur   *scene    // the scene playing, nil when idle
	clock time.Time // when the last step ran; zero while idle, the last Tick while blocked
	runAt time.Time // when the running command started, for the spinner
}

// scene is the animation of one tool call: a FIFO of steps.
type scene struct {
	id    string
	seq   int
	l     *lane
	what  string       // the agent's Activity while it plays
	build func(*scene) // adds the steps when the scene starts playing
	steps []step
	score int          // camera interest of the steps being added
	buf   *ide.Buffer  // the buffer the scene works on, set by its open step
	res   *result      // the tool result, once it arrives
	after func(result) // runs when the result arrives after the animation is over
}

type result struct {
	out   string
	isErr bool
}

type step struct {
	delay time.Duration // wait before the step, before catch-up
	wait  bool          // the step needs the tool result
	score int           // how interesting it is to watch, for the camera
	run   func()
}

// New starts with the explorer showing root and the main agent on camera.
func New(root string) *Sim {
	s := &Sim{lanes: map[*ide.Agent]*lane{}, calls: map[string]*scene{}, held: map[string]bool{}, denied: map[string]bool{}}
	s.st.Touched, s.st.Edited = map[string]string{}, map[string]bool{}
	s.st.SetRoot(root)
	s.Agent("main", ide.Main).Name = "main"
	s.st.Camera = "main"
	return s
}

// SetRoot points the explorer at another directory (the agent's cwd, known once the session starts).
func (s *Sim) SetRoot(root string) {
	if filepath.Clean(root) != s.st.Root { // the same root again keeps the expanded folders
		s.st.SetRoot(root)
	}
}

// View shows the file at path alone in the right column, as on disk now, as the user clicked it
// in the explorer: until an agent's next tool call plays or the camera is pinned. It reports
// false for a file it cannot show, binary or unreadable.
func (s *Sim) View(path string) bool {
	text, ok := snapshot(path)
	if !ok {
		return false
	}
	if t := s.st.Open(path, text); !t.Buf.Modified { // unless an agent is typing in it
		patch(&t.Buf, text)
	}
	s.st.Viewed = path
	return true
}

// Toggle folds or unfolds a directory in the explorer, as the user clicked it. An agent's next
// step there unfolds what it needs again.
func (s *Sim) Toggle(path string) { s.st.Toggle(path) }

// State is what to draw. The caller may update the agents' bookkeeping (name, status, times),
// nothing else.
func (s *Sim) State() *ide.State { return &s.st }

// Agent returns the agent with that key, adding it when it is new.
func (s *Sim) Agent(key string, kind ide.AgentKind) *ide.Agent {
	if a := s.FindAgent(key); a != nil {
		return a
	}
	a := &ide.Agent{Key: key, Kind: kind, Status: ide.Running, Color: s.added}
	s.added++
	if kind != ide.Workflow {
		a.Hotkey = s.hotkey()
	}
	s.st.Agents = append(s.st.Agents, a)
	s.lanes[a] = &lane{a: a}
	return a
}

// hotkey is the lowest of 1–9 no agent has (the ui may drop agents), or 0 when all are taken.
func (s *Sim) hotkey() int {
	for n := 1; n <= 9; n++ {
		if !slices.ContainsFunc(s.st.Agents, func(a *ide.Agent) bool { return a.Hotkey == n }) {
			return n
		}
	}
	return 0
}

// FindAgent returns the agent with that key, or nil.
func (s *Sim) FindAgent(key string) *ide.Agent {
	for _, a := range s.st.Agents {
		if a.Key == key {
			return a
		}
	}
	return nil
}

// Rekey renames an agent everywhere: a queued workflow agent got its id.
func (s *Sim) Rekey(old, key string) {
	a := s.FindAgent(old)
	if a == nil || s.FindAgent(key) != nil {
		return
	}
	a.Key = key
	for _, o := range s.st.Agents {
		if o.Parent == old {
			o.Parent = key
		}
	}
	if s.st.Camera == old {
		s.st.Camera = key
	}
	for path, k := range s.st.Touched {
		if k == old {
			s.st.Touched[path] = key
		}
	}
	for _, t := range s.st.Tabs {
		if c := t.Buf.Carets[old]; c != nil {
			delete(t.Buf.Carets, old)
			t.Buf.Carets[key] = c
		}
	}
}

// ToolUse queues the animation of a tool call on the lane of the agent with that key (an unknown
// key is added as a subagent). It is called as soon as the call is announced, before the tool
// has run, so files read from disk here still hold their old content.
func (s *Sim) ToolUse(agent, id, name string, input map[string]any) {
	a := s.Agent(agent, ide.Subagent)
	if _, dup := s.calls[id]; dup || s.denied[id] {
		return
	}
	var build func(*scene)
	var what string
	switch name {
	case "Bash":
		cmd := str(input, "command")
		what = "$ " + cmd
		build = func(sc *scene) { s.command(sc, cmd, 0) }
	case "Grep":
		cmd := "rg -n " + quote(str(input, "pattern"))
		if p := str(input, "path"); p != "" {
			cmd += " " + s.rel(p)
		}
		what = "rg " + str(input, "pattern")
		build = func(sc *scene) { s.command(sc, cmd, maxOutput) }
	case "Glob":
		p := "."
		if v := str(input, "path"); v != "" {
			p = s.rel(v)
		}
		cmd := "find " + p + " -name " + quote(str(input, "pattern"))
		what = "find " + str(input, "pattern")
		build = func(sc *scene) { s.command(sc, cmd, maxOutput) }
	case "WebFetch":
		url := str(input, "url")
		what = "fetch " + url
		build = func(sc *scene) { s.browse(sc, url, false) }
	case "WebSearch":
		query := str(input, "query")
		what = "search " + strconv.Quote(query)
		build = func(sc *scene) { s.browse(sc, query, true) }
	case "Read":
		f := s.file(input)
		start, limit := max(0, num(input, "offset")-1), num(input, "limit")
		what = "Read " + filepath.Base(f.path)
		build = func(sc *scene) { s.read(sc, f, start, limit) }
	case "Edit":
		f := s.file(input)
		old, repl := fileText(str(input, "old_string")), fileText(str(input, "new_string"))
		all, _ := input["replace_all"].(bool)
		what = "Edit " + filepath.Base(f.path)
		build = func(sc *scene) { s.edit(sc, f, old, repl, all) }
	case "Write":
		f := s.file(input)
		content := fileText(str(input, "content"))
		what = "Write " + filepath.Base(f.path)
		build = func(sc *scene) { s.write(sc, f, content) }
	default:
		return
	}
	l := s.lanes[a]
	s.seq++
	sc := &scene{id: id, seq: s.seq, l: l, what: oneLine(what), build: build}
	l.queue = append(l.queue, sc)
	s.calls[id] = sc
}

// ToolResult delivers the outcome of the call with that id.
func (s *Sim) ToolResult(id, output string, isError bool) {
	if s.held[id] { // it ended without the user's permission: it never ran
		s.Drop(id)
		return
	}
	sc := s.calls[id]
	if sc == nil {
		return
	}
	r := result{out: output, isErr: isError}
	sc.res = &r
	if sc.after != nil {
		sc.after(r)
		delete(s.calls, id)
	}
}

// Hold keeps the scene of a tool call from playing while the user is asked to permit the call,
// so the IDE does not show an edit that may never happen. The call may not have come yet: a
// workflow agent's calls arrive late, from its transcript.
func (s *Sim) Hold(id string) {
	if id != "" {
		s.held[id] = true
	}
}

// Release lets a held scene play: the user permitted the call.
func (s *Sim) Release(id string) { delete(s.held, id) }

// Drop forgets the scene of a tool call the user denied; a scene already playing stops.
func (s *Sim) Drop(id string) {
	if id == "" {
		return
	}
	delete(s.held, id)
	sc := s.calls[id]
	if sc == nil {
		s.denied[id] = true
		return
	}
	delete(s.calls, id)
	if l := sc.l; l.cur == sc {
		sc.steps, sc.after = nil, nil
		s.undo(sc)
		hangUp(&l.a.Term)
	} else {
		l.queue = slices.DeleteFunc(l.queue, func(q *scene) bool { return q == sc })
	}
}

// hangUp ends the command a dropped scene was typing or running: what was typed of its last line
// goes, the spinner stops, and the next command starts on a prompt of its own.
func hangUp(t *ide.Term) {
	n := len(t.Lines)
	if t.Running || n > 0 && (t.Lines[n-1].Kind == ide.Command || t.Lines[n-1].Kind == ide.More) {
		t.Print(ide.Gap, "")
	}
	t.Input, t.Running = "", false
}

// undo puts back the file a dropped scene was typing into, as it is on disk; a new file goes.
func (s *Sim) undo(sc *scene) {
	if sc.buf == nil || !sc.buf.Modified {
		return
	}
	for _, t := range s.st.Tabs {
		if &t.Buf != sc.buf {
			continue
		}
		text, ok := snapshot(t.Path)
		if !ok {
			s.st.Forget(t.Path)
			return
		}
		t.Buf.SetText(text)
		t.Buf.Modified = false
		return
	}
}

// Tick advances every lane and the camera to now and reports whether anything visible changed.
func (s *Sim) Tick(now time.Time) bool {
	s.now = now
	changed := false
	for _, a := range s.st.Agents {
		if s.advance(s.lanes[a], now) {
			changed = true
		}
	}
	return s.direct(now) || changed
}

// advance plays the steps of a lane that are due by now.
func (s *Sim) advance(l *lane, now time.Time) bool {
	if l.clock.IsZero() {
		l.clock = now
	}
	changed := false
	for {
		if l.cur == nil {
			if len(l.queue) == 0 || s.held[l.queue[0].id] {
				break
			}
			l.cur, l.queue = l.queue[0], l.queue[1:]
			s.st.Viewed = ""              // the agents' show goes on
			s.st.MoveOn(l.a.Key, l.clock) // what the last call typed stays lit until now
			l.cur.build(l.cur)
			l.a.Activity = l.cur.what
			changed = true
		}
		if len(l.cur.steps) == 0 {
			if l.cur.after == nil {
				delete(s.calls, l.cur.id)
			}
			l.cur = nil
			if len(l.queue) == 0 {
				l.a.Activity = ""
			}
			continue
		}
		st := l.cur.steps[0]
		if st.wait && l.cur.res == nil || s.held[l.cur.id] {
			break
		}
		due := l.clock.Add(l.scale(st.delay))
		if due.After(now) {
			break
		}
		l.clock = due
		l.cur.steps = l.cur.steps[1:]
		st.run()
		l.a.Active = l.clock
		changed = true
	}
	switch {
	case l.cur == nil:
		l.clock = time.Time{} // idle: the next scene starts at the Tick that finds it
	case l.cur.steps[0].wait && l.cur.res == nil || s.held[l.cur.id]:
		l.clock = now // blocked: the next step counts its delay from here, not from the past
	}
	if t := &l.a.Term; t.Running {
		if f := int(now.Sub(l.runAt) / spinFrame); f != t.Spinner {
			t.Spinner = f
			changed = true
		}
	}
	return changed
}

// Busy reports whether there is animation left to play.
func (s *Sim) Busy() bool {
	for _, l := range s.lanes {
		if l.cur != nil || len(l.queue) > 0 {
			return true
		}
	}
	return false
}

// scale shortens a delay while scenes wait, so the animation catches up with the agent.
func (l *lane) scale(d time.Duration) time.Duration {
	return time.Duration(float64(d) / (1 + 0.5*float64(len(l.queue))))
}

func (sc *scene) add(d time.Duration, run func()) {
	sc.steps = append(sc.steps, step{delay: d, score: sc.score, run: run})
}

// await adds a step that runs once the tool result is in; run may add the steps that follow.
func (sc *scene) await(run func(result)) {
	sc.steps = append(sc.steps, step{wait: true, score: sc.score, run: func() { run(*sc.res) }})
}

// paced adds steps that cover n units. Each step waits lo..hi and handles units [i, j); long runs
// take several units per step so that the whole run stays within about limit.
func (sc *scene) paced(n int, lo, hi, limit time.Duration, do func(i, j int)) {
	k := max(1, int((time.Duration(n)*hi+limit-1)/limit))
	for i := 0; i < n; i += k {
		j := min(i+k, n)
		sc.add(sc.jitter(lo, hi, i), func() { do(i, j) })
	}
}

// jitter is a deterministic delay in [lo, hi] for unit i of the scene.
func (sc *scene) jitter(lo, hi time.Duration, i int) time.Duration {
	h := uint64(sc.seq*7919+i) * 0x9E3779B97F4A7C15
	h ^= h >> 29
	return lo + time.Duration(h%uint64(hi-lo+1))
}
