package claude

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/pixelbender/claudit/internal/protocol"
)

const demo = "../testdata/demo.jsonl"

func init() {
	streamPace, linePace = time.Millisecond, 2*time.Millisecond
}

// next returns the next event, failing the test if none arrives in time or the channel closes.
func next(t *testing.T, events <-chan protocol.Event) protocol.Event {
	t.Helper()
	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatal("events closed")
		}
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for an event")
	}
	return nil
}

// quiet fails the test if anything happens on events for a while.
func quiet(t *testing.T, events <-chan protocol.Event) {
	t.Helper()
	select {
	case ev, ok := <-events:
		t.Fatalf("expected silence, got %#v (open=%v)", ev, ok)
	case <-time.After(50 * time.Millisecond):
	}
}

// parseFile returns what Parse makes of every line of path.
func parseFile(t *testing.T, path string) []protocol.Event {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var evs []protocol.Event
	for line := range bytes.SplitSeq(data, []byte("\n")) {
		evs = append(evs, protocol.Parse(line)...)
	}
	return evs
}

// sameEvents compares event lists without dumping megabyte-long outputs.
func sameEvents(t *testing.T, got, want []protocol.Event) {
	t.Helper()
	for i := range max(len(got), len(want)) {
		if i >= len(got) || i >= len(want) {
			t.Fatalf("got %d events, want %d", len(got), len(want))
		}
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Fatalf("event %d = %.300v, want %.300v", i, fmt.Sprintf("%#v", got[i]), fmt.Sprintf("%#v", want[i]))
		}
	}
}

const fakeClaude = `#!/bin/sh
printf '%s\n' "$@" > "$FAKE_DIR/args"
cat "$FAKE_FIXTURE"
# one line far beyond bufio.Scanner's 64 KiB default
printf '{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"big","content":"'
head -c 1048576 /dev/zero | tr '\0' 'a'
printf '"}]}}\n'
echo "warning: fake" >&2
n=0
while [ $n -lt 4 ] && IFS= read -r line; do
	printf '%s\n' "$line" >> "$FAKE_DIR/stdin"
	n=$((n+1))
done
exit 3
`

func TestSpawn(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(fakeClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	fixture, err := filepath.Abs(demo)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_DIR", dir)
	t.Setenv("FAKE_FIXTURE", fixture)

	sess, err := Spawn(t.Context(), []string{"--model", "two words"})
	if err != nil {
		t.Fatal(err)
	}
	sent := []protocol.Outbound{
		protocol.UserMessage{Text: "hi\nthere"},
		protocol.Allow{RequestID: "r1", Input: map[string]any{"command": "ls"}},
		protocol.Deny{RequestID: "r2", Message: "no"},
	}
	for _, o := range sent {
		sess.Send(o)
	}

	var stdout, stderr []protocol.Event
	var exited *protocol.Exited
	timeout := time.After(10 * time.Second)
	for done := false; !done; {
		select {
		case ev, ok := <-sess.Events:
			switch ev := ev.(type) {
			case nil:
				if ok {
					t.Fatal("nil event")
				}
				done = true
			case protocol.Stderr:
				stderr = append(stderr, ev)
			case protocol.Exited:
				exited = &ev
			default:
				if exited != nil {
					t.Fatalf("%T after Exited", ev)
				}
				stdout = append(stdout, ev)
			}
		case <-timeout:
			t.Fatal("timed out: Events never closed")
		}
	}

	want := append(parseFile(t, demo), protocol.ToolResult{ID: "big", Output: strings.Repeat("a", 1<<20)})
	sameEvents(t, stdout, want)
	if !reflect.DeepEqual(stderr, []protocol.Event{protocol.Stderr{Line: "warning: fake"}}) {
		t.Errorf("stderr events = %#v", stderr)
	}
	var ee *exec.ExitError
	if exited == nil || !errors.As(exited.Err, &ee) || ee.ExitCode() != 3 {
		t.Errorf("Exited = %#v, want exit status 3", exited)
	}

	args, err := os.ReadFile(filepath.Join(dir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := strings.Join(slices.Concat(flags, []string{"--model", "two words"}), "\n") + "\n"
	if string(args) != wantArgs {
		t.Errorf("args =\n%s\nwant\n%s", args, wantArgs)
	}

	stdin, err := os.ReadFile(filepath.Join(dir, "stdin"))
	if err != nil {
		t.Fatal(err)
	}
	var wantStdin []byte
	for _, o := range append([]protocol.Outbound{protocol.Initialize{RequestID: "init"}}, sent...) {
		b, err := protocol.Encode(o)
		if err != nil {
			t.Fatal(err)
		}
		wantStdin = append(append(wantStdin, b...), '\n')
	}
	if !bytes.Equal(stdin, wantStdin) {
		t.Errorf("stdin =\n%s\nwant\n%s", stdin, wantStdin)
	}
}

// main relies on this: once ctx is cancelled, Events closes only after claude is gone.
func TestSpawnCancelKills(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\necho $$ > \"$FAKE_DIR/pid\"\necho started >&2\nexec sleep 30\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_DIR", dir)

	ctx, cancel := context.WithCancel(t.Context())
	sess, err := Spawn(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	next(t, sess.Events) // the script is running once it has printed
	data, err := os.ReadFile(filepath.Join(dir, "pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}

	cancel()
	timeout := time.After(5 * time.Second)
	for open := true; open; {
		select {
		case _, open = <-sess.Events:
		case <-timeout:
			t.Fatal("Events not closed after cancel")
		}
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("claude (pid %d) still exists after Events closed: kill(0) = %v", pid, err)
	}
}

// Each run says which it is; the first leaves a grandchild holding its output open, the second
// exits after one message.
const fakeResumable = `#!/bin/sh
n=$(($(cat "$FAKE_DIR/n" 2>/dev/null || echo 0) + 1))
echo $n > "$FAKE_DIR/n"
printf '%s\n' "$@" > "$FAKE_DIR/args$n"
printf '{"type":"system","subtype":"status","permissionMode":"run%s"}\n' $n
if [ $n = 1 ]; then
	sleep 30 &
	echo $! > "$FAKE_DIR/grandchild"
fi
i=0
while IFS= read -r line; do
	printf '%s\n' "$line" >> "$FAKE_DIR/stdin$n"
	i=$((i+1))
	[ $n = 2 ] && [ $i = 2 ] && exit 4
done
`

func TestSpawnResume(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(fakeResumable), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_DIR", dir)
	t.Cleanup(func() {
		if data, err := os.ReadFile(filepath.Join(dir, "grandchild")); err == nil {
			pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
			syscall.Kill(pid, syscall.SIGKILL)
		}
	})

	sess, err := Spawn(t.Context(), []string{"--continue", "--model", "m"})
	if err != nil {
		t.Fatal(err)
	}
	if ev := next(t, sess.Events); ev != (protocol.ModeChanged{Mode: "run1"}) {
		t.Fatalf("first event %#v", ev)
	}
	sess.Send(protocol.Resume{SessionID: "abc"})
	sess.Send(protocol.UserMessage{Text: "two"})

	// The first run ends without a word: no Exited, and its grandchild does not hold the session up.
	if ev := next(t, sess.Events); ev != (protocol.ModeChanged{Mode: "run2"}) {
		t.Fatalf("after Resume: %#v", ev)
	}
	ev := next(t, sess.Events)
	var ee *exec.ExitError
	if ex, ok := ev.(protocol.Exited); !ok || !errors.As(ex.Err, &ee) || ee.ExitCode() != 4 {
		t.Fatalf("got %#v, want the second run's exit status 4", ev)
	}
	if _, open := <-sess.Events; open {
		t.Fatal("Events open after Exited")
	}

	args, err := os.ReadFile(filepath.Join(dir, "args2"))
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Join(slices.Concat(flags, []string{"--model", "m", "--resume", "abc"}), "\n") + "\n"; string(args) != want {
		t.Errorf("second run's args =\n%s\nwant\n%s", args, want)
	}
	stdin, err := os.ReadFile(filepath.Join(dir, "stdin2"))
	if err != nil {
		t.Fatal(err)
	}
	var want []byte
	for _, o := range []protocol.Outbound{protocol.Initialize{RequestID: "init"}, protocol.UserMessage{Text: "two"}} {
		b, _ := protocol.Encode(o)
		want = append(append(want, b...), '\n')
	}
	if !bytes.Equal(stdin, want) {
		t.Errorf("second run's stdin =\n%s\nwant\n%s", stdin, want)
	}
}

func TestWithResume(t *testing.T) {
	for _, tt := range []struct{ in, want []string }{
		{nil, []string{"--resume", "x"}},
		{[]string{"--model", "m"}, []string{"--model", "m", "--resume", "x"}},
		{[]string{"-c", "--model", "m"}, []string{"--model", "m", "--resume", "x"}},
		{[]string{"--resume", "old", "--fork-session", "-p2"}, []string{"-p2", "--resume", "x"}},
		{[]string{"-r", "--verbose"}, []string{"--verbose", "--resume", "x"}},
		{[]string{"--resume=old", "--session-id", "u", "--continue"}, []string{"--resume", "x"}},
	} {
		if got := withResume(tt.in, "x"); !slices.Equal(got, tt.want) {
			t.Errorf("withResume(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// Each source of the model outranks those added before it, as in claude.
func TestModel(t *testing.T) {
	cwd, config := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	t.Setenv("ANTHROPIC_MODEL", "")
	defer func(old string) { managedSettings = old }(managedSettings)
	managedSettings = filepath.Join(t.TempDir(), "managed-settings.json")
	if got := Model(nil, cwd); got != "default" {
		t.Errorf("no settings: %q", got)
	}
	if err := os.Mkdir(filepath.Join(cwd, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ path, json, want string }{
		{filepath.Join(config, "settings.json"), `{"model":"haiku"}`, "haiku"},
		{filepath.Join(cwd, ".claude", "settings.json"), `{"permissions":{"defaultMode":"plan"}}`, "haiku"},
		{filepath.Join(cwd, ".claude", "settings.json"), `{"model":"sonnet"}`, "sonnet"},
		{filepath.Join(cwd, ".claude", "settings.local.json"), `{"model":"opus"`, "sonnet"}, // broken
		{filepath.Join(cwd, ".claude", "settings.local.json"), `{"model":"opus"}`, "opus"},
		{managedSettings, `{"model":"fable"}`, "fable"},
	} {
		if err := os.WriteFile(tt.path, []byte(tt.json), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := Model(nil, cwd); got != tt.want {
			t.Errorf("after %s %s: %q, want %q", tt.path, tt.json, got, tt.want)
		}
	}
	t.Setenv("ANTHROPIC_MODEL", "claude-opus-5")
	if got := Model([]string{"--verbose"}, cwd); got != "claude-opus-5" {
		t.Errorf("env: %q", got)
	}
	for _, args := range [][]string{{"--model", "sonnet[1m]"}, {"-c", "--model=sonnet[1m]"}} {
		if got := Model(args, cwd); got != "sonnet[1m]" {
			t.Errorf("Model(%q) = %q", args, got)
		}
	}
}

// The effort claude starts with is picked as claude does: $CLAUDE_CODE_EFFORT_LEVEL, then --effort,
// then the effortLevel of the managed, local, project and user settings; "" when nothing names one.
func TestEffort(t *testing.T) {
	cwd, config := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	t.Setenv("CLAUDE_CODE_EFFORT_LEVEL", "")
	defer func(old string) { managedSettings = old }(managedSettings)
	managedSettings = filepath.Join(t.TempDir(), "managed-settings.json")
	if got := Effort(nil, cwd); got != "" {
		t.Errorf("nothing names one: %q", got)
	}
	if err := os.WriteFile(filepath.Join(config, "settings.json"), []byte(`{"effortLevel":"medium"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Effort(nil, cwd); got != "medium" {
		t.Errorf("settings: %q", got)
	}
	for _, args := range [][]string{{"--effort", "high"}, {"-c", "--effort=high"}} {
		if got := Effort(args, cwd); got != "high" {
			t.Errorf("Effort(%q) = %q", args, got)
		}
	}
	t.Setenv("CLAUDE_CODE_EFFORT_LEVEL", "low")
	if got := Effort([]string{"--effort", "high"}, cwd); got != "low" {
		t.Errorf("env: %q", got)
	}
}

// claudit starts claude in a mode only when neither the args nor the settings pick one.
func TestWithMode(t *testing.T) {
	cwd, config := t.TempDir(), t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	defer func(old string) { managedSettings = old }(managedSettings)
	managedSettings = filepath.Join(t.TempDir(), "managed-settings.json")
	auto := []string{"-c", "--permission-mode", "auto"}
	for _, tt := range []struct{ in, want []string }{
		{[]string{"-c"}, auto},
		{[]string{"--permission-mode", "plan"}, []string{"--permission-mode", "plan"}},
		{[]string{"--permission-mode=plan"}, []string{"--permission-mode=plan"}},
		{[]string{"--dangerously-skip-permissions"}, []string{"--dangerously-skip-permissions"}},
	} {
		if got := WithMode(tt.in, cwd, "auto"); !slices.Equal(got, tt.want) {
			t.Errorf("WithMode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if err := os.WriteFile(filepath.Join(config, "settings.json"), []byte(`{"model":"opus"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := WithMode([]string{"-c"}, cwd, "auto"); !slices.Equal(got, auto) {
		t.Errorf("settings without a mode: %q", got)
	}
	if err := os.WriteFile(filepath.Join(config, "settings.json"), []byte(`{"permissions":{"defaultMode":"acceptEdits"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := WithMode([]string{"-c"}, cwd, "auto"); !slices.Equal(got, []string{"-c"}) {
		t.Errorf("settings with a mode: %q", got)
	}
}

func TestSpawnMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := Spawn(t.Context(), nil); err == nil {
		t.Fatal("Spawn succeeded without a claude binary")
	}
}

func TestReplayPausesAtPermission(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	lines := []string{
		`{"type":"system","subtype":"init","session_id":"s","model":"m","cwd":"/w"}`,
		`not json`,
		`{"type":"stream_event","parent_tool_use_id":null,"event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}`,
		`{"type":"stream_event","parent_tool_use_id":null,"event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}}`,
		`{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}`,
		`{"type":"control_request","request_id":"r2","request":{"subtype":"can_use_tool","tool_name":"Write","input":{"file_path":"a"}}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"done","total_cost_usd":0.5,"duration_ms":7}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sess, err := Replay(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	expect := func(want protocol.Event) {
		t.Helper()
		if got := next(t, sess.Events); !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	}
	expect(protocol.Init{SessionID: "s", Model: "m", Cwd: "/w"})
	expect(protocol.TextStart{})
	expect(protocol.TextDelta{Text: "hi"})
	expect(protocol.PermissionRequest{RequestID: "r1", ToolName: "Bash", Input: map[string]any{"command": "ls"}})

	quiet(t, sess.Events)
	sess.Send(protocol.UserMessage{Text: "ignored"})
	quiet(t, sess.Events)
	sess.Send(protocol.Allow{RequestID: "r1", Input: map[string]any{"command": "ls"}})
	expect(protocol.PermissionRequest{RequestID: "r2", ToolName: "Write", Input: map[string]any{"file_path": "a"}})

	quiet(t, sess.Events)
	sess.Send(protocol.Deny{RequestID: "r2", Message: "no"})
	expect(protocol.Result{Text: "done", CostUSD: 0.5, DurationMS: 7})

	quiet(t, sess.Events) // EOF: stays open, no Exited
	cancel()
	select {
	case ev, ok := <-sess.Events:
		if ok {
			t.Fatalf("event after cancel: %#v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Events not closed after cancel")
	}
}

func TestReplayDemo(t *testing.T) {
	sess, err := Replay(t.Context(), demo)
	if err != nil {
		t.Fatal(err)
	}
	var got []protocol.Event
	for {
		ev := next(t, sess.Events)
		got = append(got, ev)
		if p, ok := ev.(protocol.PermissionRequest); ok {
			sess.Send(protocol.Allow{RequestID: p.RequestID, Input: p.Input})
		}
		if _, ok := ev.(protocol.Result); ok {
			break
		}
	}
	sameEvents(t, got, parseFile(t, demo))
}

func TestReplayMissingFile(t *testing.T) {
	if _, err := Replay(t.Context(), filepath.Join(t.TempDir(), "nope.jsonl")); err == nil {
		t.Fatal("Replay succeeded on a missing file")
	}
}
