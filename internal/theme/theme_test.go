package theme

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every colour on screen is a token of the palette or a halftone of tokens: no file but this
// package's names one, and the palette keeps to 16 tokens at most.
func TestPaletteIsClosed(t *testing.T) {
	named := regexp.MustCompile(`lipgloss\.Color\(|color\.N?RGBA(64)?\{|color\.(White|Black)\b`)
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && path != root && (d.Name() == "testdata" || strings.HasPrefix(d.Name(), ".")):
			return filepath.SkipDir
		case d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go"):
			return nil
		case filepath.Dir(path) == filepath.Join(root, "internal", "theme"):
			return nil // this package
		}
		src, err := os.ReadFile(path)
		if n := len(named.FindAllIndex(src, -1)); n > 0 {
			t.Errorf("%s names %d colours of its own", path, n)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile("theme.go")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(src), `lipgloss.Color("#`); n == 0 || n > 16 {
		t.Errorf("%d tokens in the palette, want 1 to 16", n)
	}
}
