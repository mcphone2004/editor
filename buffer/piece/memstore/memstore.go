// Package memstore implements an in-memory UndoStore.
//
// MemStore provides full undo/redo within a single editor session.
// History is lost when the editor exits; use pgstore for persistence.
package memstore

import "github.com/anthonybrice/editor/buffer/piece"

// MemStore is an in-memory undo/redo stack backed by a slice of Snapshots.
type MemStore struct {
	entries  []piece.Snapshot
	stackTop int
}

// New returns a MemStore seeded with the initial document snapshot.
func New(initial piece.Snapshot) *MemStore {
	return &MemStore{entries: []piece.Snapshot{initial}, stackTop: 0}
}

// Push records a new snapshot, discarding any redo entries above the current position.
func (s *MemStore) Push(snap piece.Snapshot) error {
	s.entries = append(s.entries[:s.stackTop+1], snap)
	s.stackTop = len(s.entries) - 1
	return nil
}

// Undo moves one step back. Returns (zero, false) if already at the oldest state.
func (s *MemStore) Undo() (piece.Snapshot, bool) {
	if s.stackTop <= 0 {
		return piece.Snapshot{}, false
	}
	s.stackTop--
	return s.entries[s.stackTop], true
}

// Redo moves one step forward. Returns (zero, false) if already at the newest state.
func (s *MemStore) Redo() (piece.Snapshot, bool) {
	if s.stackTop+1 >= len(s.entries) {
		return piece.Snapshot{}, false
	}
	s.stackTop++
	return s.entries[s.stackTop], true
}

// Current returns the snapshot at the current stack position.
func (s *MemStore) Current() piece.Snapshot {
	return s.entries[s.stackTop]
}

// CurrentIndex returns the stack position of the current snapshot.
func (s *MemStore) CurrentIndex() int { return s.stackTop }

// Close is a no-op; satisfies piece.UndoStore.
func (s *MemStore) Close() error { return nil }
