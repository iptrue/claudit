package claude

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/pixelbender/claudit/internal/protocol"
)

// claude -p reads the rate-limit headers of the API only for a claude.ai login: with a token it
// drops them, and no rate_limit_event comes. With ANTHROPIC_LOG=debug its API client dumps the
// headers of every response on stdout, among the stream-json lines: claudit reads them there.
//
// What claude runs inherits its environment: a nested claude -p, a script or a server on the
// Anthropic SDK would dump on its own stdout. claude runs its commands and hooks through
// CLAUDE_CODE_SHELL_PREFIX, as '<prefix>' '<script>': claudit's prefix drops ANTHROPIC_LOG and
// runs the script in the shell claude picks (CLAUDE_CODE_SHELL or SHELL when bash or zsh, else
// zsh or bash). MCP servers do not go through it.

// shellPrefix is the program claudit hands claude as its CLAUDE_CODE_SHELL_PREFIX.
const shellPrefix = `#!/bin/sh
unset ANTHROPIC_LOG
for sh in "$CLAUDE_CODE_SHELL" "$SHELL"; do
	case $sh in *bash*|*zsh*) [ -x "$sh" ] && exec "$sh" -c "$1" ;; esac
done
for sh in zsh bash; do
	command -v "$sh" >/dev/null 2>&1 && exec "$sh" -c "$1"
done
exec /bin/sh -c "$1"
`

// claudeEnv is the environment claude runs in: claudit's own, ANTHROPIC_LOG=debug, and the shell
// prefix that keeps it from what claude runs, unless the user has a prefix of their own. done
// removes the prefix once claude is gone for good.
func claudeEnv() (env []string, done func(), err error) {
	if runtime.GOOS == "windows" {
		// Respect Claude's native shell selection. Unix debug wrappers cannot be executed by
		// PowerShell/CMD; Windows uses the protocol's standard rate_limit_event messages.
		return os.Environ(), func() {}, nil
	}
	env, done = append(os.Environ(), "ANTHROPIC_LOG=debug"), func() {}
	if os.Getenv("CLAUDE_CODE_SHELL_PREFIX") != "" {
		return env, done, nil // theirs runs: what claude runs sees ANTHROPIC_LOG
	}
	dir, err := os.MkdirTemp("", "claudit-")
	if err != nil {
		return nil, nil, err
	}
	path := filepath.Join(dir, "shell")
	if err := os.WriteFile(path, []byte(shellPrefix), 0o700); err != nil || strings.Contains(path, " -") { // claude would take " -…" for flags
		os.RemoveAll(dir)
		return env, done, err
	}
	return append(env, "CLAUDE_CODE_SHELL_PREFIX="+path), func() { os.RemoveAll(dir) }, nil
}

// headerLine is one rate-limit header of such a dump, quoted as Bun or Node quote:
//
//	"anthropic-ratelimit-unified-5h-utilization": "0.25",
var headerLine = regexp.MustCompile(`^\s+["']?(anthropic-ratelimit-unified-[\w-]+)["']?: ["']([^"']*)["'],?$`)

// sgr is a colour of the dump, when the runtime is told to colour what it prints.
var sgr = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// debugLimits gathers the rate-limit headers dumped since the last stream-json line.
type debugLimits map[string]string

// line takes a line of claude's stdout. A stream-json line hands out the limits gathered before it.
func (d debugLimits) line(line []byte) (protocol.RateLimits, bool) {
	switch {
	case len(line) > 0 && line[0] == ' ': // the dumps indent their fields, stream-json is flush left
		if m := headerLine.FindSubmatch(sgr.ReplaceAll(line, nil)); m != nil {
			d[string(m[1])] = string(m[2])
		}
	case len(line) > 0 && line[0] == '{' && len(d) > 0:
		l, ok := headerLimits(d)
		clear(d)
		return l, ok
	}
	return protocol.RateLimits{}, false
}

// headerLimits reads the subscription's rate-limit windows from the headers of an API response as
// claude does: anthropic-ratelimit-unified-<window>-utilization and -reset, in unix seconds, and
// -status; and whether extra usage pays past them. Without a window it reports nothing.
func headerLimits(h map[string]string) (protocol.RateLimits, bool) {
	window := func(name string) *protocol.Window {
		used, err := strconv.ParseFloat(h["anthropic-ratelimit-unified-"+name+"-utilization"], 64)
		if err != nil {
			return nil
		}
		reset, err := strconv.ParseFloat(h["anthropic-ratelimit-unified-"+name+"-reset"], 64)
		if err != nil {
			return nil
		}
		return &protocol.Window{Used: used, ResetsAt: time.Unix(int64(reset), 0), Status: h["anthropic-ratelimit-unified-"+name+"-status"]}
	}
	l := protocol.RateLimits{Session: window("5h"), Week: window("7d"), Fable: window("7d_oi"),
		Overage: h["anthropic-ratelimit-unified-overage-status"]}
	return l, l.Session != nil || l.Week != nil || l.Fable != nil
}
