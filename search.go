package ripple

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Match identifies a match using rune offsets in Value; End is exclusive.
type Match struct{ Start, End int }

// SearchStatus is a snapshot of the search UI. Current is one-based, or zero
// when no match is current. Matches is a copy, safe for a host to retain.
type SearchStatus struct {
	Active, Replacing, CaseSensitive, Regex, RegexAvailable bool
	Query, Replacement, Error, ReplacementError, Message    string
	Current                                                 int
	Matches                                                 []Match
}

type searchState struct {
	active, replacing, ignoreCase, regex, allowed bool
	query, replacement                            textinput.Model
	field                                         int // find, replacement, replace, all, case, regex
	generation                                    uint64
	direction                                     int
	vimPrompt                                     bool
	originCursor, originAnchor, originViewport    int
	originQuery                                   string
	originDirection                               int
	current                                       int
	matches                                       []Match
	indices                                       [][]int
	compiled                                      *regexp.Regexp
	cachedText, cachedQuery                       string
	cachedIgnoreCase, cachedRegex                 bool
	valid                                         bool
	err, replacementErr, message                  string
}

// Search clipboard commands carry a globally unique session so a host routing
// results to another focused editor cannot paste into the wrong search field.
var nextSearchSession atomic.Uint64

type searchPasteMsg struct {
	text       string
	err        error
	generation uint64
	field      int
}

// SearchActive reports whether the editor currently owns search-bar input.
func (m Model) SearchActive() bool { return m.search.active }

// SetRegexSearchEnabled exposes the advanced Regex toggle. Searches remain
// literal until SetSearchRegex or the user explicitly enables it.
func (m *Model) SetRegexSearchEnabled(on bool) {
	m.search.allowed = on
	if !on {
		m.search.regex = false
		if m.search.field == 5 {
			m.search.field = 0
			m.focusSearchField()
		}
	}
	m.refreshSearch()
}

// OpenSearch opens the built-in Find bar. A single-line selection seeds Find.
func (m *Model) OpenSearch() { m.openSearch(false, 0) }

// OpenReplace opens Find with the replacement controls expanded.
func (m *Model) OpenReplace() { m.openSearch(true, 0) }

func (m *Model) openSearch(replacing bool, vimDirection int) {
	s := &m.search
	if !s.active {
		q, r := s.query.Value(), s.replacement.Value()
		if selected := m.SelectedText(); selected != "" && !strings.Contains(selected, "\n") {
			q = selected
		}
		s.query = textinput.New()
		s.query.Prompt = ""
		s.query.CharLimit = 0
		s.query.SetValue(q)
		s.replacement = textinput.New()
		s.replacement.Prompt = ""
		s.replacement.CharLimit = 0
		s.replacement.SetValue(r)
		s.originCursor, s.originAnchor, s.originViewport = m.cursor, m.selAnchor, m.viewport
		s.originQuery = q
		s.originDirection = s.direction
		s.generation = nextSearchSession.Add(1)
	}
	s.active, s.replacing, s.field = true, replacing, 0
	s.vimPrompt = vimDirection != 0
	if vimDirection != 0 {
		s.direction = vimDirection
	} else if s.direction == 0 {
		s.direction = 1
	}
	s.message = ""
	m.coalesceKind = kindNone
	m.focusSearchField()
	m.refreshSearch()
	m.previewSearch()
}

// CloseSearch restores the full editor viewport without undoing replacements.
func (m *Model) CloseSearch() {
	m.search.active = false
	m.search.vimPrompt = false
	m.search.query.Blur()
	m.search.replacement.Blur()
	m.search.generation = 0
	m.ensureCursorVisible()
}

// SetSearchQuery updates the query without editing the document.
func (m *Model) SetSearchQuery(q string) {
	m.search.query.SetValue(q)
	m.search.message = ""
	m.refreshSearch()
	if m.SearchActive() {
		m.previewSearch()
	}
}

// SetReplacement changes the replacement text without editing the document.
func (m *Model) SetReplacement(r string) {
	m.search.replacement.SetValue(r)
	m.search.message = ""
	m.refreshSearch()
}

// SetSearchCaseSensitive controls Unicode-aware case-insensitive matching.
func (m *Model) SetSearchCaseSensitive(on bool) {
	m.search.ignoreCase = !on
	m.search.message = ""
	m.refreshSearch()
	if m.SearchActive() {
		m.previewSearch()
	}
}

// SetSearchRegex selects regexp syntax when advanced search is available.
func (m *Model) SetSearchRegex(on bool) {
	m.search.regex = on && m.search.allowed
	m.search.message = ""
	m.refreshSearch()
	if m.SearchActive() {
		m.previewSearch()
	}
}

// Search returns current results, including after a programmatic document edit.
func (m Model) Search() SearchStatus {
	m.refreshSearch()
	s := m.search
	return SearchStatus{Active: s.active, Replacing: s.replacing, CaseSensitive: !s.ignoreCase, Regex: s.regex, RegexAvailable: s.allowed,
		Query: s.query.Value(), Replacement: s.replacement.Value(), Error: s.err, ReplacementError: s.replacementErr, Message: s.message, Current: s.current + 1, Matches: append([]Match(nil), s.matches...)}
}

func (m *Model) refreshSearch() {
	s := &m.search
	text, q := string(m.text), s.query.Value()
	if !s.valid || s.cachedText != text || s.cachedQuery != q || s.cachedIgnoreCase != s.ignoreCase || s.cachedRegex != s.regex {
		s.cachedText, s.cachedQuery, s.cachedIgnoreCase, s.cachedRegex = text, q, s.ignoreCase, s.regex
		s.valid = true
		s.matches = nil
		s.indices = nil
		s.compiled = nil
		s.err = ""
		s.current = -1
		if q != "" {
			pattern := q
			if !s.regex {
				pattern = regexp.QuoteMeta(pattern)
			} else {
				pattern = "(?m)" + pattern
			}
			if s.ignoreCase {
				pattern = "(?i)" + pattern
			}
			re, err := regexp.Compile(pattern)
			if err != nil {
				s.err = err.Error()
			} else {
				s.compiled = re
				s.indices = re.FindAllStringSubmatchIndex(text, -1)
				// Convert all byte boundaries in a single document pass.
				offsets := make(map[int]int, len(s.indices)*2)
				runeIndex := 0
				for byteIndex := range text {
					offsets[byteIndex] = runeIndex
					runeIndex++
				}
				offsets[len(text)] = runeIndex
				for _, span := range s.indices {
					s.matches = append(s.matches, Match{offsets[span[0]], offsets[span[1]]})
				}
			}
		}
	}
	s.replacementErr = ""
	if s.regex && s.compiled != nil {
		s.replacementErr = validateReplacement(s.compiled, s.replacement.Value())
	}
	if s.current >= 0 && s.current < len(s.matches) && s.matches[s.current].Start == m.cursor {
		return
	}
	s.current = -1
	for i, match := range s.matches {
		if match.Start == m.cursor {
			s.current = i
			break
		}
	}
}

func (m *Model) previewSearch() {
	m.refreshSearch()
	s := &m.search
	if len(s.matches) == 0 {
		return
	}
	origin := s.originCursor
	index := sort.Search(len(s.matches), func(i int) bool { return s.matches[i].Start >= origin })
	if s.direction < 0 {
		index = sort.Search(len(s.matches), func(i int) bool { return s.matches[i].Start > origin }) - 1
		if index < 0 {
			index = len(s.matches) - 1
		}
	} else if index == len(s.matches) {
		index = 0
	}
	m.selectSearchMatch(index)
}

func (m *Model) selectSearchMatch(index int) {
	m.search.current = index
	m.cursor = m.search.matches[index].Start
	m.selAnchor = -1
	m.goalCol = -1
	m.coalesceKind = kindNone
	m.ensureCursorVisible()
}

// FindNext and FindPrevious advance through matches, wrapping at document ends.
func (m *Model) FindNext() bool     { return m.findSearch(1) }
func (m *Model) FindPrevious() bool { return m.findSearch(-1) }
func (m *Model) findSearch(dir int) bool {
	m.refreshSearch()
	s := &m.search
	if len(s.matches) == 0 {
		return false
	}
	index := s.current
	if index >= 0 {
		index = (index + dir + len(s.matches)) % len(s.matches)
	} else if dir > 0 {
		index = sort.Search(len(s.matches), func(i int) bool { return s.matches[i].Start >= m.cursor })
		if index == len(s.matches) {
			index = 0
		}
	} else {
		index = sort.Search(len(s.matches), func(i int) bool { return s.matches[i].Start > m.cursor }) - 1
		if index < 0 {
			index = len(s.matches) - 1
		}
	}
	m.selectSearchMatch(index)
	return true
}

func (m Model) expandedReplacement(index int) string {
	s := m.search
	if !s.regex {
		return s.replacement.Value()
	}
	return string(s.compiled.ExpandString(nil, s.replacement.Value(), s.cachedText, s.indices[index]))
}

// ReplaceCurrent replaces the current match as one undo step, then advances.
func (m *Model) ReplaceCurrent() bool {
	m.refreshSearch()
	s := &m.search
	if s.current < 0 || s.err != "" || s.replacementErr != "" {
		return false
	}
	index := s.current
	match := s.matches[index]
	replacement := sanitizeInput(m.expandedReplacement(index))
	m.ReplaceRange(match.Start, match.End, replacement)
	next := match.Start + utf8.RuneCountInString(replacement)
	m.refreshSearch()
	// A zero-width match must progress past the insertion, including empty edits.
	if match.Start == match.End {
		next++
	}
	index = sort.Search(len(m.search.matches), func(i int) bool { return m.search.matches[i].Start >= next })
	if len(m.search.matches) > 0 {
		if index == len(m.search.matches) {
			index = 0
		}
		m.selectSearchMatch(index)
	}
	m.search.message = "Replaced 1 match"
	return true
}

// ReplaceAll applies a snapshot of matches in one undo step. Inserted text is
// never searched again during the operation, including for zero-width matches.
func (m *Model) ReplaceAll() int {
	m.refreshSearch()
	matches := m.search.matches
	if len(matches) == 0 || m.search.err != "" || m.search.replacementErr != "" {
		return 0
	}
	text := m.text
	var out strings.Builder
	end := 0
	caret := m.cursor
	for i, match := range matches {
		out.WriteString(string(text[end:match.Start]))
		replacement := sanitizeInput(m.expandedReplacement(i))
		out.WriteString(replacement)
		delta := utf8.RuneCountInString(replacement) - (match.End - match.Start)
		if m.cursor >= match.End {
			caret += delta
		} else if m.cursor > match.Start {
			caret = utf8.RuneCountInString(out.String())
		}
		end = match.End
	}
	out.WriteString(string(text[end:]))
	m.pushUndo(kindBoundary)
	m.text = []rune(out.String())
	m.cursor = clampInt(caret, 0, len(m.text))
	m.selAnchor = -1
	m.goalCol = -1
	m.search.message = fmt.Sprintf("Replaced %d matches", len(matches))
	m.refreshSearch()
	m.ensureCursorVisible()
	return len(matches)
}

func (m *Model) focusSearchField() {
	m.search.query.Blur()
	m.search.replacement.Blur()
	if m.search.field == 0 {
		m.search.query.Focus()
	}
	if m.search.field == 1 {
		m.search.replacement.Focus()
	}
}
func (m Model) searchFields() []int {
	fields := []int{0}
	if m.search.replacing {
		fields = append(fields, 1, 2, 3)
	}
	fields = append(fields, 4)
	if m.search.allowed {
		fields = append(fields, 5)
	}
	return fields
}

func (m Model) searchKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	s := &m.search
	switch msg.String() {
	case "esc":
		if s.vimPrompt {
			m.cursor, m.selAnchor, m.viewport = s.originCursor, s.originAnchor, s.originViewport
			s.query.SetValue(s.originQuery)
			s.direction = s.originDirection
		}
		m.CloseSearch()
		m.refreshSearch()
		return m, nil
	case "ctrl+z":
		m.search.message = ""
		m.Undo()
		m.refreshSearch()
		return m, nil
	case "ctrl+y":
		m.search.message = ""
		m.Redo()
		m.refreshSearch()
		return m, nil
	case "ctrl+f":
		s.field = 0
		m.focusSearchField()
		return m, nil
	case "ctrl+r":
		s.replacing = !s.replacing
		s.vimPrompt = false
		s.field = 0
		if s.replacing {
			s.field = 1
		}
		m.focusSearchField()
		m.ensureCursorVisible()
		return m, nil
	case "tab", "shift+tab":
		fields := m.searchFields()
		at := 0
		for i, field := range fields {
			if field == s.field {
				at = i
			}
		}
		step := 1
		if msg.String() == "shift+tab" {
			step = -1
		}
		s.field = fields[(at+step+len(fields))%len(fields)]
		m.focusSearchField()
		return m, nil
	case "f3":
		m.FindNext()
		return m, nil
	case "shift+f3":
		m.FindPrevious()
		return m, nil
	case "enter":
		switch s.field {
		case 0:
			if s.vimPrompt {
				m.CloseSearch()
			} else {
				m.FindNext()
			}
		case 1, 2:
			m.ReplaceCurrent()
		case 3:
			m.ReplaceAll()
		case 4:
			m.SetSearchCaseSensitive(s.ignoreCase)
		case 5:
			m.SetSearchRegex(!s.regex)
		}
		return m, nil
	case "ctrl+v":
		if s.field > 1 || m.clipboard == nil {
			return m, nil
		}
		cb, generation, field := m.clipboard, s.generation, s.field
		return m, func() tea.Msg { text, err := cb.Read(); return searchPasteMsg{text, err, generation, field} }
	}
	if s.field <= 1 {
		input := &s.query
		if s.field == 1 {
			input = &s.replacement
		}
		if msg.String() == "ctrl+c" || msg.String() == "ctrl+x" {
			if m.clipboard == nil {
				return m, nil
			}
			text := input.Value()
			if msg.String() == "ctrl+x" {
				input.SetValue("")
				m.searchChanged()
			}
			return m, m.copyCmd(text)
		}
		var cmd tea.Cmd
		*input, cmd = input.Update(msg)
		m.searchChanged()
		return m, cmd
	}
	return m, nil
}
func (m *Model) searchChanged() {
	m.search.message = ""
	m.refreshSearch()
	if m.search.field == 0 {
		m.previewSearch()
	}
}
func (m *Model) searchPaste(text string, field int) {
	originalField := m.search.field
	m.search.field = field
	input := &m.search.query
	if field == 1 {
		input = &m.search.replacement
	}
	wasFocused := input.Focused()
	input.Focus()
	// Clipboard inserts follow the input's caret, with the same sanitizing as typing.
	*input, _ = input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(sanitizeInput(text)), Paste: true})
	m.searchChanged()
	if !wasFocused {
		input.Blur()
	}
	m.search.field = originalField
}

func (m Model) searchRows() int {
	if !m.search.active {
		return 0
	}
	if m.search.replacing && m.height >= 3 {
		return 2
	}
	return 1
}
func (m Model) documentHeight() int { return max(0, m.height-m.searchRows()) }

func (m Model) searchBar(opts Options) []string {
	s := m.search
	width := max(1, m.width)
	style := opts.SearchControl
	if style == nil {
		style = func(text string) string { return lipgloss.NewStyle().Reverse(true).Render(text) }
	}
	button := func(field int, label string) string {
		if s.field == field {
			return style("[" + label + "]")
		}
		return "[" + label + "]"
	}
	status := "No matches"
	if len(s.matches) > 0 {
		status = fmt.Sprintf("%d/%d", s.current+1, len(s.matches))
	}
	if s.message != "" {
		status = s.message
	}
	if s.err != "" {
		status = "Invalid regex: " + s.err
		if opts.SearchError != nil {
			status = opts.SearchError(status)
		}
	}
	caseLabel := "Aa:off"
	if !s.ignoreCase {
		caseLabel = "Aa:on"
	}
	controls := button(4, caseLabel)
	if s.allowed {
		label := ".*:off"
		if s.regex {
			label = ".*:on"
		}
		controls += " " + button(5, label)
	}

	renderInput := func(input textinput.Model, label string, focused bool, suffix string) string {
		// Leave room for textinput's extra caret cell. Clip the feedback, never
		// drop it entirely when an error or count exceeds the available space.
		prefix := label
		if width < 12 {
			prefix = string([]rune(label)[:1])
		}
		prefixW := lipgloss.Width(prefix)
		available := max(0, width-prefixW-1)
		feedbackW := min(lipgloss.Width(suffix), max(0, available/2))
		if lipgloss.Width(suffix) <= available-9 {
			feedbackW = lipgloss.Width(suffix)
		}
		fieldW := available
		if feedbackW > 0 {
			fieldW -= feedbackW + 1
		}
		input.Width = max(1, fieldW-1)
		input.SetCursor(input.Position()) // update horizontal scrolling for this width
		if m.blurred {
			input.Blur()
		}
		if focused {
			prefix = style(prefix)
		}
		field := ansi.Truncate(input.View(), max(0, fieldW), "")
		field += strings.Repeat(" ", max(0, fieldW-lipgloss.Width(field)))
		line := prefix + " " + field
		if feedbackW > 0 {
			line += " " + ansi.Truncate(suffix, feedbackW, "…")
		}
		return ansi.Truncate(line, width, "")
	}
	queryLabel := "Find:"
	if s.vimPrompt {
		queryLabel = "/"
		if s.direction < 0 {
			queryLabel = "?"
		}
	}
	// Errors/counts take precedence while typing; action controls take precedence
	// when focused. This keeps every control reachable even in a tiny terminal.
	suffix := status + " " + controls
	if s.field >= 4 {
		suffix = controls + " " + status
	}
	find := renderInput(s.query, queryLabel, s.field == 0, suffix)
	if s.field >= 4 {
		label := caseLabel
		if s.field == 5 {
			label = ".*:off"
			if s.regex {
				label = ".*:on"
			}
		}
		find = ansi.Truncate(button(s.field, label)+" "+status, width, "")
	}
	if !s.replacing {
		return []string{find}
	}
	replacementSuffix := button(2, "Replace") + " " + button(3, "All")
	if s.replacementErr != "" {
		replacementSuffix = "Invalid replacement: " + s.replacementErr
		if opts.SearchError != nil {
			replacementSuffix = opts.SearchError(replacementSuffix)
		}
	}
	replace := renderInput(s.replacement, "With:", s.field == 1, replacementSuffix)
	if s.field == 2 || s.field == 3 {
		feedback := status
		if s.replacementErr != "" {
			feedback = replacementSuffix
		}
		replace = ansi.Truncate(button(s.field, map[int]string{2: "Replace", 3: "All"}[s.field])+" "+feedback, width, "")
	}
	if m.searchRows() == 1 {
		if s.field >= 1 && s.field <= 3 {
			return []string{replace}
		}
		return []string{find}
	}
	return []string{find, replace}
}

func (m Model) searchRenderOptions(opts Options) Options {
	if len(m.search.matches) == 0 || !m.search.active {
		return opts
	}
	originalKey, originalStyle := opts.StyleKey, opts.Style
	matched, current := opts.SearchMatch, opts.SearchCurrent
	if matched == nil {
		matched = func(s string) string { return lipgloss.NewStyle().Underline(true).Render(s) }
	}
	if current == nil {
		current = func(s string) string { return lipgloss.NewStyle().Reverse(true).Render(s) }
	}
	opts.StyleKey = func(offset int) string {
		original := ""
		if originalKey != nil {
			original = originalKey(offset)
		}
		i := sort.Search(len(m.search.matches), func(i int) bool { return m.search.matches[i].End > offset })
		mark := "0"
		if i < len(m.search.matches) && m.search.matches[i].Start <= offset {
			mark = "1"
			if i == m.search.current {
				mark = "2"
			}
		}
		return mark + original
	}
	opts.Style = func(k, text string) string {
		if originalStyle != nil {
			text = originalStyle(k[1:], text)
		}
		if k[0] == '2' {
			return current(text)
		}
		if k[0] == '1' {
			return matched(text)
		}
		return text
	}
	return opts
}

func (m Model) vimSearchCommand(r rune, count int) (Model, tea.Cmd, bool) {
	switch r {
	case '/', '?':
		oldQuery := m.search.query.Value()
		direction := 1
		if r == '?' {
			direction = -1
		}
		m.openSearch(false, direction)
		m.search.originQuery = oldQuery
		return m, nil, true
	case 'n', 'N':
		direction := m.search.direction
		if direction == 0 {
			direction = 1
		}
		if r == 'N' {
			direction = -direction
		}
		for range count {
			m.findSearch(direction)
		}
		return m, nil, true
	}
	return m, nil, false
}

// searchBinding returns defaults for a zero-value model while preserving an
// explicitly supplied KeyMap (including deliberately disabled search bindings).
func (m Model) searchBinding() key.Binding {
	if len(m.keys.Undo.Keys()) == 0 {
		return DefaultKeyMap().Find
	}
	return m.keys.Find
}

func (m Model) searchBarIfActive(opts Options) []string {
	if !m.SearchActive() {
		return nil
	}
	return m.searchBar(opts)
}

// Validate references before ExpandString can silently erase an unknown group.
// Token boundaries and numeric/name classification follow Go regexp expansion:
// $$ escapes a dollar, malformed references are literal, and names are greedy.
func validateReplacement(re *regexp.Regexp, template string) string {
	for {
		_, after, found := strings.Cut(template, "$")
		if !found {
			return ""
		}
		template = after
		if strings.HasPrefix(template, "$") {
			template = template[1:]
			continue
		}
		name, number, rest, ok := replacementReference(template)
		if !ok {
			continue
		}
		template = rest
		if (number >= 0 && number <= re.NumSubexp()) || (number < 0 && re.SubexpIndex(name) >= 0) {
			continue
		}
		return fmt.Sprintf("Unknown capture ${%s}; use $$ for a literal dollar", name)
	}
}

func replacementReference(text string) (name string, number int, rest string, ok bool) {
	if text == "" {
		return
	}
	braced := text[0] == '{'
	if braced {
		text = text[1:]
	}
	end := 0
	for _, r := range text {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			break
		}
		end += utf8.RuneLen(r)
	}
	if end == 0 {
		return
	}
	name = text[:end]
	if braced {
		if end >= len(text) || text[end] != '}' {
			return
		}
		end++
	}
	number = 0
	for _, digit := range []byte(name) {
		if digit < '0' || digit > '9' || number >= 100000000 {
			number = -1
			break
		}
		number = number*10 + int(digit-'0')
	}
	if len(name) > 1 && name[0] == '0' {
		number = -1
	}
	return name, number, text[end:], true
}
