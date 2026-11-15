package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/svesh87/agent-tasks/internal/anchor"
	"github.com/svesh87/agent-tasks/internal/textproc"
)

// indexRow replaces the search row of (task, src) with text.
func indexRow(ctx context.Context, tx *sql.Tx, taskID int64, src, text string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM search WHERE task_id = ? AND src = ?`, taskID, src); err != nil {
		return err
	}
	if strings.TrimSpace(text) == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO search (task_id, src, raw, stems) VALUES (?, ?, ?, ?)`,
		taskID, src, text, textproc.IndexText(text))
	return err
}

func indexTask(ctx context.Context, tx *sql.Tx, taskID int64) error {
	var title, summary string
	if err := tx.QueryRowContext(ctx, `SELECT title, summary FROM tasks WHERE id = ?`, taskID).Scan(&title, &summary); err != nil {
		return err
	}
	return indexRow(ctx, tx, taskID, "task", title+"\n\n"+summary)
}

// indexComment indexes a comment with its replies, unless its round is a
// draft (drafts are invisible to agents).
func indexComment(ctx context.Context, tx *sql.Tx, commentID int64) error {
	var taskID int64
	var body, state string
	err := tx.QueryRowContext(ctx, `SELECT d.task_id, c.body, rr.state FROM comments c
JOIN review_rounds rr ON rr.id = c.round_id JOIN documents d ON d.id = rr.document_id WHERE c.id = ?`, commentID).Scan(&taskID, &body, &state)
	if err != nil {
		return err
	}
	src := fmt.Sprintf("comment:%d", commentID)
	if state == "draft" {
		return indexRow(ctx, tx, taskID, src, "")
	}
	rows, err := tx.QueryContext(ctx, `SELECT body FROM comment_replies WHERE comment_id = ? ORDER BY id`, commentID)
	if err != nil {
		return err
	}
	parts := []string{body}
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err != nil {
			rows.Close()
			return err
		}
		parts = append(parts, b)
	}
	rows.Close()
	return indexRow(ctx, tx, taskID, src, strings.Join(parts, "\n\n"))
}

// SearchParams are the filters of task_search.
type SearchParams struct {
	Query    string
	Statuses []string // empty: all but archived
	Tags     []string // all must be present
	Place    string   // place key (git remote/host path or machine:/path); "none" = tasks without places
	Sort     string   // updated | created | relevance (default: relevance with a query, else updated)
	Limit    int      // default 20, max 100
	Offset   int
	Facets   bool // fill StatusCounts and TagCounts
	RowStats bool // fill TaskRow of every hit (list view)
	Todos    bool // with a query: also return matching todos (up to 10, any state)
}

// Hit is a search snippet from one source of a task.
type Hit struct {
	Source string `json:"source"` // task, doc:<slug>, journal:<id>, comment:<id>, round:<id>
	// Kind of the matched object: task | document | comment | round | journal.
	Kind     string `json:"kind"`
	Slug     string `json:"slug,omitempty"`     // document, comment, round
	Revision int    `json:"revision,omitempty"` // document: latest; comment, round: the reviewed revision
	Start    int    `json:"start,omitempty"`    // document: rune offset of the match in the revision body
	End      int    `json:"end,omitempty"`
	Line     int    `json:"line,omitempty"` // document: 1-based line of the match (0 = in the title)
	Comment  int64  `json:"comment,omitempty"`
	Round    int64  `json:"round,omitempty"`
	Journal  int64  `json:"journal,omitempty"`
	textproc.Snippet
}

// TodoHit is a todo matched by a task search query.
type TodoHit struct {
	Todo
	Snippet textproc.Snippet `json:"snippet"`
}

// TaskRow is what a task list row shows besides the task fields.
type TaskRow struct {
	PlanProgress    *Progress `json:"plan_progress"` // main plan document (slug plan, else first kind plan); null if none
	DocCount        int       `json:"doc_count"`
	SubmittedRounds int       `json:"submitted_rounds"`
	DraftRounds     int       `json:"draft_rounds"`
}

// TaskHit is a task found by a search.
type TaskHit struct {
	Task
	*TaskRow
	Hits []Hit `json:"hits,omitempty"`
}

// SearchResult is a page of search results.
type SearchResult struct {
	Total        int            `json:"total"`
	Tasks        []TaskHit      `json:"tasks"`
	StatusCounts map[string]int `json:"status_counts,omitempty"` // all filters except status
	TagCounts    []TagCount     `json:"tag_counts,omitempty"`    // over the whole result (all filters)
	Todos        []TodoHit      `json:"todos,omitempty"`         // todos matching the query (not filtered by task filters)
}

// SortOrders are the accepted sort values.
var SortOrders = []string{"updated", "created", "relevance"}

// Search finds tasks by full text and filters.
func (s *Store) Search(ctx context.Context, p SearchParams) (SearchResult, error) {
	for _, st := range p.Statuses {
		if !slices.Contains(Statuses, st) {
			return SearchResult{}, invalid("bad status %q: use active, waiting, later or archived", st)
		}
	}
	match := textproc.MatchQuery(p.Query)
	sortBy := p.Sort
	if sortBy == "" {
		sortBy = "updated"
		if match != "" {
			sortBy = "relevance"
		}
	}
	if !slices.Contains(SortOrders, sortBy) {
		return SearchResult{}, invalid("bad sort %q: use updated, created or relevance", sortBy)
	}
	if sortBy == "relevance" && match == "" {
		sortBy = "updated"
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 20
	}
	limit = min(limit, 100)
	offset := max(p.Offset, 0)

	var with, from strings.Builder
	var args []any
	from.WriteString(` FROM tasks t`)
	if match != "" {
		// bm25() only works directly in the FTS query, so it is materialised
		// before grouping per task.
		with.WriteString(`WITH m AS MATERIALIZED (SELECT task_id, bm25(search) AS r FROM search WHERE search MATCH ?),
h AS (SELECT task_id, min(r) AS rank FROM m GROUP BY task_id) `)
		from.WriteString(` JOIN h ON h.task_id = t.id`)
		args = append(args, match)
	}
	from.WriteString(` WHERE 1 = 1`)
	for _, tag := range normTags(p.Tags) {
		from.WriteString(` AND EXISTS (SELECT 1 FROM task_tags tt WHERE tt.task_id = t.id AND tt.tag = ?)`)
		args = append(args, tag)
	}
	switch pl := strings.TrimSpace(p.Place); pl {
	case "":
	case "none":
		from.WriteString(` AND NOT EXISTS (SELECT 1 FROM task_places tp WHERE tp.task_id = t.id)`)
	default:
		_, key, err := ParsePlaceKey(pl)
		if err != nil {
			return SearchResult{}, err
		}
		from.WriteString(` AND EXISTS (SELECT 1 FROM task_places tp JOIN places pp ON pp.id = tp.place_id WHERE tp.task_id = t.id AND pp.key = ?)`)
		args = append(args, key)
	}
	// status filter last, so facets can run without it
	baseFrom, baseArgs := from.String(), slices.Clone(args)
	if len(p.Statuses) == 0 {
		from.WriteString(` AND t.status != 'archived'`)
	} else {
		from.WriteString(` AND t.status IN (` + strings.Repeat("?,", len(p.Statuses)-1) + `?)`)
		for _, st := range p.Statuses {
			args = append(args, st)
		}
	}
	order := map[string]string{
		"updated":   ` ORDER BY t.updated_at DESC, t.id DESC`,
		"created":   ` ORDER BY t.created_at DESC, t.id DESC`,
		"relevance": ` ORDER BY h.rank, t.updated_at DESC`,
	}[sortBy]

	var res SearchResult
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, with.String()+`SELECT count(*)`+from.String(), args...).Scan(&res.Total); err != nil {
			return fmt.Errorf("search: %w", err)
		}
		rows, err := tx.QueryContext(ctx, with.String()+`SELECT t.id`+from.String()+order+` LIMIT ? OFFSET ?`, append(args, limit, offset)...)
		if err != nil {
			return fmt.Errorf("search: %w", err)
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		res.Tasks = []TaskHit{}
		for _, id := range ids {
			t, err := getTask(ctx, tx, id)
			if err != nil {
				return err
			}
			th := TaskHit{Task: t}
			if match != "" {
				if th.Hits, err = snippets(ctx, tx, id, match, p.Query); err != nil {
					return err
				}
			}
			if p.RowStats {
				if th.TaskRow, err = taskRow(ctx, tx, id); err != nil {
					return err
				}
			}
			res.Tasks = append(res.Tasks, th)
		}
		if p.Todos && match != "" {
			if res.Todos, err = searchTodos(ctx, tx, match, p.Query, 10); err != nil {
				return fmt.Errorf("search todos: %w", err)
			}
		}
		if !p.Facets {
			return nil
		}
		res.StatusCounts = map[string]int{}
		for _, st := range Statuses {
			res.StatusCounts[st] = 0
		}
		rows, err = tx.QueryContext(ctx, with.String()+`SELECT t.status, count(*)`+baseFrom+` GROUP BY t.status`, baseArgs...)
		if err != nil {
			return fmt.Errorf("facets: %w", err)
		}
		for rows.Next() {
			var st string
			var n int
			if err := rows.Scan(&st, &n); err != nil {
				rows.Close()
				return err
			}
			res.StatusCounts[st] = n
		}
		rows.Close()
		rows, err = tx.QueryContext(ctx, with.String()+`SELECT tt.tag, count(*) FROM task_tags tt WHERE tt.task_id IN (SELECT t.id`+from.String()+`)
GROUP BY tt.tag ORDER BY count(*) DESC, tt.tag`, args...)
		if err != nil {
			return fmt.Errorf("facets: %w", err)
		}
		defer rows.Close()
		res.TagCounts = []TagCount{}
		for rows.Next() {
			var tc TagCount
			if err := rows.Scan(&tc.Tag, &tc.Count); err != nil {
				return err
			}
			res.TagCounts = append(res.TagCounts, tc)
		}
		return rows.Err()
	})
	return res, err
}

func taskRow(ctx context.Context, tx *sql.Tx, taskID int64) (*TaskRow, error) {
	r := &TaskRow{}
	err := tx.QueryRowContext(ctx, `SELECT
  (SELECT count(*) FROM documents WHERE task_id = ?),
  (SELECT count(*) FROM review_rounds rr JOIN documents d ON d.id = rr.document_id WHERE d.task_id = ? AND rr.state = 'submitted'),
  (SELECT count(*) FROM review_rounds rr JOIN documents d ON d.id = rr.document_id WHERE d.task_id = ? AND rr.state = 'draft')`,
		taskID, taskID, taskID).Scan(&r.DocCount, &r.SubmittedRounds, &r.DraftRounds)
	if err != nil {
		return nil, err
	}
	var revID int64
	err = tx.QueryRowContext(ctx, `SELECT (SELECT id FROM revisions r WHERE r.document_id = d.id ORDER BY n DESC LIMIT 1)
FROM documents d WHERE d.task_id = ? AND (d.slug = 'plan' OR d.kind = 'plan')
ORDER BY d.slug != 'plan', d.id LIMIT 1`, taskID).Scan(&revID)
	if errors.Is(err, sql.ErrNoRows) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	var pr Progress
	if err := tx.QueryRowContext(ctx, `SELECT count(*), coalesce(sum(pi.done), 0) FROM plan_item_revisions pr
JOIN plan_items pi ON pi.id = pr.item_id WHERE pr.revision_id = ?`, revID).Scan(&pr.Total, &pr.Done); err != nil {
		return nil, err
	}
	r.PlanProgress = &pr
	return r, nil
}

func snippets(ctx context.Context, tx *sql.Tx, taskID int64, match, query string) ([]Hit, error) {
	rows, err := tx.QueryContext(ctx, `SELECT src, raw FROM search WHERE search MATCH ? AND task_id = ? ORDER BY rank LIMIT 3`, match, taskID)
	if err != nil {
		return nil, err
	}
	var hits []Hit
	for rows.Next() {
		var src, raw string
		if err := rows.Scan(&src, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		if sn, ok := textproc.MakeSnippet(raw, query, 160); ok {
			hits = append(hits, Hit{Source: src, Snippet: sn})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range hits {
		if err := locateHit(ctx, tx, taskID, query, &hits[i]); err != nil {
			return nil, err
		}
	}
	return hits, nil
}

// locateHit fills the kind and the position of the matched object, so a
// client can link straight to it.
func locateHit(ctx context.Context, tx *sql.Tx, taskID int64, query string, h *Hit) error {
	prefix, rest, _ := strings.Cut(h.Source, ":")
	id, _ := strconv.ParseInt(rest, 10, 64)
	var err error
	switch prefix {
	case "task":
		h.Kind = "task"
	case "doc":
		h.Kind, h.Slug = "document", rest
		var title, body string
		err = tx.QueryRowContext(ctx, `SELECT d.title, r.n, r.body FROM documents d JOIN revisions r ON r.document_id = d.id
WHERE d.task_id = ? AND d.slug = ? ORDER BY r.n DESC LIMIT 1`, taskID, rest).Scan(&title, &h.Revision, &body)
		// the indexed text is title + "\n\n" + body; position the link on
		// the first match in the body even if the title matched first
		if skip := len([]rune(title)) + 2; err == nil && h.At >= skip {
			h.Start, h.End = h.At-skip, h.AtEnd-skip
			h.Line = anchor.Line(body, h.Start)
		} else if sn, ok := textproc.MakeSnippet(body, query, 10); err == nil && ok {
			h.Start, h.End = sn.At, sn.AtEnd
			h.Line = anchor.Line(body, h.Start)
		}
	case "comment":
		h.Kind, h.Comment = "comment", id
		err = tx.QueryRowContext(ctx, `SELECT d.slug, rv.n, rr.id FROM comments c JOIN review_rounds rr ON rr.id = c.round_id
JOIN documents d ON d.id = rr.document_id JOIN revisions rv ON rv.id = rr.revision_id WHERE c.id = ?`, id).Scan(&h.Slug, &h.Revision, &h.Round)
	case "round":
		h.Kind, h.Round = "round", id
		err = tx.QueryRowContext(ctx, `SELECT d.slug, rv.n FROM review_rounds rr JOIN documents d ON d.id = rr.document_id
JOIN revisions rv ON rv.id = rr.revision_id WHERE rr.id = ?`, id).Scan(&h.Slug, &h.Revision)
	case "journal":
		h.Kind, h.Journal = "journal", id
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil // stale index row; keep the hit as is
	}
	return err
}

// searchTodos returns up to limit todos (any state) matching the query.
func searchTodos(ctx context.Context, tx *sql.Tx, match, query string, limit int) ([]TodoHit, error) {
	rows, err := tx.QueryContext(ctx, `SELECT todo_id, raw FROM todo_search WHERE todo_search MATCH ? ORDER BY rank LIMIT ?`, match, limit)
	if err != nil {
		return nil, err
	}
	type row struct {
		id  int64
		raw string
	}
	var found []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.raw); err != nil {
			rows.Close()
			return nil, err
		}
		found = append(found, r)
	}
	rows.Close()
	out := []TodoHit{}
	for _, r := range found {
		t, err := getTodo(ctx, tx, r.id)
		if err != nil {
			return nil, err
		}
		sn, _ := textproc.MakeSnippet(r.raw, query, 160)
		out = append(out, TodoHit{Todo: t, Snippet: sn})
	}
	return out, nil
}
