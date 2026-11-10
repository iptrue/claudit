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
	got := m.recalled(history.Entry{Text: "see [Pasted text #1 +1 lines]", Pasted: map[int]string{1: "theirs\nas well"}}, true)
	if got != "see [Pasted text #2 +1 lines]" || m.attached["[Pasted text #2 +1 lines]"].text != "theirs\nas well" {
		t.Fatalf("%q %+v", got, m.attached)
	}
}

// A picture of an earlier session's prompt, or a text the history lost, stays a placeholder, under
// a number of its own: what this session attached under its number does not go with it.
func TestModelHistoryLost(t *testing.T) {
	m, sent := modelLocalNew(t)
	m.attach(attachment{text: "mine\ntoo"})
	m.attach(attachment{image: &protocol.Image{Media: "image/png", Data: []byte{0}}})
	got := m.recalled(history.Entry{Text: "see [Image #2] [Pasted text #1 +1 lines]"}, true)
	if got != "see [Image #3] [Pasted text #4 +1 lines]" {
		t.Fatalf("%q", got)
	}
	modelLocalSay(m, got)
	if msg := (*sent)[0].(protocol.UserMessage); msg.Text != "See [Image #3] [Pasted text #4 +1 lines]" || len(msg.Images) != 0 {
		t.Fatalf("sent %+v", msg)
	}
	if e := m.history[len(m.history)-1]; e.Pasted != nil {
		t.Errorf("a lost placeholder kept: %+v", e)
	}
	if a := m.attached["[Pasted text #4 +1 lines]"]; a.text != "[Pasted text #4 +1 lines]" {
		t.Errorf("a lost placeholder stands for %q", a.text)
	}
}

// A picture of this session's prompt is the one it had.
func TestModelHistoryOwnPicture(t *testing.T) {
	m, sent := modelLocalNew(t)
	p := m.attach(attachment{image: &protocol.Image{Media: "image/png", Data: []byte{0}}})
	got := m.recalled(history.Entry{Text: "see " + p}, false)
	modelLocalSay(m, got)
	if msg := (*sent)[0].(protocol.UserMessage); got != "see [Image #1]" || len(msg.Images) != 1 {
		t.Fatalf("%q %+v", got, msg)
	}
}

// A prompt that repeats the last one with other pasted texts is a prompt of its own; with the same, once.
func TestModelHistoryRepeatPasted(t *testing.T) {
	m, _ := modelLocalNew(t)
	m.history = []history.Entry{{Text: "see [Pasted text #1 +3 lines]", Pasted: map[int]string{1: "old\n\n\n"}}}
	m.attached["[Pasted text #1 +3 lines]"] = attachment{text: "new\n\n\n"}
	m.remember("see [Pasted text #1 +3 lines]")
	m.remember("see [Pasted text #1 +3 lines]")
	if len(m.history) != 2 || m.history[1].Pasted[1] != "new\n\n\n" {
		t.Fatalf("%+v", m.history)
	}
}

// ↑ to an earlier session's prompt with a picture does not bring this session's picture of its number.
func TestModelHistoryRecallLost(t *testing.T) {
	file := filepath.Join(t.TempDir(), "history.jsonl")
	if err := os.WriteFile(file, []byte(`{"display":"see [Image #1]","pastedContents":{},"timestamp":1,"project":"/p"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, _ := modelLocalNew(t)
	m.UseHistory(file, "/p")
	m.attach(attachment{image: &protocol.Image{Media: "image/png", Data: []byte{0}}})
	m.Update(inputKey("up"))
	if got := string(m.Input.Text); got != "see [Image #2]" {
		t.Fatalf("%q", got)
	}
}
