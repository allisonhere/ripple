package ripple

import (
	"reflect"
	"slices"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Record the observed reflow without changing the wrapping policy. A completed
// word and its following space no longer fit together at the exact width.
func TestWordWrapExactWidthTrailingSpaceReflow(t *testing.T) {
	e := New()
	e.SetSize(11, 4)
	e.SetWrapMode(WrapWord)
	e.SetValue("hello world")
	check := func(value string, rows []string, row, col int) {
		t.Helper()
		if e.Value() != value || e.cursor != len([]rune(value)) {
			t.Fatalf("document/caret = %q/%d; want %q/%d", e.Value(), e.cursor, value, len([]rune(value)))
		}
		if got := wrappedText(e); !reflect.DeepEqual(got, rows) {
			t.Fatalf("rows = %#v; want %#v", got, rows)
		}
		if r, c := e.visualPosition(e.cursor); r != row || c != col {
			t.Fatalf("caret at %d/%d; want %d/%d", r, c, row, col)
		}
		assertWordWrapCaretVisible(t, e)
	}
	check("hello world", []string{"hello world", ""}, 1, 0)
	apply(&e, tea.KeyMsg{Type: tea.KeySpace})
	check("hello world ", []string{"hello ", "world "}, 1, 6)
	apply(&e, tea.KeyMsg{Type: tea.KeyBackspace})
	check("hello world", []string{"hello world", ""}, 1, 0)
	if !e.Undo() {
		t.Fatal("cannot undo deletion of trailing space")
	}
	check("hello world ", []string{"hello ", "world "}, 1, 6)
	if !e.Undo() {
		t.Fatal("cannot undo insertion of trailing space")
	}
	check("hello world", []string{"hello world", ""}, 1, 0)
	if !e.Redo() {
		t.Fatal("cannot redo trailing space insertion")
	}
	check("hello world ", []string{"hello ", "world "}, 1, 6)

	apply(&e, tea.KeyMsg{Type: tea.KeyShiftUp})
	if e.cursor != 5 || e.SelectedText() != " world " {
		t.Fatal("Up did not reach the preceding row's final space or lost selection")
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyShiftDown})
	if e.cursor != 12 || e.SelectedText() != "" {
		t.Fatal("Down did not retain the preferred column and original selection anchor")
	}

	// Both spaces (the separator and the new trailing space) are independently
	// reachable and selectable despite being at different visual coordinates.
	clip := &fakeClip{}
	e.SetClipboard(clip)
	for _, index := range []int{5, 11} {
		apply(&e, tea.KeyMsg{Type: tea.KeyCtrlHome})
		for i := 0; i < index; i++ {
			apply(&e, tea.KeyMsg{Type: tea.KeyRight})
		}
		apply(&e, tea.KeyMsg{Type: tea.KeyShiftRight})
		if e.SelectedText() != " " {
			t.Fatalf("space at index %d was not selectable", index)
		}
		cmd := apply(&e, tea.KeyMsg{Type: tea.KeyCtrlC})
		if cmd == nil {
			t.Fatal("copy command missing")
		}
		cmd()
		if clip.s != " " {
			t.Fatal("copy lost selected space")
		}
	}
}

// Avoiding a leading separator in ordinary prose is a useful consequence, not
// a blanket no-leading-whitespace invariant: runs and long words can still put
// actual document whitespace at the beginning of a soft visual row.
func TestWordWrapWhitespacePlacementPolicy(t *testing.T) {
	for _, tc := range []struct {
		text  string
		width int
		rows  []string
	}{
		{"hello world ", 11, []string{"hello ", "world "}},
		{"hello world  ", 11, []string{"hello ", "world  "}},
		{"hello world next", 11, []string{"hello ", "world next"}},
		{"abcde ", 5, []string{"abcde", " "}},
		{"a  b   c", 5, []string{"a  b ", "  c"}},
		{"   abc", 5, []string{"   ", "abc"}},
	} {
		e := New()
		e.SetSize(tc.width, 10)
		e.SetWrapMode(WrapWord)
		e.SetValue(tc.text)
		if got := wrappedText(e); !reflect.DeepEqual(got, tc.rows) {
			t.Fatalf("%q at width %d: %#v; want %#v", tc.text, tc.width, got, tc.rows)
		}
		if e.Value() != tc.text {
			t.Fatal("layout changed whitespace in the buffer")
		}
	}
}

func TestWordWrapWhitespaceSelectionCopyAndReflow(t *testing.T) {
	for _, text := range []string{"hello world ", "  hello   world  \n\n tail \u00a0 \u3000 "} {
		for _, backwards := range []bool{false, true} {
			e := New()
			e.SetSize(11, 3)
			e.SetWrapMode(WrapWord)
			e.SetValue(text)
			clip := &fakeClip{}
			e.SetClipboard(clip)
			key := tea.KeyShiftRight
			apply(&e, tea.KeyMsg{Type: tea.KeyCtrlHome})
			if backwards {
				apply(&e, tea.KeyMsg{Type: tea.KeyCtrlEnd})
				key = tea.KeyShiftLeft
			}
			runes := []rune(text)
			for count := 1; count <= len(runes); count++ {
				apply(&e, tea.KeyMsg{Type: key})
				want := string(runes[:count])
				if backwards {
					want = string(runes[len(runes)-count:])
				}
				before := e.currentSnapshot()
				undo, redo := slices.Clone(e.undo), slices.Clone(e.redo)
				for _, width := range []int{11, 6, 12, 1, 24} {
					for _, mode := range []WrapMode{WrapCharacter, WrapWord} {
						e.SetSize(width, 3)
						e.SetWrapMode(mode)
						if e.currentSnapshot() != before || e.SelectedText() != want || e.Value() != text {
							t.Fatalf("reflow changed %q selection %q at width %d", text, want, width)
						}
						if !slices.Equal(e.undo, undo) || !slices.Equal(e.redo, redo) {
							t.Fatal("reflow changed history")
						}
						assertWordWrapCaretVisible(t, e)
						// Every whitespace rune keeps an addressable document offset.
						for index := range len(runes) + 1 {
							row, col := e.visualPosition(index)
							if got := e.indexAtVisual(row, col); got != index {
								t.Fatalf("document index %d round-tripped to %d", index, got)
							}
						}
						cmd := apply(&e, tea.KeyMsg{Type: tea.KeyCtrlC})
						if cmd == nil {
							t.Fatal("missing clipboard command")
						}
						cmd()
						if clip.s != want {
							t.Fatalf("copied %q; want %q", clip.s, want)
						}
					}
				}
			}
			if clip.s != text {
				t.Fatal("copying the full document lost whitespace")
			}
		}
	}
}
