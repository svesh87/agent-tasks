package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
)

// Task statuses and close reasons.
var (
	Statuses     = []string{"active", "waiting", "later", "archived"}
	CloseReasons = []string{"done", "cancelled"}
)

// Task is the task record.
type Task struct {
	ID           string      `json:"id"`
	Title        string      `json:"title"`
	Summary      string      `json:"summary"`
	Status       string      `json:"status"`
	WaitingOn    string      `json:"waiting_on,omitempty"`
	ClosedReason string      `json:"closed_reason,omitempty"`
	ArchivedAt   string      `json:"archived_at,omitempty"`
	Tags         []string    `json:"tags"`
	Places       []TaskPlace `json:"places"`
	CreatedAt    string      `json:"created_at"`
	UpdatedAt    string      `json:"updated_at"`

	num int64
}

// Num is the numeric part of the id.
func (t Task) Num() int64 { return t.num }

// NewTask holds the fields of task_create.
type NewTask struct {
	Title     string
	Summary   string
	Tags      []string
	Status    string
	WaitingOn string
	Places    []PlaceRef
	FromTodos []string // open todos that go into the task (state in_task)
	Source    string   // journal source for the "formed from todos" entry
}

func normTags(tags []string) []string {
	var out []string
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t != "" && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}

// CreateTask creates a task; status defaults to active.
func (s *Store) CreateTask(ctx context.Context, nt NewTask) (Task, error) {
	title := strings.TrimSpace(nt.Title)
	if title == "" {
		return Task{}, invalid("title is required")
	}
	status := nt.Status
	if status == "" {
		status = "active"
	}
	if status == "archived" {
		return Task{}, invalid("a new task cannot be archived; create it, then archive it with task_update and a closed_reason")
	}
	if !slices.Contains(Statuses, status) {
		return Task{}, invalid("bad status %q: use active, waiting or later", status)
	}
	var waitingOn string
	if status == "waiting" {
		waitingOn = strings.TrimSpace(nt.WaitingOn)
	}
	var t Task
	err := s.tx(ctx, func(tx *sql.Tx) error {
		now := s.ts()
		res, err := tx.ExecContext(ctx, `INSERT INTO tasks (title, summary, status, waiting_on, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
			title, nt.Summary, status, waitingOn, now, now)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		for _, tag := range normTags(nt.Tags) {
			if _, err := tx.ExecContext(ctx, `INSERT INTO task_tags (task_id, tag) VALUES (?, ?)`, id, tag); err != nil {
				return err
			}
		}
		if err := s.addTaskPlaces(ctx, tx, id, nt.Places); err != nil {
			return err
		}
		if err := s.absorb(ctx, tx, id, nt, now); err != nil {
			return err
		}
		if err := indexTask(ctx, tx, id); err != nil {
			return err
		}
		t, err = getTask(ctx, tx, id)
		return err
	})
	return t, err
}

// absorb moves open todos into a new task.
func (s *Store) absorb(ctx context.Context, tx *sql.Tx, id int64, nt NewTask, now string) error {
	var used []string
	for _, ref := range nt.FromTodos {
		n, err := ParseTodoID(ref)
		if err != nil {
			return err
		}
		td, err := getTodo(ctx, tx, n)
		if err != nil {
			return err
		}
		if td.State != "open" {
			return invalid("todo %s is %s; only open todos can go into a task", td.ID, td.State)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE todos SET state = 'in_task', task_id = ?, closed_at = ?, updated_at = ? WHERE id = ?`, id, now, now, n); err != nil {
			return err
		}
		// the todo's places join the task as touched (existing links stay)
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO task_places (task_id, place_id, role) SELECT ?, place_id, 'touched' FROM todo_places WHERE todo_id = ?`, id, n); err != nil {
			return err
		}
		used = append(used, td.ID+" "+td.Text)
	}
	if len(used) > 0 {
		if err := s.journalTx(ctx, tx, id, "log", "Formed from todos:\n- "+strings.Join(used, "\n- "), nt.Source, now); err != nil {
			return err
		}
	}
	return nil
}

func getTask(ctx context.Context, q queryer, id int64) (Task, error) {
	var t Task
	var archived sql.NullString
	err := q.QueryRowContext(ctx, `SELECT id, title, summary, status, waiting_on, closed_reason, archived_at, created_at, updated_at FROM tasks WHERE id = ?`, id).
		Scan(&t.num, &t.Title, &t.Summary, &t.Status, &t.WaitingOn, &t.ClosedReason, &archived, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, notFound("task %s not found", TaskID(id))
	}
	if err != nil {
		return Task{}, err
	}
	t.ID = TaskID(t.num)
	t.ArchivedAt = nullStr(archived)
	if t.Tags, err = taskTags(ctx, q, id); err != nil {
		return Task{}, err
	}
	if t.Places, err = taskPlaces(ctx, q, id); err != nil {
		return Task{}, err
	}
	// every task response carries each place's locations on all machines
	err = withLocations(ctx, q, t.Places, "")
	return t, err
}

func taskTags(ctx context.Context, q queryer, id int64) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT tag FROM task_tags WHERE task_id = ? ORDER BY tag`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := []string{}
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// GetTask returns one task.
func (s *Store) GetTask(ctx context.Context, id string) (Task, error) {
	n, err := ParseTaskID(id)
	if err != nil {
		return Task{}, err
	}
	var t Task
	err = s.tx(ctx, func(tx *sql.Tx) error {
		t, err = getTask(ctx, tx, n)
		return err
	})
	return t, err
}

// TaskPatch holds the fields of task_update; nil means unchanged.
type TaskPatch struct {
	Title        *string
	Summary      *string
	Status       *string
	WaitingOn    *string
	ClosedReason *string
	TagsAdd      []string
	TagsRemove   []string
	PlacesAdd    []PlaceRef
	PlacesRemove []string
}

// UpdateTask changes a task. Archiving requires a closed reason; leaving the
// archive clears it. waiting_on is kept only while the task is waiting.
func (s *Store) UpdateTask(ctx context.Context, id string, p TaskPatch) (Task, error) {
	n, err := ParseTaskID(id)
	if err != nil {
		return Task{}, err
	}
	var t Task
	err = s.tx(ctx, func(tx *sql.Tx) error {
		t, err = s.updateTaskTx(ctx, tx, n, p)
		return err
	})
	return t, err
}

// UpdateTasks applies the same patch to several tasks in one transaction:
// all succeed or none (the error names the failing task).
func (s *Store) UpdateTasks(ctx context.Context, ids []string, p TaskPatch) ([]Task, error) {
	if len(ids) == 0 {
		return nil, invalid("no task ids")
	}
	nums := make([]int64, len(ids))
	for i, id := range ids {
		n, err := ParseTaskID(id)
		if err != nil {
			return nil, err
		}
		nums[i] = n
	}
	out := []Task{}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		for _, n := range nums {
			t, err := s.updateTaskTx(ctx, tx, n, p)
			if err != nil {
				return &kindError{kindOf(err), TaskID(n) + ": " + err.Error()}
			}
			out = append(out, t)
		}
		return nil
	})
	return out, err
}

// kindOf returns the error kind (ErrNotFound, ErrInvalid, ErrConflict) or
// the error itself.
func kindOf(err error) error {
	for _, k := range []error{ErrNotFound, ErrInvalid, ErrConflict} {
		if errors.Is(err, k) {
			return k
		}
	}
	return err
}

func (s *Store) updateTaskTx(ctx context.Context, tx *sql.Tx, n int64, p TaskPatch) (Task, error) {
	var t Task
	err := func() error {
		cur, err := getTask(ctx, tx, n)
		if err != nil {
			return err
		}
		now := s.ts()
		if p.Title != nil {
			if strings.TrimSpace(*p.Title) == "" {
				return invalid("title cannot be empty")
			}
			cur.Title = strings.TrimSpace(*p.Title)
		}
		if p.Summary != nil {
			cur.Summary = *p.Summary
		}
		if p.WaitingOn != nil {
			cur.WaitingOn = strings.TrimSpace(*p.WaitingOn)
		}
		if p.ClosedReason != nil {
			if *p.ClosedReason != "" && !slices.Contains(CloseReasons, *p.ClosedReason) {
				return invalid("bad closed_reason %q: use done or cancelled", *p.ClosedReason)
			}
			cur.ClosedReason = *p.ClosedReason
		}
		if p.Status != nil {
			st := *p.Status
			if !slices.Contains(Statuses, st) {
				return invalid("bad status %q: use active, waiting, later or archived", st)
			}
			if st == "archived" && cur.Status != "archived" {
				cur.ArchivedAt = now
			}
			cur.Status = st
		}
		if cur.Status == "archived" {
			if cur.ClosedReason == "" {
				return invalid("archiving needs closed_reason: done or cancelled")
			}
		} else {
			if p.ClosedReason != nil && *p.ClosedReason != "" {
				return invalid("closed_reason is only allowed when the status is archived")
			}
			cur.ClosedReason = ""
			cur.ArchivedAt = ""
		}
		if cur.Status != "waiting" {
			cur.WaitingOn = ""
		}
		var archived any
		if cur.ArchivedAt != "" {
			archived = cur.ArchivedAt
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET title = ?, summary = ?, status = ?, waiting_on = ?, closed_reason = ?, archived_at = ?, updated_at = ? WHERE id = ?`,
			cur.Title, cur.Summary, cur.Status, cur.WaitingOn, cur.ClosedReason, archived, now, n); err != nil {
			return err
		}
		if err := s.addTaskPlaces(ctx, tx, n, p.PlacesAdd); err != nil {
			return err
		}
		if err := removeTaskPlaces(ctx, tx, n, p.PlacesRemove); err != nil {
			return err
		}
		for _, tag := range normTags(p.TagsAdd) {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO task_tags (task_id, tag) VALUES (?, ?)`, n, tag); err != nil {
				return err
			}
		}
		for _, tag := range normTags(p.TagsRemove) {
			if _, err := tx.ExecContext(ctx, `DELETE FROM task_tags WHERE task_id = ? AND tag = ?`, n, tag); err != nil {
				return err
			}
		}
		if err := indexTask(ctx, tx, n); err != nil {
			return err
		}
		t, err = getTask(ctx, tx, n)
		return err
	}()
	return t, err
}

// DocSummary describes a document inside a task overview.
type DocSummary struct {
	Slug           string `json:"slug"`
	Kind           string `json:"kind"`
	Title          string `json:"title"`
	LatestRevision int    `json:"latest_revision"`
	UpdatedAt      string `json:"updated_at"`
	OpenComments   int    `json:"open_comments"`
	ItemsDone      int    `json:"items_done"`
	ItemsTotal     int    `json:"items_total"`
	// Approved is the owner's approval of the latest revision, or null.
	Approved *Approval `json:"approved"`
}

// Progress counts plan items in the latest revisions.
type Progress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// Overview is a task with everything task_get shows.
type Overview struct {
	Task
	Documents     []DocSummary   `json:"documents"`
	Progress      Progress       `json:"plan_progress"`
	Journal       []JournalEntry `json:"journal"`
	PendingRounds []RoundBrief   `json:"pending_rounds"`
	DraftRounds   []RoundBrief   `json:"draft_rounds,omitempty"`
	Todos         []Todo         `json:"todos"` // todos that went into this task
}

// RoundBrief is a short description of a review round.
type RoundBrief struct {
	ID        int64  `json:"round"`
	Slug      string `json:"slug"`
	N         int    `json:"n"`
	Revision  int    `json:"revision"`
	State     string `json:"state"`
	Comments  int    `json:"comments"`
	Submitted string `json:"submitted_at,omitempty"`
}

// TaskOverview returns the task with documents, plan progress, the last
// journalLimit journal entries and pending (submitted) rounds. withDrafts
// adds draft rounds (owner-only view). Places carry their locations on all
// machines; with machine given, each place's Here is its path there.
func (s *Store) TaskOverview(ctx context.Context, id string, journalLimit int, withDrafts bool, machine string) (Overview, error) {
	n, err := ParseTaskID(id)
	if err != nil {
		return Overview{}, err
	}
	var o Overview
	err = s.tx(ctx, func(tx *sql.Tx) error {
		t, err := getTask(ctx, tx, n)
		if err != nil {
			return err
		}
		if machine = strings.TrimSpace(machine); machine != "" {
			for i := range t.Places {
				for _, l := range t.Places[i].Locations {
					if l.Machine == machine {
						t.Places[i].Here = l.Path
						break
					}
				}
			}
		}
		o.Task = t
		if o.Todos, err = taskTodos(ctx, tx, n); err != nil {
			return err
		}
		o.Documents = []DocSummary{}
		rows, err := tx.QueryContext(ctx, `
SELECT d.slug, d.kind, d.title, d.updated_at,
  coalesce((SELECT max(n) FROM revisions r WHERE r.document_id = d.id), 0),
  (SELECT count(*) FROM comments c JOIN review_rounds rr ON rr.id = c.round_id
     WHERE rr.document_id = d.id AND rr.state != 'draft' AND c.state = 'open'),
  (SELECT count(*) FROM plan_item_revisions pr JOIN plan_items pi ON pi.id = pr.item_id
     WHERE pr.revision_id = (SELECT id FROM revisions r WHERE r.document_id = d.id ORDER BY n DESC LIMIT 1) AND pi.done = 1),
  (SELECT count(*) FROM plan_item_revisions pr
     WHERE pr.revision_id = (SELECT id FROM revisions r WHERE r.document_id = d.id ORDER BY n DESC LIMIT 1)),
  (SELECT max(a.created_at) FROM approvals a
     WHERE a.revision_id = (SELECT id FROM revisions r WHERE r.document_id = d.id ORDER BY n DESC LIMIT 1) AND a.revoked_at IS NULL)
FROM documents d WHERE d.task_id = ? ORDER BY d.created_at, d.id`, n)
		if err != nil {
			return err
		}
		for rows.Next() {
			var d DocSummary
			var approvedAt sql.NullString
			if err := rows.Scan(&d.Slug, &d.Kind, &d.Title, &d.UpdatedAt, &d.LatestRevision, &d.OpenComments, &d.ItemsDone, &d.ItemsTotal, &approvedAt); err != nil {
				rows.Close()
				return err
			}
			if approvedAt.Valid {
				d.Approved = &Approval{Revision: d.LatestRevision, At: approvedAt.String, By: AuthorOwner}
			}
			o.Progress.Done += d.ItemsDone
			o.Progress.Total += d.ItemsTotal
			o.Documents = append(o.Documents, d)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if o.Journal, err = journal(ctx, tx, n, "", journalLimit); err != nil {
			return err
		}
		if o.PendingRounds, err = roundBriefs(ctx, tx, n, "submitted"); err != nil {
			return err
		}
		if withDrafts {
			if o.DraftRounds, err = roundBriefs(ctx, tx, n, "draft"); err != nil {
				return err
			}
		}
		return nil
	})
	return o, err
}

func roundBriefs(ctx context.Context, q queryer, taskID int64, state string) ([]RoundBrief, error) {
	rows, err := q.QueryContext(ctx, `
SELECT rr.id, d.slug, rr.n, r.n, rr.state, coalesce(rr.submitted_at, ''),
  (SELECT count(*) FROM comments c WHERE c.round_id = rr.id)
FROM review_rounds rr JOIN documents d ON d.id = rr.document_id JOIN revisions r ON r.id = rr.revision_id
WHERE d.task_id = ? AND rr.state = ? ORDER BY rr.id`, taskID, state)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RoundBrief{}
	for rows.Next() {
		var b RoundBrief
		if err := rows.Scan(&b.ID, &b.Slug, &b.N, &b.Revision, &b.State, &b.Submitted, &b.Comments); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// TagCount is a tag with the number of tasks carrying it.
type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// TagUse is a tag with how many tasks and todos carry it.
type TagUse struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"` // tasks + todos
	Tasks int    `json:"tasks"`
	Todos int    `json:"todos"`
}

// Tags lists distinct tags of tasks and todos. q (case-insensitive) keeps
// tags containing it, those starting with it first; within each group by
// count desc, then tag. limit <= 0 means 20, max 200.
func (s *Store) Tags(ctx context.Context, q string, limit int) ([]TagUse, error) {
	if limit <= 0 {
		limit = 20
	}
	limit = min(limit, 200)
	q = strings.ToLower(strings.TrimSpace(q))
	var all []TagUse
	err := s.tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT tag, sum(tasks), sum(todos) FROM (
  SELECT tag, count(*) AS tasks, 0 AS todos FROM task_tags GROUP BY tag
  UNION ALL
  SELECT tag, 0, count(*) FROM todo_tags GROUP BY tag
) GROUP BY tag`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var tu TagUse
			if err := rows.Scan(&tu.Tag, &tu.Tasks, &tu.Todos); err != nil {
				return err
			}
			tu.Count = tu.Tasks + tu.Todos
			all = append(all, tu)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	// filtering in Go: SQLite lower() is ASCII-only, tags may be Cyrillic
	rank := func(t TagUse) int {
		l := strings.ToLower(t.Tag)
		switch {
		case q == "" || strings.HasPrefix(l, q):
			return 0
		case strings.Contains(l, q):
			return 1
		}
		return 2
	}
	out := []TagUse{}
	for _, t := range all {
		if rank(t) < 2 {
			out = append(out, t)
		}
	}
	slices.SortFunc(out, func(a, b TagUse) int {
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra - rb
		}
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Tag, b.Tag)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// DeleteTask removes a task and everything it owns.
func (s *Store) DeleteTask(ctx context.Context, id string) error {
	n, err := ParseTaskID(id)
	if err != nil {
		return err
	}
	return s.tx(ctx, func(tx *sql.Tx) error { return deleteTask(ctx, tx, n) })
}

func deleteTask(ctx context.Context, tx *sql.Tx, n int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM search WHERE task_id = ?`, n); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, n)
	if err != nil {
		return err
	}
	if c, _ := res.RowsAffected(); c == 0 {
		return notFound("task %s not found", TaskID(n))
	}
	return nil
}

// PurgeArchived deletes tasks archived before the cutoff and returns their ids.
func (s *Store) PurgeArchived(ctx context.Context, olderThanDays int) ([]string, error) {
	if olderThanDays <= 0 {
		return nil, nil
	}
	cutoff := s.Now().UTC().AddDate(0, 0, -olderThanDays).Format(TimeFormat)
	var ids []string
	err := s.tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM tasks WHERE status = 'archived' AND archived_at IS NOT NULL AND archived_at < ?`, cutoff)
		if err != nil {
			return err
		}
		var nums []int64
		for rows.Next() {
			var n int64
			if err := rows.Scan(&n); err != nil {
				rows.Close()
				return err
			}
			nums = append(nums, n)
		}
		rows.Close()
		for _, n := range nums {
			if err := deleteTask(ctx, tx, n); err != nil {
				return err
			}
			ids = append(ids, TaskID(n))
		}
		return nil
	})
	return ids, err
}
