// Package openspec finds the OpenSpec changes and specs in the folders under a root: the copies
// of a specs repository a developer keeps side by side, a card's own copies "specs-DEV-<N>" among
// them. It reads only names, times and a few small files, within a depth, an entry count and a
// time, so that a root with millions of files below it costs as little as one with a few.
package openspec

import (
	"bufio"
	"cmp"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Limits bound a scan.
type Limits struct {
	Depth      int           // how deep below the root an openspec folder may be: 1 for the root's own
	Entries    int           // directory entries and files looked at, at most
	DirEntries int           // entries of one folder a copy is looked for in, at most: a bigger one is left out; 0 for any
	Time       time.Duration // how long it may take
}

// Default limits: the copies in the root, beside it in a folder, or in a folder of such folders.
var Default = Limits{Depth: 3, Entries: 200_000, DirEntries: 5_000, Time: 5 * time.Second}

// Copy is a folder that holds an openspec folder: a copy of a specs repository.
type Copy struct {
	Root string // the folder
	Name string // its name
	Card string // the number of a card's own copy, "119816" of "specs-DEV-119816-retry"; "" for another
	// Branch is the branch the copy has checked out, read off its .git/HEAD, for a copy tried as the
	// main one: "main", a card's branch, or the short hash of a detached HEAD; "" when it is not known.
	Branch string
}

// Op counts the requirements of a delta by what the change does to them.
type Op int

const (
	Added Op = iota
	Modified
	Removed
	Renamed
)

// Ops are the names of the sections of a delta, by Op.
var Ops = [...]string{"ADDED", "MODIFIED", "REMOVED", "RENAMED"}

// Delta is a change's spec of a capability: the requirements it adds, modifies, removes, renames.
type Delta struct {
	Capability string // "webinar/record-conversion"
	Path       string
	Lines      int
	Ops        [len(Ops)]int
}

// Doc is a Markdown file of a change: proposal.md, design.md, tasks.md and the like.
type Doc struct {
	Name  string
	Path  string
	Lines int
}

// Change is a change, as one copy has it.
type Change struct {
	Name    string
	Tracker string // what "Tracker:" of its proposal names, "DEV-119816"; "" for none
	Own     bool   // a card's own copy has it: the copy shown
	Copy    Copy   // the copy shown
	Dir     string // its folder in that copy
	Mod     time.Time
	More    int // the other own copies that have it, when Own
	Deltas  []Delta
	Docs    []Doc
}

// Spec is a capability of the main copy.
type Spec struct {
	Capability string // "chats/messages"
	Path       string
}

// Index is what a scan found.
type Index struct {
	Root    string
	Copies  int
	Changes []Change // the own ones first, newest edit first; then the others, by name
	Main    Copy     // the copy the specs, and the changes no own copy has, come from, with its Branch; Root "" for none
	Specs   []Spec   // by capability
	Partial string   // why the scan stopped short, "stopped after 3s"; "" when it did not
}

// Matches reports whether q, in any case, is in the name of c, in its tracker id, or in the
// "domain/capability" of one of its deltas.
func (c *Change) Matches(q string) bool {
	q = strings.ToLower(strings.TrimSpace(q))
	return strings.Contains(strings.ToLower(c.Name), q) || q != "" && (strings.Contains(strings.ToLower(c.Tracker), q) ||
		slices.ContainsFunc(c.Deltas, func(d Delta) bool { return strings.Contains(strings.ToLower(d.Capability), q) }))
}

// Matches reports whether q, in any case, is in the capability of s.
func (s *Spec) Matches(q string) bool {
	return strings.Contains(strings.ToLower(s.Capability), strings.ToLower(strings.TrimSpace(q)))
}

// skipped are folders no copy is looked for in: tools' and builds' trees.
var skipped = map[string]bool{
	"node_modules": true, "vendor": true, "target": true, "dist": true, "build": true, "__pycache__": true,
	"Library": true, "openspec": true,
}

// ownCopy is the name of a card's own copy: "specs-DEV-119816", "specs-DEV-119466-create-first".
var ownCopy = regexp.MustCompile(`(?i)^specs-[a-z]+-(\d+)(?:-.*)?$`)

// Scan looks for the copies under root within lim and reads their changes and the main specs.
func Scan(root string, lim Limits) Index {
	s := &scanner{lim: lim, deadline: time.Now().Add(lim.Time)}
	root = filepath.Clean(root)
	ix := Index{Root: root}
	copies := s.copies(root)
	ix.Copies = len(copies)
	ix.Main, ix.Specs = s.main(copies)
	ix.Changes = s.changes(copies, ix.Main)
	ix.Partial = s.stop
	return ix
}

// scanner counts what a scan looks at and stops it past its limits.
type scanner struct {
	lim      Limits
	deadline time.Time
	spent    int
	stop     string
}

// spend counts n entries and reports whether the scan may go on.
func (s *scanner) spend(n int) bool {
	if s.stop != "" {
		return false
	}
	s.spent += n
	switch {
	case s.spent > s.lim.Entries:
		s.stop = fmt.Sprintf("stopped after %d entries", s.lim.Entries)
	case time.Now().After(s.deadline):
		s.stop = "stopped after " + s.lim.Time.String()
	}
	return s.stop == ""
}

// readDir lists dir by name, as far as the limits let it. With limit > 0, a folder of more entries
// than that is not listed at all: the ones read to find that out are counted, no more.
func (s *scanner) readDir(dir string, limit int) []os.DirEntry {
	if !s.spend(1) {
		return nil
	}
	f, err := os.Open(dir)
	if err != nil {
		return nil
	}
	defer f.Close()
	var all []os.DirEntry
	for {
		es, err := f.ReadDir(256)
		all = append(all, es...)
		more := s.spend(len(es))
		if limit > 0 && len(all) > limit {
			return nil
		}
		if err != nil || !more {
			break
		}
	}
	slices.SortFunc(all, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return all
}

// isDir reports whether path is a folder: a link to one is not.
func (s *scanner) isDir(path string) bool {
	if !s.spend(1) {
		return false
	}
	fi, err := os.Lstat(path)
	return err == nil && fi.IsDir()
}

// copies are the folders with an openspec folder within the depth of the limits, shallower first,
// by name: neither hidden folders nor skipped ones, nor links, are looked in.
func (s *scanner) copies(root string) []Copy {
	var out []Copy
	level := []string{root}
	for depth := 0; depth < s.lim.Depth && len(level) > 0; depth++ {
		var next []string
		for _, dir := range level {
			if s.isDir(filepath.Join(dir, "openspec")) {
				c := Copy{Root: dir, Name: filepath.Base(dir)}
				if m := ownCopy.FindStringSubmatch(c.Name); m != nil {
					c.Card = m[1]
				}
				out = append(out, c)
			}
			if depth == s.lim.Depth-1 {
				continue
			}
			for _, e := range s.readDir(dir, s.lim.DirEntries) {
				if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && !skipped[e.Name()] {
					next = append(next, filepath.Join(dir, e.Name()))
				}
			}
		}
		level = next
	}
	return out
}

// main is the copy of the main branch, and its specs: of the copies that are not a card's own, or
// of all when there are none such, the ones on main or master when there are any; of those, the one
// whose specs were changed last, by name when they tie. Its Branch says which branch it is on.
func (s *scanner) main(copies []Copy) (Copy, []Spec) {
	cands := slices.DeleteFunc(slices.Clone(copies), func(c Copy) bool { return c.Card != "" })
	if len(cands) == 0 {
		cands = slices.Clone(copies)
	}
	for i := range cands {
		cands[i].Branch = s.branch(cands[i].Root)
	}
	if onMain := slices.DeleteFunc(slices.Clone(cands), func(c Copy) bool { return c.Branch != "main" && c.Branch != "master" }); len(onMain) > 0 {
		cands = onMain
	}
	var best Copy
	var bestSpecs []Spec
	var bestMod time.Time
	for _, c := range cands {
		specs, mod := s.specs(c)
		if best.Root == "" || mod.After(bestMod) || mod.Equal(bestMod) && c.Name < best.Name {
			best, bestSpecs, bestMod = c, specs, mod
		}
	}
	return best, bestSpecs
}

// branch is the branch the repository in dir has checked out, as .git/HEAD in it says, or in the
// folder a worktree's .git file points at: the name of a branch, the short hash of a detached HEAD;
// "" for none or for what it cannot make out. Git is not run.
func (s *scanner) branch(dir string) string {
	git := filepath.Join(dir, ".git")
	fi, err := os.Lstat(git)
	if !s.spend(1) || err != nil {
		return ""
	}
	if !fi.IsDir() { // a worktree: "gitdir: <path>"
		link, ok := strings.CutPrefix(s.readHead(git), "gitdir:")
		if !ok {
			return ""
		}
		git = strings.TrimSpace(link)
		if !filepath.IsAbs(git) {
			git = filepath.Join(dir, git)
		}
	}
	head := s.readHead(filepath.Join(git, "HEAD"))
	if ref, ok := strings.CutPrefix(head, "ref:"); ok {
		ref = strings.TrimSpace(ref)
		return strings.TrimPrefix(ref, "refs/heads/")
	}
	if len(head) >= 7 && strings.Trim(head, "0123456789abcdef") == "" {
		return head[:7]
	}
	return ""
}

// readHead is the first line of a small file, "" for none.
func (s *scanner) readHead(path string) string {
	text, ok := s.readN(path, 512)
	if !ok {
		return ""
	}
	line, _, _ := strings.Cut(text, "\n")
	return strings.TrimSpace(line)
}

// specs are the capabilities of copy c, openspec/specs/<domain>/<capability>/spec.md, by name,
// and when the newest of them was changed.
func (s *scanner) specs(c Copy) ([]Spec, time.Time) {
	base := filepath.Join(c.Root, "openspec", "specs")
	var out []Spec
	var mod time.Time
	s.walk(base, 4, func(path string, fi os.FileInfo) {
		if fi.Name() != "spec.md" {
			return
		}
		rel, _ := filepath.Rel(base, filepath.Dir(path))
		if rel == "." {
			return
		}
		out = append(out, Spec{Capability: filepath.ToSlash(rel), Path: path})
		if fi.ModTime().After(mod) {
			mod = fi.ModTime()
		}
	})
	slices.SortFunc(out, func(a, b Spec) int { return strings.Compare(a.Capability, b.Capability) })
	return out, mod
}

// walk calls file for each regular file in dir and in its folders depth deep, by name; hidden ones
// and links are left out.
func (s *scanner) walk(dir string, depth int, file func(path string, fi os.FileInfo)) {
	for _, e := range s.readDir(dir, 0) {
		path := filepath.Join(dir, e.Name())
		switch {
		case strings.HasPrefix(e.Name(), "."):
		case e.IsDir():
			if depth > 1 {
				s.walk(path, depth-1, file)
			}
		case e.Type().IsRegular():
			if !s.spend(1) {
				return
			}
			if fi, err := e.Info(); err == nil {
				file(path, fi)
			}
		}
	}
}

// found is a change as one copy has it.
type found struct {
	copy    Copy
	dir     string
	dirMod  time.Time
	tracker string
	read    bool // tracker was read
	own     bool
}

// changes reads the changes of the copies, archive aside, one per name: from the own copy changed
// last when there are such, else from the main copy, else from the copy whose folder of it is the
// newest, by name when they tie.
func (s *scanner) changes(copies []Copy, main Copy) []Change {
	byName := map[string][]*found{}
	var names []string
	for _, c := range copies {
		for _, e := range s.readDir(filepath.Join(c.Root, "openspec", "changes"), 0) {
			if !e.IsDir() || e.Name() == "archive" || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			f := &found{copy: c, dir: filepath.Join(c.Root, "openspec", "changes", e.Name())}
			if s.spend(1) {
				if fi, err := e.Info(); err == nil {
					f.dirMod = fi.ModTime()
				}
			}
			if c.Card != "" {
				f.own = hasNumber(e.Name(), c.Card)
				if !f.own {
					f.tracker, f.read = s.tracker(f.dir), true
					f.own = hasNumber(f.tracker, c.Card)
				}
			}
			if byName[e.Name()] == nil {
				names = append(names, e.Name())
			}
			byName[e.Name()] = append(byName[e.Name()], f)
		}
	}
	var out []Change
	for _, name := range names {
		all := byName[name]
		var pick *found
		var files []fileInfo
		var mod time.Time
		own := slices.DeleteFunc(slices.Clone(all), func(f *found) bool { return !f.own })
		for _, f := range own { // the newest edit of the card's own copies
			fs, m := s.files(f.dir)
			if pick == nil || m.After(mod) || m.Equal(mod) && f.copy.Name < pick.copy.Name {
				pick, files, mod = f, fs, m
			}
		}
		if pick == nil {
			pick = all[0]
			for _, f := range all {
				switch {
				case pick.copy.Root == main.Root:
				case f.copy.Root == main.Root, f.dirMod.After(pick.dirMod),
					f.dirMod.Equal(pick.dirMod) && f.copy.Name < pick.copy.Name:
					pick = f
				}
			}
			files, mod = s.files(pick.dir)
		}
		if !pick.read {
			pick.tracker = s.tracker(pick.dir)
		}
		c := Change{Name: name, Tracker: pick.tracker, Own: pick.own, Copy: pick.copy, Dir: pick.dir, Mod: mod}
		if c.Own {
			c.More = len(own) - 1
		}
		c.Deltas, c.Docs = s.details(pick.dir, files)
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b Change) int {
		switch {
		case a.Own != b.Own:
			if a.Own {
				return -1
			}
			return 1
		case a.Own && !a.Mod.Equal(b.Mod):
			return b.Mod.Compare(a.Mod)
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

type fileInfo struct {
	path string
	rel  string // to the change's folder, with "/"
}

// files are the files of the change in dir, and when the newest of them was changed.
func (s *scanner) files(dir string) ([]fileInfo, time.Time) {
	var out []fileInfo
	var mod time.Time
	s.walk(dir, 5, func(path string, fi os.FileInfo) {
		rel, _ := filepath.Rel(dir, path)
		out = append(out, fileInfo{path: path, rel: filepath.ToSlash(rel)})
		if fi.ModTime().After(mod) {
			mod = fi.ModTime()
		}
	})
	return out, mod
}

// docOrder puts a change's own files first, in the order they are written in.
var docOrder = []string{"proposal.md", "design.md", "tasks.md"}

// details are the deltas of the change in dir, specs/<domain>/<capability>/spec.md by capability,
// and its Markdown files: proposal, design and tasks first, then the others by name.
func (s *scanner) details(dir string, files []fileInfo) ([]Delta, []Doc) {
	var deltas []Delta
	var docs []Doc
	for _, f := range files {
		switch {
		case strings.HasPrefix(f.rel, "specs/") && strings.HasSuffix(f.rel, "/spec.md"):
			d := Delta{Capability: strings.TrimSuffix(strings.TrimPrefix(f.rel, "specs/"), "/spec.md"), Path: f.path}
			if text, ok := s.read(f.path); ok {
				d.Lines, d.Ops = lines(text), countOps(text)
			}
			deltas = append(deltas, d)
		case !strings.Contains(f.rel, "/") && strings.EqualFold(filepath.Ext(f.rel), ".md"):
			d := Doc{Name: f.rel, Path: f.path}
			if text, ok := s.read(f.path); ok {
				d.Lines = lines(text)
			}
			docs = append(docs, d)
		}
	}
	slices.SortFunc(deltas, func(a, b Delta) int { return strings.Compare(a.Capability, b.Capability) })
	rank := func(name string) int {
		if i := slices.Index(docOrder, name); i >= 0 {
			return i
		}
		return len(docOrder)
	}
	slices.SortFunc(docs, func(a, b Doc) int { return cmp.Or(rank(a.Name)-rank(b.Name), strings.Compare(a.Name, b.Name)) })
	return deltas, docs
}

// maxRead is the most of a file read: specs are far shorter.
const maxRead = 4 << 20

// read is the text of the file at path, as far as maxRead.
func (s *scanner) read(path string) (string, bool) { return s.readN(path, maxRead) }

// readN is the text of the file at path, n bytes of it at most.
func (s *scanner) readN(path string, n int64) (string, bool) {
	if !s.spend(1) {
		return "", false
	}
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, n))
	return string(b), err == nil
}

// tracker is what the line "Tracker: <id>" of the proposal of the change in dir names; "" for none.
func (s *scanner) tracker(dir string) string {
	if !s.spend(1) {
		return ""
	}
	f, err := os.Open(filepath.Join(dir, "proposal.md"))
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(io.LimitReader(f, 64<<10))
	for sc.Scan() {
		line := strings.TrimLeft(sc.Text(), "-*> \t")
		if len(line) < 8 || !strings.EqualFold(line[:8], "tracker:") {
			continue
		}
		if fs := strings.Fields(strings.Trim(line[8:], "* \t")); len(fs) > 0 {
			return strings.Trim(fs[0], "*`[]()<>,.;")
		}
	}
	return ""
}

// digits are the numbers in a name or an id.
var digits = regexp.MustCompile(`\d+`)

// hasNumber reports whether n is one of the numbers in s: "119816" is in "dev-119816-chat", not in
// "dev-1198160".
func hasNumber(s, n string) bool {
	return n != "" && slices.Contains(digits.FindAllString(s, -1), n)
}

// lines counts the lines of text, the last one without its newline too.
func lines(text string) int {
	n := strings.Count(text, "\n")
	if text != "" && !strings.HasSuffix(text, "\n") {
		n++
	}
	return n
}

// countOps counts the requirements of a delta under its "## ADDED Requirements" and the like: a
// "### Requirement:" each, a "FROM:" each under RENAMED; code blocks aside.
func countOps(text string) (ops [len(Ops)]int) {
	op, fence := -1, ""
	for line := range strings.Lines(text) {
		t := strings.TrimSpace(line)
		switch {
		case fence != "":
			if strings.HasPrefix(t, fence) {
				fence = ""
			}
		case strings.HasPrefix(t, "```"), strings.HasPrefix(t, "~~~"):
			fence = t[:3]
		case strings.HasPrefix(t, "## "):
			f := strings.Fields(t[3:])
			op = -1
			if len(f) > 0 {
				op = slices.Index(Ops[:], strings.ToUpper(f[0]))
			}
		case op == int(Renamed):
			if strings.HasPrefix(strings.TrimLeft(t, "-* "), "FROM:") {
				ops[op]++
			}
		case op >= 0 && strings.HasPrefix(t, "### Requirement"):
			ops[op]++
		}
	}
	return ops
}
