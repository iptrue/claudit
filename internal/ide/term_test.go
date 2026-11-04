package ide

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTermTruncatesHugeLine(t *testing.T) {
	var term Term
	term.PrintLine(TermLine{Kind: ErrorOutput, Text: strings.Repeat("界", 10000), Call: "call"})
	l := term.Lines[0]
	if len(l.Text) > 16<<10 || !utf8.ValidString(l.Text) || !strings.HasSuffix(l.Text, "… [truncated]") {
		t.Fatalf("huge line: bytes=%d valid=%v suffix=%q", len(l.Text), utf8.ValidString(l.Text), l.Text[max(0, len(l.Text)-20):])
	}
	if l.Kind != ErrorOutput || l.Call != "call" {
		t.Fatal("truncation lost line metadata")
	}
}

func TestTermBoundsHistoryBytes(t *testing.T) {
	var term Term
	for range 2000 {
		term.Print(Output, strings.Repeat("x", 8192))
	}
	term.Print(Output, "latest")
	bytes := 0
	for _, l := range term.Lines {
		bytes += len(l.Text)
	}
	if bytes > 10<<20 || len(term.Lines) != 1280 || term.Lines[len(term.Lines)-1].Text != "latest" {
		t.Fatalf("history: bytes=%d lines=%d", bytes, len(term.Lines))
	}
	// A copied terminal owns its slice; printing into it must not affect the original accounting.
	copy := term
	copy.Lines = append([]TermLine(nil), term.Lines...)
	copy.Print(Output, strings.Repeat("y", 8192))
	term.Print(Output, "last")
	if copy.Lines[len(copy.Lines)-1].Text == "last" || term.Lines[len(term.Lines)-1].Text != "last" {
		t.Fatal("printing into a copy changed the original terminal")
	}
}
