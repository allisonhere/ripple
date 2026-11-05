package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/allisonhere/ripple"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

var mouseTestTime = time.Unix(1000, 0)

type testClipboard struct{ text string }

func (c *testClipboard) Read() (string, error) { return c.text, nil }
func (c *testClipboard) Write(s string) error  { c.text = s; return nil }

type editorState struct {
	text      string
	cursor    int
	selection string
	top       int
}

func state(m model) editorState {
	return editorState{m.ed.Value(), m.ed.CursorIndex(), m.ed.SelectedText(), m.ed.ViewportTop()}
}

func harnessFixture(t testing.TB) model {
	t.Helper()
	m := newModel()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(model)
	var text strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&text, "row-%03d short words 界\n", i)
	}
	m.ed.SetValue(text.String())
	m.ed.ScrollRows(-10000)
	m.ed.PlaceCursor(0, 1)
	return m
}

func enableMouse(t testing.TB, m model) model {
	t.Helper()
	before := state(m)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyF5})
	m = next.(model)
	if cmd != nil || !m.mouseEnabled || state(m) != before {
		t.Fatal("F5 changed editor or did not enable mouse")
	}
	return m
}

func pointer(m *model, action tea.MouseAction, button tea.MouseButton, x, y int) tea.Cmd {
	next, cmd := m.handleMouse(tea.MouseMsg{Action: action, Button: button, X: x, Y: y}, mouseTestTime)
	*m = next
	return cmd
}

func updateHarness(m *model, msg tea.Msg) tea.Cmd {
	next, cmd := m.Update(msg)
	*m = next.(model)
	return cmd
}

func TestMouseHarnessOffAndToggle(t *testing.T) {
	m := harnessFixture(t)
	if m.mouseEnabled || m.Init() != nil {
		t.Fatal("mouse must default OFF without idle work")
	}
	m.ed.ExtendSelectionTo(0, 5)
	before := state(m)
	x, y, w, h := m.editorRect()
	for _, msg := range []tea.Msg{
		tea.MouseMsg{X: x + 1, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress},
		tea.MouseMsg{X: x + w, Y: y + h, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion},
		tea.MouseMsg{Button: tea.MouseButtonNone, Action: tea.MouseActionRelease},
		tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress},
		scrollTickMsg(99),
	} {
		if cmd := updateHarness(&m, msg); cmd != nil || state(m) != before {
			t.Fatal("OFF mouse event changed editor or scheduled work")
		}
	}
	m = enableMouse(t, m)
	if !strings.Contains(ansi.Strip(m.View()), "F5 Mouse ON") {
		t.Fatal("mouse diagnostic missing")
	}
	updateHarness(&m, tea.KeyMsg{Type: tea.KeyF5})
	if m.mouseEnabled || state(m) != before || !strings.Contains(ansi.Strip(m.View()), "F5 Mouse OFF") {
		t.Fatal("disabling changed editor state")
	}
	if got := testing.AllocsPerRun(50, func() { m.handleMouse(tea.MouseMsg{}, mouseTestTime) }); got != 0 {
		t.Fatalf("disabled mouse allocated %g objects", got)
	}
}

func TestMouseHarnessGeometryAndOutsideClicks(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {30, 14}, {12, 9}, {100, 30}} {
		m := enableMouse(t, harnessFixture(t))
		updateHarness(&m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.ed.ScrollRows(-10000)
		m.ed.PlaceCursor(0, 0)
		x, y, w, h := m.editorRect()
		view := strings.Split(ansi.Strip(m.View()), "\n")
		if len(view) <= y || !strings.HasPrefix(string([]rune(view[y])[x:]), "row-") {
			t.Fatalf("geometry mismatch at %v: %q", size, view)
		}
		if cmd := pointer(&m, tea.MouseActionPress, tea.MouseButtonLeft, x+2, y); cmd != nil || m.ed.CursorIndex() != 2 {
			t.Fatal("inside click missed document coordinate")
		}
		before := state(m)
		for _, pos := range [][2]int{{x - 1, y}, {x + w, y}, {x, y - 1}, {x, y + h}, {0, 0}, {size[0] + 100, size[1] + 100}} {
			if cmd := pointer(&m, tea.MouseActionPress, tea.MouseButtonLeft, pos[0], pos[1]); cmd != nil || state(m) != before {
				t.Fatalf("outside click moved editor at %v", pos)
			}
		}
	}
	// A terminal too short to show any editing rows must not accept clicks.
	m := enableMouse(t, harnessFixture(t))
	updateHarness(&m, tea.WindowSizeMsg{Width: 5, Height: 1})
	before := state(m)
	pointer(&m, tea.MouseActionPress, tea.MouseButtonLeft, 2, 2)
	if state(m) != before || m.dragging {
		t.Fatal("clipped editor accepted a click")
	}
}

func TestMouseHarnessWheelAndDiagnostics(t *testing.T) {
	m := enableMouse(t, harnessFixture(t))
	x, y, _, _ := m.editorRect()
	before := state(m)
	if cmd := pointer(&m, tea.MouseActionPress, tea.MouseButtonWheelDown, x, y); cmd != nil {
		t.Fatal("wheel scheduled timer")
	}
	if m.ed.ViewportTop() != wheelStepRows || m.ed.CursorIndex() != before.cursor || m.ed.Value() != before.text {
		t.Fatal("wheel changed cursor/text or wrong scroll distance")
	}
	view := strings.Split(ansi.Strip(m.View()), "\n")
	if !strings.Contains(view[y], "row-003") {
		t.Fatal("rendering undid wheel scrolling")
	}
	if !strings.Contains(strings.Join(view, "\n"), "cursor 1 · selection 0 · top 3") {
		t.Fatal("diagnostics not live")
	}
	pointer(&m, tea.MouseActionPress, tea.MouseButtonWheelUp, x, y)
	if state(m) != before {
		t.Fatal("wheel did not scroll back")
	}
	pointer(&m, tea.MouseActionPress, tea.MouseButtonWheelDown, x-1, y)
	if state(m) != before {
		t.Fatal("wheel outside editor scrolled document")
	}
}

func TestMouseHarnessDragReverseCopyAndRelease(t *testing.T) {
	m := enableMouse(t, harnessFixture(t))
	x, y, _, _ := m.editorRect()
	pointer(&m, tea.MouseActionPress, tea.MouseButtonLeft, x+3, y)
	anchor := m.ed.CursorIndex()
	for _, target := range [][2]int{{5, 2}, {1, 0}, {3, 0}, {7, 3}, {0, 0}} {
		ref := m.ed
		ref.PlaceCursor(target[1], target[0])
		end := ref.CursorIndex()
		if cmd := pointer(&m, tea.MouseActionMotion, tea.MouseButtonLeft, x+target[0], y+target[1]); cmd != nil {
			t.Fatal("inside drag scheduled a tick")
		}
		want := string([]rune(m.ed.Value())[min(anchor, end):max(anchor, end)])
		if m.ed.SelectedText() != want || m.ed.CursorIndex() != end {
			t.Fatal("drag direction reversal lost original anchor")
		}
	}
	selected := m.ed.SelectedText()
	pointer(&m, tea.MouseActionRelease, tea.MouseButtonNone, -1, -1)
	if m.dragging {
		t.Fatal("release did not stop gesture")
	}
	before := state(m)
	pointer(&m, tea.MouseActionMotion, tea.MouseButtonLeft, x+10, y+4)
	if state(m) != before {
		t.Fatal("motion after release changed selection")
	}
	clip := &testClipboard{}
	m.ed.SetClipboard(clip)
	cmd := updateHarness(&m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("copy command missing")
	}
	updateHarness(&m, cmd())
	if clip.text != selected {
		t.Fatal("copy lost drag selection")
	}
	cmd = updateHarness(&m, tea.KeyMsg{Type: tea.KeyCtrlX})
	if cmd == nil {
		t.Fatal("cut command missing")
	}
	updateHarness(&m, cmd())
	updateHarness(&m, tea.KeyMsg{Type: tea.KeyCtrlZ})
	if m.ed.Value() != before.text {
		t.Fatal("undo did not restore cut mouse selection")
	}
	pointer(&m, tea.MouseActionPress, tea.MouseButtonLeft, x+1, y)
	if !m.dragging || m.ed.SelectedText() != "" || m.ed.CursorIndex() != 1 {
		t.Fatal("new click extended earlier selection")
	}
}

func TestMouseHarnessDoubleClicks(t *testing.T) {
	for _, tc := range []struct {
		name       string
		elapsed    time.Duration
		dx, scroll int
		want       bool
	}{
		{"same cell", 50 * time.Millisecond, 0, 0, true}, {"time boundary", doubleClickWindow, 0, 0, true},
		{"expired", doubleClickWindow + time.Nanosecond, 0, 0, false}, {"other cell", 50 * time.Millisecond, 1, 0, false},
		{"clock reversed", -time.Millisecond, 0, 0, false}, {"same cell different document offset", 50 * time.Millisecond, 0, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := enableMouse(t, harnessFixture(t))
			x, y, _, _ := m.editorRect()
			press := tea.MouseMsg{X: x + 1, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
			m, _ = m.handleMouse(press, mouseTestTime)
			m, _ = m.handleMouse(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonNone}, mouseTestTime)
			if tc.scroll != 0 {
				m.ed.ScrollRows(tc.scroll)
			}
			press.X += tc.dx
			m, _ = m.handleMouse(press, mouseTestTime.Add(tc.elapsed))
			if (m.ed.SelectedText() != "") != tc.want || m.dragging == tc.want {
				t.Fatal("double-click detection disagrees with timing/position/document identity")
			}
		})
	}
	for _, tc := range []struct {
		col  int
		want string
	}{{8, "word_here"}, {15, ","}, {16, ""}} {
		m := enableMouse(t, harnessFixture(t))
		m.ed.SetValue("hello word_here, next\n\nlast")
		m.ed.ScrollRows(-10000)
		x, y, _, _ := m.editorRect()
		msg := tea.MouseMsg{X: x + tc.col, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
		m, _ = m.handleMouse(msg, mouseTestTime)
		m, _ = m.handleMouse(msg, mouseTestTime.Add(time.Millisecond))
		if m.ed.SelectedText() != tc.want || m.dragging {
			t.Fatalf("double click selected %q; want %q", m.ed.SelectedText(), tc.want)
		}
		before := state(m)
		pointer(&m, tea.MouseActionMotion, tea.MouseButtonLeft, x+20, y+1)
		if state(m) != before {
			t.Fatal("double-click-and-drag must remain unsupported")
		}
		m, _ = m.handleMouse(msg, mouseTestTime.Add(2*time.Millisecond))
		if m.ed.SelectedText() != "" || !m.dragging {
			t.Fatal("third click did not start ordinary gesture")
		}
	}
}

func startAutoscroll(t *testing.T, m *model, dir int) scrollTickMsg {
	t.Helper()
	x, y, _, h := m.editorRect()
	pointer(m, tea.MouseActionPress, tea.MouseButtonLeft, x+1, y+1)
	outside := y + h + 2
	if dir < 0 {
		outside = y - 2
	}
	if cmd := pointer(m, tea.MouseActionMotion, tea.MouseButtonLeft, x+2, outside); cmd == nil || m.scrollDir != dir {
		t.Fatal("drag did not start autoscroll")
	}
	return scrollTickMsg(m.scrollGeneration)
}

func TestMouseHarnessAutoscrollLoopAndReversal(t *testing.T) {
	if dragScrollInterval != 75*time.Millisecond {
		t.Fatal("autoscroll interval changed")
	}
	m := enableMouse(t, harnessFixture(t))
	m.ed.ScrollRows(10)
	ref := m.ed
	ref.PlaceCursor(1, 1)
	anchor := ref.CursorIndex()
	token := startAutoscroll(t, &m, 1)
	original := m.ed.Value()
	x, y, w, h := m.editorRect()
	for i := 0; i < 8; i++ {
		top := m.ed.ViewportTop()
		var cmd tea.Cmd
		m, cmd = m.handleScrollTick(token)
		if cmd == nil || m.ed.ViewportTop() != top+1 || m.ed.SelectedText() == "" {
			t.Fatal("downward tick failed")
		}
		end := m.ed.CursorIndex()
		if m.ed.SelectedText() != string([]rune(original)[min(anchor, end):max(anchor, end)]) {
			t.Fatal("autoscroll lost its original selection anchor")
		}
		if !strings.Contains(ansi.Strip(m.View()), fmt.Sprintf("row-%03d", top+1)) {
			t.Fatal("rendering defeated autoscroll")
		}
	}
	// Drive consecutive ticks on the same live model, preserving its anchor.
	for i := 0; i < 5; i++ {
		var cmd tea.Cmd
		m, cmd = m.handleScrollTick(token)
		if cmd == nil {
			t.Fatal("held pointer did not continue scrolling")
		}
	}
	top := m.ed.ViewportTop()
	if cmd := pointer(&m, tea.MouseActionMotion, tea.MouseButtonLeft, x+w+100, y+h+3); cmd != nil {
		t.Fatal("motion started overlapping loop")
	}
	if cmd := pointer(&m, tea.MouseActionMotion, tea.MouseButtonLeft, x, y-2); cmd != nil {
		t.Fatal("direction reversal started another loop")
	}
	m, cmd := m.handleScrollTick(token)
	if cmd == nil || m.ed.ViewportTop() != top-1 {
		t.Fatal("existing loop did not reverse upward")
	}
	pointer(&m, tea.MouseActionMotion, tea.MouseButtonLeft, x+2, y+1)
	before := state(m)
	m, cmd = m.handleScrollTick(token)
	if cmd != nil || state(m) != before || m.scrollDir != 0 {
		t.Fatal("inside motion did not cancel old tick")
	}
	if m.ed.Value() != original {
		t.Fatal("autoscrolling edited document")
	}
}

func TestMouseHarnessAutoscrollCancellation(t *testing.T) {
	for _, stop := range []string{"release", "keyboard", "resize", "F5", "F2", "search", "outside click", "right click", "wheel"} {
		t.Run(stop, func(t *testing.T) {
			m := enableMouse(t, harnessFixture(t))
			token := startAutoscroll(t, &m, 1)
			x, y, w, h := m.editorRect()
			switch stop {
			case "release":
				pointer(&m, tea.MouseActionRelease, tea.MouseButtonNone, 0, 0)
			case "keyboard":
				updateHarness(&m, tea.KeyMsg{Type: tea.KeyLeft})
			case "resize":
				updateHarness(&m, tea.WindowSizeMsg{Width: 60, Height: 20})
			case "F5":
				updateHarness(&m, tea.KeyMsg{Type: tea.KeyF5})
			case "F2":
				updateHarness(&m, tea.KeyMsg{Type: tea.KeyF2})
			case "search":
				updateHarness(&m, tea.KeyMsg{Type: tea.KeyCtrlF})
			case "outside click":
				pointer(&m, tea.MouseActionPress, tea.MouseButtonLeft, x+w, y+h)
			case "right click":
				pointer(&m, tea.MouseActionPress, tea.MouseButtonRight, x, y)
			case "wheel":
				pointer(&m, tea.MouseActionPress, tea.MouseButtonWheelUp, x, y)
			}
			if m.dragging || m.scrollDir != 0 {
				t.Fatal("gesture did not stop")
			}
			before := state(m)
			for i := 0; i < 5; i++ {
				next, cmd := m.handleScrollTick(token)
				m = next
				if cmd != nil || state(m) != before {
					t.Fatal("stale tick revived cancelled gesture")
				}
			}
		})
	}
}

func TestMouseHarnessAutoscrollBoundariesAndStaleGeneration(t *testing.T) {
	m := enableMouse(t, harnessFixture(t))
	for _, dir := range []int{-1, 1} {
		m.ed.ScrollRows(dir * 10000)
		token := startAutoscroll(t, &m, dir)
		top := m.ed.ViewportTop()
		next, cmd := m.handleScrollTick(token)
		m = next
		if cmd != nil || m.scrollDir != 0 || m.ed.ViewportTop() != top {
			t.Fatal("document boundary did not stop timer")
		}
	}
	m.ed.ScrollRows(-10000)
	old := startAutoscroll(t, &m, 1)
	pointer(&m, tea.MouseActionRelease, tea.MouseButtonNone, 0, 0)
	newToken := startAutoscroll(t, &m, 1)
	if old == newToken {
		t.Fatal("new gesture reused old generation")
	}
	before := state(m)
	next, cmd := m.handleScrollTick(old)
	m = next
	if cmd != nil || state(m) != before || m.scrollDir != 1 {
		t.Fatal("old tick affected new gesture")
	}
	next, cmd = m.handleScrollTick(newToken)
	m = next
	if cmd == nil || m.ed.ViewportTop() != before.top+1 {
		t.Fatal("new gesture's tick was rejected")
	}
}

func TestMouseHarnessKeyboardSearchAndVim(t *testing.T) {
	m := enableMouse(t, harnessFixture(t))
	x, y, _, _ := m.editorRect()
	updateHarness(&m, tea.KeyMsg{Type: tea.KeyF2})
	if m.ed.InputMode() != ripple.ModeVim {
		t.Fatal("F2 did not enable Vim")
	}
	pointer(&m, tea.MouseActionPress, tea.MouseButtonLeft, x+1, y)
	pointer(&m, tea.MouseActionMotion, tea.MouseButtonLeft, x+4, y)
	clip := &testClipboard{}
	m.ed.SetClipboard(clip)
	selected := m.ed.SelectedText()
	cmd := updateHarness(&m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("Vim mouse copy failed")
	}
	cmd()
	if clip.text != selected || m.dragging {
		t.Fatal("copy did not preserve selection/end gesture")
	}
	before := state(m)
	updateHarness(&m, tea.KeyMsg{Type: tea.KeyF5})
	if m.ed.InputMode() != ripple.ModeVim || state(m) != before {
		t.Fatal("F5 altered Vim mode or selection")
	}
	updateHarness(&m, tea.KeyMsg{Type: tea.KeyF5})
	updateHarness(&m, tea.KeyMsg{Type: tea.KeyF2})
	updateHarness(&m, tea.KeyMsg{Type: tea.KeyCtrlF})
	before = state(m)
	pointer(&m, tea.MouseActionPress, tea.MouseButtonLeft, x, y)
	pointer(&m, tea.MouseActionPress, tea.MouseButtonWheelDown, x, y)
	if state(m) != before || !m.ed.SearchActive() {
		t.Fatal("mouse disturbed Find bar")
	}
	updateHarness(&m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.ed.SearchActive() {
		t.Fatal("keyboard search close failed")
	}
	if utf8.RuneCountInString(mouseDemo()) < 2000 || !strings.Contains(mouseDemo(), "界") || strings.Count(mouseDemo(), "\n") < 50 {
		t.Fatal("mouse demo lacks scrolling/Unicode content")
	}
	m.ed.SetValue(mouseDemo())
	m.ed.InsertString("editable")
	value := m.ed.Value()
	updateHarness(&m, tea.KeyMsg{Type: tea.KeyF5})
	updateHarness(&m, tea.KeyMsg{Type: tea.KeyF5})
	if m.ed.Value() != value || !m.ed.Undo() || m.ed.Value() != mouseDemo() {
		t.Fatal("toggle reloaded demo or changed undo history")
	}
}

func BenchmarkMouseHarness(b *testing.B) {
	for _, operation := range []string{"drag", "wheel", "wheel+view"} {
		b.Run(operation, func(b *testing.B) {
			m := enableMouse(b, harnessFixture(b))
			x, y, w, h := m.editorRect()
			pointer(&m, tea.MouseActionPress, tea.MouseButtonLeft, x, y)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if operation == "drag" {
					pointer(&m, tea.MouseActionMotion, tea.MouseButtonLeft, x+i%w, y+i%h)
				} else {
					button := tea.MouseButtonWheelDown
					if i%2 == 0 {
						button = tea.MouseButtonWheelUp
					}
					pointer(&m, tea.MouseActionPress, button, x, y)
					if operation == "wheel+view" {
						_ = m.View()
					}
				}
			}
		})
	}
}
