package claude

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pixelbender/claudit/internal/protocol"
)

// limitsDump is what claude 2.1.314 prints on stdout with ANTHROPIC_LOG=debug around one API
// response, shortened: two dumps of its headers, 25% of the session used, 60% of the week.
const limitsDump = `[log_f81be8] sending request {
  method: "post",
  url: "https://api.anthropic.com/v1/messages?beta=true",
}
[log_f81be8] response start {
  url: "https://api.anthropic.com/v1/messages?beta=true",
  status: 200,
  headers: {
    "anthropic-ratelimit-unified-5h-reset": "1793311200",
    "anthropic-ratelimit-unified-5h-status": "allowed",
    "anthropic-ratelimit-unified-5h-utilization": "0.25",
    "anthropic-ratelimit-unified-7d-reset": "1793660400",
    "anthropic-ratelimit-unified-7d-status": "allowed",
    "anthropic-ratelimit-unified-7d-utilization": "0.6",
    "anthropic-ratelimit-unified-overage-disabled-reason": "out_of_credits",
    "anthropic-ratelimit-unified-overage-status": "rejected",
    "anthropic-ratelimit-unified-status": "allowed",
  },
  durationMs: 1393,
}
response 200 https://api.anthropic.com/v1/messages?beta=true Headers {
  "anthropic-ratelimit-unified-5h-utilization": "0.25",
  "anthropic-ratelimit-unified-7d-reset": "1793660400",
  "anthropic-ratelimit-unified-5h-reset": "1793311200",
  "anthropic-ratelimit-unified-7d-utilization": "0.6",
} ReadableStream { locked: false, state: 'readable', supportsBYOB: false }
`

var limitsWant = protocol.RateLimits{
	Session: &protocol.Window{Used: 0.25, ResetsAt: time.Unix(1793311200, 0), Status: "allowed"},
	Week:    &protocol.Window{Used: 0.6, ResetsAt: time.Unix(1793660400, 0), Status: "allowed"},
	Overage: "rejected",
}

// The windows are read as claude reads them, with how close each is to its limit; one without a
// number is left out, and a response without any reports nothing, whatever else it says.
func TestHeaderLimits(t *testing.T) {
	h := map[string]string{
		"anthropic-ratelimit-unified-status":            "allowed",
		"anthropic-ratelimit-unified-overage-status":    "rejected",
		"anthropic-ratelimit-unified-5h-utilization":    "0.25",
		"anthropic-ratelimit-unified-5h-reset":          "1793311200",
		"anthropic-ratelimit-unified-5h-status":         "allowed",
		"anthropic-ratelimit-unified-7d-utilization":    "0.6",
		"anthropic-ratelimit-unified-7d-reset":          "1793660400",
		"anthropic-ratelimit-unified-7d-status":         "allowed",
		"anthropic-ratelimit-unified-7d_oi-utilization": "1.2", // no reset
	}
	if got, ok := headerLimits(h); !ok || !reflect.DeepEqual(got, limitsWant) {
		t.Errorf("limits %+v %v, want %+v", got, ok, limitsWant)
	}
	h["anthropic-ratelimit-unified-7d_oi-reset"] = "1793660400"
	if got, _ := headerLimits(h); got.Fable == nil || got.Fable.Used != 1.2 {
		t.Errorf("fable window %+v", got.Fable)
	}
	if got, ok := headerLimits(map[string]string{"anthropic-ratelimit-unified-status": "allowed", "anthropic-ratelimit-unified-overage-status": "rejected"}); ok {
		t.Errorf("no windows, yet %+v", got)
	}
}

// The limits of the dumps go out once, with the stream-json line after them; a line of claude's
// own output that merely holds such a header is no dump.
func TestDebugLimits(t *testing.T) {
	d := debugLimits{}
	feed := func(text string) []protocol.RateLimits {
		var out []protocol.RateLimits
		for line := range strings.Lines(text) {
			if l, ok := d.line([]byte(strings.TrimSuffix(line, "\n"))); ok {
				out = append(out, l)
			}
		}
		return out
	}
	if got := feed(limitsDump); len(got) != 0 {
		t.Errorf("limits before the next stream-json line: %+v", got)
	}
	if got := feed(`{"type":"conversation_reset"}` + "\n"); len(got) != 1 || !reflect.DeepEqual(got[0], limitsWant) {
		t.Errorf("limits %+v, want once %+v", got, limitsWant)
	}
	if got := feed(`{"type":"conversation_reset"}` + "\n"); len(got) != 0 {
		t.Errorf("the same limits again: %+v", got)
	}
	// claude run by Node quotes as Node does, and colours what it prints when told to.
	node := "    'anthropic-ratelimit-unified-5h-utilization': '0.25',\n    'anthropic-ratelimit-unified-5h-reset': \x1b[32m'1793311200'\x1b[39m,\n" +
		"    'anthropic-ratelimit-unified-5h-status': 'allowed',\n"
	want := protocol.RateLimits{Session: &protocol.Window{Used: 0.25, ResetsAt: time.Unix(1793311200, 0), Status: "allowed"}}
	if got := feed(node + `{"type":"conversation_reset"}` + "\n"); len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Errorf("Node's dump: limits %+v, want %+v", got, want)
	}
	said := `{"type":"assistant","message":{"content":[{"type":"text","text":"\n    \"anthropic-ratelimit-unified-5h-utilization\": \"0.9\",\n"}]}}`
	if got := feed(said + "\n" + said + "\n"); len(got) != 0 {
		t.Errorf("limits out of a reply: %+v", got)
	}
}

// claude runs with ANTHROPIC_LOG=debug; the commands and hooks it runs through its shell prefix,
// as it does ('prefix' 'script'), run without it, in a shell, their exit code theirs. The prefix
// goes once claude is done; a prefix of the user's own stays theirs.
func TestSpawnShellPrefix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell prefix")
	}
	dir := t.TempDir()
	fake := `#!/bin/sh
echo "$ANTHROPIC_LOG" > "$FAKE_DIR/self"
echo "$CLAUDE_CODE_SHELL_PREFIX" > "$FAKE_DIR/prefix"
[ -x "$CLAUDE_CODE_SHELL_PREFIX" ] || exit 0
"$CLAUDE_CODE_SHELL_PREFIX" 'printenv ANTHROPIC_LOG || echo unset; cd / && pwd' > "$FAKE_DIR/child"
"$CLAUDE_CODE_SHELL_PREFIX" 'exit 3'
echo $? > "$FAKE_DIR/code"
`
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_DIR", dir)
	t.Setenv("CLAUDE_CODE_SHELL_PREFIX", "")
	read := func(name string) string {
		data, _ := os.ReadFile(filepath.Join(dir, name))
		return strings.TrimSpace(string(data))
	}
	run := func() {
		sess, err := Spawn(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		for range sess.Events { // closed once claude is done and the prefix gone
		}
	}
	run()
	if read("self") != "debug" || read("child") != "unset\n/" || read("code") != "3" {
		t.Errorf("claude saw %q; its command printed %q and exited %q", read("self"), read("child"), read("code"))
	}
	if _, err := os.Stat(read("prefix")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the prefix %q outlived claude: %v", read("prefix"), err)
	}
	t.Setenv("CLAUDE_CODE_SHELL_PREFIX", "/opt/audit/wrap")
	run()
	if read("prefix") != "/opt/audit/wrap" {
		t.Errorf("the user's prefix became %q", read("prefix"))
	}
}

// claude runs with ANTHROPIC_LOG=debug, and the limits its client prints come among its events.
func TestSpawnDebugLimits(t *testing.T) {
	installClaudeHelper(t, "limits")
	t.Setenv("ANTHROPIC_LOG", "") // claudit sets it, whatever the user has
	if runtime.GOOS == "windows" {
		t.Setenv("ANTHROPIC_LOG", "debug")
	}

	sess, err := Spawn(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []protocol.Event
	for {
		ev := next(t, sess.Events)
		if _, ok := ev.(protocol.Exited); ok {
			break
		}
		got = append(got, ev)
	}
	want := []protocol.Event{limitsWant, protocol.Reset{}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("events %+v, want %+v", got, want)
	}
}
