package server

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/svesh87/agent-tasks/internal/store"
)

// The web event stream: behind the auth proxy header, a "change" event after
// a write (here one made the way MCP makes it, straight through the store),
// keep-alive pings, and the subscription ends with the request.
func TestEventStream(t *testing.T) {
	const hdr = "Cf-Access-Authenticated-User-Email"
	srv, st := setup(t, Config{WebAuthHeader: hdr, EventPing: 100 * time.Millisecond})

	if res, err := http.Get(srv.URL + "/api/events"); err != nil {
		t.Fatal(err)
	} else if res.Body.Close(); res.StatusCode != http.StatusForbidden {
		t.Fatalf("without the proxy header: %d, want 403", res.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/events", nil)
	req.Header.Set(hdr, "owner@example.com")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); res.StatusCode != 200 || ct != "text/event-stream" {
		t.Fatalf("status %d, content type %q", res.StatusCode, ct)
	}
	lines := make(chan string, 64)
	go func() {
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	next := func(want string) {
		t.Helper()
		for {
			select {
			case l, ok := <-lines:
				if !ok {
					t.Fatalf("stream closed before %q", want)
				}
				if strings.HasPrefix(l, want) {
					return
				}
			case <-ctx.Done():
				t.Fatalf("no %q", want)
			}
		}
	}
	next(": connected")
	next(": ping")
	if _, err := st.CreateTask(context.Background(), store.NewTask{Title: "t"}); err != nil {
		t.Fatal(err)
	}
	next("event: change")
	next("data: {}")
}
