package ui

import (
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
)

func writeClipboardText(text string) error {
	cmd := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-STA", "-Command", `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
$text = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String([Console]::In.ReadToEnd()))
[Windows.Forms.Clipboard]::SetText($text, [Windows.Forms.TextDataFormat]::UnicodeText)
`)
	// ASCII base64 keeps Unicode intact regardless of the console's code page.
	cmd.Stdin = strings.NewReader(base64.StdEncoding.EncodeToString([]byte(text)))
	return cmd.Run()
}

func clipboardImage() ([]byte, error) {
	cmd := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-STA", "-Command", `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$image = [Windows.Forms.Clipboard]::GetImage()
if ($null -eq $image) { throw 'No picture on the clipboard' }
$stream = New-Object IO.MemoryStream
try {
    $image.Save($stream, [Drawing.Imaging.ImageFormat]::Png)
    [Console]::Write([Convert]::ToBase64String($stream.ToArray()))
} finally {
    $stream.Dispose()
    $image.Dispose()
}
`)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("cannot read a picture from the Windows clipboard: %w", err)
	}
	return base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
}
