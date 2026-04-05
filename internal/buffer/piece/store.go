// Package piece — store.go
//
// PgStore persists piece table undo/redo stacks to Postgres so that undo
// history survives editor restarts and crashes.
//
// Schema (auto-created on first use):
//
//	editor_files        — one row per open file path
//	editor_undo_entries — one row per undo state (piece list + add-buffer)
//
// Each "entry" stores a full Snapshot.  For typical source files the
// add-buffer grows slowly so the storage overhead is small.
package piece

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// UndoStore is the interface for a persistent undo/redo log.
type UndoStore interface {
	Push(snap Snapshot) error
	Undo() (Snapshot, bool)
	Redo() (Snapshot, bool)
	Current() Snapshot
	Close() error
}

// PgStore is a Postgres-backed undo/redo log for a single file.
type PgStore struct {
	db     *sql.DB
	fileID int64

	// In-memory stack: entries[0..stackTop] are the undo stack.
	// entries[stackTop+1..] are redo entries (overwritten on new edit).
	entries  []pgEntry
	stackTop int // index of current state (-1 = before any edit)
}

type pgEntry struct {
	id       int64
	snapshot Snapshot
}

// OpenPgStore connects to postgres at dsn, creates tables if needed, and
// returns a PgStore keyed by filePath.
//
// dsn format: "host=localhost user=postgres dbname=editor sslmode=disable".
func OpenPgStore(dsn, filePath string, initial Snapshot) (*PgStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("pgstore: open: %w", err)
	}

	// closeDB is called on every error path to prevent goroutine leaks from
	// the sql.DB connection pool background goroutine.
	closeDB := func() { _ = db.Close() }

	if err := db.PingContext(context.Background()); err != nil {
		closeDB()
		return nil, fmt.Errorf("pgstore: ping: %w", err)
	}
	if err := migrate(db); err != nil {
		closeDB()
		return nil, fmt.Errorf("pgstore: migrate: %w", err)
	}

	fileID, err := upsertFile(db, filePath)
	if err != nil {
		closeDB()
		return nil, fmt.Errorf("pgstore: upsert file: %w", err)
	}

	s := &PgStore{db: db, fileID: fileID, stackTop: -1}

	// Load existing history from the DB.
	if err := s.load(); err != nil {
		closeDB()
		return nil, fmt.Errorf("pgstore: load: %w", err)
	}

	// If no history yet, record the initial state.
	if len(s.entries) == 0 {
		if err := s.push(initial); err != nil {
			closeDB()
			return nil, err
		}
		s.stackTop = 0
	}

	return s, nil
}

// Push records a new snapshot (after an edit). Any redo entries above the
// current position are discarded.
func (s *PgStore) Push(snap Snapshot) error {
	// Discard redo branch.
	if s.stackTop+1 < len(s.entries) {
		ids := make([]int64, 0, len(s.entries)-s.stackTop-1)
		for _, e := range s.entries[s.stackTop+1:] {
			ids = append(ids, e.id)
		}
		s.entries = s.entries[:s.stackTop+1]
		if err := s.deleteEntries(ids); err != nil {
			return err
		}
	}
	return s.push(snap)
}

// Undo moves one step back in history and returns the previous snapshot.
// Returns (zero, false) if already at the oldest state.
func (s *PgStore) Undo() (Snapshot, bool) {
	if s.stackTop <= 0 {
		return Snapshot{}, false
	}
	s.stackTop--
	return s.entries[s.stackTop].snapshot, true
}

// Redo moves one step forward and returns the next snapshot.
// Returns (zero, false) if already at the newest state.
func (s *PgStore) Redo() (Snapshot, bool) {
	if s.stackTop+1 >= len(s.entries) {
		return Snapshot{}, false
	}
	s.stackTop++
	return s.entries[s.stackTop].snapshot, true
}

// Current returns the snapshot at the current stack position.
func (s *PgStore) Current() Snapshot {
	if s.stackTop < 0 || s.stackTop >= len(s.entries) {
		return Snapshot{}
	}
	return s.entries[s.stackTop].snapshot
}

// Close releases the database connection.
func (s *PgStore) Close() error { return s.db.Close() }

// --- internal ---

func (s *PgStore) push(snap Snapshot) error {
	data, err := marshalSnapshot(snap)
	if err != nil {
		return err
	}
	var id int64
	err = s.db.QueryRowContext(context.Background(),
		`INSERT INTO editor_undo_entries (file_id, sequence, snapshot, created_at)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		s.fileID,
		len(s.entries),
		data,
		time.Now(),
	).Scan(&id)
	if err != nil {
		return fmt.Errorf("pgstore: push: %w", err)
	}
	s.entries = append(s.entries, pgEntry{id: id, snapshot: snap})
	s.stackTop = len(s.entries) - 1
	return nil
}

func (s *PgStore) load() error {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT id, snapshot FROM editor_undo_entries
		 WHERE file_id = $1 ORDER BY sequence ASC`,
		s.fileID,
	)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	for rows.Next() {
		var id int64
		var data []byte
		if err := rows.Scan(&id, &data); err != nil {
			return err
		}
		snap, err := unmarshalSnapshot(data)
		if err != nil {
			return err
		}
		s.entries = append(s.entries, pgEntry{id: id, snapshot: snap})
	}
	if len(s.entries) > 0 {
		s.stackTop = len(s.entries) - 1
	}
	return rows.Err()
}

func (s *PgStore) deleteEntries(ids []int64) error {
	for _, id := range ids {
		if _, err := s.db.ExecContext(context.Background(), `DELETE FROM editor_undo_entries WHERE id = $1`, id); err != nil {
			return fmt.Errorf("pgstore: delete entry %d: %w", id, err)
		}
	}
	return nil
}

// --- serialisation ---

type snapshotJSON struct {
	Pieces    []Piece `json:"pieces"`
	AddLen    int     `json:"add_len"`
	CursorRow int     `json:"cursor_row,omitempty"`
	CursorCol int     `json:"cursor_col,omitempty"`
}

func marshalSnapshot(s Snapshot) ([]byte, error) {
	return json.Marshal(snapshotJSON(s))
}

func unmarshalSnapshot(data []byte) (Snapshot, error) {
	var v snapshotJSON
	if err := json.Unmarshal(data, &v); err != nil {
		return Snapshot{}, err
	}
	return Snapshot(v), nil
}

// --- schema ---

func migrate(db *sql.DB) error {
	_, err := db.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS editor_files (
			id         BIGSERIAL PRIMARY KEY,
			path       TEXT NOT NULL,
			opened_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (path)
		);

		CREATE TABLE IF NOT EXISTS editor_undo_entries (
			id         BIGSERIAL PRIMARY KEY,
			file_id    BIGINT NOT NULL REFERENCES editor_files(id) ON DELETE CASCADE,
			sequence   INT NOT NULL,
			snapshot   JSONB NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_undo_file_seq
			ON editor_undo_entries (file_id, sequence);
	`)
	return err
}

func upsertFile(db *sql.DB, path string) (int64, error) {
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO editor_files (path) VALUES ($1) ON CONFLICT (path) DO NOTHING`,
		path,
	)
	if err != nil {
		return 0, err
	}
	var id int64
	err = db.QueryRowContext(context.Background(), `SELECT id FROM editor_files WHERE path = $1`, path).Scan(&id)
	return id, err
}
