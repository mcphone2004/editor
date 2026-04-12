// Package highlight defines the interface and shared types for syntax
// highlighting.  Language-specific implementations live in sub-packages
// (e.g. highlight/golang).
package highlight

// Kind identifies the syntactic role of a character.
// The zero value KindNone means the character carries no special highlight.
type Kind uint8

// Kind constants for the syntactic roles that highlighters can assign.
const (
	KindNone    Kind = 0
	KindKeyword Kind = 1
	KindString  Kind = 2
	KindNumber  Kind = 3
	KindComment Kind = 4
)

// LineHL holds one Kind per rune-column for a single source line.
type LineHL []Kind

// Highlighter computes per-character syntax highlight information.
type Highlighter interface {
	// Highlight tokenizes src and returns per-line highlight data for lines
	// [visStart, visEnd] (both 0-indexed, inclusive).  src is the full
	// document text; lines outside the visible range are not included in the
	// result.
	Highlight(src string, visStart, visEnd int) map[int]LineHL
}
