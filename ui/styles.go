package ui

import (
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

var (
	colAccent = lipgloss.AdaptiveColor{Light: "#005f87", Dark: "#5fd7ff"}
	colMuted  = lipgloss.AdaptiveColor{Light: "#6c6c6c", Dark: "#8a8a8a"}
	colAllow  = lipgloss.AdaptiveColor{Light: "#008700", Dark: "#5fd75f"}
	colDeny   = lipgloss.AdaptiveColor{Light: "#af0000", Dark: "#ff5f5f"}
	colWarn   = lipgloss.AdaptiveColor{Light: "#af5f00", Dark: "#ffaf00"}
	colSelBg  = lipgloss.AdaptiveColor{Light: "#d7e4f2", Dark: "#3a3f5c"}

	styleTitle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#ffffff"}).
			Background(lipgloss.AdaptiveColor{Light: "#005f87", Dark: "#1f3f5f"}).
			Padding(0, 1)
	styleLabel    = lipgloss.NewStyle().Foreground(colMuted)
	styleValue    = lipgloss.NewStyle().Bold(true)
	styleHeader   = lipgloss.NewStyle().Foreground(colAccent).Bold(true).Underline(true)
	styleSelected = lipgloss.NewStyle().Background(colSelBg).Bold(true)
	styleAllow    = lipgloss.NewStyle().Foreground(colAllow)
	styleDeny     = lipgloss.NewStyle().Foreground(colDeny)
	styleMuted    = lipgloss.NewStyle().Foreground(colMuted)
	styleWarn     = lipgloss.NewStyle().Foreground(colWarn)
	styleErr      = lipgloss.NewStyle().Foreground(colDeny).Bold(true)
	styleOK       = lipgloss.NewStyle().Foreground(colAllow).Bold(true)
	styleHelpKey  = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleHelpDesc = lipgloss.NewStyle().Foreground(colMuted)
	styleFocus    = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleButton   = lipgloss.NewStyle().Padding(0, 2).Foreground(colMuted)
	styleButtonOn = lipgloss.NewStyle().Padding(0, 2).Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#000000"}).
			Background(colAccent)
	styleBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colMuted).Padding(0, 1)
)

func huhTheme() *huh.Theme {
	t := huh.ThemeBase()
	t.Focused.Title = t.Focused.Title.Foreground(colAccent).Bold(true)
	t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(colAccent)
	t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(colAccent).Bold(true)
	t.Focused.FocusedButton = t.Focused.FocusedButton.Background(colAccent)
	t.Focused.Description = t.Focused.Description.Foreground(colMuted)
	return t
}

// titleBar renders the title bar of a screen: styleTitle spans the whole
// width, and the text is truncated first — lipgloss wraps what does not
// fit, which would make the bar two rows high and push everything down.
func titleBar(width int, s string) string {
	if width < 4 {
		return ""
	}
	// the style pads on both sides, and Width() counts that padding, so the
	// text has to fit in what is left or lipgloss wraps it onto a second row
	return styleTitle.Width(width).Render(fitCells(s, width-styleTitle.GetHorizontalFrameSize()))
}

// fitCells truncates s to w terminal cells. fit counts runes, which is the
// same thing only for narrow characters; a title with a double-width rune
// would still wrap, so the title bar measures cells.
func fitCells(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > w {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

// clamp truncates every line of a rendered screen to the width, keeping
// the escape sequences intact.
func clamp(width int, s string) string {
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}

// helpLine renders "key desc · key desc" pairs.
func helpLine(pairs ...string) string {
	s := ""
	for i := 0; i+1 < len(pairs); i += 2 {
		if i > 0 {
			s += styleHelpDesc.Render(" · ")
		}
		s += styleHelpKey.Render(pairs[i]) + " " + styleHelpDesc.Render(pairs[i+1])
	}
	return s
}

// fit pads or truncates s (by rune count) to exactly w cells (plain text).
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) > w {
		if w == 1 {
			return "…"
		}
		return string(r[:w-1]) + "…"
	}
	for len(r) < w {
		r = append(r, ' ')
	}
	return string(r)
}

// fitLeft truncates s from the left (keeping the tail, which is the most
// informative part of a path) so it is at most w cells wide. A width of
// zero or less leaves nothing: returning s would be worse than useless,
// since callers use this to keep a line inside the screen.
func fitLeft(s string, w int) string {
	r := []rune(s)
	if w <= 0 {
		return ""
	}
	if len(r) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return "…" + string(r[len(r)-(w-1):])
}
