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
| Find / replace | `ctrl+f`; `ctrl+r` inside the bar (see [Search and replace](#search-and-replace)) |
| Next / previous match | `f3` / `shift+f3` |

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

## Syntax highlighting

ripple doesn't know about any particular language, but a host that does can
color the document with `Options.StyleKey` and `Options.Style`. `StyleKey`
classifies the rune at a document offset — token kind, color name, whatever the
host's tokenizer produces — and `View` batches consecutive same-keyed runes
into a single `Style` call, exactly the way `Options.Selected` already receives
a whole run instead of one rune at a time:

```go
ed.View(ripple.Options{
	Cursor: "█",
	StyleKey: func(offset int) string {
		return tokenKindAt(doc, offset) // e.g. "key", "string", "comment", ""
	},
	Style: func(key, text string) string {
		switch key {
		case "key":
			return keyStyle.Render(text)
		case "string":
			return stringStyle.Render(text)
		case "comment":
			return commentStyle.Render(text)
		default:
			return text
		}
	},
})
```

Runs also split wherever the selection state changes, so a `Style` call always
wraps a self-contained, uniformly-styled fragment — a full SGR reset per call
(what `lipgloss.Style.Render` already does) is safe and won't clobber a
selection or cursor layered on top of highlighted text.

## Search and replace

`Ctrl+F` opens the built-in Find bar. Matches update as you type, with a current
match count and highlights. `Enter` in Find or `F3` advances; `Shift+F3` goes
backward. Navigation wraps. A single-line selection seeds Find; otherwise the
last query is reused. `Esc` closes the bar and leaves the document at its current
match. Search itself never changes undo history.

Press `Ctrl+R` **inside the bar** to show/hide Replace. Opening it focuses Replacement immediately; hiding it
returns focus to Find. `Tab`/`Shift+Tab` cycle
Find, Replacement, Replace, All, Match case, and (when available) Regex. `Enter`
in Replacement or on Replace changes the current match and advances; Enter on
All replaces a snapshot of every match in one undo step. None of the controls
needs Tabbing onto: `Alt+Enter` replaces all from anywhere in the bar while
Replace is shown, `Alt+C` toggles match case, and `Alt+R` toggles regex when the
host allows it. `Options.SearchShowKeys` prints those keys on the controls
(`Replace ⏎`, `All ⌥⏎`, `Aa ⌥C`, `.* ⌥R`; ASCII names with `SearchPlain`). Inserted text is never
replaced again during that operation. Copy/cut act on the whole focused input;
paste inserts at that input's caret using the configured clipboard backend.

Search is literal and case-sensitive by default. Hosts can expose the advanced
regex toggle with `ed.SetRegexSearchEnabled(true)`; this **does not activate**
regex. The user switches the bar's `.*` toggle on to use Go regular expressions and
capture replacements (`$0` for the whole match, `$1`, `${name}`). Regex `^` and
`$` match each logical line by default; soft wrapping does not create boundaries.
Use `\A`/`\z` for document boundaries, or `(?-m)` to disable multiline mode.
For example, replace `^> ` with nothing to strip quote prefixes from every line.

In regex replacement text, `$$` produces a literal dollar: replacing `cat` with
`$$5` in `price cat` produces `price $5`. Unknown capture groups (such as `$5`
without a fifth group) show a replacement error and block both replacement
actions without changing the document or undo history. Valid optional groups
that did not match expand to empty. Names are greedy: `${1}suffix` means group 1
followed by `suffix`; `$1suffix` refers to a group named `1suffix`. Malformed
dollar references follow Go's literal-text behavior. `Search().Error` reports
pattern errors; `Search().ReplacementError` reports replacement errors without
preventing find/navigation. Empty queries and invalid patterns cannot
replace text. Zero-width regex matches are supported and navigation progresses
between document positions. In literal mode, dollar signs and backslashes in
replacement text stay literal. Matching spans explicit newlines, independently
of visual wrapping; hosts can supply multiline queries through `SetSearchQuery`.

Vim Normal/Visual mode supports `/`, `?`, `n`, and `N`. `/` and `?` preview
incrementally; Enter accepts, Esc restores the prior caret/selection and query.
`n` repeats the accepted direction and `N` reverses it. This version does not
implement `:s` or `:%s`. `Ctrl+R` retains Vim redo outside the search bar.

The bar reserves one editor row, or two while replacing, without increasing the
configured height. Tiny editors show the focused controls and as much document
as fits. No host-rendered dialog is required. The bar is a fixed grid: the Find
and With inputs share one width, the match count sits in a fixed slot so the
`Aa`/`.*` toggles never move, and focus moves only the `▌` rail and the control
highlight. Toggles show `●`/`○` (`[x]`/`[ ]` with `Options.SearchPlain`), so their
state never depends on color alone.

To place the bar elsewhere — a footer, say — call `ed.SetSearchBarExternal(true)`
and draw `ed.SearchBarView(width, opts)` yourself; the document then keeps the
editor's full height while search is open. `Search().Field` reports the focused
control (`SearchFieldFind` … `SearchFieldRegex`) and `Search().Prompt` a Vim `/`
or `?` search, for contextual key hints. `Options.SearchText`, `SearchMuted`,
`SearchRail`, `SearchControl`, and `SearchError` theme the bar, and
`Options.SearchInput` lets the host render the text fields like its own inputs.
`Options.SearchInputMax` caps the field width so the controls follow the fields
like a compact find widget, and `Options.SearchCell` renders each button and
toggle (in exactly its label's width plus two cells, e.g. as a pill; toggles
then carry no `●`/`○` glyph, so the cell must show on/off by shape). The match
count sits above Replace and the toggles above All, and `No results` uses
`SearchError`. Empty fields show `search` / `replace with` placeholders. Forward keys to Ripple before
host shortcuts while `ed.SearchActive()` is true, especially Esc and Tab. A host
may still intercept explicit submit shortcuts. The Find binding is configurable
through `KeyMap.Find`; there is deliberately no Ctrl+H default because terminals
commonly report it as Backspace.

Hosts can also use `OpenSearch`, `OpenReplace`, `CloseSearch`, `Search`,
`SetSearchQuery`, `SetReplacement`, `SetSearchCaseSensitive`, `SetSearchRegex`,
`FindNext`, `FindPrevious`, `ReplaceCurrent`, and `ReplaceAll`. Search results use
rune offsets in `Value`, matching `ReplaceRange`. `Search` returns copied results
and current count/error state, including after host edits or undo/redo.
`SetValue` closes the bar; reopening retains the previous query.

`Options.SearchMatch`, `SearchCurrent`, `SearchControl`, and `SearchError` provide
styling hooks. Syntax styling runs first, search highlighting second, and
selection/cursor last. Defaults underline matches and reverse the current hit
and focused control. The terminal harness enables the advanced regex toggle for
interactive testing (`go run ./cmd/rippletest`).

## License

MIT (see repository).
