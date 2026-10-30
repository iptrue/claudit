package claude

import (
	"cmp"
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/pixelbender/claudit/internal/protocol"
)

// claude -p reads the rate-limit headers of the API only for a claude.ai login: with
// ANTHROPIC_AUTH_TOKEN it drops them, and no rate_limit_event comes. claudit then stands between
// claude and the API and reads them itself.

// proxyEnv starts the limits proxy when claude would drop the rate-limit headers, and returns the
// environment that sends claude through it; nil, claudit's own, otherwise.
func proxyEnv(ctx context.Context, limits chan<- protocol.RateLimits) ([]string, error) {
	if os.Getenv("ANTHROPIC_AUTH_TOKEN") == "" {
		return nil, nil
	}
	upstream, err := url.Parse(cmp.Or(os.Getenv("ANTHROPIC_BASE_URL"), "https://api.anthropic.com"))
	if err != nil {
		return nil, err
	}
	base, err := limitsProxy(ctx, upstream, func(l protocol.RateLimits) {
		select {
		case limits <- l:
		default: // one waits to go out this moment; the next response brings newer ones
		}
	})
	if err != nil {
		return nil, err
	}
	return append(os.Environ(), "ANTHROPIC_BASE_URL="+base), nil
}

// limitsProxy forwards claude's API calls to upstream as they are, streaming without delay, and
// hands report the rate limits of each response. It returns its base URL and runs until ctx is done.
func limitsProxy(ctx context.Context, upstream *url.URL, report func(protocol.RateLimits)) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	quiet := log.New(io.Discard, "", 0) // a failed call is claude's to report, not a line over the UI
	srv := &http.Server{ErrorLog: quiet, Handler: &httputil.ReverseProxy{
		Rewrite:       func(r *httputil.ProxyRequest) { r.SetURL(upstream) },
		FlushInterval: -1,
		ErrorLog:      quiet,
		ModifyResponse: func(resp *http.Response) error {
			if l, ok := headerLimits(resp.Header); ok {
				report(l)
			}
			return nil
		},
	}}
	go srv.Serve(ln)
	go func() {
		<-ctx.Done()
		srv.Close()
	}()
	return "http://" + ln.Addr().String(), nil
}

// headerLimits reads the subscription's rate-limit windows from the headers of an API response as
// claude does: anthropic-ratelimit-unified-<window>-utilization and -reset, in unix seconds.
func headerLimits(h http.Header) (protocol.RateLimits, bool) {
	window := func(name string) *protocol.Window {
		used, err := strconv.ParseFloat(h.Get("anthropic-ratelimit-unified-"+name+"-utilization"), 64)
		if err != nil {
			return nil
		}
		reset, err := strconv.ParseFloat(h.Get("anthropic-ratelimit-unified-"+name+"-reset"), 64)
		if err != nil {
			return nil
		}
		return &protocol.Window{Used: used, ResetsAt: time.Unix(int64(reset), 0)}
	}
	l := protocol.RateLimits{Session: window("5h"), Week: window("7d"), Fable: window("7d_oi")}
	return l, l != (protocol.RateLimits{})
}
