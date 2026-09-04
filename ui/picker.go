package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// pickItem is one choice of a picker.
type pickItem struct {
	label string // shown
	value string // returned
}

// picker is a filterable list of choices: typing filters (the cursor goes
// back to the first match), ↑/↓ move, enter picks, esc cancels. It replaces
// huh's Select for long lists, which keeps a stale cursor/scroll offset
// when the filter changes.
type picker struct {
	title, desc string
	help        string // footer; the default one when empty
	items       []pickItem
	filter      editLine
	cursor      int
	offset      int
	width       int
	height      int
	vis         []int // indexes of the items matching the filter
}

func newPicker(title, desc string, items []pickItem, initial string, width, height int) *picker {
	p := &picker{title: title, desc: desc, items: items, width: width, height: height}
	p.filter = newEditLine("")
	p.filter.Focus()
	p.refilter()
	for i, idx := range p.vis {
		if items[idx].value == initial {
			p.cursor = i
		}
	}
	p.clamp()
	return p
}

func (p *picker) setSize(w, h int) { p.width, p.height = w, h; p.clamp() }

func (p *picker) listHeight() int { return max(3, p.height-5) }

func (p *picker) refilter() {
	f := strings.ToLower(strings.TrimSpace(p.filter.Value()))
	p.vis = p.vis[:0]
	for i, it := range p.items {
		if f == "" || strings.Contains(strings.ToLower(it.label), f) {
			p.vis = append(p.vis, i)
		}
	}
	p.cursor, p.offset = 0, 0
}

func (p *picker) clamp() {
	if p.cursor >= len(p.vis) {
		p.cursor = len(p.vis) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
	h := p.listHeight()
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+h {
		p.offset = p.cursor - h + 1
	}
	if p.offset < 0 {
		p.offset = 0
	}
}

// pickAction is what the picker asks the root model to do after a key.
type pickAction int

const (
	pickNone pickAction = iota
	pickDone
	pickCancel
)

// Update handles a key; on pickDone Value() is the chosen value.
func (p *picker) Update(msg tea.Msg) pickAction {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return pickNone
	}
	switch k.String() {
	case "esc":
		if !p.filter.Empty() {
			p.filter.Set("")
			p.refilter()
			return pickNone
		}
		return pickCancel
	case "enter":
		if len(p.vis) == 0 {
			return pickNone
		}
		return pickDone
	case "up", "ctrl+p":
		p.cursor--
	case "down", "ctrl+n":
		p.cursor++
	case "pgup":
		p.cursor -= p.listHeight()
	case "pgdown":
		p.cursor += p.listHeight()
	case "home":
		p.cursor = 0
	case "end":
		p.cursor = len(p.vis) - 1
	default:
		// every other key edits the filter, inside it as well as at its
		// end: the list keeps only the keys that move in it
		if p.filter.Update(k) {
			p.refilter()
		}
	}
	p.clamp()
	return pickNone
}

// Mouse maps a mouse event onto the picker: the wheel moves, a click
// selects a row, a click on the selected row chooses it. Rows start on
// screen line 3 (title, description, filter).
func (p *picker) Mouse(mm tea.MouseMsg) pickAction {
	switch {
	case mm.Button == tea.MouseButtonWheelUp:
		p.cursor -= 3
	case mm.Button == tea.MouseButtonWheelDown:
		p.cursor += 3
	case mm.Button == tea.MouseButtonLeft && mm.Action == tea.MouseActionPress:
		idx := p.offset + mm.Y - 3
		if mm.Y < 3 || idx < 0 || idx >= len(p.vis) || idx >= p.offset+p.listHeight() {
			return pickNone
		}
		if idx == p.cursor {
			return pickDone
		}
		p.cursor = idx
	}
	p.clamp()
	return pickNone
}

// Value is the value under the cursor ("" when nothing matches).
func (p *picker) Value() string {
	if len(p.vis) == 0 {
		return ""
	}
	return p.items[p.vis[p.cursor]].value
}

func (p *picker) View() string {
	var b strings.Builder
	b.WriteString(titleBar(p.width, p.title) + "\n")
	b.WriteString(" " + styleMuted.Render(fit(p.desc, p.width-2)) + "\n")
	f := " " + styleLabel.Render("Filter: ") + p.filter.View()
	if len(p.vis) != len(p.items) {
		f += styleMuted.Render(fmt.Sprintf("   %d of %d", len(p.vis), len(p.items)))
	}
	b.WriteString(f + "\n")
	h := p.listHeight()
	for i := p.offset; i < len(p.vis) && i < p.offset+h; i++ {
		line := "   " + p.items[p.vis[i]].label
		if i == p.cursor {
			line = styleSelected.Width(p.width).Render(" > " + p.items[p.vis[i]].label)
		}
		b.WriteString(fit2(line, p.width) + "\n")
	}
	if len(p.vis) == 0 {
		b.WriteString(styleMuted.Render("   (no match)") + "\n")
	}
	for i := max(len(p.vis)-p.offset, 1); i < h; i++ {
		b.WriteString("\n")
	}
	if p.help != "" {
		b.WriteString(p.help)
	} else {
		b.WriteString(helpLine("type", "filter", "↑/↓", "move", "enter", "choose", "esc", "cancel"))
	}
	return clamp(p.width, b.String())
}

// fit2 truncates a possibly styled line by plain width only when it has no
// escape codes; styled lines are rendered at the right width already.
func fit2(s string, w int) string {
	if strings.ContainsRune(s, '\x1b') {
		return s
	}
	return fit(s, w)
}
