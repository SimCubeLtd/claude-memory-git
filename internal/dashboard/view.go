package dashboard

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/SimCubeLtd/mneme/internal/sync"
	"github.com/SimCubeLtd/mneme/internal/ui"
)

// View renders the whole dashboard as a centered kawaii card.
func (m model) View() string {
	if m.quitting {
		return ""
	}
	pal := ui.Palette()
	flower, spark, cat, heart, _, _ := ui.Emoji()

	var (
		title  = lipgloss.NewStyle().Foreground(pal.Pink).Bold(true)
		label  = lipgloss.NewStyle().Foreground(pal.Lavender)
		value  = lipgloss.NewStyle().Foreground(pal.Sky)
		okSt   = lipgloss.NewStyle().Foreground(pal.Mint)
		warnSt = lipgloss.NewStyle().Foreground(pal.Peach)
		errSt  = lipgloss.NewStyle().Foreground(pal.Rose).Bold(true)
		muteSt = lipgloss.NewStyle().Foreground(pal.Muted).Italic(true)
		keyCap = lipgloss.NewStyle().Foreground(pal.Pink).Bold(true)
	)

	var b strings.Builder

	// Header.
	fmt.Fprintf(&b, "%s  %s  %s\n", flower, title.Render("mneme"), muteSt.Render("dashboard"))
	b.WriteString(muteSt.Render(strings.Repeat("─", 52)) + "\n\n")

	if !m.loaded {
		b.WriteString(fmt.Sprintf("%s %s\n", m.sp.View(), muteSt.Render("loading…")))
		return m.frame(card(b.String(), pal, m.cardWidth()))
	}

	s := m.snap
	row := func(k, v string) {
		fmt.Fprintf(&b, "%s %s\n", label.Render(pad(k, 10)), value.Render(v))
	}
	row("bucket", s.Project)
	row("branch", s.Branch)
	if s.RemoteURL != "" {
		row("remote", fmt.Sprintf("%s → %s", s.Remote, s.RemoteURL))
	} else {
		row("remote", muteSt.Render("none"))
	}
	b.WriteString("\n")

	// Link state.
	switch {
	case s.Linked && s.LinkOK:
		b.WriteString(okSt.Render(fmt.Sprintf("%s link healthy — %d file(s)", spark, s.FileCount)) + "\n")
	case s.Linked && !s.LinkOK:
		b.WriteString(errSt.Render("✗ link is broken") + "\n")
	case s.LinkOK:
		b.WriteString(warnSt.Render("⚠ memory not linked yet — run setup") + "\n")
	default:
		b.WriteString(warnSt.Render("⚠ no memory found") + "\n")
	}

	// Repo / sync state.
	if !s.RepoExists {
		b.WriteString(warnSt.Render("⚠ memory repo not initialized — run setup") + "\n")
	} else {
		if s.RebaseStuck {
			b.WriteString(errSt.Render("✗ rebase in progress — resolve in the repo") + "\n")
		}
		if s.RepoClean {
			b.WriteString(okSt.Render(fmt.Sprintf("%s repo clean", spark)) + "\n")
		} else {
			b.WriteString(warnSt.Render("⚠ uncommitted changes — press s to sync") + "\n")
		}
		b.WriteString(syncLine(s, pal, spark, heart) + "\n")
		if s.LastCommit != "" {
			b.WriteString(muteSt.Render("  "+s.LastCommit) + "\n")
		}
	}

	b.WriteString("\n")

	// Busy / status line.
	if m.busy {
		b.WriteString(fmt.Sprintf("%s %s\n", m.sp.View(), muteSt.Render(m.busyMsg+"…")))
	} else if m.status != "" {
		if m.statusErr {
			b.WriteString(errSt.Render("✗ "+m.status) + "\n")
		} else {
			b.WriteString(okSt.Render(cat+"  "+m.status) + "\n")
		}
	} else {
		b.WriteString("\n")
	}

	// Key hints.
	b.WriteString("\n")
	hint := func(k, desc string) string {
		return keyCap.Render(k) + muteSt.Render(" "+desc)
	}
	keys := []string{
		hint("s", "sync"),
		hint("p", "push"),
		hint("l", "pull"),
		hint("r", "refresh"),
		hint("q", "quit"),
	}
	b.WriteString(strings.Join(keys, muteSt.Render("   ")))

	return m.frame(card(b.String(), pal, m.cardWidth()))
}

// cardWidth picks a comfortable content width that scales with the terminal but
// stays readable: ~70% of the screen, clamped to [48, 100] columns.
func (m model) cardWidth() int {
	if m.width <= 0 {
		return 60
	}
	w := m.width * 7 / 10
	if w < 48 {
		w = 48
	}
	if w > 100 {
		w = 100
	}
	// Never exceed what actually fits (minus the border/padding budget).
	if w > m.width-4 {
		w = m.width - 4
	}
	return w
}

// frame centers the rendered card in the full terminal viewport.
func (m model) frame(card string) string {
	if m.width <= 0 || m.height <= 0 {
		return card
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
}

// syncLine renders the ahead/behind position in themed colors.
func syncLine(s sync.Snapshot, pal ui.Theme, spark, heart string) string {
	ok := lipgloss.NewStyle().Foreground(pal.Mint)
	warn := lipgloss.NewStyle().Foreground(pal.Peach)
	mute := lipgloss.NewStyle().Foreground(pal.Muted).Italic(true)

	if !s.HasRemote {
		return mute.Render("  no remote configured (local-only)")
	}
	if !s.RemoteKnown {
		return mute.Render("  remote not reachable / branch not created")
	}
	switch {
	case s.Ahead == 0 && s.Behind == 0:
		return ok.Render(fmt.Sprintf("%s up to date with %s/%s %s", spark, s.Remote, s.Branch, heart))
	case s.Ahead > 0 && s.Behind == 0:
		return warn.Render(fmt.Sprintf("↑ %d to push — press s", s.Ahead))
	case s.Ahead == 0 && s.Behind > 0:
		return warn.Render(fmt.Sprintf("↓ %d to pull — press s", s.Behind))
	default:
		return warn.Render(fmt.Sprintf("↕ diverged (%d ahead, %d behind) — press s", s.Ahead, s.Behind))
	}
}

// card wraps content in a rounded, padded border in the theme's lavender, sized
// to a fixed content width so the box fills the screen rather than shrinking to
// its text.
func card(content string, pal ui.Theme, width int) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Lavender).
		Padding(1, 3).
		Width(width).
		Render(content)
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
