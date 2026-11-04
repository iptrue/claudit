package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func writeClipboardText(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

func clipboardImage() ([]byte, error) {
	f, err := os.CreateTemp("", "claudit-*.png")
	if err != nil {
		return nil, err
	}
	f.Close()
	defer os.Remove(f.Name())
	err = exec.Command("osascript",
		"-e", "set png_data to (the clipboard as «class PNGf»)",
		"-e", fmt.Sprintf("set fp to open for access POSIX file %q with write permission", f.Name()),
		"-e", "write png_data to fp",
		"-e", "close access fp").Run()
	if err != nil {
		return nil, errors.New("no picture on the clipboard")
	}
	return os.ReadFile(f.Name())
}
