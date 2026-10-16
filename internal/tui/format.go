package tui

import (
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// toggleMark wraps the word under the cursor in mark (* for bold, _ for
// italic), or unwraps it when it already is. With no word there it inserts
// a pair and puts the cursor between them, ready to type.
func (m *Model) toggleMark(mark rune) {
	lines := splitLines(m.input.Value())
	row := m.input.Line()
	if row >= len(lines) {
		return
	}
	line := lines[row]
	info := m.input.LineInfo()
	col := min(info.StartColumn+info.ColumnOffset, len(line))

	start, end := col, col
	for start > 0 && !unicode.IsSpace(line[start-1]) {
		start--
	}
	for end < len(line) && !unicode.IsSpace(line[end]) {
		end++
	}
	defer m.fitInput()
	if start == end {
		m.input.InsertString(string(mark) + string(mark))
		m.input.SetCursor(col + 1)
		return
	}
	if end-start >= 2 && line[start] == mark && line[end-1] == mark {
		// Unwrap: delete the closing mark, then the opening one.
		m.input.SetCursor(end)
		m.backspace()
		m.input.SetCursor(start + 1)
		m.backspace()
		m.input.SetCursor(end - 2)
		return
	}
	m.input.SetCursor(start)
	m.input.InsertRune(mark)
	m.input.SetCursor(end + 1)
	m.input.InsertRune(mark)
}

func (m *Model) backspace() {
	m.input, _ = m.input.Update(tea.KeyMsg{Type: tea.KeyBackspace})
}

func splitLines(s string) [][]rune {
	var out [][]rune
	cur := []rune{}
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = []rune{}
			continue
		}
		cur = append(cur, r)
	}
	return append(out, cur)
}

// inputRows is how many rows the input's text takes once soft-wrapped.
func (m *Model) inputRows() int {
	w := m.input.Width()
	if w <= 0 {
		return m.input.LineCount()
	}
	n := 0
	for _, line := range splitLines(m.input.Value()) {
		n += wrappedRows(line, w)
	}
	return n
}

// wrappedRows counts rows the way the textarea wraps a line (bubbles'
// textarea wrap(), which isn't exported): words move down whole, a word
// wider than the box is split, and the end keeps a cell for the cursor.
func wrappedRows(runes []rune, width int) int {
	sw := func(rs []rune) int { return ansi.StringWidth(string(rs)) }
	rows, lineW := 1, 0
	var word []rune
	spaces := 0
	for _, r := range runes {
		if unicode.IsSpace(r) {
			spaces++
		} else {
			word = append(word, r)
		}
		if spaces > 0 {
			if lineW+sw(word)+spaces > width {
				rows++
				lineW = sw(word) + spaces
			} else {
				lineW += sw(word) + spaces
			}
			spaces, word = 0, nil
		} else if last := ansi.StringWidth(string(word[len(word)-1])); sw(word)+last > width {
			if lineW > 0 {
				rows++
			}
			lineW = sw(word)
			word = nil
		}
	}
	if lineW+sw(word)+spaces >= width {
		rows++
	}
	return rows
}
