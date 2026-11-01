package ripple

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func wrappedText(e Model) []string {
	var rows []string
	for _, line := range e.visualLines() {
		rows = append(rows, string(e.text[line.start:line.end]))
	}
	return rows
}

// The first five cases retain the recovered prototype's wrapping expectations.
func TestWordWrapRows(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		width      int
		want       []string
	}{
		{"words", "hello world again", 10, []string{"hello ", "world ", "again"}},
		{"long word", "abcdefghijkl", 5, []string{"abcde", "fghij", "kl"}},
		{"url", "https://example.org/path", 10, []string{"https://ex", "ample.org/", "path"}},
		{"whitespace and newline", "ab  cd\nef", 5, []string{"ab  ", "cd", "ef"}},
		{"wide", "ab 界界 x", 6, []string{"ab ", "界界 x", ""}},
		{"paragraph", "one two three four\n\nfive six", 12, []string{"one two ", "three four", "", "five six"}},
		{"punctuation", "hi,there! ok?", 8, []string{"hi,there", "! ok?"}},
		{"multiple spaces", "a  b   c", 5, []string{"a  b ", "  c"}},
		{"leading spaces", "   abc def", 5, []string{"   ", "abc ", "def"}},
		{"space before long word", " abcdefghij", 5, []string{" ", "abcde", "fghij", ""}},
		{"tabs normalized on load", "ab\tcd", 6, []string{"ab    ", "cd"}},
		{"nonbreaking space", "ab\u00a0cd ef", 5, []string{"ab\u00a0", "cd ef", ""}},
		{"ideographic space", "ab\u3000cd", 5, []string{"ab\u3000", "cd"}},
		{"unicode letters", "café été", 6, []string{"café ", "été"}},
		{"emoji", "hi 🙂 go", 5, []string{"hi ", "🙂 go", ""}},
		{"combining follows existing cell model", "e\u0301 e\u0301", 4, []string{"e\u0301 ", "e\u0301"}},
		{"one cell", "a b", 1, []string{"a", " ", "b", ""}},
		{"wide rune in one cell", "界a", 1, []string{"界", "a", ""}},
		{"wide exact", "界界", 4, []string{"界界", ""}},
		{"exact word", "abcde", 5, []string{"abcde", ""}},
		{"exact space", "ab cd ", 6, []string{"ab cd ", ""}},
		{"trailing spaces", "abc  ", 5, []string{"abc  ", ""}},
		{"only spaces", "     ", 3, []string{"   ", "  "}},
		{"empty", "", 5, []string{""}},
		{"empty hard lines", "\n\n", 5, []string{"", "", ""}},
		{"trailing newline", "abc\n", 6, []string{"abc", ""}},
		{"exact before newline", "abc\nx", 3, []string{"abc", "", "x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := New()
			e.SetSize(tc.width, 20)
			e.SetValue(tc.text)
			value := e.Value() // Loading tabs has always normalized them to spaces.
			character := e.visualLines()
			e.SetWrapMode(WrapWord)
			if got := wrappedText(e); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("rows = %#v; want %#v", got, tc.want)
			}
			if e.Value() != value {
				t.Fatal("wrapping modified the document")
			}
			e.SetWrapMode(WrapCharacter)
			if !reflect.DeepEqual(character, e.visualLines()) {
				t.Fatal("character layout changed after round trip")
			}
		})
	}
}

func TestWordWrapDefaultCompatibility(t *testing.T) {
	for _, tc := range []struct {
		text  string
		width int
		want  []string
	}{
		{"hello world again", 10, []string{"hello worl", "d again"}},
		{"ab 界界 x", 6, []string{"ab 界", "界 x"}},
		{"abc\nx", 3, []string{"abc", "", "x"}},
		{"ab  cd", 5, []string{"ab  c", "d"}},
		{"", 1, []string{""}},
	} {
		for _, e := range []Model{New(), {}} {
			e.SetSize(tc.width, 10)
			e.SetValue(tc.text)
			if e.wrapMode != WrapCharacter || !reflect.DeepEqual(wrappedText(e), tc.want) {
				t.Fatalf("default layout for %q = %#v; want %#v", tc.text, wrappedText(e), tc.want)
			}
		}
	}
	// Existing callers and explicit character-mode callers must keep identical
	// document, cursor, selection, history, viewport, and rendered output.
	a, b := New(), New()
	b.SetWrapMode(WrapCharacter)
	for _, e := range []*Model{&a, &b} {
		e.SetSize(7, 3)
		e.SetValue("hello wide 界 words\nsecond line")
	}
	for i, key := range []tea.KeyMsg{
		{Type: tea.KeyCtrlHome}, {Type: tea.KeyDown}, {Type: tea.KeyShiftDown},
		{Type: tea.KeyShiftEnd}, keyRunes("new"), {Type: tea.KeyBackspace},
		{Type: tea.KeyCtrlZ}, {Type: tea.KeyCtrlY}, {Type: tea.KeyPgDown},
		{Type: tea.KeyUp}, {Type: tea.KeyHome}, {Type: tea.KeyEnd},
	} {
		apply(&a, key)
		apply(&b, key)
		if i == 5 {
			a.SetSize(4, 2)
			b.SetSize(4, 2)
		}
		if a.currentSnapshot() != b.currentSnapshot() || a.viewport != b.viewport || !reflect.DeepEqual(a.undo, b.undo) || !reflect.DeepEqual(a.redo, b.redo) || a.View(Options{}) != b.View(Options{}) {
			t.Fatalf("default/explicit character modes diverged at key %s", key)
		}
	}
}

func TestWordWrapVerticalMovementAndSelection(t *testing.T) {
	e := New()
	e.SetSize(6, 2)
	e.SetWrapMode(WrapWord)
	text := "aaaa b cccc dddd\nlast"
	e.SetValue(text)
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlHome})
	for i := 0; i < 4; i++ {
		apply(&e, tea.KeyMsg{Type: tea.KeyRight})
	}
	for _, tc := range []struct {
		key        tea.KeyType
		index, row int
	}{
		{tea.KeyDown, 6, 1}, {tea.KeyDown, 11, 2}, {tea.KeyDown, 16, 3}, {tea.KeyDown, 21, 4},
		{tea.KeyUp, 16, 3}, {tea.KeyUp, 11, 2}, {tea.KeyUp, 6, 1}, {tea.KeyUp, 4, 0},
	} {
		apply(&e, tea.KeyMsg{Type: tc.key})
		row, _ := e.visualPosition(e.cursor)
		if e.cursor != tc.index || row != tc.row {
			t.Fatalf("%v: index=%d row=%d; want %d/%d", tc.key, e.cursor, row, tc.index, tc.row)
		}
		assertWordWrapCaretVisible(t, e)
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyShiftDown})
	if e.SelectedText() != " b" {
		t.Fatalf("vertical selection = %q", e.SelectedText())
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyShiftDown})
	if e.SelectedText() != text[4:11] || e.selAnchor != 4 {
		t.Fatal("selection lost anchor across wraps")
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyHome})
	if e.cursor != 0 || e.SelectedText() != "" {
		t.Fatal("Home must select the hard line start")
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyShiftEnd})
	if e.cursor != 16 || e.SelectedText() != text[:16] {
		t.Fatal("Shift+End must select through soft wraps to hard line end")
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyShiftHome})
	if e.cursor != 0 || e.SelectedText() != "" {
		t.Fatal("Shift+Home did not collapse selection at anchor")
	}
	for i := 0; i < 5; i++ {
		apply(&e, tea.KeyMsg{Type: tea.KeyRight})
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyShiftLeft})
	if e.cursor != 4 || e.SelectedText() != " " {
		t.Fatal("Shift+Left across a soft boundary")
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyShiftRight})
	apply(&e, tea.KeyMsg{Type: tea.KeyShiftRight})
	if e.cursor != 6 || e.SelectedText() != "b" {
		t.Fatal("Shift+Right did not retain boundary anchor")
	}
	if e.Value() != text {
		t.Fatal("movement changed text")
	}
}

func assertWordWrapCaretVisible(t *testing.T, e Model) {
	t.Helper()
	row, _ := e.visualPosition(e.cursor)
	if row < e.viewport || row >= e.viewport+max(1, e.documentHeight()) {
		t.Fatalf("caret row %d outside viewport %d+%d", row, e.viewport, e.documentHeight())
	}
	if got := len(strings.Split(e.View(Options{}), "\n")); got != e.height {
		t.Fatalf("rendered height = %d; want %d", got, e.height)
	}
}

// Retains the prototype's vertical selection and wrap-mode state checks using
// keyboard navigation, rather than the out-of-scope PlaceCursor/ScrollRows APIs.
func TestWordWrapViewportResizeAndState(t *testing.T) {
	for _, mode := range []WrapMode{WrapCharacter, WrapWord} {
		e := New()
		e.SetSize(10, 2)
		e.SetWrapMode(mode)
		text := "hello world again\n" + strings.Repeat("another row here\n", 12)
		e.SetValue(text)
		apply(&e, tea.KeyMsg{Type: tea.KeyCtrlHome})
		apply(&e, tea.KeyMsg{Type: tea.KeyRight})
		apply(&e, tea.KeyMsg{Type: tea.KeyRight})
		apply(&e, tea.KeyMsg{Type: tea.KeyShiftDown})
		cursor, selection, anchor := e.cursor, e.SelectedText(), e.selAnchor
		if selection == "" {
			t.Fatal("wrapped vertical selection is empty")
		}
		for _, width := range []int{5, 20, 1, 10, 40, 6} {
			e.SetSize(width, 3)
			e.SetWrapMode(WrapWord)
			e.SetWrapMode(WrapCharacter)
			e.SetWrapMode(mode)
			if e.cursor != cursor || e.selAnchor != anchor || e.SelectedText() != selection || e.Value() != text {
				t.Fatal("reflow changed text or selection endpoints")
			}
			assertWordWrapCaretVisible(t, e)
		}
		for i := 0; i < 8; i++ {
			apply(&e, tea.KeyMsg{Type: tea.KeyPgDown})
			assertWordWrapCaretVisible(t, e)
		}
		if e.viewport == 0 {
			t.Fatal("PageDown failed to scroll")
		}
		for i := 0; i < 60; i++ {
			apply(&e, tea.KeyMsg{Type: tea.KeyUp})
			assertWordWrapCaretVisible(t, e)
		}
		apply(&e, tea.KeyMsg{Type: tea.KeyCtrlHome})
		if e.viewport != 0 || e.cursor != 0 {
			t.Fatal("document-start clamp failed")
		}
		apply(&e, tea.KeyMsg{Type: tea.KeyCtrlEnd})
		apply(&e, tea.KeyMsg{Type: tea.KeyPgDown})
		if e.cursor != len([]rune(text)) {
			t.Fatal("document-end clamp failed")
		}
		if e.Value() != text {
			t.Fatal("scrolling changed text")
		}
	}
}

func TestWordWrapHistoryAndEditing(t *testing.T) {
	e := New()
	e.SetSize(8, 3)
	apply(&e, keyRunes("ab"))
	e.SetWrapMode(WrapWord)
	apply(&e, keyRunes("cd"))
	e.SetSize(2, 2)
	apply(&e, keyRunes("ef"))
	if !e.Undo() || e.Value() != "" {
		t.Fatal("mode change/resize split the typing undo group")
	}
	e.SetWrapMode(WrapCharacter)
	e.SetSize(15, 4)
	if !e.Redo() || e.Value() != "abcdef" {
		t.Fatal("layout change corrupted redo")
	}
	e.SetValue("hello world again\nwide 界 text")
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlHome})
	for i := 0; i < 6; i++ {
		apply(&e, tea.KeyMsg{Type: tea.KeyRight})
	}
	for i := 0; i < 5; i++ {
		apply(&e, tea.KeyMsg{Type: tea.KeyShiftRight})
	}
	before := e.currentSnapshot()
	e.SetWrapMode(WrapWord)
	e.SetSize(5, 2)
	e.InsertString("new word")
	changed := e.currentSnapshot()
	if changed.text != "hello new word again\nwide 界 text" {
		t.Fatalf("replacement after reflow = %q", changed.text)
	}
	e.SetSize(30, 6)
	e.SetWrapMode(WrapCharacter)
	if !e.Undo() || e.currentSnapshot() != before {
		t.Fatal("undo did not restore selected document state")
	}
	undo, redo := append([]snapshot(nil), e.undo...), append([]snapshot(nil), e.redo...)
	for _, mode := range []WrapMode{WrapWord, WrapWord, WrapCharacter, WrapMode(99)} {
		e.SetWrapMode(mode)
		e.SetWidth(7)
		e.SetHeight(3)
	}
	if !slices.Equal(e.undo, undo) || !slices.Equal(e.redo, redo) {
		t.Fatal("layout mutated undo/redo stacks")
	}
	if !e.Redo() || e.currentSnapshot() != changed {
		t.Fatal("redo failed after mode/size changes")
	}
	if e.wrapMode != WrapCharacter {
		t.Fatal("unknown wrap mode did not select default")
	}
	var zero Model
	zero.SetWrapMode(WrapWord)
	if w, h := zero.Size(); w != 80 || h != 24 {
		t.Fatal("zero-value setter lost default dimensions")
	}
}

func TestWordWrapPlaceholders(t *testing.T) {
	for _, tc := range []struct {
		text  string
		width int
		want  []string
	}{
		{"hello world again", 10, []string{"hello ", "world ", "again"}},
		{"ab 界界 x", 6, []string{"ab ", "界界 x", ""}},
		{"ab\n\ncd", 3, []string{"ab", "", "cd"}},
		{"abcde", 5, []string{"abcde", ""}},
		{"", 3, []string{""}},
	} {
		e := New()
		e.SetSize(tc.width, len(tc.want))
		e.SetWrapMode(WrapWord)
		e.SetPlaceholder(tc.text)
		e.Blur()
		if got := strings.Split(e.View(Options{}), "\n"); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("placeholder %q = %#v; want %#v", tc.text, got, tc.want)
		}
		if e.Value() != "" || len(e.undo) != 0 || e.cursor != 0 {
			t.Fatal("placeholder changed document")
		}
	}
	e := New()
	e.SetSize(10, 3)
	e.SetPlaceholder("hello world again")
	e.Blur()
	before := e.View(Options{})
	if before != "hello worl\nd again\n" {
		t.Fatalf("default placeholder changed: %q", before)
	}
	e.SetWrapMode(WrapWord)
	if e.View(Options{}) == before {
		t.Fatal("placeholder ignored word mode")
	}
	e.SetWrapMode(WrapCharacter)
	if e.View(Options{}) != before {
		t.Fatal("character placeholder did not restore")
	}
	e.SetWrapMode(WrapWord)
	e.Focus()
	if !strings.HasPrefix(e.View(Options{Cursor: "|"}), "|ello ") {
		t.Fatal("focused placeholder cursor changed")
	}
}

func TestWordWrapLayoutInvariants(t *testing.T) {
	// A deterministic cross-product catches lost/duplicated whitespace and bad
	// cursor mappings across many widths without relying on the layout algorithm.
	for _, text := range []string{"a  b cdefgh ij\n\nkl ", "日本語 abc 界界 \n🙂 hi", "   \n", strings.Repeat("one two three! ", 8)} {
		for width := 1; width <= 18; width++ {
			e := New()
			e.SetSize(width, 4)
			e.SetValue(text)
			e.SetWrapMode(WrapWord)
			lines := e.visualLines()
			position := 0
			for row, line := range lines {
				if line.start != position || line.end < line.start {
					t.Fatal("gap or overlap in visual rows")
				}
				cells := e.cellWidth(line.start, line.end)
				if cells > width && line.end-line.start != 1 {
					t.Fatalf("row %d exceeds width %d", row, width)
				}
				position = line.end
				if row+1 < len(lines) && lines[row+1].start == position+1 {
					if e.text[position] != '\n' {
						t.Fatal("skipped a non-newline rune")
					}
					position++
				}
			}
			if position != len(e.text) {
				t.Fatal("layout omitted document tail")
			}
			for index := 0; index <= len(e.text); index++ {
				row, col := e.visualPosition(index)
				if got := e.indexAtVisual(row, col); got != index {
					t.Fatalf("width %d index %d mapped back to %d", width, index, got)
				}
			}
			apply(&e, tea.KeyMsg{Type: tea.KeyCtrlHome})
			e.Blur()
			rendered := strings.Split(ansi.Strip(e.View(Options{})), "\n")
			for row := range rendered {
				if row < len(lines) && rendered[row] != string(e.text[lines[row].start:lines[row].end]) {
					t.Fatal("rendered row differs from layout")
				}
			}
			if e.Value() != text {
				t.Fatal("layout changed buffer")
			}
		}
	}
}

func TestWordWrapSearchAndVim(t *testing.T) {
	for _, mode := range []InputMode{ModePlain, ModeVim} {
		e := New()
		e.SetSize(9, 5)
		e.SetValue("one word two\nthree word four")
		e.SetWrapMode(WrapWord)
		e.SetInputMode(mode)
		e.OpenReplace()
		e.SetSearchQuery("word")
		e.SetReplacement("new words")
		if !e.FindNext() {
			t.Fatal("search failed to advance")
		}
		match := e.Search().Matches[e.Search().Current-1]
		if e.cursor != match.Start || string(e.text[match.Start:match.End]) != "word" {
			t.Fatal("search selection broke across word wraps")
		}
		e.SetSize(6, 6)
		e.SetWrapMode(WrapCharacter)
		e.SetWrapMode(WrapWord)
		assertWordWrapCaretVisible(t, e)
		if e.ReplaceAll() != 2 {
			t.Fatal("replace all did not find both matches")
		}
		e.CloseSearch()
		if !e.Undo() || e.Value() != "one word two\nthree word four" {
			t.Fatal("search replacement undo changed with wrapping")
		}
		if mode == ModeVim {
			keys(&e, "ggvj")
			if e.SelectedText() == "" {
				t.Fatal("Vim visual movement lost selection")
			}
		}
	}
}

var wrapBenchmarkLines []visualLine

func BenchmarkWrapLayout(b *testing.B) {
	for _, paragraphs := range []int{64, 1024} {
		for _, mode := range []WrapMode{WrapCharacter, WrapWord} {
			b.Run(fmt.Sprintf("mode=%d/paragraphs=%d", mode, paragraphs), func(b *testing.B) {
				e := New()
				e.SetSize(72, 24)
				e.SetWrapMode(mode)
				e.SetValue(strings.Repeat("A paragraph with ordinary words, Unicode 界界, and a long_unbroken_word_to_wrap_naturally.\n", paragraphs))
				b.SetBytes(int64(len(e.Value())))
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					wrapBenchmarkLines = e.visualLines()
				}
			})
		}
	}
}

func TestWordWrapNavigationAfterWidthChange(t *testing.T) {
	e := New()
	e.SetSize(6, 3)
	e.SetWrapMode(WrapWord)
	e.SetValue("aaaa b cccc dddd\nlast")
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlHome})
	for i := 0; i < 4; i++ {
		apply(&e, tea.KeyMsg{Type: tea.KeyRight})
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyDown})
	if e.cursor != 6 {
		t.Fatal("short-row fixture")
	}
	e.SetSize(10, 3)
	apply(&e, tea.KeyMsg{Type: tea.KeyDown})
	if e.cursor != 13 {
		t.Fatalf("Down used a column from before reflow: got %d, want 13", e.cursor)
	}
	for _, width := range []int{-10, 0, 1, 3, 10} {
		e.SetSize(width, 0)
		assertWordWrapCaretVisible(t, e)
		if e.Value() != "aaaa b cccc dddd\nlast" {
			t.Fatal("resize changed value")
		}
	}
}

func TestWordWrapUnicodeVerticalMovement(t *testing.T) {
	e := New()
	e.SetSize(6, 3)
	e.SetWrapMode(WrapWord)
	e.SetValue("界界 a 界界 end")
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlHome})
	apply(&e, tea.KeyMsg{Type: tea.KeyRight}) // column 2 after the first wide rune
	apply(&e, tea.KeyMsg{Type: tea.KeyShiftDown})
	row, col := e.visualPosition(e.cursor)
	if row != 1 || col != 1 || e.cursor != 4 || e.SelectedText() != "界 a" {
		t.Fatalf("wide movement/selection: cursor %d at %d/%d, selected %q", e.cursor, row, col, e.SelectedText())
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyDown})
	row, col = e.visualPosition(e.cursor)
	if row != 2 || col != 2 {
		t.Fatalf("preferred terminal-cell column lost: %d/%d", row, col)
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyUp})
	apply(&e, tea.KeyMsg{Type: tea.KeyUp})
	if e.cursor != 1 {
		t.Fatal("Up did not return to the original wide-rune column")
	}
}

func TestWordWrapSelectionAndCursorRendering(t *testing.T) {
	e := New()
	e.SetSize(6, 10)
	e.SetWrapMode(WrapWord)
	e.SetValue("aaaa b cccc dddd\nlast")
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlHome})
	for i := 0; i < 4; i++ {
		apply(&e, tea.KeyMsg{Type: tea.KeyRight})
	}
	for i := 0; i < 10; i++ {
		apply(&e, tea.KeyMsg{Type: tea.KeyShiftRight})
	}
	selected, cursor := "", ""
	e.View(Options{
		Selected:   func(s string) string { selected += s; return s },
		CursorRune: func(s string) string { cursor += s; return s },
	})
	if selected != e.SelectedText() || cursor != "d" {
		t.Fatalf("rendered selection %q, cursor %q", selected, cursor)
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyHome})
	for i := 0; i < 6; i++ {
		apply(&e, tea.KeyMsg{Type: tea.KeyRight})
	}
	view := strings.Split(e.View(Options{Cursor: "|"}), "\n")
	if view[1] != "b|" {
		t.Fatalf("cursor not rendered on the short visual row: %#v", view)
	}
}
