// Command rippletest is a standalone harness for exercising the ripple editor in
// a real terminal — the "verify before integrating" step. It toggles between
// plain and vim input modes (F2), shows the active vim mode and ":" command
// line, renders a mode-aware cursor (block in Normal/Visual, bar in Insert), and
// reports the Submit/Cancel intents that ":w"/":q"/double-Esc emit.
//
// Run it with: go run ./cmd/rippletest
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/allisonhere/ripple"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	ed     ripple.Model
	edW    int
	edH    int
	status string
}

func newModel() model {
	ed := ripple.New()
	ed.SetSize(64, 12)
	ed.SetClipboard(sysClip{})
	ed.SetPlaceholder("Type here. F2 toggles vim mode. Ctrl+Q quits.")
	return model{ed: ed, edW: 64, edH: 12, status: "plain mode — press F2 for vim"}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		w := min(msg.Width-6, 72)
		h := max(3, msg.Height-7)
		m.ed.SetSize(w, h)
		m.edW, m.edH = w, h
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
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
	body := frameStyle.Width(m.edW).Render(m.ed.View(opts))

	// Header: the mode indicator and any ":" command line being typed.
	header := "plain"
	if mode != "" {
		header = "-- " + mode + " --"
	}
	head := modeStyle.Render(header)
	if cl := m.ed.CommandLine(); cl != "" {
		head += "   " + commandStyle.Render(cl)
	}

	return strings.Join([]string{
		head,
		body,
		statusStyle.Render(m.status),
	}, "\n")
}

func main() {
	p := tea.NewProgram(newModel())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "rippletest:", err)
		os.Exit(1)
	}
}
