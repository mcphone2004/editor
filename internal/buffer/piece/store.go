package piece

// UndoStore is the interface for a persistent undo/redo log.
type UndoStore interface {
	Push(snap Snapshot) error
	Undo() (Snapshot, bool)
	Redo() (Snapshot, bool)
	Current() Snapshot
	Close() error
}
