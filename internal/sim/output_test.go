package sim

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pixelbender/claudit/internal/ide"
)

// classified is what classify makes of output, one "kind lang prefix|text" per line for short diffs.
func classified(cmd, output string, failed bool) []string {
	names := map[ide.LineKind]string{ide.Output: "out", ide.ErrorOutput: "err", ide.Code: "code", ide.Match: "hit",
		ide.Added: "add", ide.Removed: "del", ide.Hunk: "hunk", ide.Meta: "meta", ide.Status: "status", ide.Stat: "stat"}
	var out []string
	for _, l := range classify(cmd, strings.Split(output, "\n"), failed) {
		s := names[l.Kind]
		if l.Lang != "" {
			s += " " + l.Lang
		}
		if l.Prefix > 0 {
			s += " " + strings.Repeat("^", l.Prefix)
		}
		out = append(out, s+"|"+l.Text)
	}
	return out
}

func checkClassified(t *testing.T, cmd, output string, failed bool, want []string) {
	t.Helper()
	if got := classified(cmd, output, failed); !reflect.DeepEqual(got, want) {
		t.Errorf("%s:\n got %q\nwant %q", cmd, got, want)
	}
}

// A diff's lines go by their sign, its headers and hunks by what they are, and the lines around a
// change are code in the language of their file; the next file's diff starts over.
func TestClassifyDiff(t *testing.T) {
	out := "diff --git a/calc.go b/calc.go\n" +
		"index 1a2b3c4..5d6e7f8 100644\n" +
		"--- a/calc.go\n" +
		"+++ b/calc.go\n" +
		"@@ -1,4 +1,4 @@ package calc\n" +
		" func Div(a, b int) (int, error) {\n" +
		"-\treturn a / b, nil\n" +
		"+\treturn 0, errors.New(\"division by zero\")\n" +
		"--- a comment that lost a dash\n" +
		"\\ No newline at end of file\n" +
		"diff --git a/notes.md b/notes.md\n" +
		"new file mode 100644\n" +
		"--- /dev/null\n" +
		"+++ b/notes.md\n" +
		"@@ -0,0 +1 @@\n" +
		"+# Notes"
	checkClassified(t, "git diff", out, false, []string{
		"meta|diff --git a/calc.go b/calc.go",
		"meta|index 1a2b3c4..5d6e7f8 100644",
		"meta|--- a/calc.go",
		"meta|+++ b/calc.go",
		"hunk|@@ -1,4 +1,4 @@ package calc",
		"code calc.go| func Div(a, b int) (int, error) {",
		"del calc.go|-\treturn a / b, nil",
		"add calc.go|+\treturn 0, errors.New(\"division by zero\")",
		"del calc.go|--- a comment that lost a dash",
		"meta|\\ No newline at end of file",
		"meta|diff --git a/notes.md b/notes.md",
		"meta|new file mode 100644",
		"meta|--- /dev/null",
		"meta|+++ b/notes.md",
		"hunk|@@ -0,0 +1 @@",
		"add notes.md|+# Notes",
	})
	// diff -u puts the time after each name; a failed diff (they differ) is a diff all the same.
	checkClassified(t, "diff -u old.py new.py", "--- old.py    2026-10-31 12:00:00\n+++ new.py    2026-10-31 12:01:00\n@@ -1,2 +1 @@\n x = 1\n-y = 2", true, []string{
		"meta|--- old.py    2026-10-31 12:00:00",
		"meta|+++ new.py    2026-10-31 12:01:00",
		"hunk|@@ -1,2 +1 @@",
		"code new.py| x = 1",
		"del new.py|-y = 2",
	})
}

// A hunk ends with the lines its header counts: what follows is no change. A file's text that
// shows a diff is the file's text; a patch's is a diff.
func TestClassifyDiffEnds(t *testing.T) {
	checkClassified(t, "git diff; go test ./...", "@@ -1,2 +1,2 @@\n x\n-a\n+b\n--- FAIL: TestDiv\n    calc_test.go:12: got 1", true, []string{
		"hunk|@@ -1,2 +1,2 @@",
		"code| x",
		"del|-a",
		"add|+b",
		"err|--- FAIL: TestDiv",
		"err|    calc_test.go:12: got 1",
	})
	readme := "```diff\n@@ -1 +1 @@\n-a\n+b\n```"
	checkClassified(t, "cat README.md", readme, false, []string{
		"code README.md|```diff", "code README.md|@@ -1 +1 @@", "code README.md|-a", "code README.md|+b", "code README.md|```",
	})
	checkClassified(t, "cat fix.patch", "@@ -1 +1 @@\n-a\n+b", false, []string{"hunk|@@ -1 +1 @@", "del|-a", "add|+b"})
}

// A log with patches has its commits' headers apart, their messages as plain output.
func TestClassifyLogPatch(t *testing.T) {
	out := "commit 0a1b2c3d4e5f\nAuthor: Ann <ann@example.com>\nDate:   Fri Oct 31 12:00:00 2026\n\n    Fix the error in Div\n\n" +
		"diff --git a/calc.go b/calc.go\n@@ -1 +1 @@\n-a\n+b\ncommit 9f8e7d6c5b4a\n\n    Docs"
	checkClassified(t, "git log -p -2", out, false, []string{
		"meta|commit 0a1b2c3d4e5f",
		"meta|Author: Ann <ann@example.com>",
		"meta|Date:   Fri Oct 31 12:00:00 2026",
		"out|",
		"out|    Fix the error in Div",
		"out|",
		"meta|diff --git a/calc.go b/calc.go",
		"hunk|@@ -1 +1 @@",
		"del|-a",
		"add|+b",
		"meta|commit 9f8e7d6c5b4a",
		"out|",
		"out|    Docs",
	})
}

// What prints a file shows code in that file's language, whatever else the command line does.
func TestClassifyViewer(t *testing.T) {
	for cmd, lang := range map[string]string{
		"cat internal/calc/calc.go":            "internal/calc/calc.go",
		"sed -n '1,40p' calc.go":               "calc.go",
		"head -n 20 app.py | cat -n":           "app.py",
		"cd sub && tail -5 'src/main.rs'":      "src/main.rs",
		"awk 'NR>2' Makefile":                  "Makefile",
		"git show HEAD~1:internal/ui/model.go": "internal/ui/model.go",
		"cat > out.go <<'EOF'":                 "",
		"head -c 100 /dev/urandom | wc -c":     "",
		"cat calc.go && echo done":             "", // the last command prints what is seen last
		"sed -i 's/a/b/' calc.go && go test":   "",
	} {
		got := classify(cmd, []string{"x := 1"}, false)[0]
		want := ide.TermLine{Kind: ide.Code, Text: "x := 1", Lang: lang}
		if lang == "" {
			want = ide.TermLine{Kind: ide.Output, Text: "x := 1"}
		}
		if got != want {
			t.Errorf("%s: %+v, want %+v", cmd, got, want)
		}
	}
}

// What a viewer that failed prints is its complaint, not the file.
func TestClassifyViewerFailed(t *testing.T) {
	checkClassified(t, "cat missing.go", "cat: missing.go: No such file or directory", true, []string{"err|cat: missing.go: No such file or directory"})
}

// A search's hits are code in the language of the file they are from, after where they are; a
// search in one file has only the line numbers.
func TestClassifySearch(t *testing.T) {
	out := "internal/calc/calc.go:12:\treturn 0, errors.New(\"x\")\ninternal/calc/calc-test.go-13-\t// context\n--\nFound 2 files"
	checkClassified(t, "rg -n 'errors.New' internal", out, false, []string{
		"hit internal/calc/calc.go " + strings.Repeat("^", len("internal/calc/calc.go:12:")) + "|internal/calc/calc.go:12:\treturn 0, errors.New(\"x\")",
		"hit internal/calc/calc-test.go " + strings.Repeat("^", len("internal/calc/calc-test.go-13-")) + "|internal/calc/calc-test.go-13-\t// context",
		"out|--",
		"out|Found 2 files",
	})
	checkClassified(t, "grep -n err calc.go", "12:if err != nil {", false, []string{"hit calc.go ^^^|12:if err != nil {"})
	checkClassified(t, "git grep -n TODO", "a.py:3:# TODO", false, []string{"hit a.py ^^^^^^^|a.py:3:# TODO"})
	checkClassified(t, "cat calc.go | grep -n err | head", "12:if err != nil {", false, []string{"hit calc.go ^^^|12:if err != nil {"})
	checkClassified(t, "git show HEAD:calc.go | grep -n err", "12:if err != nil {", false, []string{"hit calc.go ^^^|12:if err != nil {"})
	// A search without a file of its own after another: the hits are the other's.
	checkClassified(t, "git show HEAD:calc.go | grep -n -E 'err|Div'; git diff calc.go | grep -c ''", "12:if err != nil {\n0", false,
		[]string{"hit calc.go ^^^|12:if err != nil {", "out|0"})
	checkClassified(t, "grep -n err calc.go; go vet ./...", "./calc.go:12:2: unreachable code", false, []string{"out|./calc.go:12:2: unreachable code"})
	checkClassified(t, "grep -n x a.go; grep -c y b.go", "1:x\n0", false, []string{"hit a.go ^^|1:x", "out|0"})
	// A search that prints numbered lines of its own takes no earlier search's file.
	checkClassified(t, "grep -n x a.go; go test ./... 2>&1 | grep -n FAIL", "1:x\n12:--- FAIL: TestX", false,
		[]string{"out|1:x", "out|12:--- FAIL: TestX"})
	checkClassified(t, "git show --format=%h:%s HEAD | grep -n x", "3:x", false, []string{"out|3:x"})
	// The flags that take a value, the patterns that look like redirections, pipes of stderr too.
	checkClassified(t, "grep -n -A 3 Div calc.go", "12:func Div() {", false, []string{"hit calc.go ^^^|12:func Div() {"})
	checkClassified(t, "grep -n '<div' index.html", "3:<div>", false, []string{"hit index.html ^^|3:<div>"})
	checkClassified(t, "rg -n TODO |& head", "a.go:1:// TODO", false, []string{"hit a.go ^^^^^^^|a.go:1:// TODO"})
	// A date is no line number; a path may have dashes and digits.
	checkClassified(t, "grep ERROR app.log", "2024-05-01 10:00:00 ERROR db down", false, []string{"out|2024-05-01 10:00:00 ERROR db down"})
	checkClassified(t, "rg -n x _posts", "_posts/2024-01-15-x.md:3:x", false, []string{"hit _posts/2024-01-15-x.md " + strings.Repeat("^", len("_posts/2024-01-15-x.md:3:")) + "|_posts/2024-01-15-x.md:3:x"})
}

func TestClassifyWindowsPaths(t *testing.T) {
	for _, path := range []string{`C:\work\calc.go`, `C:/work/calc.go`, `C:\my work\calc.go`, `\\server\share\calc.go`} {
		if got := classify(`cat "`+path+`"`, []string{"package calc"}, false)[0]; got.Kind != ide.Code || got.Lang != path {
			t.Errorf("viewer %q: %+v", path, got)
		}
		for _, sep := range []string{":", "-"} {
			prefix := path + sep + "12" + sep
			line := prefix + "func Div() {"
			got := classify("rg -n Div .", []string{line}, false)[0]
			want := ide.TermLine{Kind: ide.Match, Text: line, Lang: path, Prefix: len([]rune(prefix))}
			if got != want {
				t.Errorf("%q: %+v, want %+v", line, got, want)
			}
		}
	}
}

func TestCommandPaths(t *testing.T) {
	for _, tc := range []struct{ cmd, path string }{
		{`cat C:\work\calc.go`, `C:\work\calc.go`},
		{`cat "C:\my work\calc.go"`, `C:\my work\calc.go`},
		{`cat '\\server\share\calc.go'`, `\\server\share\calc.go`},
		{`cat \\server\share\calc.go`, `\\server\share\calc.go`},
		{`cat "\\server\my share\calc.go"`, `\\server\my share\calc.go`},
		{`cat src/my\ file.go`, `src/my file.go`},
		{`cat src/back\\slash.go`, `src/back\slash.go`},
		{`cat "src/back\\slash.go"`, `src/back\slash.go`},
	} {
		got := commands(tc.cmd)
		want := [][][]string{{{"cat", tc.path}}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("commands(%q) = %q, want %q", tc.cmd, got, want)
		}
	}
}

// Other output stays plain, red when the command failed.
func TestClassifyPlain(t *testing.T) {
	checkClassified(t, "go test ./...", "ok  \tcalc\t0.1s\n--- FAIL: TestDiv", false, []string{"out|ok  \tcalc\t0.1s", "out|--- FAIL: TestDiv"})
	checkClassified(t, "go build ./...", "./calc.go:12:2: undefined: x", true, []string{"err|./calc.go:12:2: undefined: x"})
}

// The terminal prints a command's output classified.
func TestBashClassifiesOutput(t *testing.T) {
	s, _ := setup(t, nil)
	s.ToolUse("main", "b1", "Bash", map[string]any{"command": "git diff"})
	s.ToolResult("b1", "@@ -1 +1 @@\n-old\n+new", false)
	play(s, t0, 5*time.Second, nil)
	var kinds []ide.LineKind
	for _, l := range mainAgent(s).Term.Lines {
		kinds = append(kinds, l.Kind)
	}
	if want := []ide.LineKind{ide.Command, ide.Hunk, ide.Removed, ide.Added, ide.Gap}; !reflect.DeepEqual(kinds, want) {
		t.Errorf("lines %v, want %v", kinds, want)
	}
}

// A short git status gives each change its status, a diffstat its size: the terminal colours their
// marks as git does. The command tells a short status, the look a diffstat.
func TestClassifyGit(t *testing.T) {
	out := "## main...origin/main\nM  staged.go\n M changed.go\n?? new.txt\n  indented\n" +
		" calc.go       | 7 ++++++-\n {a => b}/x.go | 2 --\n img.png       | Bin 0 -> 12 bytes\n" +
		" 3 files changed, 6 insertions(+), 3 deletions(-)"
	checkClassified(t, "git status -sb && git diff --stat", out, false, []string{
		"out|## main...origin/main",
		"status|M  staged.go",
		"status| M changed.go",
		"status|?? new.txt",
		"out|  indented",
		"stat| calc.go       | 7 ++++++-",
		"stat| {a => b}/x.go | 2 --",
		"out| img.png       | Bin 0 -> 12 bytes",
		"out| 3 files changed, 6 insertions(+), 3 deletions(-)",
	})
	checkClassified(t, "git status --porcelain=1", " m sub\nMm mod", false, []string{"status| m sub", "status|Mm mod"})
	// What only looks like a short status is plain output.
	checkClassified(t, "printf ' M x'", " M x", false, []string{"out| M x"})
	checkClassified(t, "git status", " M x", false, []string{"out| M x"})
}

// The lines of a here-document's body are in the language of what it feeds: an interpreter's
// script, the file cat or tee writes; the rest of the command is the shell's.
func TestLineLangs(t *testing.T) {
	for _, c := range []struct {
		cmd  string
		want []string
	}{
		{"python3 - <<'EOF'\ndef f():\n    pass\nEOF\necho done", []string{"", ".py", ".py", "", ""}},
		{"cat > main.go <<EOF\npackage main\nEOF", []string{"", "main.go", ""}},
		{"cat <<'EOF' > notes.md\n# x\nEOF", []string{"", "notes.md", ""}},
		{"cd x && node <<-JS\n\tconsole.log(1)\n\tJS\nls", []string{"", ".js", "", ""}},
		{"tee -a conf.yaml <<EOF\nk: v\nEOF", []string{"", "conf.yaml", ""}},
		{"git commit -F - <<'MSG'\nfix it\nMSG", []string{"", "", ""}},
		{"ls\npwd", []string{"", ""}},
	} {
		if got := lineLangs(c.cmd); !slices.Equal(got, c.want) {
			t.Errorf("%q: %q, want %q", c.cmd, got, c.want)
		}
	}
}
