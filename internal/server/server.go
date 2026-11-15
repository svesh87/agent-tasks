// Package server wires the HTTP endpoints: /mcp (MCP, bearer tokens),
// /hooks/session-context (bearer tokens), /api (web JSON API) and the web UI,
// plus the daily retention job.
package server

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/svesh87/agent-tasks/internal/api"
	"github.com/svesh87/agent-tasks/internal/links"
	"github.com/svesh87/agent-tasks/internal/mcpserver"
	"github.com/svesh87/agent-tasks/internal/store"
	"github.com/svesh87/agent-tasks/internal/webui"
)

// Token is a bearer token with a name used in logs (the secret is never logged).
type Token struct {
	Name   string
	Secret string
}

// Config configures the HTTP server.
type Config struct {
	BaseURL       string
	WorkspaceRoot string
	Tokens        []Token
	MCPNoAuth     bool          // development only: accept MCP and hook calls without a token
	WebAuthHeader string        // if set, /api and the UI require this header (set by the auth proxy)
	WebDir        string        // serve the UI from this directory instead of the embedded files
	RetentionDays int           // 0 disables the retention job
	ReviewWaitMax time.Duration // cap of review_wait's timeout_s; 0 = 900 s
	EventPing     time.Duration // keep-alive on the web event stream; 0 = 25 s
	Version       string
	Logger        *slog.Logger
}

// Server is the HTTP application.
type Server struct {
	cfg   Config
	store *store.Store
	links links.Links
	log   *slog.Logger
}

// New creates the server.
func New(st *store.Store, cfg Config) *Server {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.WorkspaceRoot == "" {
		cfg.WorkspaceRoot = "~/agent-work"
	}
	return &Server{cfg: cfg, store: st, links: links.New(cfg.BaseURL), log: cfg.Logger}
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	mcpSrv := (&mcpserver.Server{Store: s.store, Links: s.links, WorkspaceRoot: s.cfg.WorkspaceRoot, Version: s.cfg.Version, MaxWait: s.cfg.ReviewWaitMax}).New()
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return mcpSrv }, &mcp.StreamableHTTPOptions{
		Stateless:                  true,
		DisableLocalhostProtection: true, // the server runs behind a reverse proxy; tokens protect /mcp
		// a client that drops the HTTP request (e.g. review_wait cancelled)
		// stops the tool handler
		PropagateRequestCancellation: true,
	})
	apiHandler := (&api.API{Store: s.store, Links: s.links, WorkspaceRoot: s.cfg.WorkspaceRoot, Version: s.cfg.Version, UserHeader: s.cfg.WebAuthHeader, PingEvery: s.cfg.EventPing}).Handler()

	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ok") })
	m.Handle("/mcp", s.bearer(mcpHandler))
	m.Handle("/mcp/", s.bearer(mcpHandler))
	m.Handle("GET /hooks/session-context", s.bearer(http.HandlerFunc(s.sessionContext)))
	m.Handle("/api/", s.webAuth(apiHandler))
	m.Handle("/", s.webAuth(webui.Handler(s.cfg.WebDir)))
	return s.logRequests(m)
}

// bearer checks Authorization: Bearer <token> against the configured tokens.
func (s *Server) bearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.MCPNoAuth {
			next.ServeHTTP(w, r)
			return
		}
		if len(s.cfg.Tokens) == 0 {
			http.Error(w, "no API tokens configured on the server", http.StatusServiceUnavailable)
			return
		}
		h := r.Header.Get("Authorization")
		const prefix = "bearer "
		if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="agent-tasks"`)
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		got := []byte(strings.TrimSpace(h[len(prefix):]))
		ok := false
		for _, t := range s.cfg.Tokens {
			if subtle.ConstantTimeCompare(got, []byte(t.Secret)) == 1 {
				ok = true
			}
		}
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="agent-tasks", error="invalid_token"`)
			http.Error(w, "invalid bearer token", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// webAuth requires the auth proxy's header when configured.
func (s *Server) webAuth(next http.Handler) http.Handler {
	if s.cfg.WebAuthHeader == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.Header.Get(s.cfg.WebAuthHeader)) == "" {
			http.Error(w, "forbidden: request did not come through the auth proxy", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(sw, r)
		if r.URL.Path == "/healthz" {
			return
		}
		s.log.Info("http", "method", r.Method, "path", r.URL.Path, "status", sw.code, "dur", time.Since(start).Round(time.Millisecond))
	})
}

// sessionContext answers GET /hooks/session-context?repo=&cwd=&machine= with
// a few plain-text lines for an agent's context, or an empty body. It also
// registers where the place lives on the machine.
func (s *Server) sessionContext(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 250*time.Millisecond)
	defer cancel()
	q := r.URL.Query()
	info, err := s.store.SessionContext(ctx, q.Get("repo"), q.Get("cwd"), q.Get("machine"))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err != nil {
		s.log.Warn("session-context", "err", err)
		return // empty body: the hook prints nothing
	}
	if len(info.Tasks) == 0 && len(info.Todos) == 0 {
		return
	}
	var b strings.Builder
	if info.Place != nil {
		fmt.Fprintf(&b, "agent-tasks: place %s (MCP server agent-tasks; task_get <id> for details)\n", info.Place.Key)
	} else {
		b.WriteString("agent-tasks: task of this workspace folder (MCP server agent-tasks; task_get <id> for details)\n")
	}
	if len(info.Tasks) > 0 {
		b.WriteString("Tasks:\n")
	}
	for _, t := range info.Tasks {
		status := t.Status
		if t.Status == "waiting" && t.WaitingOn != "" {
			status += ": " + t.WaitingOn
		}
		if t.Status == "archived" {
			status += ", " + t.ClosedReason
		}
		for _, a := range t.Approvals {
			status += fmt.Sprintf(", %s r%d approved", a.Slug, a.Revision)
		}
		fmt.Fprintf(&b, "- %s [%s] %s — %s\n", t.ID, status, t.Title, s.links.Task(t.ID))
		fmt.Fprintf(&b, "  workspace: %s/%s\n", strings.TrimRight(s.cfg.WorkspaceRoot, "/"), t.ID)
		for _, rb := range t.PendingRounds {
			fmt.Fprintf(&b, "  review waiting: round %d on %s r%d, %d comments (round id %d) — %s\n",
				rb.N, rb.Slug, rb.Revision, rb.Comments, rb.ID, s.links.Doc(t.ID, rb.Slug))
		}
		for _, op := range t.OtherPlaces {
			where := "not on this machine"
			if op.Path != "" {
				where = op.Path
			}
			fmt.Fprintf(&b, "  also %s (%s): %s\n", op.Key, op.Role, where)
		}
	}
	if len(info.Todos) > 0 {
		fmt.Fprintf(&b, "Open todos here (todo_list for more; %s):\n", s.links.Todos(info.Place.Key))
		for _, d := range info.Todos {
			fmt.Fprintf(&b, "- %s %s\n", d.ID, d.Text)
		}
	}
	w.Write([]byte(b.String()))
}

// RunRetention deletes expired archived tasks now and then every 24 hours
// until ctx is done.
func (s *Server) RunRetention(ctx context.Context) {
	if s.cfg.RetentionDays <= 0 {
		s.log.Info("retention disabled")
		return
	}
	run := func() {
		ids, err := s.store.PurgeArchived(ctx, s.cfg.RetentionDays)
		if err != nil {
			s.log.Error("retention", "err", err)
			return
		}
		if len(ids) > 0 {
			s.log.Info("retention: deleted archived tasks", "days", s.cfg.RetentionDays, "tasks", ids)
		}
	}
	run()
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}
