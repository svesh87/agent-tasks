package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/svesh87/agent-tasks/internal/store"
)

const token = "test-token-123"

func setup(t *testing.T, cfg Config) (*httptest.Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://tasks.example"
	}
	cfg.Tokens = []Token{{Name: "test", Secret: token}}
	cfg.Logger = slog.New(slog.DiscardHandler)
	srv := httptest.NewServer(New(st, cfg).Handler())
	t.Cleanup(srv.Close)
	return srv, st
}

type authTransport struct{ token string }

func (a authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+a.token)
	return http.DefaultTransport.RoundTrip(r)
}

func TestReviewWaitOverHTTP(t *testing.T) {
	srv, st := setup(t, Config{})
	ctx := context.Background()
	connect := func() *mcp.ClientSession {
		tr := &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: authTransport{token}}}
		cs, err := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "1"}, nil).Connect(ctx, tr, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cs.Close() })
		return cs
	}
	task, _ := st.CreateTask(ctx, store.NewTask{Title: "t"})
	st.WriteDoc(ctx, store.WriteDocParams{Task: task.ID, Slug: "plan", Body: "x\n", Author: store.AuthorAgent})

	// a client that gives up returns promptly and the server keeps serving
	cs := connect()
	c, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	_, err := cs.CallTool(c, &mcp.CallToolParams{Name: "review_wait", Arguments: map[string]any{"timeout_s": 60}})
	cancel()
	if err == nil {
		t.Fatal("expected a cancellation error")
	}

	cs = connect()
	done := make(chan *mcp.CallToolResult, 1)
	go func() {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "review_wait", Arguments: map[string]any{"task": task.ID, "timeout_s": 10}})
		if err != nil {
			t.Error(err)
		}
		done <- res
	}()
	time.Sleep(200 * time.Millisecond)
	r, _ := st.DraftRound(ctx, task.ID, "plan", 0)
	st.AddComment(ctx, r.ID, store.NewComment{Body: "fix"})
	st.SubmitRound(ctx, r.ID)
	select {
	case res := <-done:
		b, _ := json.Marshal(res.StructuredContent)
		if !strings.Contains(string(b), `"rounds":[{`) {
			t.Fatalf("result %s", b)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("review_wait over HTTP not woken")
	}
}

func TestMCPOverHTTP(t *testing.T) {
	srv, _ := setup(t, Config{})
	ctx := context.Background()

	// wrong token is rejected
	bad := &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: authTransport{"nope"}}}
	if _, err := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "1"}, nil).Connect(ctx, bad, nil); err == nil {
		t.Fatal("connected with a wrong token")
	}

	tr := &mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp", HTTPClient: &http.Client{Transport: authTransport{token}}}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "1"}, nil).Connect(ctx, tr, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "task_create", Arguments: map[string]any{"title": "over http"}})
	if err != nil || res.IsError {
		t.Fatalf("task_create: %v %+v", err, res)
	}
	b, _ := json.Marshal(res.StructuredContent)
	if !strings.Contains(string(b), `"url":"https://tasks.example/t/T-1"`) {
		t.Fatalf("result %s", b)
	}
}

func get(t *testing.T, url string, hdr map[string]string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestSessionContextHook(t *testing.T) {
	srv, st := setup(t, Config{})
	ctx := context.Background()
	auth := map[string]string{"Authorization": "Bearer " + token}
	url := srv.URL + "/hooks/session-context?repo=github.com/acme/infra&cwd=/home/u/src/infra&machine=laptop"

	if code, _ := get(t, url, nil); code != http.StatusUnauthorized {
		t.Fatalf("no token: %d", code)
	}
	code, body := get(t, url, auth)
	if code != http.StatusOK || body != "" {
		t.Fatalf("empty case: %d %q", code, body)
	}
	// the call above registered the location of infra on laptop
	if p, err := st.GetPlace(ctx, "github.com/acme/infra"); err != nil || len(p.Locations) != 1 || p.Locations[0].Path != "/home/u/src/infra" {
		t.Fatalf("location not learned: %+v %v", p, err)
	}

	task, _ := st.CreateTask(ctx, store.NewTask{Title: "Сервер задач", Status: "waiting", WaitingOn: "owner review",
		Places: []store.PlaceRef{{Place: "github.com/acme/infra", Role: "primary"}, {Place: "github.com/acme/tools"}, {Place: "github.com/acme/docs"}}})
	if _, err := st.PlaceHere(ctx, "/home/u/src/tools", "git@github.com:acme/tools.git", "laptop"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddTodo(ctx, store.NewTodo{Text: "Почистить старые бэкапы", Places: []string{"github.com/acme/infra"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteDoc(ctx, store.WriteDocParams{Task: task.ID, Slug: "plan", Body: "# Plan\n\ntext\n", Author: store.AuthorAgent}); err != nil {
		t.Fatal(err)
	}
	r, _ := st.DraftRound(ctx, task.ID, "plan", 0)
	st.AddComment(ctx, r.ID, store.NewComment{Body: "general"})
	st.SubmitRound(ctx, r.ID)

	start := time.Now()
	code, body = get(t, srv.URL+"/hooks/session-context?repo="+"git@github.com:acme/infra.git&cwd=/home/u/src/infra&machine=laptop", auth)
	if el := time.Since(start); el > 300*time.Millisecond {
		t.Errorf("slow: %v", el)
	}
	for _, want := range []string{
		"place github.com/acme/infra",
		"T-1 [waiting: owner review] Сервер задач — https://tasks.example/t/T-1",
		"workspace: ~/agent-work/T-1",
		"review waiting: round 1 on plan r1, 1 comments",
		"also github.com/acme/tools (touched): /home/u/src/tools",
		"also github.com/acme/docs (touched): not on this machine",
		"- D-1 Почистить старые бэкапы",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	if code != http.StatusOK {
		t.Fatalf("code %d", code)
	}

	// The owner's approval shows on the task's line.
	if _, err := st.Approve(ctx, task.ID, "plan", 1, false); err != nil {
		t.Fatal(err)
	}
	_, body = get(t, url, auth)
	if !strings.Contains(body, "T-1 [waiting: owner review, plan r1 approved] Сервер задач") {
		t.Errorf("approval missing in:\n%s", body)
	}
}

func TestWebAuthHeaderAndAPI(t *testing.T) {
	srv, _ := setup(t, Config{WebAuthHeader: "Cf-Access-Authenticated-User-Email"})
	if code, _ := get(t, srv.URL+"/api/tasks", nil); code != http.StatusForbidden {
		t.Fatalf("without header: %d", code)
	}
	hdr := map[string]string{"Cf-Access-Authenticated-User-Email": "owner@example.com"}
	code, body := get(t, srv.URL+"/api/config", hdr)
	if code != 200 || !strings.Contains(body, `"user":"owner@example.com"`) {
		t.Fatalf("config %d %s", code, body)
	}
	// SPA fallback serves index.html for client routes
	code, body = get(t, srv.URL+"/t/T-1/d/plan", hdr)
	if code != 200 || !strings.Contains(body, "<html") {
		t.Fatalf("spa %d %s", code, body)
	}
	if code, _ := get(t, srv.URL+"/healthz", nil); code != 200 {
		t.Fatalf("healthz %d", code)
	}
}

// do sends a JSON request to the API and decodes the response.
func do(t *testing.T, method, url string, in any, wantCode int) map[string]any {
	t.Helper()
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, url, body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantCode {
		t.Fatalf("%s %s: %d %s", method, url, resp.StatusCode, raw)
	}
	out := map[string]any{}
	if len(raw) > 0 {
		json.Unmarshal(raw, &out)
	}
	return out
}

func TestRESTReviewFlow(t *testing.T) {
	srv, st := setup(t, Config{})
	api := srv.URL + "/api"
	task := do(t, "POST", api+"/tasks", map[string]any{"title": "Задача про DNS", "tags": []string{"topic:dns"}}, 201)
	id := task["id"].(string)
	ctx := context.Background()
	if _, err := st.WriteDoc(ctx, store.WriteDocParams{Task: id, Slug: "plan", Author: store.AuthorAgent,
		Body: "# Plan\n\n- [ ] Create the record\n\nUse a CNAME to the tunnel.\n"}); err != nil {
		t.Fatal(err)
	}

	list := do(t, "GET", api+"/tasks?q=задачи&tag=topic:dns", nil, 200)
	if list["total"].(float64) != 1 {
		t.Fatalf("search %v", list)
	}
	ov := do(t, "GET", api+"/tasks/"+id, nil, 200)
	if len(ov["documents"].([]any)) != 1 {
		t.Fatalf("overview %v", ov)
	}
	doc := do(t, "GET", api+"/tasks/"+id+"/docs/plan", nil, 200)
	if doc["revision"].(map[string]any)["n"].(float64) != 1 || len(doc["items"].([]any)) != 1 {
		t.Fatalf("doc %v", doc)
	}
	item := do(t, "POST", api+"/tasks/"+id+"/docs/plan/items/i1", map[string]any{"done": true, "note": "done by hand"}, 200)
	if item["done_by"] != "owner" || item["notes"].([]any)[0].(map[string]any)["by"] != "owner" {
		t.Fatalf("owner tick %v", item)
	}

	round := do(t, "POST", api+"/tasks/"+id+"/docs/plan/rounds/draft", map[string]any{}, 200)
	rid := int64(round["round"].(float64))
	rurl := api + "/rounds/" + itoa(rid)
	c := do(t, "POST", rurl+"/comments", map[string]any{"body": "why CNAME?", "anchor": map[string]any{"quote": "CNAME", "start": 0}}, 201)
	cid := itoa(int64(c["id"].(float64)))
	if p := c["anchor"].(map[string]any)["prefix"].(string); len([]rune(p)) != 32 || !strings.HasSuffix(p, "record\n\nUse a ") {
		t.Fatalf("anchor %v", c["anchor"])
	}
	do(t, "PATCH", api+"/comments/"+cid, map[string]any{"body": "why a CNAME?"}, 200)
	do(t, "POST", rurl+"/comments", map[string]any{"body": "x", "anchor": map[string]any{"quote": "absent"}}, 400)
	do(t, "PATCH", rurl, map[string]any{"summary": "mostly fine"}, 200)
	sub := do(t, "POST", rurl+"/submit", nil, 200)
	if sub["state"] != "submitted" {
		t.Fatalf("submit %v", sub)
	}
	do(t, "PATCH", api+"/comments/"+cid, map[string]any{"body": "edit after submit"}, 409)
	pend := do(t, "GET", api+"/rounds/pending?task="+id, nil, 200)
	if len(pend["rounds"].([]any)) != 1 {
		t.Fatalf("pending %v", pend)
	}
	rep := do(t, "POST", api+"/comments/"+cid+"/replies", map[string]any{"body": "never mind", "resolve": true}, 201)
	if rep["state"] != "resolved" || len(rep["replies"].([]any)) != 1 {
		t.Fatalf("reply %v", rep)
	}
	do(t, "PATCH", api+"/comments/"+cid, map[string]any{"state": "open"}, 200)

	do(t, "PUT", api+"/tasks/"+id+"/docs/plan", map[string]any{"body": "# Plan\n\n- [ ] Create the record\n\nUse an A record.\n", "message": "owner edit"}, 200)
	d := do(t, "GET", api+"/tasks/"+id+"/docs/plan/diff?from=1&to=2", nil, 200)
	if !strings.Contains(d["unified"].(string), "+Use an A record.") {
		t.Fatalf("diff %v", d)
	}
	revs := do(t, "GET", api+"/tasks/"+id+"/docs/plan/revisions", nil, 200)
	if r := revs["revisions"].([]any); len(r) != 2 || r[1].(map[string]any)["author"] != "owner" {
		t.Fatalf("revisions %v", revs)
	}
	do(t, "POST", api+"/tasks/"+id+"/journal", map[string]any{"kind": "decision", "body": "A record, not CNAME"}, 201)
	j := do(t, "GET", api+"/tasks/"+id+"/journal", nil, 200)
	if len(j["entries"].([]any)) != 1 {
		t.Fatalf("journal %v", j)
	}
	up := do(t, "PATCH", api+"/tasks/"+id, map[string]any{"status": "archived", "closed_reason": "done"}, 200)
	if up["status"] != "archived" {
		t.Fatalf("archive %v", up)
	}
	do(t, "GET", api+"/tasks/T-99", nil, 404)
	do(t, "PATCH", api+"/tasks/"+id, map[string]any{"bogus": 1}, 400)
	tags := do(t, "GET", api+"/tags", nil, 200)
	if len(tags["tags"].([]any)) != 1 {
		t.Fatalf("tags %v", tags)
	}
}

func TestRESTPlacesAndTodos(t *testing.T) {
	srv, st := setup(t, Config{})
	api := srv.URL + "/api"
	ctx := context.Background()
	st.PlaceHere(ctx, "/src/infra", "git@github.com:acme/infra.git", "laptop")

	d1 := do(t, "POST", api+"/todos", map[string]any{"text": "first", "places": []string{"github.com/acme/infra"}}, 201)
	do(t, "POST", api+"/todos", map[string]any{"text": "second"}, 201)
	do(t, "POST", api+"/todos", map[string]any{"text": "bad\ntext"}, 400)
	if d1["id"] != "D-1" || d1["url"] != "https://tasks.example/todos/D-1" {
		t.Fatalf("todo %v", d1)
	}
	l := do(t, "GET", api+"/todos?place=none", nil, 200)
	if l["total"].(float64) != 1 {
		t.Fatalf("none %v", l)
	}
	l = do(t, "GET", api+"/todos?place=github.com/acme/infra&q=first", nil, 200)
	if l["total"].(float64) != 1 {
		t.Fatalf("place %v", l)
	}
	// "create task from these"
	task := do(t, "POST", api+"/tasks", map[string]any{"title": "from todos", "from_todos": []string{"D-1", "D-2"},
		"places": []map[string]any{{"place": "github.com/acme/infra", "role": "primary"}}}, 201)
	id := task["id"].(string)
	if p := task["places"].([]any); len(p) != 1 || p[0].(map[string]any)["role"] != "primary" {
		t.Fatalf("places %v", task["places"])
	}
	if d := do(t, "GET", api+"/todos/D-2", nil, 200); d["state"] != "in_task" || d["task"] != id {
		t.Fatalf("in_task %v", d)
	}
	ov := do(t, "GET", api+"/tasks/"+id+"?machine=laptop", nil, 200)
	pl := ov["places"].([]any)[0].(map[string]any)
	if len(ov["todos"].([]any)) != 2 || pl["locations"].([]any)[0].(map[string]any)["path"] != "/src/infra" {
		t.Fatalf("overview %v", ov)
	}
	do(t, "PATCH", api+"/todos/D-2", map[string]any{"state": "open"}, 200)
	do(t, "PATCH", api+"/todos/D-2", map[string]any{"state": "dropped"}, 200)

	other := do(t, "POST", api+"/tasks", map[string]any{"title": "other", "tags": []string{"x"}}, 201)
	// merging is gone
	do(t, "PATCH", api+"/tasks/"+other["id"].(string), map[string]any{"merged_into": id}, 400)
	do(t, "POST", api+"/tasks", map[string]any{"title": "y", "from_tasks": []map[string]any{{"task": id, "note": "n"}}}, 400)
	do(t, "PATCH", api+"/tasks/"+other["id"].(string), map[string]any{"status": "archived", "closed_reason": "merged"}, 400)
	m := do(t, "PATCH", api+"/tasks/"+other["id"].(string), map[string]any{"places_add": []map[string]any{{"place": "laptop:/tmp/x"}}}, 200)
	if len(m["places"].([]any)) != 1 {
		t.Fatalf("places_add %v", m)
	}
	s := do(t, "GET", api+"/tasks?place=github.com/acme/infra", nil, 200)
	if s["total"].(float64) != 1 {
		t.Fatalf("search place %v", s)
	}
	// list rows and facets
	s = do(t, "GET", api+"/tasks", nil, 200)
	sc := s["status_counts"].(map[string]any)
	if s["total"].(float64) != 2 || sc["active"].(float64) != 2 || sc["archived"].(float64) != 0 || len(s["tag_counts"].([]any)) != 1 {
		t.Fatalf("facets %v", s)
	}
	row := s["tasks"].([]any)[0].(map[string]any)
	if _, ok := row["doc_count"]; !ok {
		t.Fatalf("row stats missing %v", row)
	}
	ps := do(t, "GET", api+"/places", nil, 200)
	if len(ps["places"].([]any)) != 2 {
		t.Fatalf("places %v", ps)
	}
	ps = do(t, "GET", api+"/places?q=infr", nil, 200)
	if len(ps["places"].([]any)) != 1 {
		t.Fatalf("places q %v", ps)
	}
	tl := do(t, "GET", api+"/todos?state=open,dropped,in_task", nil, 200)
	stc := tl["state_counts"].(map[string]any)
	if stc["in_task"].(float64) != 1 || stc["dropped"].(float64) != 1 || tl["total"].(float64) != 2 {
		t.Fatalf("todo facets %v", tl)
	}
	do(t, "PATCH", api+"/todos/D-1", map[string]any{"text": "first, edited", "note": "n", "tags_add": []string{"t"}, "places_add": []string{"laptop:/tmp/x"}}, 200)
	cfg := do(t, "GET", api+"/config", nil, 200)
	if ms := cfg["machines"].([]any); len(ms) != 1 || ms[0] != "laptop" {
		t.Fatalf("config machines %v", cfg)
	}
	p := do(t, "GET", api+"/place?key=git@github.com:acme/infra.git", nil, 200)
	if p["key"] != "github.com/acme/infra" {
		t.Fatalf("place %v", p)
	}
	do(t, "GET", api+"/place?key=github.com/no/such", nil, 404)
}

func TestRESTv06(t *testing.T) {
	srv, st := setup(t, Config{})
	api := srv.URL + "/api"
	ctx := context.Background()
	a := do(t, "POST", api+"/tasks", map[string]any{"title": "Кошки"}, 201)["id"].(string)
	b := do(t, "POST", api+"/tasks", map[string]any{"title": "Собаки"}, 201)["id"].(string)
	st.PlaceHere(ctx, "/src/x", "github.com/o/x", "laptop")
	st.PlaceHere(ctx, "/Users/me/x", "github.com/o/x", "mac")
	do(t, "PATCH", api+"/tasks/"+a, map[string]any{"places_add": []map[string]any{{"place": "github.com/o/x", "role": "primary"}}}, 200)

	// rename and summary
	ren := do(t, "PATCH", api+"/tasks/"+a, map[string]any{"title": "Коты", "summary": "про **котов**"}, 200)
	if ren["title"] != "Коты" || ren["summary"] != "про **котов**" {
		t.Fatalf("rename %v", ren)
	}
	// places carry locations on all machines, with or without ?machine=
	for _, q := range []string{"", "?machine=laptop"} {
		ov := do(t, "GET", api+"/tasks/"+a+q, nil, 200)
		if locs := ov["places"].([]any)[0].(map[string]any)["locations"].([]any); len(locs) != 2 {
			t.Fatalf("locations %s %v", q, locs)
		}
	}

	// owner revision with base_revision
	st.WriteDoc(ctx, store.WriteDocParams{Task: a, Slug: "plan", Body: "- [ ] шаг\n", Author: store.AuthorAgent})
	w := do(t, "PUT", api+"/tasks/"+a+"/docs/plan", map[string]any{"body": "Про котиков.\n\n- [ ] шаг\n", "base_revision": 1, "message": "owner edit"}, 200)
	if w["revision"].(float64) != 2 || w["items"].([]any)[0].(map[string]any)["id"] != "i1" {
		t.Fatalf("owner revision %v", w)
	}
	do(t, "PUT", api+"/tasks/"+a+"/docs/plan", map[string]any{"body": "другое\n", "base_revision": 1}, 409)

	// revert: r3 = body of r1, owner, message; stale base 409; same body no-op; missing r 404
	do(t, "POST", api+"/tasks/"+a+"/docs/plan/revert", map[string]any{"to": 1, "base_revision": 1}, 409)
	rv := do(t, "POST", api+"/tasks/"+a+"/docs/plan/revert", map[string]any{"to": 1, "base_revision": 2}, 200)
	if rv["revision"].(float64) != 3 || rv["unchanged"] != false || rv["items"].([]any)[0].(map[string]any)["id"] != "i1" {
		t.Fatalf("revert %v", rv)
	}
	revs := do(t, "GET", api+"/tasks/"+a+"/docs/plan/revisions", nil, 200)["revisions"].([]any)
	if last := revs[2].(map[string]any); last["author"] != "owner" || last["message"] != "revert to r1" {
		t.Fatalf("revert revision %v", last)
	}
	if rv = do(t, "POST", api+"/tasks/"+a+"/docs/plan/revert", map[string]any{"to": 1}, 200); rv["unchanged"] != true || rv["revision"].(float64) != 3 {
		t.Fatalf("revert no-op %v", rv)
	}
	do(t, "POST", api+"/tasks/"+a+"/docs/plan/revert", map[string]any{"to": 9}, 404)
	// put the owner's text back for the search checks below
	do(t, "POST", api+"/tasks/"+a+"/docs/plan/revert", map[string]any{"to": 2, "base_revision": 3}, 200)

	// search hits carry kind and url; todos are found too
	st.AddTodo(ctx, store.NewTodo{Text: "Котиков покормить"})
	s := do(t, "GET", api+"/tasks?q=котики", nil, 200)
	hits := s["tasks"].([]any)[0].(map[string]any)["hits"].([]any)
	var docHit map[string]any
	for _, h := range hits {
		if hm := h.(map[string]any); hm["kind"] == "document" {
			docHit = hm
		}
	}
	if docHit == nil || docHit["url"] != "https://tasks.example/t/"+a+"/d/plan/r/4?at=4-11" || docHit["line"].(float64) != 1 {
		t.Fatalf("document hit %v", hits)
	}
	todos := s["todos"].([]any)
	if len(todos) != 1 || todos[0].(map[string]any)["kind"] != "todo" || todos[0].(map[string]any)["url"] != "https://tasks.example/todos/D-1" {
		t.Fatalf("todo hits %v", todos)
	}

	// bulk
	bt := do(t, "POST", api+"/tasks/bulk", map[string]any{"ids": []string{a, b}, "status": "archived", "closed_reason": "cancelled"}, 200)
	if l := bt["tasks"].([]any); len(l) != 2 || l[1].(map[string]any)["closed_reason"] != "cancelled" {
		t.Fatalf("bulk tasks %v", bt)
	}
	do(t, "POST", api+"/tasks/bulk", map[string]any{"ids": []string{a, "T-99"}, "status": "active"}, 404)
	if got := do(t, "GET", api+"/tasks/"+a, nil, 200); got["status"] != "archived" {
		t.Fatalf("bulk not atomic %v", got["status"])
	}
	do(t, "POST", api+"/tasks/bulk", map[string]any{"ids": []string{b}, "status": "archived", "closed_reason": "merged"}, 400)
	do(t, "POST", api+"/tasks/bulk", map[string]any{"ids": []string{a}}, 400)
	st.AddTodo(ctx, store.NewTodo{Text: "второй"})
	btd := do(t, "POST", api+"/todos/bulk", map[string]any{"ids": []string{"D-1", "D-2"}, "state": "done"}, 200)
	if l := btd["todos"].([]any); len(l) != 2 || l[0].(map[string]any)["state"] != "done" {
		t.Fatalf("bulk todos %v", btd)
	}
	do(t, "POST", api+"/todos/bulk", map[string]any{"ids": []string{"D-1"}, "state": "in_task"}, 400)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
