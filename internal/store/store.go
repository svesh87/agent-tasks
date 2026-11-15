// Package store keeps tasks, documents, revisions, plan items, review rounds,
// comments and the journal in a single SQLite file, and implements the
// operations of the contract on top of it. Both the MCP tools and the REST
// API call these methods.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

//go:embed migrations/*.sql
var migrations embed.FS

// TimeFormat is fixed-width so that stored timestamps sort as strings.
const TimeFormat = "2006-01-02T15:04:05.000000Z"

// Store is the database handle. It is safe for concurrent use: all access
// goes through one connection, one transaction at a time.
type Store struct {
	db      *sql.DB
	Now     func() time.Time // replaceable in tests
	changes notifier         // broadcast after every transaction that changed data
}

// Open opens (creating if needed) the database at path and applies pending
// migrations. Use ":memory:" for a throwaway database.
func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)&_txlock=immediate"
	if path != ":memory:" {
		dsn += "&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)
	s := &Store{db: db, Now: func() time.Time { return time.Now().UTC() }}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		version := strings.TrimSuffix(strings.TrimPrefix(name, "migrations/"), ".sql")
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE version = ?`, version).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		body, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		err = s.tx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, string(body)); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, version, s.ts())
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ts() string { return s.Now().UTC().Format(TimeFormat) }

// tx runs fn in a transaction, committing when it returns nil. If the
// transaction changed any rows, waiters on Changes are woken after the
// commit. Every write of the store goes through here, so new write methods
// are covered without extra code.
func (s *Store) tx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// total_changes() counts rows changed on this connection; the store
	// uses a single connection, so the difference is this transaction's.
	var before, after int64
	if err := tx.QueryRowContext(ctx, `SELECT total_changes()`).Scan(&before); err != nil {
		tx.Rollback()
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT total_changes()`).Scan(&after); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if after != before {
		s.changes.broadcast()
	}
	return nil
}

// Error kinds. Messages are written for agents: they say what is wrong and
// what to do.
var (
	ErrNotFound = errors.New("not found")
	ErrInvalid  = errors.New("invalid request")
	ErrConflict = errors.New("conflict")
)

type kindError struct {
	kind error
	msg  string
}

func (e *kindError) Error() string { return e.msg }
func (e *kindError) Unwrap() error { return e.kind }

func notFound(format string, a ...any) error {
	return &kindError{ErrNotFound, fmt.Sprintf(format, a...)}
}
func invalid(format string, a ...any) error {
	return &kindError{ErrInvalid, fmt.Sprintf(format, a...)}
}
func conflict(format string, a ...any) error {
	return &kindError{ErrConflict, fmt.Sprintf(format, a...)}
}

// TaskID formats a task number as T-<n>.
func TaskID(n int64) string { return "T-" + strconv.FormatInt(n, 10) }

// ParseTaskID accepts "T-12", "t-12" or "12".
func ParseTaskID(id string) (int64, error) {
	s := strings.TrimSpace(id)
	if len(s) > 2 && (s[0] == 'T' || s[0] == 't') && s[1] == '-' {
		s = s[2:]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, invalid("bad task id %q: expected T-<number>, e.g. T-12", id)
	}
	return n, nil
}

func nullStr(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func touchTask(ctx context.Context, tx *sql.Tx, taskID int64, now string) error {
	_, err := tx.ExecContext(ctx, `UPDATE tasks SET updated_at = ? WHERE id = ?`, now, taskID)
	return err
}
