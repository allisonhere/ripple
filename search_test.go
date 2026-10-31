package ripple

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func searchEditor(text, query string) Model {
	e := New()
	e.SetValue(text)
	e.cursor = 0
	e.OpenSearch()
	e.SetSearchQuery(query)
	return e
}
func TestSearchLiteralUnicodeAndNavigation(t *testing.T) {
	e := searchEditor("猫 Café\n猫 café 猫", "猫")
	if got := e.Search().Matches; !reflect.DeepEqual(got, []Match{{0, 1}, {7, 8}, {14, 15}}) {
		t.Fatalf("rune offsets: %+v", got)
	}
	e.FindPrevious()
	if e.cursor != 14 {
		t.Fatal("backward wrap")
	}
	e.FindNext()
	if e.cursor != 0 {
		t.Fatal("forward wrap")
	}
	e.SetSearchQuery("café")
	if len(e.Search().Matches) != 1 {
		t.Fatal("case sensitive default")
	}
	e.SetSearchCaseSensitive(false)
	if len(e.Search().Matches) != 2 {
		t.Fatal("case insensitive")
	}
	e.SetSearchQuery("Café\n猫")
	if len(e.Search().Matches) != 1 {
		t.Fatal("multiline literal")
	}
	e.SetSearchQuery(".")
	if len(e.Search().Matches) != 0 {
		t.Fatal("literal metacharacter")
	}
}
func TestSearchRegexAndAtomicReplace(t *testing.T) {
	e := searchEditor("猫12 dog34", "(猫|dog)([0-9]+)")
	e.SetSearchRegex(true)
	if e.Search().Regex {
		t.Fatal("regex must be opt-in")
	}
	e.SetRegexSearchEnabled(true)
	if e.Search().Regex {
		t.Fatal("availability must not activate regex")
	}
	e.SetSearchRegex(true)
	e.SetReplacement("${2}-$1")
	if n := e.ReplaceAll(); n != 2 || e.Value() != "12-猫 34-dog" {
		t.Fatalf("capture replace: %d %q", n, e.Value())
	}
	if !e.Undo() || e.Value() != "猫12 dog34" {
		t.Fatal("atomic undo")
	}
	if !e.Redo() || e.Value() != "12-猫 34-dog" {
		t.Fatal("atomic redo")
	}
	e.SetSearchQuery("[")
	if e.Search().Error == "" || e.ReplaceAll() != 0 || e.ReplaceCurrent() {
		t.Fatal("invalid regex must block edits")
	}
	e.SetRegexSearchEnabled(false)
	if e.Search().Regex || e.Search().Error != "" {
		t.Fatal("disabling regex returns to literal")
	}
}
func TestSearchReplaceSnapshotAndZeroWidth(t *testing.T) {
	e := searchEditor("a a", "a")
	e.SetReplacement("aa")
	if e.ReplaceAll() != 2 || e.Value() != "aa aa" {
		t.Fatal("must not revisit inserted matches")
	}
	e = searchEditor("a a", "a")
	e.SetReplacement("$1\\")
	e.ReplaceAll()
	if e.Value() != "$1\\ $1\\" {
		t.Fatal("literal replacement must stay literal")
	}
	e = searchEditor("猫a", "^")
	e.SetRegexSearchEnabled(true)
	e.SetSearchRegex(true)
	e.SetReplacement("X")
	if e.ReplaceAll() != 1 || e.Value() != "X猫a" {
		t.Fatal("zero-width replacement")
	}
	e = searchEditor("猫a", "(?:)")
	e.SetRegexSearchEnabled(true)
	e.SetSearchRegex(true)
	if len(e.Search().Matches) != 3 {
		t.Fatalf("zero-width matches: %+v", e.Search())
	}
	e.FindNext()
	if e.cursor != 1 {
		t.Fatal("zero-width navigation must progress")
	}
	e.SetReplacement("")
	if !e.ReplaceCurrent() || e.cursor != 2 {
		t.Fatal("zero-width empty replacement must progress")
	}
	e.SetReplacement("!")
	if e.ReplaceAll() != 3 || e.Value() != "!猫!a!" {
		t.Fatalf("snapshot zero-width: %q", e.Value())
	}
}
func TestSearchSingleReplaceAndRefresh(t *testing.T) {
	e := searchEditor("cat cat", "cat")
	e.SetReplacement("dog")
	if !e.ReplaceCurrent() || e.Value() != "dog cat" || e.cursor != 4 {
		t.Fatal("replace and advance")
	}
	if !e.Undo() || e.Value() != "cat cat" || len(e.Search().Matches) != 2 {
		t.Fatal("undo refresh")
	}
	e.ReplaceRange(0, 3, "dog")
	if len(e.Search().Matches) != 1 {
		t.Fatal("programmatic refresh")
	}
	e.SetValue("new")
	if e.SearchActive() {
		t.Fatal("loading closes search")
	}
}
func TestSearchKeysAndSmallView(t *testing.T) {
	for _, size := range [][2]int{{1, 1}, {8, 2}, {24, 3}, {60, 8}} {
		e := searchEditor("猫 café\nlong long long document", "long")
		e.OpenReplace()
		e.SetRegexSearchEnabled(true)
		e.SetSearchQuery(strings.Repeat("猫", 30))
		for range 7 {
			view := e.View(Options{})
			e.SetSize(size[0], size[1])
			view = e.View(Options{})
			if len(strings.Split(view, "\n")) != size[1] {
				t.Fatalf("height %v: %q", size, view)
			}
			for _, line := range strings.Split(view, "\n") {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("overflow %v: %q", size, line)
				}
			}
			apply(&e, tea.KeyMsg{Type: tea.KeyTab})
		}
		apply(&e, tea.KeyMsg{Type: tea.KeyEsc})
		if e.SearchActive() {
			t.Fatal("esc closes bar")
		}
	}
	e := New()
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlF})
	if !e.SearchActive() {
		t.Fatal("ctrl+f")
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlR})
	if !e.Search().Replacing {
		t.Fatal("ctrl+r")
	}
}
func TestVimIncrementalSearchCancelAndRepeat(t *testing.T) {
	e := New()
	e.SetValue("one two one two")
	e.SetInputMode(ModeVim)
	e.cursor = 0
	apply(&e, keyRunes("/"))
	apply(&e, keyRunes("two"))
	if !e.SearchActive() || e.cursor != 4 {
		t.Fatal("incremental forward search")
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyEsc})
	if e.cursor != 0 || e.SearchActive() {
		t.Fatal("cancel restores origin")
	}
	apply(&e, keyRunes("/"))
	apply(&e, keyRunes("two"))
	apply(&e, tea.KeyMsg{Type: tea.KeyEnter})
	if e.SearchActive() || e.cursor != 4 {
		t.Fatal("accept closes prompt")
	}
	apply(&e, keyRunes("n"))
	if e.cursor != 12 {
		t.Fatal("n forward")
	}
	apply(&e, keyRunes("N"))
	if e.cursor != 4 {
		t.Fatal("N reverse")
	}
	apply(&e, keyRunes("?"))
	e.SetSearchQuery("one")
	apply(&e, tea.KeyMsg{Type: tea.KeyEnter})
	if e.cursor != 0 {
		t.Fatal("backward prompt")
	}
	apply(&e, keyRunes("n"))
	if e.cursor != 8 {
		t.Fatal("n uses accepted backward direction")
	}
}

type searchClipboard struct{ text string }

func (c *searchClipboard) Read() (string, error) { return c.text, nil }
func (c *searchClipboard) Write(s string) error  { c.text = s; return nil }
func TestSearchClipboardSessionAndInputTarget(t *testing.T) {
	e := searchEditor("body", "b")
	cb := &searchClipboard{"query"}
	e.SetClipboard(cb)
	cmd := apply(&e, tea.KeyMsg{Type: tea.KeyCtrlV})
	msg := cmd()
	e.CloseSearch()
	apply(&e, msg)
	if e.Value() != "body" {
		t.Fatal("stale paste edited document")
	}
	e.OpenReplace()
	e.SetSearchQuery("")
	cmd = apply(&e, tea.KeyMsg{Type: tea.KeyCtrlV})
	apply(&e, cmd())
	if e.Search().Query != "query" || e.Value() != "body" {
		t.Fatalf("paste query %+v", e.Search())
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyTab})
	cb.text = "replacement"
	cmd = apply(&e, tea.KeyMsg{Type: tea.KeyCtrlV})
	msg = cmd()
	apply(&e, tea.KeyMsg{Type: tea.KeyTab})
	apply(&e, msg)
	if e.Search().Replacement != "replacement" || e.Value() != "body" {
		t.Fatal("paste must target original field")
	}
}
func TestSearchStylesLayerUnderSelection(t *testing.T) {
	e := searchEditor("hit hit", "hit")
	opts := Options{StyleKey: func(int) string { return "syntax" }, Style: func(k, s string) string {
		if k != "syntax" {
			t.Fatal(k)
		}
		return s
	}, SearchMatch: func(s string) string { return "M" + s }, SearchCurrent: func(s string) string { return "C" + s }}
	view := e.View(opts)
	if !strings.Contains(view, "Chit") || !strings.Contains(view, "Mhit") {
		t.Fatalf("search styling: %q", view)
	}
}

func TestSearchUndoKeysAndEmptyTinyEditor(t *testing.T) {
	e := searchEditor("a a", "a")
	e.OpenReplace()
	e.SetReplacement("b")
	e.ReplaceAll()
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlZ})
	if e.Value() != "a a" || len(e.Search().Matches) != 2 {
		t.Fatal("undo while bar is active")
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlY})
	if e.Value() != "b b" || len(e.Search().Matches) != 0 {
		t.Fatal("redo while bar is active")
	}
	var zero Model
	zero.OpenSearch()
	view := zero.View(Options{})
	if len(strings.Split(view, "\n")) != 24 {
		t.Fatal("zero-value search must use default height")
	}
	e = New()
	e.SetPlaceholder("Start here")
	e.SetSize(12, 3)
	e.OpenReplace()
	if lines := strings.Split(e.View(Options{}), "\n"); len(lines) != 3 || !strings.Contains(ansi.Strip(lines[0]), "Start") {
		t.Fatal("placeholder must leave room for bar")
	}
}

func TestSearchClipboardCannotCrossEditors(t *testing.T) {
	a := searchEditor("first", "a")
	b := searchEditor("second", "b")
	a.SetClipboard(&searchClipboard{"private query"})
	cmd := apply(&a, tea.KeyMsg{Type: tea.KeyCtrlV})
	apply(&b, cmd())
	if b.Search().Query != "b" || b.Value() != "second" {
		t.Fatal("paste crossed editor sessions")
	}
}

func TestSearchReplaceToggleFocusesInput(t *testing.T) {
	e := searchEditor("cat cat", "cat")
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlR})
	apply(&e, keyRunes("dog"))
	if e.Search().Query != "cat" || e.Search().Replacement != "dog" {
		t.Fatal("Ctrl+R must focus replacement without Tab")
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyEnter})
	if e.Value() != "dog cat" {
		t.Fatal("Enter must replace the current match")
	}
	apply(&e, tea.KeyMsg{Type: tea.KeyCtrlR})
	apply(&e, keyRunes("s"))
	if e.Search().Replacing || e.Search().Query != "cats" || e.Search().Replacement != "dog" {
		t.Fatal("hiding Replace must return focus to Find")
	}
}

func TestRegexLineAnchorsAndOverrides(t *testing.T) {
	for _, tc := range []struct {
		pattern string
		want    int
	}{
		{"^> ", 3}, {"$", 3}, {`\A> `, 1}, {`c\z`, 1}, {"(?-m)^> ", 1},
	} {
		e := searchEditor("> a\n> b\n> c", tc.pattern)
		e.SetRegexSearchEnabled(true)
		e.SetSearchRegex(true)
		e.SetSize(2, 4)
		if got := len(e.Search().Matches); got != tc.want {
			t.Errorf("%q: got %d matches, want %d", tc.pattern, got, tc.want)
		}
	}
	e := searchEditor("> a\n> b\n> c", "^> ")
	e.SetRegexSearchEnabled(true)
	e.SetSearchRegex(true)
	if e.ReplaceAll() != 3 || e.Value() != "a\nb\nc" {
		t.Fatal("quote prefix stripping must operate on every line")
	}
	e = searchEditor("A\na\n", "^a$")
	e.SetRegexSearchEnabled(true)
	e.SetSearchRegex(true)
	e.SetSearchCaseSensitive(false)
	if len(e.Search().Matches) != 2 {
		t.Fatal("multiline and ignore case must compose")
	}
	e.SetSearchQuery("$")
	if len(e.Search().Matches) != 3 {
		t.Fatal("trailing newline includes an empty final line")
	}
}

func TestRegexReplacementValidation(t *testing.T) {
	for _, tc := range []struct {
		pattern, replacement, want string
		invalid                    bool
	}{
		{"cat", "$5", "", true}, {"cat", "${missing}", "", true},
		{"cat", "$$5", "price $5", false}, {"cat", "$0", "price cat", false},
		{"(cat)", "$1suffix", "", true}, {"(cat)", "${1}suffix", "price catsuffix", false},
		{"(?P<word>cat)", "${word}", "price cat", false},
		{"(dog)?cat", "$1", "price ", false}, {"cat", "$", "price $", false},
		{"cat", "${", "price ${", false}, {"cat", "$$$5", "", true},
		{"(?P<01>cat)", "$01", "price cat", false},
	} {
		t.Run(tc.pattern+"/"+tc.replacement, func(t *testing.T) {
			for _, all := range []bool{false, true} {
				e := searchEditor("price cat", tc.pattern)
				e.OpenReplace()
				e.SetRegexSearchEnabled(true)
				e.SetSearchRegex(true)
				e.SetReplacement(tc.replacement)
				status := e.Search()
				if status.Error != "" || len(status.Matches) != 1 {
					t.Fatal("replacement validation must not break finding")
				}
				if (status.ReplacementError != "") != tc.invalid {
					t.Fatalf("unexpected replacement error %q", status.ReplacementError)
				}
				undoBefore := len(e.undo)
				changed := false
				if all {
					changed = e.ReplaceAll() > 0
				} else {
					changed = e.ReplaceCurrent()
				}
				if tc.invalid {
					if changed || e.Value() != "price cat" || len(e.undo) != undoBefore {
						t.Fatal("invalid replacement modified document/history")
					}
					if !strings.Contains(ansi.Strip(e.View(Options{})), "Invalid replacement") {
						t.Fatal("replacement error missing from bar")
					}
					e.SetReplacement("$$5")
					if e.Search().ReplacementError != "" || !e.ReplaceCurrent() || e.Value() != "price $5" {
						t.Fatal("replacement did not recover after correction")
					}
				} else if !changed || e.Value() != tc.want {
					t.Fatalf("replacement got %q, want %q", e.Value(), tc.want)
				}
			}
		})
	}
}

func TestRegexReplacementErrorRefreshesWithPatternAndMode(t *testing.T) {
	e := searchEditor("cat", "cat")
	e.SetRegexSearchEnabled(true)
	e.SetSearchRegex(true)
	e.SetReplacement("$1")
	if e.Search().ReplacementError == "" {
		t.Fatal("missing group should fail")
	}
	e.SetSearchQuery("(cat)")
	if e.Search().ReplacementError != "" {
		t.Fatal("pattern change must refresh validation")
	}
	e.SetSearchQuery("cat")
	e.SetSearchRegex(false)
	if e.Search().ReplacementError != "" || !e.ReplaceCurrent() || e.Value() != "$1" {
		t.Fatal("literal replacement must preserve dollars")
	}
}

// fieldStart returns the display column where the input text after label begins.
func fieldStart(row, label string) int {
	return ansi.StringWidth(row[:strings.Index(row, label)]) + 6
}

func TestSearchBarGridIsStable(t *testing.T) {
	for _, width := range []int{40, 60, 100} {
		e := searchEditor("cat cat dog", "cat")
		e.SetRegexSearchEnabled(true)
		e.OpenReplace()
		rows := e.SearchBarView(width, Options{})
		if len(rows) != 2 {
			t.Fatalf("width %d: want Find and With rows, got %d", width, len(rows))
		}
		find, with := ansi.Strip(rows[0]), ansi.Strip(rows[1])
		for _, r := range []string{find, with} {
			if ansi.StringWidth(r) != width {
				t.Fatalf("width %d: row is %d cells: %q", width, ansi.StringWidth(r), r)
			}
		}
		// Both inputs end where the shared control cluster begins.
		if strings.Index(find, " 1 of 2") < 0 && width >= 60 {
			t.Fatalf("width %d: status missing: %q", width, find)
		}
		toggleCol := ansi.StringWidth(find[:strings.Index(find, "Aa")])
		for _, query := range []string{"dog", "zzz", "c"} {
			e.SetSearchQuery(query)
			row := ansi.Strip(e.SearchBarView(width, Options{})[0])
			if got := ansi.StringWidth(row[:strings.Index(row, "Aa")]); got != toggleCol {
				t.Fatalf("width %d: toggles moved from %d to %d for %q", width, toggleCol, got, query)
			}
		}
		replaceCol := ansi.StringWidth(with[:strings.Index(with, "Replace")])
		if statusCol := ansi.StringWidth(find[:strings.Index(find, "Aa")]); replaceCol >= statusCol {
			t.Fatalf("width %d: buttons are not in the shared cluster: %q / %q", width, find, with)
		}
	}
}

func TestSearchBarFocusMovesRailNotRows(t *testing.T) {
	e := searchEditor("cat cat", "cat")
	e.OpenReplace()
	e.SetReplacement("dog")
	for _, field := range []int{SearchFieldFind, SearchFieldReplacement, SearchFieldReplace, SearchFieldAll, SearchFieldCase} {
		e.search.field = field
		rows := e.SearchBarView(60, Options{})
		find, with := ansi.Strip(rows[0]), ansi.Strip(rows[1])
		if !strings.Contains(find, "Find  cat") || !strings.Contains(with, "With  dog") {
			t.Fatalf("field %d replaced an input row: %q / %q", field, find, with)
		}
		withFocused := field >= SearchFieldReplacement && field <= SearchFieldAll
		if strings.HasPrefix(find, "▌") == withFocused || strings.HasPrefix(with, "▌") != withFocused {
			t.Fatalf("field %d: rail on wrong row: %q / %q", field, find, with)
		}
		if e.Search().Field != field {
			t.Fatal("status must report the focused field")
		}
	}
}

func TestSearchBarExternalKeepsDocumentHeight(t *testing.T) {
	e := searchEditor("one\ntwo\nthree\nfour", "two")
	e.SetSize(40, 4)
	e.OpenReplace()
	if got := strings.Count(e.View(Options{}), "\n") + 1; got != 4 || !strings.Contains(e.View(Options{}), "Find") {
		t.Fatalf("built-in bar must share the editor height: %d rows", got)
	}
	e.SetSearchBarExternal(true)
	view := ansi.Strip(e.View(Options{}))
	if strings.Contains(view, "Find") || !strings.Contains(view, "four") {
		t.Fatalf("external bar must leave all rows to the document: %q", view)
	}
	if len(e.SearchBarView(20, Options{})) != 2 {
		t.Fatal("host must still be able to draw the bar")
	}
	e.CloseSearch()
	if e.SearchBarView(20, Options{}) != nil {
		t.Fatal("closed search has no bar")
	}
}

func TestSearchBarPlainGlyphsAndHostInput(t *testing.T) {
	e := searchEditor("cat", "cat")
	called := 0
	opts := Options{SearchPlain: true, SearchInput: func(_ textinput.Model, width int, _ bool) string {
		called++
		return strings.Repeat("#", width)
	}}
	row := ansi.Strip(e.SearchBarView(50, opts)[0])
	if strings.ContainsAny(row, "▌●○") || !strings.Contains(row, "> Find") || !strings.Contains(row, "[x] Aa") {
		t.Fatalf("plain bar: %q", row)
	}
	if called != 1 || !strings.Contains(row, "Find  #") {
		t.Fatalf("host input renderer not used: %q", row)
	}
}

func TestSearchBarCompactWidgetAndHostCells(t *testing.T) {
	e := searchEditor("cat cat dog", "cat")
	e.SetRegexSearchEnabled(true)
	e.OpenReplace()
	cells := 0
	opts := Options{SearchInputMax: 20, SearchCell: func(label string, focused, on, toggle bool) string {
		cells++
		if toggle && on {
			return "(" + label + ")"
		}
		return "[" + label + "]"
	}}
	rows := e.SearchBarView(100, opts)
	find, with := ansi.Strip(rows[0]), ansi.Strip(rows[1])
	for _, r := range []string{find, with} {
		if ansi.StringWidth(r) != 100 {
			t.Fatalf("host cells changed the row width: %d %q", ansi.StringWidth(r), r)
		}
	}
	if cells != 4 || strings.Contains(find, "●") || !strings.Contains(find, "(Aa)") || !strings.Contains(find, "[.*]") {
		t.Fatalf("toggles must come from the host cell without glyphs: %q", find)
	}
	// Controls follow the capped input; status sits above Replace, toggles above All.
	col := func(row, word string) int { return ansi.StringWidth(row[:strings.Index(row, word)]) }
	if col(find, "1 of 2") != 2+6+20+1 {
		t.Fatalf("controls did not follow the capped input: %q", find)
	}
	if col(find, "1 of 2") != col(with, "[Replace]") || col(find, "(Aa)") != col(with, "[All]") {
		t.Fatalf("rows do not share columns:\n%q\n%q", find, with)
	}
}

func TestSearchBarPlaceholdersAndNoResults(t *testing.T) {
	e := searchEditor("cat", "")
	e.OpenReplace()
	rows := e.SearchBarView(60, Options{})
	if !strings.Contains(ansi.Strip(rows[0]), "search") || !strings.Contains(ansi.Strip(rows[1]), "replace with") {
		t.Fatalf("empty inputs need placeholders: %q", rows)
	}
	e.SetSearchQuery("dog")
	errored := false
	row := e.SearchBarView(60, Options{SearchError: func(s string) string {
		if s == "No results" {
			errored = true
		}
		return s
	}})[0]
	if !errored || !strings.Contains(ansi.Strip(row), "No results") {
		t.Fatalf("no results must use the error style: %q", ansi.Strip(row))
	}
}

func TestSearchDirectKeysNeedNoTab(t *testing.T) {
	alt := func(k tea.KeyType, r ...rune) tea.KeyMsg { return tea.KeyMsg{Type: k, Runes: r, Alt: true} }
	e := searchEditor("cat cat Cat", "cat")
	e.SetReplacement("dog")
	undo := len(e.undo)
	// Alt+Enter is inert until the replacement is visible.
	e, _ = e.Update(alt(tea.KeyEnter))
	if e.Value() != "cat cat Cat" {
		t.Fatal("replace all must wait for the Replace row")
	}
	e.OpenReplace()
	e, _ = e.Update(alt(tea.KeyEnter))
	if e.Value() != "dog dog Cat" || len(e.undo) != undo+1 {
		t.Fatalf("Alt+Enter must replace all as one undo step: %q", e.Value())
	}
	e.SetSearchQuery("cat")
	e, _ = e.Update(alt(tea.KeyRunes, 'c'))
	if e.Search().CaseSensitive || len(e.Search().Matches) != 1 || e.Search().Query != "cat" {
		t.Fatalf("Alt+C must toggle case without typing: %+v", e.Search())
	}
	e, _ = e.Update(alt(tea.KeyRunes, 'r'))
	if e.Search().Regex {
		t.Fatal("Alt+R needs regex enabled by the host")
	}
	e.SetRegexSearchEnabled(true)
	e, _ = e.Update(alt(tea.KeyRunes, 'r'))
	if !e.Search().Regex || e.Search().Query != "cat" || e.Search().Replacement != "dog" {
		t.Fatalf("Alt+R must toggle regex without typing: %+v", e.Search())
	}
}

func TestSearchBarShowsKeysOnControls(t *testing.T) {
	e := searchEditor("cat cat", "cat")
	e.SetRegexSearchEnabled(true)
	e.OpenReplace()
	rows := e.SearchBarView(100, Options{SearchShowKeys: true})
	find, with := ansi.Strip(rows[0]), ansi.Strip(rows[1])
	for _, want := range []string{"Aa ⌥C", ".* ⌥R"} {
		if !strings.Contains(find, want) {
			t.Fatalf("missing %q: %q", want, find)
		}
	}
	for _, want := range []string{"Replace ⏎", "All ⌥⏎"} {
		if !strings.Contains(with, want) {
			t.Fatalf("missing %q: %q", want, with)
		}
	}
	col := func(row, word string) int { return ansi.StringWidth(row[:strings.Index(row, word)]) }
	// The status lines up with the control's edge, its padding cell (or pill cap).
	if ansi.StringWidth(find) != 100 || ansi.StringWidth(with) != 100 || col(find, "1 of 2") != col(with, "Replace")-1 {
		t.Fatalf("keyed labels broke the grid:\n%q\n%q", find, with)
	}
	plain := ansi.Strip(strings.Join(e.SearchBarView(100, Options{SearchShowKeys: true, SearchPlain: true}), "\n"))
	if !strings.Contains(plain, "Replace Enter") || !strings.Contains(plain, "All Alt+Enter") || strings.ContainsAny(plain, "⏎⌥") {
		t.Fatalf("plain keys must be ASCII: %q", plain)
	}
}

// Tab must never land on a control the bar has clipped away.
func TestSearchBarKeepsFocusedControlVisibleWhenNarrow(t *testing.T) {
	labels := map[int]string{SearchFieldCase: "Aa", SearchFieldRegex: ".*", SearchFieldReplace: "Replace", SearchFieldAll: "All"}
	pill := func(label string, focused, on, toggle bool) string { return "(" + label + ")" }
	for _, opts := range []Options{{}, {SearchCell: pill, SearchShowKeys: true}} {
		for _, width := range []int{16, 22, 30, 40} {
			e := searchEditor("cat cat", "cat")
			e.SetRegexSearchEnabled(true)
			e.OpenReplace()
			e.SetReplacement("dog")
			for _, field := range e.searchFields() {
				e.search.field = field
				rows := e.SearchBarView(width, opts)
				find, with := ansi.Strip(rows[0]), ansi.Strip(rows[1])
				if ansi.StringWidth(find) != width || ansi.StringWidth(with) != width {
					t.Fatalf("width %d field %d: rows overflow: %q / %q", width, field, find, with)
				}
				label, ok := labels[field]
				if !ok {
					continue
				}
				row := find
				if field == SearchFieldReplace || field == SearchFieldAll {
					row = with
				}
				if !strings.Contains(row, label) {
					t.Fatalf("width %d: focused %q is clipped: %q / %q", width, label, find, with)
				}
			}
		}
	}
}
