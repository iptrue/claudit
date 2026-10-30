package ui

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/pixelbender/claudit/internal/protocol"
)

// modelPastePNG writes a w×h PNG to dir/name and returns its path.
func modelPastePNG(t *testing.T, dir, name string, w, h int) string {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// modelPasteRun pastes s and feeds the model what the paste's command brings.
func modelPasteRun(m *Model, s string) {
	_, cmd := m.Update(tea.PasteMsg{Content: s})
	if cmd != nil {
		m.Update(cmd())
	}
}

func TestPasteLongTextStandsAsPlaceholder(t *testing.T) {
	m, sent := modelNew(t, false)
	long := "a\nb\nc\nd"
	modelPress(m, inputTyped("See ")...)
	modelPasteRun(m, long)
	if got := string(m.Input.Text); got != "See [Pasted text #1 +3 lines]" {
		t.Fatalf("prompt %q", got)
	}
	modelPasteRun(m, strings.Repeat("x", pasteChars+1))
	if got := string(m.Input.Text); !strings.HasSuffix(got, "[Pasted text #2]") {
		t.Fatalf("a long line: prompt %q", got)
	}
	modelPress(m, inputKey("enter"))
	if len(*sent) != 1 || modelUserText((*sent)[0]) != "See "+long+strings.Repeat("x", pasteChars+1) {
		t.Fatalf("sent %#v", *sent)
	}
	if got := m.Chat[len(m.Chat)-1].Text; got != "See [Pasted text #1 +3 lines][Pasted text #2]" {
		t.Errorf("chat %q, want the placeholders", got)
	}
}

func TestPasteQueuedAndRecalledKeepTheirText(t *testing.T) {
	m, sent := modelNew(t, false)
	m.Status.Busy = true
	modelPasteRun(m, "1\n2\n3\n4")
	modelPress(m, inputKey("enter"))
	modelPress(m, inputKey("up")) // back to edit, then again
	modelPress(m, inputKey("enter"))
	modelEvents(m, protocol.Result{})
	if len(*sent) != 1 || modelUserText((*sent)[0]) != "1\n2\n3\n4" {
		t.Fatalf("sent %#v", *sent)
	}
	modelPress(m, inputKey("up"), inputKey("enter")) // from the history
	modelEvents(m, protocol.Result{})
	if len(*sent) != 2 || modelUserText((*sent)[1]) != "1\n2\n3\n4" {
		t.Fatalf("sent %#v", *sent)
	}
}

func TestPasteBackspaceDeletesPlaceholder(t *testing.T) {
	m, _ := modelNew(t, false)
	modelPress(m, inputTyped("x")...)
	modelPasteRun(m, "1\n2\n3\n4")
	modelPress(m, inputKey("backspace"))
	if got := string(m.Input.Text); got != "x" || m.Input.Cursor != 1 {
		t.Fatalf("prompt %q cursor %d", got, m.Input.Cursor)
	}
	modelPress(m, inputKey("backspace"))
	if len(m.Input.Text) != 0 {
		t.Fatalf("prompt %q", string(m.Input.Text))
	}
}

func TestPasteDroppedPicturesAttach(t *testing.T) {
	m, sent := modelNew(t, false)
	dir := t.TempDir()
	a := modelPastePNG(t, dir, "shot one.png", 4, 3)
	b := modelPastePNG(t, dir, "b.PNG", 2, 2)
	modelPress(m, inputTyped("Look ")...)
	modelPasteRun(m, strings.ReplaceAll(a, " ", `\ `)+" '"+b+"' ")
	if got := string(m.Input.Text); got != "Look [Image #1] [Image #2] " {
		t.Fatalf("prompt %q", got)
	}
	modelPress(m, inputKey("enter"))
	u := (*sent)[0].(protocol.UserMessage)
	want, _ := os.ReadFile(a)
	if u.Text != "Look [Image #1] [Image #2]" || len(u.Images) != 2 || u.Images[0].Media != "image/png" || !bytes.Equal(u.Images[0].Data, want) {
		t.Fatalf("sent %q with %d images", u.Text, len(u.Images))
	}
}

// A terminal that pastes a picture as base64, wrapped or as a data URL, gives a picture; base64
// of anything else stays text.
func TestPasteBase64PictureAttaches(t *testing.T) {
	data, _ := os.ReadFile(modelPastePNG(t, t.TempDir(), "a.png", 3, 2))
	enc := base64.StdEncoding.EncodeToString(data)
	wrapped := enc[:20] + "\n" + enc[20:]
	for i, s := range []string{wrapped, "data:image/png;base64," + enc} {
		m, sent := modelNew(t, false)
		modelPasteRun(m, s)
		if got := string(m.Input.Text); got != "[Image #1] " {
			t.Fatalf("paste %d: prompt %q", i, got)
		}
		modelPress(m, inputKey("enter"))
		if u := (*sent)[0].(protocol.UserMessage); len(u.Images) != 1 || !bytes.Equal(u.Images[0].Data, data) {
			t.Fatalf("paste %d: sent %d images", i, len(u.Images))
		}
	}
	m, _ := modelNew(t, false)
	modelPasteRun(m, "aGVsbG8=") // "hello"
	if got := string(m.Input.Text); got != "aGVsbG8=" {
		t.Errorf("base64 text: prompt %q", got)
	}
}

func TestPastePathsThatAreNotPicturesStayText(t *testing.T) {
	m, _ := modelNew(t, false)
	dir := t.TempDir()
	txt := filepath.Join(dir, "notes.txt")
	os.WriteFile(txt, []byte("hi"), 0o644)
	for _, s := range []string{txt, filepath.Join(dir, "gone.png"), modelPastePNG(t, dir, "a.png", 1, 1) + " " + txt} {
		m.Input = Input{}
		modelPasteRun(m, s)
		if got := string(m.Input.Text); got != s {
			t.Errorf("paste %q: prompt %q", s, got)
		}
	}
}

func TestPasteShellWords(t *testing.T) {
	for _, tt := range []struct {
		in   string
		want []string
	}{
		{`/a\ b.png /c.png`, []string{"/a b.png", "/c.png"}},
		{`'/a b.png' "/c d.png"`, []string{"/a b.png", "/c d.png"}},
		{`'/a b.png`, nil},
		{"  ", nil},
	} {
		if got := shellWords(tt.in); !slices.Equal(got, tt.want) {
			t.Errorf("shellWords(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPasteLargePictureShrinks(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("sips is macOS's")
	}
	dir := t.TempDir()
	small, err := loadImage(modelPastePNG(t, dir, "small.png", 300, 200))
	if err != nil || small.Media != "image/png" {
		t.Fatalf("small: %v %q", err, small.Media)
	}
	big, err := loadImage(modelPastePNG(t, dir, "big.png", 3000, 1000))
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(big.Data))
	if err != nil || big.Media != "image/jpeg" || max(cfg.Width, cfg.Height) != imageSide {
		t.Fatalf("big: %v %q %dx%d", err, big.Media, cfg.Width, cfg.Height)
	}
}
