package piece

// UndoStore is the interface for a persistent undo/redo log.
type UndoStore interface {
	Push(snap Snapshot) error
	Undo() (Snapshot, bool)
	Redo() (Snapshot, bool)
	Current() Snapshot
	// CurrentIndex returns the stack position of the current snapshot.
	// fileBuffer records this at save time to detect when undo/redo
	// returns the buffer to its last-saved state.
	CurrentIndex() int
	Close() error
}
