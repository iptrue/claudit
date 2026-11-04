// Package ide is the data model of the simulated IDE: explorer, editor tabs, terminals, and the
// agents working in them. The sim package drives it, the ui package draws it. Nothing here
// touches the screen.
package ide

import "time"

// Pane is the part of the IDE an agent is working in.
type Pane int

const (
	Editor Pane = iota
	Explorer
	Terminal
	Browser
)

// Pos is a position in a buffer. Col counts runes.
type Pos struct{ Line, Col int }

// State is everything the IDE shows. Several agents share the explorer and the open files; each
// has its own caret, terminal and current file. The view follows one of them: the camera.
type State struct {
	Root    string    // directory the explorer shows
	Tree    []TreeRow // visible explorer rows, in display order
	Tabs    []*Tab
	Agents  []*Agent          // in order of appearance; Agents[0] is the main agent, key "main"
	Camera  string            // key of the agent the view follows
	Pinned  bool              // the user pinned the camera; otherwise it follows the action
	Viewed  string            // a file the user opened from the explorer: it fills the right column for a while
	Touched map[string]string // file path → key of the agent that last worked on it
	Edited  map[string]bool   // files an agent changed: an Edit or a Write that succeeded
}

// AgentKind tells what an agent is.
type AgentKind int

const (
	Main          AgentKind = iota
	Subagent                // started by the Agent tool; key = that tool call's id
	Workflow                // a Workflow run: a node in the agents tree with no workplace; key = the Workflow tool call's id
	WorkflowAgent           // an agent of a workflow; key = its agent id (or "<workflow key>#<index>" while queued)
)

type AgentStatus int

const (
	Running AgentStatus = iota
	Queued
	Done
	Failed
)

// Agent is one agent at work.
type Agent struct {
	Key       string
	Kind      AgentKind
	Name      string // "main", the Agent description, the workflow name, the workflow agent label
	Parent    string // key of the agent or workflow it belongs to; "" for main
	Phase     string // workflow phase title (WorkflowAgent)
	Status    AgentStatus
	Activity  string // what it is doing now, e.g. "Edit calc.py"; "" when idle
	Model     string // the model it runs, e.g. "claude-haiku-5-5"; "" until known
	Tokens    int
	ToolCalls int
	Started   time.Time
	Ended     time.Time // zero while running
	Active    time.Time // when it last did something on screen
	TypedAt   time.Time // when it last typed text, or selected or deleted some: its caret is a block a while
	Hotkey    int       // 1–9 pins the camera on it; 0 for none (workflow nodes, the tenth agent on)
	Color     int       // palette slot, stable for its lifetime

	// Its workplace (unused for Workflow nodes).
	Term    Term
	Page    Page   // what its browser shows
	File    string // path of the open file it works on; "" for none
	TreeSel string // path of the explorer row it points at; "" for none
	Focus   Pane
}

// TreeRow is one visible line of the explorer.
type TreeRow struct {
	Depth    int
	Name     string
	Path     string // absolute
	Dir      bool
	Expanded bool
	Ignored  bool // git ignores it
}

// Tab is an open file.
type Tab struct {
	Path  string // absolute
	Title string
	Buf   Buffer
	Hex   bool // Buf is a hex dump of a binary file the user viewed: no language to colour
}

// Buffer is the text of an open file, as shown: lines never contain "\t" (tabs become 4 spaces) or "\r".
type Buffer struct {
	Lines    []string
	Carets   map[string]*Caret // by agent key: every agent working in this file
	Modified bool              // unsaved: an edit is typed but its tool has not finished yet
	Glow     []Glow            // lines agents typed: lit until their agent moves on, then fading out
}

// Glow marks a line an agent typed: the view flashes it green, keeps it lit while the agent is
// still on that tool call, and lets it fade once the agent moves on.
type Glow struct {
	Line  int
	Agent string
	At    time.Time // when typed: the flash fades from here
	Off   time.Time // when the agent moved on to its next tool call: the light fades from here; zero while it holds
}

// Caret is one agent's cursor and selection in a buffer.
type Caret struct {
	Pos Pos
	Sel *Selection // highlighted range, nil for none
}

// Selection is a half-open range [From, To).
type Selection struct{ From, To Pos }

// Term is a simulated terminal.
type Term struct {
	Lines     []TermLine
	Input     string    // command typed so far on the live prompt line
	InputLang string    // the language of what Input types, as TermLine.Lang: "" for the shell's
	Running   bool      // a command is executing: the live prompt line is replaced by a spinner
	Spinner   int       // spinner frame
	RunAt     time.Time // when the running command started
	bytes     int       // bytes of text kept in Lines
	kept      int       // line count used by the byte accounting
}

// TermLine is one line of terminal scrollback.
type TermLine struct {
	Kind   LineKind
	Text   string // for Command: the command alone, without the prompt
	Lang   string // for Code, Match, Added and Removed: the file whose language the code is in; for More, that of a here-document's body
	Prefix int    // for Match: the runes of where the hit is ("path:12:") before its code
	Call   string // for Command, More and Comment: the id of the tool call that typed it
}

type LineKind int

const (
	Command LineKind = iota
	Output
	ErrorOutput
	Gap     // the blank line between a command's output and the next prompt
	More    // a command's line after its first: no prompt of its own
	Code    // output that is the text of a file, or the lines around a diff's change
	Match   // a search's hit: where it is, then its line of code
	Added   // a diff's added line
	Removed // a diff's removed line
	Hunk    // a diff's "@@ -1,2 +1,3 @@"
	Meta    // a diff's or a log's header: "diff --git …", "index …", "commit …"
	Status  // a line of git status --short: "XY path"
	Stat    // a line of a diffstat: " path | 7 ++++++-"
	Comment // what the command below does, in claude's words: "# …"
)

// Page is what an agent's browser shows: the address it fetched, or the query it searched for,
// and as much of the page as has loaded.
type Page struct {
	URL    string     // the address bar as typed so far
	Search bool       // URL is a query
	Lines  []PageLine // what has loaded
	LoadAt time.Time  // when the page began to load; zero once it has
	Failed bool       // the tool failed: Lines say why
}

// PageLine is a line of a page: text, or a link to URL titled Text.
type PageLine struct {
	Text string
	URL  string
}
