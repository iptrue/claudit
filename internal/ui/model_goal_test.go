package ui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/pixelbender/claudit/internal/protocol"
	"github.com/pixelbender/claudit/internal/theme"
)

func TestGoalAppearsImmediately(t *testing.T) {
	for _, busy := range []bool{false, true} {
		t.Run(fmt.Sprintf("busy=%v", busy), func(t *testing.T) {
			m, sent := modelMouseNew(t)
			m.addCommands([]protocol.Command{{Name: "goal"}})
			m.Status.Busy = busy
			m.View()
			modelPress(m, inputTyped("/goal поправь  тесты")...)
			m.View()
			modelPress(m, inputKey("enter"))
			out := m.View().Content
			want := lipgloss.NewStyle().Foreground(theme.Orange).Render("› Поправь тесты")
			if !strings.Contains(out, want) || strings.Contains(ansi.Strip(out), "Goal set:") {
				t.Fatalf("goal is not immediately visible and capitalized:\n%s", ansi.Strip(out))
			}
			if busy {
				if len(*sent) != 0 {
					t.Fatal("a queued goal was sent before the current turn ended")
				}
				modelEvents(m, protocol.Result{})
			}
			if len(*sent) != 1 || modelUserText((*sent)[0]) != "/goal поправь тесты" {
				t.Fatalf("sent %#v", *sent)
			}
			modelEvents(m, protocol.CLIReply{Text: "Goal set: поправь тесты"})
			if out := ansi.Strip(m.View().Content); strings.Count(out, "› Поправь тесты") != 1 {
				t.Fatalf("goal confirmation duplicated or hid the request:\n%s", out)
			}
			modelEvents(m, protocol.Result{})
			if len(m.Chat) != 1 || m.Chat[0].Kind != CommandEntry {
				t.Fatalf("goal changed command-turn accounting: %+v", m.Chat)
			}
		})
	}
}

func TestGoalControlsStayCommands(t *testing.T) {
	for _, args := range []string{"", "clear", "stop", "off", "reset", "none", "cancel"} {
		m, sent := modelMouseNew(t)
		m.addCommands([]protocol.Command{{Name: "goal"}})
		text := strings.TrimSpace("/goal " + args)
		modelPress(m, inputTyped(text)...)
		modelPress(m, inputKey("enter"))
		if len(m.Chat) != 0 || len(*sent) != 1 || modelUserText((*sent)[0]) != text {
			t.Errorf("control %q: chat=%+v sent=%+v", text, m.Chat, *sent)
		}
	}
}
