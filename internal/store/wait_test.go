package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func waitSetup(t *testing.T) (*Store, Task, Round) {
	t.Helper()
	s := newStore(t)
	task := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "t"}))
	for _, slug := range []string{"plan", "notes"} {
		must[WriteResult](t)(s.WriteDoc(ctx, WriteDocParams{Task: task.ID, Slug: slug, Body: "text\n", Author: AuthorAgent}))
	}
	r := must[Round](t)(s.DraftRound(ctx, task.ID, "plan", 0))
	must[Comment](t)(s.AddComment(ctx, r.ID, NewComment{Body: "fix it"}))
	return s, task, r
}

type waitResult struct {
	rounds []Round
	err    error
	took   time.Duration
}

func waitAsync(s *Store, c context.Context, p WaitParams) chan waitResult {
	ch := make(chan waitResult, 1)
	go func() {
		start := time.Now()
		rs, err := s.WaitRounds(c, p)
		ch <- waitResult{rs, err, time.Since(start)}
	}()
	return ch
}

func TestWaitWokenBySubmit(t *testing.T) {
	s, task, r := waitSetup(t)
	ch := waitAsync(s, ctx, WaitParams{Task: task.ID, Slug: "plan", Timeout: 10 * time.Second})
	time.Sleep(100 * time.Millisecond)
	select {
	case res := <-ch:
		t.Fatalf("returned before submit: %+v", res)
	default:
	}
	must[Round](t)(s.SubmitRound(ctx, r.ID))
	select {
	case res := <-ch:
		if res.err != nil || len(res.rounds) != 1 || res.rounds[0].ID != r.ID || res.took > 2*time.Second {
			t.Fatalf("wait result %+v", res)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("not woken by submit")
	}
	// already pending: returns at once
	rs := must[[]Round](t)(s.WaitRounds(ctx, WaitParams{Timeout: 10 * time.Second}))
	if len(rs) != 1 {
		t.Fatalf("pending %+v", rs)
	}
}

func TestWaitOtherDocDoesNotReturn(t *testing.T) {
	s, task, _ := waitSetup(t)
	ch := waitAsync(s, ctx, WaitParams{Task: task.ID, Slug: "notes", Timeout: 500 * time.Millisecond})
	time.Sleep(50 * time.Millisecond)
	// a round on "plan" wakes the waiter, which re-checks and keeps waiting
	r := must[Round](t)(s.DraftRound(ctx, task.ID, "plan", 0))
	must[Round](t)(s.SubmitRound(ctx, r.ID))
	res := <-ch
	if res.err != nil || len(res.rounds) != 0 || res.took < 450*time.Millisecond {
		t.Fatalf("expected timeout, got %+v", res)
	}
}

func TestWaitTimeoutAndTick(t *testing.T) {
	s, task, _ := waitSetup(t)
	ticks := 0
	rs, err := s.WaitRounds(ctx, WaitParams{Task: task.ID, Timeout: 300 * time.Millisecond, TickEvery: 50 * time.Millisecond, Tick: func(time.Duration) { ticks++ }})
	if err != nil || len(rs) != 0 || rs == nil {
		t.Fatalf("timeout: %v %v", rs, err)
	}
	if ticks < 3 {
		t.Fatalf("ticks %d", ticks)
	}
}

func TestWaitCancel(t *testing.T) {
	s, _, _ := waitSetup(t)
	c, cancel := context.WithCancel(ctx)
	ch := waitAsync(s, c, WaitParams{Timeout: 10 * time.Second})
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case res := <-ch:
		if !errors.Is(res.err, context.Canceled) {
			t.Fatalf("cancel: %+v", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel ignored")
	}
}

func TestWaitBadArgs(t *testing.T) {
	s, task, _ := waitSetup(t)
	if _, err := s.WaitRounds(ctx, WaitParams{Slug: "plan", Timeout: time.Second}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("doc without task: %v", err)
	}
	if _, err := s.WaitRounds(ctx, WaitParams{Task: task.ID, Slug: "nope", Timeout: time.Second}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing doc: %v", err)
	}
	if _, err := s.WaitRounds(ctx, WaitParams{Task: "T-99", Timeout: time.Second}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing task: %v", err)
	}
}
