package mcpserver

import (
	"strings"
	"testing"
)

func TestPlacesTodosMerge(t *testing.T) {
	e := setup(t)
	h := e.call("place_here", map[string]any{"cwd": "/home/u/src/infra", "remote": "https://u:secret@github.com/acme/infra.git", "machine": "laptop"})
	pl := h["place"].(map[string]any)
	if pl["key"] != "github.com/acme/infra" || h["path"] != "/home/u/src/infra" || h["created"] != true {
		t.Fatalf("place_here %v", h)
	}
	if strings.Contains(strings.Join([]string{pl["key"].(string), h["path"].(string)}, " "), "secret") {
		t.Fatal("credentials leaked")
	}

	d := e.call("todo_add", map[string]any{"text": "Проверить бэкапы", "places": []string{"github.com/acme/infra"}, "source": "laptop"})
	if d["id"] != "D-1" || d["url"] != "https://tasks.example/todos/D-1" {
		t.Fatalf("todo_add %v", d)
	}
	e.call("todo_add", map[string]any{"text": "Без места"})
	l := e.call("todo_list", map[string]any{"place": "none"})
	if l["total"].(float64) != 1 {
		t.Fatalf("todo_list none %v", l)
	}
	l = e.call("todo_list", map[string]any{"query": "бэкапа"})
	if l["total"].(float64) != 1 {
		t.Fatalf("todo_list query %v", l)
	}

	old := e.call("task_create", map[string]any{"title": "old", "places": []map[string]any{{"place": "git@github.com:acme/infra.git", "role": "primary"}}})
	h = e.call("place_here", map[string]any{"cwd": "/home/u/src/infra/roles", "machine": "laptop"})
	if h["path"] != "/home/u/src/infra" || len(h["tasks"].([]any)) != 1 || len(h["todos"].([]any)) != 1 {
		t.Fatalf("place_here 2 %v", h)
	}
	nt := e.call("task_create", map[string]any{"title": "new", "from_todos": []string{"D-1"}})
	if nt["id"] != "T-2" || len(nt["places"].([]any)) != 1 {
		t.Fatalf("task_create from %v", nt)
	}
	// merging is gone: unknown fields and the old close reason are rejected
	e.callErr("task_create", map[string]any{"title": "x", "from_tasks": []map[string]any{{"task": old["id"], "note": "n"}}})
	e.callErr("task_update", map[string]any{"id": old["id"], "merged_into": "T-2"})
	e.callErr("task_update", map[string]any{"id": old["id"], "status": "archived", "closed_reason": "merged"})
	e.call("task_update", map[string]any{"id": old["id"], "status": "archived", "closed_reason": "done"})
	g := e.call("task_get", map[string]any{"id": "T-2", "machine": "laptop"})
	if len(g["todos"].([]any)) != 1 {
		t.Fatalf("task_get todos %v", g)
	}
	pg := e.call("place_get", map[string]any{"key": "github.com/acme/infra"})
	if locs := pg["locations"].([]any); len(locs) != 1 {
		t.Fatalf("place_get %v", pg)
	}
	s := e.call("task_search", map[string]any{"place": "github.com/acme/infra", "status": []string{"active", "archived"}})
	if s["total"].(float64) != 2 {
		t.Fatalf("task_search place %v", s)
	}
	u := e.call("todo_update", map[string]any{"id": "D-2", "state": "done"})
	if u["state"] != "done" {
		t.Fatalf("todo_update %v", u)
	}
	if msg := e.callErr("todo_update", map[string]any{"id": "D-2", "state": "in_task"}); msg == "" {
		t.Fatal("in_task accepted")
	}
}
