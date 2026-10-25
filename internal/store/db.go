// Package store is the SQLite persistence layer.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite" // pure Go driver, builds for windows/386
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Store wraps the database handle.
type Store struct {
	DB   *sql.DB
	Path string
}

// Open opens (or creates) the database, checks its integrity and applies pending migrations.
// beforeMigrate is called before any pending migration runs (used to take a safety backup).
func Open(path string, beforeMigrate func() error) (*Store, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=synchronous(FULL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection: a desktop app with a single user; this serialises writes and avoids SQLITE_BUSY.
	db.SetMaxOpenConns(1)
	s := &Store{DB: db, Path: path}
	if err := s.Integrity(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.migrate(beforeMigrate); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.DB.Close() }

// Integrity runs PRAGMA integrity_check.
func (s *Store) Integrity() error {
	var res string
	if err := s.DB.QueryRow(`PRAGMA integrity_check`).Scan(&res); err != nil {
		return fmt.Errorf("contrôle d'intégrité impossible : %w", err)
	}
	if res != "ok" {
		return fmt.Errorf("base de données endommagée : %s", res)
	}
	return nil
}

func (s *Store) migrate(before func() error) error {
	if _, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT (datetime('now','localtime')))`); err != nil {
		return err
	}
	applied := map[int]bool{}
	rows, err := s.DB.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v int
		rows.Scan(&v)
		applied[v] = true
	}
	rows.Close()
	entries, _ := fs.ReadDir(migrationFS, "migrations")
	type mig struct {
		v    int
		name string
	}
	var pending []mig
	for _, e := range entries {
		v, err := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration mal nommée : %s", e.Name())
		}
		if !applied[v] {
			pending = append(pending, mig{v, e.Name()})
		}
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].v < pending[j].v })
	if len(pending) > 0 && len(applied) > 0 && before != nil {
		if err := before(); err != nil {
			return fmt.Errorf("sauvegarde avant mise à jour impossible : %w", err)
		}
	}
	for _, m := range pending {
		body, _ := migrationFS.ReadFile("migrations/" + m.name)
		err := s.Tx(context.Background(), func(tx *sql.Tx) error {
			if _, err := tx.Exec(string(body)); err != nil {
				return fmt.Errorf("migration %s : %w", m.name, err)
			}
			_, err := tx.Exec(`INSERT INTO schema_migrations(version) VALUES (?)`, m.v)
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Tx runs fn in a transaction, committing on success.
func (s *Store) Tx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// BackupTo writes a consistent copy of the database to dst.
func (s *Store) BackupTo(dst string) error {
	_, err := s.DB.Exec(`VACUUM INTO ?`, dst)
	return err
}

// Counts returns the number of customers, quotes and invoices (used to detect a reset).
func (s *Store) Counts() (customers, quotes, invoices int, err error) {
	err = s.DB.QueryRow(`SELECT (SELECT COUNT(*) FROM customers), (SELECT COUNT(*) FROM quotes), (SELECT COUNT(*) FROM invoices)`).
		Scan(&customers, &quotes, &invoices)
	return
}

// Signature changes whenever the content changes (every write goes through the audit log).
func (s *Store) Signature() (string, error) {
	var a, t, sz int64
	err := s.DB.QueryRow(`SELECT COALESCE(MAX(id),0), (SELECT COUNT(*) FROM description_templates),
		(SELECT COALESCE(SUM(usage_count),0) FROM description_templates) FROM audit_log`).Scan(&a, &t, &sz)
	c, q, i, err2 := s.Counts()
	if err == nil {
		err = err2
	}
	return fmt.Sprintf("%d/%d/%d/%d/%d/%d", a, t, sz, c, q, i), err
}

func audit(tx *sql.Tx, entity string, id int64, action, details string) error {
	_, err := tx.Exec(`INSERT INTO audit_log(entity, entity_id, action, details) VALUES (?,?,?,?)`, entity, id, action, details)
	return err
}

// queryer is satisfied by *sql.DB and *sql.Tx.
type queryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
	Exec(query string, args ...any) (sql.Result, error)
}
