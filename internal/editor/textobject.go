package editor

// TextObject resolves an inclusive range [start, end) within the buffer
// relative to the current cursor. inner=true means "inner" (i), inner=false
// means "around" (a).
type TextObject func(e *Editor, inner bool) (r1, c1, r2, c2 int, linewise bool, ok bool)

var textObjects = map[rune]TextObject{
	'w': toWord,
	'W': toWordBig,
	'"': toQuote('"'),
	'\'': toQuote('\''),
	'`': toQuote('`'),
	'(': toPair('(', ')'),
	')': toPair('(', ')'),
	'{': toPair('{', '}'),
	'}': toPair('{', '}'),
	'[': toPair('[', ']'),
	']': toPair('[', ']'),
	'<': toPair('<', '>'),
	'>': toPair('<', '>'),
	'p': toParagraph,
}

func toWord(e *Editor, inner bool) (r1, c1, r2, c2 int, linewise bool, ok bool) {
	row := e.cursor.Row
	line := []rune(e.buf.Line(row))
	col := e.cursor.Col
	if col >= len(line) {
		return
	}
	isW := wordClass(false)
	class := isW(line[col])
	// Expand left.
	start := col
	for start > 0 && isW(line[start-1]) == class {
		start--
	}
	// Expand right.
	end := col
	for end < len(line)-1 && isW(line[end+1]) == class {
		end++
	}
	end++ // exclusive
	if !inner {
		// "around": also consume trailing (or leading) whitespace.
		if end < len(line) {
			for end < len(line) && isWord(line[end]) == 0 {
				end++
			}
		} else {
			for start > 0 && isWord(line[start-1]) == 0 {
				start--
			}
		}
	}
	return row, start, row, end, false, true
}

func toWordBig(e *Editor, inner bool) (r1, c1, r2, c2 int, linewise bool, ok bool) {
	row := e.cursor.Row
	line := []rune(e.buf.Line(row))
	col := e.cursor.Col
	if col >= len(line) {
		return
	}
	isW := wordClass(true)
	class := isW(line[col])
	start := col
	for start > 0 && isW(line[start-1]) == class {
		start--
	}
	end := col
	for end < len(line)-1 && isW(line[end+1]) == class {
		end++
	}
	end++
	return row, start, row, end, false, true
}

func toQuote(q rune) TextObject {
	return func(e *Editor, inner bool) (r1, c1, r2, c2 int, linewise bool, ok bool) {
		row := e.cursor.Row
		line := []rune(e.buf.Line(row))
		col := e.cursor.Col

		// Find enclosing quotes on the same line.
		left := -1
		for i := col - 1; i >= 0; i-- {
			if line[i] == q {
				left = i
				break
			}
		}
		if left < 0 {
			// Try from start.
			for i := 0; i < col; i++ {
				if line[i] == q {
					left = i
					break
				}
			}
		}
		if left < 0 {
			return
		}
		right := -1
		for i := left + 1; i < len(line); i++ {
			if line[i] == q {
				right = i
				break
			}
		}
		if right < 0 {
			return
		}
		if inner {
			return row, left + 1, row, right, false, true
		}
		return row, left, row, right + 1, false, true
	}
}

func toPair(open, close rune) TextObject {
	return func(e *Editor, inner bool) (r1, c1, r2, c2 int, linewise bool, ok bool) {
		row, col := e.cursor.Row, e.cursor.Col
		// Search backwards for open.
		depth := 0
		lr, lc := -1, -1
	outer:
		for r := row; r >= 0; r-- {
			line := []rune(e.buf.Line(r))
			startC := len(line) - 1
			if r == row {
				startC = col
			}
			for c := startC; c >= 0; c-- {
				ch := line[c]
				if ch == close {
					depth++
				} else if ch == open {
					if depth == 0 {
						lr, lc = r, c
						break outer
					}
					depth--
				}
			}
		}
		if lr < 0 {
			return
		}
		// Search forwards for matching close.
		depth = 0
		rr, rc := -1, -1
	outer2:
		for r := lr; r < e.buf.LineCount(); r++ {
			line := []rune(e.buf.Line(r))
			startC := 0
			if r == lr {
				startC = lc
			}
			for c := startC; c < len(line); c++ {
				ch := line[c]
				if ch == open {
					depth++
				} else if ch == close {
					depth--
					if depth == 0 {
						rr, rc = r, c
						break outer2
					}
				}
			}
		}
		if rr < 0 {
			return
		}
		if inner {
			// Contents between the delimiters.
			return lr, lc + 1, rr, rc, false, true
		}
		return lr, lc, rr, rc + 1, false, true
	}
}

func toParagraph(e *Editor, inner bool) (r1, c1, r2, c2 int, linewise bool, ok bool) {
	row := e.cursor.Row
	start := row
	for start > 0 && e.buf.Line(start-1) != "" {
		start--
	}
	end := row
	for end < e.buf.LineCount()-1 && e.buf.Line(end+1) != "" {
		end++
	}
	return start, 0, end + 1, 0, true, true
}

func isWord(r rune) int {
	return wordClass(false)(r)
}
