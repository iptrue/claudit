package ideview

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/ide"
	"github.com/pixelbender/claudit/internal/theme"
)

// browserState has main in its browser, with the results of a search loaded, and no file of its
// own beside it.
func browserState() *ide.State {
	st := sampleState()
	st.Agents[0].Focus, st.Agents[0].File = ide.Browser, ""
	st.Agents[0].Page = ide.Page{URL: "bubble tea", Search: true, Lines: []ide.PageLine{
		{Text: "Bubble Tea", URL: "https://github.com/charmbracelet/bubbletea"},
		{},
		{Text: "## Summary"},
		{Text: "- A **TUI** framework for Go, " + strings.Repeat("with words ", 12) + "end."},
	}}
	return st
}

// The browser shows the address in its bar, then the page: links over their addresses, text
// wrapped at spaces, headings and bullets without their markdown.
func TestBrowser(t *testing.T) {
	st := browserState()
	out := Render(st, Options{Now: now}, 100, 20)
	checkSize(t, "browser", out, 100, 20)
	plain := ansi.Strip(out)
	for _, want := range []string{"╭─ web ─", "│ search bubble tea", "Bubble Tea", "https://github.com/charmbracelet/bubbletea", "│ Summary", "• A TUI framework for Go,", "end."} {
		if !strings.Contains(plain, want) {
			t.Errorf("lacks %q:\n%s", want, plain)
		}
	}
	if !styled(out, "Bubble Tea", sgr("38", theme.Sky)) || !styled(out, "Summary", sgr("38", theme.Strong)) {
		t.Errorf("link or heading not lit:\n%q", out)
	}
	// The address bar has no buttons and no background of its own.
	if bar := lineWith(out, "search"); strings.ContainsAny(ansi.Strip(bar), "←→↻") || strings.Contains(bar, sgr("48", theme.LineBg)) {
		t.Errorf("address bar %q", bar)
	}

	// While the page loads a spinner turns a space after the address; the rule stays plain.
	st.Agents[0].Page = ide.Page{URL: "https://go.dev/doc", LoadAt: now.Add(-100 * time.Millisecond)}
	out = Render(st, Options{Now: now}, 100, 20)
	if plain := ansi.Strip(out); !strings.Contains(plain, "│ https://go.dev/doc "+spinFrame(now)+" ") || strings.Contains(plain, "━") {
		t.Errorf("loading:\n%s", plain)
	}

	// The wheel scrolls the page down from its top, and back.
	st = browserState()
	st.Agents[0].Page.Lines = append(st.Agents[0].Page.Lines, make([]ide.PageLine, 30)...)
	o := Options{Now: now, Scroll: Scroll{}}
	key, back := Wheel(st, o, 100, 20, 50, 10, 3)
	if key != webKey("main") || back != 0 {
		t.Errorf("up from the top: %q %d", key, back)
	}
	if _, back = Wheel(st, o, 100, 20, 50, 10, -3); back != -3 {
		t.Errorf("down: %d", back)
	}
	o.Scroll[key] = -3
	if plain := ansi.Strip(Render(st, o, 100, 20)); strings.Contains(plain, "Bubble Tea") || !strings.Contains(plain, "Summary") {
		t.Errorf("scrolled down 3:\n%s", plain)
	}
}
