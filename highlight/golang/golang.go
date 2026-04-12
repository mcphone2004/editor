// Package golang implements Go syntax highlighting using go/scanner from the
// standard library.  No external dependencies are required.
package golang

import (
	"go/scanner"
	"go/token"
	"strings"
	"unicode/utf8"

	"github.com/anthonybrice/editor/highlight"
)

// Highlighter implements highlight.Highlighter for Go source files.
type Highlighter struct{}

// New returns a new Go Highlighter.
func New() *Highlighter { return &Highlighter{} }

// Highlight tokenizes src as Go source and returns per-line highlight data for
// lines [visStart, visEnd] (0-indexed, inclusive).  The full source is scanned
// from the beginning so that multi-line tokens (raw strings, block comments)
// are classified correctly even when they start before the visible window.
func (h *Highlighter) Highlight(src string, visStart, visEnd int) map[int]highlight.LineHL {
	allLines := strings.Split(src, "\n")

	// Pre-allocate LineHL slices only for visible rows.
	out := make(map[int]highlight.LineHL, visEnd-visStart+1)
	for r := visStart; r <= visEnd && r < len(allLines); r++ {
		out[r] = make(highlight.LineHL, utf8.RuneCountInString(allLines[r]))
	}

	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))

	var s scanner.Scanner
	// Suppress scan errors — partial or syntactically invalid Go is still
	// worth highlighting on a best-effort basis.
	s.Init(file, []byte(src), func(token.Position, string) {}, scanner.ScanComments)

	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		kind, ok := kindOf(tok)
		if !ok {
			continue
		}

		// Use the literal text when available (strings, comments, numbers);
		// fall back to the token's canonical spelling for keywords.
		text := lit
		if text == "" {
			text = tok.String()
		}

		position := fset.Position(pos)
		startRow := position.Line - 1       // 0-indexed
		startByteCol := position.Column - 1 //nolint:mnd // 0-indexed byte offset within its line

		// A single token can span multiple lines (raw strings, block comments).
		for i, part := range strings.Split(text, "\n") {
			row := startRow + i
			if row < visStart || row > visEnd {
				continue
			}
			hl, found := out[row]
			if !found || len(hl) == 0 {
				continue
			}

			// Continuation lines always start at rune column 0.
			// The first line uses startByteCol converted to a rune offset.
			runeCol := 0
			if i == 0 && row < len(allLines) {
				off := min(startByteCol, len(allLines[row]))
				runeCol = utf8.RuneCountInString(allLines[row][:off])
			}

			end := min(runeCol+utf8.RuneCountInString(part), len(hl))
			for c := runeCol; c < end; c++ {
				hl[c] = kind
			}
		}
	}
	return out
}

// kindOf maps a Go token to a highlight.Kind.
// Returns (KindNone, false) for tokens that should not be highlighted
// (identifiers, operators, punctuation).
func kindOf(tok token.Token) (highlight.Kind, bool) {
	switch {
	case tok.IsKeyword():
		return highlight.KindKeyword, true
	case tok == token.INT || tok == token.FLOAT || tok == token.IMAG:
		return highlight.KindNumber, true
	case tok == token.CHAR || tok == token.STRING:
		return highlight.KindString, true
	case tok == token.COMMENT:
		return highlight.KindComment, true
	default:
		return highlight.KindNone, false
	}
}
