// Package dashboard is an interactive full-screen Bubble Tea UI in the kawaii
// theme. It shows the current memory-sync state and lets the user sync, push,
// pull, and refresh with single keypresses. It is the `dashboard` (alias `ui`)
// subcommand; the one-shot CLI commands remain the primary interface.
package dashboard

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/SimCubeLtd/mneme/internal/sync"
	"github.com/SimCubeLtd/mneme/internal/ui"
)

// Run launches the dashboard for the given resolved config. It blocks until the
// user quits.
func Run(c sync.Config) error {
	p := tea.NewProgram(newModel(c), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// ---- messages ----

type snapshotMsg struct{ snap sync.Snapshot }
type actionDoneMsg struct {
	verb string
	err  error
}

// ---- model ----

type model struct {
	cfg       sync.Config
	snap      sync.Snapshot
	loaded    bool
	busy      bool
	busyMsg   string
	status    string // last action result line
	statusErr bool
	sp        spinner.Model
	width     int
	height    int
	quitting  bool
}

func newModel(c sync.Config) model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(ui.Palette().Pink)
	return model{cfg: c, sp: sp, busy: true, busyMsg: "loading"}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.sp.Tick, m.refreshCmd(true))
}

// refreshCmd gathers a fresh snapshot in the background (optionally fetching).
func (m model) refreshCmd(fetch bool) tea.Cmd {
	cfg := m.cfg
	return func() tea.Msg {
		return snapshotMsg{snap: sync.Gather(cfg, fetch)}
	}
}

// actionCmd runs a sync action (sync/push/pull) off the UI thread. The action
// functions print via ui.*, which writes to stdout/stderr; under the alt-screen
// that output is not visible, so we rely on the returned error + a follow-up
// refresh for feedback.
func (m model) actionCmd(verb string) tea.Cmd {
	cfg := m.cfg
	return func() tea.Msg {
		var err error
		switch verb {
		case "sync":
			err = sync.SyncQuiet(cfg)
		case "push":
			err = sync.PushQuiet(cfg)
		case "pull":
			err = sync.PullQuiet(cfg)
		}
		return actionDoneMsg{verb: verb, err: err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case snapshotMsg:
		m.snap = msg.snap
		m.loaded = true
		m.busy = false
		return m, nil

	case actionDoneMsg:
		m.busy = false
		if msg.err != nil {
			m.status = fmt.Sprintf("%s failed: %v", msg.verb, trimErr(msg.err))
			m.statusErr = true
		} else {
			m.status = fmt.Sprintf("%s complete ♡", msg.verb)
			m.statusErr = false
		}
		// Refresh state after any action.
		return m, m.refreshCmd(true)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		if m.busy {
			// Ignore action keys while busy, but allow quit.
			if k := msg.String(); k == "q" || k == "ctrl+c" {
				m.quitting = true
				return m, tea.Quit
			}
			return m, nil
		}
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "r":
			m.busy, m.busyMsg = true, "refreshing"
			return m, tea.Batch(m.sp.Tick, m.refreshCmd(true))
		case "s":
			m.busy, m.busyMsg = true, "syncing"
			return m, tea.Batch(m.sp.Tick, m.actionCmd("sync"))
		case "p":
			m.busy, m.busyMsg = true, "pushing"
			return m, tea.Batch(m.sp.Tick, m.actionCmd("push"))
		case "l":
			m.busy, m.busyMsg = true, "pulling"
			return m, tea.Batch(m.sp.Tick, m.actionCmd("pull"))
		}
	}
	return m, nil
}

func trimErr(err error) string {
	s := err.Error()
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 60 {
		s = s[:57] + "…"
	}
	return s
}
