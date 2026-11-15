package store

import (
	"errors"
	"strings"
	"testing"
)

func TestApproval(t *testing.T) {
	s := newStore(t)
	task := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "t"}))
	write := func(body string) WriteResult {
		t.Helper()
		return must[WriteResult](t)(s.WriteDoc(ctx, WriteDocParams{Task: task.ID, Slug: "plan", Body: body, Author: AuthorAgent}))
	}
	write("- [ ] a\n")
	write("- [ ] a\n- [ ] b\n")

	if _, err := s.Approve(ctx, task.ID, "plan", 1, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("approving an old revision: %v, want conflict", err)
	}
	if _, err := s.Revoke(ctx, task.ID, "plan"); !errors.Is(err, ErrConflict) {
		t.Fatalf("revoking without approval: %v, want conflict", err)
	}
	res := must[ApproveResult](t)(s.Approve(ctx, task.ID, "plan", 2, false))
	if res.Approval.Revision != 2 || res.Approval.By != "owner" || res.Round != nil || res.Document.Approved == nil {
		t.Fatalf("approve: %+v", res)
	}
	again := must[ApproveResult](t)(s.Approve(ctx, task.ID, "plan", 2, false))
	if again.Approval.At != res.Approval.At {
		t.Fatalf("second approve changed the approval: %+v", again)
	}
	v := must[DocView](t)(s.GetDoc(ctx, task.ID, "plan", DocGetOptions{}))
	if v.Document.Approved == nil || v.Document.Approved.Revision != 2 || len(v.ApprovedRevisions) != 1 {
		t.Fatalf("doc after approve: %+v %v", v.Document.Approved, v.ApprovedRevisions)
	}
	o := must[Overview](t)(s.TaskOverview(ctx, task.ID, 10, false, ""))
	if o.Documents[0].Approved == nil || o.Documents[0].Approved.Revision != 2 {
		t.Fatalf("task overview: %+v", o.Documents[0])
	}
	if !strings.Contains(o.Journal[0].Body, "approved plan r2") || o.Journal[0].Kind != "decision" || o.Journal[0].Source != "web" {
		t.Fatalf("journal: %+v", o.Journal[0])
	}
	as := must[[]DocApproval](t)(s.Approvals(ctx, task.ID))
	if len(as) != 1 || as[0].Slug != "plan" || as[0].Revision != 2 {
		t.Fatalf("approvals: %+v", as)
	}

	// A new revision is not approved; r2 keeps its mark in history.
	wr := write("- [ ] a\n- [ ] b\n- [ ] c\n")
	if wr.Document.Approved != nil {
		t.Fatalf("new revision carries an approval: %+v", wr.Document.Approved)
	}
	v = must[DocView](t)(s.GetDoc(ctx, task.ID, "plan", DocGetOptions{}))
	if v.Document.Approved != nil || len(v.ApprovedRevisions) != 1 || v.ApprovedRevisions[0] != 2 {
		t.Fatalf("doc after r3: %+v %v", v.Document.Approved, v.ApprovedRevisions)
	}
	if as := must[[]DocApproval](t)(s.Approvals(ctx, task.ID)); len(as) != 0 {
		t.Fatalf("approvals after r3: %+v", as)
	}

	// Approve r3, then withdraw.
	must[ApproveResult](t)(s.Approve(ctx, task.ID, "plan", 3, false))
	d := must[Document](t)(s.Revoke(ctx, task.ID, "plan"))
	if d.Approved != nil {
		t.Fatalf("revoked doc still approved: %+v", d.Approved)
	}
	if v = must[DocView](t)(s.GetDoc(ctx, task.ID, "plan", DocGetOptions{})); v.Document.Approved != nil {
		t.Fatal("approval survived revoke")
	}
}

func TestApproveWithComments(t *testing.T) {
	s := newStore(t)
	task := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "t"}))
	must[WriteResult](t)(s.WriteDoc(ctx, WriteDocParams{Task: task.ID, Slug: "plan", Body: "- [ ] a\n", Author: AuthorAgent}))

	// An empty draft is left alone.
	empty := must[Round](t)(s.DraftRound(ctx, task.ID, "plan", 0))
	res := must[ApproveResult](t)(s.Approve(ctx, task.ID, "plan", 1, true))
	if res.Round != nil {
		t.Fatalf("empty draft submitted: %+v", res.Round)
	}
	if r := must[Round](t)(s.GetRound(ctx, empty.ID, true)); r.State != "draft" {
		t.Fatalf("empty draft state %s", r.State)
	}
	must[Document](t)(s.Revoke(ctx, task.ID, "plan"))

	must[Comment](t)(s.AddComment(ctx, empty.ID, NewComment{Body: "nit"}))
	res = must[ApproveResult](t)(s.Approve(ctx, task.ID, "plan", 1, true))
	if res.Round == nil || res.Round.State != "submitted" || res.Document.Approved == nil {
		t.Fatalf("approve with comments: %+v", res)
	}
	if rs := must[[]Round](t)(s.PendingRounds(ctx, task.ID)); len(rs) != 1 {
		t.Fatalf("pending rounds: %d", len(rs))
	}
}
