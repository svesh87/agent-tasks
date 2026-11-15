package store

import (
	"errors"
	"strings"
	"testing"

	"github.com/svesh87/agent-tasks/internal/anchor"
)

func TestSearchHitKindsAndPositions(t *testing.T) {
	s := newStore(t)
	task := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "Сервер", Summary: "Про кошек немного"}))
	body := "# План\n\nвступление\n\nЗдесь говорится про кошку и собак.\n"
	must[WriteResult](t)(s.WriteDoc(ctx, WriteDocParams{Task: task.ID, Slug: "plan", Body: body, Author: AuthorAgent}))
	r := must[Round](t)(s.DraftRound(ctx, task.ID, "plan", 0))
	c := must[Comment](t)(s.AddComment(ctx, r.ID, NewComment{Body: "а кошки точно нужны?"}))
	must[Round](t)(s.SetRoundSummary(ctx, r.ID, "в целом про кошек ок"))
	must[Round](t)(s.SubmitRound(ctx, r.ID))
	e := must[JournalEntry](t)(s.AppendJournal(ctx, task.ID, "log", "покормил кошку", ""))
	must[Todo](t)(s.AddTodo(ctx, NewTodo{Text: "Купить корм для кошки"}))

	res := must[SearchResult](t)(s.Search(ctx, SearchParams{Query: "кошка", Todos: true}))
	if res.Total != 1 {
		t.Fatalf("total %d", res.Total)
	}
	kinds := map[string]Hit{}
	for _, h := range res.Tasks[0].Hits {
		kinds[h.Kind] = h
	}
	// snippets() keeps the 3 best sources; check what came back is located
	if len(kinds) < 3 {
		t.Fatalf("hits %+v", res.Tasks[0].Hits)
	}
	for k, h := range kinds {
		switch k {
		case "document":
			if h.Slug != "plan" || h.Revision != 1 || h.Line != 5 || string([]rune(body)[h.Start:h.End]) != "кошку" {
				t.Errorf("document hit %+v", h)
			}
		case "comment":
			if h.Comment != c.ID || h.Round != r.ID || h.Slug != "plan" || h.Revision != 1 {
				t.Errorf("comment hit %+v", h)
			}
		case "round":
			if h.Round != r.ID || h.Slug != "plan" || h.Revision != 1 {
				t.Errorf("round hit %+v", h)
			}
		case "journal":
			if h.Journal != e.ID {
				t.Errorf("journal hit %+v", h)
			}
		case "task":
		default:
			t.Errorf("unknown kind %q", k)
		}
	}
	if len(res.Todos) != 1 || !strings.Contains(res.Todos[0].Snippet.Marked(), "**кошки**") {
		t.Fatalf("todo hits %+v", res.Todos)
	}
	// title-only match in a document still points into the body when the body matches too
	must[WriteResult](t)(s.WriteDoc(ctx, WriteDocParams{Task: task.ID, Slug: "cats", Title: "Кошки", Body: "текст\n\nи снова кошки\n", Author: AuthorAgent}))
	res = must[SearchResult](t)(s.Search(ctx, SearchParams{Query: "кошки"}))
	for _, h := range res.Tasks[0].Hits {
		if h.Kind == "document" && h.Slug == "cats" && h.Line != 3 {
			t.Errorf("cats hit %+v", h)
		}
	}
}

func TestTagsSearch(t *testing.T) {
	s := newStore(t)
	must[Task](t)(s.CreateTask(ctx, NewTask{Title: "a", Tags: []string{"topic:backup", "area:work"}}))
	must[Task](t)(s.CreateTask(ctx, NewTask{Title: "b", Tags: []string{"area:work", "Бэкап"}}))
	must[Todo](t)(s.AddTodo(ctx, NewTodo{Text: "c", Tags: []string{"area:work", "backup-old"}}))

	all := must[[]TagUse](t)(s.Tags(ctx, "", 0))
	if len(all) != 4 || all[0].Tag != "area:work" || all[0].Count != 3 || all[0].Tasks != 2 || all[0].Todos != 1 {
		t.Fatalf("all %+v", all)
	}
	// prefix matches first, then substring matches
	got := must[[]TagUse](t)(s.Tags(ctx, "BACK", 0))
	if len(got) != 2 || got[0].Tag != "backup-old" || got[1].Tag != "topic:backup" {
		t.Fatalf("q=back %+v", got)
	}
	if got = must[[]TagUse](t)(s.Tags(ctx, "бэк", 0)); len(got) != 1 || got[0].Tag != "Бэкап" {
		t.Fatalf("cyrillic %+v", got)
	}
	if got = must[[]TagUse](t)(s.Tags(ctx, "", 2)); len(got) != 2 {
		t.Fatalf("limit %+v", got)
	}
}

func TestBulkUpdates(t *testing.T) {
	s := newStore(t)
	a := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "a"}))
	b := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "b"}))
	ts := must[[]Task](t)(s.UpdateTasks(ctx, []string{a.ID, b.ID}, TaskPatch{Status: ptr("archived"), ClosedReason: ptr("done")}))
	if len(ts) != 2 || ts[0].Status != "archived" || ts[1].ClosedReason != "done" {
		t.Fatalf("bulk archive %+v", ts)
	}
	// all or nothing: a missing task rolls back the others
	if _, err := s.UpdateTasks(ctx, []string{a.ID, "T-99"}, TaskPatch{Status: ptr("active")}); !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "T-99") {
		t.Fatalf("missing task: %v", err)
	}
	if got := must[Task](t)(s.GetTask(ctx, a.ID)); got.Status != "archived" {
		t.Fatalf("not rolled back %+v", got)
	}
	if _, err := s.UpdateTasks(ctx, []string{a.ID}, TaskPatch{Status: ptr("active"), ClosedReason: ptr("done")}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("closed_reason without archive: %v", err)
	}

	d1 := must[Todo](t)(s.AddTodo(ctx, NewTodo{Text: "one"}))
	d2 := must[Todo](t)(s.AddTodo(ctx, NewTodo{Text: "two"}))
	ds := must[[]Todo](t)(s.UpdateTodos(ctx, []string{d1.ID, d2.ID}, TodoPatch{State: ptr("dropped")}))
	if len(ds) != 2 || ds[0].State != "dropped" || ds[1].ClosedAt == "" {
		t.Fatalf("bulk drop %+v", ds)
	}
	if _, err := s.UpdateTodos(ctx, []string{d1.ID}, TodoPatch{State: ptr("in_task")}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bulk in_task: %v", err)
	}
	ds = must[[]Todo](t)(s.UpdateTodos(ctx, []string{d1.ID, d2.ID}, TodoPatch{State: ptr("open")}))
	if ds[0].State != "open" || ds[0].ClosedAt != "" {
		t.Fatalf("bulk reopen %+v", ds)
	}
}

func TestOwnerRevision(t *testing.T) {
	s := newStore(t)
	task := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "t"}))
	must[WriteResult](t)(s.WriteDoc(ctx, WriteDocParams{Task: task.ID, Slug: "plan", Author: AuthorAgent,
		Body: "# Plan\n\n- [ ] Write the role\n- [ ] Set up DNS\n\nUse Traefik for routing.\n"}))
	must[PlanItem](t)(s.SetPlanItem(ctx, task.ID, "plan", "i1", ptr(true), "", AuthorAgent))
	r := must[Round](t)(s.DraftRound(ctx, task.ID, "plan", 0))
	c := must[Comment](t)(s.AddComment(ctx, r.ID, NewComment{Anchor: &anchor.Anchor{Quote: "Traefik"}, Body: "?"}))
	must[Round](t)(s.SubmitRound(ctx, r.ID))

	// stale base revision is refused
	if _, err := s.WriteDoc(ctx, WriteDocParams{Task: task.ID, Slug: "plan", Author: AuthorOwner, BaseRevision: 7, Body: "x\n"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale base: %v", err)
	}
	w := must[WriteResult](t)(s.WriteDoc(ctx, WriteDocParams{Task: task.ID, Slug: "plan", Author: AuthorOwner, BaseRevision: 1,
		Body: "# Plan\n\nIntro by the owner.\n\n- [ ] Write the role\n- [ ] Set up the DNS\n\nUse Traefik for routing.\n"}))
	if w.Revision != 2 || !w.Items[0].Done || w.Items[0].ID != "i1" || w.Items[1].ID != "i2" || w.Reanchored != 1 {
		t.Fatalf("owner revision %+v", w)
	}
	v := must[DocView](t)(s.GetDoc(ctx, task.ID, "plan", DocGetOptions{WithRevisions: true}))
	if v.Revisions[1].Author != "owner" {
		t.Fatalf("author %+v", v.Revisions)
	}
	got := must[Comment](t)(s.GetComment(ctx, c.ID, false))
	if got.Current == nil || got.Current.Revision != 2 || got.Current.Line != 8 {
		t.Fatalf("re-anchored %+v", got.Current)
	}
}
