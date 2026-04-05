package memstore_test

import (
	"testing"

	"github.com/anthonybrice/editor/internal/buffer/piece"
	"github.com/anthonybrice/editor/internal/buffer/piece/memstore"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func snap(addLen int) piece.Snapshot {
	return piece.Snapshot{AddLen: addLen}
}

func TestNew_currentIsInitial(t *testing.T) {
	s := memstore.New(snap(0))
	if got := s.Current(); got.AddLen != 0 {
		t.Fatalf("Current().AddLen = %d, want 0", got.AddLen)
	}
}

func TestPush_advancesStack(t *testing.T) {
	s := memstore.New(snap(0))
	if err := s.Push(snap(1)); err != nil {
		t.Fatal(err)
	}
	if got := s.Current(); got.AddLen != 1 {
		t.Fatalf("Current().AddLen = %d, want 1", got.AddLen)
	}
}

func TestUndo_restoresPrevious(t *testing.T) {
	s := memstore.New(snap(0))
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
	s := memstore.New(snap(0))
	_, ok := s.Undo()
	if ok {
		t.Fatal("Undo should return false at oldest state")
	}
}

func TestRedo_reappliesChange(t *testing.T) {
	s := memstore.New(snap(0))
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
	s := memstore.New(snap(0))
	_, ok := s.Redo()
	if ok {
		t.Fatal("Redo should return false at newest state")
	}
}

func TestPush_discardsRedoBranch(t *testing.T) {
	s := memstore.New(snap(0))
	_ = s.Push(snap(1))
	_ = s.Push(snap(2))
	s.Undo()
	// Now push a diverging edit — snap(3) should replace snap(2) in history.
	_ = s.Push(snap(3))
	_, ok := s.Redo()
	if ok {
		t.Fatal("Redo should return false after pushing on an undo branch")
	}
	if got := s.Current(); got.AddLen != 3 {
		t.Fatalf("Current().AddLen = %d, want 3", got.AddLen)
	}
}

func TestClose_isNoOp(t *testing.T) {
	s := memstore.New(snap(0))
	if err := s.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}
}
