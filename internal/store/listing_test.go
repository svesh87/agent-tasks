package store

import (
	"errors"
	"testing"
)

func TestPlanItemBy(t *testing.T) {
	s := newStore(t)
	task := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "t"}))
	w := must[WriteResult](t)(s.WriteDoc(ctx, WriteDocParams{Task: task.ID, Slug: "plan", Body: "- [ ] a\n- [x] b\n", Author: AuthorAgent}))
	if w.Items[0].DoneBy != "" || w.Items[1].DoneBy != "agent" {
		t.Fatalf("initial %+v", w.Items)
	}
	it := must[PlanItem](t)(s.SetPlanItem(ctx, task.ID, "plan", "i1", ptr(true), "done by hand", AuthorOwner))
	if !it.Done || it.DoneBy != "owner" || it.Notes[0].By != "owner" {
		t.Fatalf("owner tick %+v", it)
	}
	it = must[PlanItem](t)(s.SetPlanItem(ctx, task.ID, "plan", "i1", ptr(false), "", ""))
	if it.Done || it.DoneBy != "agent" {
		t.Fatalf("agent untick (default by) %+v", it)
	}
	it = must[PlanItem](t)(s.SetPlanItem(ctx, task.ID, "plan", "i2", nil, "note only", AuthorAgent))
	if it.DoneBy != "agent" || it.Notes[0].By != "agent" {
		t.Fatalf("note only keeps done_by %+v", it)
	}
	if _, err := s.SetPlanItem(ctx, task.ID, "plan", "i1", ptr(true), "", "robot"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad by: %v", err)
	}
	// an owner revision with a new [x] line records owner
	w = must[WriteResult](t)(s.WriteDoc(ctx, WriteDocParams{Task: task.ID, Slug: "plan", Body: "- [ ] a\n- [x] b\n- [x] c\n", Author: AuthorOwner}))
	if w.Items[2].DoneBy != "owner" {
		t.Fatalf("owner revision %+v", w.Items[2])
	}
}

func TestSearchFacetsAndRows(t *testing.T) {
	s := newStore(t)
	a := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "a", Tags: []string{"x", "y"}}))
	must[Task](t)(s.CreateTask(ctx, NewTask{Title: "b", Tags: []string{"x"}, Status: "later"}))
	c := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "c", Tags: []string{"z"}}))
	must[Task](t)(s.UpdateTask(ctx, c.ID, TaskPatch{Status: ptr("archived"), ClosedReason: ptr("done")}))
	must[WriteResult](t)(s.WriteDoc(ctx, WriteDocParams{Task: a.ID, Slug: "notes", Body: "- [x] n\n", Author: AuthorAgent}))
	must[WriteResult](t)(s.WriteDoc(ctx, WriteDocParams{Task: a.ID, Slug: "plan", Body: "- [ ] one\n- [x] two\n- [ ] three\n", Author: AuthorAgent}))
	r := must[Round](t)(s.DraftRound(ctx, a.ID, "plan", 0))
	must[Comment](t)(s.AddComment(ctx, r.ID, NewComment{Body: "c"}))
	must[Round](t)(s.SubmitRound(ctx, r.ID))
	must[Round](t)(s.DraftRound(ctx, a.ID, "notes", 0))

	res := must[SearchResult](t)(s.Search(ctx, SearchParams{Statuses: []string{"active"}, Facets: true, RowStats: true}))
	if res.Total != 1 || res.StatusCounts["active"] != 1 || res.StatusCounts["later"] != 1 || res.StatusCounts["archived"] != 1 || res.StatusCounts["waiting"] != 0 {
		t.Fatalf("status counts %+v", res.StatusCounts)
	}
	if len(res.TagCounts) != 2 || res.TagCounts[0].Tag != "x" {
		t.Fatalf("tag counts %+v", res.TagCounts)
	}
	row := res.Tasks[0].TaskRow
	if row == nil || row.DocCount != 2 || row.SubmittedRounds != 1 || row.DraftRounds != 1 || row.PlanProgress == nil || row.PlanProgress.Total != 3 || row.PlanProgress.Done != 1 {
		t.Fatalf("row %+v %+v", row, row.PlanProgress)
	}
	res = must[SearchResult](t)(s.Search(ctx, SearchParams{Tags: []string{"x"}, Facets: true}))
	if res.Total != 2 || res.StatusCounts["archived"] != 0 || res.TagCounts[0].Tag != "x" || res.TagCounts[0].Count != 2 {
		t.Fatalf("tag filter facets %+v", res)
	}
	must[Round](t)(s.RoundAddressed(ctx, r.ID, "ok"))
	must[WriteResult](t)(s.WriteDoc(ctx, WriteDocParams{Task: a.ID, Slug: "plan", Body: "- [ ] one\n", Author: AuthorAgent}))
	got := must[Round](t)(s.GetRound(ctx, r.ID, false))
	if got.AddressedRevision != 1 {
		t.Fatalf("addressed_revision %+v", got)
	}
}

func TestTodoFacets(t *testing.T) {
	s := newStore(t)
	must[Todo](t)(s.AddTodo(ctx, NewTodo{Text: "a", Tags: []string{"x"}}))
	must[Todo](t)(s.AddTodo(ctx, NewTodo{Text: "b", Tags: []string{"x", "y"}}))
	d := must[Todo](t)(s.AddTodo(ctx, NewTodo{Text: "c", Tags: []string{"z"}}))
	must[Todo](t)(s.UpdateTodo(ctx, d.ID, TodoPatch{State: ptr("done")}))
	l := must[TodoList](t)(s.ListTodos(ctx, TodoListParams{Facets: true}))
	if l.Total != 2 || l.StateCounts["open"] != 2 || l.StateCounts["done"] != 1 || l.StateCounts["in_task"] != 0 {
		t.Fatalf("state counts %+v", l.StateCounts)
	}
	if len(l.TagCounts) != 2 || l.TagCounts[0].Tag != "x" || l.TagCounts[0].Count != 2 {
		t.Fatalf("tag counts %+v", l.TagCounts)
	}
	l = must[TodoList](t)(s.ListTodos(ctx, TodoListParams{Tags: []string{"y"}, Facets: true}))
	if l.Total != 1 || l.StateCounts["open"] != 1 || l.StateCounts["done"] != 0 {
		t.Fatalf("filtered %+v", l)
	}
}
