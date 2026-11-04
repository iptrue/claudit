package ui

import (
	"bytes"
	"encoding/base64"
	"image"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestWindowsPasteRelativeImagePaths(t *testing.T) {
	for _, path := range []string{`shots\one.png`, `.\one.png`, `..\one.png`, `"shots\one two.png"`} {
		got := shellWords(path)
		if len(got) != 1 || got[0] != strings.Trim(path, `"`) {
			t.Errorf("shellWords(%q) = %q", path, got)
		}
	}
	if got := shellWords(`C:/shots/one\ two.png`); len(got) != 1 || got[0] != "C:/shots/one two.png" {
		t.Errorf("Git Bash path = %q", got)
	}
}

// These integration tests overwrite the clipboard and require an interactive Windows session.
func TestWindowsClipboardUnicodeRoundTrip(t *testing.T) {
	if os.Getenv("CLAUDIT_CLIPBOARD_TEST") != "1" {
		t.Skip("set CLAUDIT_CLIPBOARD_TEST=1 to test the real Windows clipboard")
	}
	want := "Привет, Windows 👋\nquotes: ' \" ` $()\r\nfinal line"
	if err := writeClipboardText(want); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-STA", "-Command", `
Add-Type -AssemblyName System.Windows.Forms
[Console]::Write([Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes([Windows.Forms.Clipboard]::GetText())))
`).Output()
	if err != nil {
		t.Fatal(err)
	}
	got, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
	if err != nil || string(got) != want {
		t.Fatalf("clipboard text = %q, err=%v", got, err)
	}
}

func TestWindowsClipboardImageRoundTrip(t *testing.T) {
	if os.Getenv("CLAUDIT_CLIPBOARD_TEST") != "1" {
		t.Skip("set CLAUDIT_CLIPBOARD_TEST=1 to test the real Windows clipboard")
	}
	path := modelPastePNG(t, t.TempDir(), "clipboard image.png", 3, 2)
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-STA", "-Command", `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$image = [Drawing.Image]::FromFile($env:CLAUDIT_TEST_IMAGE)
try { [Windows.Forms.Clipboard]::SetImage($image) } finally { $image.Dispose() }
`)
	cmd.Env = append(os.Environ(), "CLAUDIT_TEST_IMAGE="+path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("set image: %v: %s", err, out)
	}
	got := pasteClipboard().(imagesMsg)
	if got.err != nil || len(got.images) != 1 {
		t.Fatalf("clipboard image: %v, %d images", got.err, len(got.images))
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(got.images[0].Data))
	if err != nil || cfg.Width != 3 || cfg.Height != 2 || got.images[0].Media != "image/png" {
		t.Fatalf("clipboard image: %v, %dx%d %s", err, cfg.Width, cfg.Height, got.images[0].Media)
	}
}
