package claude

import (
	"slices"
	"testing"
	"time"

	"github.com/pixelbender/claudit/internal/protocol"
)

// While the UI is busy with a batch, the events go on being read, and all of them arrive in order.
func TestPump(t *testing.T) {
	events := make(chan protocol.Event) // unbuffered: a send blocks until Pump reads it
	busy := make(chan struct{})
	var got [][]protocol.Event
	done := make(chan struct{})
	go func() {
		defer close(done)
		Pump(events, func(evs []protocol.Event) {
			if got = append(got, evs); len(got) == 1 {
				<-busy // the UI draws a slow frame
			}
		})
	}()

	var want []protocol.Event
	for i := range 100 {
		ev := protocol.TextDelta{Text: string(rune('a' + i%26))}
		want = append(want, ev)
		select {
		case events <- ev:
		case <-time.After(time.Second):
			t.Fatalf("event %d is not read while the UI is busy", i)
		}
	}
	close(busy)
	close(events)
	<-done
	if all := slices.Concat(got...); !slices.Equal(all, want) {
		t.Errorf("events arrived as %v", got)
	}
	if len(got) > 3 {
		t.Errorf("%d batches for events that waited on one frame", len(got))
	}
}
