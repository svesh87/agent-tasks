package store

import (
	"testing"
)

// Every write wakes Changes (the web event stream relies on it); reads do not.
func TestChangesOnEveryWrite(t *testing.T) {
	s := newStore(t)
	var (
		task    Task
		round   Round
		comment Comment
		todo    Todo
	)
	steps := []struct {
		name string
		do   func() error
	}{
		{"CreateTask", func() (err error) { task, err = s.CreateTask(ctx, NewTask{Title: "t"}); return }},
		{"UpdateTask", func() error { _, err := s.UpdateTask(ctx, task.ID, TaskPatch{Title: ptr("t2")}); return err }},
		{"UpdateTasks", func() error {
			_, err := s.UpdateTasks(ctx, []string{task.ID}, TaskPatch{Status: ptr("later")})
			return err
		}},
		{"WriteDoc", func() error {
			_, err := s.WriteDoc(ctx, WriteDocParams{Task: task.ID, Slug: "plan", Body: "- [ ] a\n", Author: AuthorAgent})
			return err
		}},
		{"WriteDoc r2", func() error {
			_, err := s.WriteDoc(ctx, WriteDocParams{Task: task.ID, Slug: "plan", Body: "- [ ] a\n- [ ] b\n", Author: AuthorAgent})
			return err
		}},
		{"RevertDoc", func() error { _, err := s.RevertDoc(ctx, task.ID, "plan", 1, 0); return err }},
		{"SetPlanItem", func() error { _, err := s.SetPlanItem(ctx, task.ID, "plan", "i1", ptr(true), "", ""); return err }},
		{"AppendJournal", func() error { _, err := s.AppendJournal(ctx, task.ID, "log", "x", ""); return err }},
		{"DraftRound", func() (err error) { round, err = s.DraftRound(ctx, task.ID, "plan", 0); return }},
		{"SetRoundSummary", func() error { _, err := s.SetRoundSummary(ctx, round.ID, "sum"); return err }},
		{"AddComment", func() (err error) { comment, err = s.AddComment(ctx, round.ID, NewComment{Body: "c"}); return }},
		{"EditComment", func() error { _, err := s.EditComment(ctx, comment.ID, "c2"); return err }},
		{"SubmitRound", func() error { _, err := s.SubmitRound(ctx, round.ID); return err }},
		{"ReplyComment", func() error {
			_, err := s.ReplyComment(ctx, ReplyParams{Comment: comment.ID, Author: "agent", Body: "ok", Resolve: true})
			return err
		}},
		{"RoundAddressed", func() error { _, err := s.RoundAddressed(ctx, round.ID, "done"); return err }},
		{"PlaceHere", func() error {
			_, err := s.PlaceHere(ctx, "/home/u/repo", "git@github.com:acme/repo.git", "laptop")
			return err
		}},
		{"AddTodo", func() (err error) { todo, err = s.AddTodo(ctx, NewTodo{Text: "later"}); return }},
		{"UpdateTodo", func() error { _, err := s.UpdateTodo(ctx, todo.ID, TodoPatch{Text: ptr("later!")}); return err }},
		{"UpdateTodos", func() error {
			_, err := s.UpdateTodos(ctx, []string{todo.ID}, TodoPatch{State: ptr("done")})
			return err
		}},
		{"DeleteTask", func() error { return s.DeleteTask(ctx, task.ID) }},
	}
	for _, st := range steps {
		ch := s.Changes()
		if err := st.do(); err != nil {
			t.Fatalf("%s: %v", st.name, err)
		}
		select {
		case <-ch:
		default:
			t.Errorf("%s: no change broadcast", st.name)
		}
	}

	// Reads and no-op writes do not wake anyone.
	t2 := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "u"}))
	must[WriteResult](t)(s.WriteDoc(ctx, WriteDocParams{Task: t2.ID, Slug: "plan", Body: "x\n", Author: AuthorAgent}))
	ch := s.Changes()
	if _, err := s.GetTask(ctx, t2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetDoc(ctx, t2.ID, "plan", DocGetOptions{OwnerView: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteDoc(ctx, WriteDocParams{Task: t2.ID, Slug: "plan", Body: "x\n", Author: AuthorAgent}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch:
		t.Error("a read or an unchanged write broadcast a change")
	default:
	}
}
