package api

import (
	"fmt"
	"net/http"
	"time"
)

// defaultPing keeps the stream alive through proxies that close idle
// connections (Cloudflare drops a response with no bytes for ~100 s).
const defaultPing = 25 * time.Second

// events streams Server-Sent Events: "change" after every committed change
// of the data, whoever made it (the owner here, an agent through MCP). The
// event carries no data; the page re-reads what it shows. A comment line
// every PingEvery keeps the connection open.
func (a *API) events(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no") // nginx-style proxies: do not buffer the stream
	w.WriteHeader(http.StatusOK)
	// retry: how long the browser waits before reconnecting after a drop.
	if _, err := fmt.Fprint(w, "retry: 3000\n: connected\n\n"); err != nil {
		return
	}
	if err := rc.Flush(); err != nil {
		return
	}
	every := a.PingEvery
	if every <= 0 {
		every = defaultPing
	}
	ping := time.NewTicker(every)
	defer ping.Stop()
	for {
		changed := a.Store.Changes()
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
		case <-changed:
			if _, err := fmt.Fprint(w, "event: change\ndata: {}\n\n"); err != nil {
				return
			}
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}
