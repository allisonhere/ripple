// Command rippletest is a standalone harness for exercising the ripple editor in
// a real terminal — the "verify before integrating" step. It toggles between
// plain and vim input modes (F2), shows the active vim mode and ":" command
// line, renders a mode-aware cursor (block in Normal/Visual, bar in Insert), and
// reports the Submit/Cancel intents that ":w"/":q"/double-Esc emit.
//
// F5 toggles host mouse handling (initially off). Use --mouse-demo for editable
// sample content.
//
// Run it with: go run ./cmd/rippletest
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/allisonhere/ripple"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// sysClip shells out to the OS clipboard so copy/cut/paste round-trip with the
// real system clipboard — clipboard integration is the host's job, not ripple's.
// This mirrors how TideMail injects its own clipboard via SetClipboard.
type sysClip struct{}

func (sysClip) Read() (string, error) {
	for _, c := range [][]string{
		{"wl-paste", "--no-newline"},
		{"xclip", "-selection", "clipboard", "-o"},
		{"xsel", "-b", "-o"},
		{"pbpaste"},
	} {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		out, err := exec.Command(c[0], c[1:]...).Output()
		if err == nil {
			return string(out), nil
		}
	}
	return "", nil
}

func (sysClip) Write(s string) error {
	for _, c := range [][]string{
		{"wl-copy"},
		{"xclip", "-selection", "clipboard"},
		{"xsel", "-b", "-i"},
		{"pbcopy"},
	} {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Stdin = strings.NewReader(s)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return nil
}

var (
	blockCursor  = lipgloss.NewStyle().Reverse(true)
	barCursor    = lipgloss.NewStyle().Background(lipgloss.Color("10")).Foreground(lipgloss.Color("0"))
	selectStyle  = lipgloss.NewStyle().Reverse(true)
	faintStyle   = lipgloss.NewStyle().Faint(true)
	frameStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	modeStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	statusStyle  = lipgloss.NewStyle().Faint(true)
	commandStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
)

type model struct {
	mouseEnabled               bool
	dragging                   bool
	scrollDir                  int
	scrollGeneration           uint64
	clickAt                    time.Time
	clickX, clickY, clickIndex int
	termW, termH               int
	ed                         ripple.Model
	edW                        int
	edH                        int
	status                     string
}

func newModel() model {
	ed := ripple.New()
	ed.SetSize(64, 12)
	ed.SetClipboard(sysClip{})
	ed.SetRegexSearchEnabled(true)
	ed.SetPlaceholder("Type here. F2 toggles vim mode. Ctrl+Q quits.")
	return model{ed: ed, edW: 64, edH: 12, status: "plain mode — Ctrl+F find, Ctrl+R in Find replaces; F2 vim"}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		return m.handleMouse(msg, time.Now())
	case scrollTickMsg:
		return m.handleScrollTick(msg)
	case tea.WindowSizeMsg:
		m.stopDrag()
		m.clickAt = time.Time{}
		m.termW, m.termH = msg.Width, msg.Height
		w := max(1, min(msg.Width-6, 72))
		h := max(1, msg.Height-7)
		m.ed.SetSize(w, h)
		m.edW, m.edH = w, h
		return m, nil

	case tea.KeyMsg:
		m.stopDrag()
		m.clickAt = time.Time{}
		switch msg.Type {
		case tea.KeyF5:
			m.mouseEnabled = !m.mouseEnabled
			return m, nil
		case tea.KeyCtrlQ:
			return m, tea.Quit
		case tea.KeyF2:
			if m.ed.InputMode() == ripple.ModeVim {
				m.ed.SetInputMode(ripple.ModePlain)
				m.status = "plain mode"
			} else {
				m.ed.SetInputMode(ripple.ModeVim)
				m.status = "vim mode — NORMAL. i to insert, : for commands, :w/:q intents"
			}
			return m, nil
		}

	case ripple.SubmitMsg:
		m.status = "→ SubmitMsg received (:w/:wq/:x) — a host would SEND here"
		return m, nil

	case ripple.CancelMsg:
		m.stopDrag()
		m.status = "→ CancelMsg received (:q / double-Esc) — a host would CANCEL here"
		return m, tea.Quit
	}

	var cmd tea.Cmd
	m.ed, cmd = m.ed.Update(msg)
	return m, cmd
}

func (m model) View() string {
	mode := m.ed.Mode()

	// Mode-aware cursor: a block that shows the glyph under it in Normal/Visual,
	// a colored bar in Insert. Plain mode uses the block too.
	opts := ripple.Options{
		Selected:    func(s string) string { return selectStyle.Render(s) },
		Placeholder: func(s string) string { return faintStyle.Render(s) },
	}
	if mode == "INSERT" {
		opts.Cursor = barCursor.Render(" ")
	} else {
		opts.CursorRune = func(s string) string { return blockCursor.Render(s) }
	}

	// ripple renders each line to its own length, not padded to the editor width,
	// so frame it at a fixed width — otherwise the border collapses to the longest
	// line and grows as you type. Height is already exact (View returns edH lines).
	body := frameStyle.Width(m.edW + frameStyle.GetHorizontalPadding()).Render(m.ed.View(opts))

	// Header: the mode indicator and any ":" command line being typed.
	header := "plain"
	if mode != "" {
		header = "-- " + mode + " --"
	}
	head := modeStyle.Render(header)
	if cl := m.ed.CommandLine(); cl != "" {
		head += "   " + commandStyle.Render(cl)
	}

	mouse := "OFF"
	if m.mouseEnabled {
		mouse = "ON"
	}
	diagnostic := fmt.Sprintf("F5 Mouse %s · cursor %d · selection %d · top %d", mouse, m.ed.CursorIndex(), utf8.RuneCountInString(m.ed.SelectedText()), m.ed.ViewportTop())
	// Header and status stay single-line so terminal wrapping cannot shift the
	// editor's mouse origin. The frame already uses the stored editor width.
	clip := func(s string) string {
		if m.termW > 0 {
			return ansi.Truncate(s, m.termW, "")
		}
		return s
	}
	view := strings.Join([]string{clip(head), body, clip(statusStyle.Render(m.status)), clip(statusStyle.Render(diagnostic))}, "\n")
	if m.termH > 0 {
		rows := strings.Split(view, "\n")
		view = strings.Join(rows[:min(len(rows), m.termH)], "\n")
	}
	return view
}

func main() {
	demo := flag.Bool("mouse-demo", false, "load an editable document for mouse interaction testing")
	flag.Parse()
	m := newModel()
	if *demo {
		m.ed.SetValue(mouseDemo())
		m.ed, _ = m.ed.Update(tea.KeyMsg{Type: tea.KeyCtrlHome})
	}
	// Cell motion reports button drags without idle pointer-motion traffic.
	// F5 controls handling; reporting remains enabled for the program lifetime.
	p := tea.NewProgram(m, tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "rippletest:", err)
		os.Exit(1)
	}
}

func mouseDemo() string {
	paragraph := "Mouse editing: click to place the caret, drag to select across wrapped rows, and double-click a word. Hold a drag above or below the editor to extend the selection while scrolling.\n\n"
	var b strings.Builder
	for i := 1; i <= 12; i++ {
		fmt.Fprintf(&b, "Paragraph %d\n%s", i, paragraph)
		b.WriteString("Short line.\n\nPunctuation: hello, world! underscores_and_123; multiple   spaces.\n    Indented text and trailing spaces.   \n")
		b.WriteString("Unicode: café Ελληνικά привет 日本語 中文 界界 🙂.\nhttps://example.org/a/very/long/unbroken/path/to/test/character/wrapping\n\n")
	}
	return b.String()
}
