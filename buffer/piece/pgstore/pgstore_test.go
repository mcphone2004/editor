package pgstore_test

import (
	"os"
	"testing"

	"github.com/anthonybrice/editor/buffer/piece"
	"github.com/anthonybrice/editor/buffer/piece/pgstore"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func dsn(t *testing.T) string {
	t.Helper()
	d := os.Getenv("EDITOR_TEST_DSN")
	if d == "" {
		t.Skip("EDITOR_TEST_DSN not set — skipping Postgres tests")
	}
	return d
}

func snap(addLen int) piece.Snapshot {
	return piece.Snapshot{AddLen: addLen}
}

func openStore(t *testing.T) *pgstore.PgStore {
	t.Helper()
	s, err := pgstore.Open(dsn(t), t.Name(), snap(0))
	if err != nil {
		t.Skipf("cannot connect to Postgres: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestNew_currentIsInitial(t *testing.T) {
	s := openStore(t)
	if got := s.Current(); got.AddLen != 0 {
		t.Fatalf("Current().AddLen = %d, want 0", got.AddLen)
	}
}

func TestPush_advancesStack(t *testing.T) {
	s := openStore(t)
	if err := s.Push(snap(1)); err != nil {
		t.Fatal(err)
	}
	if got := s.Current(); got.AddLen != 1 {
		t.Fatalf("Current().AddLen = %d, want 1", got.AddLen)
	}
}

func TestUndo_restoresPrevious(t *testing.T) {
	s := openStore(t)
	_ = s.Push(snap(1))
	got, ok := s.Undo()
	if !ok {
		t.Fatal("Undo returned ok=false")
	}
	if got.AddLen != 0 {
		t.Fatalf("after Undo: AddLen = %d, want 0", got.AddLen)
	}
}

func TestUndo_atOldest_returnsFalse(t *testing.T) {
	s := openStore(t)
	_, ok := s.Undo()
	if ok {
		t.Fatal("Undo should return false at oldest state")
	}
}

func TestRedo_reappliesChange(t *testing.T) {
	s := openStore(t)
	_ = s.Push(snap(1))
	s.Undo()
	got, ok := s.Redo()
	if !ok {
		t.Fatal("Redo returned ok=false")
	}
	if got.AddLen != 1 {
		t.Fatalf("after Redo: AddLen = %d, want 1", got.AddLen)
	}
}

func TestRedo_atNewest_returnsFalse(t *testing.T) {
	s := openStore(t)
	_, ok := s.Redo()
	if ok {
		t.Fatal("Redo should return false at newest state")
	}
}

func TestPush_discardsRedoBranch(t *testing.T) {
	s := openStore(t)
	_ = s.Push(snap(1))
	_ = s.Push(snap(2))
	s.Undo()
	_ = s.Push(snap(3))
	_, ok := s.Redo()
	if ok {
		t.Fatal("Redo should return false after pushing on an undo branch")
	}
	if got := s.Current(); got.AddLen != 3 {
		t.Fatalf("Current().AddLen = %d, want 3", got.AddLen)
	}
}

func TestClose_releasesConnection(t *testing.T) {
	s := openStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}
	// Prevent double-close in cleanup.
	t.Cleanup(func() {})
}
