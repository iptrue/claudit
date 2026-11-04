package claude

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("CLAUDIT_TEST_HELPER"); mode != "" {
		os.Exit(runClaudeHelper(mode))
	}
	os.Exit(m.Run())
}

// installClaudeHelper puts this test executable on PATH as a native fake claude.
func installClaudeHelper(t *testing.T, mode string) string {
	t.Helper()
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	name := "claude"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CLAUDIT_TEST_HELPER", mode)
	t.Setenv("FAKE_DIR", dir)
	return dir
}

func helperWrite(name, text string) {
	if err := os.WriteFile(filepath.Join(os.Getenv("FAKE_DIR"), name), []byte(text), 0o600); err != nil {
		panic(err)
	}
}

func helperInputs(name string, count int) {
	f, err := os.Create(filepath.Join(os.Getenv("FAKE_DIR"), name))
	if err != nil {
		panic(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(os.Stdin)
	for i := 0; i < count && scanner.Scan(); i++ {
		fmt.Fprintln(f, scanner.Text())
	}
}

func helperChild() *exec.Cmd {
	exe, err := os.Executable()
	if err != nil {
		panic(err)
	}
	child := exec.Command(exe)
	child.Env = append(os.Environ(), "CLAUDIT_TEST_HELPER=sleep")
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		panic(err)
	}
	return child
}

func runClaudeHelper(mode string) int {
	switch mode {
	case "stream":
		helperWrite("args", strings.Join(os.Args[1:], "\n")+"\n")
		data, err := os.ReadFile(os.Getenv("FAKE_FIXTURE"))
		if err != nil {
			panic(err)
		}
		os.Stdout.Write(data)
		// One line far beyond bufio.Scanner's default 64 KiB limit.
		fmt.Printf("{\"type\":\"user\",\"message\":{\"content\":[{\"type\":\"tool_result\",\"tool_use_id\":\"big\",\"content\":\"%s\"}]}}\n", strings.Repeat("a", 1<<20))
		fmt.Fprintln(os.Stderr, "warning: fake")
		helperInputs("stdin", 5)
		return 3
	case "cancel":
		helperWatchHangup()
		child := helperChild()
		helperWrite("child", strconv.Itoa(child.Process.Pid))
		helperWrite("pid", strconv.Itoa(os.Getpid()))
		fmt.Fprintln(os.Stderr, "started")
		child.Wait()
	case "resume":
		data, _ := os.ReadFile(filepath.Join(os.Getenv("FAKE_DIR"), "n"))
		n, _ := strconv.Atoi(strings.TrimSpace(string(data)))
		n++
		suffix := strconv.Itoa(n)
		helperWrite("n", suffix)
		helperWrite("args"+suffix, strings.Join(os.Args[1:], "\n")+"\n")
		var child *exec.Cmd
		if n == 1 {
			helperWatchHangup()
			child = helperChild()
			helperWrite("grandchild", strconv.Itoa(child.Process.Pid))
		}
		fmt.Printf("{\"type\":\"system\",\"subtype\":\"status\",\"permissionMode\":\"run%d\"}\n", n)
		if child != nil {
			child.Wait()
		} else {
			helperInputs("stdin"+suffix, 3)
			return 4
		}
	case "limits":
		if os.Getenv("ANTHROPIC_LOG") == "debug" {
			fmt.Print(limitsDump)
		}
		fmt.Println(`{"type":"conversation_reset"}`)
	case "sleep":
		time.Sleep(30 * time.Second)
	default:
		panic("unknown helper mode: " + mode)
	}
	return 0
}
