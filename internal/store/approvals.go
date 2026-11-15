package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Approval is the owner's "go" on a document revision. It counts while that
// revision is the latest; a new revision leaves the document unapproved
// (the old revision keeps its mark in history). Only the owner approves.
type Approval struct {
	Revision int    `json:"revision"`
	At       string `json:"at"`
	By       string `json:"by"` // always owner
}

// approvalOf returns the live approval of a revision, or nil.
func approvalOf(ctx context.Context, q queryer, revID int64, n int) (*Approval, error) {
	var at string
	err := q.QueryRowContext(ctx, `SELECT created_at FROM approvals WHERE revision_id = ? AND revoked_at IS NULL ORDER BY id DESC LIMIT 1`, revID).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &Approval{Revision: n, At: at, By: AuthorOwner}, nil
}

// latestApproval returns the approval of the document's latest revision, or nil.
func latestApproval(ctx context.Context, q queryer, d Document) (*Approval, error) {
	if d.LatestRevision == 0 {
		return nil, nil
	}
	var revID int64
	if err := q.QueryRowContext(ctx, `SELECT id FROM revisions WHERE document_id = ? AND n = ?`, d.id, d.LatestRevision).Scan(&revID); err != nil {
		return nil, err
	}
	return approvalOf(ctx, q, revID, d.LatestRevision)
}

// approvedRevisions lists the revisions of a document that carry a live
// approval (history: older ones no longer count).
func approvedRevisions(ctx context.Context, q queryer, docID int64) ([]int, error) {
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT r.n FROM approvals a JOIN revisions r ON r.id = a.revision_id
WHERE r.document_id = ? AND a.revoked_at IS NULL ORDER BY r.n`, docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int{}
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ApproveResult is the outcome of Approve.
type ApproveResult struct {
	Document Document `json:"document"`
	Approval Approval `json:"approval"`
	// Round is the draft round submitted together with the approval
	// ("approve with comments"), if there was one with comments.
	Round *Round `json:"round,omitempty"`
}

// Approve records the owner's approval of revision n, which must be the
// latest (else a conflict: the document moved on). With submitDraft, a draft
// round with comments is submitted first, so the agent gets both. Approving
// an approved revision again changes nothing. A decision entry goes to the
// journal.
func (s *Store) Approve(ctx context.Context, task, slug string, n int, submitDraft bool) (ApproveResult, error) {
	num, err := ParseTaskID(task)
	if err != nil {
		return ApproveResult{}, err
	}
	var res ApproveResult
	if submitDraft {
		var draftID int64
		var comments int
		err := s.tx(ctx, func(tx *sql.Tx) error {
			d, err := getDoc(ctx, tx, num, slug)
			if err != nil {
				return err
			}
			if n > 0 && n != d.LatestRevision {
				return staleApproval(d, n)
			}
			err = tx.QueryRowContext(ctx, `SELECT rr.id, (SELECT count(*) FROM comments c WHERE c.round_id = rr.id)
FROM review_rounds rr WHERE rr.document_id = ? AND rr.state = 'draft'`, d.id).Scan(&draftID, &comments)
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		})
		if err != nil {
			return ApproveResult{}, err
		}
		if draftID != 0 && comments > 0 {
			r, err := s.SubmitRound(ctx, draftID)
			if err != nil {
				return ApproveResult{}, err
			}
			res.Round = &r
		}
	}
	err = s.tx(ctx, func(tx *sql.Tx) error {
		d, err := getDoc(ctx, tx, num, slug)
		if err != nil {
			return err
		}
		if n <= 0 {
			return invalid("approve needs the revision number the owner saw")
		}
		if n != d.LatestRevision {
			return staleApproval(d, n)
		}
		r, err := getRevision(ctx, tx, d, n)
		if err != nil {
			return err
		}
		a, err := approvalOf(ctx, tx, r.id, n)
		if err != nil {
			return err
		}
		if a == nil {
			now := s.ts()
			if _, err := tx.ExecContext(ctx, `INSERT INTO approvals (revision_id, created_at) VALUES (?, ?)`, r.id, now); err != nil {
				return err
			}
			body := fmt.Sprintf("Owner approved %s r%d in the web UI.", slug, n)
			if res.Round != nil {
				body = fmt.Sprintf("Owner approved %s r%d in the web UI, with comments (review round %d).", slug, n, res.Round.N)
			}
			if err := s.journalTx(ctx, tx, num, "decision", body, "web", now); err != nil {
				return err
			}
			a = &Approval{Revision: n, At: now, By: AuthorOwner}
		}
		d.Approved = a
		res.Document, res.Approval = d, *a
		return nil
	})
	return res, err
}

// Revoke withdraws the approval of the latest revision.
func (s *Store) Revoke(ctx context.Context, task, slug string) (Document, error) {
	num, err := ParseTaskID(task)
	if err != nil {
		return Document{}, err
	}
	var out Document
	err = s.tx(ctx, func(tx *sql.Tx) error {
		d, err := getDoc(ctx, tx, num, slug)
		if err != nil {
			return err
		}
		if d.Approved == nil {
			return conflict("%s/%s r%d is not approved; nothing to withdraw", d.Task, d.Slug, d.LatestRevision)
		}
		now := s.ts()
		if _, err := tx.ExecContext(ctx, `UPDATE approvals SET revoked_at = ? WHERE revoked_at IS NULL AND revision_id =
  (SELECT id FROM revisions WHERE document_id = ? AND n = ?)`, now, d.id, d.LatestRevision); err != nil {
			return err
		}
		if err := s.journalTx(ctx, tx, num, "decision", fmt.Sprintf("Owner withdrew the approval of %s r%d.", slug, d.LatestRevision), "web", now); err != nil {
			return err
		}
		d.Approved = nil
		out = d
		return nil
	})
	return out, err
}

func staleApproval(d Document, n int) error {
	return conflict("revision %d of %s/%s is not the latest (%d): read the new revision before approving", n, d.Task, d.Slug, d.LatestRevision)
}

// Approvals lists the documents whose latest revision is approved: of one
// task, or of all non-archived tasks when task is "".
func (s *Store) Approvals(ctx context.Context, task string) ([]DocApproval, error) {
	where, args := `t.status != 'archived'`, []any{}
	if task != "" {
		n, err := ParseTaskID(task)
		if err != nil {
			return nil, err
		}
		where, args = `t.id = ?`, []any{n}
	}
	var out []DocApproval
	err := s.tx(ctx, func(tx *sql.Tx) (err error) {
		if len(args) > 0 {
			if _, err := getTask(ctx, tx, args[0].(int64)); err != nil {
				return err
			}
		}
		out, err = docApprovals(ctx, tx, where, args...)
		return err
	})
	return out, err
}

// DocApproval is an approved latest revision of a document.
type DocApproval struct {
	Task string `json:"task"`
	Slug string `json:"slug"`
	Approval
}

func docApprovals(ctx context.Context, q queryer, where string, args ...any) ([]DocApproval, error) {
	rows, err := q.QueryContext(ctx, `
SELECT t.id, d.slug, r.n, a.created_at
FROM approvals a
JOIN revisions r ON r.id = a.revision_id
JOIN documents d ON d.id = r.document_id
JOIN tasks t ON t.id = d.task_id
WHERE a.revoked_at IS NULL
  AND r.n = (SELECT max(n) FROM revisions r2 WHERE r2.document_id = d.id)
  AND `+where+`
GROUP BY d.id ORDER BY t.id, d.slug`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DocApproval{}
	for rows.Next() {
		var a DocApproval
		var tid int64
		if err := rows.Scan(&tid, &a.Slug, &a.Revision, &a.At); err != nil {
			return nil, err
		}
		a.Task, a.By = TaskID(tid), AuthorOwner
		out = append(out, a)
	}
	return out, rows.Err()
}
