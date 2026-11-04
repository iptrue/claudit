package ideview

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestShortPathWindows(t *testing.T) {
	for _, tc := range []struct {
		root, path string
		width      int
		want       string
	}{
		{`C:\work`, `C:\work\src\main.go`, 40, "src/main.go"},
		{`C:\work`, `c:\WORK\src\main.go`, 40, "src/main.go"},
		{`C:\work`, `C:\work\internal\ui\deep\er\model_mouse.go`, 30, "internal/…/er/model_mouse.go"},
		{`C:\work`, `C:\elsewhere\main.go`, 40, "C:/elsewhere/main.go"},
		{`C:\work`, `D:\work\main.go`, 40, "D:/work/main.go"},
		{`\\server\share\work`, `\\server\share\work\src\main.go`, 40, "src/main.go"},
		{`\\server\share\work`, `\\server\other\main.go`, 40, "//server/other/main.go"},
	} {
		if got := shortPath(tc.path, tc.root, tc.width); got != tc.want || ansi.StringWidth(got) > tc.width {
			t.Errorf("shortPath(%q, %q, %d) = %q, want %q", tc.path, tc.root, tc.width, got, tc.want)
		}
	}
}
