package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/svesh87/agent-tasks/internal/anchor"
	"github.com/svesh87/agent-tasks/internal/diff"
	"github.com/svesh87/agent-tasks/internal/plan"
)

// DocKinds are the allowed document kinds.
var DocKinds = []string{"plan", "research", "review", "notes", "other"}

// Authors of revisions.
const (
	AuthorAgent  = "agent"
	AuthorOwner  = "owner"
	AuthorImport = "import"
)

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// Document is a document of a task (without content).
type Document struct {
	Task           string `json:"task"`
	Slug           string `json:"slug"`
	Kind           string `json:"kind"`
	Title          string `json:"title"`
	LatestRevision int    `json:"latest_revision"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	// Approved is the owner's approval of the latest revision; null when the
	// latest revision is not approved.
	Approved *Approval `json:"approved"`

	id      int64
	taskNum int64
}

// Revision is an immutable version of a document.
type Revision struct {
	N         int    `json:"n"`
	Author    string `json:"author"`
	Message   string `json:"message,omitempty"`
	CreatedAt string `json:"created_at"`
	Body      string `json:"body,omitempty"`

	id int64
}

// Note is a short entry attached to a plan item.
type Note struct {
	By        string `json:"by"` // owner | agent
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

// PlanItem is a task-list line with its server-side state.
type PlanItem struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Done   bool   `json:"done"`
	DoneAt string `json:"done_at,omitempty"`
	DoneBy string `json:"done_by,omitempty"` // who ticked or unticked last: owner | agent
	Line   int    `json:"line"`
	Notes  []Note `json:"notes"`

	rowID int64
}

func defaultKind(slug string) string {
	switch {
	case slug == "plan" || strings.HasPrefix(slug, "plan-"):
		return "plan"
	case slug == "research" || strings.HasPrefix(slug, "research-"):
		return "research"
	case slug == "review" || strings.HasPrefix(slug, "review-"):
		return "review"
	case slug == "notes" || strings.HasPrefix(slug, "notes-"):
		return "notes"
	}
	return "other"
}

var headingRe = regexp.MustCompile(`(?m)^#\s+(.+?)\s*#*\s*$`)

func defaultTitle(slug, body string) string {
	if m := headingRe.FindStringSubmatch(body); m != nil {
		return m[1]
	}
	return slug
}

func getDoc(ctx context.Context, q queryer, taskNum int64, slug string) (Document, error) {
	d := Document{Task: TaskID(taskNum), Slug: slug, taskNum: taskNum}
	err := q.QueryRowContext(ctx, `SELECT d.id, d.kind, d.title, d.created_at, d.updated_at,
  coalesce((SELECT max(n) FROM revisions r WHERE r.document_id = d.id), 0)
FROM documents d WHERE d.task_id = ? AND d.slug = ?`, taskNum, slug).
		Scan(&d.id, &d.Kind, &d.Title, &d.CreatedAt, &d.UpdatedAt, &d.LatestRevision)
	if errors.Is(err, sql.ErrNoRows) {
		if _, terr := getTask(ctx, q, taskNum); terr != nil {
			return Document{}, terr
		}
		return Document{}, notFound("document %q not found in task %s", slug, TaskID(taskNum))
	}
	if err != nil {
		return Document{}, err
	}
	d.Approved, err = latestApproval(ctx, q, d)
	return d, err
}

func getRevision(ctx context.Context, q queryer, d Document, n int) (Revision, error) {
	if n <= 0 {
		n = d.LatestRevision
	}
	r := Revision{N: n}
	err := q.QueryRowContext(ctx, `SELECT id, author, message, created_at, body FROM revisions WHERE document_id = ? AND n = ?`, d.id, n).
		Scan(&r.id, &r.Author, &r.Message, &r.CreatedAt, &r.Body)
	if errors.Is(err, sql.ErrNoRows) {
		return Revision{}, notFound("revision %d of %s/%s not found; latest is %d", n, d.Task, d.Slug, d.LatestRevision)
	}
	return r, err
}

// WriteDocParams are the inputs of doc_write.
type WriteDocParams struct {
	Task    string
	Slug    string
	Body    string
	Kind    string // optional; default from slug for new documents
	Title   string // optional; default: first heading or slug
	Message string
	Author  string // agent | owner | import
	// BaseRevision, when > 0, is the revision the edit started from; if the
	// document has moved on since, the write fails with a conflict instead of
	// silently overwriting a newer revision.
	BaseRevision int
}

// WriteResult is the outcome of doc_write.
type WriteResult struct {
	Document   Document   `json:"document"`
	Revision   int        `json:"revision"`
	Created    bool       `json:"created"`   // the document is new
	Unchanged  bool       `json:"unchanged"` // body equals the latest revision; nothing written
	Items      []PlanItem `json:"items"`
	Reanchored int        `json:"reanchored"` // comments moved onto the new revision
	Outdated   []Comment  `json:"outdated"`   // comments whose text is gone (visible rounds only)
}

// WriteDoc creates the document or adds a revision to it. Plan items are
// matched to the previous revision; open comments are re-anchored.
func (s *Store) WriteDoc(ctx context.Context, p WriteDocParams) (WriteResult, error) {
	n, err := ParseTaskID(p.Task)
	if err != nil {
		return WriteResult{}, err
	}
	if !slugRe.MatchString(p.Slug) {
		return WriteResult{}, invalid("bad slug %q: use lowercase letters, digits, '.', '_' or '-', up to 64 chars, e.g. plan or review-2026-11-20", p.Slug)
	}
	if p.Kind != "" && !slices.Contains(DocKinds, p.Kind) {
		return WriteResult{}, invalid("bad kind %q: use plan, research, review, notes or other", p.Kind)
	}
	if !slices.Contains([]string{AuthorAgent, AuthorOwner, AuthorImport}, p.Author) {
		return WriteResult{}, invalid("bad author %q", p.Author)
	}
	if strings.TrimSpace(p.Body) == "" {
		return WriteResult{}, invalid("body is empty")
	}
	var res WriteResult
	err = s.tx(ctx, func(tx *sql.Tx) error {
		now := s.ts()
		if _, err := getTask(ctx, tx, n); err != nil {
			return err
		}
		d, err := getDoc(ctx, tx, n, p.Slug)
		switch {
		case errors.Is(err, ErrNotFound):
			kind := p.Kind
			if kind == "" {
				kind = defaultKind(p.Slug)
			}
			title := strings.TrimSpace(p.Title)
			if title == "" {
				title = defaultTitle(p.Slug, p.Body)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO documents (task_id, slug, kind, title, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
				n, p.Slug, kind, title, now, now); err != nil {
				return err
			}
			res.Created = true
			if d, err = getDoc(ctx, tx, n, p.Slug); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			if p.Kind != "" || strings.TrimSpace(p.Title) != "" {
				if p.Kind != "" {
					d.Kind = p.Kind
				}
				if t := strings.TrimSpace(p.Title); t != "" {
					d.Title = t
				}
				if _, err := tx.ExecContext(ctx, `UPDATE documents SET kind = ?, title = ? WHERE id = ?`, d.Kind, d.Title, d.id); err != nil {
					return err
				}
			}
		}

		if p.BaseRevision > 0 && p.BaseRevision != d.LatestRevision {
			return conflict("%s/%s is at revision %d, but the edit started from revision %d; reload and apply the edit again",
				d.Task, d.Slug, d.LatestRevision, p.BaseRevision)
		}
		var prev Revision
		if d.LatestRevision > 0 {
			if prev, err = getRevision(ctx, tx, d, 0); err != nil {
				return err
			}
			if prev.Body == p.Body {
				res.Unchanged = true
				res.Document = d
				res.Revision = prev.N
				res.Outdated = []Comment{}
				res.Items, err = revisionItems(ctx, tx, prev.id)
				return err
			}
		}

		newN := d.LatestRevision + 1
		r, err := tx.ExecContext(ctx, `INSERT INTO revisions (document_id, n, body, author, message, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			d.id, newN, p.Body, p.Author, strings.TrimSpace(p.Message), now)
		if err != nil {
			return err
		}
		revID, _ := r.LastInsertId()

		by := AuthorAgent // who wrote a new "- [x]" line
		if p.Author != AuthorAgent {
			by = AuthorOwner
		}
		if err := s.carryPlanItems(ctx, tx, d, prev, revID, p.Body, by, now); err != nil {
			return err
		}
		if res.Reanchored, res.Outdated, err = s.reanchor(ctx, tx, d, revID, p.Body, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE documents SET updated_at = ? WHERE id = ?`, now, d.id); err != nil {
			return err
		}
		if err := indexRow(ctx, tx, n, "doc:"+d.Slug, d.Title+"\n\n"+p.Body); err != nil {
			return err
		}
		if err := touchTask(ctx, tx, n, now); err != nil {
			return err
		}
		d.LatestRevision = newN
		d.UpdatedAt = now
		d.Approved = nil // a new revision is not approved yet
		res.Document = d
		res.Revision = newN
		res.Items, err = revisionItems(ctx, tx, revID)
		return err
	})
	return res, err
}

// carryPlanItems matches the items of the new body with those of prev and
// records them for the new revision. New items take the box state written in
// the body; matched items keep their stored state.
func (s *Store) carryPlanItems(ctx context.Context, tx *sql.Tx, d Document, prev Revision, revID int64, body, by, now string) error {
	type oldItem struct {
		id   int64
		text string
	}
	var olds []oldItem
	if prev.id != 0 {
		rows, err := tx.QueryContext(ctx, `SELECT item_id, text FROM plan_item_revisions WHERE revision_id = ? ORDER BY position`, prev.id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var o oldItem
			if err := rows.Scan(&o.id, &o.text); err != nil {
				rows.Close()
				return err
			}
			olds = append(olds, o)
		}
		rows.Close()
	}
	items := plan.Parse(body)
	oldTexts := make([]string, len(olds))
	for i, o := range olds {
		oldTexts[i] = o.text
	}
	newTexts := make([]string, len(items))
	for i, it := range items {
		newTexts[i] = it.Text
	}
	match := plan.Match(oldTexts, newTexts)
	for i, it := range items {
		var itemID int64
		if j := match[i]; j >= 0 {
			itemID = olds[j].id
		} else {
			var next int
			if err := tx.QueryRowContext(ctx, `UPDATE documents SET next_item = next_item + 1 WHERE id = ? RETURNING next_item - 1`, d.id).Scan(&next); err != nil {
				return err
			}
			var doneAt any
			doneBy := ""
			if it.Checked {
				doneAt, doneBy = now, by
			}
			r, err := tx.ExecContext(ctx, `INSERT INTO plan_items (document_id, key, done, done_at, done_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				d.id, fmt.Sprintf("i%d", next), boolInt(it.Checked), doneAt, doneBy, now, now)
			if err != nil {
				return err
			}
			itemID, _ = r.LastInsertId()
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO plan_item_revisions (item_id, revision_id, position, line, text) VALUES (?, ?, ?, ?, ?)`,
			itemID, revID, i, it.Line, it.Text); err != nil {
			return err
		}
	}
	return nil
}

func revisionItems(ctx context.Context, q queryer, revID int64) ([]PlanItem, error) {
	rows, err := q.QueryContext(ctx, `SELECT pi.id, pi.key, pr.text, pi.done, coalesce(pi.done_at, ''), pi.done_by, pr.line
FROM plan_item_revisions pr JOIN plan_items pi ON pi.id = pr.item_id WHERE pr.revision_id = ? ORDER BY pr.position`, revID)
	if err != nil {
		return nil, err
	}
	items := []PlanItem{}
	for rows.Next() {
		var it PlanItem
		if err := rows.Scan(&it.rowID, &it.ID, &it.Text, &it.Done, &it.DoneAt, &it.DoneBy, &it.Line); err != nil {
			rows.Close()
			return nil, err
		}
		it.Notes = []Note{}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].Notes, err = itemNotes(ctx, q, items[i].rowID); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func itemNotes(ctx context.Context, q queryer, itemID int64) ([]Note, error) {
	rows, err := q.QueryContext(ctx, `SELECT author, body, created_at FROM plan_item_notes WHERE item_id = ? ORDER BY id`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	notes := []Note{}
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.By, &n.Body, &n.CreatedAt); err != nil {
			return nil, err
		}
		notes = append(notes, n)
	}
	return notes, rows.Err()
}

// reanchor moves anchored comments (open or resolved) of the document onto
// the new revision. Open comments whose text is gone become outdated.
func (s *Store) reanchor(ctx context.Context, tx *sql.Tx, d Document, revID int64, body, now string) (int, []Comment, error) {
	type row struct {
		id, curRev           int64
		state, roundState    string
		quote, prefix, suffx string
		start, end           int
	}
	rows, err := tx.QueryContext(ctx, `SELECT c.id, c.cur_revision_id, c.state, rr.state, c.cur_quote, c.cur_prefix, c.cur_suffix, c.cur_start, c.cur_end
FROM comments c JOIN review_rounds rr ON rr.id = c.round_id
WHERE rr.document_id = ? AND c.quote != '' AND c.state IN ('open', 'resolved') AND c.cur_revision_id IS NOT NULL AND c.cur_revision_id != ?`, d.id, revID)
	if err != nil {
		return 0, nil, err
	}
	var list []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.curRev, &r.state, &r.roundState, &r.quote, &r.prefix, &r.suffx, &r.start, &r.end); err != nil {
			rows.Close()
			return 0, nil, err
		}
		list = append(list, r)
	}
	rows.Close()

	bodies := map[int64]string{}
	moved := 0
	var outdatedIDs []int64
	for _, r := range list {
		old, ok := bodies[r.curRev]
		if !ok {
			if err := tx.QueryRowContext(ctx, `SELECT body FROM revisions WHERE id = ?`, r.curRev).Scan(&old); err != nil {
				return 0, nil, err
			}
			bodies[r.curRev] = old
		}
		a, found := anchor.Relocate(old, body, anchor.Anchor{Quote: r.quote, Prefix: r.prefix, Suffix: r.suffx, Start: r.start, End: r.end})
		if found {
			moved++
			if _, err := tx.ExecContext(ctx, `UPDATE comments SET cur_revision_id = ?, cur_quote = ?, cur_prefix = ?, cur_suffix = ?, cur_start = ?, cur_end = ?, updated_at = ? WHERE id = ?`,
				revID, a.Quote, a.Prefix, a.Suffix, a.Start, a.End, now, r.id); err != nil {
				return 0, nil, err
			}
			continue
		}
		state := r.state
		if state == "open" {
			state = "outdated"
			if r.roundState != "draft" {
				outdatedIDs = append(outdatedIDs, r.id)
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE comments SET state = ?, outdated_revision_id = ?, updated_at = ? WHERE id = ?`, state, revID, now, r.id); err != nil {
			return 0, nil, err
		}
	}
	outdated := []Comment{}
	if len(outdatedIDs) > 0 {
		args := make([]any, len(outdatedIDs))
		for i, id := range outdatedIDs {
			args[i] = id
		}
		outdated, err = loadComments(ctx, tx, `c.id IN (`+strings.Repeat("?,", len(args)-1)+`?)`, args...)
		if err != nil {
			return 0, nil, err
		}
	}
	return moved, outdated, nil
}

// DocView is a document revision with its plan items and comments.
type DocView struct {
	Document  Document   `json:"document"`
	Revision  Revision   `json:"revision"`
	Items     []PlanItem `json:"items"`
	Comments  []Comment  `json:"comments"`
	Revisions []Revision `json:"revisions,omitempty"`
	Rounds    []Round    `json:"rounds,omitempty"`
	// ApprovedRevisions lists revisions the owner approved (history; only
	// the latest revision's approval counts, see Document.Approved).
	ApprovedRevisions []int `json:"approved_revisions"`
}

// DocGetOptions select what GetDoc returns.
type DocGetOptions struct {
	Revision      int  // 0: latest
	OwnerView     bool // include draft rounds, all comment states, revision list and rounds
	OnlyOpen      bool // agent view: only open comments of submitted/addressed rounds
	WithRevisions bool
}

// GetDoc returns a revision of a document with plan items (state as stored)
// and comments.
func (s *Store) GetDoc(ctx context.Context, task, slug string, o DocGetOptions) (DocView, error) {
	n, err := ParseTaskID(task)
	if err != nil {
		return DocView{}, err
	}
	var v DocView
	err = s.tx(ctx, func(tx *sql.Tx) error {
		d, err := getDoc(ctx, tx, n, slug)
		if err != nil {
			return err
		}
		r, err := getRevision(ctx, tx, d, o.Revision)
		if err != nil {
			return err
		}
		v.Document, v.Revision = d, r
		if v.Items, err = revisionItems(ctx, tx, r.id); err != nil {
			return err
		}
		if v.ApprovedRevisions, err = approvedRevisions(ctx, tx, d.id); err != nil {
			return err
		}
		where := `rr.document_id = ?`
		if !o.OwnerView {
			where += ` AND rr.state != 'draft'`
		}
		if o.OnlyOpen {
			where += ` AND c.state = 'open'`
		}
		if v.Comments, err = loadComments(ctx, tx, where, d.id); err != nil {
			return err
		}
		if o.WithRevisions || o.OwnerView {
			if v.Revisions, err = listRevisions(ctx, tx, d); err != nil {
				return err
			}
		}
		if o.OwnerView {
			if v.Rounds, err = docRounds(ctx, tx, d, true); err != nil {
				return err
			}
		}
		return nil
	})
	return v, err
}

func listRevisions(ctx context.Context, q queryer, d Document) ([]Revision, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, n, author, message, created_at FROM revisions WHERE document_id = ? ORDER BY n`, d.id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Revision{}
	for rows.Next() {
		var r Revision
		if err := rows.Scan(&r.id, &r.N, &r.Author, &r.Message, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RevertDoc writes a new owner revision with the body of revision to
// (plan items matched, comments re-anchored as usual). base > 0 guards
// against a document that moved on; an identical body writes nothing.
func (s *Store) RevertDoc(ctx context.Context, task, slug string, to, base int) (WriteResult, error) {
	if to <= 0 {
		return WriteResult{}, invalid("to must be a revision number")
	}
	v, err := s.GetDoc(ctx, task, slug, DocGetOptions{Revision: to})
	if err != nil {
		return WriteResult{}, err
	}
	return s.WriteDoc(ctx, WriteDocParams{Task: task, Slug: slug, Body: v.Revision.Body, Author: AuthorOwner,
		Message: fmt.Sprintf("revert to r%d", to), BaseRevision: base})
}

// Revisions lists the revisions of a document without bodies.
func (s *Store) Revisions(ctx context.Context, task, slug string) ([]Revision, error) {
	n, err := ParseTaskID(task)
	if err != nil {
		return nil, err
	}
	var out []Revision
	err = s.tx(ctx, func(tx *sql.Tx) error {
		d, err := getDoc(ctx, tx, n, slug)
		if err != nil {
			return err
		}
		out, err = listRevisions(ctx, tx, d)
		return err
	})
	return out, err
}

// DiffResult is a diff between two revisions.
type DiffResult struct {
	Document Document    `json:"document"`
	From     int         `json:"from"`
	To       int         `json:"to"`
	Unified  string      `json:"unified"`
	Hunks    []diff.Hunk `json:"hunks"`
}

// Diff compares two revisions; to <= 0 means the latest.
func (s *Store) Diff(ctx context.Context, task, slug string, from, to int) (DiffResult, error) {
	n, err := ParseTaskID(task)
	if err != nil {
		return DiffResult{}, err
	}
	var res DiffResult
	err = s.tx(ctx, func(tx *sql.Tx) error {
		d, err := getDoc(ctx, tx, n, slug)
		if err != nil {
			return err
		}
		if to <= 0 {
			to = d.LatestRevision
		}
		if from <= 0 {
			return invalid("from must be a revision number (1..%d)", d.LatestRevision)
		}
		a, err := getRevision(ctx, tx, d, from)
		if err != nil {
			return err
		}
		b, err := getRevision(ctx, tx, d, to)
		if err != nil {
			return err
		}
		res = DiffResult{
			Document: d, From: from, To: to,
			Unified: diff.Unified(fmt.Sprintf("%s/%s r%d", d.Task, slug, from), fmt.Sprintf("%s/%s r%d", d.Task, slug, to), a.Body, b.Body),
			Hunks:   diff.Hunks(a.Body, b.Body, 3),
		}
		if res.Hunks == nil {
			res.Hunks = []diff.Hunk{}
		}
		return nil
	})
	return res, err
}

// SetPlanItem ticks or unticks an item and/or appends a note, recording who
// did it (by: owner | agent; "" = agent). It does not create a revision.
func (s *Store) SetPlanItem(ctx context.Context, task, slug, item string, done *bool, note, by string) (PlanItem, error) {
	n, err := ParseTaskID(task)
	if err != nil {
		return PlanItem{}, err
	}
	if by == "" {
		by = AuthorAgent
	}
	if by != AuthorAgent && by != AuthorOwner {
		return PlanItem{}, invalid("bad by %q: use owner or agent", by)
	}
	if done == nil && strings.TrimSpace(note) == "" {
		return PlanItem{}, invalid("nothing to change: pass done, note or both")
	}
	var out PlanItem
	err = s.tx(ctx, func(tx *sql.Tx) error {
		now := s.ts()
		d, err := getDoc(ctx, tx, n, slug)
		if err != nil {
			return err
		}
		var itemID int64
		var curDone bool
		err = tx.QueryRowContext(ctx, `SELECT id, done FROM plan_items WHERE document_id = ? AND key = ?`, d.id, item).Scan(&itemID, &curDone)
		if errors.Is(err, sql.ErrNoRows) {
			return notFound("plan item %q not found in %s/%s; doc_get lists item ids", item, d.Task, slug)
		}
		if err != nil {
			return err
		}
		if done != nil && *done != curDone {
			var doneAt any
			if *done {
				doneAt = now
			}
			if _, err := tx.ExecContext(ctx, `UPDATE plan_items SET done = ?, done_at = ?, done_by = ?, updated_at = ? WHERE id = ?`, boolInt(*done), doneAt, by, now, itemID); err != nil {
				return err
			}
		}
		if strings.TrimSpace(note) != "" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO plan_item_notes (item_id, author, body, created_at) VALUES (?, ?, ?, ?)`, itemID, by, strings.TrimSpace(note), now); err != nil {
				return err
			}
		}
		if err := touchTask(ctx, tx, n, now); err != nil {
			return err
		}
		// Return the item as it appears in the latest revision (or its last
		// appearance if it was dropped from the document).
		err = tx.QueryRowContext(ctx, `SELECT pi.key, pr.text, pi.done, coalesce(pi.done_at, ''), pi.done_by, pr.line FROM plan_items pi
JOIN plan_item_revisions pr ON pr.item_id = pi.id JOIN revisions r ON r.id = pr.revision_id
WHERE pi.id = ? ORDER BY r.n DESC LIMIT 1`, itemID).Scan(&out.ID, &out.Text, &out.Done, &out.DoneAt, &out.DoneBy, &out.Line)
		if err != nil {
			return err
		}
		out.Notes, err = itemNotes(ctx, tx, itemID)
		return err
	})
	return out, err
}
