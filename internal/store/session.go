package store

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
)

// NormalizeRepo turns a git remote into host/path: no scheme, no credentials,
// no port, no ".git", host lower-cased. "git@github.com:a/b.git",
// "https://user:pw@GitHub.com:443/a/b" and "ssh://git@github.com:22/a/b.git"
// all give "github.com/a/b". A legacy "repo:" prefix is dropped.
func NormalizeRepo(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "repo:")
	if s == "" {
		return ""
	}
	scheme := false
	if i := strings.Index(s, "://"); i >= 0 {
		s, scheme = s[i+3:], true
	}
	// drop credentials (user@ or user:pass@) before the first '/'
	if at := strings.LastIndex(s[:strings.Index(s+"/", "/")], "@"); at >= 0 {
		s = s[at+1:]
	}
	if !scheme {
		// scp-like host:path
		if c, sl := strings.Index(s, ":"), strings.Index(s+"/", "/"); c >= 0 && c < sl {
			s = s[:c] + "/" + strings.TrimPrefix(s[c+1:], "/")
		}
	}
	s = strings.TrimSuffix(strings.TrimSuffix(s, "/"), ".git")
	host, path, _ := strings.Cut(s, "/")
	host, _, _ = strings.Cut(host, ":") // drop a port
	if path == "" {
		return strings.ToLower(host)
	}
	return strings.ToLower(host) + "/" + path
}

// OtherPlace is another place of a session task, with its path on the
// caller's machine ("" when not present there).
type OtherPlace struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Role  string `json:"role"`
	Path  string `json:"path,omitempty"`
}

// SessionTask is a task shown to an agent at session start.
type SessionTask struct {
	Task
	PendingRounds []RoundBrief `json:"pending_rounds"`
	OtherPlaces   []OtherPlace `json:"other_places"`
}

// SessionInfo is what the session hook shows.
type SessionInfo struct {
	Place *Place        `json:"place,omitempty"`
	Path  string        `json:"path,omitempty"`
	Tasks []SessionTask `json:"tasks"`
	Todos []Todo        `json:"todos"`
}

var taskDirRe = regexp.MustCompile(`^[Tt]-\d+$`)

// SessionContext resolves the caller's place (git remote, or cwd on machine),
// registers the location, and returns the place's non-archived tasks, its
// open todos, and the task whose workspace folder (a T-<n> path component)
// contains cwd.
func (s *Store) SessionContext(ctx context.Context, repo, cwd, machine string) (SessionInfo, error) {
	var cwdTask int64
	for _, part := range strings.Split(strings.ReplaceAll(cwd, `\`, "/"), "/") {
		if taskDirRe.MatchString(part) {
			cwdTask, _ = ParseTaskID(part)
		}
	}
	info := SessionInfo{Tasks: []SessionTask{}, Todos: []Todo{}}
	machine = strings.TrimSpace(machine)
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var tasks []Task
		seen := map[int64]bool{}
		if NormalizeRepo(repo) != "" || (cwd != "" && machine != "") {
			p, loc, _, err := s.resolveHere(ctx, tx, cwd, repo, machine)
			if err != nil {
				return err
			}
			info.Place, info.Path = &p, loc
			if tasks, err = placeTasks(ctx, tx, p.id); err != nil {
				return err
			}
			if info.Todos, err = listTodos(ctx, tx, todoFilter{placeID: p.id, states: []string{"open"}, limit: 20}); err != nil {
				return err
			}
		}
		for _, t := range tasks {
			seen[t.num] = true
		}
		if cwdTask != 0 && !seen[cwdTask] {
			if t, err := getTask(ctx, tx, cwdTask); err == nil {
				tasks = append(tasks, t)
			}
		}
		for _, t := range tasks {
			st := SessionTask{Task: t, OtherPlaces: []OtherPlace{}}
			for _, tp := range t.Places {
				if info.Place != nil && tp.Key == info.Place.Key {
					continue
				}
				op := OtherPlace{Key: tp.Key, Label: tp.Label, Role: tp.Role}
				if machine != "" {
					locs, err := locations(ctx, tx, tp.id, machine)
					if err != nil {
						return err
					}
					if len(locs) > 0 {
						op.Path = locs[0].Path
					}
				}
				st.OtherPlaces = append(st.OtherPlaces, op)
			}
			var err error
			if st.PendingRounds, err = roundBriefs(ctx, tx, t.num, "submitted"); err != nil {
				return err
			}
			info.Tasks = append(info.Tasks, st)
		}
		return nil
	})
	return info, err
}
