package gap_test

import (
	"testing"

	"github.com/anthonybrice/editor/buffer/gap"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestNew_emptyText(t *testing.T) {
	b := gap.New(nil)
	if b.Len() != 0 {
		t.Fatalf("expected len 0, got %d", b.Len())
	}
}

func TestNew_withText(t *testing.T) {
	b := gap.New([]rune("hello"))
	if b.Len() != 5 {
		t.Fatalf("expected len 5, got %d", b.Len())
	}
	if b.String() != "hello" {
		t.Fatalf("expected 'hello', got %q", b.String())
	}
}

func TestInsert_atEnd(t *testing.T) {
	b := gap.New([]rune("hello"))
	b.MoveTo(5)
	b.Insert('!')
	if b.String() != "hello!" {
		t.Fatalf("got %q", b.String())
	}
}

func TestInsert_atStart(t *testing.T) {
	b := gap.New([]rune("ello"))
	b.MoveTo(0)
	b.Insert('h')
	if b.String() != "hello" {
		t.Fatalf("got %q", b.String())
	}
}

func TestInsert_inMiddle(t *testing.T) {
	b := gap.New([]rune("hllo"))
	b.MoveTo(1)
	b.Insert('e')
	if b.String() != "hello" {
		t.Fatalf("got %q", b.String())
	}
}

func TestInsertAt(t *testing.T) {
	b := gap.New([]rune("hllo"))
	b.InsertAt(1, 'e')
	if b.String() != "hello" {
		t.Fatalf("got %q", b.String())
	}
}

func TestInsertSlice(t *testing.T) {
	b := gap.New([]rune("hllo"))
	b.MoveTo(1)
	b.InsertSlice([]rune("e"))
	if b.String() != "hello" {
		t.Fatalf("got %q", b.String())
	}
}

func TestDeleteBefore(t *testing.T) {
	b := gap.New([]rune("hello"))
	b.MoveTo(5)
	b.DeleteBefore(1)
	if b.String() != "hell" {
		t.Fatalf("got %q", b.String())
	}
}

func TestDeleteBefore_clamp(t *testing.T) {
	b := gap.New([]rune("hi"))
	b.MoveTo(1)
	b.DeleteBefore(10) // more than available — should clamp
	if b.String() != "i" {
		t.Fatalf("got %q", b.String())
	}
}

func TestDeleteAfter(t *testing.T) {
	b := gap.New([]rune("hello"))
	b.MoveTo(0)
	b.DeleteAfter(1)
	if b.String() != "ello" {
		t.Fatalf("got %q", b.String())
	}
}

func TestDeleteAfter_clamp(t *testing.T) {
	b := gap.New([]rune("hi"))
	b.MoveTo(1)
	b.DeleteAfter(10) // more than available
	if b.String() != "h" {
		t.Fatalf("got %q", b.String())
	}
}

func TestDeleteRange(t *testing.T) {
	b := gap.New([]rune("hello world"))
	b.DeleteRange(5, 11) // delete " world"
	if b.String() != "hello" {
		t.Fatalf("got %q", b.String())
	}
}

func TestDeleteRange_noop(t *testing.T) {
	b := gap.New([]rune("hello"))
	b.DeleteRange(2, 2) // empty range
	if b.String() != "hello" {
		t.Fatalf("got %q", b.String())
	}
}

func TestMoveTo_backAndForth(t *testing.T) {
	b := gap.New([]rune("abcde"))
	b.MoveTo(3)
	b.Insert('X')
	// "abcXde"
	b.MoveTo(1)
	b.Insert('Y')
	// "aYbcXde"
	if b.String() != "aYbcXde" {
		t.Fatalf("got %q", b.String())
	}
}

func TestRune(t *testing.T) {
	b := gap.New([]rune("hello"))
	for i, want := range []rune("hello") {
		if got := b.Rune(i); got != want {
			t.Errorf("Rune(%d) = %c, want %c", i, got, want)
		}
	}
}

func TestSlice(t *testing.T) {
	b := gap.New([]rune("hello world"))
	s := b.Slice(6, 11)
	if string(s) != "world" {
		t.Fatalf("got %q", string(s))
	}
}

func TestText(t *testing.T) {
	b := gap.New([]rune("hello"))
	text := b.Text()
	if string(text) != "hello" {
		t.Fatalf("got %q", string(text))
	}
}

func TestGrow_manyInserts(t *testing.T) {
	// Insert enough runes to force multiple internal re-allocations.
	b := gap.New(nil)
	b.MoveTo(0)
	const n = 1000
	for i := range n {
		b.Insert(rune('a' + i%26))
	}
	if b.Len() != n {
		t.Fatalf("expected %d, got %d", n, b.Len())
	}
}
