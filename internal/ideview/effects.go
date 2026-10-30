package ideview

import (
	"fmt"
	"image/color"
	"math"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"github.com/pixelbender/claudit/internal/ide"
	"github.com/pixelbender/claudit/internal/theme"
)

// cutTime is how long the right column takes to wipe in after the camera cuts to another agent or
// its agent turns between the editor and the terminal.
const cutTime = 220 * time.Millisecond

// wipe reveals lines top-down behind a scanline in colour c, as a CRT refreshing, cutTime after at.
func wipe(lines []string, at, now time.Time, c color.Color, w int) []string {
	age := now.Sub(at)
	if at.IsZero() || age < 0 || age >= cutTime {
		return lines
	}
	t := float64(age) / float64(cutTime)
	k := int(easeOut(t) * float64(len(lines)))
	out := make([]string, len(lines))
	copy(out, lines[:k])
	for i := k; i < len(lines); i++ {
		if i == k {
			out[i] = lipgloss.NewStyle().Foreground(c).Render(strings.Repeat("━", w))
		} else {
			out[i] = strings.Repeat(" ", w)
		}
	}
	return out
}

func easeOut(t float64) float64 { return 1 - math.Pow(1-t, 3) }

// glowLife is how long a typed line flashes, and how long its light takes to fade once its agent
// moves on (ide keeps faded glows as long).
const glowLife = 2 * time.Second

// glowHold is how lit a typed line stays after its flash, while its agent is on that tool call.
const glowHold = 0.16

// glow is the background of a line g lights in an agent's colour c: a flash that settles to a
// steady light, which fades once the agent moves on; nil when unlit.
func glow(g ide.Glow, now time.Time, c color.Color) color.Color {
	fade := func(from time.Time) float64 {
		k := max(1-float64(max(now.Sub(from), 0))/float64(glowLife), 0)
		return k * k
	}
	f := glowHold
	if !g.Off.IsZero() {
		f *= fade(g.Off)
	}
	if f = max(f, 0.42*fade(g.At)); f < 0.42*0.06 {
		return nil
	}
	return theme.Mix(theme.Bg, c, f)
}

// Cutting reports whether the right column is wiping in at now.
func Cutting(st *ide.State, now time.Time) bool {
	at := cutAt(st, camera(st))
	return !at.IsZero() && now.Sub(at) >= 0 && now.Sub(at) < cutTime
}

// logo is the splash of an empty editor: a cat at a laptop, in pixels, two to a cell (30×10
// cells). A letter is the ink of a pixel (logoInks), a dot is clear.
var logo = []string{
	"..............o...............",
	"............ooo...............",
	"............ooocc.....o.......",
	"...........oocccc...ooo.......",
	"..........cccccccccoooo.......",
	"..........cccccccccoooo.......",
	".........cccccccccccoooo......",
	"........ccccccccccccooo.......",
	"........ccc..ccccccccooo......",
	".............cccs..ccccc......",
	"llllllllllll.sscc..cccss......",
	"lgggggggggggl.cccssssscc....oo",
	"lgggggggggggl.ssssssssc....ooo",
	".ggggrgrgrgggl.ssssssss....ooo",
	".gggggrrrggggl.oosssso...boooo",
	".ggggrrrrrgggl.sssssooo.ssso..",
	".gggggrrrggggl..sssssoo..sso..",
	".ggggrgrgrgggl..ssssssss.ss...",
	"..ggggggggggggl......ss.......",
	"..gggggggggggglgggggg.........",
}

// logoInks are the colours of the logo's pixels by letter.
var logoInks = [256]color.Color{
	'c': theme.Mix(theme.Accent, theme.Bright, 0.71),                       // fur
	's': theme.Mix(theme.Fg, theme.Orange, 0.31),                           // fur in the shade
	'o': theme.Mix(theme.Bg, theme.Orange, 0.89),                           // the patches
	'b': theme.Mix(theme.Bg, theme.Orange, 0.46),                           // a dark patch
	'g': theme.Mix(theme.Bg, theme.Orange, 0.12),                           // the laptop
	'l': theme.Mix(theme.Mix(theme.Bg, theme.Orange, 0.12), theme.Fg, 0.2), // its rim
	'r': theme.Accent,                                                      // the asterisk on its lid
}

// splash fills an empty editor with the logo, centred in w×h cells; nothing where it does not fit.
func splash(w, h int) []string {
	out := make([]string, h)
	lw, lh := len(logo[0]), (len(logo)+1)/2
	if lw > w || lh > h {
		return out
	}
	top, pad := (h-lh)/2, strings.Repeat(" ", (w-lw)/2)
	// ink is the colour of the pixel at x, y; nil where clear.
	ink := func(x, y int) color.Color {
		if y >= len(logo) {
			return nil
		}
		return logoInks[logo[y][x]]
	}
	rgb := func(c color.Color) string {
		r, g, b, _ := c.RGBA()
		return fmt.Sprintf("%d;%d;%d", r>>8, g>>8, b>>8)
	}
	for y := range lh {
		var b strings.Builder
		back := false // the cell before set a background
		for x := range lw {
			// Raw SGR: this runs for every cell of every frame, lipgloss is too slow for it.
			hi, lo, r := ink(x, 2*y), ink(x, 2*y+1), '▀'
			if hi == nil {
				hi, lo, r = lo, nil, '▄'
			}
			if back && lo == nil {
				b.WriteString("\x1b[49m")
				back = false
			}
			switch {
			case hi == nil:
				b.WriteByte(' ')
			case lo == nil:
				fmt.Fprintf(&b, "\x1b[38;2;%sm%c", rgb(hi), r)
			default:
				fmt.Fprintf(&b, "\x1b[38;2;%s;48;2;%sm%c", rgb(hi), rgb(lo), r)
				back = true
			}
		}
		b.WriteString("\x1b[m")
		out[top+y] = pad + b.String()
	}
	return out
}

var (
	okLine   = regexp.MustCompile(`(?i)(^ok\b|\bpass(ed)?\b|\bsuccess|✓|\ball tests passed\b|^exit=0$)`)
	errLine  = regexp.MustCompile(`(?i)(\bfail(ed|ure)?\b|\berror\b|\bpanic\b|traceback|✗|exception\b|^exit=[1-9])`)
	warnLine = regexp.MustCompile(`(?i)\bwarn(ing)?\b`)
)

// outputStyle colours a line of command output by what it says: passes green, failures red,
// warnings yellow, diff lines by their sign. ok is false for a line that says none of these.
func outputStyle(line string) (st lipgloss.Style, ok bool) {
	t := strings.TrimSpace(line)
	switch {
	case errLine.MatchString(t):
		return errStyle, true
	case warnLine.MatchString(t):
		return lipgloss.NewStyle().Foreground(theme.Warn), true
	case okLine.MatchString(t):
		return okStyle, true
	case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
		return okStyle, true
	case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
		return errStyle, true
	case strings.HasPrefix(line, "@@"):
		return lipgloss.NewStyle().Foreground(theme.Violet), true
	}
	return fgStyle, false
}

// outLang is what command output has of a language: numbers, quoted strings, constants, calls.
var outLang = newLanguage(`true false null nil none None undefined NaN`, "", "", `"`)

// outputWord is a word of command output that may name a file: a path without blanks.
var outputWord = regexp.MustCompile(`[\p{L}\p{N}_./~@+-]+`)

// outputLine draws text, a row of the output line line: all of it in the colour of what the line
// says, else its numbers, strings and constants as in code, and the files it names as the
// explorer does (a relative path is taken from the root).
func outputLine(st *ide.State, text, line string) string {
	if s, ok := outputStyle(line); ok {
		return s.Render(text)
	}
	rs := []rune(text)
	var fg []color.Color
	for _, m := range outputWord.FindAllStringIndex(text, -1) {
		name := strings.TrimRight(text[m[0]:m[1]], ".") // the end of a sentence
		if fileColor(name) == nil {
			continue
		}
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(st.Root, path)
		}
		if fg == nil {
			fg = make([]color.Color, len(rs))
		}
		from, c := utf8.RuneCountInString(text[:m[0]]), fileTint(path)
		for i := range utf8.RuneCountInString(name) {
			fg[from+i] = c
		}
	}
	return paintRunes(rs, highlight(rs, outLang), fg)
}

// working reports whether a is at work: main by the ui's word (its agent stays Running), the
// others by their status.
func working(a *ide.Agent, o Options) bool {
	if a.Kind == ide.Main {
		return o.Busy
	}
	return a.Status == ide.Running
}

// spinFrame is the spinner frame for now, turning every 80 ms.
func spinFrame(now time.Time) string {
	n := int64(len(spinner))
	return spinner[(now.UnixMilli()/80%n+n)%n]
}
