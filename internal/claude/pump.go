package claude

import (
	"sync"

	"github.com/pixelbender/claudit/internal/protocol"
)

// Pump hands the events to send in batches: what arrives while send is busy goes in the next
// call. claude's output is read as fast as it comes, however long the UI takes to draw a frame.
func Pump(events <-chan protocol.Event, send func([]protocol.Event)) {
	var mu sync.Mutex
	var batch []protocol.Event
	ready := make(chan struct{}, 1)
	go func() {
		defer close(ready)
		for ev := range events {
			mu.Lock()
			batch = append(batch, ev)
			mu.Unlock()
			select {
			case ready <- struct{}{}:
			default: // a wake-up is pending: it takes this event too
			}
		}
	}()
	for range ready {
		mu.Lock()
		evs := batch
		batch = nil
		mu.Unlock()
		if len(evs) > 0 { // a wake-up after its events went with the one before
			send(evs)
		}
	}
}
