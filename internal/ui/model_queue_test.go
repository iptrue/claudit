package ui

import (
	"slices"
	"testing"
	"time"

	"github.com/pixelbender/claudit/internal/protocol"
)

func TestModelQueueForwardsAfterQuietPeriod(t *testing.T) {
	m, sent := modelUnqueueNew(t, "Next", "And this")
	m.Status.State = "running"
	m.Update(tickMsg(modelT0.Add(4999 * time.Millisecond)))
	if len(*sent) != 0 {
		t.Fatalf("forwarded too early: %#v", *sent)
	}
	m.Update(tickMsg(modelT0.Add(5 * time.Second)))
	if len(*sent) != 1 || modelUserText((*sent)[0]) != "Next\n\nAnd this" || len(m.pending) != 0 {
		t.Fatalf("sent %#v, pending %q", *sent, m.pending)
	}
	if !m.Status.Busy || m.Status.State != "running" || len(m.Chat) != 2 || m.Chat[0].Text != "Next" || m.Chat[1].Text != "And this" {
		t.Fatalf("status %+v, chat %+v", m.Status, m.Chat)
	}
	m.Update(tickMsg(modelT0.Add(10 * time.Second)))
	modelEvents(m, protocol.Result{})
	if len(*sent) != 1 {
		t.Fatalf("forwarded twice: %#v", *sent)
	}
}

func TestModelQueueNewPromptRestartsQuietPeriod(t *testing.T) {
	m, sent := modelUnqueueNew(t, "Next")
	modelAt(m, 4*time.Second)
	modelLocalSay(m, "And this")
	m.Update(tickMsg(modelT0.Add(5 * time.Second)))
	if len(*sent) != 0 {
		t.Fatalf("new prompt did not restart the timer: %#v", *sent)
	}
	m.Update(tickMsg(modelT0.Add(9 * time.Second)))
	if len(*sent) != 1 || modelUserText((*sent)[0]) != "Next\n\nAnd this" {
		t.Fatalf("sent %#v", *sent)
	}
}

func TestModelQueueForwardKeepsStreamingReplyWhole(t *testing.T) {
	m, sent := modelUnqueueNew(t, "Next", "And this")
	modelEvents(m, protocol.TextStart{}, protocol.TextDelta{Text: "```go\nfunc main() {"})
	m.Update(tickMsg(modelT0.Add(5 * time.Second)))
	modelEvents(m, protocol.TextDelta{Text: "}\n```"})
	if len(*sent) != 1 || modelUserText((*sent)[0]) != "Next\n\nAnd this" {
		t.Fatalf("sent %#v", *sent)
	}
	if len(m.Chat) != 3 || m.Chat[0].Text != "Next" || m.Chat[1].Text != "And this" ||
		m.Chat[2].Kind != AssistantEntry || m.Chat[2].Text != "```go\nfunc main() {}\n```" {
		t.Fatalf("streaming reply split by queued prompts: %+v", m.Chat)
	}
}

func TestModelQueueEditingHoldsWholeQueue(t *testing.T) {
	m, sent := modelUnqueueNew(t, "Next", "And this")
	modelAt(m, 4*time.Second)
	modelPress(m, inputKey("up"))
	m.Update(tickMsg(modelT0.Add(10 * time.Second)))
	if len(*sent) != 0 || string(m.Input.Text) != "And this" {
		t.Fatalf("sent %#v, editing %q", *sent, string(m.Input.Text))
	}
	modelAt(m, 10*time.Second)
	modelPress(m, inputTyped(" too")...)
	modelPress(m, inputKey("enter"))
	m.Update(tickMsg(modelT0.Add(14999 * time.Millisecond)))
	if len(*sent) != 0 {
		t.Fatalf("saved edit did not restart the timer: %#v", *sent)
	}
	m.Update(tickMsg(modelT0.Add(15 * time.Second)))
	if len(*sent) != 1 || modelUserText((*sent)[0]) != "Next\n\nAnd this too" {
		t.Fatalf("sent %#v", *sent)
	}
}

func TestModelQueueDraftDoesNotRestartQuietPeriod(t *testing.T) {
	m, sent := modelUnqueueNew(t, "Next")
	modelAt(m, 4*time.Second)
	modelPress(m, inputTyped("Unsent draft")...)
	m.Update(tickMsg(modelT0.Add(5 * time.Second)))
	if len(*sent) != 1 || modelUserText((*sent)[0]) != "Next" || string(m.Input.Text) != "Unsent draft" {
		t.Fatalf("sent %#v, draft %q", *sent, string(m.Input.Text))
	}
}

func TestModelQueueTimerKeepsSlashBoundary(t *testing.T) {
	m, sent := modelUnqueueNew(t, "Next", "/compact", "Later")
	m.Update(tickMsg(modelT0.Add(5 * time.Second)))
	if len(*sent) != 1 || modelUserText((*sent)[0]) != "Next" || !slices.Equal(m.pending, []string{"/compact", "Later"}) {
		t.Fatalf("sent %#v, pending %q", *sent, m.pending)
	}
	m.Update(tickMsg(modelT0.Add(10 * time.Second)))
	if len(*sent) != 1 {
		t.Fatalf("command sent mid-turn: %#v", *sent)
	}
	modelEvents(m, protocol.Result{})
	if len(*sent) != 2 || modelUserText((*sent)[1]) != "/compact" {
		t.Fatalf("sent %#v", *sent)
	}
	m.Update(tickMsg(modelT0.Add(15 * time.Second)))
	if len(*sent) != 2 {
		t.Fatalf("prompt crossed the command's turn: %#v", *sent)
	}
	modelEvents(m, protocol.Result{})
	if len(*sent) != 3 || modelUserText((*sent)[2]) != "Later" {
		t.Fatalf("sent %#v", *sent)
	}
}

func TestModelQueueTimerDoesNotSendToUnavailableSession(t *testing.T) {
	for _, state := range []string{"exited", "replay"} {
		t.Run(state, func(t *testing.T) {
			m, sent := modelUnqueueNew(t, "Next")
			if state == "exited" {
				modelEvents(m, protocol.Exited{})
			} else {
				m.Status.Replay = true
			}
			m.Update(tickMsg(modelT0.Add(10 * time.Second)))
			if len(*sent) != 0 || !slices.Equal(m.pending, []string{"Next"}) {
				t.Fatalf("sent %#v, pending %q", *sent, m.pending)
			}
		})
	}
}
