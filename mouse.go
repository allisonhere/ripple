package ripple

import "unicode"

// PlaceCursor moves the caret to a zero-based viewport-relative visual row and
// terminal-cell column, clearing selection. Coordinates clamp to the visible
// document area and line ends. Keyboard movement semantics remain unchanged.
func (m *Model) PlaceCursor(row, col int) {
	pos, _, ok := m.viewportPosition(row, col)
	if !ok {
		return
	}
	m.preparePointerSelection()
	m.moveTo(pos, false)
	m.goalCol = -1
	m.ensureCursorVisible()
}

// ScrollRows scrolls visual rows (positive down), clamped to the document,
// without changing text, caret, selection, or undo/redo history. View honours
// this viewport even when the caret is off-screen; keyboard editing/navigation
// and resizing subsequently restore caret visibility.
func (m *Model) ScrollRows(delta int) {
	m.clamp()
	maxTop := max(0, len(m.visualLines())-max(1, m.documentHeight()))
	m.viewport = max(0, min(maxTop, m.viewport))
	// Bound the delta before addition so extreme coordinates cannot overflow.
	delta = max(-m.viewport, min(delta, maxTop-m.viewport))
	m.viewport += delta
	m.viewportDetached = true
}

// ExtendSelectionTo extends from the existing anchor (or the current caret)
// to a zero-based viewport-relative visual cell. It preserves text, clipboard
// and viewport; hosts own drag events and any autoscrolling policy.
func (m *Model) ExtendSelectionTo(row, col int) {
	pos, _, ok := m.viewportPosition(row, col)
	if !ok {
		return
	}
	m.preparePointerSelection()
	m.moveTo(pos, true)
	m.goalCol = -1
	m.viewportDetached = true
}

// SelectWordAt selects a run of Unicode letters/digits/underscores at a
// viewport-relative visual cell, including across soft wraps. Punctuation
// selects one rune; whitespace, empty rows and cells past a visual line clear
// selection without selecting an adjacent word. Text, clipboard and viewport
// remain unchanged. Selection endpoints use document rune offsets.
func (m *Model) SelectWordAt(row, col int) {
	pos, endOfRow, ok := m.viewportPosition(row, col)
	if !ok {
		return
	}
	m.preparePointerSelection()
	m.moveTo(pos, false)
	m.goalCol = -1
	m.viewportDetached = true
	if pos >= endOfRow || pos >= len(m.text) || unicode.IsSpace(m.text[pos]) {
		return
	}
	start, end := pos, pos+1
	if isWordRune(m.text[pos]) {
		for start > 0 && isWordRune(m.text[start-1]) {
			start--
		}
		for end < len(m.text) && isWordRune(m.text[end]) {
			end++
		}
	}
	m.moveTo(start, false)
	m.moveTo(end, true)
}

// Internal search rows are not part of the visible editing surface. In a
// terminal too short to display document rows, coordinate operations do nothing.
func (m *Model) viewportPosition(row, col int) (position, endOfRow int, ok bool) {
	m.clamp()
	height := m.documentHeight()
	if height == 0 {
		return 0, 0, false
	}
	row = max(0, min(row, height-1))
	lines := m.visualLines()
	docRow := m.viewport + row
	if docRow >= len(lines) {
		// Blank screen rows below EOF refer to the document end, not a word
		// at the same column on the last nonempty row.
		return len(m.text), len(m.text), true
	}
	return m.indexAtLine(lines[docRow], col), lines[docRow].end, true
}

// Pointer selections use exclusive document ranges, unlike Vim's inclusive
// Visual/linewise ranges. A pointer gesture ends those modes and partial
// commands, while preserving Insert mode and the existing register.
func (m *Model) preparePointerSelection() {
	if m.inputMode == ModeVim {
		if m.vim.mode != vimInsert {
			m.vim.mode = vimNormal
			m.vim.cmdline = nil
		}
		m.vim.pending, m.vim.count, m.vim.gPending = 0, 0, false
	}
}
