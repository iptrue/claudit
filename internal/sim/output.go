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
	hitHead    = regexp.MustCompile(`^((?:[A-Za-z]:[\\/])?[^:\s][^:]*?):(\d+):`) // "path:12:" of a hit, including a Windows drive
	aroundHead = regexp.MustCompile(`^((?:[A-Za-z]:[\\/])?[^:\s][^:]*?)-(\d+)-`) // "path-12-" of a line around one
	numHead    = regexp.MustCompile(`^\d+:`)                                     // "12:" of a hit of a search in one file
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
	markGit(cmd, out)
	return out
}

var (
	statusRow = regexp.MustCompile(`^(?:[MTADRCU?!][ MTADRCU?!m]| [MTADRCU?!m]) \S`) // "XY path" of git status --short; "m" a submodule's
	statRow   = regexp.MustCompile(`^ \S.* \| +\d+ [+-]+$`)                          // " path | 7 ++++++-" of a diffstat
)

// markGit marks, among the plain lines, those of git status --short, for the terminal to colour
// their status as git does, and those of a diffstat, for their "+" and "-". A short status is told
// by the command, a diffstat by its look: git diff, show, log, pull and merge print one.
func markGit(cmd string, out []ide.TermLine) {
	short := slices.ContainsFunc(commands(cmd), func(p [][]string) bool { return slices.ContainsFunc(p, shortStatus) })
	for i := range out {
		switch l := &out[i]; {
		case l.Kind != ide.Output:
		case short && statusRow.MatchString(l.Text):
			l.Kind = ide.Status
		case statRow.MatchString(l.Text):
			l.Kind = ide.Stat
		}
	}
}

// shortStatus reports whether words run git status in its short format: -s, --short, --porcelain.
func shortStatus(words []string) bool {
	prog, args := program(words)
	return prog == "git status" && slices.ContainsFunc(args, func(a string) bool {
		return a == "--short" || a == "--porcelain" || a == "--porcelain=v1" || a == "--porcelain=1" ||
			len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.ContainsRune(a, 's') // -s, -sb
	})
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
// several commands the last decides, what prints last; but when it searches to print no hits (a
// count, names of files), the hits are those of the search before it.
func source(cmd string) (what, file string) {
	cmds := slices.DeleteFunc(commands(cmd), func(p [][]string) bool { return len(p) == 1 && len(p[0]) == 0 })
	if len(cmds) == 0 {
		return "", ""
	}
	what, file = pipelineSource(cmds[len(cmds)-1])
	if what == "search" && hitless(cmds[len(cmds)-1]) {
		for i := len(cmds) - 2; i >= 0; i-- {
			if w, f := pipelineSource(cmds[i]); w == "search" && !hitless(cmds[i]) {
				return w, f
			}
		}
	}
	return what, file
}

// hitless reports whether the last search of pipeline prints no hits: only their count, the names of
// the files with or without them, or nothing.
func hitless(pipeline [][]string) bool {
	for i := len(pipeline) - 1; i >= 0; i-- {
		if prog, args := program(pipeline[i]); searchers[prog] {
			return slices.ContainsFunc(args, func(a string) bool {
				switch a {
				case "--count", "--files-with-matches", "--files-without-match", "--quiet", "--silent":
					return true
				}
				return len(a) > 1 && a[0] == '-' && a[1] != '-' && strings.ContainsAny(a[1:], "clLq") // -c, -lr
			})
		}
	}
	return false
}

// pipelineSource is source for one pipeline: its last command decides, or the one before when
// that only passes on what it reads.
func pipelineSource(pipeline [][]string) (what, file string) {
	for i := len(pipeline) - 1; i >= 0; i-- {
		prog, args := program(pipeline[i])
		switch {
		case searchers[prog]:
			file = firstFile(prog, args)
			for j := i - 1; j >= 0 && file == ""; j-- { // a search in what a viewer prints
				file = shown(program(pipeline[j]))
			}
			return "search", file
		case prog == "git show":
			if f := shown(prog, args); f != "" {
				return "view", f
			}
		case viewers[prog]:
			if f := shown(prog, args); f != "" {
				return "view", f
			}
			continue // it reads its input: what comes before it decides
		}
		break // the pipeline prints what this command makes: no text of a file, no search
	}
	return "", ""
}

// shown is the file prog prints the text of: a viewer's first file, or the path of git show's
// "rev:path"; "" when it prints none.
func shown(prog string, args []string) string {
	if prog == "git show" {
		for _, a := range args {
			if _, path, ok := strings.Cut(a, ":"); ok && !strings.HasPrefix(a, "-") && fileName(path) { // not --format=%h:%s
				return path
			}
		}
		return ""
	}
	if viewers[prog] {
		return firstFile(prog, args)
	}
	return ""
}

var (
	hereDoc  = regexp.MustCompile(`<<-?\s*['"]?([A-Za-z_]\w*)['"]?`)           // <<EOF, <<-EOF, <<'EOF'
	redirect = regexp.MustCompile(`(?:^|[^<>&0-9])>>?\s*['"]?([^\s'"&|;<>]+)`) // "> file", ">> file"
)

// interpreters are the programs that run a script read from their input, by the file name of its
// language (python3.12 and the like by their prefix).
var interpreters = map[string]string{
	"node": ".js", "deno": ".js", "bun": ".js", "ruby": ".rb", "php": ".php", "lua": ".lua",
	"sh": ".sh", "bash": ".sh", "zsh": ".sh", "psql": ".sql", "sqlite3": ".sql", "mysql": ".sql",
}

// lineLangs is the language of each line of cmd as a file name langOf knows: "" for the shell's;
// for the body of a here-document, that of what it feeds: an interpreter's script, the file cat or
// tee writes.
func lineLangs(cmd string) []string {
	lines := strings.Split(cmd, "\n")
	langs := make([]string, len(lines))
	for i := 0; i < len(lines); i++ {
		m := hereDoc.FindStringSubmatchIndex(lines[i])
		if m == nil {
			continue
		}
		end, lang := lines[i][m[2]:m[3]], fed(lines[i], m[0])
		for i++; i < len(lines) && strings.TrimSpace(lines[i]) != end; i++ {
			langs[i] = lang
		}
	}
	return langs
}

// fed is what the here-document that starts at byte at of line feeds, as a file name; "" for what
// has no language.
func fed(line string, at int) string {
	cmds := commands(line[:at])
	p := cmds[len(cmds)-1]
	prog, args := program(p[len(p)-1])
	switch {
	case prog == "cat":
		if m := redirect.FindStringSubmatch(line); m != nil {
			return m[1]
		}
	case prog == "tee":
		return firstFile(prog, args)
	case strings.HasPrefix(prog, "python"):
		return ".py"
	}
	return interpreters[prog]
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
	return !strings.ContainsAny(w, "*?{}[]()$<>|;&'\"=,!") && strings.ContainsFunc(w, unicode.IsLetter)
}

// commands splits a command line into its commands (at &&, ||, ; and line breaks), each a
// pipeline of words, without quotes and redirections.
func commands(cmd string) [][][]string {
	var out [][][]string
	var pipeline [][]string
	var words []string
	var w strings.Builder
	inWord, target := false, false // target: the next word is what a redirection goes to or from
	windowsPath := false           // absolute drive and UNC paths keep their separators
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
		windowsPath = false
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
		if w.Len() == 0 && i+2 < len(rs) {
			windowsPath = ('A' <= r && r <= 'Z' || 'a' <= r && r <= 'z') && rs[i+1] == ':' && (rs[i+2] == '\\' || rs[i+2] == '/') ||
				r == '\\' && rs[i+1] == '\\' && rs[i+2] != '\\'
		}
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote == '"' && r == '\\' && !windowsPath && i+1 < len(rs):
			i++
			w.WriteRune(rs[i])
		case quote != 0:
			w.WriteRune(r)
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == '\\' && !windowsPath && i+1 < len(rs):
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
