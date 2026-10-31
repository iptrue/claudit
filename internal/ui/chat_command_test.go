package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Replies as `claude -p` sends them (trimmed).
const (
	drawUsageReply = "You are currently using your subscription to power your Claude Code usage\n\n" +
		"Current session: 1% used · resets Oct 30 at 12:39pm (Etc/UTC)\n" +
		"Current week (all models): 64% used · resets Nov 3 at 1:59am (Etc/UTC)\n\n" +
		"What's contributing to your usage?\nApproximate · sessions on this machine only · shares overlap\n\n" +
		"Last 24h · 1593 requests · 15 sessions\n  69%  Start fresh when you switch tasks · run /clear\n  Top skills: /init 2%"
	drawContextReply = "## Context Usage\n\n**Model:** claude-haiku-5-5  \n**Tokens:** 15.8k / 1m (2%)\n\n" +
		"### Estimated usage by category\n\n| Category | Tokens | Percentage |\n|----------|--------|------------|\n" +
		"| System prompt | 3.3k | 0.3% |\n| System tools (deferred) | 13.5k | 1.3% |\n| Messages | 10 | 0.0% |\n" +
		"| Free space | 951.2k | 95.1% |\n| Autocompact buffer | 33k | 3.3% |\n\n" +
		"### Memory Files\n\n| Type | Path | Tokens |\n|------|------|--------|\n| User | ~/.claude/CLAUDE.md | 1.5k |"
	drawColumnsReply = "Skills loaded this session\n\n" +
		"  skill                                  source          context  7d tokens   uses  last used\n" +
		"  animate                                userSettings       ~160          -     0×  never\n" +
		"  micelio-go-service                     userSettings       ~450          -     2×  107 days"
	drawConfigReply = "Usage: /config key=value [key=value ...]\n  autoCompact=true|false\n  theme=auto|dark|light"
)

// drawCommandPlain renders a reply and strips its styles.
func drawCommandPlain(text string, w int) []string {
	return drawPlainLines(strings.Join(drawCommand(text, w), "\n"))
}

// Usage limits become meters: name, bar, share, reset time.
func TestChatCommandUsage(t *testing.T) {
	lines := drawCommandPlain(drawUsageReply, 80)
	want := []string{
		"You are currently using your subscription to power your Claude Code usage",
		"",
		"Current session",
		"▌" + strings.Repeat("░", 49) + " 1% used",
		"Resets Oct 30 at 12:39pm (Etc/UTC)",
		"",
		"Current week (all models)",
		strings.Repeat("█", 32) + strings.Repeat("░", 18) + " 64% used",
		"Resets Nov 3 at 1:59am (Etc/UTC)",
		"",
		"What's contributing to your usage?",
		"Approximate · sessions on this machine only · shares overlap",
		"",
		"Last 24h · 1593 requests · 15 sessions",
		"  69%  Start fresh when you switch tasks · run /clear",
		"  Top skills: /init 2%",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	// No room for a bar: the share alone.
	if lines := drawCommandPlain("Current session: 7% used", 8); lines[1] != "7% used" {
		t.Errorf("narrow meter %q", lines)
	}
}

// The context breakdown becomes a grid of cells with a legend beside it, or below when narrow.
func TestChatCommandContext(t *testing.T) {
	lines := drawCommandPlain(drawContextReply, 100)
	at := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "⛀ ⛁ ⛁ ⛀ ⛶") {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("no grid:\n%s", strings.Join(lines, "\n"))
	}
	grid := lines[at : at+10]
	for _, l := range grid {
		if cells := strings.Fields(ansi.Truncate(l, 39, "")); len(cells) != 20 {
			t.Errorf("grid row of %d cells: %q", len(cells), l)
		}
	}
	// 3.3% of 200 cells is 7 cells of buffer, at the very end.
	if !strings.HasSuffix(ansi.Truncate(grid[9], 39, ""), strings.Repeat(" ⛝", 7)) || strings.Count(strings.Join(grid, ""), "⛝") != 7+1 {
		t.Errorf("buffer cells: %q", grid[9])
	}
	if !strings.Contains(grid[0], "⛁ System prompt") || !strings.Contains(grid[0], "3.3k   0.3%") || !strings.Contains(grid[3], "⛶ Free space") {
		t.Errorf("legend:\n%s", strings.Join(grid, "\n"))
	}
	text := strings.Join(lines, "\n")
	for _, s := range []string{"Context Usage", "Model: claude-haiku-5-5", "Memory Files", "User │ ~/.claude/CLAUDE.md"} {
		if !strings.Contains(text, s) {
			t.Errorf("missing %q in\n%s", s, text)
		}
	}
	// Narrow: ten cells a row, the legend under the grid.
	lines = drawCommandPlain(drawContextReply, 40)
	text = strings.Join(lines, "\n")
	if !strings.Contains(text, "⛀ ⛁ ⛀ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶ ⛶\n") || !strings.Contains(text, "\n⛁ System prompt") {
		t.Errorf("narrow grid:\n%s", text)
	}
}

// Aligned columns stay aligned: cut at the edge, not wrapped; the header is the first row.
func TestChatCommandColumns(t *testing.T) {
	lines := drawCommandPlain(drawColumnsReply, 40)
	if len(lines) != 5 {
		t.Fatalf("got %d lines:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if lines[3] != "  animate                              …" {
		t.Errorf("row %q", lines[3])
	}
	lines = drawCommandPlain(drawConfigReply, 80)
	if lines[1] != "  autoCompact=true|false" || lines[2] != "  theme=auto|dark|light" {
		t.Errorf("settings %q", lines)
	}
}

// An API error reads as one.
func TestChatCommandError(t *testing.T) {
	if s := drawCommand("API Error: 529 overloaded", 40)[0]; !strings.Contains(s, "248;81;73") {
		t.Errorf("not red: %q", s)
	}
}

// The output follows the command behind a "›" as a reply does, and fits any width.
func TestChatCommandEntry(t *testing.T) {
	entries := []ChatEntry{{Kind: UserEntry, Text: "/usage"}, {Kind: CommandEntry, Text: drawUsageReply}}
	out, _, _ := renderChat(new(chatCache), entries, 90, 40, 0)
	lines := drawPlainLines(out)
	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "  › /usage\n  › You are currently using") || !strings.Contains(text, "\n    Current session\n") {
		t.Errorf("got\n%s", text)
	}
	all := append([]ChatEntry{}, entries...)
	for _, r := range []string{drawContextReply, drawColumnsReply, drawConfigReply, "API Error: 529"} {
		all = append(all, ChatEntry{Kind: CommandEntry, Text: r})
	}
	for _, w := range []int{1, 2, 5, 9, 13, 20, 40, 80, 133} {
		for _, h := range []int{1, 7, 80} {
			out, _, _ := renderChat(new(chatCache), all, w, h, 0)
			drawCheckSize(t, out, w, h)
		}
	}
}
