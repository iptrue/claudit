package ideview

import (
	"fmt"
	"image/color"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"github.com/pixelbender/claudit/internal/ide"
	"github.com/pixelbender/claudit/internal/theme"
)

// glowLife is how long a typed line flashes, and how long its light takes to fade once its agent
// moves on (ide keeps faded glows as long).
const glowLife = 2 * time.Second

// glowHold is how lit a typed line stays after its flash, while its agent is on that tool call.
const glowHold = 0.16

// glow is the background of a line g lights green, as a diff's added lines: a flash that settles
// to a steady light, which fades once its agent moves on; nil when unlit.
func glow(g ide.Glow, now time.Time) color.Color {
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
	return theme.Mix(theme.Bg, theme.Green, f)
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
	okLine   = regexp.MustCompile(`(?i)(^ok\b|\bpass(ed|es)?\b|\bsuccess|✓|\ball tests passed\b|^exit=0$)`)
	errLine  = regexp.MustCompile(`(?i)(\bfail(s|ed|ures?)?\b|\berrors?\b|\w+error:|\bpanic\b|traceback|✗|exception\b|^exit=[1-9])`)
	warnLine = regexp.MustCompile(`(?i)warn(ings?)?\b`) // DeprecationWarning too
	// noneLine is a count of none: "0 errors", "no failures", "Errors: 0" say nothing went wrong.
	noneLine = regexp.MustCompile(`(?i)\b(0|no|zero)\s+(errors?|failures?|failed|problems?|warnings?)\b|\b(errors?|failures?|failed|problems?|warnings?):\s*0\b`)
	// diagLine is a compiler's or a linter's word on a line and column of a file: "./calc.go:12:2:
	// undefined: x"; a test's log has no column.
	diagLine = regexp.MustCompile(`^[^\s:]*[./][^\s:]*:\d+:\d+: `)
)

// outputStyle colours a line of command output by what it says: passes green, failures and a
// compiler's complaints red, warnings yellow. ok is false for a line that says none of these.
func outputStyle(line string) (st lipgloss.Style, ok bool) {
	t := noneLine.ReplaceAllString(strings.TrimSpace(line), "")
	switch {
	case errLine.MatchString(t):
		return errStyle, true
	case warnLine.MatchString(t):
		return lipgloss.NewStyle().Foreground(theme.Warn), true
	case diagLine.MatchString(t):
		return errStyle, true
	case okLine.MatchString(t):
		return okStyle, true
	}
	return fgStyle, false
}

// codeLine draws text, a row of the line l of code, as the editor draws its file's language; in
// a file of no language it knows, as plain output.
func codeLine(st *ide.State, text string, l *ide.TermLine) string {
	if lang := langOf(l.Lang); lang != nil {
		return paint(text, lang)
	}
	return outputLine(st, text, l.Text)
}

// hitLine draws text, a row of the search hit l: on its first row where the hit is, the file in its
// explorer colour and the line number grey; then the hit's line as the editor draws its code, or
// in a file of no language it knows, as plain output.
func hitLine(st *ide.State, text string, first bool, l *ide.TermLine) string {
	var at string
	if first {
		rs, src := []rune(text), []rune(l.Text) // Prefix counts the runes of the line, text those drawn
		n := min(utf8.RuneCountInString(drawn(string(src[:min(l.Prefix, len(src))]))), len(rs))
		at, text = string(rs[:n]), string(rs[n:])
		if path := drawn(l.Lang); path != "" && strings.HasPrefix(at, path) {
			at = lipgloss.NewStyle().Foreground(fileTint(l.Lang)).Render(path) + dimStyle.Render(at[len(path):])
		} else {
			at = dimStyle.Render(at)
		}
	}
	if lang := langOf(l.Lang); lang != nil {
		return at + paint(text, lang)
	}
	return at + outputLine(st, text, text)
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

// typeHold is how long a caret stays a block after its agent typed, selected or deleted text:
// longer than an edit's pauses, so that it does not spin in between.
const typeHold = 500 * time.Millisecond

// typing reports whether a types, selects or deletes text by now: its caret is a solid block.
func typing(a *ide.Agent, o Options) bool {
	return !a.TypedAt.IsZero() && o.Now.Sub(a.TypedAt) < typeHold
}

// spinFrame is the spinner frame for now, turning every 80 ms.
func spinFrame(now time.Time) string {
	n := int64(len(spinner))
	return spinner[(now.UnixMilli()/80%n+n)%n]
}
