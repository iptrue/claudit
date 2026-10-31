package ui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/ideview"

	"github.com/pixelbender/claudit/internal/theme"
)

// drawCheckFits fails unless s is at most h lines of at most w cells.
func drawCheckFits(t *testing.T, s string, w, h int) {
	t.Helper()
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		t.Fatalf("%dx%d: %d lines", w, h, len(lines))
	}
	for i, l := range lines {
		if got := ansi.StringWidth(l); got > w {
			t.Fatalf("%dx%d: line %d is %d cells: %q", w, h, i, got, ansi.Strip(l))
		}
	}
}

// drawPopupLine returns the first plain line of s containing sub.
func drawPopupLine(s, sub string) string {
	for _, l := range drawPlainLines(s) {
		if strings.Contains(l, sub) {
			return l
		}
	}
	return ""
}

func drawTestQuestion(multi bool, cursor int, other string) *QuestionPopup {
	opts := []Option{
		{Label: "Division", Description: "Add a div function that raises on zero"},
		{Label: "Modulo", Description: "Add mod"},
		{Label: "Power"},
	}
	return &QuestionPopup{
		RequestID: "r1",
		Questions: []Question{
			{Header: "Feature", Text: "Which operation should calc.py support next?", Options: opts, Multi: multi,
				Cursor: cursor, Checked: []bool{true, false, false, false}, Other: Input{Text: []rune(other), Cursor: len([]rune(other))}},
			{Header: "Tests", Text: "Write tests?", Options: opts[:2]},
		},
	}
}

var drawPopupSizes = [][2]int{{36, 14}, {50, 18}, {76, 22}, {80, 46}}

func TestPopupQuestion(t *testing.T) {
	tests := []struct {
		name string
		p    *QuestionPopup
		want []string
		mark string // a row that must carry the "›" marker
	}{
		{"single", drawTestQuestion(false, 1, ""), []string{"Feature", "Question 1/2", "Which operation", "Division", "Modulo", "Power", "Other:", "enter select"}, "Modulo"},
		{"multi", drawTestQuestion(true, 0, ""), []string{"[x] Division", "[ ] Modulo", "[ ] Power", "[ ] Other:", "space toggle"}, "Division"},
		{"other highlighted", drawTestQuestion(false, 3, "my answer"), []string{"Other: my answer"}, "Other:"},
	}
	for _, tt := range tests {
		for _, sz := range drawPopupSizes {
			out := renderPopup(tt.p, sz[0], sz[1])
			drawCheckFits(t, out, sz[0], sz[1])
			for _, want := range tt.want {
				if drawPopupLine(out, want) == "" {
					t.Errorf("%s %v: missing %q in\n%s", tt.name, sz, want, ansi.Strip(out))
				}
			}
			if l := drawPopupLine(out, tt.mark); !strings.Contains(l, "› ") {
				t.Errorf("%s %v: row %q is not highlighted", tt.name, sz, l)
			}
		}
	}

	// The Other row draws its own block cursor only while highlighted.
	if out := renderPopup(drawTestQuestion(false, 3, "x"), 60, 20); !strings.Contains(out, "\x1b[7m") {
		t.Error("highlighted Other row has no reverse-video cursor")
	}
	if out := renderPopup(drawTestQuestion(false, 0, "x"), 60, 20); strings.Contains(out, "\x1b[7m") {
		t.Error("cursor drawn on an inactive Other row")
	}
	// Roomy boxes put descriptions under their labels.
	out := renderPopup(drawTestQuestion(false, 0, ""), 60, 30)
	if l := drawPopupLine(out, "Division"); strings.Contains(l, "raises") {
		t.Errorf("description should be under the label: %q", l)
	}
	second := drawTestQuestion(false, 0, "")
	second.Current = 1
	if drawPopupLine(renderPopup(second, 60, 20), "Question 2/2") == "" {
		t.Error("second question not shown")
	}
}

func TestPopupQuestionLong(t *testing.T) {
	p := drawTestQuestion(true, 3, strings.Repeat("long answer 你好 ", 20))
	q := &p.Questions[0]
	q.Text = strings.Repeat("A very long question that keeps going. ", 30)
	for i := range q.Options {
		q.Options[i].Description = strings.Repeat("Option description with details. ", 6)
	}
	q.Options = append(q.Options, Option{Label: strings.Repeat("Label ", 30), Description: "x"})
	q.Checked = append(q.Checked, false)
	q.Cursor = len(q.Options) // the Other row
	for _, sz := range drawPopupSizes {
		out := renderPopup(p, sz[0], sz[1])
		drawCheckFits(t, out, sz[0], sz[1])
		for _, want := range []string{"Question 1/2", "› [ ] Other:", "↑/↓ choose"} {
			if drawPopupLine(out, want) == "" {
				t.Errorf("%v: missing %q in\n%s", sz, want, ansi.Strip(out))
			}
		}
		if !strings.Contains(out, "\x1b[7m") {
			t.Errorf("%v: caret scrolled out of the Other field", sz)
		}
	}
	// Malformed input from the agent must not crash the UI.
	drawCheckFits(t, renderPopup(&QuestionPopup{}, 40, 14), 40, 14)
}

// A popup stands out in a white frame; the help keeps the grey of a frame in focus.
func TestPopupFrameWhite(t *testing.T) {
	out, _ := drawPopup(&PermissionPopup{Tool: "Bash", Detail: "ls", Allow: true}, 60, 20)
	white := lipgloss.NewStyle().Foreground(theme.Bright).Render("╭")
	if top := strings.Split(out, "\n")[0]; !strings.HasPrefix(top, white[:strings.Index(white, "╭")]+"╭") {
		t.Errorf("popup frame %q is not white", top)
	}
}

func TestPopupPermission(t *testing.T) {
	long := strings.Repeat("echo 'output line 世界'\n", 40)
	tests := []struct {
		name   string
		p      *PermissionPopup
		want   []string
		active string
	}{
		{"allow", &PermissionPopup{Tool: "Bash", Detail: "rm -rf build", Allow: true}, []string{"Allow Bash?", "rm -rf build", "[ Allow ]  [ Deny ]", "y allow · n/esc deny"}, "[ Allow ]"},
		{"deny", &PermissionPopup{Tool: "Write", Detail: "", Allow: false}, []string{"Allow Write?", "[ Allow ]  [ Deny ]"}, "[ Deny ]"},
		{"long detail", &PermissionPopup{Tool: "Bash", Detail: long, Allow: true}, []string{"Allow Bash?", "of 40 lines", "[ Allow ]", "scroll"}, "[ Allow ]"},
	}
	for _, tt := range tests {
		for _, sz := range drawPopupSizes {
			out := renderPopup(tt.p, sz[0], sz[1])
			drawCheckFits(t, out, sz[0], sz[1])
			for _, want := range tt.want {
				if drawPopupLine(out, want) == "" {
					t.Errorf("%s %v: missing %q in\n%s", tt.name, sz, want, ansi.Strip(out))
				}
			}
			if !strings.Contains(out, styleInverse.Render(tt.active)) {
				t.Errorf("%s %v: %s is not highlighted", tt.name, sz, tt.active)
			}
		}
	}

	// The detail takes the height there is and scrolls; the scroll stays in range.
	p := tests[2].p
	p.Scroll = 0
	out := renderPopup(p, 80, 46)
	if n := strings.Count(ansi.Strip(out), "echo"); n != 46-2-7 || drawPopupLine(out, "── 1–37 of 40 lines") == "" {
		t.Errorf("long detail shows %d lines:\n%s", n, ansi.Strip(out))
	}
	p.Scroll = 100
	if out := renderPopup(p, 80, 46); p.Scroll != 3 || drawPopupLine(out, "── 4–40 of 40 lines") == "" {
		t.Errorf("scrolled past the end: scroll %d\n%s", p.Scroll, ansi.Strip(out))
	}
}

// The detail reads as what it is: a diff in its colours, code highlighted, a plan as markdown.
func TestPopupDetail(t *testing.T) {
	diff := drawDetail("Edit", "calc.py\n def sub(a, b):\n-    return a - b\n+    return b - a", 40)
	if want := []string{styleStrong.Render("calc.py"), styleDim.Render(" def sub(a, b):"), styleFg.Background(theme.RemovedBg).Render("-    return a - b"), styleFg.Background(theme.AddedBg).Render("+    return b - a")}; strings.Join(diff, "\n") != strings.Join(want, "\n") {
		t.Errorf("diff %q", diff)
	}
	plan := ansi.Strip(strings.Join(drawDetail("ExitPlanMode", "# Plan\n\n- add `mul`", 40), "\n"))
	if plan != "Plan\n\n• add mul" {
		t.Errorf("plan %q", plan)
	}
	if got := drawDetail("Bash", "echo hi", 40)[0]; got != ideview.Highlight("echo hi", "sh") {
		t.Errorf("bash %q", got)
	}
}

// A subagent's question or permission request names the asker in a chip of its colour in the header.
func TestPopupAskedBy(t *testing.T) {
	q := drawTestQuestion(false, 0, "")
	q.AgentName, q.AgentColor = "tests", 1
	perm := &PermissionPopup{AgentName: "tests", AgentColor: 1, Tool: "Bash", Detail: "go test ./...", Allow: true}
	chip := lipgloss.NewStyle().Foreground(theme.Agents[1]).Reverse(true).Render(" asked by tests ")
	for _, tt := range []struct {
		p    Popup
		head string
	}{
		{q, "Feature   Question 1/2   asked by tests"},
		{perm, "Allow Bash?   asked by tests"},
	} {
		for _, sz := range drawPopupSizes {
			out := renderPopup(tt.p, sz[0], sz[1])
			drawCheckFits(t, out, sz[0], sz[1])
			if sz[0] >= 50 && !strings.Contains(drawPlainLines(out)[1], tt.head) {
				t.Errorf("%T %v: header %q", tt.p, sz, drawPlainLines(out)[1])
			}
		}
		if out := renderPopup(tt.p, 80, 30); !strings.Contains(out, chip) {
			t.Errorf("%T: no chip in the agent's colour:\n%s", tt.p, out)
		}
	}

	// The main agent asks without a chip.
	for _, p := range []Popup{drawTestQuestion(false, 0, ""), &PermissionPopup{Tool: "Bash"}} {
		if out := ansi.Strip(renderPopup(p, 80, 30)); strings.Contains(out, "asked by") {
			t.Errorf("%T: chip for the main agent:\n%s", p, out)
		}
	}
}

// A long answer to /btw scrolls; the question stays on top.
func TestPopupBtwScrolls(t *testing.T) {
	var answer []string
	for i := range 40 {
		answer = append(answer, fmt.Sprintf("line %d", i))
	}
	p := &BtwPopup{Question: "why?", Answer: strings.Join(answer, "\n"), Scroll: 5}
	box := ansi.Strip(renderPopup(p, 60, 20))
	for _, want := range []string{"/btw why?", "line 5", "6–", "of 40 lines", "↑/↓ pgup/pgdn scroll · esc close"} {
		if !strings.Contains(box, want) {
			t.Errorf("lacks %q:\n%s", want, box)
		}
	}
	if strings.Contains(box, "line 4\n") || lipgloss.Height(box) > 20 {
		t.Errorf("scrolled box:\n%s", box)
	}
}
