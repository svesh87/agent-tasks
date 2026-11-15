package store

import (
	"context"
	"database/sql"
	"errors"
	"path"
	"regexp"
	"slices"
	"strings"
)

// Place kinds and task place roles.
const (
	PlaceGit  = "git"
	PlacePath = "path"
)

// PlaceRoles are the roles a place can have in a task.
var PlaceRoles = []string{"primary", "touched"}

// Place is a repository (kind git, key host/path) or a directory without a
// remote (kind path, key <machine>:<absolute path>).
type Place struct {
	Key       string     `json:"key"`
	Kind      string     `json:"kind"`
	Label     string     `json:"label"`
	CreatedAt string     `json:"created_at,omitempty"`
	Locations []Location `json:"locations,omitempty"`

	id int64
}

// Location is where a place lives on a machine.
type Location struct {
	Machine string `json:"machine"`
	Path    string `json:"path"`
	SeenAt  string `json:"seen_at"`
}

// TaskPlace is a place linked to a task.
type TaskPlace struct {
	Place
	Role string `json:"role"`
	Note string `json:"note,omitempty"`
	// Here is the path on the caller's machine, when the caller named one
	// (MCP task_get machine); empty = not known there.
	Here string `json:"here,omitempty"`
}

// PlaceRef names a place to link: key (git remote, host/path or
// machine:/path), role and note.
type PlaceRef struct {
	Place string `json:"place"`
	Role  string `json:"role,omitempty"`
	Note  string `json:"note,omitempty"`
}

// <machine>:<absolute path>; a '@' before the colon means an scp-style git
// remote (git@host:/srv/repo.git) instead.
var pathKeyRe = regexp.MustCompile(`^([^:/@\s]+):(/.*|[A-Za-z]:[\\/].*)$`)

// cleanPath normalises a directory path: forward slashes, no trailing slash,
// no "." or ".." parts.
func cleanPath(p string) string {
	p = strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
	if p == "" {
		return ""
	}
	return path.Clean(p)
}

// PathKey is the key of a path place.
func PathKey(machine, dir string) string { return strings.TrimSpace(machine) + ":" + cleanPath(dir) }

// ParsePlaceKey returns the kind and the canonical key of a place reference:
// "<machine>:/abs/path" is a path place; anything else is a git remote or
// host/path, normalised with NormalizeRepo.
func ParsePlaceKey(s string) (kind, key string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", invalid("empty place key")
	}
	if m := pathKeyRe.FindStringSubmatch(s); m != nil && !strings.Contains(s, "://") {
		return PlacePath, PathKey(m[1], m[2]), nil
	}
	key = NormalizeRepo(s)
	if !strings.Contains(key, "/") {
		return "", "", invalid("bad place %q: use a git remote or host/path (github.com/owner/repo), or <machine>:<absolute path>", s)
	}
	return PlaceGit, key, nil
}

func placeLabel(kind, key string) string {
	if kind == PlacePath {
		_, p, _ := strings.Cut(key, ":")
		return path.Base(p)
	}
	return path.Base(key)
}

func scanPlace(row interface{ Scan(...any) error }) (Place, error) {
	var p Place
	err := row.Scan(&p.id, &p.Kind, &p.Key, &p.Label, &p.CreatedAt)
	return p, err
}

const placeCols = `p.id, p.kind, p.key, p.label, p.created_at`

func placeByKey(ctx context.Context, q queryer, key string) (Place, error) {
	p, err := scanPlace(q.QueryRowContext(ctx, `SELECT `+placeCols+` FROM places p WHERE p.key = ?`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return Place{}, notFound("place %q is not known; call place_here in its directory to register it", key)
	}
	return p, err
}

// ensurePlace returns the place for a reference, creating it when new.
func (s *Store) ensurePlace(ctx context.Context, tx *sql.Tx, ref string) (Place, error) {
	kind, key, err := ParsePlaceKey(ref)
	if err != nil {
		return Place{}, err
	}
	p, err := placeByKey(ctx, tx, key)
	if err == nil || !errors.Is(err, ErrNotFound) {
		return p, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO places (kind, key, label, created_at) VALUES (?, ?, ?, ?)`,
		kind, key, placeLabel(kind, key), s.ts()); err != nil {
		return Place{}, err
	}
	return placeByKey(ctx, tx, key)
}

// lookupPlace resolves a reference to an existing place.
func lookupPlace(ctx context.Context, q queryer, ref string) (Place, error) {
	_, key, err := ParsePlaceKey(ref)
	if err != nil {
		return Place{}, err
	}
	return placeByKey(ctx, q, key)
}

func locations(ctx context.Context, q queryer, placeID int64, machine string) ([]Location, error) {
	query := `SELECT machine, path, seen_at FROM place_locations WHERE place_id = ?`
	args := []any{placeID}
	if machine != "" {
		query += ` AND machine = ?`
		args = append(args, machine)
	}
	rows, err := q.QueryContext(ctx, query+` ORDER BY machine, seen_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Location{}
	for rows.Next() {
		var l Location
		if err := rows.Scan(&l.Machine, &l.Path, &l.SeenAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// registerLocation records that place lives at dir on machine. When a known
// location of the place on that machine contains dir (dir is a subfolder),
// that location is refreshed instead of adding a new one.
func (s *Store) registerLocation(ctx context.Context, tx *sql.Tx, p Place, machine, dir string) (string, error) {
	machine, dir = strings.TrimSpace(machine), cleanPath(dir)
	if machine == "" || dir == "" {
		return "", nil
	}
	locs, err := locations(ctx, tx, p.id, machine)
	if err != nil {
		return "", err
	}
	for _, l := range locs {
		if dir == l.Path || strings.HasPrefix(dir, strings.TrimSuffix(l.Path, "/")+"/") {
			dir = l.Path
			break
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO place_locations (place_id, machine, path, seen_at) VALUES (?, ?, ?, ?)
ON CONFLICT (place_id, machine, path) DO UPDATE SET seen_at = excluded.seen_at`, p.id, machine, dir, s.ts())
	return dir, err
}

// resolveHere finds (and registers) the place of a directory:
//   - with a remote: the git place of that remote;
//   - else the place with the longest known location on this machine that
//     contains cwd (a subfolder of a known repository or directory);
//   - else a new path place <machine>:<cwd>.
func (s *Store) resolveHere(ctx context.Context, tx *sql.Tx, cwd, remote, machine string) (Place, string, bool, error) {
	machine, cwd = strings.TrimSpace(machine), cleanPath(cwd)
	if strings.TrimSpace(remote) != "" {
		key := NormalizeRepo(remote)
		_, err := placeByKey(ctx, tx, key)
		created := errors.Is(err, ErrNotFound)
		p, err := s.ensurePlace(ctx, tx, key)
		if err != nil {
			return Place{}, "", false, err
		}
		loc, err := s.registerLocation(ctx, tx, p, machine, cwd)
		return p, loc, created, err
	}
	if machine == "" || cwd == "" {
		return Place{}, "", false, invalid("machine and cwd are required when there is no git remote")
	}
	var placeID int64
	var loc string
	err := tx.QueryRowContext(ctx, `SELECT place_id, path FROM place_locations
WHERE machine = ? AND (path = ? OR substr(?, 1, length(path) + 1) = path || '/')
ORDER BY length(path) DESC LIMIT 1`, machine, cwd, cwd).Scan(&placeID, &loc)
	if err == nil {
		p, err := scanPlace(tx.QueryRowContext(ctx, `SELECT `+placeCols+` FROM places p WHERE p.id = ?`, placeID))
		if err != nil {
			return Place{}, "", false, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE place_locations SET seen_at = ? WHERE place_id = ? AND machine = ? AND path = ?`, s.ts(), placeID, machine, loc)
		return p, loc, false, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Place{}, "", false, err
	}
	p, err := s.ensurePlace(ctx, tx, PathKey(machine, cwd))
	if err != nil {
		return Place{}, "", false, err
	}
	loc, err = s.registerLocation(ctx, tx, p, machine, cwd)
	return p, loc, true, err
}

// HereResult is the outcome of place_here.
type HereResult struct {
	Place   Place  `json:"place"`
	Created bool   `json:"created"`
	Path    string `json:"path"` // the registered location on this machine
	Tasks   []Task `json:"tasks"`
	Todos   []Todo `json:"todos"`
}

// PlaceHere resolves the caller's directory to a place, registers the
// location and returns the place's non-archived tasks and open todos.
func (s *Store) PlaceHere(ctx context.Context, cwd, remote, machine string) (HereResult, error) {
	var res HereResult
	err := s.tx(ctx, func(tx *sql.Tx) error {
		p, loc, created, err := s.resolveHere(ctx, tx, cwd, remote, machine)
		if err != nil {
			return err
		}
		if p.Locations, err = locations(ctx, tx, p.id, ""); err != nil {
			return err
		}
		res = HereResult{Place: p, Created: created, Path: loc}
		if res.Tasks, err = placeTasks(ctx, tx, p.id); err != nil {
			return err
		}
		res.Todos, err = listTodos(ctx, tx, todoFilter{placeID: p.id, states: []string{"open"}, limit: 50})
		return err
	})
	return res, err
}

func placeTasks(ctx context.Context, tx *sql.Tx, placeID int64) ([]Task, error) {
	rows, err := tx.QueryContext(ctx, `SELECT t.id FROM tasks t JOIN task_places tp ON tp.task_id = t.id
WHERE tp.place_id = ? AND t.status != 'archived'
ORDER BY CASE t.status WHEN 'active' THEN 0 WHEN 'waiting' THEN 1 ELSE 2 END, t.updated_at DESC LIMIT 20`, placeID)
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
	out := []Task{}
	for _, id := range ids {
		t, err := getTask(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// GetPlace returns a place with all known locations.
func (s *Store) GetPlace(ctx context.Context, ref string) (Place, error) {
	var p Place
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		if p, err = lookupPlace(ctx, tx, ref); err != nil {
			return err
		}
		p.Locations, err = locations(ctx, tx, p.id, "")
		return err
	})
	return p, err
}

// PlaceSummary is a place with counts, for the place list.
type PlaceSummary struct {
	Place
	Tasks     int `json:"tasks"`      // non-archived tasks
	OpenTodos int `json:"open_todos"` // open todos
}

// Places lists places with locations and counts. q, if not empty, keeps
// places whose key, label or a location path contains it (case-insensitive);
// matches on the label come first.
func (s *Store) Places(ctx context.Context, q string) ([]PlaceSummary, error) {
	q = strings.ToLower(strings.TrimSpace(q))
	var out []PlaceSummary
	err := s.tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT `+placeCols+`,
  (SELECT count(*) FROM task_places tp JOIN tasks t ON t.id = tp.task_id WHERE tp.place_id = p.id AND t.status != 'archived'),
  (SELECT count(*) FROM todo_places dp JOIN todos d ON d.id = dp.todo_id WHERE dp.place_id = p.id AND d.state = 'open'),
  coalesce((SELECT group_concat(path, char(10)) FROM place_locations pl WHERE pl.place_id = p.id), '')
FROM places p ORDER BY p.kind, p.key`)
		if err != nil {
			return err
		}
		out = []PlaceSummary{}
		var byLabel, other []PlaceSummary
		for rows.Next() {
			var ps PlaceSummary
			var paths string
			if err := rows.Scan(&ps.id, &ps.Kind, &ps.Key, &ps.Label, &ps.CreatedAt, &ps.Tasks, &ps.OpenTodos, &paths); err != nil {
				rows.Close()
				return err
			}
			switch {
			case q == "" || strings.Contains(strings.ToLower(ps.Label), q):
				byLabel = append(byLabel, ps)
			case strings.Contains(strings.ToLower(ps.Key), q) || strings.Contains(strings.ToLower(paths), q):
				other = append(other, ps)
			}
		}
		out = append(append(out, byLabel...), other...)
		rows.Close()
		for i := range out {
			if out[i].Locations, err = locations(ctx, tx, out[i].id, ""); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

func taskPlaces(ctx context.Context, q queryer, taskID int64) ([]TaskPlace, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+placeCols+`, tp.role, tp.note FROM task_places tp JOIN places p ON p.id = tp.place_id
WHERE tp.task_id = ? ORDER BY CASE tp.role WHEN 'primary' THEN 0 ELSE 1 END, p.key`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TaskPlace{}
	for rows.Next() {
		var tp TaskPlace
		if err := rows.Scan(&tp.id, &tp.Kind, &tp.Key, &tp.Label, &tp.CreatedAt, &tp.Role, &tp.Note); err != nil {
			return nil, err
		}
		out = append(out, tp)
	}
	return out, rows.Err()
}

// addTaskPlaces links places to a task (creating unknown places); an
// existing link gets the new role/note when given.
func (s *Store) addTaskPlaces(ctx context.Context, tx *sql.Tx, taskID int64, refs []PlaceRef) error {
	for _, r := range refs {
		role := r.Role
		if role == "" {
			role = "touched"
		}
		if !slices.Contains(PlaceRoles, role) {
			return invalid("bad place role %q: use primary or touched", role)
		}
		p, err := s.ensurePlace(ctx, tx, r.Place)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO task_places (task_id, place_id, role, note) VALUES (?, ?, ?, ?)
ON CONFLICT (task_id, place_id) DO UPDATE SET role = excluded.role, note = CASE WHEN excluded.note != '' THEN excluded.note ELSE task_places.note END`,
			taskID, p.id, role, strings.TrimSpace(r.Note)); err != nil {
			return err
		}
	}
	return nil
}

func removeTaskPlaces(ctx context.Context, tx *sql.Tx, taskID int64, refs []string) error {
	for _, ref := range refs {
		_, key, err := ParsePlaceKey(ref)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM task_places WHERE task_id = ? AND place_id = (SELECT id FROM places WHERE key = ?)`, taskID, key); err != nil {
			return err
		}
	}
	return nil
}

// Machines lists machine names seen in place locations, most recent first.
func (s *Store) Machines(ctx context.Context) ([]string, error) {
	out := []string{}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT machine FROM place_locations GROUP BY machine ORDER BY max(seen_at) DESC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m string
			if err := rows.Scan(&m); err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}

// withLocations fills the locations of task places, all machines or one.
func withLocations(ctx context.Context, q queryer, tps []TaskPlace, machine string) error {
	for i := range tps {
		var err error
		if tps[i].Locations, err = locations(ctx, q, tps[i].id, machine); err != nil {
			return err
		}
	}
	return nil
}
