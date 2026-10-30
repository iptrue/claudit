package ui

import (
	"fmt"
	"strings"
	"time"
)

// ToastKind sets the icon and colour of a toast.
type ToastKind int

const (
	ToastInfo ToastKind = iota
	ToastOK
	ToastErr
)

// Toast is a short notice in the corner of the IDE: an agent started or finished, a command
// failed, claude said something outside the conversation. It slides in and fades away.
type Toast struct {
	Kind ToastKind
	Icon string // replaces the icon of the kind; "" keeps it
	Text string
	Slot int // palette slot of the agent it is about; -1 for none
	At   time.Time
}

const (
	toastLife = 4 * time.Second
	maxToasts = 4
)

// toast shows a notice; slot is the palette slot of the agent it is about, or -1. A text that
// starts with a glyph and a space ("◇ tests started") shows that glyph as its icon.
func (m *Model) toast(kind ToastKind, slot int, format string, args ...any) {
	text := firstLineOf(fmt.Sprintf(format, args...))
	if text == "" {
		return
	}
	var icon string
	if r := []rune(text); len(r) > 2 && r[1] == ' ' && strings.ContainsRune("◇▣◆◉", r[0]) {
		icon, text = string(r[0]), string(r[2:])
	}
	m.Toasts = append(m.Toasts, Toast{Kind: kind, Icon: icon, Text: clipRunes(text, 120), Slot: slot, At: m.Now}) // the clock the view animates by
	if n := len(m.Toasts) - maxToasts; n > 0 {
		m.Toasts = m.Toasts[n:]
	}
}

// expireToasts drops the toasts that have faded out by now.
func (m *Model) expireToasts(now time.Time) {
	i := 0
	for i < len(m.Toasts) && now.Sub(m.Toasts[i].At) >= toastLife {
		i++
	}
	m.Toasts = m.Toasts[i:]
}
