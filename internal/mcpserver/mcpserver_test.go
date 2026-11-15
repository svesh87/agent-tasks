package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/svesh87/agent-tasks/internal/anchor"
	"github.com/svesh87/agent-tasks/internal/links"
	"github.com/svesh87/agent-tasks/internal/store"
)

type env struct {
	t  *testing.T
	st *store.Store
	cs *mcp.ClientSession
}

func setup(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv := (&Server{Store: st, Links: links.New("https://tasks.example/"), WorkspaceRoot: "~/agent-work", Version: "test"}).New()
	ct, stt := mcp.NewInMemoryTransports()
	ctx := context.Background()
	ss, err := srv.Connect(ctx, stt, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return &env{t: t, st: st, cs: cs}
}

// call invokes a tool and decodes its structured result into a map.
func (e *env) call(name string, args map[string]any) map[string]any {
	e.t.Helper()
	res, err := e.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		e.t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		e.t.Fatalf("%s: tool error: %s", name, text(res))
	}
	var out map[string]any
	b, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(b, &out); err != nil {
		e.t.Fatalf("%s: %v", name, err)
	}
	return out
}

func (e *env) callErr(name string, args map[string]any) string {
	e.t.Helper()
	res, err := e.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		e.t.Fatalf("%s: %v", name, err)
	}
	if !res.IsError {
		e.t.Fatalf("%s: expected an error", name)
	}
	return text(res)
}

func text(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func TestToolList(t *testing.T) {
	e := setup(t)
	res, err := e.cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"task_create", "task_get", "task_update", "task_search", "doc_write", "doc_get", "doc_diff",
		"plan_item_set", "review_pending", "comment_reply", "round_addressed", "journal_append", "journal_get",
		"place_here", "place_get", "todo_add", "todo_list", "todo_update", "review_wait"}
	got := map[string]*mcp.Tool{}
	for _, tl := range res.Tools {
		got[tl.Name] = tl
	}
	for _, name := range want {
		tl := got[name]
		if tl == nil {
			t.Errorf("missing tool %s", name)
			continue
		}
		if tl.Description == "" {
			t.Errorf("%s has no description", name)
		}
	}
	if len(res.Tools) != len(want) {
		t.Errorf("got %d tools, want %d", len(res.Tools), len(want))
	}
	b, _ := json.Marshal(got["task_update"].InputSchema)
	if !strings.Contains(string(b), `"archived"`) || !strings.Contains(string(b), `"required":["id"]`) {
		t.Errorf("task_update schema: %s", b)
	}
}

func TestEndToEnd(t *testing.T) {
	e := setup(t)
	task := e.call("task_create", map[string]any{"title": "Сервер задач", "tags": []string{"repo:github.com/acme/tools"}})
	if task["id"] != "T-1" || task["url"] != "https://tasks.example/t/T-1" || task["workspace"] != "~/agent-work/T-1" {
		t.Fatalf("task_create %v", task)
	}
	w := e.call("doc_write", map[string]any{"task": "T-1", "slug": "plan", "body": "# План\n\n- [ ] Написать роль\n- [ ] Настроить DNS\n\nИспользуем Traefik для маршрутизации.\n"})
	if w["url"] != "https://tasks.example/t/T-1/d/plan" || w["revision"].(float64) != 1 {
		t.Fatalf("doc_write %v", w)
	}
	items := w["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["id"] != "i1" {
		t.Fatalf("items %v", items)
	}
	it := e.call("plan_item_set", map[string]any{"task": "T-1", "slug": "plan", "item": "i1", "done": true, "note": "готово"})
	if item := it["item"].(map[string]any); item["done"] != true || item["done_by"] != "agent" || item["notes"].([]any)[0].(map[string]any)["by"] != "agent" {
		t.Fatalf("plan_item_set %v", it)
	}
	it = e.call("plan_item_set", map[string]any{"task": "T-1", "slug": "plan", "item": "i2", "done": true, "by": "owner"})
	if it["item"].(map[string]any)["done_by"] != "owner" {
		t.Fatalf("plan_item_set by owner %v", it)
	}
	e.call("plan_item_set", map[string]any{"task": "T-1", "slug": "plan", "item": "i2", "done": false})

	// The owner reviews through the store (REST in production).
	ctx := context.Background()
	r, err := e.st.DraftRound(ctx, "T-1", "plan", 0)
	if err != nil {
		t.Fatal(err)
	}
	c, err := e.st.AddComment(ctx, r.ID, store.NewComment{Anchor: &anchor.Anchor{Quote: "Traefik"}, Body: "почему не Caddy?"})
	if err != nil {
		t.Fatal(err)
	}
	if p := e.call("review_pending", nil); len(p["rounds"].([]any)) != 0 {
		t.Fatalf("draft leaked: %v", p)
	}
	if _, err := e.st.SubmitRound(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	p := e.call("review_pending", map[string]any{"task": "T-1"})
	rounds := p["rounds"].([]any)
	if len(rounds) != 1 {
		t.Fatalf("review_pending %v", p)
	}
	rc := rounds[0].(map[string]any)["comments"].([]any)[0].(map[string]any)
	if rc["anchor"].(map[string]any)["quote"] != "Traefik" || rc["line"].(float64) != 6 || rc["url"] != fmt.Sprintf("https://tasks.example/t/T-1/d/plan/r/1?c=%d", c.ID) {
		t.Fatalf("comment %v", rc)
	}
	g := e.call("task_get", map[string]any{"id": "T-1"})
	if len(g["pending_rounds"].([]any)) != 1 || g["plan_progress"].(map[string]any)["done"].(float64) != 1 {
		t.Fatalf("task_get %v", g)
	}

	w2 := e.call("doc_write", map[string]any{"task": "T-1", "slug": "plan", "message": "addresses round 1",
		"body": "# План\n\n- [ ] Написать роль\n- [ ] Настроить DNS\n- [ ] Бэкапы\n\nИспользуем Caddy.\n"})
	out := w2["outdated"].([]any)
	if len(out) != 1 || out[0].(map[string]any)["id"].(float64) != float64(c.ID) || w2["diff_url"] != "https://tasks.example/t/T-1/d/plan/diff/1..2" {
		t.Fatalf("doc_write 2 %v", w2)
	}
	items = w2["items"].([]any)
	if items[0].(map[string]any)["done"] != true || items[2].(map[string]any)["id"] != "i3" {
		t.Fatalf("items 2 %v", items)
	}
	rep := e.call("comment_reply", map[string]any{"comment": c.ID, "body": "Перешёл на Caddy", "resolve": true})
	if rep["state"] != "resolved" {
		t.Fatalf("comment_reply %v", rep)
	}
	ad := e.call("round_addressed", map[string]any{"round": r.ID, "message": "ревизия 2"})
	if ad["state"] != "addressed" {
		t.Fatalf("round_addressed %v", ad)
	}

	d := e.call("doc_diff", map[string]any{"task": "T-1", "slug": "plan", "from": 1})
	if !strings.Contains(d["diff"].(string), "+Используем Caddy.") {
		t.Fatalf("doc_diff %v", d)
	}
	dg := e.call("doc_get", map[string]any{"task": "T-1", "slug": "plan", "revision": 1})
	if !strings.Contains(dg["body"].(string), "Traefik") || dg["latest_revision"].(float64) != 2 || len(dg["revisions"].([]any)) != 2 {
		t.Fatalf("doc_get %v", dg)
	}

	e.call("journal_append", map[string]any{"task": "T-1", "kind": "context", "body": "Остановился на DNS", "source": "laptop"})
	j := e.call("journal_get", map[string]any{"task": "T-1", "kind": "context"})
	if len(j["entries"].([]any)) != 1 {
		t.Fatalf("journal_get %v", j)
	}
	s := e.call("task_search", map[string]any{"query": "задачи"})
	tasks := s["tasks"].([]any)
	if s["total"].(float64) != 1 || len(tasks) != 1 {
		t.Fatalf("task_search %v", s)
	}
	hit := tasks[0].(map[string]any)["hits"].([]any)[0].(map[string]any)
	if !strings.Contains(hit["snippet"].(string), "**задач") || hit["kind"] != "task" || hit["url"] != "https://tasks.example/t/T-1" {
		t.Fatalf("snippet %v", hit)
	}
	s = e.call("task_search", map[string]any{"query": "Caddy"})
	found := map[string]string{}
	for _, h := range s["tasks"].([]any)[0].(map[string]any)["hits"].([]any) {
		hm := h.(map[string]any)
		found[hm["kind"].(string)] = hm["url"].(string)
	}
	// "Используем Caddy." — Caddy starts at code point 73 of revision 2
	if found["document"] != "https://tasks.example/t/T-1/d/plan/r/2?at=73-78" {
		t.Errorf("document hit url %v", found)
	}
	if found["comment"] != fmt.Sprintf("https://tasks.example/t/T-1/d/plan/r/1?c=%d", c.ID) {
		t.Errorf("comment hit url %v", found)
	}
	up := e.call("task_update", map[string]any{"id": "T-1", "status": "archived", "closed_reason": "done"})
	if up["status"] != "archived" {
		t.Fatalf("task_update %v", up)
	}
}

func TestToolErrors(t *testing.T) {
	e := setup(t)
	if msg := e.callErr("task_get", map[string]any{"id": "T-5"}); !strings.Contains(msg, "task T-5 not found") {
		t.Errorf("task_get error: %s", msg)
	}
	e.call("task_create", map[string]any{"title": "x"})
	if msg := e.callErr("task_update", map[string]any{"id": "T-1", "status": "archived"}); !strings.Contains(msg, "closed_reason") {
		t.Errorf("archive error: %s", msg)
	}
	if msg := e.callErr("doc_write", map[string]any{"task": "T-1", "slug": "Bad Slug", "body": "x"}); !strings.Contains(msg, "bad slug") {
		t.Errorf("slug error: %s", msg)
	}
	if msg := e.callErr("task_update", map[string]any{"id": "T-1", "status": "bogus"}); msg == "" {
		t.Error("enum not enforced")
	}
}
