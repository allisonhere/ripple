// Package ripple is a keyboard-first, soft-wrapping multi-line text editor
// component for [Bubble Tea] programs.
//
// It is a self-contained editing surface — selection, undo/redo, system-
// clipboard copy/cut/paste, word movement, and grapheme-width-aware soft wrap —
// with no opinion about how the host frames or styles it.
//
// # Usage
//
// Embed [Model] in your own model. Construct it with [New], size it with
// [Model.SetSize], route messages through [Model.Update], and render with
// [Model.View]:
//
//	type app struct{ ed ripple.Model }
//
//	func (a app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
//		var cmd tea.Cmd
//		a.ed, cmd = a.ed.Update(msg)
//		return a, cmd
//	}
//
//	func (a app) View() string {
//		return a.ed.View(ripple.Options{Cursor: "█"})
//	}
//
// Update follows the standard Bubble Tea component shape, returning the updated
// model and any command. Copy/cut/paste emit their clipboard side effect as a
// [tea.Cmd] so it travels back with the model; wire a backend with
// [Model.SetClipboard] (without one, those keys are inert).
//
// # Wrapping
//
// The editor owns wrapping: it fits every visual line to the width given to
// [Model.SetSize] using terminal cell widths. Do not re-wrap the string
// returned by [Model.View] — measuring it with a different width model can
// disagree on wide runes and overflow. Clip/pad to the configured width
// instead.
//
// # Keys
//
// Editing commands (undo/redo, select-all, copy/cut/paste) are configurable via
// [KeyMap] and [Model.SetKeyMap]. Text input and cursor movement (arrows, word
// jumps with ctrl, home/end, page up/down, and shift selection variants) are
// fixed.
//
// # Vim mode
//
// Modal (vim-style) editing is available as an opt-in via [Model.SetInputMode]
// with [ModeVim]; the default [ModePlain] is conventional insert-only editing,
// so existing callers are unaffected. Vim mode adds Normal, Insert, Visual, and
// Visual-line sub-modes with the common Phase-1 grammar — motions (h/j/k/l,
// w/b/e, 0/^/$, gg/G, numeric counts), insert entry (i/a/o/O/I/A), edits
// (x/dd/yy/p/P, the d/c/y operators with motions like dw/cw/d$), visual v/V, and
// u / ctrl+r undo-redo. [Model.Mode] returns a label ("NORMAL", "INSERT", …) for
// a status indicator, and [Model.CommandLine] returns the ":" line being typed.
//
// Because ripple is content-agnostic, the ":" command line surfaces host intents
// rather than acting itself: ":w"/":wq"/":x" emit a [SubmitMsg] and ":q" (or a
// second Esc from Normal mode) emits a [CancelMsg], both delivered as ordinary
// messages through the command [Model.Update] returns. The host decides what
// "submit" and "cancel" mean. Render a vim block cursor with [Options.CursorRune].
//
// [Bubble Tea]: https://github.com/charmbracelet/bubbletea
package ripple
