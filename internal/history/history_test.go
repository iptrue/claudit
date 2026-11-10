package history

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, file string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(file, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The prompts of the project, oldest first, the last ones up to the limit; a repeat right after
// itself once; other projects, empty and unreadable lines left out.
func TestLoad(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "history.jsonl")
	write(t, file,
		`{"display":"one","pastedContents":{},"timestamp":1,"project":"/p","sessionId":"a"}`,
		`{"display":"elsewhere","pastedContents":{},"timestamp":2,"project":"/q"}`,
		`not json`,
		`{"display":"","project":"/p"}`,
		`{"display":"two","pastedContents":{},"timestamp":3,"project":"/p"}`,
		`{"display":"two","pastedContents":{},"timestamp":4,"project":"/p"}`,
		`{"display":"three","pastedContents":{},"timestamp":5,"project":"/p"}`)
	got, err := Load(file, "/p", 10)
	if err != nil || !reflect.DeepEqual(got, []Entry{{Text: "one"}, {Text: "two"}, {Text: "three"}}) {
		t.Fatalf("%+v %v", got, err)
	}
	if got, _ := Load(file, "/p", 2); !reflect.DeepEqual(got, []Entry{{Text: "two"}, {Text: "three"}}) {
		t.Errorf("limit: %+v", got)
	}
	if got, err := Load(filepath.Join(dir, "none.jsonl"), "/p", 10); got != nil || err != nil {
		t.Errorf("missing file: %+v %v", got, err)
	}
	if _, err := Load(dir, "/p", 10); err == nil {
		t.Error("a directory read as a history")
	}
}

// A repeat with other pasted texts, as after a restart numbers them anew, is a prompt of its own.
func TestLoadRepeatPasted(t *testing.T) {
	file := filepath.Join(t.TempDir(), "history.jsonl")
	write(t, file,
		`{"display":"see [Pasted text #1]","pastedContents":{"1":{"id":1,"type":"text","content":"old"}},"timestamp":1,"project":"/p"}`,
		`{"display":"see [Pasted text #1]","pastedContents":{"1":{"id":1,"type":"text","content":"new"}},"timestamp":2,"project":"/p"}`,
		`{"display":"see [Pasted text #1]","pastedContents":{"1":{"id":1,"type":"text","content":"new"}},"timestamp":3,"project":"/p"}`)
	got, err := Load(file, "/p", 10)
	if err != nil || len(got) != 2 || got[0].Pasted[1] != "old" || got[1].Pasted[1] != "new" {
		t.Fatalf("%+v %v", got, err)
	}
}

// Pasted texts come from the line, or from the paste cache by their hash; one the cache lost,
// a picture, or a hash that is a path, are left out.
func TestLoadPasted(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "history.jsonl")
	if err := os.MkdirAll(filepath.Join(dir, "paste-cache"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "paste-cache", "abc.txt"), []byte("cached\ntext"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("no"), 0o600); err != nil {
		t.Fatal(err)
	}
	write(t, file, `{"display":"see [Pasted text #1 +1 lines] and [Pasted text #2] [Pasted text #3] [Image #4] [Pasted text #5]","pastedContents":{`+
		`"1":{"id":1,"type":"text","contentHash":"abc"},"2":{"id":2,"type":"text","content":"inline"},`+
		`"3":{"id":3,"type":"text","contentHash":"gone"},"4":{"id":4,"type":"image","content":"x"},`+
		`"5":{"id":5,"type":"text","contentHash":"../secret"}},"timestamp":1,"project":"/p"}`)
	got, err := Load(file, "/p", 10)
	if err != nil || len(got) != 1 || !reflect.DeepEqual(got[0].Pasted, map[int]string{1: "cached\ntext", 2: "inline"}) {
		t.Fatalf("%+v %v", got, err)
	}
}

// What Append writes Load reads back, and Claude Code's fields are there; the file is the user's alone.
func TestAppend(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config", "history.jsonl")
	at := time.UnixMilli(1794275014919)
	if err := Append(file, "/p", "s1", Entry{Text: "!git status"}, at); err != nil {
		t.Fatal(err)
	}
	if err := Append(file, "/p", "s1", Entry{Text: "look [Pasted text #2 +1 lines]", Pasted: map[int]string{2: "a\nb", 1: "z"}}, at); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(file)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	want := `{"display":"!git status","pastedContents":{},"timestamp":1794275014919,"project":"/p","sessionId":"s1"}`
	if len(lines) != 2 || lines[0] != want {
		t.Fatalf("lines %q", lines)
	}
	got, _ := Load(file, "/p", 10)
	if len(got) != 2 || got[1].Text != "look [Pasted text #2 +1 lines]" || !reflect.DeepEqual(got[1].Pasted, map[int]string{1: "z", 2: "a\nb"}) {
		t.Fatalf("read back %+v", got)
	}
	if fi, _ := os.Stat(file); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode())
	}
	if err := Append(filepath.Join(file, "under-a-file"), "/p", "", Entry{Text: "x"}, at); err == nil {
		t.Error("no error writing under a file")
	}
	if err := Append(filepath.Dir(file), "/p", "", Entry{Text: "x"}, at); err == nil {
		t.Error("no error writing to a directory")
	}
}

func TestFileAndProject(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "cfg")
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	if f := File(); f != filepath.Join(cfg, "history.jsonl") {
		t.Errorf("config dir: %q", f)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	home, _ := os.UserHomeDir()
	if f := File(); f != filepath.Join(home, ".claude", "history.jsonl") {
		t.Errorf("home: %q", f)
	}
	dir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Skip(err)
	}
	real, _ := filepath.EvalSymlinks(dir)
	if p := Project(link); p != real {
		t.Errorf("symlink: %q, want %q", p, real)
	}
	if p := Project(filepath.Join(dir, "no", "such")); p != filepath.Join(dir, "no", "such") {
		t.Errorf("missing: %q", p)
	}
}
