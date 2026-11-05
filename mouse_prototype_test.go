package ripple

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Adapted from the recovered prototypes, retaining character-wrap cases only.
func TestMouseRecoveredExtendSelectionTo(t *testing.T) {
	e := New()
	e.SetSize(10, 8)
	text := "hello world again\nsecond hard line\nlast"
	e.SetValue(text)
	clip := &fakeClip{s: "unchanged"}
	e.SetClipboard(clip)
	e.PlaceCursor(0, 2)
	e.ExtendSelectionTo(1, 3)
	end := 13
	if e.SelectedText() != text[2:end] {
		t.Fatalf("forward selection %q", e.SelectedText())
	}
	e.ExtendSelectionTo(0, 0)
	if e.SelectedText() != "he" || e.selAnchor != 2 {
		t.Fatal("backward drag lost anchor")
	}
	e.ExtendSelectionTo(0, 2)
	e.ExtendSelectionTo(3, 2)
	if e.selAnchor != 2 || !strings.Contains(e.SelectedText(), "\n") || e.Value() != text || clip.s != "unchanged" {
		t.Fatal("cross-row selection changed anchor/text/clipboard")
	}
	selected := e.SelectedText()
	cmd := apply(&e, tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("missing copy command")
	}
	cmd()
	if clip.s != selected {
		t.Fatal("copy did not use drag selection")
	}
	e.PlaceCursor(0, 1)
	if e.SelectedText() != "" || e.CursorIndex() != 1 {
		t.Fatal("subsequent click")
	}
	e.SetSize(10, 2)
	e.ScrollRows(2)
	top := e.ViewportTop()
	e.PlaceCursor(0, 1)
	anchor := e.CursorIndex()
	end = e.indexAtVisual(top+1, 3)
	e.ExtendSelectionTo(1, 3)
	if e.selAnchor != anchor || e.CursorIndex() != end || e.ViewportTop() != top {
		t.Fatal("scrolled selection")
	}
	e.ExtendSelectionTo(999, 999)
	if e.ViewportTop() != top {
		t.Fatal("out-of-bounds selection auto-scrolled")
	}
	if e.Value() != text {
		t.Fatal("drag modified document")
	}
}

func TestMouseRecoveredSelectWordAt(t *testing.T) {
	e := New()
	e.SetSize(5, 10)
	text := "hello_longword, next\n\n last"
	e.SetValue(text)
	clip := &fakeClip{s: "unchanged"}
	e.SetClipboard(clip)
	e.SelectWordAt(1, 2) // middle of a word split across visual rows
	if e.SelectedText() != "hello_longword" {
		t.Fatalf("selection %q", e.SelectedText())
	}
	for _, tc := range []struct {
		index int
		want  string
	}{{0, "hello_longword"}, {14, ","}, {15, ""}, {16, "next"}, {20, ""}, {21, ""}, {22, ""}, {23, "last"}, {len([]rune(text)), ""}} {
		row, col := e.visualPosition(tc.index)
		e.SelectWordAt(row, col)
		if e.SelectedText() != tc.want {
			t.Fatalf("index %d got %q want %q", tc.index, e.SelectedText(), tc.want)
		}
	}
	e.SelectWordAt(0, 100) // blank area after the first soft row must not select the next word
	if e.SelectedText() != "" {
		t.Fatal("blank cell selected adjacent word")
	}
	e.SetSize(5, 2)
	e.ScrollRows(2)
	top := e.ViewportTop()
	e.SelectWordAt(0, 1)
	if e.SelectedText() != "hello_longword" || e.ViewportTop() != top {
		t.Fatal("selection after scroll")
	}
	if e.Value() != text || clip.s != "unchanged" {
		t.Fatal("word selection modified text/clipboard")
	}
	e.SetValue("")
	e.SelectWordAt(100, 100)
	if e.SelectedText() != "" || e.Value() != "" {
		t.Fatal("empty selection")
	}
}

func TestMouseRecoveredCursorSelectionAndScroll(t *testing.T) {
	e := New()
	e.SetSize(10, 2)
	text := "hello world again\n" + strings.Repeat("another row here\n", 5)
	e.SetValue(text)
	e.ScrollRows(-1000)
	e.PlaceCursor(0, 2)
	if e.CursorIndex() != 2 {
		t.Fatal("placement")
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyShiftDown})
	cursor, selection := e.CursorIndex(), e.SelectedText()
	if selection == "" {
		t.Fatal("wrapped vertical selection is empty")
	}
	e.ScrollRows(3)
	if e.ViewportTop() != 3 || e.CursorIndex() != cursor || e.SelectedText() != selection {
		t.Fatal("scroll changed caret/selection")
	}
	_ = e.View(Options{})
	if e.ViewportTop() != 3 {
		t.Fatal("render undid scroll")
	}
	want := e.indexAtVisual(e.ViewportTop()+1, 3)
	e.PlaceCursor(1, 3)
	if e.CursorIndex() != want || e.SelectedText() != "" || e.Value() != text {
		t.Fatal("placement after scroll")
	}
	e.ScrollRows(1000)
	if e.ViewportTop() != max(0, len(e.visualLines())-2) {
		t.Fatal("scroll lower clamp")
	}
	e.ScrollRows(-1000)
	want = e.indexAtVisual(0, 100)
	e.PlaceCursor(0, 100)
	if e.CursorIndex() != want {
		t.Fatal("placement beyond visual end")
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyShiftRight})
	cursor, selection = e.CursorIndex(), e.SelectedText()
	e.ScrollRows(1)
	if e.CursorIndex() != cursor || e.SelectedText() != selection || e.Value() != text {
		t.Fatal("scroll state preservation")
	}
}
