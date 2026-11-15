package store

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

// notifier wakes waiters on every committed change. It is in-process: the
// server is the only writer of the database. Waiters re-read what they need
// after waking, so a broadcast carries no data.
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

// Changes returns a channel that is closed at the next committed change of
// any data (a transaction that modified at least one row). Take the channel
// before reading, then read, then wait on it: a change between the read and
// the wait still closes it. Used by the web event stream and by WaitRounds.
func (s *Store) Changes() <-chan struct{} { return s.changes.wait() }

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
	res, err := s.WaitReview(ctx, p)
	return res.Rounds, err
}

// WaitResult is what review_wait returns: pending rounds, and for a single
// document (Task and Slug given) the approval of its latest revision.
type WaitResult struct {
	Rounds   []Round
	Approved *Approval
}

// WaitReview is WaitRounds that also returns when the owner approves the
// latest revision of the document p names (only when p.Slug is given: an
// approval stays until the next revision, so waiting on a whole task would
// return at once for every approved document). An approval is not consumed:
// waiting again returns it again until a new revision is written.
func (s *Store) WaitReview(ctx context.Context, p WaitParams) (WaitResult, error) {
	if p.Slug != "" && p.Task == "" {
		return WaitResult{}, invalid("doc needs task: a document slug is only unique within a task")
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
		woke := s.Changes()
		rs, err := s.PendingRounds(ctx, p.Task)
		if err != nil {
			return WaitResult{}, err
		}
		var approved *Approval
		if p.Slug != "" {
			if approved, err = s.docApproval(ctx, p.Task, p.Slug); err != nil {
				return WaitResult{}, err
			}
			var keep []Round
			for _, r := range rs {
				if r.Slug == p.Slug {
					keep = append(keep, r)
				}
			}
			rs = keep
		}
		if len(rs) > 0 || approved != nil {
			if rs == nil {
				rs = []Round{}
			}
			return WaitResult{Rounds: rs, Approved: approved}, nil
		}
		for waiting := true; waiting; {
			select {
			case <-ctx.Done():
				return WaitResult{}, ctx.Err()
			case <-timer.C:
				return WaitResult{Rounds: []Round{}}, nil
			case <-tick:
				p.Tick(time.Since(start))
			case <-woke:
				waiting = false
			}
		}
	}
}

// docApproval returns the approval of the document's latest revision, or
// nil; it fails when the document does not exist.
func (s *Store) docApproval(ctx context.Context, task, slug string) (*Approval, error) {
	n, err := ParseTaskID(task)
	if err != nil {
		return nil, err
	}
	var a *Approval
	err = s.tx(ctx, func(tx *sql.Tx) error {
		d, err := getDoc(ctx, tx, n, slug)
		a = d.Approved
		return err
	})
	return a, err
}
