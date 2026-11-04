package claude

import (
	"os"
	"slices"
	"testing"
)

func TestWindowsEnvironmentKeepsNativeShell(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SHELL", "powershell")
	t.Setenv("CLAUDE_CODE_SHELL_PREFIX", "")
	t.Setenv("ANTHROPIC_LOG", "")
	env, done, err := claudeEnv()
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	if !slices.Equal(env, os.Environ()) {
		t.Fatal("Windows environment introduced a Unix wrapper or forced debug logging")
	}
	t.Setenv("CLAUDE_CODE_SHELL_PREFIX", `C:\\Tools\\own-wrapper.exe`)
	env, done, err = claudeEnv()
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	if !slices.Contains(env, `CLAUDE_CODE_SHELL_PREFIX=C:\\Tools\\own-wrapper.exe`) {
		t.Fatal("the user's shell prefix was replaced")
	}
}
