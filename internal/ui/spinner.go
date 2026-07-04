package ui

import (
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// WithSpinner runs work() while showing a themed spinner labelled with title.
// On a non-interactive terminal it prints a single plain "…" line and just runs
// the work — so hooks and pipes never get animation control codes. The work
// function's error is returned unchanged.
func WithSpinner(title string, work func() error) error {
	if quiet {
		return work()
	}
	if !isTTY {
		fmt.Fprintln(os.Stdout, ic.info+title+"…")
		return work()
	}

	done := make(chan error, 1)
	go func() { done <- work() }()

	m := spinnerModel{
		title: title,
		done:  done,
		sp:    newSpinner(),
	}
	final, err := tea.NewProgram(m, tea.WithOutput(os.Stderr)).Run()
	if err != nil {
		// If the TUI itself fails, fall back to just waiting on the work.
		return <-done
	}
	return final.(spinnerModel).result
}

func newSpinner() spinner.Model {
	s := spinner.New()
	// Dot is a soft, rounded frame set that suits the pastel theme.
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(SpinnerColor())
	return s
}

type spinnerModel struct {
	title  string
	sp     spinner.Model
	done   chan error
	result error
	fin    bool
}

// workDoneMsg is delivered when the background work() finishes.
type workDoneMsg struct{ err error }

func (m spinnerModel) Init() tea.Cmd {
	return tea.Batch(m.sp.Tick, m.waitForWork())
}

// waitForWork blocks on the done channel in a command goroutine and surfaces the
// result as a message so Update can quit the program cleanly.
func (m spinnerModel) waitForWork() tea.Cmd {
	return func() tea.Msg {
		return workDoneMsg{err: <-m.done}
	}
}

func (m spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case workDoneMsg:
		m.result = msg.err
		m.fin = true
		return m, tea.Quit
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		return m, cmd
	case tea.KeyMsg:
		// Ctrl+C cancels the wait but not the underlying git op; we still return.
		if msg.String() == "ctrl+c" {
			m.fin = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m spinnerModel) View() string {
	if m.fin {
		return "" // clear the line; the caller prints the real result
	}
	return fmt.Sprintf("%s %s%s", m.sp.View(), ic.flower, styleNote.Render(" "+m.title+"…"))
}
