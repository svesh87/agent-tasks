package store

import (
	"context"
	"sync"
	"time"
)

// notifier wakes waiters when a review round is submitted. It is
// in-process: the server is the only writer of rounds.
type notifier struct {
	mu sync.Mutex
	ch chan struct{}
}

// wait returns a channel closed at the next broadcast.
func (n *notifier) wait() <-chan struct{} {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.ch == nil {
		n.ch = make(chan struct{})
	}
	return n.ch
}

func (n *notifier) broadcast() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.ch != nil {
		close(n.ch)
		n.ch = nil
	}
}

// WaitParams select which rounds review_wait waits for.
type WaitParams struct {
	Task    string        // "" = any task
	Slug    string        // document within Task; "" = any document
	Timeout time.Duration // how long to block
	// Tick, if set, is called every TickEvery while waiting (keep-alive,
	// e.g. MCP progress notifications).
	Tick      func(elapsed time.Duration)
	TickEvery time.Duration
}

// WaitRounds returns submitted, not addressed rounds matching p as soon as
// there are any: immediately if some are already pending, else when the
// owner submits one, or an empty list when the timeout passes. It returns
// ctx.Err() when the caller cancels.
func (s *Store) WaitRounds(ctx context.Context, p WaitParams) ([]Round, error) {
	if p.Slug != "" && p.Task == "" {
		return nil, invalid("doc needs task: a document slug is only unique within a task")
	}
	timer := time.NewTimer(p.Timeout)
	defer timer.Stop()
	var tick <-chan time.Time
	if p.Tick != nil && p.TickEvery > 0 {
		t := time.NewTicker(p.TickEvery)
		defer t.Stop()
		tick = t.C
	}
	start := time.Now()
	for {
		// Take the channel before reading, so a submit between the read and
		// the select still wakes us.
		woke := s.rounds.wait()
		rs, err := s.PendingRounds(ctx, p.Task)
		if err != nil {
			return nil, err
		}
		if p.Slug != "" {
			if err := s.docExists(ctx, p.Task, p.Slug); err != nil {
				return nil, err
			}
			var keep []Round
			for _, r := range rs {
				if r.Slug == p.Slug {
					keep = append(keep, r)
				}
			}
			rs = keep
		}
		if len(rs) > 0 {
			return rs, nil
		}
		for waiting := true; waiting; {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-timer.C:
				return []Round{}, nil
			case <-tick:
				p.Tick(time.Since(start))
			case <-woke:
				waiting = false
			}
		}
	}
}

func (s *Store) docExists(ctx context.Context, task, slug string) error {
	_, err := s.Revisions(ctx, task, slug)
	return err
}
