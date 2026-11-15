package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/svesh87/agent-tasks/internal/anchor"
)

// Round is a review round with its comments.
type Round struct {
	ID          int64  `json:"round"`
	Task        string `json:"task"`
	Slug        string `json:"slug"`
	N           int    `json:"n"`
	Revision    int    `json:"revision"` // the reviewed revision
	State       string `json:"state"`    // draft | submitted | addressed
	Summary     string `json:"summary,omitempty"`
	Message     string `json:"message,omitempty"` // agent's note when addressed
	CreatedAt   string `json:"created_at"`
	SubmittedAt string `json:"submitted_at,omitempty"`
	AddressedAt string `json:"addressed_at,omitempty"`
	// latest revision of the document when the round was addressed
	AddressedRevision int       `json:"addressed_revision,omitempty"`
	Comments          []Comment `json:"comments"`

	docID int64
	revID int64
}

// Reply is an answer in a comment thread.
type Reply struct {
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

// CurrentAnchor is where an anchored comment sits in the newest revision in
// which its text was found.
type CurrentAnchor struct {
	Revision int    `json:"revision"`
	Quote    string `json:"quote"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
	Line     int    `json:"line"`
}

// Comment is a review comment. Anchor is nil for a general comment.
type Comment struct {
	ID         int64          `json:"id"`
	Round      int64          `json:"round"`
	RoundN     int            `json:"round_n"`
	RoundState string         `json:"round_state"`
	Task       string         `json:"task"`
	Slug       string         `json:"slug"`
	Revision   int            `json:"revision"` // the reviewed revision
	Anchor     *anchor.Anchor `json:"anchor,omitempty"`
	Line       int            `json:"line,omitempty"` // line of the anchor in the reviewed revision
	Current    *CurrentAnchor `json:"current,omitempty"`
	OutdatedIn int            `json:"outdated_in,omitempty"` // revision where the text was no longer found
	Body       string         `json:"body"`
	State      string         `json:"state"` // open | resolved | outdated
	Replies    []Reply        `json:"replies"`
	CreatedAt  string         `json:"created_at"`
	UpdatedAt  string         `json:"updated_at"`
}

// loadComments loads comments matching a condition over c (comments), rr
// (review_rounds) and d (documents).
func loadComments(ctx context.Context, q queryer, where string, args ...any) ([]Comment, error) {
	rows, err := q.QueryContext(ctx, `
SELECT c.id, rr.id, rr.n, rr.state, d.task_id, d.slug, rv.n, rv.id,
  c.quote, c.prefix, c.suffix, c.start_off, c.end_off,
  coalesce(cr.n, 0), coalesce(cr.id, 0), c.cur_quote, c.cur_start, c.cur_end,
  coalesce(orv.n, 0), c.body, c.state, c.created_at, c.updated_at
FROM comments c
JOIN review_rounds rr ON rr.id = c.round_id
JOIN documents d ON d.id = rr.document_id
JOIN revisions rv ON rv.id = rr.revision_id
LEFT JOIN revisions cr ON cr.id = c.cur_revision_id
LEFT JOIN revisions orv ON orv.id = c.outdated_revision_id
WHERE `+where+` ORDER BY c.id`, args...)
	if err != nil {
		return nil, err
	}
	type extra struct{ revID, curRevID int64 }
	var out []Comment
	var ex []extra
	for rows.Next() {
		var c Comment
		var taskNum, revID, curRevID int64
		var a anchor.Anchor
		var cur CurrentAnchor
		if err := rows.Scan(&c.ID, &c.Round, &c.RoundN, &c.RoundState, &taskNum, &c.Slug, &c.Revision, &revID,
			&a.Quote, &a.Prefix, &a.Suffix, &a.Start, &a.End,
			&cur.Revision, &curRevID, &cur.Quote, &cur.Start, &cur.End,
			&c.OutdatedIn, &c.Body, &c.State, &c.CreatedAt, &c.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		c.Task = TaskID(taskNum)
		if a.Quote != "" {
			c.Anchor = &a
			if cur.Revision > 0 {
				c.Current = &cur
			}
		}
		c.Replies = []Reply{}
		out = append(out, c)
		ex = append(ex, extra{revID, curRevID})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	bodies := map[int64]string{}
	body := func(id int64) (string, error) {
		if b, ok := bodies[id]; ok {
			return b, nil
		}
		var b string
		err := q.QueryRowContext(ctx, `SELECT body FROM revisions WHERE id = ?`, id).Scan(&b)
		bodies[id] = b
		return b, err
	}
	for i := range out {
		c := &out[i]
		if c.Anchor != nil {
			b, err := body(ex[i].revID)
			if err != nil {
				return nil, err
			}
			c.Line = anchor.Line(b, c.Anchor.Start)
			if c.Current != nil {
				cb, err := body(ex[i].curRevID)
				if err != nil {
					return nil, err
				}
				c.Current.Line = anchor.Line(cb, c.Current.Start)
			}
		}
		rrows, err := q.QueryContext(ctx, `SELECT author, body, created_at FROM comment_replies WHERE comment_id = ? ORDER BY id`, c.ID)
		if err != nil {
			return nil, err
		}
		for rrows.Next() {
			var r Reply
			if err := rrows.Scan(&r.Author, &r.Body, &r.CreatedAt); err != nil {
				rrows.Close()
				return nil, err
			}
			c.Replies = append(c.Replies, r)
		}
		rrows.Close()
	}
	if out == nil {
		out = []Comment{}
	}
	return out, nil
}

func loadRounds(ctx context.Context, q queryer, where string, args ...any) ([]Round, error) {
	rows, err := q.QueryContext(ctx, `
SELECT rr.id, rr.document_id, rr.revision_id, d.task_id, d.slug, rr.n, rv.n, rr.state, rr.summary, rr.message,
  rr.created_at, coalesce(rr.submitted_at, ''), coalesce(rr.addressed_at, ''), rr.addressed_revision
FROM review_rounds rr JOIN documents d ON d.id = rr.document_id JOIN revisions rv ON rv.id = rr.revision_id
WHERE `+where+` ORDER BY rr.id`, args...)
	if err != nil {
		return nil, err
	}
	var out []Round
	for rows.Next() {
		var r Round
		var taskNum int64
		if err := rows.Scan(&r.ID, &r.docID, &r.revID, &taskNum, &r.Slug, &r.N, &r.Revision, &r.State, &r.Summary, &r.Message,
			&r.CreatedAt, &r.SubmittedAt, &r.AddressedAt, &r.AddressedRevision); err != nil {
			rows.Close()
			return nil, err
		}
		r.Task = TaskID(taskNum)
		out = append(out, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Comments, err = loadComments(ctx, q, `c.round_id = ?`, out[i].ID); err != nil {
			return nil, err
		}
	}
	if out == nil {
		out = []Round{}
	}
	return out, nil
}

func docRounds(ctx context.Context, q queryer, d Document, withDrafts bool) ([]Round, error) {
	where := `rr.document_id = ?`
	if !withDrafts {
		where += ` AND rr.state != 'draft'`
	}
	return loadRounds(ctx, q, where, d.id)
}

func getRound(ctx context.Context, q queryer, id int64) (Round, error) {
	rs, err := loadRounds(ctx, q, `rr.id = ?`, id)
	if err != nil {
		return Round{}, err
	}
	if len(rs) == 0 {
		return Round{}, notFound("review round %d not found", id)
	}
	return rs[0], nil
}

// GetRound returns one round. Draft rounds are returned only when ownerView.
func (s *Store) GetRound(ctx context.Context, id int64, ownerView bool) (Round, error) {
	var r Round
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		r, err = getRound(ctx, tx, id)
		if err == nil && r.State == "draft" && !ownerView {
			err = notFound("review round %d not found", id)
		}
		return err
	})
	return r, err
}

// PendingRounds returns submitted, not yet addressed rounds, for one task or
// (task == "") for all tasks.
func (s *Store) PendingRounds(ctx context.Context, task string) ([]Round, error) {
	where := `rr.state = 'submitted'`
	var args []any
	if task != "" {
		n, err := ParseTaskID(task)
		if err != nil {
			return nil, err
		}
		where += ` AND d.task_id = ?`
		args = append(args, n)
	}
	var out []Round
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if len(args) > 0 {
			if _, err := getTask(ctx, tx, args[0].(int64)); err != nil {
				return err
			}
		}
		var err error
		out, err = loadRounds(ctx, tx, where, args...)
		return err
	})
	return out, err
}

// Rounds lists all rounds of a document; drafts only when ownerView.
func (s *Store) Rounds(ctx context.Context, task, slug string, ownerView bool) ([]Round, error) {
	n, err := ParseTaskID(task)
	if err != nil {
		return nil, err
	}
	var out []Round
	err = s.tx(ctx, func(tx *sql.Tx) error {
		d, err := getDoc(ctx, tx, n, slug)
		if err != nil {
			return err
		}
		out, err = docRounds(ctx, tx, d, ownerView)
		return err
	})
	return out, err
}

// DraftRound returns the document's draft round, creating one for the given
// revision (0: latest) if there is none. A document has at most one draft.
func (s *Store) DraftRound(ctx context.Context, task, slug string, revision int) (Round, error) {
	n, err := ParseTaskID(task)
	if err != nil {
		return Round{}, err
	}
	var out Round
	err = s.tx(ctx, func(tx *sql.Tx) error {
		d, err := getDoc(ctx, tx, n, slug)
		if err != nil {
			return err
		}
		var id int64
		err = tx.QueryRowContext(ctx, `SELECT id FROM review_rounds WHERE document_id = ? AND state = 'draft'`, d.id).Scan(&id)
		if err == nil {
			out, err = getRound(ctx, tx, id)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		rev, err := getRevision(ctx, tx, d, revision)
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO review_rounds (document_id, n, revision_id, state, created_at)
VALUES (?, coalesce((SELECT max(n) FROM review_rounds WHERE document_id = ?), 0) + 1, ?, 'draft', ?)`, d.id, d.id, rev.id, s.ts())
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		out, err = getRound(ctx, tx, id)
		return err
	})
	return out, err
}

func (s *Store) withDraft(ctx context.Context, roundID int64, fn func(tx *sql.Tx, r Round) error) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		r, err := getRound(ctx, tx, roundID)
		if err != nil {
			return err
		}
		if r.State != "draft" {
			return conflict("round %d is %s; only a draft round can be changed", roundID, r.State)
		}
		return fn(tx, r)
	})
}

// SetRoundSummary sets the owner's general remark of a draft round.
func (s *Store) SetRoundSummary(ctx context.Context, roundID int64, summary string) (Round, error) {
	var out Round
	err := s.withDraft(ctx, roundID, func(tx *sql.Tx, r Round) error {
		if _, err := tx.ExecContext(ctx, `UPDATE review_rounds SET summary = ? WHERE id = ?`, summary, roundID); err != nil {
			return err
		}
		var err error
		out, err = getRound(ctx, tx, roundID)
		return err
	})
	return out, err
}

// DeleteDraftRound discards a draft round with its comments.
func (s *Store) DeleteDraftRound(ctx context.Context, roundID int64) error {
	return s.withDraft(ctx, roundID, func(tx *sql.Tx, r Round) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM review_rounds WHERE id = ?`, roundID)
		return err
	})
}

// SubmitRound turns a draft into a submitted round: the "review is ready"
// signal for agents. Anchored comments are carried to the latest revision.
func (s *Store) SubmitRound(ctx context.Context, roundID int64) (Round, error) {
	var out Round
	err := s.withDraft(ctx, roundID, func(tx *sql.Tx, r Round) error {
		if len(r.Comments) == 0 && strings.TrimSpace(r.Summary) == "" {
			return invalid("round %d is empty: add a comment or a summary before submitting", roundID)
		}
		now := s.ts()
		if _, err := tx.ExecContext(ctx, `UPDATE review_rounds SET state = 'submitted', submitted_at = ? WHERE id = ?`, now, roundID); err != nil {
			return err
		}
		taskNum, _ := ParseTaskID(r.Task)
		d, err := getDoc(ctx, tx, taskNum, r.Slug)
		if err != nil {
			return err
		}
		if d.LatestRevision != r.Revision {
			latest, err := getRevision(ctx, tx, d, 0)
			if err != nil {
				return err
			}
			if _, _, err := s.reanchor(ctx, tx, d, latest.id, latest.Body, now); err != nil {
				return err
			}
		}
		for _, c := range r.Comments {
			if err := indexComment(ctx, tx, c.ID); err != nil {
				return err
			}
		}
		if strings.TrimSpace(r.Summary) != "" {
			if err := indexRow(ctx, tx, taskNum, "round:"+strconv.FormatInt(roundID, 10), r.Summary); err != nil {
				return err
			}
		}
		if err := touchTask(ctx, tx, taskNum, now); err != nil {
			return err
		}
		out, err = getRound(ctx, tx, roundID)
		return err
	})
	return out, err // the commit wakes review_wait callers (Store.tx)
}

// NewComment holds a comment to add to a draft round. Anchor nil means a
// general comment. The anchor's quote is looked up in the reviewed revision:
// [start, end) is used when it holds the quote, else the nearest occurrence.
type NewComment struct {
	Anchor *anchor.Anchor
	Body   string
}

// AddComment adds a comment to a draft round.
func (s *Store) AddComment(ctx context.Context, roundID int64, nc NewComment) (Comment, error) {
	if strings.TrimSpace(nc.Body) == "" {
		return Comment{}, invalid("comment body is empty")
	}
	var out Comment
	err := s.withDraft(ctx, roundID, func(tx *sql.Tx, r Round) error {
		now := s.ts()
		var a anchor.Anchor
		var curRev any
		if nc.Anchor != nil && nc.Anchor.Quote != "" {
			var body string
			if err := tx.QueryRowContext(ctx, `SELECT body FROM revisions WHERE id = ?`, r.revID).Scan(&body); err != nil {
				return err
			}
			found, ok := anchor.Resolve(body, *nc.Anchor)
			if !ok {
				return invalid("the quoted text is not in revision %d of %s", r.Revision, r.Slug)
			}
			a = found
			curRev = r.revID
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO comments (round_id, body, state, quote, prefix, suffix, start_off, end_off,
  cur_revision_id, cur_quote, cur_prefix, cur_suffix, cur_start, cur_end, created_at, updated_at)
VALUES (?, ?, 'open', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			roundID, nc.Body, a.Quote, a.Prefix, a.Suffix, a.Start, a.End,
			curRev, a.Quote, a.Prefix, a.Suffix, a.Start, a.End, now, now)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		cs, err := loadComments(ctx, tx, `c.id = ?`, id)
		if err != nil {
			return err
		}
		out = cs[0]
		return nil
	})
	return out, err
}

func commentRound(ctx context.Context, q queryer, commentID int64) (int64, error) {
	var roundID int64
	err := q.QueryRowContext(ctx, `SELECT round_id FROM comments WHERE id = ?`, commentID).Scan(&roundID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, notFound("comment %d not found", commentID)
	}
	return roundID, err
}

// EditComment changes the body of a comment in a draft round.
func (s *Store) EditComment(ctx context.Context, commentID int64, body string) (Comment, error) {
	if strings.TrimSpace(body) == "" {
		return Comment{}, invalid("comment body is empty")
	}
	roundID, err := s.roundOf(ctx, commentID)
	if err != nil {
		return Comment{}, err
	}
	var out Comment
	err = s.withDraft(ctx, roundID, func(tx *sql.Tx, r Round) error {
		if _, err := tx.ExecContext(ctx, `UPDATE comments SET body = ?, updated_at = ? WHERE id = ?`, body, s.ts(), commentID); err != nil {
			return err
		}
		cs, err := loadComments(ctx, tx, `c.id = ?`, commentID)
		if err == nil {
			out = cs[0]
		}
		return err
	})
	return out, err
}

// DeleteComment removes a comment from a draft round.
func (s *Store) DeleteComment(ctx context.Context, commentID int64) error {
	roundID, err := s.roundOf(ctx, commentID)
	if err != nil {
		return err
	}
	return s.withDraft(ctx, roundID, func(tx *sql.Tx, r Round) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM comments WHERE id = ?`, commentID)
		return err
	})
}

func (s *Store) roundOf(ctx context.Context, commentID int64) (int64, error) {
	var id int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		id, err = commentRound(ctx, tx, commentID)
		return err
	})
	return id, err
}

// GetComment returns one comment; comments of draft rounds only when ownerView.
func (s *Store) GetComment(ctx context.Context, commentID int64, ownerView bool) (Comment, error) {
	var out Comment
	err := s.tx(ctx, func(tx *sql.Tx) error {
		cs, err := loadComments(ctx, tx, `c.id = ?`, commentID)
		if err != nil {
			return err
		}
		if len(cs) == 0 || (cs[0].RoundState == "draft" && !ownerView) {
			return notFound("comment %d not found", commentID)
		}
		out = cs[0]
		return nil
	})
	return out, err
}

// ReplyParams are the inputs of comment_reply.
type ReplyParams struct {
	Comment int64
	Author  string // agent | owner
	Body    string
	Resolve bool // set state to resolved
	Reopen  bool // owner only: set state back to open
}

// ReplyComment appends a reply to a comment of a submitted or addressed
// round and optionally resolves or reopens it. Body may be empty when only
// the state changes.
func (s *Store) ReplyComment(ctx context.Context, p ReplyParams) (Comment, error) {
	if strings.TrimSpace(p.Body) == "" && !p.Resolve && !p.Reopen {
		return Comment{}, invalid("reply body is empty")
	}
	if p.Resolve && p.Reopen {
		return Comment{}, invalid("pass resolve or reopen, not both")
	}
	var out Comment
	err := s.tx(ctx, func(tx *sql.Tx) error {
		cs, err := loadComments(ctx, tx, `c.id = ?`, p.Comment)
		if err != nil {
			return err
		}
		if len(cs) == 0 || (cs[0].RoundState == "draft" && p.Author != AuthorOwner) {
			return notFound("comment %d not found", p.Comment)
		}
		c := cs[0]
		if c.RoundState == "draft" {
			return conflict("comment %d is in a draft round; edit it instead of replying", p.Comment)
		}
		now := s.ts()
		if strings.TrimSpace(p.Body) != "" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO comment_replies (comment_id, author, body, created_at) VALUES (?, ?, ?, ?)`,
				p.Comment, p.Author, p.Body, now); err != nil {
				return err
			}
		}
		state := c.State
		switch {
		case p.Resolve:
			state = "resolved"
		case p.Reopen:
			state = "open"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE comments SET state = ?, updated_at = ? WHERE id = ?`, state, now, p.Comment); err != nil {
			return err
		}
		if err := indexComment(ctx, tx, p.Comment); err != nil {
			return err
		}
		taskNum, _ := ParseTaskID(c.Task)
		if err := touchTask(ctx, tx, taskNum, now); err != nil {
			return err
		}
		cs, err = loadComments(ctx, tx, `c.id = ?`, p.Comment)
		if err == nil {
			out = cs[0]
		}
		return err
	})
	return out, err
}

// RoundAddressed marks a submitted round as handled by the agent.
func (s *Store) RoundAddressed(ctx context.Context, roundID int64, message string) (Round, error) {
	var out Round
	err := s.tx(ctx, func(tx *sql.Tx) error {
		r, err := getRound(ctx, tx, roundID)
		if err != nil {
			return err
		}
		if r.State == "draft" {
			return notFound("review round %d not found", roundID)
		}
		if r.State == "addressed" {
			out = r
			return nil
		}
		now := s.ts()
		if _, err := tx.ExecContext(ctx, `UPDATE review_rounds SET state = 'addressed', addressed_at = ?, message = ?,
  addressed_revision = (SELECT coalesce(max(n), 0) FROM revisions WHERE document_id = review_rounds.document_id) WHERE id = ?`,
			now, strings.TrimSpace(message), roundID); err != nil {
			return err
		}
		taskNum, _ := ParseTaskID(r.Task)
		if err := touchTask(ctx, tx, taskNum, now); err != nil {
			return err
		}
		out, err = getRound(ctx, tx, roundID)
		return err
	})
	return out, err
}
