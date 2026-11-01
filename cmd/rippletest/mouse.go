package main

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	doubleClickWindow  = 400 * time.Millisecond
	dragScrollInterval = 75 * time.Millisecond
	wheelStepRows      = 3
)

// Each started scroll loop has a generation; stopped loops cannot be revived
// by a pending timer. Only an active gesture schedules another one-shot tick.
type scrollTickMsg uint64

func (m model) scrollCmd() tea.Cmd {
	token := scrollTickMsg(m.scrollGeneration)
	return tea.Tick(dragScrollInterval, func(time.Time) tea.Msg { return token })
}

func (m *model) stopScroll() {
	if m.scrollDir != 0 {
		m.scrollDir = 0
		m.scrollGeneration++
	}
}

func (m *model) stopDrag() {
	m.stopScroll()
	m.dragging = false
}

// The single-line header precedes the frame. Match the frame's actual borders
// and padding without rendering the editor to measure its position.
func (m model) editorRect() (x, y, width, height int) {
	x = frameStyle.GetBorderLeftSize() + frameStyle.GetPaddingLeft()
	y = 1 + frameStyle.GetBorderTopSize() + frameStyle.GetPaddingTop()
	width, height = m.edW, m.edH
	if m.termW > 0 {
		width = max(0, min(width, m.termW-x))
	}
	if m.termH > 0 {
		height = max(0, min(height, m.termH-y))
	}
	return
}

func (m model) handleScrollTick(token scrollTickMsg) (model, tea.Cmd) {
	if uint64(token) != m.scrollGeneration {
		return m, nil
	}
	if !m.mouseEnabled || !m.dragging || m.ed.SearchActive() {
		m.stopDrag()
		return m, nil
	}
	if m.scrollDir == 0 {
		return m, nil
	}
	_, _, w, h := m.editorRect()
	if w <= 0 || h <= 0 {
		m.stopDrag()
		return m, nil
	}
	top := m.ed.ViewportTop()
	m.ed.ScrollRows(m.scrollDir)
	row, col := 0, 0
	if m.scrollDir > 0 {
		row, col = h-1, w
	}
	m.ed.ExtendSelectionTo(row, col)
	if m.ed.ViewportTop() == top {
		m.stopScroll()
		return m, nil
	}
	return m, m.scrollCmd()
}

// now is supplied by Update; tests pass explicit times without sleeping.
func (m model) handleMouse(msg tea.MouseMsg, now time.Time) (model, tea.Cmd) {
	if !m.mouseEnabled {
		return m, nil
	}
	if m.ed.SearchActive() {
		m.stopDrag()
		m.clickAt = time.Time{}
		return m, nil
	}
	// Legacy terminals may omit button identity on release.
	if msg.Action == tea.MouseActionRelease {
		m.stopDrag()
		return m, nil
	}
	x, y, w, h := m.editorRect()
	motion := msg.Action == tea.MouseActionMotion && msg.Button == tea.MouseButtonLeft && m.dragging
	if motion {
		m.clickAt = time.Time{}
		if w <= 0 || h <= 0 {
			m.stopDrag()
			return m, nil
		}
		m.ed.ExtendSelectionTo(max(0, min(h-1, msg.Y-y)), max(0, min(w-1, msg.X-x)))
		dir := 0
		if msg.Y < y {
			dir = -1
		} else if msg.Y >= y+h {
			dir = 1
		}
		if dir == 0 {
			m.stopScroll()
		} else {
			start := m.scrollDir == 0
			m.scrollDir = dir
			if start {
				m.scrollGeneration++
				return m, m.scrollCmd()
			}
		}
		return m, nil
	}
	if tea.MouseEvent(msg).IsWheel() {
		m.stopDrag()
		m.clickAt = time.Time{}
		if msg.X >= x && msg.X < x+w && msg.Y >= y && msg.Y < y+h {
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				m.ed.ScrollRows(-wheelStepRows)
			case tea.MouseButtonWheelDown:
				m.ed.ScrollRows(wheelStepRows)
			}
		}
		return m, nil
	}
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}
	m.stopDrag()
	if msg.Button != tea.MouseButtonLeft || msg.X < x || msg.X >= x+w || msg.Y < y || msg.Y >= y+h {
		m.clickAt = time.Time{}
		return m, nil
	}
	m.ed.PlaceCursor(msg.Y-y, msg.X-x)
	index := m.ed.CursorIndex()
	double := !m.clickAt.IsZero() && now.Sub(m.clickAt) >= 0 && now.Sub(m.clickAt) <= doubleClickWindow &&
		m.clickX == msg.X && m.clickY == msg.Y && m.clickIndex == index
	if double {
		m.ed.SelectWordAt(msg.Y-y, msg.X-x)
		m.clickAt = time.Time{} // A third click starts a fresh single-click gesture.
	} else {
		m.clickAt, m.clickX, m.clickY, m.clickIndex = now, msg.X, msg.Y, index
	}
	m.dragging = !double
	return m, nil
}
