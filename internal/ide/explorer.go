package ide

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// hidden names never show up in the explorer.
var hidden = map[string]bool{
	".git": true, "node_modules": true, "target": true, "__pycache__": true,
	".DS_Store": true, ".idea": true, ".venv": true,
}

// SetRoot points the explorer at root and lists its children.
func (st *State) SetRoot(root string) {
	st.Root = filepath.Clean(root)
	st.Tree = list(st.Root, 0)
}

// Reveal makes path visible in the explorer: it expands the ancestors and adds the rows that are
// missing (the file may not be on disk yet). It returns the row of path, or -1 when path is
// outside Root or hidden.
func (st *State) Reveal(path string) int {
	rel, err := filepath.Rel(st.Root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return -1
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if slices.ContainsFunc(parts, func(p string) bool { return hidden[p] }) {
		return -1
	}
	row, dir := -1, st.Root
	for i, name := range parts {
		dir = filepath.Join(dir, name)
		last := i == len(parts)-1
		r := st.child(row, name)
		if r < 0 {
			rel := filepath.Join(parts[:i+1]...)
			if !last {
				rel += "/" // git cannot tell a folder that is not on disk yet
			}
			r = st.insert(row, TreeRow{Depth: i, Name: name, Path: dir, Dir: !last, Ignored: ignored(st.Root, rel)[rel]})
		}
		if !last {
			if !st.Tree[r].Dir {
				return -1
			}
			st.expand(r)
		}
		row = r
	}
	return row
}

// child returns the row named name directly under the row parent (-1 for Root), or -1.
func (st *State) child(parent int, name string) int {
	depth := 0
	if parent >= 0 {
		depth = st.Tree[parent].Depth + 1
	}
	for i := parent + 1; i < len(st.Tree) && st.Tree[i].Depth >= depth; i++ {
		if st.Tree[i].Depth == depth && st.Tree[i].Name == name {
			return i
		}
	}
	return -1
}

// insert puts row among the children of parent (-1 for Root) in sorted order and returns its index.
func (st *State) insert(parent int, row TreeRow) int {
	at := parent + 1
	for at < len(st.Tree) && st.Tree[at].Depth >= row.Depth {
		if st.Tree[at].Depth == row.Depth && compare(row, st.Tree[at]) < 0 {
			break
		}
		at++
	}
	st.Tree = slices.Insert(st.Tree, at, row)
	return at
}

// Toggle folds the directory at path in the explorer, or unfolds it, read from disk afresh;
// anything else stays as it is.
func (st *State) Toggle(path string) {
	r := slices.IndexFunc(st.Tree, func(row TreeRow) bool { return row.Path == path })
	switch {
	case r < 0 || !st.Tree[r].Dir:
	case st.Tree[r].Expanded:
		end := r + 1
		for end < len(st.Tree) && st.Tree[end].Depth > st.Tree[r].Depth {
			end++
		}
		st.Tree = slices.Delete(st.Tree, r+1, end)
		st.Tree[r].Expanded = false
	default:
		st.expand(r)
	}
}

// expand lists the directory at row r below it.
func (st *State) expand(r int) {
	if st.Tree[r].Expanded {
		return
	}
	st.Tree[r].Expanded = true
	rows := list(st.Tree[r].Path, st.Tree[r].Depth+1)
	st.Tree = slices.Insert(st.Tree, r+1, rows...)
}

// list reads dir as explorer rows: directories first, then files, case-insensitive.
func list(dir string, depth int) []TreeRow {
	entries, _ := os.ReadDir(dir)
	var rows []TreeRow
	var names []string
	for _, e := range entries {
		if hidden[e.Name()] {
			continue
		}
		rows = append(rows, TreeRow{Depth: depth, Name: e.Name(), Path: filepath.Join(dir, e.Name()), Dir: e.IsDir()})
		names = append(names, e.Name())
	}
	ig := ignored(dir, names...)
	for i := range rows {
		rows[i].Ignored = ig[rows[i].Name]
	}
	slices.SortFunc(rows, compare)
	return rows
}

// ignored is the set of paths, relative to dir, that git ignores; outside a repository, none.
func ignored(dir string, paths ...string) map[string]bool {
	if len(paths) == 0 {
		return nil
	}
	cmd := exec.Command("git", "-C", dir, "check-ignore", "-z", "--stdin")
	cmd.Stdin = strings.NewReader(strings.Join(paths, "\x00"))
	out, _ := cmd.Output() // exit 1: none ignored; 128: not a repository
	set := map[string]bool{}
	for _, p := range strings.Split(string(out), "\x00") {
		set[p] = true
	}
	return set
}

func compare(a, b TreeRow) int {
	if a.Dir != b.Dir {
		if a.Dir {
			return -1
		}
		return 1
	}
	if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
		return c
	}
	return strings.Compare(a.Name, b.Name)
}

// Forget undoes Reveal and Open for a path that is not on disk (a Write that failed): it closes
// the tab, no agent works on the file any more, and the rows of the path and of its folders that
// are not on disk either go away.
func (st *State) Forget(path string) {
	if onDisk(path) {
		return
	}
	for i, t := range st.Tabs {
		if t.Path == path {
			st.Tabs = slices.Delete(st.Tabs, i, i+1)
			st.retitle()
			break
		}
	}
	for _, a := range st.Agents {
		if a.File == path {
			a.File = ""
		}
	}
	delete(st.Touched, path)
	delete(st.Edited, path)
	for p := path; !onDisk(p); p = filepath.Dir(p) {
		if r := slices.IndexFunc(st.Tree, func(row TreeRow) bool { return row.Path == p }); r >= 0 {
			st.remove(r)
		}
	}
}

// remove deletes row r with the rows below it.
func (st *State) remove(r int) {
	end := r + 1
	for end < len(st.Tree) && st.Tree[end].Depth > st.Tree[r].Depth {
		end++
	}
	st.Tree = slices.Delete(st.Tree, r, end)
}

func onDisk(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
