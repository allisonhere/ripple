package ripple

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// vimEditor returns a vim-mode editor preloaded with s, caret at the document
// start in Normal mode (SetValue leaves the caret at the end, so we rewind).
func vimEditor(s string) Model {
	e := New()
	e.SetValue(s)
	e.SetInputMode(ModeVim)
	e.cursor = 0
	e.vimClampNormal()
	return e
}

// keys feeds a string as individual rune keystrokes (Normal-mode commands).
func keys(e *Model, s string) tea.Cmd {
	var last tea.Cmd
	for _, r := range s {
		if r == ' ' {
			last = apply(e, tea.KeyMsg{Type: tea.KeySpace})
			continue
		}
		last = apply(e, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return last
}

func esc(e *Model) tea.Cmd   { return apply(e, tea.KeyMsg{Type: tea.KeyEsc}) }
func enter(e *Model) tea.Cmd { return apply(e, tea.KeyMsg{Type: tea.KeyEnter}) }

func TestVimModeIndicator(t *testing.T) {
	e := vimEditor("hello")
	if got := e.Mode(); got != "NORMAL" {
		t.Fatalf("initial mode = %q, want NORMAL", got)
	}
	keys(&e, "i")
	if got := e.Mode(); got != "INSERT" {
		t.Fatalf("after i, mode = %q, want INSERT", got)
	}
	esc(&e)
	if got := e.Mode(); got != "NORMAL" {
		t.Fatalf("after esc, mode = %q, want NORMAL", got)
	}
	// A plain-mode editor reports no mode.
	p := New()
	if got := p.Mode(); got != "" {
		t.Fatalf("plain mode = %q, want empty", got)
	}
}

func TestVimInsertEntryAndEsc(t *testing.T) {
	e := vimEditor("ac")
	// i types before the caret: caret at 'a', insert "X" → "Xac"? vim 'i' inserts
	// before cursor; caret on 'a' → "X" then "ac".
	keys(&e, "i")
	keys(&e, "b") // KeyRunes 'b' in insert mode types
	if got := e.Value(); got != "bac" {
		t.Fatalf("after i+b: %q, want %q", got, "bac")
	}
	esc(&e)
	if e.Mode() != "NORMAL" {
		t.Fatalf("expected Normal after esc")
	}
}

func TestVimAppend(t *testing.T) {
	e := vimEditor("ac")
	keys(&e, "a") // append after 'a'
	keys(&e, "b") // type b
	if got := e.Value(); got != "abc" {
		t.Fatalf("after a+b: %q, want %q", got, "abc")
	}
}

func TestVimMotionsHL(t *testing.T) {
	e := vimEditor("hello")
	keys(&e, "ll") // right twice → index 2
	if e.cursor != 2 {
		t.Fatalf("after ll cursor = %d, want 2", e.cursor)
	}
	keys(&e, "h") // left → 1
	if e.cursor != 1 {
		t.Fatalf("after h cursor = %d, want 1", e.cursor)
	}
}

func TestVimDollarAndZero(t *testing.T) {
	e := vimEditor("hello")
	keys(&e, "$")
	if e.cursor != 4 { // Normal mode caret rests on last char, not after
		t.Fatalf("after $ cursor = %d, want 4", e.cursor)
	}
	keys(&e, "0")
	if e.cursor != 0 {
		t.Fatalf("after 0 cursor = %d, want 0", e.cursor)
	}
}

func TestVimWordMotion(t *testing.T) {
	e := vimEditor("foo bar baz")
	keys(&e, "w")
	if e.cursor != 4 {
		t.Fatalf("after w cursor = %d, want 4 (start of bar)", e.cursor)
	}
	keys(&e, "w")
	if e.cursor != 8 {
		t.Fatalf("after ww cursor = %d, want 8 (start of baz)", e.cursor)
	}
	keys(&e, "b")
	if e.cursor != 4 {
		t.Fatalf("after b cursor = %d, want 4", e.cursor)
	}
	keys(&e, "e")
	if e.cursor != 6 {
		t.Fatalf("after e cursor = %d, want 6 (end of bar)", e.cursor)
	}
}

func TestVimCountedMotion(t *testing.T) {
	e := vimEditor("abcdef")
	keys(&e, "3l")
	if e.cursor != 3 {
		t.Fatalf("after 3l cursor = %d, want 3", e.cursor)
	}
}

func TestVimGotoLines(t *testing.T) {
	e := vimEditor("one\ntwo\nthree")
	keys(&e, "G")
	r, _ := e.logicalLineRange(e.cursor)
	if string(e.text[r:r+5]) != "three" {
		t.Fatalf("after G not on last line, line starts %q", string(e.text[r:]))
	}
	keys(&e, "gg")
	if e.cursor != 0 {
		t.Fatalf("after gg cursor = %d, want 0", e.cursor)
	}
}

func TestVimDeleteChar(t *testing.T) {
	e := vimEditor("hello")
	keys(&e, "x")
	if got := e.Value(); got != "ello" {
		t.Fatalf("after x: %q, want ello", got)
	}
	keys(&e, "2x")
	if got := e.Value(); got != "lo" {
		t.Fatalf("after 2x: %q, want lo", got)
	}
}

func TestVimDeleteLine(t *testing.T) {
	e := vimEditor("one\ntwo\nthree")
	keys(&e, "dd")
	if got := e.Value(); got != "two\nthree" {
		t.Fatalf("after dd: %q, want two\\nthree", got)
	}
}

func TestVimDeleteWord(t *testing.T) {
	e := vimEditor("foo bar baz")
	keys(&e, "dw")
	if got := e.Value(); got != "bar baz" {
		t.Fatalf("after dw: %q, want 'bar baz'", got)
	}
}

func TestVimDeleteToEnd(t *testing.T) {
	e := vimEditor("hello world")
	keys(&e, "wd$") // move to 'world', delete to end
	if got := e.Value(); got != "hello " {
		t.Fatalf("after wd$: %q, want 'hello '", got)
	}
}

func TestVimChangeWord(t *testing.T) {
	e := vimEditor("foo bar")
	keys(&e, "cw")
	if e.Mode() != "INSERT" {
		t.Fatalf("cw should enter insert, mode=%q", e.Mode())
	}
	keys(&e, "X")
	if got := e.Value(); got != "X bar" {
		t.Fatalf("after cwX: %q, want 'X bar'", got)
	}
}

func TestVimYankPaste(t *testing.T) {
	e := vimEditor("ab")
	// yl yanks 'a' (caret stays on 'a'); p pastes it after → "aab".
	keys(&e, "ylp")
	if got := e.Value(); got != "aab" {
		t.Fatalf("after ylp: %q, want 'aab'", got)
	}
}

func TestVimYankLinePaste(t *testing.T) {
	e := vimEditor("one\ntwo")
	keys(&e, "yyp") // yank line one, paste below
	if got := e.Value(); got != "one\none\ntwo" {
		t.Fatalf("after yyp: %q", got)
	}
}

func TestVimOpenLine(t *testing.T) {
	e := vimEditor("one\ntwo")
	keys(&e, "o")
	keys(&e, "x")
	if got := e.Value(); got != "one\nx\ntwo" {
		t.Fatalf("after o+x: %q, want 'one\\nx\\ntwo'", got)
	}
	e2 := vimEditor("one")
	keys(&e2, "O")
	keys(&e2, "z")
	if got := e2.Value(); got != "z\none" {
		t.Fatalf("after O+z: %q, want 'z\\none'", got)
	}
}

func TestVimUndoRedo(t *testing.T) {
	e := vimEditor("hello")
	keys(&e, "x") // delete 'h'
	if e.Value() != "ello" {
		t.Fatalf("setup: %q", e.Value())
	}
	keys(&e, "u")
	if got := e.Value(); got != "hello" {
		t.Fatalf("after u: %q, want hello", got)
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlR})
	if got := e.Value(); got != "ello" {
		t.Fatalf("after ctrl+r: %q, want ello", got)
	}
}

func TestVimVisualDelete(t *testing.T) {
	e := vimEditor("hello")
	keys(&e, "vll") // select h,e,l (3 chars, inclusive caret on 3rd)
	keys(&e, "d")
	if got := e.Value(); got != "lo" {
		t.Fatalf("after vlld: %q, want 'lo'", got)
	}
}

func TestVimVisualLineDelete(t *testing.T) {
	e := vimEditor("one\ntwo\nthree")
	keys(&e, "Vd")
	if got := e.Value(); got != "two\nthree" {
		t.Fatalf("after Vd: %q, want 'two\\nthree'", got)
	}
}

func TestVimCommandSubmit(t *testing.T) {
	e := vimEditor("body")
	keys(&e, ":")
	if e.Mode() != "COMMAND" {
		t.Fatalf("expected COMMAND mode, got %q", e.Mode())
	}
	keys(&e, "wq")
	cmd := enter(&e)
	if cmd == nil {
		t.Fatal("expected a command from :wq")
	}
	if _, ok := cmd().(SubmitMsg); !ok {
		t.Fatalf(":wq should emit SubmitMsg, got %T", cmd())
	}
	if e.Mode() != "NORMAL" {
		t.Fatalf("after :wq mode = %q, want NORMAL", e.Mode())
	}
}

func TestVimCommandQuit(t *testing.T) {
	e := vimEditor("body")
	keys(&e, ":q")
	cmd := enter(&e)
	if cmd == nil {
		t.Fatal("expected a command from :q")
	}
	if _, ok := cmd().(CancelMsg); !ok {
		t.Fatalf(":q should emit CancelMsg, got %T", cmd())
	}
}

func TestVimDoubleEscCancels(t *testing.T) {
	e := vimEditor("body")
	keys(&e, "i")  // insert
	cmd := esc(&e) // first esc → Normal, no intent
	if cmd != nil {
		t.Fatalf("first esc should not emit an intent, got %v", cmd())
	}
	cmd = esc(&e) // second esc in Normal → cancel
	if cmd == nil {
		t.Fatal("second esc should emit CancelMsg")
	}
	if _, ok := cmd().(CancelMsg); !ok {
		t.Fatalf("second esc should emit CancelMsg, got %T", cmd())
	}
}

func TestVimEscClearsPending(t *testing.T) {
	e := vimEditor("hello")
	keys(&e, "d")  // pending operator
	cmd := esc(&e) // clears pending, no cancel
	if cmd != nil {
		t.Fatalf("esc with pending operator should not cancel, got %v", cmd())
	}
	if e.vim.pending != 0 {
		t.Fatalf("pending operator not cleared")
	}
}

func TestVimPlainModeUnaffected(t *testing.T) {
	// Without SetInputMode, the editor types normally — vim keys are literal text.
	var e Model
	keys(&e, "dd")
	if got := e.Value(); got != "dd" {
		t.Fatalf("plain mode should type 'dd', got %q", got)
	}
}

func TestVimBlockCursorShowsGlyph(t *testing.T) {
	e := vimEditor("hi")
	e.SetSize(10, 2)
	// A block cursor (CursorRune) renders the character under the caret in place,
	// rather than blanking the cell. With caret on 'h', the rune reaches the func.
	var got string
	e.View(Options{CursorRune: func(s string) string { got = s; return "[" + s + "]" }})
	if got != "h" {
		t.Fatalf("CursorRune received %q, want \"h\" (the glyph under the block)", got)
	}
}

func TestVimVisualSelectionRange(t *testing.T) {
	e := vimEditor("hello")
	keys(&e, "v") // v alone highlights the single char under the caret
	s, en, ok := e.selectionForRender()
	if !ok || s != 0 || en != 1 {
		t.Fatalf("after v: %d,%d,%v want 0,1,true", s, en, ok)
	}
	keys(&e, "ll") // v + ll → inclusive "hel"
	s, en, ok = e.selectionForRender()
	if !ok || s != 0 || en != 3 {
		t.Fatalf("after vll: %d,%d,%v want 0,3,true", s, en, ok)
	}
}

func TestVimVisualLineSelectionRange(t *testing.T) {
	e := vimEditor("one\ntwo\nthree")
	keys(&e, "V") // whole first line highlights immediately (incl. newline cell)
	s, en, ok := e.selectionForRender()
	if !ok || s != 0 || en != 4 {
		t.Fatalf("after V: %d,%d,%v want 0,4,true", s, en, ok)
	}
}

func TestVimArrowKeysMove(t *testing.T) {
	e := vimEditor("hello")
	apply(&e, tea.KeyMsg{Type: tea.KeyRight})
	apply(&e, tea.KeyMsg{Type: tea.KeyRight})
	if e.cursor != 2 {
		t.Fatalf("after two Right arrows cursor = %d, want 2", e.cursor)
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyLeft})
	if e.cursor != 1 {
		t.Fatalf("after Left cursor = %d, want 1", e.cursor)
	}
}

func TestVimCursorRendersAtMovedPosition(t *testing.T) {
	// Prove the rendered block cursor follows hjkl, not just the cursor index.
	e := vimEditor("abc")
	e.SetSize(10, 1)
	mark := func() string {
		return e.View(Options{CursorRune: func(s string) string { return "<" + s + ">" }})
	}
	if got := mark(); got != "<a>bc" {
		t.Fatalf("at start render = %q, want <a>bc", got)
	}
	keys(&e, "l")
	if got := mark(); got != "a<b>c" {
		t.Fatalf("after l render = %q, want a<b>c", got)
	}
	keys(&e, "l")
	if got := mark(); got != "ab<c>" {
		t.Fatalf("after ll render = %q, want ab<c>", got)
	}
}

type fakeClip struct{ s string }

func (c *fakeClip) Read() (string, error) { return c.s, nil }
func (c *fakeClip) Write(s string) error  { c.s = s; return nil }

func vimEditorClip(s string, cb Clipboard) Model {
	e := New()
	e.SetValue(s)
	e.SetClipboard(cb)
	e.SetInputMode(ModeVim)
	e.cursor = 0
	e.vimClampNormal()
	return e
}

func TestVimYankSyncsToClipboard(t *testing.T) {
	cb := &fakeClip{}
	e := vimEditorClip("hello", cb)
	keys(&e, "yl") // yank 'h'
	if cb.s != "h" {
		t.Fatalf("yank should mirror to clipboard, got %q", cb.s)
	}
}

func TestVimPasteReadsClipboard(t *testing.T) {
	cb := &fakeClip{s: "world"} // as if copied from another app
	e := vimEditorClip("hi", cb)
	keys(&e, "p") // p pastes the system clipboard, not just the register
	if got := e.Value(); got != "hworldi" {
		t.Fatalf("p should paste clipboard: %q, want hworldi", got)
	}
}

func TestVimLinewiseClipboardRoundTrip(t *testing.T) {
	cb := &fakeClip{}
	e := vimEditorClip("one\ntwo", cb)
	keys(&e, "yy")
	if cb.s != "one\n" {
		t.Fatalf("linewise yank clipboard = %q, want \"one\\n\"", cb.s)
	}
	keys(&e, "G") // last line
	keys(&e, "p") // trailing newline → linewise paste below
	if got := e.Value(); got != "one\ntwo\none" {
		t.Fatalf("linewise p across clipboard: %q", got)
	}
}

func TestVimStartInsert(t *testing.T) {
	e := vimEditor("hi")
	if e.Mode() != "NORMAL" {
		t.Fatalf("vim editor should default to NORMAL, got %q", e.Mode())
	}
	e.StartInsert()
	if e.Mode() != "INSERT" {
		t.Fatalf("after StartInsert, mode = %q, want INSERT", e.Mode())
	}
	// In plain mode it's a no-op.
	p := New()
	p.StartInsert()
	if p.Mode() != "" {
		t.Fatalf("StartInsert in plain mode should be a no-op, got %q", p.Mode())
	}
}

func TestVimCtrlHomeEnd(t *testing.T) {
	e := vimEditor("one\ntwo\nthree")
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlEnd})
	// Normal mode rests the caret on the last character, not past it.
	if e.cursor != len(e.text)-1 {
		t.Fatalf("ctrl+end cursor = %d, want %d", e.cursor, len(e.text)-1)
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlHome})
	if e.cursor != 0 {
		t.Fatalf("ctrl+home cursor = %d, want 0", e.cursor)
	}
}
