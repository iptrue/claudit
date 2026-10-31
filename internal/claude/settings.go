package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// managedSettings is the file an administrator sets claude up with; it outranks the user's settings.
var managedSettings = func() string {
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/ClaudeCode/managed-settings.json"
	case "windows":
		return `C:\Program Files\ClaudeCode\managed-settings.json`
	}
	return "/etc/claude-code/managed-settings.json"
}()

// settings is what one of claude's settings files sets of what claudit needs to know.
type settings struct {
	Model       string `json:"model"`
	EffortLevel string `json:"effortLevel"`
	Permissions struct {
		DefaultMode string `json:"defaultMode"`
	} `json:"permissions"`
}

// readSettings reads the settings files claude reads in cwd, the one that wins first: managed,
// local, project, user. Those missing or broken are left out.
func readSettings(cwd string) []settings {
	config := os.Getenv("CLAUDE_CONFIG_DIR")
	if config == "" {
		home, _ := os.UserHomeDir()
		config = filepath.Join(home, ".claude")
	}
	var out []settings
	for _, path := range []string{
		managedSettings,
		filepath.Join(cwd, ".claude", "settings.local.json"),
		filepath.Join(cwd, ".claude", "settings.json"),
		filepath.Join(config, "settings.json"),
	} {
		var s settings
		if data, err := os.ReadFile(path); err == nil && json.Unmarshal(data, &s) == nil {
			out = append(out, s)
		}
	}
	return out
}

// flag is the value args give the flag name, as "name value" or "name=value"; ok is false for none.
func flag(args []string, name string) (v string, ok bool) {
	for i, a := range args {
		if v, ok := strings.CutPrefix(a, name+"="); ok {
			return v, true
		}
		if a == name && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

// Model is the model claude starts with in cwd given args, picked as claude does: --model, then
// $ANTHROPIC_MODEL, then the "model" of the managed, local, project and user settings. It is an
// alias ("opus"), a full id, or "default" when nothing names one. claude -p tells its model only
// with the first turn.
func Model(args []string, cwd string) string {
	if v, ok := flag(args, "--model"); ok {
		return v
	}
	if v := os.Getenv("ANTHROPIC_MODEL"); v != "" {
		return v
	}
	for _, s := range readSettings(cwd) {
		if s.Model != "" {
			return s.Model
		}
	}
	return "default"
}

// Effort is the effort claude starts with in cwd given args, picked as claude does:
// $CLAUDE_CODE_EFFORT_LEVEL, which overrides it for the session, then --effort, then the
// effortLevel of the managed, local, project and user settings. "" when nothing names one: the
// model's own. claude -p tells its effort only with the first turn.
func Effort(args []string, cwd string) string {
	if v := os.Getenv("CLAUDE_CODE_EFFORT_LEVEL"); v != "" {
		return v
	}
	if v, ok := flag(args, "--effort"); ok {
		return v
	}
	for _, s := range readSettings(cwd) {
		if s.EffortLevel != "" {
			return s.EffortLevel
		}
	}
	return ""
}

// WithMode is args with --permission-mode mode added, unless they or the settings in cwd pick a
// permission mode already.
func WithMode(args []string, cwd, mode string) []string {
	if _, ok := flag(args, "--permission-mode"); ok || slices.Contains(args, "--dangerously-skip-permissions") {
		return args
	}
	for _, s := range readSettings(cwd) {
		if s.Permissions.DefaultMode != "" {
			return args
		}
	}
	return append(slices.Clip(args), "--permission-mode", mode)
}
