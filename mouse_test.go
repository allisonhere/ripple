package ripple

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestMousePlaceCursorCoordinates(t *testing.T) {
	for _, tc := range []struct {
		text                          string
		width, height, row, col, want int
	}{
		{"hello\n\nlast", 20, 4, 0, 2, 2}, {"hello\n\nlast", 20, 4, 0, 100, 5},
		{"hello\n\nlast", 20, 4, 1, 100, 6}, {"hello\n\nlast", 20, 4, 2, 2, 9},
		{"hello\n\nlast", 20, 6, 4, 0, 11},
		{"hello\n\nlast", 20, 4, -100, -100, 0}, {"hello\n\nlast", 20, 4, 100, 100, 11},
		{"abcdefghij", 4, 4, 1, 2, 6}, {"abc", 3, 3, 1, 0, 3},
		{"a界b🙂z", 20, 3, 0, 1, 1}, {"a界b🙂z", 20, 3, 0, 2, 1},
		{"a界b🙂z", 20, 3, 0, 3, 2}, {"a界b🙂z", 20, 3, 0, 5, 3},
		{"界界x", 3, 4, 1, 1, 1}, {"", 1, 1, 999, 999, 0},
	} {
		t.Run(fmt.Sprintf("%q/%d/%d,%d", tc.text, tc.width, tc.row, tc.col), func(t *testing.T) {
			e := New()
			e.SetSize(tc.width, tc.height)
			e.SetValue(tc.text)
			e.SelectAll()
			e.ScrollRows(-10000)
			e.PlaceCursor(tc.row, tc.col)
			if e.cursor != tc.want || e.SelectedText() != "" || e.Value() != tc.text {
				t.Fatalf("cursor %d selected %q value %q; want %d", e.cursor, e.SelectedText(), e.Value(), tc.want)
			}
			row, _ := e.visualPosition(e.cursor)
			if row < e.viewport || row >= e.viewport+e.documentHeight() {
				t.Fatal("placed cursor not visible")
			}
		})
	}
	var e Model
	e.PlaceCursor(-1, -1)
	if e.Value() != "" || e.cursor != 0 {
		t.Fatal("zero-value placement failed")
	}
}

func TestMouseScrollRenderingAndHistory(t *testing.T) {
	e := New()
	e.SetSize(12, 3)
	e.SetValue("first\n" + strings.Repeat("middle row\n", 30) + "last")
	e.InsertString(" edit")
	e.Undo() // Both undo and redo must survive scrolling and selection.
	e.SelectAll()
	before := e.currentSnapshot()
	undo, redo := slices.Clone(e.undo), slices.Clone(e.redo)
	maxInt := int(^uint(0) >> 1)
	for _, delta := range []int{-maxInt - 1, 3, maxInt, -1, 0, -maxInt - 1} {
		e.ScrollRows(delta)
		top := e.viewport
		lines := e.visualLines()
		if top < 0 || top > max(0, len(lines)-3) {
			t.Fatalf("invalid viewport %d", top)
		}
		view := strings.Split(e.View(Options{}), "\n")
		want := string(e.text[lines[top].start:lines[top].end])
		if view[0] != want {
			t.Fatalf("View pulled scroll back to caret: got %q want %q", view[0], want)
		}
		if e.currentSnapshot() != before || !slices.Equal(e.undo, undo) || !slices.Equal(e.redo, redo) {
			t.Fatal("scroll changed editing state/history")
		}
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyLeft})
	row, _ := e.visualPosition(e.cursor)
	if row < e.viewport || row >= e.viewport+3 {
		t.Fatal("keyboard did not restore caret visibility")
	}
	e.ScrollRows(-10000)
	e.SetSize(8, 2)
	row, _ = e.visualPosition(e.cursor)
	if row < e.viewport || row >= e.viewport+2 {
		t.Fatal("resize did not restore caret visibility")
	}
	for _, text := range []string{"", "x", "x\ny"} {
		var short Model
		short.SetSize(20, 4)
		short.SetValue(text)
		short.ScrollRows(maxInt)
		if short.viewport != 0 || short.Value() != text {
			t.Fatal("short document scroll failed")
		}
	}
}

func TestMouseSelectionAnchorCopyCutAndUndo(t *testing.T) {
	text := "hello  world\n\nwide 界 characters and more"
	e := New()
	e.SetSize(8, 3)
	e.SetValue(text)
	e.ScrollRows(-10000)
	e.PlaceCursor(0, 3)
	clip := &fakeClip{}
	e.SetClipboard(clip)
	for _, target := range [][2]int{{2, 5}, {0, 0}, {0, 3}, {1, 7}, {-100, -100}, {100, 100}} {
		pos := e.indexAtVisual(e.viewport+max(0, min(2, target[0])), target[1])
		top := e.viewport
		e.ExtendSelectionTo(target[0], target[1])
		start, end := min(3, pos), max(3, pos)
		if e.selAnchor != 3 || e.cursor != pos || e.SelectedText() != string([]rune(text)[start:end]) || e.viewport != top {
			t.Fatal("drag endpoint or anchor changed")
		}
		e.View(Options{})
		if e.viewport != top || e.Value() != text || len(e.undo) != 0 {
			t.Fatal("selection altered document/history/viewport")
		}
	}
	e.ScrollRows(1)
	e.ExtendSelectionTo(2, 7)
	selected := e.SelectedText()
	cmd := apply(&e, tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("copy command missing")
	}
	cmd()
	if clip.s != selected {
		t.Fatal("copy lost exact selected content")
	}
	before := e.currentSnapshot()
	cmd = apply(&e, tea.KeyMsg{Type: tea.KeyCtrlX})
	if cmd == nil {
		t.Fatal("cut command missing")
	}
	cmd()
	if clip.s != selected || e.Value() == text {
		t.Fatal("cut failed")
	}
	if !e.Undo() || e.currentSnapshot() != before {
		t.Fatal("undo did not restore mouse-selected state")
	}
	e.PlaceCursor(0, 0)
	if e.selAnchor != -1 {
		t.Fatal("new click retained old selection anchor")
	}
}

func TestMouseSelectWordBoundaries(t *testing.T) {
	for _, tc := range []struct {
		text string
		col  int
		want string
	}{
		{"hello_word42, next", 4, "hello_word42"}, {"hello_word42, next", 12, ","},
		{"hi there", 2, ""}, {"hi there", 100, ""}, {"", 0, ""},
		{"can't", 3, "'"}, {"hello-world", 5, "-"},
		{"café Ελληνικά", 2, "café"}, {"café Ελληνικά", 6, "Ελληνικά"},
		{"界界 word", 0, "界界"}, {"界界 word", 3, "界界"},
		{"🙂!", 1, "🙂"}, {"a\u00a0b", 1, ""}, {"a\u3000b", 2, ""},
	} {
		e := New()
		e.SetSize(40, 4)
		e.SetValue(tc.text)
		e.SelectWordAt(0, tc.col)
		if got := e.SelectedText(); got != tc.want {
			t.Fatalf("%q at %d selected %q; want %q", tc.text, tc.col, got, tc.want)
		}
		if e.Value() != tc.text || len(e.undo) != 0 {
			t.Fatal("word selection edited text")
		}
	}
	e := New()
	e.SetSize(5, 2)
	e.SetValue("long_word_across_many_rows and next")
	e.ScrollRows(-10000)
	e.ScrollRows(2)
	top := e.viewport
	e.SelectWordAt(0, 1)
	if e.SelectedText() != "long_word_across_many_rows" {
		t.Fatal("word did not cross soft wraps")
	}
	var highlighted string
	e.View(Options{Selected: func(s string) string { highlighted += s; return s }})
	if e.viewport != top || highlighted == "" {
		t.Fatal("rendering word selection changed viewport")
	}
}

func TestMouseCoordinatesWithSearch(t *testing.T) {
	e := New()
	e.SetSize(8, 4)
	e.SetValue(strings.Repeat("abc def\n", 20))
	e.OpenReplace()
	e.SetSearchQuery("abc")
	e.ScrollRows(-10000)
	e.ScrollRows(3)
	top := e.viewport
	e.PlaceCursor(999, 2)
	want := e.indexAtVisual(top+e.documentHeight()-1, 2)
	if e.cursor != want || e.Search().Query != "abc" || !e.SearchActive() {
		t.Fatal("coordinates included search bar or changed search state")
	}
	e.ScrollRows(10000)
	if e.viewport != len(e.visualLines())-e.documentHeight() {
		t.Fatal("scroll budget included search rows")
	}
	view := e.View(Options{})
	if len(strings.Split(view, "\n")) != 4 {
		t.Fatal("search render height changed")
	}
	e.SetSize(8, 1)
	before := e.currentSnapshot()
	e.PlaceCursor(0, 0)
	e.ExtendSelectionTo(100, 100)
	e.SelectWordAt(0, 0)
	if e.currentSnapshot() != before {
		t.Fatal("coordinates edited invisible document behind search-only view")
	}
	e.CloseSearch()
	e.PlaceCursor(0, 0)
	if e.SearchActive() {
		t.Fatal("pointer reopened search")
	}
}

func TestMouseCoordinateRoundTripsAndNavigation(t *testing.T) {
	for _, text := range []string{"abc\n\n  end ", "a界🙂 b\nΕλληνικά", "longword_and_more_text_across_rows"} {
		for width := 1; width <= 12; width++ {
			e := New()
			e.SetSize(width, 30)
			e.SetValue(text)
			e.ScrollRows(-10000)
			for index := 0; index <= len(e.text); index++ {
				row, col := e.visualPosition(index)
				if row >= 30 {
					continue
				}
				e.PlaceCursor(row, col)
				if e.cursor != index {
					t.Fatalf("width %d index %d => cursor %d", width, index, e.cursor)
				}
				e.ExtendSelectionTo(row, col)
				if e.SelectedText() != "" {
					t.Fatal("selection to caret became nonempty")
				}
			}
			if e.Value() != text {
				t.Fatal("coordinates altered Unicode text")
			}
		}
	}
}

func TestMouseVimModeAndSelection(t *testing.T) {
	for _, mode := range []string{"normal", "insert", "visual", "visual line", "command", "operator"} {
		t.Run(mode, func(t *testing.T) {
			e := vimEditor("hello world\nsecond")
			e.SetSize(20, 4)
			switch mode {
			case "insert":
				keys(&e, "i")
			case "visual":
				keys(&e, "vl")
			case "visual line":
				keys(&e, "V")
			case "command":
				keys(&e, ":w")
			case "operator":
				keys(&e, "d")
			}
			e.PlaceCursor(0, 1)
			e.ExtendSelectionTo(0, 4)
			if e.SelectedText() != "ell" {
				t.Fatal("mouse selection lost exclusive endpoints in Vim")
			}
			start, end, ok := e.selectionForRender()
			if !ok || start != 1 || end != 4 {
				t.Fatalf("Vim render range %d:%d disagrees with mouse selection", start, end)
			}
			clip := &fakeClip{}
			e.SetClipboard(clip)
			cmd := apply(&e, tea.KeyMsg{Type: tea.KeyCtrlC})
			if cmd == nil {
				t.Fatal("mouse selection cannot be copied in Vim")
			}
			cmd()
			if clip.s != "ell" {
				t.Fatal("Vim mouse copy differs from selection")
			}
			cmd = apply(&e, tea.KeyMsg{Type: tea.KeyCtrlX})
			if cmd == nil {
				t.Fatal("mouse selection cannot be cut in Vim")
			}
			cmd()
			if e.Value() != "ho world\nsecond" {
				t.Fatal("Vim mouse cut changed wrong range")
			}
			if !e.Undo() || e.Value() != "hello world\nsecond" {
				t.Fatal("Vim mouse cut undo failed")
			}
			if mode == "insert" && e.Mode() != "INSERT" {
				t.Fatal("mouse left Insert mode")
			}
		})
	}
}

func TestMouseOnlyOperationsPreserveHistory(t *testing.T) {
	e := New()
	e.SetSize(8, 3)
	e.InsertString("one two\nthree four\nfive six")
	e.InsertString(" more")
	e.Undo()
	undo, redo := slices.Clone(e.undo), slices.Clone(e.redo)
	value := e.Value()
	for i := 0; i < 40; i++ {
		e.ScrollRows(i%3 - 1)
		e.PlaceCursor(i%3, i%8)
		e.ExtendSelectionTo((i+1)%3, (i+2)%8)
		e.SelectWordAt(i%3, i%8)
		e.View(Options{})
	}
	if e.Value() != value || !reflect.DeepEqual(e.undo, undo) || !reflect.DeepEqual(e.redo, redo) {
		t.Fatal("pointer operations changed editing history")
	}
	if !e.Redo() || !strings.HasSuffix(e.Value(), " more") {
		t.Fatal("pointer operations invalidated redo")
	}
}

func BenchmarkMouseAPIs(b *testing.B) {
	for _, operation := range []string{"drag", "wheel"} {
		b.Run(operation, func(b *testing.B) {
			e := New()
			e.SetSize(72, 24)
			e.SetValue(strings.Repeat("ordinary text with wide 界 glyphs and words for selecting\n", 200))
			e.ScrollRows(-10000)
			e.PlaceCursor(0, 0)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if operation == "drag" {
					e.ExtendSelectionTo(i%24, i%72)
				} else {
					e.ScrollRows(i%7 - 3)
				}
			}
		})
	}
}

func TestMouseEmptyVisibleRows(t *testing.T) {
	e := New()
	e.SetSize(20, 6)
	e.SetValue("hello\nlast")
	e.SelectWordAt(3, 0)
	if e.SelectedText() != "" || e.CursorIndex() != len([]rune(e.Value())) {
		t.Fatal("blank visible row selected the adjacent last word")
	}
	e.PlaceCursor(4, 0)
	if e.CursorIndex() != len([]rune(e.Value())) {
		t.Fatal("blank visible row did not place caret at document end")
	}
	e.PlaceCursor(0, 1)
	e.ExtendSelectionTo(5, 0)
	if e.SelectedText() != "ello\nlast" {
		t.Fatal("dragging into blank rows did not extend to document end")
	}
}

func TestMouseKeyboardAndSearchResumeCaretFollowing(t *testing.T) {
	for _, tc := range []struct {
		name string
		vim  bool
		key  tea.KeyMsg
	}{
		{"Find", false, tea.KeyMsg{Type: tea.KeyCtrlF}},
		{"arrow", false, tea.KeyMsg{Type: tea.KeyRight}},
		{"Vim insert", true, keyRunes("i")},
		{"Vim operator", true, keyRunes("d")},
		{"Vim command", true, keyRunes(":")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := New()
			e.SetSize(12, 4)
			e.SetValue(strings.Repeat("hello words\n", 40))
			if tc.vim {
				e.SetInputMode(ModeVim)
			}
			e.ScrollRows(-10000)
			e.PlaceCursor(0, 1)
			e.ScrollRows(20)
			apply(&e, tc.key)
			row, _ := e.visualPosition(e.cursor)
			if e.viewportDetached || row < e.viewport || row >= e.viewport+max(1, e.documentHeight()) {
				t.Fatal("keyboard left caret off-screen after manual scrolling")
			}
		})
	}
	e := New()
	e.SetSize(12, 4)
	e.SetValue(strings.Repeat("hello words\n", 40))
	e.ScrollRows(-10000)
	e.PlaceCursor(0, 1)
	e.ScrollRows(20)
	e.OpenSearch()
	if e.viewportDetached || e.viewport != 0 {
		t.Fatal("opening Find did not resume caret following")
	}
}

func TestMouseSearchOpeningKeepsStoredViewportInSync(t *testing.T) {
	e := New()
	e.SetSize(12, 4)
	e.SetValue(strings.Repeat("hello words\n", 40))
	e.ScrollRows(-10000)
	e.PlaceCursor(3, 1)
	e.OpenSearch()
	row, _ := e.visualPosition(e.cursor)
	if row < e.viewport || row >= e.viewport+e.documentHeight() {
		t.Fatal("Find reduced document height without updating stored viewport")
	}
	lines := e.visualLines()
	first := strings.Split(e.View(Options{}), "\n")[0]
	if first != string(e.text[lines[e.viewport].start:lines[e.viewport].end]) {
		t.Fatal("stored viewport differs from rendered viewport")
	}
}
