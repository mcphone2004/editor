package lsp_test

import (
	"context"
	"testing"
	"time"

	"github.com/anthonybrice/editor/lsp"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// TestClientExited verifies that Client.exited (via Session.Exited) is closed
// when the subprocess exits.
func TestClientExited(t *testing.T) {
	// "true" exits immediately with no output, causing readLoop to get EOF and exit.
	c, err := lsp.Start(context.Background(), "true")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(context.Background()) }()

	select {
	case <-c.Exited():
		// pass
	case <-time.After(2 * time.Second):
		t.Fatal("exited channel was not closed after subprocess exit")
	}
}
