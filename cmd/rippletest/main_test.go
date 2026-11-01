package main

import (
	"strings"
	"testing"

	"github.com/allisonhere/ripple"
	tea "github.com/charmbracelet/bubbletea"
)

func TestWrapHarnessTogglePreservesEditsAndHistory(t *testing.T) {
	m := newModel()
	if m.ed.Value() != "" || m.wrapMode != ripple.WrapCharacter {
		t.Fatal("default harness changed")
	}
	m.ed.SetValue(wrapDemo)
	m.ed.InsertString("user edit")
	value, cursor := m.ed.Value(), m.ed.CursorIndex()
	for _, mode := range []ripple.WrapMode{ripple.WrapWord, ripple.WrapCharacter} {
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyF4})
		m = next.(model)
		if cmd != nil || m.wrapMode != mode || m.ed.Value() != value || m.ed.CursorIndex() != cursor {
			t.Fatal("F4 changed document state")
		}
		label := "character"
		if mode == ripple.WrapWord {
			label = "word-aware"
		}
		if !strings.Contains(m.View(), "wrap: "+label) {
			t.Fatal("active wrapping mode not visible")
		}
		for _, size := range [][2]int{{30, 14}, {100, 30}, {60, 20}} {
			next, _ = m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m = next.(model)
			w, h := m.ed.Size()
			if w != m.edW || h != m.edH || w != min(size[0]-6, 72) || m.ed.Value() != value {
				t.Fatal("resize changed content or stored dimensions")
			}
		}
	}
	if !m.ed.Undo() || m.ed.Value() != wrapDemo {
		t.Fatal("F4 or resizing changed edit history")
	}
	if !m.ed.Redo() || m.ed.Value() != value {
		t.Fatal("redo lost user edit")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyF2})
	m = next.(model)
	if m.ed.InputMode() != ripple.ModeVim {
		t.Fatal("F2 stopped toggling Vim")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyF4})
	m = next.(model)
	if m.ed.InputMode() != ripple.ModeVim || m.ed.Value() != value {
		t.Fatal("F4 affected Vim mode or demo")
	}
}
