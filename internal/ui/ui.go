// Package ui handles all terminal output with a soft pastel "kawaii" theme built
// on Lip Gloss. Styling degrades automatically: Lip Gloss detects the terminal's
// color support and NO_COLOR, and when stdout is not a TTY we drop icons/boxes so
// piping into hooks stays clean and parseable.
package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// isTTY reports whether stdout is an interactive terminal. When false we emit
// plain, icon-free lines so hook/pipe output is unambiguous.
var isTTY = term.IsTerminal(int(os.Stdout.Fd()))

// quiet suppresses all output. The dashboard sets this while running actions
// under the alt-screen, where stray stdout writes would corrupt the display.
var quiet bool

// SetQuiet toggles output suppression and returns the previous value so callers
// can restore it.
func SetQuiet(q bool) bool {
	prev := quiet
	quiet = q
	return prev
}

// Pastel kawaii palette. Adaptive so it stays legible on light and dark
// backgrounds; Lip Gloss down-samples to the terminal's real color depth.
var (
	pink     = lipgloss.AdaptiveColor{Light: "#D6559B", Dark: "#F6A6D0"} // primary
	lavender = lipgloss.AdaptiveColor{Light: "#7C6BC4", Dark: "#C7B6F5"}
	mint     = lipgloss.AdaptiveColor{Light: "#1F9E7A", Dark: "#A6F0D4"} // success
	peach    = lipgloss.AdaptiveColor{Light: "#C77A2E", Dark: "#F7CBA0"} // warning
	rose     = lipgloss.AdaptiveColor{Light: "#C64157", Dark: "#F5A3B0"} // error
	sky      = lipgloss.AdaptiveColor{Light: "#3B7CB5", Dark: "#AFD6F5"}
	muted    = lipgloss.AdaptiveColor{Light: "#8A8595", Dark: "#8C879A"} // dim
)

// Reusable styles.
var (
	styleBold  = lipgloss.NewStyle().Bold(true)
	styleOk    = lipgloss.NewStyle().Foreground(mint)
	styleWarn  = lipgloss.NewStyle().Foreground(peach)
	styleErr   = lipgloss.NewStyle().Foreground(rose).Bold(true)
	styleNote  = lipgloss.NewStyle().Foreground(muted).Italic(true)
	styleLabel = lipgloss.NewStyle().Foreground(lavender)
	styleValue = lipgloss.NewStyle().Foreground(sky)
	styleTitle = lipgloss.NewStyle().Foreground(pink).Bold(true)

	styleBanner = lipgloss.NewStyle().
			Foreground(pink).
			Bold(true).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lavender).
			Padding(0, 2)
)

// Kawaii emoji iconography. Blanked on non-TTY so machine consumers get clean
// text. Chosen to render on most emoji-capable terminals.
type icons struct{ ok, warn, err, note, info, spark, cat, flower, link, cloud string }

func loadIcons() icons {
	if !isTTY {
		return icons{} // no icons when piped
	}
	return icons{
		ok:     "✨ ",
		warn:   "⚠️  ",
		err:    "✗ ",
		note:   "♡ ",
		info:   "✿ ",
		spark:  "✧",
		cat:    "₍ᐢ•ﻌ•ᐢ₎",
		flower: "🌸",
		link:   "🔗",
		cloud:  "☁️",
	}
}

var ic = loadIcons()

// out writes to stdout; msgs write to stderr for warn/err so stdout stays the
// "result" stream. Suppressed entirely when quiet.
func line(w *os.File, s string) {
	if quiet {
		return
	}
	fmt.Fprintln(w, s)
}

// Bold returns s in bold (styled only on a TTY).
func Bold(s string) string {
	if !isTTY {
		return s
	}
	return styleBold.Render(s)
}

// Info prints a plain informational line with a flower bullet.
func Info(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	if msg == "" {
		line(os.Stdout, "")
		return
	}
	line(os.Stdout, ic.info+msg)
}

// Note prints a dimmed, italic secondary line with a heart.
func Note(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	line(os.Stdout, styleNote.Render(ic.note+msg))
}

// Ok prints a mint success line with a sparkle.
func Ok(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	line(os.Stdout, styleOk.Render(ic.ok+msg))
}

// Warn prints a peach warning line to stderr.
func Warn(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	line(os.Stderr, styleWarn.Render(ic.warn+msg))
}

// Err prints a rose error line to stderr.
func Err(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	line(os.Stderr, styleErr.Render(ic.err+msg))
}

// Field prints an aligned "label: value" row, used for the setup/status headers.
func Field(label, format string, a ...any) {
	val := fmt.Sprintf(format, a...)
	if !isTTY {
		line(os.Stdout, fmt.Sprintf("%-13s %s", label+":", val))
		return
	}
	l := styleLabel.Render(fmt.Sprintf("%-12s", label))
	line(os.Stdout, "  "+l+" "+styleValue.Render(val))
}

// Banner prints a rounded, bordered section header.
func Banner(title string) {
	if !isTTY {
		line(os.Stdout, "")
		line(os.Stdout, "== "+title+" ==")
		return
	}
	line(os.Stdout, "")
	line(os.Stdout, styleBanner.Render(ic.spark+" "+title+" "+ic.spark))
}

// Title prints the app title card — a cute header shown at the top of commands
// that produce a report (setup/status/doctor). Kept subtle so it's not noisy.
func Title(sub string) {
	if !isTTY {
		return
	}
	name := styleTitle.Render("mneme")
	tag := styleNote.Render(sub)
	line(os.Stdout, fmt.Sprintf("%s  %s  %s", ic.flower, name, tag))
	line(os.Stdout, styleNote.Render(strings.Repeat("─", 48)))
}

// Done prints a friendly closing flourish with the kawaii cat.
func Done(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	if !isTTY {
		line(os.Stdout, msg)
		return
	}
	line(os.Stdout, styleOk.Render(ic.cat+"  "+msg))
}

// Die prints a fatal error and exits with status 1.
func Die(format string, a ...any) {
	Err(format, a...)
	os.Exit(1)
}

// IsTTY exposes whether output is interactive, so callers (e.g. the spinner) can
// decide whether to animate.
func IsTTY() bool { return isTTY }

// Palette accessors for the spinner, so it matches the theme.
func SpinnerColor() lipgloss.AdaptiveColor { return pink }

// Theme exposes the palette so other packages (the dashboard) render in the same
// kawaii colors without redefining them.
type Theme struct {
	Pink, Lavender, Mint, Peach, Rose, Sky, Muted lipgloss.AdaptiveColor
}

// Palette returns the shared kawaii color palette.
func Palette() Theme {
	return Theme{Pink: pink, Lavender: lavender, Mint: mint, Peach: peach, Rose: rose, Sky: sky, Muted: muted}
}

// Emoji exposes the icon set for the dashboard.
func Emoji() (flower, spark, cat, heart, link, cloud string) {
	i := loadIcons()
	// loadIcons blanks these on non-TTY; the dashboard only runs on a TTY, so
	// fall back to the literal glyphs when empty.
	if i.flower == "" {
		return "🌸", "✧", "₍ᐢ•ﻌ•ᐢ₎", "♡", "🔗", "☁️"
	}
	return i.flower, i.spark, i.cat, i.note, i.link, i.cloud
}
