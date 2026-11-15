// Command agent-tasks is the task server for coding agents: MCP at /mcp, the
// web UI and its JSON API, a session hook endpoint, markdown export/import
// and a retention job, all over one SQLite file.
//
//	agent-tasks [serve] [flags]          run the server (default)
//	agent-tasks export -task T-12 -out DIR
//	agent-tasks import -task T-12 -file plan.md [-slug plan] [-kind plan] [-title ...] [-message ...]
//	agent-tasks version
//
// Every flag has an environment variable; secrets (tokens) are read only from
// the environment or a file, never from flags.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/svesh87/agent-tasks/internal/exportmd"
	"github.com/svesh87/agent-tasks/internal/server"
	"github.com/svesh87/agent-tasks/internal/store"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func env(name, def string) string {
	if v, ok := os.LookupEnv(name); ok {
		return v
	}
	return def
}

func envInt(name string, def int) int {
	if v, ok := os.LookupEnv(name); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(name string) bool {
	b, _ := strconv.ParseBool(os.Getenv(name))
	return b
}

func main() {
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve(args)
	case "export":
		err = export(args)
	case "import":
		err = importCmd(args)
	case "version":
		fmt.Println(version)
	default:
		err = fmt.Errorf("unknown command %q: use serve, export, import or version", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "agent-tasks:", err)
		os.Exit(1)
	}
}

func dbFlag(fs *flag.FlagSet) *string {
	return fs.String("db", env("AGENT_TASKS_DB", "agent-tasks.db"), "SQLite database file (env AGENT_TASKS_DB)")
}

// parseTokens reads "name=secret" or bare "secret" entries separated by
// commas or newlines; lines starting with # are ignored.
func parseTokens(s string) []server.Token {
	var out []server.Token
	for i, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' }) {
		part = strings.TrimSpace(part)
		if part == "" || strings.HasPrefix(part, "#") {
			continue
		}
		name, secret, ok := strings.Cut(part, "=")
		if !ok {
			name, secret = fmt.Sprintf("token%d", i+1), part
		}
		if secret = strings.TrimSpace(secret); secret != "" {
			out = append(out, server.Token{Name: strings.TrimSpace(name), Secret: secret})
		}
	}
	return out
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	listen := fs.String("listen", env("AGENT_TASKS_LISTEN", ":8080"), "listen address (env AGENT_TASKS_LISTEN)")
	db := dbFlag(fs)
	baseURL := fs.String("base-url", env("AGENT_TASKS_BASE_URL", "http://localhost:8080"), "public base URL of the web UI, used in links (env AGENT_TASKS_BASE_URL)")
	tokensFile := fs.String("tokens-file", env("AGENT_TASKS_TOKENS_FILE", ""), "file with MCP bearer tokens, one 'name=secret' per line (env AGENT_TASKS_TOKENS_FILE); tokens may also come from env AGENT_TASKS_TOKENS")
	retention := fs.Int("retention-days", envInt("AGENT_TASKS_RETENTION_DAYS", 90), "delete archived tasks older than this many days; 0 disables (env AGENT_TASKS_RETENTION_DAYS)")
	workspace := fs.String("workspace-root", env("AGENT_TASKS_WORKSPACE_ROOT", "~/agent-work"), "workspace root on agents' machines, returned as <root>/<task id> (env AGENT_TASKS_WORKSPACE_ROOT)")
	webHeader := fs.String("web-auth-header", env("AGENT_TASKS_WEB_AUTH_HEADER", ""), "if set, /api and the UI require this header from the auth proxy, e.g. Cf-Access-Authenticated-User-Email (env AGENT_TASKS_WEB_AUTH_HEADER)")
	webDir := fs.String("web-dir", env("AGENT_TASKS_WEB_DIR", ""), "serve the web UI from this directory instead of the embedded build (env AGENT_TASKS_WEB_DIR)")
	waitMax := fs.Int("review-wait-max", envInt("AGENT_TASKS_REVIEW_WAIT_MAX", 900), "longest review_wait in seconds (env AGENT_TASKS_REVIEW_WAIT_MAX)")
	noAuth := fs.Bool("mcp-no-auth", envBool("AGENT_TASKS_MCP_NO_AUTH"), "development only: accept /mcp and /hooks without a token (env AGENT_TASKS_MCP_NO_AUTH)")
	fs.Parse(args)

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	tokens := parseTokens(os.Getenv("AGENT_TASKS_TOKENS"))
	if *tokensFile != "" {
		b, err := os.ReadFile(*tokensFile)
		if err != nil {
			return fmt.Errorf("tokens file: %w", err)
		}
		tokens = append(tokens, parseTokens(string(b))...)
	}
	if len(tokens) == 0 && !*noAuth {
		log.Warn("no MCP tokens configured: /mcp and /hooks will answer 503 (set AGENT_TASKS_TOKENS or -tokens-file)")
	}
	if *noAuth {
		log.Warn("MCP authentication is disabled (-mcp-no-auth); do not expose this server")
	}

	st, err := store.Open(*db)
	if err != nil {
		return err
	}
	defer st.Close()

	srv := server.New(st, server.Config{
		BaseURL: *baseURL, WorkspaceRoot: *workspace, Tokens: tokens, MCPNoAuth: *noAuth,
		WebAuthHeader: *webHeader, WebDir: *webDir, RetentionDays: *retention, Version: version, Logger: log,
		ReviewWaitMax: time.Duration(*waitMax) * time.Second,
	})
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go srv.RunRetention(ctx)

	httpSrv := &http.Server{Addr: *listen, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- httpSrv.ListenAndServe() }()
	names := make([]string, len(tokens))
	for i, t := range tokens {
		names[i] = t.Name
	}
	log.Info("agent-tasks started", "version", version, "listen", *listen, "db", *db, "base_url", *baseURL,
		"tokens", names, "retention_days", *retention, "web_auth_header", *webHeader)
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpSrv.Shutdown(sctx)
	}
	return nil
}

func export(args []string) error {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	db := dbFlag(fs)
	task := fs.String("task", "", "task id, e.g. T-12 (required)")
	out := fs.String("out", ".", "directory to write <task id>/ into")
	fs.Parse(args)
	if *task == "" {
		return errors.New("export: -task is required")
	}
	st, err := store.Open(*db)
	if err != nil {
		return err
	}
	defer st.Close()
	dir, err := exportmd.Export(context.Background(), st, *task, *out)
	if err != nil {
		return err
	}
	fmt.Println(dir)
	return nil
}

func importCmd(args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	db := dbFlag(fs)
	task := fs.String("task", "", "task id, e.g. T-12 (required)")
	file := fs.String("file", "", "markdown file (required)")
	slug := fs.String("slug", "", "document slug; default from front matter or file name")
	kind := fs.String("kind", "", "document kind for a new document")
	title := fs.String("title", "", "document title")
	message := fs.String("message", "", "revision message")
	fs.Parse(args)
	if *task == "" || *file == "" {
		return errors.New("import: -task and -file are required")
	}
	st, err := store.Open(*db)
	if err != nil {
		return err
	}
	defer st.Close()
	res, err := exportmd.Import(context.Background(), st, exportmd.ImportParams{Task: *task, File: *file, Slug: *slug, Kind: *kind, Title: *title, Message: *message})
	if err != nil {
		return err
	}
	if res.Unchanged {
		fmt.Printf("%s/%s: unchanged, revision %d\n", res.Document.Task, res.Document.Slug, res.Revision)
	} else {
		fmt.Printf("%s/%s: revision %d (%d plan items, %d comments outdated)\n", res.Document.Task, res.Document.Slug, res.Revision, len(res.Items), len(res.Outdated))
	}
	return nil
}
