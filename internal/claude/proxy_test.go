package claude

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pixelbender/claudit/internal/protocol"
)

// limitsHeaders are the rate-limit headers of a response: 25% of the session used, 60% of the week.
func limitsHeaders(h http.Header) {
	h.Set("anthropic-ratelimit-unified-status", "allowed")
	h.Set("anthropic-ratelimit-unified-5h-utilization", "0.25")
	h.Set("anthropic-ratelimit-unified-5h-reset", "1793311200")
	h.Set("anthropic-ratelimit-unified-7d-utilization", "0.6")
	h.Set("anthropic-ratelimit-unified-7d-reset", "1793660400")
}

var limitsWant = protocol.RateLimits{
	Session: &protocol.Window{Used: 0.25, ResetsAt: time.Unix(1793311200, 0)},
	Week:    &protocol.Window{Used: 0.6, ResetsAt: time.Unix(1793660400, 0)},
}

// The windows are read as claude reads them; one without a number is left out, and a response
// without any reports nothing.
func TestHeaderLimits(t *testing.T) {
	h := http.Header{}
	limitsHeaders(h)
	h.Set("anthropic-ratelimit-unified-7d_oi-utilization", "1.2") // no reset
	if got, ok := headerLimits(h); !ok || !reflect.DeepEqual(got, limitsWant) {
		t.Errorf("limits %+v %v, want %+v", got, ok, limitsWant)
	}
	h.Set("anthropic-ratelimit-unified-7d_oi-reset", "1793660400")
	if got, _ := headerLimits(h); got.Fable == nil || got.Fable.Used != 1.2 {
		t.Errorf("fable window %+v", got.Fable)
	}
	if got, ok := headerLimits(http.Header{"Anthropic-Ratelimit-Unified-Status": {"allowed"}}); ok {
		t.Errorf("no windows, yet %+v", got)
	}
}

// The proxy passes the call on as it is, under the upstream's path, streams the reply as it comes
// and reports the limits of its headers.
func TestLimitsProxy(t *testing.T) {
	release := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gw/v1/messages" || r.URL.RawQuery != "beta=true" || r.Header.Get("Authorization") != "Bearer t" {
			http.Error(w, r.URL.String(), http.StatusTeapot)
			return
		}
		limitsHeaders(w.Header())
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: one\n\n")
		w.(http.Flusher).Flush()
		<-release
		io.WriteString(w, "data: two\n\n")
	}))
	defer up.Close()
	upstream, err := url.Parse(up.URL + "/gw")
	if err != nil {
		t.Fatal(err)
	}
	reports := make(chan protocol.RateLimits, 4)
	base, err := limitsProxy(t.Context(), upstream, func(l protocol.RateLimits) { reports <- l })
	if err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest("POST", base+"/v1/messages?beta=true", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer t")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := bufio.NewReader(resp.Body)
	first := make(chan string, 1)
	go func() {
		l, _ := body.ReadString('\n')
		first <- l
	}()
	select {
	case l := <-first:
		if l != "data: one\n" {
			close(release)
			t.Fatalf("status %d, first line %q", resp.StatusCode, l)
		}
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("the stream is held back until the upstream ends it")
	}
	close(release)
	if rest, _ := io.ReadAll(body); string(rest) != "\ndata: two\n\n" {
		t.Errorf("rest %q", rest)
	}
	select {
	case l := <-reports:
		if !reflect.DeepEqual(l, limitsWant) {
			t.Errorf("reported %+v, want %+v", l, limitsWant)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no limits reported")
	}
}

// With ANTHROPIC_AUTH_TOKEN claude goes to the API through the proxy, and the limits of the
// responses come among its events.
func TestSpawnLimitsProxy(t *testing.T) {
	seen := make(chan string, 1)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Get("Authorization")
		limitsHeaders(w.Header())
	}))
	defer up.Close()
	dir := t.TempDir()
	fake := "#!/bin/sh\ncurl -s -o /dev/null -H \"Authorization: Bearer $ANTHROPIC_AUTH_TOKEN\" \"$ANTHROPIC_BASE_URL/v1/messages\"\n"
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "t")
	t.Setenv("ANTHROPIC_BASE_URL", up.URL)

	sess, err := Spawn(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []protocol.Event
	for {
		ev := next(t, sess.Events)
		if _, ok := ev.(protocol.Exited); ok {
			break
		}
		got = append(got, ev)
	}
	if len(got) != 1 || !reflect.DeepEqual(got[0], limitsWant) {
		t.Errorf("events %+v, want the limits", got)
	}
	select {
	case auth := <-seen:
		if auth != "Bearer t" {
			t.Errorf("upstream saw Authorization %q", auth)
		}
	default:
		t.Error("the call did not reach the upstream")
	}
}
