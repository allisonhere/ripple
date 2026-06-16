package ripple

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// InputMode selects how the editor interprets keystrokes.
type InputMode int

const (
	// ModePlain is conventional insert-only editing: every printable key types,
	// arrows move, and the editing-command KeyMap handles undo/copy/etc. This is
	// the default and the only mode older callers ever saw.
	ModePlain InputMode = iota
	// ModeVim enables modal (vim-style) editing: a Normal mode for motions and
	// operators, an Insert mode that behaves like ModePlain, Visual selection,
	// and a ":" command line. Opt in with SetInputMode.
	ModeVim
)

// vimMode is the active sub-mode while InputMode is ModeVim. The zero value is
// Normal, so a freshly switched-on editor starts in Normal mode.
type vimMode int

const (
	vimNormal vimMode = iota
	vimInsert
	vimVisual
	vimVisualLine
	vimCommand
)

// SubmitMsg and CancelMsg are intents the editor emits in vim mode so the host
// can act on them. The editor stays content-agnostic: ":w"/":wq"/":x" emit a
// SubmitMsg and ":q" (or a second Esc in Normal mode) emits a CancelMsg; what
// "submit" and "cancel" mean — send an email, close a dialog — is the host's
// decision. They arrive as ordinary tea.Msgs via the command Update returns.
type (
	SubmitMsg struct{}
	CancelMsg struct{}
)

// vimState holds modal-editing state. The zero value is a clean Normal mode with
// no pending operator, count, or register.
type vimState struct {
	mode     vimMode
	count    int    // pending numeric count being typed (0 means "none")
	pending  rune   // pending operator awaiting a motion: 'd', 'c', 'y' (0 = none)
	gPending bool   // a 'g' prefix was seen (for the gg motion)
	register string // unnamed register holding the last yank/delete/x text
	regLine  bool   // the register holds whole line(s) → linewise paste
	cmdline  []rune // ":" command-line buffer (without the leading colon)
}

// SetInputMode switches between conventional and vim editing. Switching to vim
// starts in Normal mode and clears any pending modal state; switching back to
// plain leaves the document untouched. Default is ModePlain, so existing callers
// are unaffected.
func (m *Model) SetInputMode(mode InputMode) {
	m.inputMode = mode
	m.vim = vimState{mode: vimNormal}
	if mode == ModeVim {
		m.vimClampNormal()
	}
}

// InputMode reports the current editing mode.
func (m Model) InputMode() InputMode { return m.inputMode }

// StartInsert puts a vim-mode editor into Insert mode (a no-op in plain mode).
// Hosts that focus the editor for immediate typing — e.g. a compose box where
// the user expects to type right away and press Esc to reach Normal — call this
// after enabling vim so the editor doesn't sit in Normal mode swallowing the
// first keystrokes (and the first Esc).
func (m *Model) StartInsert() {
	if m.inputMode == ModeVim {
		m.vim.mode = vimInsert
		m.vim.pending, m.vim.count, m.vim.gPending = 0, 0, false
	}
}

// Mode returns a short label for the active vim sub-mode — "NORMAL", "INSERT",
// "VISUAL", "V-LINE", or "COMMAND" — for the host to render as a status
// indicator. It returns "" in plain mode, where there is no mode to show.
func (m Model) Mode() string {
	if m.inputMode != ModeVim {
		return ""
	}
	switch m.vim.mode {
	case vimInsert:
		return "INSERT"
	case vimVisual:
		return "VISUAL"
	case vimVisualLine:
		return "V-LINE"
	case vimCommand:
		return "COMMAND"
	default:
		return "NORMAL"
	}
}

// CommandLine returns the text of the ":" command line currently being typed
// (including the leading colon), or "" when the editor is not in command mode.
// The host renders this on its status line so the user sees what they type.
func (m Model) CommandLine() string {
	if m.inputMode != ModeVim || m.vim.mode != vimCommand {
		return ""
	}
	return ":" + string(m.vim.cmdline)
}

// handleVim is the entry point for vim-mode key handling, dispatching by
// sub-mode. Insert mode delegates to the plain key path (plus Esc); the others
// have dedicated handlers.
func (m Model) handleVim(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch m.vim.mode {
	case vimInsert:
		return m.vimInsertKey(msg)
	case vimCommand:
		return m.vimCommandKey(msg)
	default:
		return m.vimNormalKey(msg)
	}
}

// vimInsertKey handles Insert mode: Esc returns to Normal (pulling the caret
// back onto a character, as vim does); every other key is conventional editing.
func (m Model) vimInsertKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc {
		m.vim.mode = vimNormal
		// Leaving insert moves the caret one cell left, back onto the last typed
		// character, unless it is already at the start of the line.
		if ls, _ := m.logicalLineRange(m.cursor); m.cursor > ls {
			m.cursor--
		}
		m.vimClampNormal()
		m.ensureCursorVisible()
		return m, nil
	}
	return m.handlePlainKey(msg)
}

// vimCommandKey handles the ":" command line: runes accumulate, Enter runs the
// command, Backspace deletes (and exits when empty), Esc cancels.
func (m Model) vimCommandKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.vim.mode = vimNormal
		m.vim.cmdline = nil
	case tea.KeyEnter:
		cmd := strings.TrimSpace(string(m.vim.cmdline))
		m.vim.cmdline = nil
		m.vim.mode = vimNormal
		return m.execVimCommand(cmd)
	case tea.KeyBackspace:
		if len(m.vim.cmdline) > 0 {
			m.vim.cmdline = m.vim.cmdline[:len(m.vim.cmdline)-1]
		} else {
			m.vim.mode = vimNormal
		}
	case tea.KeySpace:
		m.vim.cmdline = append(m.vim.cmdline, ' ')
	case tea.KeyRunes:
		m.vim.cmdline = append(m.vim.cmdline, msg.Runes...)
	}
	return m, nil
}

// execVimCommand maps a typed ex-command to a host intent. Write-family commands
// (w, wq, x) emit SubmitMsg; quit (q) emits CancelMsg. A trailing "!" is
// accepted and ignored. Unknown commands are silently dropped — the editor has
// nothing to report and the host stays in compose.
func (m Model) execVimCommand(cmd string) (Model, tea.Cmd) {
	switch strings.TrimSuffix(cmd, "!") {
	case "q":
		return m, func() tea.Msg { return CancelMsg{} }
	case "w", "wq", "x":
		return m, func() tea.Msg { return SubmitMsg{} }
	}
	return m, nil
}

// vimNormalKey handles Normal, Visual, and Visual-line modes (they share motions
// and differ only in what operators act on). It threads the pending count,
// operator, and "g" prefix through successive keystrokes.
func (m Model) vimNormalKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	// Esc clears any partial command first; only a "clean" Esc cancels. This is
	// what makes Esc-mashing safe and gives the double-Esc-to-close behavior:
	// the first Esc out of Insert lands in Normal, a second clean Esc cancels.
	if msg.Type == tea.KeyEsc {
		if m.vim.pending != 0 || m.vim.count != 0 || m.vim.gPending {
			m.vim.pending, m.vim.count, m.vim.gPending = 0, 0, false
			return m, nil
		}
		if m.vim.mode == vimVisual || m.vim.mode == vimVisualLine {
			m.vim.mode = vimNormal
			m.ClearSelection()
			m.vimClampNormal()
			return m, nil
		}
		return m, func() tea.Msg { return CancelMsg{} }
	}

	// ctrl+r redoes (vim's redo); it arrives as a control key, not a rune.
	if msg.Type == tea.KeyCtrlR {
		m.Redo()
		m.vimClampNormal()
		m.ensureCursorVisible()
		return m, nil
	}

	// ctrl+v pastes the system clipboard at the caret even in Normal mode (the
	// register-based p/P still work for in-editor yanks).
	if msg.Type == tea.KeyCtrlV {
		return m, m.pasteCmd()
	}

	// Arrow keys and Home/End/PageUp/Down move in Normal and Visual modes too
	// (vim accepts them alongside hjkl); in Visual they extend the selection.
	selecting := m.vim.mode == vimVisual || m.vim.mode == vimVisualLine
	switch msg.Type {
	case tea.KeyLeft:
		m.moveHorizontal(-1, selecting)
	case tea.KeyRight:
		m.moveHorizontal(1, selecting)
	case tea.KeyUp:
		m.moveVertical(-1, selecting)
	case tea.KeyDown:
		m.moveVertical(1, selecting)
	case tea.KeyHome:
		m.moveLineBoundary(false, selecting)
	case tea.KeyEnd:
		m.moveLineBoundary(true, selecting)
	case tea.KeyCtrlHome:
		m.moveTo(0, selecting)
	case tea.KeyCtrlEnd:
		m.moveTo(len(m.text), selecting)
	case tea.KeyPgUp:
		m.moveVertical(-m.height, selecting)
	case tea.KeyPgDown:
		m.moveVertical(m.height, selecting)
	case tea.KeyRunes:
		break // handled below
	default:
		return m, nil
	}
	if msg.Type != tea.KeyRunes {
		m.vimClampNormal()
		m.ensureCursorVisible()
		return m, nil
	}

	// A bracketed (terminal) paste arrives as multi-rune input; insert it at the
	// caret rather than interpreting each character as a command.
	if msg.Paste {
		m.InsertString(string(msg.Runes))
		m.vimClampNormal()
		m.ensureCursorVisible()
		return m, nil
	}

	// Only single runes drive vim commands; ignore other multi-rune input.
	if len(msg.Runes) != 1 {
		return m, nil
	}
	r := msg.Runes[0]

	// Digits accumulate into a count, except a leading 0 is the line-start motion.
	if r >= '1' && r <= '9' || (r == '0' && m.vim.count > 0) {
		m.vim.count = m.vim.count*10 + int(r-'0')
		return m, nil
	}

	if m.vim.gPending {
		m.vim.gPending = false
		if r == 'g' { // gg → first line
			return m.applyMotionKey('g', true), nil
		}
		return m, nil
	}

	// An operator is pending: this key is its motion (or a doubled operator).
	if m.vim.pending != 0 {
		return m.applyPendingOperator(r)
	}

	return m.vimCommand(r)
}

// vimCommand dispatches a complete Normal/Visual command rune (no operator
// pending). It consumes and resets the pending count.
func (m Model) vimCommand(r rune) (Model, tea.Cmd) {
	count := m.takeCount()
	visual := m.vim.mode == vimVisual || m.vim.mode == vimVisualLine

	switch r {
	// --- mode entry -----------------------------------------------------------
	case 'i':
		return m.enterInsert(m.cursor), nil
	case 'a':
		pos := m.cursor
		if _, le := m.logicalLineRange(pos); pos < le {
			pos++ // append after the current character
		}
		return m.enterInsert(pos), nil
	case 'I':
		return m.enterInsert(m.firstNonBlank(m.cursor)), nil
	case 'A':
		_, le := m.logicalLineRange(m.cursor)
		return m.enterInsert(le), nil
	case 'o':
		return m.openLine(false), nil
	case 'O':
		return m.openLine(true), nil
	case 'v':
		return m.toggleVisual(vimVisual), nil
	case 'V':
		return m.toggleVisual(vimVisualLine), nil
	case ':':
		m.vim.mode = vimCommand
		m.vim.cmdline = nil
		return m, nil

	// --- operators ------------------------------------------------------------
	case 'd', 'c', 'y':
		if visual {
			return m.operateVisual(r), nil
		}
		m.vim.pending = r
		if count > 1 {
			m.vim.count = count // preserve count for the motion
		}
		return m, nil

	// --- standalone edits -----------------------------------------------------
	case 'x':
		return m.deleteChars(count, visual), nil
	case 'D':
		return m.deleteToLineEnd(), nil
	case 'C':
		m = m.deleteToLineEnd()
		return m.enterInsert(m.cursor), nil
	case 's':
		m = m.deleteChars(count, visual)
		return m.enterInsert(m.cursor), nil
	case 'p':
		return m.paste(true), nil
	case 'P':
		return m.paste(false), nil
	case 'u':
		m.Undo()
		m.vimClampNormal()
		return m, nil

	// --- motions --------------------------------------------------------------
	case 'g':
		m.vim.gPending = true
		return m, nil
	default:
		return m.applyMotionKeyN(r, count), nil
	}
}

// applyMotionKeyN runs a plain cursor motion count times (count defaults to 1).
func (m Model) applyMotionKeyN(r rune, count int) Model {
	if count < 1 {
		count = 1
	}
	selecting := m.vim.mode == vimVisual || m.vim.mode == vimVisualLine
	for i := 0; i < count; i++ {
		target, _, ok := m.motionTarget(r, 1)
		if !ok {
			break
		}
		m.moveTo(target, selecting)
	}
	m.vimClampNormal()
	m.ensureCursorVisible()
	return m
}

// applyMotionKey runs a single motion (used for gg, which carries its own
// target). selecting follows the current mode.
func (m Model) applyMotionKey(r rune, _ bool) Model {
	selecting := m.vim.mode == vimVisual || m.vim.mode == vimVisualLine
	if target, _, ok := m.motionTarget(r, 1); ok {
		m.moveTo(target, selecting)
	}
	m.vimClampNormal()
	m.ensureCursorVisible()
	return m
}

// motionTarget resolves a motion key to a destination index and its "wise"
// (false = charwise, true = linewise). ok is false for keys that are not
// motions. count repeats word/line motions.
func (m Model) motionTarget(r rune, count int) (target int, linewise bool, ok bool) {
	if count < 1 {
		count = 1
	}
	switch r {
	case 'h':
		t := m.cursor
		for i := 0; i < count && t > 0 && m.text[t-1] != '\n'; i++ {
			t--
		}
		return t, false, true
	case 'l':
		t := m.cursor
		_, le := m.logicalLineRange(t)
		for i := 0; i < count && t < le; i++ {
			t++
		}
		return t, false, true
	case 'j':
		return m.lineStep(count), true, true
	case 'k':
		return m.lineStep(-count), true, true
	case 'w':
		t := m.cursor
		for i := 0; i < count; i++ {
			t = m.wordForward(t)
		}
		return t, false, true
	case 'b':
		t := m.cursor
		for i := 0; i < count; i++ {
			t = m.wordBackward(t)
		}
		return t, false, true
	case 'e':
		t := m.cursor
		for i := 0; i < count; i++ {
			t = m.wordEnd(t)
		}
		return t, false, true
	case '0':
		ls, _ := m.logicalLineRange(m.cursor)
		return ls, false, true
	case '^':
		return m.firstNonBlank(m.cursor), false, true
	case '$':
		_, le := m.logicalLineRange(m.cursor)
		return le, false, true
	case 'G':
		return len(m.text), false, true
	case 'g': // gg
		return 0, false, true
	}
	return 0, false, false
}

// applyPendingOperator completes a pending operator (d/c/y) with motion key r,
// or with a doubled operator key (dd/cc/yy) for the whole current line.
func (m Model) applyPendingOperator(r rune) (Model, tea.Cmd) {
	op := m.vim.pending
	m.vim.pending = 0
	count := m.takeCount()

	// Doubled operator → linewise on count lines from the current line.
	if r == op || (op == 'd' && r == 'd') || (op == 'y' && r == 'y') || (op == 'c' && r == 'c') {
		start, end := m.lineSpan(count)
		return m.applyOperator(op, start, end, true), nil
	}

	if r == 'g' { // operator + gg handled simply as to-document-start, linewise
		// minimal support: dG/yG style is more common; treat 'g' here as no-op.
		return m, nil
	}

	// cw/cW act like ce in vim: change to the end of the word, not including the
	// trailing whitespace that w would sweep up.
	if op == 'c' && r == 'w' {
		r = 'e'
	}

	target, linewise, ok := m.motionTarget(r, count)
	if !ok {
		return m, nil // unknown motion cancels the operator
	}

	start, end := m.cursor, target
	if start > end {
		start, end = end, start
	}
	// 'e' is inclusive of the character under the target.
	if r == 'e' && end < len(m.text) {
		end++
	}
	// '$' includes the line's last character.
	if r == '$' {
		linewise = false
	}
	if linewise {
		start, end = m.lineRangeSpanning(start, end)
	}
	return m.applyOperator(op, start, end, linewise), nil
}

// applyOperator runs op over [start,end). Yank stores the text; delete and
// change remove it (change then enters insert). Linewise operations stash a
// trailing newline marker so a later p pastes on its own line.
func (m Model) applyOperator(op rune, start, end int, linewise bool) Model {
	if start < 0 {
		start = 0
	}
	if end > len(m.text) {
		end = len(m.text)
	}
	if end < start {
		end = start
	}
	text := string(m.text[start:end])
	// A linewise register holds line content without the trailing newline; p/P
	// re-add the newline when pasting it onto its own line.
	regText := text
	if linewise {
		regText = strings.TrimSuffix(regText, "\n")
	}

	switch op {
	case 'y':
		m.setRegister(regText, linewise)
		// Yank leaves the caret at the start of the yanked region.
		m.moveTo(start, false)
	case 'd', 'c':
		m.setRegister(regText, linewise)
		m.pushUndo(kindBoundary)
		m.text = append(m.text[:start], m.text[end:]...)
		m.cursor = start
		m.selAnchor = -1
		if op == 'c' {
			if linewise {
				// cc keeps an empty line to type into.
				m.insertRunes("\n")
				m.cursor--
			}
			m.vim.mode = vimInsert
		}
	}
	m.goalCol = -1
	if m.vim.mode != vimInsert {
		m.vimClampNormal()
	}
	m.clamp()
	m.ensureCursorVisible()
	return m
}

// operateVisual applies d/c/y to the active visual selection, then leaves visual
// mode (change stays in insert).
func (m Model) operateVisual(op rune) Model {
	linewise := m.vim.mode == vimVisualLine
	start, end, ok := m.selectionRange()
	if !ok {
		start, end = m.cursor, m.cursor
		if end < len(m.text) {
			end++
		}
	}
	if linewise {
		start, end = m.lineRangeSpanning(start, end)
	} else if end < len(m.text) {
		// Visual selection is inclusive of the character under the caret.
		end++
	}
	m.vim.mode = vimNormal
	return m.applyOperator(op, start, end, linewise)
}

// enterInsert positions the caret at pos and switches to Insert mode.
func (m Model) enterInsert(pos int) Model {
	m.moveTo(pos, false)
	m.vim.mode = vimInsert
	m.ensureCursorVisible()
	return m
}

// openLine inserts a blank line above or below the current line and enters
// insert at its start (vim's O and o).
func (m Model) openLine(above bool) Model {
	m.pushUndo(kindBoundary)
	ls, le := m.logicalLineRange(m.cursor)
	if above {
		m.cursor = ls
		m.insertRunes("\n")
		m.cursor = ls
	} else {
		m.cursor = le
		m.insertRunes("\n")
		// caret now sits just after the new newline, on the blank line
	}
	m.vim.mode = vimInsert
	m.ensureCursorVisible()
	return m
}

// toggleVisual enters the given visual mode, or returns to Normal if already in
// it. Entering anchors the selection at the caret.
func (m Model) toggleVisual(mode vimMode) Model {
	if m.vim.mode == mode {
		m.vim.mode = vimNormal
		m.ClearSelection()
		m.vimClampNormal()
		return m
	}
	m.vim.mode = mode
	m.selAnchor = m.cursor
	return m
}

// deleteChars implements x: delete count characters under/after the caret
// (charwise), or the selection in visual mode. Deleted text goes to the register.
func (m Model) deleteChars(count int, visual bool) Model {
	if count < 1 {
		count = 1
	}
	if visual {
		return m.operateVisual('d')
	}
	_, le := m.logicalLineRange(m.cursor)
	end := m.cursor + count
	if end > le {
		end = le
	}
	if end <= m.cursor {
		return m
	}
	m.setRegister(string(m.text[m.cursor:end]), false)
	m.pushUndo(kindBoundary)
	m.text = append(m.text[:m.cursor], m.text[end:]...)
	m.selAnchor = -1
	m.goalCol = -1
	m.vimClampNormal()
	m.clamp()
	m.ensureCursorVisible()
	return m
}

// deleteToLineEnd implements D: delete from the caret to the end of the line.
func (m Model) deleteToLineEnd() Model {
	_, le := m.logicalLineRange(m.cursor)
	if le <= m.cursor {
		return m
	}
	m.setRegister(string(m.text[m.cursor:le]), false)
	m.pushUndo(kindBoundary)
	m.text = append(m.text[:m.cursor], m.text[le:]...)
	m.selAnchor = -1
	m.goalCol = -1
	m.vimClampNormal()
	m.clamp()
	m.ensureCursorVisible()
	return m
}

// paste implements p (after) and P (before). When a clipboard is wired it pastes
// the system clipboard — so p matches ctrl+v and reflects external copies —
// detecting linewise content by a trailing newline; otherwise (or if the
// clipboard is empty) it falls back to the in-editor register so dd/p and yy/p
// keep working with no clipboard tool installed. Linewise content pastes on its
// own new line; charwise content inserts inline.
func (m Model) paste(after bool) Model {
	text, linewise := m.vim.register, m.vim.regLine
	if m.clipboard != nil {
		if t, err := m.clipboard.Read(); err == nil && t != "" {
			text = t
			linewise = strings.HasSuffix(t, "\n")
		}
	}
	if text == "" {
		return m
	}
	m.pushUndo(kindBoundary)
	if linewise {
		body := strings.TrimSuffix(text, "\n")
		ls, le := m.logicalLineRange(m.cursor)
		if after {
			m.cursor = le
			m.insertRunes("\n" + body)
			m.cursor = le + 1 // caret to start of pasted line
		} else {
			m.cursor = ls
			m.insertRunes(body + "\n")
			m.cursor = ls
		}
	} else {
		if after && m.cursor < len(m.text) {
			m.cursor++
		}
		m.insertRunes(text)
		m.cursor-- // leave caret on the last pasted character (vim behavior)
	}
	m.goalCol = -1
	m.vimClampNormal()
	m.clamp()
	m.ensureCursorVisible()
	return m
}

// selectionForRender returns the range View should highlight. In vim Visual it
// is inclusive of the character under the caret (charwise) or spans whole
// logical lines (linewise) — and is non-empty the instant v/V is pressed, before
// any motion. Outside vim Visual it defers to the ordinary (exclusive) selection
// so plain-mode shift-selection is unchanged.
func (m Model) selectionForRender() (int, int, bool) {
	if m.inputMode != ModeVim || (m.vim.mode != vimVisual && m.vim.mode != vimVisualLine) {
		return m.selectionRange()
	}
	if len(m.text) == 0 {
		return 0, 0, false
	}
	anchor := m.selAnchor
	if anchor < 0 {
		anchor = m.cursor
	}
	start, end := anchor, m.cursor
	if start > end {
		start, end = end, start
	}
	if m.vim.mode == vimVisualLine {
		start, end = m.lineRangeSpanning(start, end)
		return start, end, true
	}
	if end < len(m.text) { // charwise: include the character under the caret
		end++
	}
	return start, end, true
}

// setRegister stores text in the unnamed register, recording whether it is
// linewise (affects how p/P paste it). When a clipboard is wired it also mirrors
// the text to the system clipboard — so y/d/c/x and an external copy share one
// buffer (vim's "unnamedplus" behavior), and ctrl+c, y, ctrl+v, and p all agree.
// Linewise content carries a trailing newline so a later paste can detect it.
func (m *Model) setRegister(text string, linewise bool) {
	m.vim.register = text
	m.vim.regLine = linewise
	if m.clipboard != nil && text != "" {
		clip := text
		if linewise {
			clip += "\n"
		}
		_ = m.clipboard.Write(clip) //nolint:errcheck // best-effort OS clipboard sync
	}
}

// takeCount returns and clears the pending count, defaulting to 1.
func (m *Model) takeCount() int {
	c := m.vim.count
	m.vim.count = 0
	if c < 1 {
		return 1
	}
	return c
}

// vimClampNormal keeps the caret on a character in Normal/Visual mode — vim never
// rests the caret past the last character of a non-empty line. Insert and command
// modes allow the caret at end-of-line.
func (m *Model) vimClampNormal() {
	if m.inputMode != ModeVim {
		return
	}
	if m.vim.mode == vimInsert || m.vim.mode == vimCommand {
		return
	}
	ls, le := m.logicalLineRange(m.cursor)
	if le > ls && m.cursor == le {
		m.cursor = le - 1
	}
}

// lineStep moves the caret n logical lines (negative = up), keeping the column.
func (m Model) lineStep(n int) int {
	ls, _ := m.logicalLineRange(m.cursor)
	col := m.cursor - ls
	pos := m.cursor
	if n > 0 {
		for i := 0; i < n; i++ {
			_, le := m.logicalLineRange(pos)
			if le >= len(m.text) {
				break
			}
			pos = le + 1
		}
	} else {
		for i := 0; i < -n; i++ {
			s, _ := m.logicalLineRange(pos)
			if s == 0 {
				pos = 0
				break
			}
			pos = s - 1
		}
	}
	nls, nle := m.logicalLineRange(pos)
	target := nls + col
	if target > nle {
		target = nle
	}
	return target
}

// lineSpan returns the rune range covering count whole logical lines starting at
// the caret's line, including the trailing newline (so dd removes the line).
func (m Model) lineSpan(count int) (int, int) {
	if count < 1 {
		count = 1
	}
	start, _ := m.logicalLineRange(m.cursor)
	end := start
	for i := 0; i < count; i++ {
		_, le := m.logicalLineRange(end)
		end = le
		if end < len(m.text) {
			end++ // consume the newline
		} else {
			break
		}
	}
	return start, end
}

// lineRangeSpanning expands an arbitrary [a,b] range to cover the full logical
// lines it touches, including a trailing newline where present.
func (m Model) lineRangeSpanning(a, b int) (int, int) {
	if a > b {
		a, b = b, a
	}
	start, _ := m.logicalLineRange(a)
	_, end := m.logicalLineRange(b)
	if end < len(m.text) {
		end++ // include the newline
	}
	return start, end
}

// firstNonBlank returns the index of the first non-space character on the line
// containing index, or the line start if the line is all blank.
func (m Model) firstNonBlank(index int) int {
	ls, le := m.logicalLineRange(index)
	for i := ls; i < le; i++ {
		if m.text[i] != ' ' && m.text[i] != '\t' {
			return i
		}
	}
	return ls
}

// wordForward returns the index of the start of the next word after pos (vim w).
func (m Model) wordForward(pos int) int {
	n := len(m.text)
	if pos >= n {
		return n
	}
	if isWordRune(m.text[pos]) {
		for pos < n && isWordRune(m.text[pos]) {
			pos++
		}
	} else if !isSpace(m.text[pos]) {
		for pos < n && !isWordRune(m.text[pos]) && !isSpace(m.text[pos]) {
			pos++
		}
	}
	for pos < n && isSpace(m.text[pos]) {
		pos++
	}
	return pos
}

// wordBackward returns the index of the start of the word at or before pos (vim b).
func (m Model) wordBackward(pos int) int {
	if pos == 0 {
		return 0
	}
	pos--
	for pos > 0 && isSpace(m.text[pos]) {
		pos--
	}
	if pos == 0 {
		return 0
	}
	if isWordRune(m.text[pos]) {
		for pos > 0 && isWordRune(m.text[pos-1]) {
			pos--
		}
	} else {
		for pos > 0 && !isWordRune(m.text[pos-1]) && !isSpace(m.text[pos-1]) {
			pos--
		}
	}
	return pos
}

// wordEnd returns the index of the end of the next word from pos (vim e).
func (m Model) wordEnd(pos int) int {
	n := len(m.text)
	if pos >= n-1 {
		return maxInt(pos, n-1)
	}
	pos++
	for pos < n && isSpace(m.text[pos]) {
		pos++
	}
	if pos >= n {
		return n - 1
	}
	if isWordRune(m.text[pos]) {
		for pos < n-1 && isWordRune(m.text[pos+1]) {
			pos++
		}
	} else {
		for pos < n-1 && !isWordRune(m.text[pos+1]) && !isSpace(m.text[pos+1]) {
			pos++
		}
	}
	return pos
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' || r == '\n' }

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
