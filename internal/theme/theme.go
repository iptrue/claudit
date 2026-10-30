// Package theme is the shared palette, tuned for a dark terminal. The background stays the terminal's own.
package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// The palette: every colour on screen is one of these tokens or a halftone between two of them
// (Mix), so that there are never more than these to tell apart. Nothing else names a colour.
var (
	Bg     = lipgloss.Color("#0d1117") // the background the palette assumes; fades blend toward it
	Fg     = lipgloss.Color("#c9d1d9")
	Bright = lipgloss.Color("#f8f8f2") // replies, commands, and the white of flashes and pastels
	Accent = lipgloss.Color("#d97757")
	Red    = lipgloss.Color("#f85149")
	Orange = lipgloss.Color("#ffa657")
	Yellow = lipgloss.Color("#e3b341")
	Lime   = lipgloss.Color("#bfff00") // a terminal's prompt and caret
	Green  = lipgloss.Color("#56d364")
	Teal   = lipgloss.Color("#3fb9a0")
	Cyan   = lipgloss.Color("#56d4dd")
	Blue   = lipgloss.Color("#79c0ff")
	Violet = lipgloss.Color("#d2a8ff") // the help title's gradient, inline code, diff hunks
	Pink   = lipgloss.Color("#ff79c6")
)

// What each part of the screen takes from the palette.
var (
	Strong      = Yellow // emphasis in place of bold: titles, headers, **strong**; as bright as the auto mode
	Dim         = Mix(Fg, Bg, 0.5)
	Border      = Mix(Fg, Bg, 0.82)
	BorderFocus = Mix(Fg, Bg, 0.6) // a pane in focus: a lighter grey, never white
	SelBg       = Mix(Blue, Bg, 0.65)
	SelBgDim    = Mix(Blue, Bg, 0.75) // a selection out of focus: the explorer's while its agent works elsewhere
	LineBg      = Mix(Fg, Bg, 0.93)
	OK          = Green
	Err         = Red
	Warn        = Yellow
	Sky         = Mix(Blue, Bright, 0.3) // the help title's gradient, links

	// Syntax: on the terminal's own background, never bold or italic.
	Code    = Fg                       // names, operators, punctuation
	Keyword = Mix(Orange, Dim, 0.18)   // a muted orange
	String  = Mix(Green, Yellow, 0.45) // olive
	Interp  = Mix(String, Bright, 0.7) // what a string interpolates: ${…}
	Comment = Mix(Blue, Bg, 0.45)
	Number  = Sky
	Func    = Mix(Violet, Blue, 0.15) // lavender: what is called
)

// Agents colours agents by their palette slot (ide.Agent.Color); slot 0 is the main agent.
var Agents = []color.Color{Accent, Cyan, Green, Violet, Yellow, Blue, Pink, Orange}

// Mix blends a toward b: t = 0 gives a, t = 1 gives b.
func Mix(a, b color.Color, t float64) color.Color {
	t = min(max(t, 0), 1)
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	mix := func(x, y uint32) uint8 { return uint8((float64(x)*(1-t) + float64(y)*t) / 257) }
	return color.RGBA{R: mix(ar, br), G: mix(ag, bg), B: mix(ab, bb), A: 0xff}
}

// Gradient is the colour at t ∈ [0, 1] along the stops, evenly spaced.
func Gradient(t float64, stops ...color.Color) color.Color {
	if len(stops) == 1 {
		return stops[0]
	}
	t = min(max(t, 0), 1) * float64(len(stops)-1)
	i := min(int(t), len(stops)-2)
	return Mix(stops[i], stops[i+1], t-float64(i))
}
