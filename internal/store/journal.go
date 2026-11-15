package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
)

// JournalKinds are the allowed journal entry kinds.
var JournalKinds = []string{"context", "log", "decision"}

// JournalEntry is one journal record of a task.
type JournalEntry struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`
	Body      string `json:"body"`
	Source    string `json:"source,omitempty"`
	CreatedAt string `json:"created_at"`
}

// AppendJournal adds a journal entry to a task.
func (s *Store) AppendJournal(ctx context.Context, task, kind, body, source string) (JournalEntry, error) {
	n, err := ParseTaskID(task)
	if err != nil {
		return JournalEntry{}, err
	}
	if !slices.Contains(JournalKinds, kind) {
		return JournalEntry{}, invalid("bad journal kind %q: use context, log or decision", kind)
	}
	if strings.TrimSpace(body) == "" {
		return JournalEntry{}, invalid("journal body is empty")
	}
	var e JournalEntry
	err = s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getTask(ctx, tx, n); err != nil {
			return err
		}
		now := s.ts()
		id, err := s.insertJournal(ctx, tx, n, kind, body, source, now)
		e = JournalEntry{ID: id, Kind: kind, Body: body, Source: source, CreatedAt: now}
		return err
	})
	return e, err
}

// journalTx adds a journal entry inside an open transaction.
func (s *Store) journalTx(ctx context.Context, tx *sql.Tx, taskID int64, kind, body, source, now string) error {
	_, err := s.insertJournal(ctx, tx, taskID, kind, body, source, now)
	return err
}

func (s *Store) insertJournal(ctx context.Context, tx *sql.Tx, taskID int64, kind, body, source, now string) (int64, error) {
	res, err := tx.ExecContext(ctx, `INSERT INTO journal (task_id, kind, body, source, created_at) VALUES (?, ?, ?, ?, ?)`, taskID, kind, body, source, now)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if err := indexRow(ctx, tx, taskID, fmt.Sprintf("journal:%d", id), body); err != nil {
		return 0, err
	}
	return id, touchTask(ctx, tx, taskID, now)
}

// Journal returns the latest entries of a task, newest first. kind "" means
// all kinds; limit <= 0 means 20.
func (s *Store) Journal(ctx context.Context, task, kind string, limit int) ([]JournalEntry, error) {
	n, err := ParseTaskID(task)
	if err != nil {
		return nil, err
	}
	if kind != "" && !slices.Contains(JournalKinds, kind) {
		return nil, invalid("bad journal kind %q: use context, log or decision", kind)
	}
	var out []JournalEntry
	err = s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getTask(ctx, tx, n); err != nil {
			return err
		}
		out, err = journal(ctx, tx, n, kind, limit)
		return err
	})
	return out, err
}

func journal(ctx context.Context, q queryer, taskID int64, kind string, limit int) ([]JournalEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	query := `SELECT id, kind, body, source, created_at FROM journal WHERE task_id = ?`
	args := []any{taskID}
	if kind != "" {
		query += ` AND kind = ?`
		args = append(args, kind)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JournalEntry{}
	for rows.Next() {
		var e JournalEntry
		if err := rows.Scan(&e.ID, &e.Kind, &e.Body, &e.Source, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
