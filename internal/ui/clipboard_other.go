//go:build !darwin && !windows

package ui

import "errors"

func writeClipboardText(string) error {
	return errors.New("system clipboard is unavailable")
}

func clipboardImage() ([]byte, error) {
	return nil, errors.New("clipboard pictures are supported on macOS and Windows")
}
