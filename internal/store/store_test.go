package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/svesh87/agent-tasks/internal/anchor"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func ptr[T any](v T) *T { return &v }

func must[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

var ctx = context.Background()

func TestTaskLifecycle(t *testing.T) {
	s := newStore(t)
	task := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "Сервер задач", Tags: []string{"repo:github.com/acme/infra", "topic:mcp", "topic:mcp"}}))
	if task.ID != "T-1" || task.Status != "active" || len(task.Tags) != 2 {
		t.Fatalf("created %+v", task)
	}
	if _, err := s.UpdateTask(ctx, "T-1", TaskPatch{Status: ptr("archived")}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("archive without reason: %v", err)
	}
	up := must[Task](t)(s.UpdateTask(ctx, "t-1", TaskPatch{Status: ptr("waiting"), WaitingOn: ptr("owner review"), TagsAdd: []string{"area:work"}, TagsRemove: []string{"topic:mcp"}}))
	if up.Status != "waiting" || up.WaitingOn != "owner review" || strings.Join(up.Tags, ",") != "area:work,repo:github.com/acme/infra" {
		t.Fatalf("updated %+v", up)
	}
	ar := must[Task](t)(s.UpdateTask(ctx, "1", TaskPatch{Status: ptr("archived"), ClosedReason: ptr("done")}))
	if ar.ArchivedAt == "" || ar.ClosedReason != "done" || ar.WaitingOn != "" {
		t.Fatalf("archived %+v", ar)
	}
	back := must[Task](t)(s.UpdateTask(ctx, "T-1", TaskPatch{Status: ptr("active")}))
	if back.ArchivedAt != "" || back.ClosedReason != "" {
		t.Fatalf("unarchived %+v", back)
	}
	if _, err := s.GetTask(ctx, "T-99"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing task: %v", err)
	}
	// ids are never reused
	must[Task](t)(s.CreateTask(ctx, NewTask{Title: "second"}))
	if err := s.DeleteTask(ctx, "T-2"); err != nil {
		t.Fatal(err)
	}
	third := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "third"}))
	if third.ID != "T-3" {
		t.Fatalf("id reused: %s", third.ID)
	}
}

func TestSearchRussianMorphology(t *testing.T) {
	s := newStore(t)
	t1 := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "Новая задача для бэкапов", Tags: []string{"topic:backup"}}))
	t2 := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "Deploy the tasks server", Tags: []string{"topic:deploy"}}))
	must[WriteResult](t)(s.WriteDoc(ctx, WriteDocParams{Task: t2.ID, Slug: "plan", Body: "# План\n\nНастроить резервные копии базы данных.\n", Author: AuthorAgent}))
	must[JournalEntry](t)(s.AppendJournal(ctx, t1.ID, "decision", "Владелец выбрал Syncthing для синхронизации.", "laptop"))

	res := must[SearchResult](t)(s.Search(ctx, SearchParams{Query: "задачи"}))
	if res.Total != 1 || res.Tasks[0].ID != t1.ID {
		t.Fatalf("задачи: %+v", res)
	}
	if len(res.Tasks[0].Hits) == 0 || !strings.Contains(res.Tasks[0].Hits[0].Marked(), "**задача**") {
		t.Fatalf("snippet: %+v", res.Tasks[0].Hits)
	}
	res = must[SearchResult](t)(s.Search(ctx, SearchParams{Query: "резервная копия"}))
	if res.Total != 1 || res.Tasks[0].ID != t2.ID || res.Tasks[0].Hits[0].Source != "doc:plan" {
		t.Fatalf("резервная копия: %+v", res)
	}
	res = must[SearchResult](t)(s.Search(ctx, SearchParams{Query: "deploying"}))
	if res.Total != 1 || res.Tasks[0].ID != t2.ID {
		t.Fatalf("deploying: %+v", res)
	}
	res = must[SearchResult](t)(s.Search(ctx, SearchParams{Query: "синхронизацию"}))
	if res.Total != 1 || res.Tasks[0].Hits[0].Source[:8] != "journal:" {
		t.Fatalf("journal hit: %+v", res)
	}
	// filters
	res = must[SearchResult](t)(s.Search(ctx, SearchParams{Tags: []string{"topic:deploy"}}))
	if res.Total != 1 || res.Tasks[0].ID != t2.ID {
		t.Fatalf("tag filter: %+v", res)
	}
	must[Task](t)(s.UpdateTask(ctx, t1.ID, TaskPatch{Status: ptr("archived"), ClosedReason: ptr("cancelled")}))
	res = must[SearchResult](t)(s.Search(ctx, SearchParams{Query: "задача"}))
	if res.Total != 0 {
		t.Fatalf("archived must be hidden by default: %+v", res)
	}
	res = must[SearchResult](t)(s.Search(ctx, SearchParams{Query: "задача", Statuses: []string{"archived"}}))
	if res.Total != 1 {
		t.Fatalf("archived filter: %+v", res)
	}
	// re-indexing on doc update: old text no longer found
	must[WriteResult](t)(s.WriteDoc(ctx, WriteDocParams{Task: t2.ID, Slug: "plan", Body: "# План\n\nТолько DNS.\n", Author: AuthorAgent}))
	res = must[SearchResult](t)(s.Search(ctx, SearchParams{Query: "резервные"}))
	if res.Total != 0 {
		t.Fatalf("stale index: %+v", res)
	}
}

func TestPlanItemsAcrossRevisions(t *testing.T) {
	s := newStore(t)
	task := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "t"}))
	r1 := must[WriteResult](t)(s.WriteDoc(ctx, WriteDocParams{Task: task.ID, Slug: "plan", Author: AuthorAgent,
		Body: "# Plan\n\n- [ ] Write the role\n- [ ] Set up DNS record for tasks\n- [x] Read the docs\n"}))
	if !r1.Created || r1.Revision != 1 || len(r1.Items) != 3 || r1.Document.Kind != "plan" || r1.Document.Title != "Plan" {
		t.Fatalf("r1 %+v", r1)
	}
	if r1.Items[2].ID != "i3" || !r1.Items[2].Done || r1.Items[0].Done {
		t.Fatalf("initial state %+v", r1.Items)
	}
	must[PlanItem](t)(s.SetPlanItem(ctx, task.ID, "plan", "i1", ptr(true), "role in roles/stack_agent_tasks", AuthorAgent))
	must[PlanItem](t)(s.SetPlanItem(ctx, task.ID, "plan", "i2", nil, "waiting for zone access", AuthorAgent))

	r2 := must[WriteResult](t)(s.WriteDoc(ctx, WriteDocParams{Task: task.ID, Slug: "plan", Author: AuthorAgent, Message: "reorder",
		Body: "# Plan\n\n- [ ] Add backups\n- [ ] Write the role\n- [ ] Set up the DNS record for tasks host\n"}))
	if r2.Revision != 2 || len(r2.Items) != 3 {
		t.Fatalf("r2 %+v", r2)
	}
	byText := map[string]PlanItem{}
	for _, it := range r2.Items {
		byText[it.Text] = it
	}
	if it := byText["Write the role"]; it.ID != "i1" || !it.Done || len(it.Notes) != 1 {
		t.Errorf("exact match lost state: %+v", it)
	}
	if it := byText["Set up the DNS record for tasks host"]; it.ID != "i2" || len(it.Notes) != 1 {
		t.Errorf("similar match failed: %+v", it)
	}
	if it := byText["Add backups"]; it.ID != "i4" {
		t.Errorf("new item id: %+v", it)
	}
	// unchanged body writes nothing
	r3 := must[WriteResult](t)(s.WriteDoc(ctx, WriteDocParams{Task: task.ID, Slug: "plan", Author: AuthorAgent,
		Body: "# Plan\n\n- [ ] Add backups\n- [ ] Write the role\n- [ ] Set up the DNS record for tasks host\n"}))
	if !r3.Unchanged || r3.Revision != 2 {
		t.Fatalf("r3 %+v", r3)
	}
	// old revision still shows its own items
	v1 := must[DocView](t)(s.GetDoc(ctx, task.ID, "plan", DocGetOptions{Revision: 1}))
	if len(v1.Items) != 3 || v1.Items[2].Text != "Read the docs" {
		t.Fatalf("history %+v", v1.Items)
	}
	ov := must[Overview](t)(s.TaskOverview(ctx, task.ID, 5, false, ""))
	if ov.Progress.Total != 3 || ov.Progress.Done != 1 || ov.Documents[0].LatestRevision != 2 {
		t.Fatalf("overview %+v", ov)
	}
	if _, err := s.SetPlanItem(ctx, task.ID, "plan", "i99", ptr(true), "", AuthorAgent); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing item: %v", err)
	}
	d := must[DiffResult](t)(s.Diff(ctx, task.ID, "plan", 1, 0))
	if !strings.Contains(d.Unified, "+- [ ] Add backups") || d.To != 2 {
		t.Fatalf("diff %s", d.Unified)
	}
}

func TestReviewRoundFlow(t *testing.T) {
	s := newStore(t)
	task := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "t"}))
	body1 := "# Plan\n\nUse Traefik for routing.\n\nBackups go to R2 every night at three.\n\nDNS in Cloudflare.\n"
	must[WriteResult](t)(s.WriteDoc(ctx, WriteDocParams{Task: task.ID, Slug: "plan", Body: body1, Author: AuthorAgent}))

	draft := must[Round](t)(s.DraftRound(ctx, task.ID, "plan", 0))
	if draft.State != "draft" || draft.N != 1 || draft.Revision != 1 {
		t.Fatalf("draft %+v", draft)
	}
	again := must[Round](t)(s.DraftRound(ctx, task.ID, "plan", 0))
	if again.ID != draft.ID {
		t.Fatal("second draft created")
	}
	c1 := must[Comment](t)(s.AddComment(ctx, draft.ID, NewComment{Anchor: &anchor.Anchor{Quote: "Traefik"}, Body: "why not Caddy?"}))
	c2 := must[Comment](t)(s.AddComment(ctx, draft.ID, NewComment{Anchor: &anchor.Anchor{Quote: "Backups go to R2 every night"}, Body: "weekly is enough"}))
	c3 := must[Comment](t)(s.AddComment(ctx, draft.ID, NewComment{Body: "general: looks fine"}))
	if c1.Anchor == nil || c1.Line != 3 || c3.Anchor != nil {
		t.Fatalf("comments %+v %+v", c1, c3)
	}
	if _, err := s.AddComment(ctx, draft.ID, NewComment{Anchor: &anchor.Anchor{Quote: "nope"}, Body: "x"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad quote: %v", err)
	}
	// drafts are invisible to agents
	if p := must[[]Round](t)(s.PendingRounds(ctx, "")); len(p) != 0 {
		t.Fatalf("draft visible: %+v", p)
	}
	agentView := must[DocView](t)(s.GetDoc(ctx, task.ID, "plan", DocGetOptions{OnlyOpen: true}))
	if len(agentView.Comments) != 0 {
		t.Fatal("draft comments visible to agent")
	}
	if _, err := s.ReplyComment(ctx, ReplyParams{Comment: c1.ID, Author: AuthorAgent, Body: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("agent reply to draft: %v", err)
	}

	sub := must[Round](t)(s.SubmitRound(ctx, draft.ID))
	if sub.State != "submitted" || len(sub.Comments) != 3 {
		t.Fatalf("submitted %+v", sub)
	}
	if _, err := s.AddComment(ctx, sub.ID, NewComment{Body: "late"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("comment on submitted: %v", err)
	}
	pending := must[[]Round](t)(s.PendingRounds(ctx, task.ID))
	if len(pending) != 1 || pending[0].ID != sub.ID {
		t.Fatalf("pending %+v", pending)
	}

	// agent writes revision 2: Traefik paragraph moves, backup sentence is
	// rewritten slightly, so both comments follow; then revision 3 drops the
	// backup sentence entirely.
	body2 := "# Plan\n\nDNS in Cloudflare.\n\nUse Traefik for routing.\n\nBackups go to R2 every week at three.\n"
	w2 := must[WriteResult](t)(s.WriteDoc(ctx, WriteDocParams{Task: task.ID, Slug: "plan", Body: body2, Author: AuthorAgent, Message: "addresses round 1"}))
	if w2.Reanchored != 2 || len(w2.Outdated) != 0 {
		t.Fatalf("w2 %+v", w2)
	}
	got := must[Comment](t)(s.GetComment(ctx, c1.ID, false))
	if got.Current == nil || got.Current.Revision != 2 || got.Current.Line != 5 || got.Anchor.Start != c1.Anchor.Start {
		t.Fatalf("c1 after r2: %+v %+v", got, got.Current)
	}
	got = must[Comment](t)(s.GetComment(ctx, c2.ID, false))
	if got.Current == nil || !strings.Contains(got.Current.Quote, "every week") {
		t.Fatalf("c2 fuzzy: %+v", got.Current)
	}
	body3 := "# Plan\n\nDNS in Cloudflare.\n\nUse Traefik for routing.\n"
	w3 := must[WriteResult](t)(s.WriteDoc(ctx, WriteDocParams{Task: task.ID, Slug: "plan", Body: body3, Author: AuthorAgent}))
	if len(w3.Outdated) != 1 || w3.Outdated[0].ID != c2.ID || w3.Outdated[0].OutdatedIn != 3 || w3.Outdated[0].State != "outdated" {
		t.Fatalf("w3 %+v", w3)
	}

	rep := must[Comment](t)(s.ReplyComment(ctx, ReplyParams{Comment: c1.ID, Author: AuthorAgent, Body: "Traefik is already on the host", Resolve: true}))
	if rep.State != "resolved" || len(rep.Replies) != 1 {
		t.Fatalf("reply %+v", rep)
	}
	reo := must[Comment](t)(s.ReplyComment(ctx, ReplyParams{Comment: c1.ID, Author: AuthorOwner, Reopen: true}))
	if reo.State != "open" {
		t.Fatalf("reopen %+v", reo)
	}
	ad := must[Round](t)(s.RoundAddressed(ctx, sub.ID, "rev 2 and 3"))
	if ad.State != "addressed" || ad.AddressedAt == "" {
		t.Fatalf("addressed %+v", ad)
	}
	if p := must[[]Round](t)(s.PendingRounds(ctx, "")); len(p) != 0 {
		t.Fatalf("still pending %+v", p)
	}
	// comments are searchable once submitted
	res := must[SearchResult](t)(s.Search(ctx, SearchParams{Query: "Caddy"}))
	if res.Total != 1 {
		t.Fatalf("comment search %+v", res)
	}
	// second round numbering
	d2 := must[Round](t)(s.DraftRound(ctx, task.ID, "plan", 0))
	if d2.N != 2 || d2.Revision != 3 {
		t.Fatalf("round 2 %+v", d2)
	}
	if err := s.DeleteDraftRound(ctx, d2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SubmitRound(ctx, sub.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("resubmit: %v", err)
	}
}

func TestDraftOnOldRevisionIsCarriedOnSubmit(t *testing.T) {
	s := newStore(t)
	task := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "t"}))
	must[WriteResult](t)(s.WriteDoc(ctx, WriteDocParams{Task: task.ID, Slug: "notes", Body: "alpha beta gamma delta\n", Author: AuthorAgent}))
	dr := must[Round](t)(s.DraftRound(ctx, task.ID, "notes", 1))
	c := must[Comment](t)(s.AddComment(ctx, dr.ID, NewComment{Anchor: &anchor.Anchor{Quote: "gamma"}, Body: "?"}))
	must[WriteResult](t)(s.WriteDoc(ctx, WriteDocParams{Task: task.ID, Slug: "notes", Body: "intro\nalpha beta gamma delta\n", Author: AuthorAgent}))
	must[Round](t)(s.SubmitRound(ctx, dr.ID))
	got := must[Comment](t)(s.GetComment(ctx, c.ID, false))
	if got.Revision != 1 || got.Current == nil || got.Current.Revision != 2 || got.Current.Line != 2 {
		t.Fatalf("carried %+v %+v", got, got.Current)
	}
}

func TestRetention(t *testing.T) {
	s := newStore(t)
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	old := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "old"}))
	must[WriteResult](t)(s.WriteDoc(ctx, WriteDocParams{Task: old.ID, Slug: "plan", Body: "- [ ] x\n", Author: AuthorAgent}))
	must[Task](t)(s.UpdateTask(ctx, old.ID, TaskPatch{Status: ptr("archived"), ClosedReason: ptr("done")}))
	fresh := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "fresh"}))
	now = now.AddDate(0, 0, 40)
	must[Task](t)(s.UpdateTask(ctx, fresh.ID, TaskPatch{Status: ptr("archived"), ClosedReason: ptr("done")}))
	must[Task](t)(s.CreateTask(ctx, NewTask{Title: "active"}))

	ids := must[[]string](t)(s.PurgeArchived(ctx, 30))
	if len(ids) != 1 || ids[0] != old.ID {
		t.Fatalf("purged %v", ids)
	}
	if _, err := s.GetTask(ctx, old.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("old task still there")
	}
	if _, err := s.GetTask(ctx, fresh.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM revisions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("revisions left: %d %v", n, err)
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM search WHERE task_id = 1`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("search rows left: %d %v", n, err)
	}
}

func TestJournal(t *testing.T) {
	s := newStore(t)
	task := must[Task](t)(s.CreateTask(ctx, NewTask{Title: "t"}))
	for i, k := range []string{"log", "context", "decision", "log"} {
		s.Now = func() time.Time { return time.Date(2026, 11, 15, 10, i, 0, 0, time.UTC) }
		must[JournalEntry](t)(s.AppendJournal(ctx, task.ID, k, "entry "+k, "laptop"))
	}
	all := must[[]JournalEntry](t)(s.Journal(ctx, task.ID, "", 0))
	if len(all) != 4 || all[0].Kind != "log" || all[1].Kind != "decision" {
		t.Fatalf("journal %+v", all)
	}
	ctxs := must[[]JournalEntry](t)(s.Journal(ctx, task.ID, "context", 1))
	if len(ctxs) != 1 || ctxs[0].Kind != "context" {
		t.Fatalf("context %+v", ctxs)
	}
	if _, err := s.AppendJournal(ctx, task.ID, "bogus", "x", ""); !errors.Is(err, ErrInvalid) {
		t.Fatal("bad kind accepted")
	}
}
