package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pixelbender/claudit/internal/history"
	"github.com/pixelbender/claudit/internal/protocol"
)

// modelHistoryNew is a live model on a history file of its own, holding two earlier prompts of
// the project, one with a pasted text.
func modelHistoryNew(t *testing.T) (*Model, *[]protocol.Outbound, string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "history.jsonl")
	if err := os.WriteFile(file, []byte(
		`{"display":"earlier","pastedContents":{},"timestamp":1,"project":"/p"}`+"\n"+
			`{"display":"other project","pastedContents":{},"timestamp":2,"project":"/q"}`+"\n"+
			`{"display":"check [Pasted text #1 +1 lines]","pastedContents":{"1":{"id":1,"type":"text","content":"line one\nline two"}},"timestamp":3,"project":"/p"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, sent := modelLocalNew(t)
	m.UseHistory(file, "/p")
	return m, sent, file
}

// ↑ recalls this session's prompts first, then the project's earlier ones from Claude Code's history.
func TestModelHistoryRecall(t *testing.T) {
	m, _, _ := modelHistoryNew(t)
	modelLocalSay(m, "now")
	modelEvents(m, protocol.Result{})
	var seen []string
	for range 4 {
		m.Update(inputKey("up"))
		seen = append(seen, string(m.Input.Text))
	}
	if strings.Join(seen, " | ") != "Now | check [Pasted text #1 +1 lines] | earlier | earlier" {
		t.Fatalf("recalled %q", seen)
	}
}

// A recalled prompt sends its pasted text again, as Claude Code does.
func TestModelHistoryPasted(t *testing.T) {
	m, sent, _ := modelHistoryNew(t)
	m.Update(inputKey("up"))
	m.Update(inputKey("enter"))
	if text := modelBangSentText(t, sent); text != "Check line one\nline two" {
		t.Fatalf("sent %q", text)
	}
}

// What claudit sends goes to the history file, its pasted text too, a repeat once; not in a replay.
func TestModelHistoryWrites(t *testing.T) {
	m, _, file := modelHistoryNew(t)
	long := strings.Repeat("x\n", 30) + "end"
	m.paste(long)
	placeholder := string(m.Input.Text)
	modelLocalSay(m, "look "+placeholder)
	modelEvents(m, protocol.Result{})
	modelLocalSay(m, "look "+placeholder)
	got, err := history.Load(file, "/p", 10)
	if err != nil || len(got) != 3 || got[2].Text != "Look "+placeholder || got[2].Pasted[1] != long {
		t.Fatalf("%+v %v", got, err)
	}

	r, _ := modelNew(t, true)
	r.UseHistory(file, "/p")
	r.remember("replayed")
	if got, _ := history.Load(file, "/p", 10); len(got) != 3 {
		t.Fatalf("a replay wrote: %+v", got)
	}
}

func TestPastedNumber(t *testing.T) {
	for p, want := range map[string]int{"[Pasted text #12 +3 lines]": 12, "[Pasted text #7]": 7} {
		if n, ok := pastedNumber(p); !ok || n != want {
			t.Errorf("%q: %d %v", p, n, ok)
		}
	}
	if _, ok := pastedNumber("[Image #2]"); ok {
		t.Error("a picture taken for a text")
	}
}

// modelBangSentText is the text of the only message sent.
func modelBangSentText(t *testing.T, sent *[]protocol.Outbound) string {
	t.Helper()
	if len(*sent) != 1 {
		t.Fatalf("sent %#v", *sent)
	}
	return (*sent)[0].(protocol.UserMessage).Text
}

// A prompt of this session keeps its placeholder; an earlier one whose number this session uses
// for another text gets a placeholder of its own.
func TestModelHistoryPlaceholders(t *testing.T) {
	m, sent, _ := modelHistoryNew(t)
	long := strings.Repeat("y\n", 30) + "mine"
	m.paste(long)
	mine := string(m.Input.Text) // "[Pasted text #1 +30 lines]": the number the earlier prompt has too
	modelLocalSay(m, "own "+mine)
	modelEvents(m, protocol.Result{})
	m.Update(inputKey("up"))
	if got := string(m.Input.Text); got != "Own "+mine {
		t.Fatalf("own: %q", got)
	}
	m.Update(inputKey("up"))
	got := string(m.Input.Text)
	if !strings.HasPrefix(got, "check [Pasted text #2 ") {
		t.Fatalf("earlier: %q", got)
	}
	*sent = nil
	m.Update(inputKey("enter"))
	if text := modelBangSentText(t, sent); text != "Check line one\nline two" {
		t.Fatalf("sent %q", text)
	}
}

// An earlier prompt whose placeholder this session holds for another text gets one of its own.
func TestModelHistorySamePlaceholder(t *testing.T) {
	m, _ := modelLocalNew(t)
	m.attached["[Pasted text #1 +1 lines]"] = attachment{text: "mine\ntoo"}
	got := m.recalled(history.Entry{Text: "see [Pasted text #1 +1 lines]", Pasted: map[int]string{1: "theirs\nas well"}})
	if got != "see [Pasted text #2 +1 lines]" || m.attached["[Pasted text #2 +1 lines]"].text != "theirs\nas well" {
		t.Fatalf("%q %+v", got, m.attached)
	}
}

// A picture of an earlier prompt, or a text the history lost, stays a placeholder.
func TestModelHistoryLost(t *testing.T) {
	m, _ := modelLocalNew(t)
	if got := m.recalled(history.Entry{Text: "see [Image #4] [Pasted text #2]"}); got != "see [Image #4] [Pasted text #2]" {
		t.Fatalf("%q", got)
	}
}
