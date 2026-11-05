package main

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/allisonhere/ripple"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type geometryProbe struct{ model }

func (geometryProbe) Init() tea.Cmd { return tea.Quit }

// Render through the real Bubble Tea startup/renderer/shutdown path. Input is
// disabled, so this test needs no TTY, wall-clock sleeps, or terminal replies.
func programOutput(t *testing.T, m model, anchored bool) string {
	t.Helper()
	var output bytes.Buffer
	options := []tea.ProgramOption{tea.WithInput(nil), tea.WithOutput(&output), tea.WithoutSignalHandler(), tea.WithoutCatchPanics(), tea.WithEnvironment([]string{"TERM=dumb"})}
	var program *tea.Program
	if anchored {
		program = newProgram(geometryProbe{m}, options...)
	} else {
		// Original inline startup is retained only as a regression control.
		program = tea.NewProgram(geometryProbe{m}, append(options, tea.WithMouseCellMotion())...)
	}
	if _, err := program.Run(); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

// Track only screen-origin controls emitted by Bubble Tea, including scrolling
// as an inline frame is printed from the bottom shell prompt. The marker is the
// harness header; subsequent newlines can scroll that marker upward. ANSI style
// and reporting changes do not move it. This is deliberately not editorRect().
func frameOrigin(t *testing.T, output string, terminalHeight, initialRow int) int {
	t.Helper()
	row, marker := initialRow, -1
	for i := 0; i < len(output); {
		if strings.HasPrefix(output[i:], "plain\r") || strings.HasPrefix(output[i:], "plain\n") || strings.HasPrefix(output[i:], "plain\x1b") || strings.HasPrefix(output[i:], "plain · wrap:") {
			marker = row
		}
		switch output[i] {
		case '\x1b':
			if i+1 >= len(output) {
				t.Fatal("incomplete escape")
			}
			if output[i+1] == '[' {
				end := i + 2
				for end < len(output) && (output[end] < '@' || output[end] > '~') {
					end++
				}
				if end == len(output) {
					t.Fatal("incomplete CSI")
				}
				args, action := output[i+2:end], output[end]
				count := 1
				if first := strings.Split(args, ";")[0]; first != "" {
					if n, err := strconv.Atoi(first); err == nil && n > 0 {
						count = n
					}
				}
				switch action {
				case 'H', 'f':
					row = min(terminalHeight-1, count-1)
				case 'A':
					row = max(0, row-count)
				case 'B':
					row = min(terminalHeight-1, row+count)
				case 'J':
					if args == "2" {
						marker = -1
					}
				}
				i = end + 1
			} else if output[i+1] == ']' {
				i += 2
				for i < len(output) && output[i] != '\a' && !strings.HasPrefix(output[i:], "\x1b\\") {
					i++
				}
				if i < len(output) && output[i] == '\a' {
					i++
				} else {
					i += 2
				}
			} else {
				i += 2
			}
		case '\n':
			if row == terminalHeight-1 {
				if marker >= 0 {
					marker--
				}
			} else {
				row++
			}
			i++
		default:
			_, n := utf8.DecodeRuneInString(output[i:])
			i += n
		}
	}
	if marker < 0 {
		t.Fatal("harness header missing from terminal output")
	}
	return marker
}

func demoGeometryModel(t *testing.T) model {
	t.Helper()
	m := enableMouse(t, harnessFixture(t))
	m.ed.SetValue(mouseDemo())
	m.ed.ScrollRows(-10000)
	m.ed.PlaceCursor(0, 0)
	return m
}

func renderedWordCell(t *testing.T, m model, word string) (x, y int) {
	t.Helper()
	for row, line := range strings.Split(ansi.Strip(m.View()), "\n") {
		if at := strings.Index(line, word); at >= 0 {
			return ansi.StringWidth(line[:at]) + 1, row
		}
	}
	t.Fatalf("%q not present in rendered view", word)
	return
}

func TestMouseGeometryReportedTwoRowOffset(t *testing.T) {
	m := demoGeometryModel(t)
	if got := len(strings.Split(m.View(), "\n")); got != 22 {
		t.Fatalf("24-row terminal control frame has %d rows; want 22", got)
	}
	x, y := renderedWordCell(t, m, "click")
	inlineOrigin := frameOrigin(t, programOutput(t, m, false), 24, 23)
	if inlineOrigin != 2 {
		t.Fatalf("inline frame origin=%d; want reproduced two-row shell offset", inlineOrigin)
	}
	legacy := m
	press := tea.MouseMsg{X: x, Y: y + inlineOrigin, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	legacy, _ = legacy.handleMouse(press, mouseTestTime)
	legacy, _ = legacy.handleMouse(press, mouseTestTime.Add(time.Millisecond))
	if legacy.ed.SelectedText() != "selection" {
		t.Fatalf("reported inline failure selected %q; want selection", legacy.ed.SelectedText())
	}

	output := programOutput(t, m, true)
	if !strings.Contains(output, "\x1b[?1002h") || !strings.Contains(output, "\x1b[?1006h") {
		t.Fatal("startup lost drag/SGR mouse reporting")
	}
	for _, shellRow := range []int{0, 8, 23} {
		origin := frameOrigin(t, output, 24, shellRow)
		if origin != 0 {
			t.Fatalf("frame inherited shell row %d: origin=%d", shellRow, origin)
		}
		fixed := m
		press.Y = y + origin
		fixed, _ = fixed.handleMouse(press, mouseTestTime)
		fixed, _ = fixed.handleMouse(press, mouseTestTime.Add(time.Millisecond))
		if fixed.ed.SelectedText() != "click" {
			t.Fatalf("double-click selected %q; want click", fixed.ed.SelectedText())
		}
	}
}

// Find the actual content origin in the rendered frame, rather than using the
// coordinate calculation under test. CursorRune styling preserves these glyphs.
func renderedEditorRect(t *testing.T, m model) (x, y, w, h int) {
	t.Helper()
	ed := m.ed
	ed.Blur()
	document := strings.Split(ansi.Strip(ed.View(ripple.Options{})), "\n")
	for row, line := range strings.Split(ansi.Strip(m.View()), "\n") {
		if at := strings.Index(line, document[0]); document[0] != "" && at >= 0 {
			return ansi.StringWidth(line[:at]), row, m.edW, len(document)
		}
	}
	t.Fatal("first editor row not found in rendered frame")
	return
}

func TestMouseGeometryRenderedRowsScrollAndResize(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {30, 14}, {18, 12}, {110, 35}} {
		for _, top := range []int{0, 3, 20} {
			base := enableMouse(t, harnessFixture(t))
			updateHarness(&base, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			base.ed.ScrollRows(-10000)
			base.ed.ScrollRows(top)
			x, y, w, h := renderedEditorRect(t, base)
			for _, row := range []int{0, h / 2, h - 1} {
				for _, col := range []int{0, w / 2, w - 1} {
					t.Run(fmt.Sprintf("%v/top=%d/row=%d/col=%d", size, top, row, col), func(t *testing.T) {
						m := base
						ref := m.ed
						ref.PlaceCursor(row, col)
						pointer(&m, tea.MouseActionPress, tea.MouseButtonLeft, x+col, y+row)
						if m.ed.CursorIndex() != ref.CursorIndex() {
							t.Fatalf("rendered cell selected offset %d; want %d", m.ed.CursorIndex(), ref.CursorIndex())
						}
					})
				}
			}
			base.ed.ExtendSelectionTo(1, 4)
			before := state(base)
			for _, pos := range [][2]int{{x - 1, y}, {x + w, y}, {x, y - 1}, {x, y + h}, {0, 0}, {size[0], size[1]}} {
				m := base
				pointer(&m, tea.MouseActionPress, tea.MouseButtonLeft, pos[0], pos[1])
				if state(m) != before {
					t.Fatalf("outside rendered editor at %v changed caret/selection", pos)
				}
			}
		}
	}
}

func TestMouseGeometryDragAndAutoscrollUseRenderedBounds(t *testing.T) {
	m := enableMouse(t, harnessFixture(t))
	m.ed.ScrollRows(8)
	x, y, w, h := renderedEditorRect(t, m)
	pointer(&m, tea.MouseActionPress, tea.MouseButtonLeft, x+2, y+h/2)
	for _, row := range []int{0, h - 1} {
		ref := m.ed
		ref.ExtendSelectionTo(row, 3)
		if cmd := pointer(&m, tea.MouseActionMotion, tea.MouseButtonLeft, x+3, y+row); cmd != nil {
			t.Fatal("inside rendered bounds started autoscroll")
		}
		if m.ed.CursorIndex() != ref.CursorIndex() || m.ed.SelectedText() != ref.SelectedText() {
			t.Fatal("drag mapping disagrees with rendered row")
		}
	}
	for _, dir := range []int{1, -1} {
		outside := y + h
		if dir < 0 {
			outside = y - 1
		}
		if cmd := pointer(&m, tea.MouseActionMotion, tea.MouseButtonLeft, x+3, outside); cmd == nil || m.scrollDir != dir {
			t.Fatal("crossing rendered border did not start scrolling")
		}
		ref := m.ed
		ref.ScrollRows(dir)
		row, col := h-1, w
		if dir < 0 {
			row, col = 0, 0
		}
		ref.ExtendSelectionTo(row, col)
		token := scrollTickMsg(m.scrollGeneration)
		m, _ = m.handleScrollTick(token)
		if m.ed.ViewportTop() != ref.ViewportTop() || m.ed.SelectedText() != ref.SelectedText() {
			t.Fatal("autoscroll endpoint disagrees with rendered viewport")
		}
		pointer(&m, tea.MouseActionMotion, tea.MouseButtonLeft, x+3, y+h/2)
	}
}

func TestMouseGeometryDoubleClickDemoAfterScrollAndResize(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {48, 20}, {30, 14}, {110, 35}} {
		for _, top := range []int{0, 1} {
			m := demoGeometryModel(t)
			updateHarness(&m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m.ed.ScrollRows(-10000)
			m.ed.ScrollRows(top)
			x, y := renderedWordCell(t, m, "click")
			press := tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
			m, _ = m.handleMouse(press, mouseTestTime)
			m, _ = m.handleMouse(press, mouseTestTime.Add(time.Millisecond))
			if m.ed.SelectedText() != "click" {
				t.Fatalf("size %v, top %d selected %q; want click", size, top, m.ed.SelectedText())
			}
		}
	}
}
