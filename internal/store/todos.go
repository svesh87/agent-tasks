package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/svesh87/agent-tasks/internal/textproc"
)

// TodoStates are the states of a todo.
var TodoStates = []string{"open", "done", "dropped", "in_task"}

// Todo is a standalone note to grow into a task later or be dropped.
type Todo struct {
	ID         string   `json:"id"`
	Text       string   `json:"text"`
	Note       string   `json:"note,omitempty"`
	State      string   `json:"state"`
	Task       string   `json:"task,omitempty"`        // set when in_task
	OriginTask string   `json:"origin_task,omitempty"` // where it came up (provenance only)
	Places     []Place  `json:"places"`
	Tags       []string `json:"tags"`
	Source     string   `json:"source,omitempty"`
	CreatedAt  string   `json:"created_at"`
	UpdatedAt  string   `json:"updated_at"`
	ClosedAt   string   `json:"closed_at,omitempty"`
}

// TodoID formats a todo number as D-<n>.
func TodoID(n int64) string { return "D-" + strconv.FormatInt(n, 10) }

// ParseTodoID accepts "D-7", "d-7" or "7".
func ParseTodoID(id string) (int64, error) {
	s := strings.TrimSpace(id)
	if len(s) > 2 && (s[0] == 'D' || s[0] == 'd') && s[1] == '-' {
		s = s[2:]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, invalid("bad todo id %q: expected D-<number>, e.g. D-7", id)
	}
	return n, nil
}

func getTodo(ctx context.Context, q queryer, n int64) (Todo, error) {
	var t Todo
	var num int64
	var task, origin sql.NullInt64
	var closed sql.NullString
	err := q.QueryRowContext(ctx, `SELECT id, text, note, state, task_id, origin_task_id, source, created_at, updated_at, closed_at FROM todos WHERE id = ?`, n).
		Scan(&num, &t.Text, &t.Note, &t.State, &task, &origin, &t.Source, &t.CreatedAt, &t.UpdatedAt, &closed)
	if errors.Is(err, sql.ErrNoRows) {
		return Todo{}, notFound("todo %s not found", TodoID(n))
	}
	if err != nil {
		return Todo{}, err
	}
	t.ID = TodoID(num)
	if task.Valid {
		t.Task = TaskID(task.Int64)
	}
	if origin.Valid {
		t.OriginTask = TaskID(origin.Int64)
	}
	t.ClosedAt = nullStr(closed)
	rows, err := q.QueryContext(ctx, `SELECT `+placeCols+` FROM todo_places dp JOIN places p ON p.id = dp.place_id WHERE dp.todo_id = ? ORDER BY p.key`, n)
	if err != nil {
		return Todo{}, err
	}
	t.Places = []Place{}
	for rows.Next() {
		p, err := scanPlace(rows)
		if err != nil {
			rows.Close()
			return Todo{}, err
		}
		t.Places = append(t.Places, p)
	}
	rows.Close()
	trows, err := q.QueryContext(ctx, `SELECT tag FROM todo_tags WHERE todo_id = ? ORDER BY tag`, n)
	if err != nil {
		return Todo{}, err
	}
	defer trows.Close()
	t.Tags = []string{}
	for trows.Next() {
		var tag string
		if err := trows.Scan(&tag); err != nil {
			return Todo{}, err
		}
		t.Tags = append(t.Tags, tag)
	}
	return t, trows.Err()
}

func indexTodo(ctx context.Context, tx *sql.Tx, n int64) error {
	var text, note string
	if err := tx.QueryRowContext(ctx, `SELECT text, note FROM todos WHERE id = ?`, n).Scan(&text, &note); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM todo_search WHERE todo_id = ?`, n); err != nil {
		return err
	}
	raw := text + "\n\n" + note
	_, err := tx.ExecContext(ctx, `INSERT INTO todo_search (todo_id, raw, stems) VALUES (?, ?, ?)`, n, raw, textproc.IndexText(raw))
	return err
}

func oneLine(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", invalid("todo text is empty")
	}
	if strings.ContainsAny(text, "\r\n") {
		return "", invalid("todo text must be one line; put details in note")
	}
	return text, nil
}

// NewTodo holds the fields of todo_add.
type NewTodo struct {
	Text       string
	Note       string
	Places     []string
	OriginTask string
	Tags       []string
	Source     string
}

// AddTodo creates an open todo.
func (s *Store) AddTodo(ctx context.Context, nt NewTodo) (Todo, error) {
	text, err := oneLine(nt.Text)
	if err != nil {
		return Todo{}, err
	}
	var origin any
	if nt.OriginTask != "" {
		n, err := ParseTaskID(nt.OriginTask)
		if err != nil {
			return Todo{}, err
		}
		origin = n
	}
	var out Todo
	err = s.tx(ctx, func(tx *sql.Tx) error {
		if origin != nil {
			if _, err := getTask(ctx, tx, origin.(int64)); err != nil {
				return err
			}
		}
		now := s.ts()
		res, err := tx.ExecContext(ctx, `INSERT INTO todos (text, note, state, origin_task_id, source, created_at, updated_at) VALUES (?, ?, 'open', ?, ?, ?, ?)`,
			text, strings.TrimSpace(nt.Note), origin, strings.TrimSpace(nt.Source), now, now)
		if err != nil {
			return err
		}
		n, _ := res.LastInsertId()
		if err := s.todoLinks(ctx, tx, n, nt.Places, nil, nt.Tags, nil); err != nil {
			return err
		}
		if err := indexTodo(ctx, tx, n); err != nil {
			return err
		}
		out, err = getTodo(ctx, tx, n)
		return err
	})
	return out, err
}

func (s *Store) todoLinks(ctx context.Context, tx *sql.Tx, n int64, placesAdd, placesRemove, tagsAdd, tagsRemove []string) error {
	for _, ref := range placesAdd {
		p, err := s.ensurePlace(ctx, tx, ref)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO todo_places (todo_id, place_id) VALUES (?, ?)`, n, p.id); err != nil {
			return err
		}
	}
	for _, ref := range placesRemove {
		_, key, err := ParsePlaceKey(ref)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM todo_places WHERE todo_id = ? AND place_id = (SELECT id FROM places WHERE key = ?)`, n, key); err != nil {
			return err
		}
	}
	for _, tag := range normTags(tagsAdd) {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO todo_tags (todo_id, tag) VALUES (?, ?)`, n, tag); err != nil {
			return err
		}
	}
	for _, tag := range normTags(tagsRemove) {
		if _, err := tx.ExecContext(ctx, `DELETE FROM todo_tags WHERE todo_id = ? AND tag = ?`, n, tag); err != nil {
			return err
		}
	}
	return nil
}

// GetTodo returns one todo.
func (s *Store) GetTodo(ctx context.Context, id string) (Todo, error) {
	n, err := ParseTodoID(id)
	if err != nil {
		return Todo{}, err
	}
	var t Todo
	err = s.tx(ctx, func(tx *sql.Tx) error {
		t, err = getTodo(ctx, tx, n)
		return err
	})
	return t, err
}

// TodoPatch holds the fields of todo_update; nil means unchanged.
type TodoPatch struct {
	Text         *string
	Note         *string
	State        *string // open | done | dropped; in_task only through task_create from_todos
	PlacesAdd    []string
	PlacesRemove []string
	TagsAdd      []string
	TagsRemove   []string
}

// UpdateTodo edits or closes a todo. Reopening a todo that went into a task
// clears the task link.
func (s *Store) UpdateTodo(ctx context.Context, id string, p TodoPatch) (Todo, error) {
	n, err := ParseTodoID(id)
	if err != nil {
		return Todo{}, err
	}
	var out Todo
	err = s.tx(ctx, func(tx *sql.Tx) error {
		out, err = s.updateTodoTx(ctx, tx, n, p)
		return err
	})
	return out, err
}

// UpdateTodos applies the same patch (typically a state) to several todos in
// one transaction: all succeed or none.
func (s *Store) UpdateTodos(ctx context.Context, ids []string, p TodoPatch) ([]Todo, error) {
	if len(ids) == 0 {
		return nil, invalid("no todo ids")
	}
	nums := make([]int64, len(ids))
	for i, id := range ids {
		n, err := ParseTodoID(id)
		if err != nil {
			return nil, err
		}
		nums[i] = n
	}
	out := []Todo{}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		for _, n := range nums {
			t, err := s.updateTodoTx(ctx, tx, n, p)
			if err != nil {
				return &kindError{kindOf(err), TodoID(n) + ": " + err.Error()}
			}
			out = append(out, t)
		}
		return nil
	})
	return out, err
}

func (s *Store) updateTodoTx(ctx context.Context, tx *sql.Tx, n int64, p TodoPatch) (Todo, error) {
	var out Todo
	err := func() error {
		cur, err := getTodo(ctx, tx, n)
		if err != nil {
			return err
		}
		now := s.ts()
		if p.Text != nil {
			if cur.Text, err = oneLine(*p.Text); err != nil {
				return err
			}
		}
		if p.Note != nil {
			cur.Note = strings.TrimSpace(*p.Note)
		}
		closedAt := any(nil)
		if cur.ClosedAt != "" {
			closedAt = cur.ClosedAt
		}
		var taskID any
		if cur.Task != "" {
			tn, _ := ParseTaskID(cur.Task)
			taskID = tn
		}
		if p.State != nil && *p.State != cur.State {
			switch *p.State {
			case "open":
				closedAt, taskID = nil, nil
			case "done", "dropped":
				closedAt, taskID = now, nil
			case "in_task":
				return invalid("a todo moves to in_task only through task_create from_todos")
			default:
				return invalid("bad todo state %q: use open, done or dropped", *p.State)
			}
			cur.State = *p.State
		}
		if _, err := tx.ExecContext(ctx, `UPDATE todos SET text = ?, note = ?, state = ?, task_id = ?, closed_at = ?, updated_at = ? WHERE id = ?`,
			cur.Text, cur.Note, cur.State, taskID, closedAt, now, n); err != nil {
			return err
		}
		if err := s.todoLinks(ctx, tx, n, p.PlacesAdd, p.PlacesRemove, p.TagsAdd, p.TagsRemove); err != nil {
			return err
		}
		if err := indexTodo(ctx, tx, n); err != nil {
			return err
		}
		out, err = getTodo(ctx, tx, n)
		return err
	}()
	return out, err
}

// TodoListParams are the filters of todo_list.
type TodoListParams struct {
	Place  string   // place key; "none" = todos without places
	States []string // default: open
	Query  string
	Tags   []string
	Limit  int // default 50, max 200
	Offset int
	Facets bool // fill StateCounts and TagCounts
}

// TodoList is a page of todos.
type TodoList struct {
	Total       int            `json:"total"`
	Todos       []Todo         `json:"todos"`
	StateCounts map[string]int `json:"state_counts,omitempty"` // all filters except state
	TagCounts   []TagCount     `json:"tag_counts,omitempty"`   // over the whole result
}

type todoFilter struct {
	placeID   int64
	noPlace   bool
	states    []string
	match     string
	tags      []string
	limit     int
	offset    int
	wantTotal *int
	facets    *TodoList // when set, StateCounts and TagCounts are filled
}

func listTodos(ctx context.Context, tx *sql.Tx, f todoFilter) ([]Todo, error) {
	var with, where strings.Builder
	var args []any
	from := ` FROM todos d`
	if f.match != "" {
		with.WriteString(`WITH m AS MATERIALIZED (SELECT todo_id, bm25(todo_search) AS r FROM todo_search WHERE todo_search MATCH ?) `)
		from += ` JOIN m ON m.todo_id = d.id`
		args = append(args, f.match)
	}
	where.WriteString(` WHERE 1 = 1`)
	if f.placeID != 0 {
		where.WriteString(` AND EXISTS (SELECT 1 FROM todo_places dp WHERE dp.todo_id = d.id AND dp.place_id = ?)`)
		args = append(args, f.placeID)
	}
	if f.noPlace {
		where.WriteString(` AND NOT EXISTS (SELECT 1 FROM todo_places dp WHERE dp.todo_id = d.id)`)
	}
	for _, tag := range normTags(f.tags) {
		where.WriteString(` AND EXISTS (SELECT 1 FROM todo_tags dt WHERE dt.todo_id = d.id AND dt.tag = ?)`)
		args = append(args, tag)
	}
	if f.facets != nil {
		if err := todoFacets(ctx, tx, f, with.String(), from+where.String(), args); err != nil {
			return nil, err
		}
	}
	where.WriteString(` AND d.state IN (` + strings.Repeat("?,", len(f.states)-1) + `?)`)
	for _, st := range f.states {
		args = append(args, st)
	}
	if f.facets != nil {
		rows, err := tx.QueryContext(ctx, with.String()+`SELECT dt.tag, count(*) FROM todo_tags dt WHERE dt.todo_id IN (SELECT d.id`+from+where.String()+`)
GROUP BY dt.tag ORDER BY count(*) DESC, dt.tag`, args...)
		if err != nil {
			return nil, err
		}
		f.facets.TagCounts = []TagCount{}
		for rows.Next() {
			var tc TagCount
			if err := rows.Scan(&tc.Tag, &tc.Count); err != nil {
				rows.Close()
				return nil, err
			}
			f.facets.TagCounts = append(f.facets.TagCounts, tc)
		}
		rows.Close()
	}
	if f.wantTotal != nil {
		if err := tx.QueryRowContext(ctx, with.String()+`SELECT count(*)`+from+where.String(), args...).Scan(f.wantTotal); err != nil {
			return nil, err
		}
	}
	order := ` ORDER BY d.created_at DESC, d.id DESC`
	if f.match != "" {
		order = ` ORDER BY m.r, d.created_at DESC`
	}
	rows, err := tx.QueryContext(ctx, with.String()+`SELECT d.id`+from+where.String()+order+` LIMIT ? OFFSET ?`, append(args, f.limit, f.offset)...)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	out := []Todo{}
	for _, id := range ids {
		t, err := getTodo(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func todoFacets(ctx context.Context, tx *sql.Tx, f todoFilter, with, fromWhere string, args []any) error {
	f.facets.StateCounts = map[string]int{}
	for _, st := range TodoStates {
		f.facets.StateCounts[st] = 0
	}
	rows, err := tx.QueryContext(ctx, with+`SELECT d.state, count(*)`+fromWhere+` GROUP BY d.state`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return err
		}
		f.facets.StateCounts[st] = n
	}
	return rows.Err()
}

// ListTodos finds todos by place, state, words and tags; newest first (best
// match first with a query).
func (s *Store) ListTodos(ctx context.Context, p TodoListParams) (TodoList, error) {
	states := p.States
	if len(states) == 0 {
		states = []string{"open"}
	}
	for _, st := range states {
		if !slices.Contains(TodoStates, st) {
			return TodoList{}, invalid("bad todo state %q: use open, done, dropped or in_task", st)
		}
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}
	f := todoFilter{states: states, match: textproc.MatchQuery(p.Query), tags: p.Tags, limit: min(limit, 200), offset: max(p.Offset, 0)}
	var res TodoList
	f.wantTotal = &res.Total
	if p.Facets {
		f.facets = &res
	}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		switch strings.TrimSpace(p.Place) {
		case "":
		case "none":
			f.noPlace = true
		default:
			pl, err := lookupPlace(ctx, tx, p.Place)
			if errors.Is(err, ErrNotFound) {
				res.Todos = []Todo{}
				return nil
			}
			if err != nil {
				return err
			}
			f.placeID = pl.id
		}
		var err error
		res.Todos, err = listTodos(ctx, tx, f)
		return err
	})
	return res, err
}

func taskTodos(ctx context.Context, q queryer, taskID int64) ([]Todo, error) {
	rows, err := q.QueryContext(ctx, `SELECT id FROM todos WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	out := []Todo{}
	for _, id := range ids {
		t, err := getTodo(ctx, q, id)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}
