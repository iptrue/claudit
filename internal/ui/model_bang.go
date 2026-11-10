package ui

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// A prompt that starts with "!" is a shell command, as in Claude Code's bash mode: claudit runs it
// itself in the project, since claude -p would take it for words to the model. The chat shows
// what it printed; claude sees the command and its output with the next prompt, in the tags
// Claude Code uses, and no turn starts for it alone.

// bangTimeout is how long a ! command may run before it is stopped.
const bangTimeout = 10 * time.Minute

// bangLimit is how many bytes of each of stdout and stderr go to the chat and to claude.
const bangLimit = 30000

// bangRun is the ! command running.
type bangRun struct {
	id      int
	command string
	stop    context.CancelFunc
	stopped bool // esc or ctrl+c stopped it
}

// bangDoneMsg is what a ! command left once it ended.
type bangDoneMsg struct {
	id             int
	stdout, stderr string
	code           int   // exit code; -1 when it did not exit by itself
	err            error // it could not start, or it ran out of time
}

// bangInput reports whether the prompt is in the shell mode: a "!" first.
func bangInput(text []rune) bool { return len(text) > 0 && text[0] == '!' }

// bangCommand reports the command of a prompt typed as "!command".
func bangCommand(text string) (string, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(text), "!")
	if rest = strings.TrimSpace(rest); !ok || rest == "" {
		return "", false
	}
	return rest, true
}

// bang runs a ! command off the UI's way: one at a time, not in a replay.
func (m *Model) bang(command string) tea.Cmd {
	switch {
	case m.Status.Replay:
		m.local("replay: commands are not run")
		return nil
	case m.bangRun != nil:
		m.local("! " + m.bangRun.command + " is still running: esc stops it")
		return nil
	}
	m.bangs++
	ctx, stop := context.WithTimeout(context.Background(), bangTimeout)
	m.bangRun = &bangRun{id: m.bangs, command: command, stop: stop}
	at := len(m.Chat)
	if m.Status.Busy && at > 0 && m.Chat[at-1].Kind == AssistantEntry {
		at-- // keep the streaming reply last, as local output does
	}
	m.Chat = slices.Insert(m.Chat, at, ChatEntry{Kind: CommandEntry, Bang: command, At: m.clock()})
	id, dir := m.bangs, m.Sim.State().Root
	return func() tea.Msg {
		defer stop()
		return runBang(ctx, id, command, dir)
	}
}

// runBang runs command in dir through the user's shell, without a terminal or input.
func runBang(ctx context.Context, id int, command, dir string) bangDoneMsg {
	shell, flag := bangShell()
	cmd := exec.CommandContext(ctx, shell, flag, command)
	cmd.Dir = dir
	cmd.Env = bangEnv(os.Environ())
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	bangProcess(cmd)
	err := cmd.Run()
	done := bangDoneMsg{id: id, stdout: stdout.String(), stderr: stderr.String(), code: -1}
	if cmd.ProcessState != nil {
		done.code = cmd.ProcessState.ExitCode()
	}
	var exit *exec.ExitError
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		done.err = errors.New("stopped after " + bangTimeout.String())
	case err != nil && !errors.As(err, &exit) && ctx.Err() == nil:
		done.err = err
	}
	return done
}

// bangEnv is the environment of a ! command: claudit's own, without the debug dump of the API
// client, which would land in the output.
func bangEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, "ANTHROPIC_LOG=") {
			out = append(out, kv)
		}
	}
	return out
}

// bangDone puts what a ! command printed in the chat and keeps it for claude's next prompt.
func (m *Model) bangDone(msg bangDoneMsg) {
	r := m.bangRun
	if r == nil || r.id != msg.id {
		return
	}
	m.bangRun = nil
	stdout, stderr := bangClip(ansi.Strip(msg.stdout)), bangClip(ansi.Strip(msg.stderr))
	var note string
	switch {
	case msg.err != nil:
		note = msg.err.Error()
	case r.stopped:
		note = "stopped"
	case msg.code > 0:
		note = "exit code " + strconv.Itoa(msg.code)
	}
	shown := strings.TrimRight(stdout, "\n")
	if e := strings.TrimRight(stderr, "\n"); e != "" {
		shown = strings.TrimLeft(shown+"\n"+e, "\n")
	}
	if note != "" {
		shown = strings.TrimLeft(shown+"\n["+note+"]", "\n")
	}
	for i := len(m.Chat) - 1; i >= 0; i-- {
		if e := &m.Chat[i]; e.Kind == CommandEntry && e.Bang == r.command && e.Text == "" {
			e.Text = cmp.Or(shown, "(no output)")
			break
		}
	}
	if note != "" {
		stderr = strings.TrimLeft(strings.TrimRight(stderr, "\n")+"\n["+note+"]", "\n")
	}
	m.bangSaid = append(m.bangSaid, "<bash-input>"+r.command+"</bash-input>\n<bash-stdout>"+
		strings.TrimRight(stdout, "\n")+"</bash-stdout><bash-stderr>"+strings.TrimRight(stderr, "\n")+"</bash-stderr>")
}

// bangStop stops the ! command running, if any, and reports whether there was one.
func (m *Model) bangStop() bool {
	if m.bangRun == nil {
		return false
	}
	m.bangRun.stopped = true
	m.bangRun.stop()
	return true
}

// withBangs is text with the ! commands run since the last prompt in front, for claude to see
// them as Claude Code shows them. A slash command goes as it is: claude reads only a whole
// message as one; the commands wait for the next prompt.
func (m *Model) withBangs(text string) string {
	if len(m.bangSaid) == 0 {
		return text
	}
	if _, ok := slash(text); ok {
		return text
	}
	said := strings.Join(m.bangSaid, "\n")
	m.bangSaid = nil
	return said + "\n\n" + text
}

// bangClip cuts s to bangLimit bytes on a rune boundary, saying how much is left out.
func bangClip(s string) string {
	if len(s) <= bangLimit {
		return s
	}
	cut := bangLimit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n… " + strconv.Itoa(len(s)-cut) + " more bytes"
}
