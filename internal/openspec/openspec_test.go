package openspec

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

// write puts text in the file at root/rel, changed at the time at.
func write(t *testing.T, root, rel, text string, at time.Time) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
	return path
}

// touch sets when the folder at root/rel was changed.
func touch(t *testing.T, root, rel string, at time.Time) {
	t.Helper()
	if err := os.Chtimes(filepath.Join(root, filepath.FromSlash(rel)), at, at); err != nil {
		t.Fatal(err)
	}
}

const delta = `# Delta

## ADDED Requirements

### Requirement: One
text

` + "```md\n### Requirement: In a code block\n```" + `

### Requirement: Two

## MODIFIED Requirements

### Requirement: Three

## REMOVED Requirements

### Requirement: Four

## RENAMED Requirements

- FROM: ` + "`### Requirement: Five`" + `
- TO: ` + "`### Requirement: Six`" + `
`

// tree is a folder of copies of a specs repository, as a developer keeps them.
func tree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	main := "workspace-specs/openspec/"
	write(t, root, main+"specs/chats/messages/spec.md", "# Messages\n", t0.Add(-48*time.Hour))
	write(t, root, main+"specs/webinar/record/spec.md", "# Record\n", t0.Add(-48*time.Hour))
	write(t, root, main+"specs/spec.md", "a spec of no capability\n", t0)
	write(t, root, main+"changes/archive/2026-01-01-old/proposal.md", "old\n", t0)
	write(t, root, main+"changes/chats-17616-system-messages/proposal.md", "# Proposal\n\nTracker: CHATS-17616\n", t0.Add(-72*time.Hour))
	write(t, root, main+"changes/chats-17616-system-messages/specs/chats/messages/spec.md", delta, t0.Add(-72*time.Hour))
	write(t, root, main+"changes/dev-100-alpha/proposal.md", "main's copy\n", t0.Add(-96*time.Hour))
	write(t, root, main+"changes/notes.md", "not a change\n", t0)

	// A card's own copy and a retry of it: the retry was edited last.
	own := "_wt/specs-DEV-100/openspec/changes/dev-100-alpha/"
	write(t, root, own+"proposal.md", "Tracker: DEV-100\n", t0.Add(-3*time.Hour))
	retry := "_wt/specs-DEV-100-retry/openspec/changes/dev-100-alpha/"
	write(t, root, retry+"proposal.md", "# Proposal\n\nTracker: DEV-100\n\nWhy.\n", t0.Add(-5*time.Hour))
	write(t, root, retry+"design.md", "a\nb", t0.Add(-5*time.Hour))
	write(t, root, retry+"tasks.md", "- [ ] one\n", t0.Add(-5*time.Hour))
	write(t, root, retry+"zz-notes.md", "x\n", t0.Add(-5*time.Hour))
	write(t, root, retry+"README.txt", "not Markdown\n", t0.Add(-5*time.Hour))
	write(t, root, retry+"specs/webinar/record/spec.md", delta, t0.Add(-time.Hour))
	write(t, root, retry+"specs/chats/messages/spec.md", "## ADDED Requirements\n### Requirement: A\n", t0.Add(-5*time.Hour))
	// The own copies have the main copy's changes too: those are not theirs.
	write(t, root, "_wt/specs-DEV-100/openspec/changes/chats-17616-system-messages/proposal.md", "Tracker: CHATS-17616\n", t0)

	// A change of card 200 whose name has no number: its tracker tells.
	write(t, root, "_wt/specs-DEV-200/openspec/changes/odd-name/proposal.md", "**Tracker:** DEV-200\n", t0.Add(-2*time.Hour))
	// A number that only starts like the card's is not it.
	write(t, root, "_wt/specs-DEV-200/openspec/changes/dev-2000-other/proposal.md", "Tracker: DEV-2000\n", t0)

	// A change no own copy and not the main copy has: from the copy whose folder of it is the newest.
	write(t, root, "_wt/specs-DEV-300/openspec/changes/colleague-change/proposal.md", "Tracker: CHATS-9\n", t0)
	touch(t, root, "_wt/specs-DEV-300/openspec/changes/colleague-change", t0.Add(-10*time.Hour))
	write(t, root, "_wt/specs-DEV-400/openspec/changes/colleague-change/proposal.md", "Tracker: CHATS-9\n", t0)
	touch(t, root, "_wt/specs-DEV-400/openspec/changes/colleague-change", t0.Add(-9*time.Hour))

	// Where no copy is looked for: hidden, tools' trees, too deep.
	write(t, root, ".hidden/openspec/changes/hidden-change/proposal.md", "x\n", t0)
	write(t, root, "node_modules/pkg/openspec/changes/module-change/proposal.md", "x\n", t0)
	write(t, root, "a/b/c/openspec/changes/too-deep/proposal.md", "x\n", t0)
	write(t, root, "a/b/openspec/changes/deep-enough/proposal.md", "x\n", t0)
	return root
}

func names(cs []Change) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}

// One row per change: the own ones first, newest edit first, then the others by name.
func TestScan(t *testing.T) {
	root := tree(t)
	ix := Scan(root, Default)
	if ix.Partial != "" || ix.Root != root {
		t.Fatalf("partial %q root %q", ix.Partial, ix.Root)
	}
	want := []string{"dev-100-alpha", "odd-name", "chats-17616-system-messages", "colleague-change", "deep-enough", "dev-2000-other"}
	if got := names(ix.Changes); !reflect.DeepEqual(got, want) {
		t.Fatalf("changes %q, want %q", got, want)
	}
	if ix.Copies != 7 {
		t.Errorf("copies %d, want 7", ix.Copies)
	}
	byName := map[string]Change{}
	for _, c := range ix.Changes {
		byName[c.Name] = c
	}

	alpha := byName["dev-100-alpha"]
	if !alpha.Own || alpha.Copy.Name != "specs-DEV-100-retry" || alpha.Copy.Card != "100" || alpha.More != 1 {
		t.Errorf("alpha from %+v own %v more %d", alpha.Copy, alpha.Own, alpha.More)
	}
	if !alpha.Mod.Equal(t0.Add(-time.Hour)) || alpha.Tracker != "DEV-100" {
		t.Errorf("alpha mod %v tracker %q", alpha.Mod, alpha.Tracker)
	}
	if alpha.Dir != filepath.Join(root, "_wt/specs-DEV-100-retry/openspec/changes/dev-100-alpha") {
		t.Errorf("alpha dir %q", alpha.Dir)
	}
	var docs []string
	for _, d := range alpha.Docs {
		docs = append(docs, d.Name+":"+strconv.Itoa(d.Lines))
	}
	if want := []string{"proposal.md:5", "design.md:2", "tasks.md:1", "zz-notes.md:1"}; !reflect.DeepEqual(docs, want) {
		t.Errorf("docs %q, want %q", docs, want)
	}
	if len(alpha.Deltas) != 2 || alpha.Deltas[0].Capability != "chats/messages" || alpha.Deltas[1].Capability != "webinar/record" {
		t.Fatalf("deltas %+v", alpha.Deltas)
	}
	if d := alpha.Deltas[1]; d.Ops != [4]int{2, 1, 1, 1} || d.Lines != strings.Count(delta, "\n") || !strings.HasSuffix(d.Path, "webinar/record/spec.md") {
		t.Errorf("delta %+v", d)
	}

	if odd := byName["odd-name"]; !odd.Own || odd.Tracker != "DEV-200" || odd.Copy.Name != "specs-DEV-200" {
		t.Errorf("odd-name: %+v", odd)
	}
	if other := byName["dev-2000-other"]; other.Own {
		t.Error("card 2000 taken for card 200")
	}
	chats := byName["chats-17616-system-messages"]
	if chats.Own || chats.Copy.Name != "workspace-specs" || chats.Tracker != "CHATS-17616" || len(chats.Deltas) != 1 || chats.Deltas[0].Ops[Added] != 2 {
		t.Errorf("chats: %+v", chats)
	}
	if c := byName["colleague-change"]; c.Own || c.Copy.Name != "specs-DEV-400" {
		t.Errorf("colleague-change from %q", c.Copy.Name)
	}

	if ix.Main.Name != "workspace-specs" {
		t.Errorf("main %q", ix.Main.Name)
	}
	var caps []string
	for _, s := range ix.Specs {
		caps = append(caps, s.Capability)
	}
	if want := []string{"chats/messages", "webinar/record"}; !reflect.DeepEqual(caps, want) {
		t.Errorf("specs %q, want %q", caps, want)
	}
}

// Of several copies that are not a card's own, the main one is the one whose specs were changed
// last, by name when they tie; with none such, all copies are candidates.
func TestScanMain(t *testing.T) {
	root := t.TempDir()
	write(t, root, "old-specs/openspec/specs/a/spec.md", "a\n", t0.Add(-time.Hour))
	write(t, root, "new-specs/openspec/specs/a/spec.md", "a\n", t0)
	write(t, root, "specs-DEV-1/openspec/specs/a/spec.md", "a\n", t0.Add(time.Hour))
	if ix := Scan(root, Default); ix.Main.Name != "new-specs" || len(ix.Specs) != 1 {
		t.Errorf("main %q specs %d", ix.Main.Name, len(ix.Specs))
	}
	write(t, root, "old-specs/openspec/specs/a/spec.md", "a\n", t0)
	if ix := Scan(root, Default); ix.Main.Name != "new-specs" {
		t.Errorf("a tie: main %q, want the first by name", ix.Main.Name)
	}

	only := t.TempDir()
	write(t, only, "specs-DEV-1/openspec/specs/a/spec.md", "a\n", t0)
	write(t, only, "specs-DEV-2/openspec/specs/b/spec.md", "b\n", t0.Add(time.Hour))
	if ix := Scan(only, Default); ix.Main.Name != "specs-DEV-2" || ix.Specs[0].Capability != "b" {
		t.Errorf("own copies only: main %q", ix.Main.Name)
	}

	// The root is a copy itself.
	self := t.TempDir()
	write(t, self, "openspec/changes/x/proposal.md", "x\n", t0)
	if ix := Scan(self, Default); len(ix.Changes) != 1 || ix.Main.Root != self {
		t.Errorf("the root's own: %+v", ix)
	}
	if ix := Scan(t.TempDir(), Default); ix.Main.Root != "" || ix.Changes != nil || ix.Specs != nil || ix.Partial != "" {
		t.Errorf("no copy: %+v", ix)
	}
}

// A scan stops at its limits and says so.
func TestScanLimits(t *testing.T) {
	root := tree(t)
	if ix := Scan(root, Limits{Depth: 3, Entries: 10, Time: time.Minute}); ix.Partial != "stopped after 10 entries" {
		t.Errorf("entries: partial %q", ix.Partial)
	}
	if ix := Scan(root, Limits{Depth: 3, Entries: 1 << 20, Time: -time.Second}); !strings.HasPrefix(ix.Partial, "stopped after ") || len(ix.Changes) != 0 {
		t.Errorf("time: partial %q, %d changes", ix.Partial, len(ix.Changes))
	}
	if ix := Scan(root, Limits{Depth: 2, Entries: 1 << 20, Time: time.Minute}); ix.Copies != 1 || ix.Main.Name != "workspace-specs" {
		t.Errorf("depth 2: %d copies", ix.Copies)
	}
}

// A link to a folder is not followed: it may lead around in circles, or off the machine.
func TestScanLinks(t *testing.T) {
	root := t.TempDir()
	write(t, root, "elsewhere/specs/openspec/changes/linked/proposal.md", "x\n", t0)
	if err := os.Symlink(filepath.Join(root, "elsewhere"), filepath.Join(root, "link")); err != nil {
		t.Skip("no links here:", err)
	}
	ix := Scan(root, Default)
	if ix.Copies != 1 || ix.Changes[0].Copy.Root != filepath.Join(root, "elsewhere", "specs") {
		t.Errorf("copies %d: %+v", ix.Copies, ix.Changes)
	}
}

func TestMatches(t *testing.T) {
	c := Change{Name: "chats-17616-system-messages", Tracker: "CHATS-17616"}
	d := Change{Name: "odd-name", Tracker: "DEV-119816"}
	for _, q := range []string{"", "17616", "System", " chats-176 "} {
		if !c.Matches(q) {
			t.Errorf("%q does not find %s", q, c.Name)
		}
	}
	if !d.Matches("119816") || !d.Matches("dev-1198") || d.Matches("chats") {
		t.Error("by tracker")
	}
	// By the domain/capability of a delta, whatever the change is called.
	e := Change{Name: "odd-name", Deltas: []Delta{{Capability: "webinar/record-conversion"}, {Capability: "chats/messages"}}}
	for _, q := range []string{"record-conversion", "Webinar/Record", " chats/mess "} {
		if !e.Matches(q) {
			t.Errorf("%q does not find a change by its delta", q)
		}
	}
	if e.Matches("encoder") || (&Change{Name: "x"}).Matches("record") {
		t.Error("a delta found for what it is not")
	}
	s := Spec{Capability: "webinar/record-conversion"}
	if !s.Matches("WEBINAR/rec") || !s.Matches("conversion") || s.Matches("chats") {
		t.Error("spec by capability")
	}
}

func TestHasNumber(t *testing.T) {
	for _, c := range []struct {
		s, n string
		want bool
	}{
		{"dev-119816-chat", "119816", true}, {"dev-1198160", "119816", false}, {"x119816", "119816", true},
		{"DEV-119816", "119816", true}, {"dev-11981", "119816", false}, {"anything", "", false},
	} {
		if got := hasNumber(c.s, c.n); got != c.want {
			t.Errorf("hasNumber(%q, %q) = %v", c.s, c.n, got)
		}
	}
}

func TestLines(t *testing.T) {
	for text, want := range map[string]int{"": 0, "a": 1, "a\n": 1, "a\nb": 2, "a\n\n": 2} {
		if got := lines(text); got != want {
			t.Errorf("lines(%q) = %d, want %d", text, got, want)
		}
	}
}

func TestCountOps(t *testing.T) {
	text := "## ADDED Requirements\n### Requirement: A\n## Purpose\n### Requirement: not under an op\n~~~\n## MODIFIED\n~~~\n### Requirement: B\n"
	if got := countOps(text); got != [4]int{1, 0, 0, 0} {
		t.Errorf("ops %v", got)
	}
	if got := countOps(delta); got != [4]int{2, 1, 1, 1} {
		t.Errorf("delta ops %v", got)
	}
}

// gitDir makes root/rel a repository whose HEAD says head.
func gitDir(t *testing.T, root, rel, head string) {
	t.Helper()
	write(t, root, rel+"/.git/HEAD", head, t0)
}

// The main copy is the one on main or master, among the copies of no card, however the specs of
// the others were changed; the header can say which branch it is on, a worktree's and a detached
// HEAD's included. Without such a copy the specs changed last win, and the branch is still told.
func TestScanMainBranch(t *testing.T) {
	root := t.TempDir()
	write(t, root, "old-main/openspec/specs/a/spec.md", "a\n", t0.Add(-time.Hour))
	gitDir(t, root, "old-main", "ref: refs/heads/main\n")
	write(t, root, "fresh-feature/openspec/specs/a/spec.md", "a\n", t0)
	gitDir(t, root, "fresh-feature", "ref: refs/heads/DEV-119462\n")
	write(t, root, "specs-DEV-7/openspec/specs/a/spec.md", "a\n", t0.Add(time.Hour))
	gitDir(t, root, "specs-DEV-7", "ref: refs/heads/main\n") // a card's own copy: never the main one while others are
	ix := Scan(root, Default)
	if ix.Main.Name != "old-main" || ix.Main.Branch != "main" {
		t.Errorf("main %q on %q, want old-main on main", ix.Main.Name, ix.Main.Branch)
	}
	gitDir(t, root, "fresh-feature", "ref: refs/heads/master\n")
	write(t, root, "old-main/openspec/specs/a/spec.md", "a\n", t0.Add(2*time.Hour))
	if ix := Scan(root, Default); ix.Main.Name != "old-main" {
		t.Errorf("two on main or master: %q, want the one changed last", ix.Main.Name)
	}

	feature := t.TempDir() // no copy on main: the specs changed last, and its branch
	write(t, feature, "a/openspec/specs/x/spec.md", "x\n", t0.Add(-time.Hour))
	gitDir(t, feature, "a", "ref: refs/heads/DEV-1\n")
	write(t, feature, "b/openspec/specs/x/spec.md", "x\n", t0)
	gitDir(t, feature, "b", "ref: refs/heads/DEV-119462\n")
	if ix := Scan(feature, Default); ix.Main.Name != "b" || ix.Main.Branch != "DEV-119462" {
		t.Errorf("no copy on main: %q on %q", ix.Main.Name, ix.Main.Branch)
	}

	own := t.TempDir() // only a card's own copy: all are tried, and the one on main is preferred
	write(t, own, "specs-DEV-1/openspec/specs/x/spec.md", "x\n", t0)
	gitDir(t, own, "specs-DEV-1", "ref: refs/heads/DEV-1\n")
	write(t, own, "specs-DEV-2/openspec/specs/x/spec.md", "x\n", t0.Add(-time.Hour))
	gitDir(t, own, "specs-DEV-2", "ref: refs/heads/master\n")
	if ix := Scan(own, Default); ix.Main.Name != "specs-DEV-2" || ix.Main.Branch != "master" {
		t.Errorf("own copies: %q on %q", ix.Main.Name, ix.Main.Branch)
	}
}

// branch reads HEAD of a repository, of a worktree whose .git is a file pointing at its folder, by
// an absolute path or a relative one, and of a detached HEAD; what it cannot make out is "".
func TestBranch(t *testing.T) {
	root := t.TempDir()
	gitDir(t, root, "plain", "ref: refs/heads/feature/x-1\n")
	gitDir(t, root, "detached", "0123456789abcdef0123456789abcdef01234567\n")
	gitDir(t, root, "junk", "not a head\n")
	gitDir(t, root, "short", "abc\n")
	write(t, root, "main/.git/worktrees/wt/HEAD", "ref: refs/heads/DEV-5\n", t0)
	write(t, root, "abs/.git", "gitdir: "+filepath.Join(root, "main", ".git", "worktrees", "wt")+"\n", t0)
	write(t, root, "rel/.git", "gitdir: ../main/.git/worktrees/wt\n", t0)
	write(t, root, "bad/.git", "something else\n", t0)
	write(t, root, "dangling/.git", "gitdir: ../nowhere\n", t0)
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := &scanner{lim: Default, deadline: time.Now().Add(time.Minute)}
	for name, want := range map[string]string{
		"plain": "feature/x-1", "detached": "0123456", "junk": "", "short": "", "abs": "DEV-5", "rel": "DEV-5",
		"bad": "", "dangling": "", "empty": "",
	} {
		if got := s.branch(filepath.Join(root, name)); got != want {
			t.Errorf("%s: branch %q, want %q", name, got, want)
		}
	}
}

// A folder with more entries than the limit is not looked in for copies, and the search goes on: a
// copy after it, by name, is found, and the entries it was found too big by are all it costs.
func TestScanBigFolder(t *testing.T) {
	root := t.TempDir()
	for i := range 1000 {
		write(t, root, fmt.Sprintf("a-big/f%04d", i), "x\n", t0)
	}
	write(t, root, "a-big/inner/openspec/changes/in-big/proposal.md", "x\n", t0) // a copy no search gets to
	write(t, root, "specs-DEV-9/openspec/changes/dev-9-x/proposal.md", "Tracker: DEV-9\n", t0)
	lim := Limits{Depth: 3, Entries: 600, DirEntries: 50, Time: time.Minute}
	if ix := Scan(root, lim); ix.Partial != "" || ix.Copies != 1 || !reflect.DeepEqual(names(ix.Changes), []string{"dev-9-x"}) {
		t.Fatalf("partial %q, %d copies, changes %v", ix.Partial, ix.Copies, names(ix.Changes))
	}
	lim.DirEntries = 0 // no limit for one folder: it takes the whole of the total
	if ix := Scan(root, lim); !strings.HasPrefix(ix.Partial, "stopped after ") || ix.Copies != 0 {
		t.Errorf("no limit for a folder: partial %q, %d copies", ix.Partial, ix.Copies)
	}
	lim.Entries = 1 << 20
	if ix := Scan(root, lim); ix.Partial != "" || ix.Copies != 2 {
		t.Errorf("the big folder looked in: partial %q, %d copies", ix.Partial, ix.Copies)
	}

	// The same one level down.
	deep := t.TempDir()
	for i := range 1000 {
		write(t, deep, fmt.Sprintf("z/a-big/f%04d", i), "x\n", t0)
	}
	write(t, deep, "z/b/openspec/changes/beyond/proposal.md", "x\n", t0)
	if ix := Scan(deep, Limits{Depth: 3, Entries: 600, DirEntries: 50, Time: time.Minute}); ix.Partial != "" || ix.Copies != 1 {
		t.Errorf("a big folder at level 2: partial %q, %d copies", ix.Partial, ix.Copies)
	}
}

// What a scan costs does not grow with the files at or below the depth it looks to: thousands of
// them in the folders there are not read, so a small entry limit is no obstacle.
func TestScanCostByDepth(t *testing.T) {
	root := t.TempDir()
	for i := range 3000 {
		write(t, root, fmt.Sprintf("a/b/c/f%04d", i), "x\n", t0)
	}
	for i := range 3000 {
		write(t, root, fmt.Sprintf("a/b/f%04d", i), "x\n", t0) // the folder at the depth: not listed either
	}
	write(t, root, "a/openspec/changes/shallow/proposal.md", "x\n", t0)
	ix := Scan(root, Limits{Depth: 3, Entries: 50, Time: time.Minute})
	if ix.Partial != "" || ix.Copies != 1 {
		t.Errorf("partial %q, %d copies", ix.Partial, ix.Copies)
	}
}

// A link named openspec is no openspec folder: it may lead anywhere.
func TestScanOpenspecLink(t *testing.T) {
	root := t.TempDir()
	write(t, root, "elsewhere/changes/linked/proposal.md", "x\n", t0)
	write(t, root, "copy/x.txt", "x\n", t0)
	if err := os.Symlink(filepath.Join(root, "elsewhere"), filepath.Join(root, "copy", "openspec")); err != nil {
		t.Skip("no links here:", err)
	}
	write(t, root, "real/openspec/changes/real-change/proposal.md", "x\n", t0)
	ix := Scan(root, Default)
	if ix.Copies != 1 || len(ix.Changes) != 1 || ix.Changes[0].Name != "real-change" {
		t.Errorf("copies %d, changes %v", ix.Copies, names(ix.Changes))
	}
}

// The default limits leave out a folder of thousands of entries, as a home folder or a downloads
// folder is, and go on to the copies after it.
func TestScanDefaultDirEntries(t *testing.T) {
	root := t.TempDir()
	for i := range Default.DirEntries + 1 {
		write(t, root, fmt.Sprintf("a-big/f%05d", i), "x\n", t0)
	}
	write(t, root, "a-big/inner/openspec/changes/in-big/proposal.md", "x\n", t0)
	write(t, root, "specs-DEV-9/openspec/changes/dev-9-x/proposal.md", "Tracker: DEV-9\n", t0)
	if ix := Scan(root, Default); ix.Partial != "" || ix.Copies != 1 || len(ix.Changes) != 1 {
		t.Errorf("partial %q, %d copies, changes %v", ix.Partial, ix.Copies, names(ix.Changes))
	}
	if Default.DirEntries <= 0 {
		t.Error("the default has no limit for a folder")
	}
}
