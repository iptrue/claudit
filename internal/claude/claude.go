// Package claude runs the agent: a live `claude -p` process, or a replay of a recorded stream.
package claude

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/pixelbender/claudit/internal/protocol"
)

// Session is a running agent. Events is closed after the final protocol.Exited.
type Session struct {
	Events <-chan protocol.Event
	send   chan<- protocol.Outbound
}

// Send hands a message to the agent. It does not block the caller.
func (s *Session) Send(o protocol.Outbound) {
	select {
	case s.send <- o:
	default: // the buffer is full only when nobody drains it any more: the agent is gone
	}
}

var flags = []string{
	"-p",
	"--input-format", "stream-json",
	"--output-format", "stream-json",
	"--verbose",
	"--include-partial-messages",
	"--permission-prompt-tool", "stdio",
}

// Spawn starts `claude -p` in stream-json mode in the current directory, with args appended.
// A protocol.Resume restarts it in that conversation. Cancelling ctx kills the process.
func Spawn(ctx context.Context, args []string) (*Session, error) {
	events := make(chan protocol.Event, 64)
	send := make(chan protocol.Outbound, 64)
	env, done, err := claudeEnv()
	if err != nil {
		return nil, err
	}
	p, err := start(ctx, args, env, events)
	if err != nil {
		done()
		return nil, err
	}

	go func() {
		defer close(events)
		defer done() // before events close: claude is gone for good
		for {
			select {
			case o := <-send:
				r, ok := o.(protocol.Resume)
				if !ok {
					p.write(o)
					continue
				}
				p.stop() // its events are all out: none follows those of the new one
				next, err := start(ctx, withResume(args, r.SessionID), env, events)
				if err != nil {
					emit(ctx, events, protocol.Exited{Err: err})
					return
				}
				p = next
			case <-p.done:
				emit(ctx, events, protocol.Exited{Err: p.err})
				return
			}
		}
	}()

	return &Session{Events: events, send: send}, nil
}

// withResume is args with the flags that pick a conversation replaced by --resume id.
func withResume(args []string, id string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-c" || a == "--continue" || a == "--fork-session":
		case a == "-r" || a == "--resume" || a == "--session-id":
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++ // its value
			}
		case strings.HasPrefix(a, "--resume=") || strings.HasPrefix(a, "--session-id="):
		default:
			out = append(out, a)
		}
	}
	return append(out, "--resume", id)
}

// proc is one claude process of a session.
type proc struct {
	stdin          io.Writer
	stdout, stderr io.Closer
	cancel         context.CancelFunc // kills it
	werr           error              // the first failed write; later messages are dropped
	done           chan struct{}      // closed once its output is read and it is reaped
	err            error              // how it exited, once done
}

// start runs claude with args in env (claudeEnv). Its events go to events until it ends or stop
// is called.
func start(ctx context.Context, args, env []string, events chan<- protocol.Event) (*proc, error) {
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, "claude", append(slices.Clone(flags), args...)...)
	cmd.Env = env
	// A session of its own: neither claude nor the commands it runs (sudo, ssh, git asking for a
	// password) has a terminal to draw over the TUI or to steal keys from.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// Stopped, it hears what a closed terminal would tell it: a hangup to its group, so that it
	// shuts down cleanly and what it runs goes too. What ignores the hangup is killed a second later.
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP) }
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	p := &proc{stdin: stdin, stdout: stdout, stderr: stderr, cancel: cancel, done: make(chan struct{})}
	p.write(protocol.Initialize{RequestID: "init"})      // the slash commands, before any turn
	p.write(protocol.GetSettings{RequestID: "settings"}) // the effort: claude -p's Init leaves it out

	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		readLines(stderr, func(line []byte) bool {
			emit(ctx, events, protocol.Stderr{Line: string(line)})
			return true
		})
	}()

	go func() {
		limits := debugLimits{}
		readLines(stdout, func(line []byte) bool {
			if l, ok := limits.line(line); ok {
				emit(ctx, events, l)
			}
			for _, ev := range protocol.Parse(line) {
				emit(ctx, events, ev)
			}
			return true
		})
		<-stderrDone // Wait closes the pipes: finish reading them first
		p.err = cmd.Wait()
		close(p.done)
	}()

	return p, nil
}

// write sends o to claude's stdin. After a write error it drops o, so senders never pile up.
func (p *proc) write(o protocol.Outbound) {
	if b, err := protocol.Encode(o); err == nil && p.werr == nil {
		_, p.werr = p.stdin.Write(append(b, '\n'))
	}
}

// stop kills the process and returns once nothing more of it can reach the events: the cancelled
// context drops what it still says, and closing the pipes ends the reads even when a grandchild
// holds them open.
func (p *proc) stop() {
	p.cancel()
	p.stdout.Close()
	p.stderr.Close()
	<-p.done
}

// Replay pacing; package variables so tests can shrink them.
var (
	streamPace = 10 * time.Millisecond  // before a stream_event line
	linePace   = 250 * time.Millisecond // before any other line
)

// Replay plays back a recorded stdout stream (one JSON object per line) at a watchable pace.
// It pauses at every permission request until the host answers it. Cancelling ctx stops it.
func Replay(ctx context.Context, path string) (*Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	events := make(chan protocol.Event, 64)
	send := make(chan protocol.Outbound, 64)

	go func() {
		defer close(events)
		defer f.Close()
		readLines(f, func(line []byte) bool {
			evs := protocol.Parse(line)
			if len(evs) == 0 {
				return true // lines without events take no time
			}
			if !sleep(ctx, send, pace(evs[0])) {
				return false
			}
			for _, ev := range evs {
				if !emit(ctx, events, ev) {
					return false
				}
				if _, ok := ev.(protocol.PermissionRequest); ok && !awaitAnswer(ctx, send) {
					return false
				}
			}
			return true
		})
		<-ctx.Done() // at EOF stay open, so the UI stays up
	}()

	return &Session{Events: events, send: send}, nil
}

// pace is the delay before the line that produced ev.
func pace(ev protocol.Event) time.Duration {
	switch ev.(type) {
	case protocol.Init:
		return 0
	case protocol.Thinking, protocol.TextStart, protocol.TextDelta:
		return streamPace
	}
	return linePace
}

// sleep waits d, discarding whatever is sent meanwhile. It reports false when ctx is done.
func sleep(ctx context.Context, send <-chan protocol.Outbound, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			return true
		case <-send:
		case <-ctx.Done():
			return false
		}
	}
}

// awaitAnswer waits for an Allow or Deny. It reports false when ctx is done.
func awaitAnswer(ctx context.Context, send <-chan protocol.Outbound) bool {
	for {
		select {
		case o := <-send:
			switch o.(type) {
			case protocol.Allow, protocol.Deny:
				return true
			}
		case <-ctx.Done():
			return false
		}
	}
}

// emit delivers ev unless ctx is done first.
func emit(ctx context.Context, events chan<- protocol.Event, ev protocol.Event) bool {
	if ctx.Err() != nil { // select would pick at random while there is room
		return false
	}
	select {
	case events <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

// readLines calls fn with each line of r, without its line ending, until EOF, a read error,
// or fn returning false. Lines can be megabytes long, hence no bufio.Scanner.
func readLines(r io.Reader, fn func(line []byte) bool) {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 && !fn(bytes.TrimRight(line, "\r\n")) {
			return
		}
		if err != nil {
			return
		}
	}
}
