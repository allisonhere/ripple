# ripple

A keyboard-first, soft-wrapping multi-line text editor component for
[Bubble Tea](https://github.com/charmbracelet/bubbletea).

`ripple` is a self-contained editing surface — selection, undo/redo,
system-clipboard copy/cut/paste, word movement, and grapheme-width-aware soft
wrap — with no opinion about how the host frames or styles it. It was extracted
from [TideMail](https://github.com/allisonhere/tidemail)'s compose editor.

## Install

```bash
go get github.com/allisonhere/ripple
```

## Usage

Embed `ripple.Model` in your own model, size it, route messages through
`Update`, and render with `View`:

```go
type app struct{ ed ripple.Model }

func (a app) Init() tea.Cmd { return nil }

func (a app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.ed.SetSize(msg.Width, msg.Height)
		return a, nil
	case tea.KeyMsg:
		if msg.Type == tea.KeyEsc {
			return a, tea.Quit
		}
	}
	var cmd tea.Cmd
	a.ed, cmd = a.ed.Update(msg)
	return a, cmd
}

func (a app) View() string {
	return a.ed.View(ripple.Options{Cursor: "█"})
}
```

`Update` returns the updated model and a command, like any Bubble Tea component.

If your host has textarea-style separate sizing hooks, use `SetWidth` and
`SetHeight`; they preserve the other dimension and avoid temporary one-row
viewport jumps while a compose surface is being laid out. Prefer `SetSize` when
both dimensions are known together.

## Clipboard

Copy/cut/paste emit their clipboard side effect as a `tea.Cmd`, so it travels
back with the model. Provide a backend with `SetClipboard`; without one those
keys are inert:

```go
type osClipboard struct{}

func (osClipboard) Read() (string, error)  { return clipboard.ReadAll() }
func (osClipboard) Write(s string) error   { return clipboard.WriteAll(s) }

ed.SetClipboard(osClipboard{})
```

`ctrl+v` reads the clipboard and feeds the text back through `Update` as a
`ripple.PasteMsg`; forward all messages to the focused editor and it is handled
for you.

## Wrapping

The editor owns wrapping: it fits every visual line to the width passed to
`SetSize`. **Do not re-wrap the string returned by `View`** — measuring it with
a different width model can disagree on wide runes and overflow. Clip or pad
each line to the configured width instead.

## Keys

| Action | Default |
|--------|---------|
| Move | arrows; `ctrl`+arrows by word; home/end; ctrl+home/end |
| Select | `shift`+movement; `ctrl+shift`+arrows by word |
| Select all | `ctrl+a` |
| Undo / redo | `ctrl+z` / `ctrl+y` |
| Copy / cut / paste | `ctrl+c` / `ctrl+x` / `ctrl+v` |
| Page | pgup / pgdn |

Editing commands are configurable via `KeyMap` and `SetKeyMap`. Text input and
cursor movement are fixed.

## Vim mode

Modal (vim-style) editing is opt-in — the default is conventional insert-only
editing, so existing callers are unaffected:

```go
ed.SetInputMode(ripple.ModeVim) // ModePlain (default) restores insert-only
```

It adds Normal, Insert, Visual, Visual-line, and a `:` command line. `Mode()`
returns the active sub-mode label (`"NORMAL"`, `"INSERT"`, …) for a status
indicator, and `CommandLine()` returns the `:` text being typed.

| Group | Keys |
|-------|------|
| Modes | `i a o O I A` enter Insert; `v` / `V` visual; `:` command; `Esc` leaves |
| Motions | `h j k l`, `w b e`, `0 ^ $`, `gg G`, counts (`3j`); arrows also move |
| Edits | `x`, `dd`, `yy`, `p`/`P`, `D`, `C`, `s`; `d`/`c`/`y` + motion (`dw`, `cw`, `d$`, …) |
| History | `u` undo, `ctrl+r` redo |

**Host intents.** The editor stays content-agnostic: `:w` / `:wq` / `:x` emit a
`ripple.SubmitMsg` and `:q` (or a second `Esc` from Normal mode) emits a
`ripple.CancelMsg`, both delivered as ordinary messages via the command `Update`
returns. The host decides what "submit" and "cancel" mean:

```go
case ripple.SubmitMsg:
	return a, a.send()   // e.g. send the message
case ripple.CancelMsg:
	return a, a.close()  // e.g. confirm-and-close
```

**Clipboard.** In vim mode `y`/`d`/`c`/`x` mirror to the wired clipboard and
`p`/`P` read it (vim's `unnamedplus`), so yank/paste and `ctrl+c`/`ctrl+v` share
one buffer; the in-editor register is the fallback when no clipboard is wired.

**Cursor.** Set `Options.CursorRune` to style the character under the caret in
place (a block cursor that still shows its glyph) for Normal/Visual mode, while
Insert can keep a bar via `Options.Cursor`.

A runnable harness lives in `cmd/rippletest` (`go run ./cmd/rippletest`; `F2`
toggles vim, `Ctrl+Q` quits).

## License

MIT (see repository).
