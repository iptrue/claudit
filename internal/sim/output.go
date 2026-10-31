package sim

import (
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/pixelbender/claudit/internal/ide"
)

// What a command's output shows, for the terminal to colour it as the editor would: a diff, the
// text of a file, the hits of a search, or plain output.

var (
	hunkHead   = regexp.MustCompile(`^@@ -\d+(?:,(\d+))? \+\d+(?:,(\d+))? @@`) // with the lines it takes of each side
	commitHead = regexp.MustCompile(`^commit [0-9a-f]{7,40}\b`)
	hitHead    = regexp.MustCompile(`^([^:\s][^:]*?):(\d+):`) // "path:12:" of a hit
	aroundHead = regexp.MustCompile(`^([^:\s][^:]*?)-(\d+)-`) // "path-12-" of a line around one
	numHead    = regexp.MustCompile(`^\d+:`)                  // "12:" of a hit of a search in one file
)

// diffMeta are the starts of a diff's or a log's header lines.
var diffMeta = []string{"diff ", "index ", "new file mode", "deleted file mode", "old mode", "new mode",
	"similarity index", "rename from", "rename to", "Binary files", "Author:", "Date:", "Merge:"}

// viewers print the files they are given, or else what they read, lines and all.
var viewers = map[string]bool{"cat": true, "bat": true, "batcat": true, "head": true, "tail": true, "sed": true,
	"awk": true, "nl": true, "tac": true, "less": true, "more": true, "sort": true, "uniq": true}

// searchers print the lines that match: "path:12:text", or "12:text" in a single file.
var searchers = map[string]bool{"rg": true, "grep": true, "egrep": true, "fgrep": true, "ag": true, "ack": true, "git grep": true}

// valued are the flags of a viewer or a searcher that take the word after them as their value.
var valued = map[string][]string{
	"grep": {"-A", "-B", "-C", "-m", "-e", "-f", "-d", "-D"}, "egrep": {"-A", "-B", "-C", "-m", "-e", "-f"},
	"fgrep": {"-A", "-B", "-C", "-m", "-e", "-f"}, "git grep": {"-A", "-B", "-C", "-m", "-e", "-f"},
	"rg": {"-A", "-B", "-C", "-m", "-e", "-f", "-t", "-T", "-g", "-M", "-j", "-r"}, "ag": {"-A", "-B", "-C", "-m", "-G"},
	"head": {"-n", "-c"}, "tail": {"-n", "-c"}, "sed": {"-e", "-f"}, "awk": {"-F", "-v", "-f", "-e"},
	"sort": {"-k", "-t", "-o"}, "uniq": {"-f", "-s"},
}

// classify marks the lines of the output of cmd by what they show. failed says the command failed:
// its plain output is an error's.
func classify(cmd string, lines []string, failed bool) []ide.TermLine {
	base := ide.Output
	if failed {
		base = ide.ErrorOutput
	}
	out := make([]ide.TermLine, len(lines))
	for i, l := range lines {
		out[i] = ide.TermLine{Kind: base, Text: l}
	}
	what, file := source(cmd)
	diff := slices.ContainsFunc(lines, func(l string) bool { return hunkHead.MatchString(l) || strings.HasPrefix(l, "diff --git ") })
	switch ext := filepath.Ext(file); {
	case what == "view" && failed: // what it says is its complaint, not the file
	case diff && (what != "view" || ext == ".diff" || ext == ".patch"): // a file that shows a diff is that file
		markDiff(out)
	case what == "search":
		markHits(out, file)
	case what == "view":
		for i := range out {
			out[i].Kind, out[i].Lang = ide.Code, file
		}
	}
	return out
}

// markDiff marks the lines of a diff: changes by their sign, headers, hunks, and the lines around
// the changes as code in the language of their file. A hunk ends with the lines its header counts
// of each side: what follows it is no change.
func markDiff(out []ide.TermLine) {
	file := ""
	old, new := 0, 0 // the lines left of the hunk, of the old side and of the new
	for i := range out {
		l := &out[i]
		t := l.Text
		inHunk := old > 0 || new > 0
		switch {
		case inHunk && strings.HasPrefix(t, "+"):
			l.Kind, l.Lang, new = ide.Added, file, new-1
		case inHunk && strings.HasPrefix(t, "-"):
			l.Kind, l.Lang, old = ide.Removed, file, old-1
		case inHunk && (strings.HasPrefix(t, " ") || t == ""):
			l.Kind, l.Lang, old, new = ide.Code, file, old-1, new-1
		case strings.HasPrefix(t, `\`): // "\ No newline at end of file"
			l.Kind = ide.Meta
		case hunkHead.MatchString(t):
			m := hunkHead.FindStringSubmatch(t)
			l.Kind, old, new = ide.Hunk, count(m[1]), count(m[2])
		case strings.HasPrefix(t, "+++ "):
			if p := diffPath(t[4:]); p != "" {
				file = p
			}
			l.Kind, old, new = ide.Meta, 0, 0
		case strings.HasPrefix(t, "--- ") && i+1 < len(out) && strings.HasPrefix(out[i+1].Text, "+++ "): // a file's header
			l.Kind, old, new, file = ide.Meta, 0, 0, diffPath(t[4:])
		case commitHead.MatchString(t) || slices.ContainsFunc(diffMeta, func(p string) bool { return strings.HasPrefix(t, p) }):
			l.Kind, old, new = ide.Meta, 0, 0
		default:
			old, new = 0, 0
		}
	}
}

// count is the number of lines a hunk header gives a side: 1 when it gives none.
func count(s string) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return 1
}

// diffPath is the file a diff's "--- " or "+++ " line names, without git's "a/" or "b/" and the
// time diff -u puts after it; "" for /dev/null.
func diffPath(s string) string {
	s, _, _ = strings.Cut(s, "    ") // the time, after a tab turned into spaces
	if s = strings.TrimSpace(s); s == "/dev/null" {
		return ""
	}
	if rest, ok := strings.CutPrefix(s, "a/"); ok {
		return rest
	}
	return strings.TrimPrefix(s, "b/")
}

// markHits marks the hits of a search as code in the language of their file, after the "path:12:"
// (or "path-12-" of a line around one) that says where; in file, when a search in a single file
// prints only "12:".
func markHits(out []ide.TermLine, file string) {
	path := func(s string) bool {
		return strings.ContainsFunc(s, func(r rune) bool { return unicode.IsLetter(r) || r == '/' || r == '.' })
	}
	for i := range out {
		l := &out[i]
		m := hitHead.FindStringSubmatch(l.Text)
		if m == nil || !path(m[1]) {
			m = aroundHead.FindStringSubmatch(l.Text)
		}
		switch {
		case m != nil && path(m[1]):
			l.Kind, l.Lang, l.Prefix = ide.Match, m[1], len([]rune(m[0]))
		case file != "" && numHead.MatchString(l.Text):
			l.Kind, l.Lang, l.Prefix = ide.Match, file, len(numHead.FindString(l.Text))
		}
	}
}

// source says where the output of cmd comes from: "view" for the text of file, "search" for the
// hits of a search (in file, when it searches one); "" for anything else. Of a command line of
// several commands the last decides, what prints last; of a pipeline, its last command, or the
// one before when that only passes on what it reads.
func source(cmd string) (what, file string) {
	cmds := slices.DeleteFunc(commands(cmd), func(p [][]string) bool { return len(p) == 1 && len(p[0]) == 0 })
	if len(cmds) > 0 {
		pipeline := cmds[len(cmds)-1]
		for i := len(pipeline) - 1; i >= 0; i-- {
			prog, args := program(pipeline[i])
			switch {
			case searchers[prog]:
				file = firstFile(prog, args)
				for j := i - 1; j >= 0 && file == ""; j-- { // a search in what a viewer prints
					if p, a := program(pipeline[j]); viewers[p] {
						file = firstFile(p, a)
					}
				}
				return "search", file
			case prog == "git show":
				for _, a := range args {
					if _, path, ok := strings.Cut(a, ":"); ok && fileName(path) {
						return "view", path
					}
				}
			case viewers[prog]:
				if f := firstFile(prog, args); f != "" {
					return "view", f
				}
				continue // it reads its input: what comes before it decides
			}
			break // the pipeline prints what this command makes: no text of a file, no search
		}
	}
	return "", ""
}

// program is the program a command runs and its arguments, past variable assignments and
// wrappers such as sudo; for git, "git" and its subcommand ("git grep").
func program(words []string) (string, []string) {
	for len(words) > 0 && (strings.Contains(words[0], "=") && !strings.HasPrefix(words[0], "-") ||
		slices.Contains([]string{"sudo", "env", "time", "command", "exec", "nice"}, words[0])) {
		words = words[1:]
	}
	if len(words) == 0 {
		return "", nil
	}
	prog, args := filepath.Base(words[0]), words[1:]
	if prog == "git" {
		for i := 0; i < len(args); i++ {
			switch {
			case args[i] == "-C" || args[i] == "-c":
				i++ // its value
			case !strings.HasPrefix(args[i], "-"):
				return "git " + args[i], args[i+1:]
			}
		}
	}
	return prog, args
}

// firstFile is the first file among the arguments of prog: not a flag nor its value, nor the
// program of sed or awk, nor the pattern of a search, nor what is no file name.
func firstFile(prog string, args []string) string {
	script := prog == "sed" || prog == "awk" || searchers[prog] // their first word is no file
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case slices.Contains(valued[prog], a):
			if a == "-e" || a == "-f" {
				script = false // the script or the pattern, or the file it is in, comes by the flag
			}
			i++
		case strings.HasPrefix(a, "-"):
		case script:
			script = false
		case fileName(a):
			return a
		}
	}
	return ""
}

// fileName reports whether a word of a command line can name a file: it has letters and none of
// what patterns, scripts and redirections have.
func fileName(w string) bool {
	return !strings.ContainsAny(w, " *?{}[]()$<>|;&'\"=,!") && strings.ContainsFunc(w, unicode.IsLetter)
}

// commands splits a command line into its commands (at &&, ||, ; and line breaks), each a
// pipeline of words, without quotes and redirections.
func commands(cmd string) [][][]string {
	var out [][][]string
	var pipeline [][]string
	var words []string
	var w strings.Builder
	inWord, target := false, false // target: the next word is what a redirection goes to or from
	var quote rune
	word := func() {
		if inWord && !target {
			words = append(words, w.String())
		}
		if inWord {
			target = false
		}
		w.Reset()
		inWord = false
	}
	pipe := func() {
		word()
		pipeline = append(pipeline, words)
		words, target = nil, false
	}
	end := func() {
		pipe()
		out = append(out, pipeline)
		pipeline = nil
	}
	rs := []rune(cmd)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote == '"' && r == '\\' && i+1 < len(rs):
			i++
			w.WriteRune(rs[i])
		case quote != 0:
			w.WriteRune(r)
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == '\\' && i+1 < len(rs):
			i++
			w.WriteRune(rs[i])
			inWord = true
		case r == '\n' || r == ';':
			end()
		case r == '&' && i+1 < len(rs) && rs[i+1] == '&', r == '|' && i+1 < len(rs) && rs[i+1] == '|':
			i++
			end()
		case r == '|':
			if i+1 < len(rs) && rs[i+1] == '&' { // |&: stderr too
				i++
			}
			pipe()
		case r == '>' || r == '<': // "> out", "2>&1", "<<'EOF'": the word before, a descriptor, goes too
			if strings.Trim(w.String(), "0123456789&") == "" {
				w.Reset()
				inWord = false
			}
			word()
			for i+1 < len(rs) && strings.ContainsRune("<>&-", rs[i+1]) {
				i++
			}
			target = true
		case unicode.IsSpace(r):
			word()
		default:
			w.WriteRune(r)
			inWord = true
		}
	}
	end()
	return out
}
