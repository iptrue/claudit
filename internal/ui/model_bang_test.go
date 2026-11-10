//go:build !windows

package ui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/protocol"
)

// modelBang types "!command", runs what that starts and plays its end.
func modelBang(t *testing.T, m *Model, typed string) {
	t.Helper()
	cmd := modelLocalSay(m, typed)
	if cmd == nil {
		t.Fatalf("%q started nothing; chat %+v", typed, m.Chat)
	}
	m.Update(cmd())
}

// modelBangSent is the text of the only message sent.
func modelBangSent(t *testing.T, sent *[]protocol.Outbound) string {
	t.Helper()
	if len(*sent) != 1 {
		t.Fatalf("sent %#v", *sent)
	}
	u, ok := (*sent)[0].(protocol.UserMessage)
	if !ok {
		t.Fatalf("sent %#v", (*sent)[0])
	}
	return u.Text
}

// modelBangEntry is the last ! command in the chat.
func modelBangEntry(t *testing.T, m *Model) ChatEntry {
	t.Helper()
	for i := len(m.Chat) - 1; i >= 0; i-- {
		if m.Chat[i].Bang != "" {
			return m.Chat[i]
		}
	}
	t.Fatalf("no ! command in %+v", m.Chat)
	return ChatEntry{}
}

// The command shows as typed after "!", what it printed under it; "running…" until it ends.
func TestDrawBang(t *testing.T) {
	strip := func(lines []string) string { return ansi.Strip(strings.Join(lines, "\n")) }
	got := strip(drawEntry(ChatEntry{Kind: CommandEntry, Bang: "git status", Text: "On branch main\n\nclean"}, 60))
	if got != "! git status\n  ⎿ On branch main\n    clean" {
		t.Errorf("done:\n%s", got)
	}
	if got := strip(drawEntry(ChatEntry{Kind: CommandEntry, Bang: "sleep 9"}, 60)); got != "! sleep 9\n  ⎿ running…" {
		t.Errorf("running:\n%s", got)
	}
	if _, ok := chatGoal(ChatEntry{Kind: CommandEntry, Bang: "cat notes", Text: "Goal set: not one"}); ok {
		t.Error("the output of a ! command taken for a goal")
	}
	md := drawExport([]ChatEntry{{Kind: CommandEntry, Bang: "ls", Text: "a.go"}}, "p", "now")
	if !strings.Contains(md, "```\n! ls\na.go\n```") {
		t.Errorf("export:\n%s", md)
	}
}

// While claude's reply streams, a ! command goes above it, for the reply to stay in one piece.
func TestModelBangStreaming(t *testing.T) {
	m, _ := modelLocalNew(t)
	modelLocalSay(m, "work")
	m.Chat = append(m.Chat, ChatEntry{Kind: AssistantEntry, Text: "half a repl"})
	modelBang(t, m, "!echo hi")
	if n := len(m.Chat); m.Chat[n-1].Kind != AssistantEntry || m.Chat[n-2].Bang != "echo hi" || m.Chat[n-2].Text != "hi" {
		t.Fatalf("chat %+v", m.Chat)
	}
}

func TestBangCommand(t *testing.T) {
	for _, c := range []struct {
		text, command string
		ok            bool
	}{
		{"!git status", "git status", true},
		{"  ! ls -la  ", "ls -la", true},
		{"!printf '%s' \"a  b\"", "printf '%s' \"a  b\"", true},
		{"!", "", false},
		{"!   ", "", false},
		{"fix it!", "", false},
		{"/context", "", false},
	} {
		command, ok := bangCommand(c.text)
		if command != c.command || ok != c.ok {
			t.Errorf("%q: %q %v, want %q %v", c.text, command, ok, c.command, c.ok)
		}
	}
}

// A ! command runs in the project without a turn; claude sees it and its output with the next prompt.
func TestModelBang(t *testing.T) {
	m, sent := modelLocalNew(t)
	modelBang(t, m, `!printf '%s|' "a  b"; echo; pwd`)
	if len(*sent) != 0 || m.Status.Busy {
		t.Fatalf("a ! command went to claude: %#v", *sent)
	}
	root, _ := filepath.EvalSymlinks(m.Sim.State().Root)
	if e := modelBangEntry(t, m); e.Bang != `printf '%s|' "a  b"; echo; pwd` || !strings.HasPrefix(e.Text, "a  b|") {
		t.Fatalf("chat %+v", m.Chat)
	}
	if !slicesAnyContains(m.Chat, root) && !slicesAnyContains(m.Chat, m.Sim.State().Root) {
		t.Errorf("not run in the project %q: %+v", root, m.Chat)
	}
	if got := m.history[len(m.history)-1]; got != `!printf '%s|' "a  b"; echo; pwd` {
		t.Errorf("history %q", got)
	}

	modelLocalSay(m, "what changed")
	text := modelBangSent(t, sent)
	want := "<bash-input>printf '%s|' \"a  b\"; echo; pwd</bash-input>\n<bash-stdout>a  b|\n"
	if !strings.HasPrefix(text, want) || !strings.HasSuffix(text, "</bash-stdout><bash-stderr></bash-stderr>\n\nWhat changed") {
		t.Fatalf("sent %q", text)
	}

	*sent = nil
	modelEvents(m, protocol.Result{})
	modelLocalSay(m, "again")
	if text := modelBangSent(t, sent); text != "Again" {
		t.Errorf("the output went twice: %q", text)
	}
}

// The exit code and stderr go to the chat and to claude.
func TestModelBangFails(t *testing.T) {
	m, sent := modelLocalNew(t)
	modelBang(t, m, "!echo out; echo oops >&2; exit 3")
	if !modelResumeSaid(m, "out\noops\n[exit code 3]") {
		t.Fatalf("chat %+v", m.Chat)
	}
	modelLocalSay(m, "why")
	if text := modelBangSent(t, sent); !strings.Contains(text, "<bash-stdout>out</bash-stdout><bash-stderr>oops\n[exit code 3]</bash-stderr>") {
		t.Fatalf("sent %q", text)
	}
}

// A slash command goes alone: the output waits for the next prompt.
func TestModelBangSlash(t *testing.T) {
	m, sent := modelLocalNew(t)
	modelBang(t, m, "!echo hi")
	modelLocalSay(m, "/context")
	if text := modelBangSent(t, sent); text != "/context" {
		t.Fatalf("sent %q", text)
	}
	*sent = nil
	modelEvents(m, protocol.Result{})
	modelLocalSay(m, "go")
	if text := modelBangSent(t, sent); !strings.HasPrefix(text, "<bash-input>echo hi</bash-input>") {
		t.Fatalf("sent %q", text)
	}
}

// A prompt queued while claude works takes the output when it goes.
func TestModelBangQueued(t *testing.T) {
	m, sent := modelLocalNew(t)
	modelLocalSay(m, "work")
	*sent = nil
	modelBang(t, m, "!echo hi")
	modelLocalSay(m, "then this")
	if len(*sent) != 0 {
		t.Fatalf("sent while busy: %#v", *sent)
	}
	modelEvents(m, protocol.Result{})
	if text := modelBangSent(t, sent); !strings.HasPrefix(text, "<bash-input>echo hi</bash-input>") || !strings.HasSuffix(text, "Then this") {
		t.Fatalf("sent %q", text)
	}
}

// Esc and ctrl+c stop a ! command before they interrupt claude.
func TestModelBangStop(t *testing.T) {
	for _, key := range []string{"esc", "ctrl+c"} {
		m, sent := modelLocalNew(t)
		cmd := modelLocalSay(m, "!sleep 30")
		done := make(chan tea.Msg, 1)
		start := time.Now()
		go func() { done <- cmd() }()
		time.Sleep(100 * time.Millisecond)
		m.Update(inputKey(key))
		var msg tea.Msg
		select {
		case msg = <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: not stopped", key)
		}
		if time.Since(start) > 5*time.Second {
			t.Fatalf("%s: took %s", key, time.Since(start))
		}
		m.Update(msg)
		if m.bangRun != nil || !modelResumeSaid(m, "[stopped]") || len(*sent) != 0 || m.quitKey != "" {
			t.Fatalf("%s: run %+v chat %+v sent %#v quit %q", key, m.bangRun, m.Chat, *sent, m.quitKey)
		}
	}
}

// One ! command at a time; none in a replay.
func TestModelBangRefused(t *testing.T) {
	m, _ := modelLocalNew(t)
	cmd := modelLocalSay(m, "!sleep 30")
	if modelLocalSay(m, "!echo two") != nil || !modelResumeSaid(m, "! sleep 30 is still running: esc stops it") {
		t.Fatalf("a second one ran: %+v", m.Chat)
	}
	m.Update(inputKey("esc"))
	m.Update(cmd())

	m, sent := modelNew(t, true)
	if modelLocalSay(m, "!echo hi") != nil || !modelResumeSaid(m, "replay: commands are not run") || len(*sent) != 0 {
		t.Fatalf("replay: %+v", m.Chat)
	}
}

// What ended after its command was replaced, or stopped by a stale id, changes nothing.
func TestModelBangStale(t *testing.T) {
	m, _ := modelLocalNew(t)
	n := len(m.Chat)
	m.Update(bangDoneMsg{id: 7, stdout: "late"})
	if len(m.Chat) != n || len(m.bangSaid) != 0 {
		t.Fatalf("chat %+v said %q", m.Chat, m.bangSaid)
	}
}

// A command past its time is stopped and says so; one that cannot start says why.
func TestRunBangErrors(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer stop()
	if done := runBang(ctx, 1, "sleep 30", t.TempDir()); done.err == nil || !strings.HasPrefix(done.err.Error(), "stopped after ") {
		t.Errorf("timeout: %+v", done)
	}
	done := runBang(context.Background(), 2, "true", filepath.Join(t.TempDir(), "gone"))
	if done.err == nil || done.code != -1 {
		t.Fatalf("no dir: %+v", done)
	}
	m, _ := modelLocalNew(t)
	m.bangRun = &bangRun{id: 2, command: "true", stop: func() {}}
	m.Chat = append(m.Chat, ChatEntry{Kind: CommandEntry, Bang: "true"})
	m.Update(done)
	if !modelResumeSaid(m, "no such file or directory]") || !strings.Contains(m.bangSaid[0], "no such file or directory]</bash-stderr>") {
		t.Fatalf("chat %+v said %q", m.Chat, m.bangSaid)
	}
}

func TestModelBangNoOutput(t *testing.T) {
	m, _ := modelLocalNew(t)
	modelBang(t, m, "!true")
	if !modelResumeSaid(m, "(no output)") {
		t.Fatalf("chat %+v", m.Chat)
	}
}

func TestBangClip(t *testing.T) {
	if s := bangClip("short"); s != "short" {
		t.Errorf("short: %q", s)
	}
	long := strings.Repeat("a", bangLimit-1) + "яz"
	s := bangClip(long)
	if !strings.HasSuffix(s, "\n… 3 more bytes") || strings.Contains(s, "\xd1") {
		t.Errorf("clip: %q", s[len(s)-30:])
	}
}

// The debug dump claudit asks of claude does not reach a ! command.
func TestModelBangEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_LOG", "debug")
	m, _ := modelLocalNew(t)
	modelBang(t, m, "!echo log=${ANTHROPIC_LOG:-none}")
	if !modelResumeSaid(m, "log=none") {
		t.Fatalf("chat %+v", m.Chat)
	}
}

func TestBangEnv(t *testing.T) {
	env := bangEnv([]string{"A=1", "ANTHROPIC_LOG=debug", "ANTHROPIC_LOGX=2", "B=3"})
	if strings.Join(env, " ") != "A=1 ANTHROPIC_LOGX=2 B=3" {
		t.Errorf("env %q", env)
	}
}

func TestBangShell(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SHELL", "")
	t.Setenv("SHELL", "/bin/zsh-not-here")
	if sh, flag := bangShell(); flag != "-c" || sh == "/bin/zsh-not-here" {
		t.Errorf("missing shell taken: %q %q", sh, flag)
	}
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("PATH", "")
	if sh, _ := bangShell(); sh != "/bin/sh" {
		t.Errorf("fallback %q", sh)
	}
}

func slicesAnyContains(chat []ChatEntry, s string) bool {
	for _, e := range chat {
		if strings.Contains(e.Text, s) {
			return true
		}
	}
	return false
}

// A "!" first puts the prompt in the shell mode: the "!" stands in for the "›", once.
func TestRenderInputBang(t *testing.T) {
	for _, c := range []struct {
		text   string
		cursor int
		want   string
		x      int
	}{
		{"!git status", 11, "! git status", 12},
		{"!git status", 1, "! git status", 2},
		{"!git status", 0, "! git status", 2},
		{"!", 1, "! a shell command, run in the project", 2},
		{"git!", 4, "› git!", 6},
	} {
		out, x := renderInput(&Input{Text: []rune(c.text), Cursor: c.cursor}, 60, "Ask claude…", false)
		if got := strings.TrimRight(ansi.Strip(out), " "); got != c.want || x != c.x {
			t.Errorf("%q at %d: %q x=%d, want %q x=%d", c.text, c.cursor, got, x, c.want, c.x)
		}
	}
	if on, off := renderInput(&Input{Text: []rune("!ls"), Cursor: 3}, 60, "", false); on == "" || off != 4 {
		t.Errorf("focused: %q %d", on, off)
	}
	if dim, _ := renderInput(&Input{Text: []rune("!ls"), Cursor: 3}, 60, "", true); !strings.HasPrefix(ansi.Strip(dim), "! ls") {
		t.Errorf("out of focus: %q", ansi.Strip(dim))
	}
}

// The shell mode says so where the key hints go; backspace over its "!" ends it.
func TestModelBangMode(t *testing.T) {
	m, _ := modelMouseNew(t)
	modelPress(m, inputTyped("!ls")...)
	if out := ansi.Strip(m.View().Content); !strings.Contains(out, "! for shell mode") || !strings.Contains(out, "! ls") {
		t.Fatalf("shell mode not shown:\n%s", out)
	}
	modelPress(m, inputKey("home"), inputKey("delete"))
	if out := ansi.Strip(m.View().Content); strings.Contains(out, "! for shell mode") || !strings.Contains(out, "› ls") {
		t.Fatalf("still in the shell mode:\n%s", out)
	}
}
