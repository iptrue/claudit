package sim

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/pixelbender/claudit/internal/ide"
)

// act queues a scene that keeps the lane of key at score for d from the Tick that starts it.
func act(s *Sim, key string, score int, d time.Duration) {
	l := s.lanes[s.Agent(key, ide.Subagent)]
	s.seq++
	l.queue = append(l.queue, &scene{id: fmt.Sprint("act", s.seq), seq: s.seq, l: l, build: func(sc *scene) {
		sc.score = score
		sc.add(d, func() {})
	}})
}

// cut is a camera move: when and to whom.
type cut struct {
	at  time.Duration
	key string
}

// shoot ticks every 10 ms over [from, to) and records the camera moves.
func shoot(s *Sim, from, to time.Duration) []cut {
	var cuts []cut
	for at := from; at < to; at += 10 * time.Millisecond {
		was := s.State().Camera
		moved := s.Tick(t0.Add(at))
		if cam := s.State().Camera; cam != was {
			if !moved {
				return append(cuts, cut{at, "Tick reported no change"})
			}
			cuts = append(cuts, cut{at, cam})
		}
	}
	return cuts
}

func TestCamera(t *testing.T) {
	s, _ := setup(t, nil)
	var cuts []cut
	cuts = append(cuts, shoot(s, 0, 100*time.Millisecond)...)
	act(s, "sub", 2, 5*time.Second) // main has been idle since 0: cut at 400 ms
	cuts = append(cuts, shoot(s, 100*time.Millisecond, 600*time.Millisecond)...)
	act(s, "main", 3, 5*time.Second) // more interesting, but sub is busy: cut 2.5 s after the last one
	cuts = append(cuts, shoot(s, 600*time.Millisecond, 3000*time.Millisecond)...)
	act(s, "t", 3, 5*time.Second) // a tie keeps main until main goes idle at 5.6 s
	cuts = append(cuts, shoot(s, 3000*time.Millisecond, 20*time.Second)...)
	want := []cut{{400 * time.Millisecond, "sub"}, {2900 * time.Millisecond, "main"}, {5600 * time.Millisecond, "t"}}
	if !reflect.DeepEqual(cuts, want) {
		t.Fatalf("cuts %v, want %v", cuts, want)
	}
	if s.Busy() || s.State().Camera != "t" {
		t.Fatal("with nothing going on the camera stays put")
	}

	// Pinned, the camera never moves; back to auto, it cuts at once.
	s.Pin("sub")
	act(s, "main", 3, 2*time.Second)
	if cuts := shoot(s, 20*time.Second, 21*time.Second); cuts != nil || s.State().Camera != "sub" || !s.State().Pinned {
		t.Fatalf("pinned: cuts %v camera %q", cuts, s.State().Camera)
	}
	s.Pin("nope")
	if s.State().Camera != "sub" || !s.State().Pinned {
		t.Fatal("pinning an unknown agent must do nothing")
	}
	s.Pin("")
	if cuts := shoot(s, 21*time.Second, 22*time.Second); !reflect.DeepEqual(cuts, []cut{{21 * time.Second, "main"}}) || s.State().Pinned {
		t.Fatalf("auto again: cuts %v", cuts)
	}
}

// The agent on camera may pause for less than 0.4 s without losing the camera.
func TestCameraIdleGrace(t *testing.T) {
	t.Run("short pause", func(t *testing.T) {
		s, _ := setup(t, nil)
		act(s, "main", 3, 200*time.Millisecond)
		act(s, "sub", 2, 3*time.Second)
		cuts := shoot(s, 0, 400*time.Millisecond)
		act(s, "main", 3, time.Second) // back at work after 200 ms idle
		cuts = append(cuts, shoot(s, 400*time.Millisecond, 3*time.Second)...)
		// main idle again at 1.4 s: the camera leaves it 0.4 s later
		if want := []cut{{1800 * time.Millisecond, "sub"}}; !reflect.DeepEqual(cuts, want) {
			t.Fatalf("cuts %v, want %v", cuts, want)
		}
	})
	t.Run("long pause", func(t *testing.T) {
		s, _ := setup(t, nil)
		act(s, "main", 3, 200*time.Millisecond)
		act(s, "sub", 2, 3*time.Second)
		if cuts, want := shoot(s, 0, 3*time.Second), []cut{{600 * time.Millisecond, "sub"}}; !reflect.DeepEqual(cuts, want) {
			t.Fatalf("cuts %v, want %v", cuts, want)
		}
	})
}

// An agent that has finished and gone idle hands the camera back to main; a running one keeps it.
func TestCameraFinishedAgent(t *testing.T) {
	s, _ := setup(t, nil)
	act(s, "sub", 2, time.Second)
	act(s, "run", 2, time.Second)
	cuts := shoot(s, 0, 3*time.Second) // sub at 400 ms; run is a tie
	s.FindAgent("run").Status = ide.Done
	cuts = append(cuts, shoot(s, 3*time.Second, 4*time.Second)...)
	s.FindAgent("sub").Status = ide.Failed
	cuts = append(cuts, shoot(s, 4*time.Second, 5*time.Second)...)
	if want := []cut{{400 * time.Millisecond, "sub"}, {4 * time.Second, "main"}}; !reflect.DeepEqual(cuts, want) {
		t.Fatalf("cuts %v, want %v", cuts, want)
	}
}

// The scores of real scenes: code 3, commands 2, explorer, scanning and waiting 1, idle 0.
func TestScores(t *testing.T) {
	files := map[string]string{"z.txt": "one\ntwo\nthree\n"}
	for i := range 5 {
		files[fmt.Sprintf("a%d.txt", i)] = ""
	}
	tests := []struct {
		name  string
		input map[string]any
		want  []int
	}{
		{"Bash", map[string]any{"command": "go test"}, []int{2, 1, 2, 0}},
		{"Read", map[string]any{"file_path": "z.txt"}, []int{1, 0}},
		{"Edit", editInput("z.txt", "two", "deux", false), []int{1, 3, 0}},
		{"Write", map[string]any{"file_path": "z.txt", "content": "new"}, []int{1, 3, 0}},
		{"Glob", map[string]any{"pattern": "*.txt"}, []int{2, 1, 2, 0}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, root := setup(t, files)
			if p, ok := tc.input["file_path"].(string); ok {
				tc.input["file_path"] = filepath.Join(root, p)
			}
			s.ToolUse("main", "x", tc.name, tc.input)
			var got []int
			see := func() {
				if sc := s.score(mainAgent(s)); len(got) == 0 || got[len(got)-1] != sc {
					got = append(got, sc)
				}
			}
			now := watch(s, t0, 3*time.Second, see) // a command waits for its result
			s.ToolResult("x", "a\nb\nc", false)
			watch(s, now, 5*time.Second, see)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("scores %v, want %v", got, tc.want)
			}
		})
	}
}

// The agent waiting for the user's answer gets the camera at once, unless the camera is pinned.
func TestCameraAsks(t *testing.T) {
	s := New(t.TempDir())
	s.Agent("sub", ide.Subagent)
	s.Tick(t0)
	s.Ask("sub")
	if s.Tick(t0.Add(frame)); s.State().Camera != "sub" {
		t.Fatalf("camera on %q", s.State().Camera)
	}
	s.Pin("main")
	if s.Tick(t0.Add(2 * frame)); s.State().Camera != "main" {
		t.Fatalf("pinned camera moved to %q", s.State().Camera)
	}
}
