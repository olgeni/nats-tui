package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// A menu in the style of the Lotus 1-2-3 control panel: two rows above the
// tree. The first holds the items of the level the cursor is in, the second
// the items of whichever one is highlighted — so the level below is read
// before it is entered. ←/→ move, a letter picks the item it accelerates
// outright, enter (or ↓) descends or runs, esc (or ↑) leaves one level and
// closes the menu at the top.
//
// The accelerator is the first letter of the name unless Key says otherwise,
// which is how two items of one level (Connections and Cpu) keep a letter
// each. TestMenuAccelerators asserts they stay unique, since the first match
// wins and a duplicate would make an item unreachable.

// menuItem is one word of a menu: a submenu when Items is set, otherwise a
// leaf that runs Run.
type menuItem struct {
	Name  string
	Key   string // accelerator; "" for the first letter of Name
	Desc  string // the second row when the item is a highlighted leaf
	Items []menuItem
	Run   func(m *Model) tea.Cmd
}

// accel is the key that picks the item.
func (i menuItem) accel() string {
	if i.Key != "" {
		return i.Key
	}
	r := []rune(i.Name)
	if len(r) == 0 {
		return ""
	}
	return strings.ToLower(string(r[0]))
}

// menu is an open menu and the path taken into it.
type menu struct {
	key   string // the key that opened it, which closes it again
	title string
	root  []menuItem
	path  []int // the item entered at each level above the current one
	cur   int   // the highlighted item of the current level
}

// items is the level the cursor is in.
func (mn *menu) items() []menuItem {
	items := mn.root
	for _, i := range mn.path {
		items = items[i].Items
	}
	return items
}

// here is the highlighted item.
func (mn *menu) here() menuItem {
	items := mn.items()
	if mn.cur < 0 || mn.cur >= len(items) {
		return menuItem{}
	}
	return items[mn.cur]
}

// crumb is the title followed by the path taken so far.
func (mn *menu) crumb() string {
	out := mn.title
	items := mn.root
	for _, i := range mn.path {
		out += "/" + items[i].Name
		items = items[i].Items
	}
	return out
}

// openMenu shows a menu above the tree; key is what opened it, and closes it
// again from any level.
func (m *Model) openMenu(key, title string, items []menuItem) tea.Cmd {
	if len(items) == 0 {
		m.setStatus("Nothing to show for this row")
		return nil
	}
	m.mn = &menu{key: key, title: title, root: items}
	return nil
}

// closeMenu puts the tree back.
func (m *Model) closeMenu() { m.mn = nil }

// menuKeys drives the open menu. It swallows every key: while the menu is up
// the tree's own bindings must not fire, or a letter that accelerates nothing
// would reach d (delete) or x (seal) behind it.
func (m *Model) menuKeys(k string) tea.Cmd {
	mn := m.mn
	items := mn.items()
	switch k {
	case "left", "shift+tab":
		mn.cur = (mn.cur - 1 + len(items)) % len(items)
	case "right", "tab":
		mn.cur = (mn.cur + 1) % len(items)
	case "home":
		mn.cur = 0
	case "end":
		mn.cur = len(items) - 1
	case "esc", "up":
		m.menuUp()
	case "enter", "down":
		return m.menuChoose(mn.cur)
	default:
		if k == mn.key {
			m.closeMenu()
			return nil
		}
		if len([]rune(k)) != 1 {
			return nil
		}
		for i, it := range items {
			if it.accel() == strings.ToLower(k) {
				return m.menuChoose(i)
			}
		}
	}
	return nil
}

// menuUp leaves one level, or closes the menu when it is at the top.
func (m *Model) menuUp() {
	mn := m.mn
	if len(mn.path) == 0 {
		m.closeMenu()
		return
	}
	mn.cur = mn.path[len(mn.path)-1]
	mn.path = mn.path[:len(mn.path)-1]
}

// menuChoose descends into an item, or closes the menu and runs it.
func (m *Model) menuChoose(i int) tea.Cmd {
	mn := m.mn
	items := mn.items()
	if i < 0 || i >= len(items) {
		return nil
	}
	it := items[i]
	if len(it.Items) > 0 {
		mn.path = append(mn.path, i)
		mn.cur = 0
		return nil
	}
	m.closeMenu()
	if it.Run == nil {
		return nil
	}
	return it.Run(m)
}

// menuView is the two rows of the control panel: the current level, and
// under it what the highlighted item leads to.
func (m *Model) menuView(width int) string {
	mn := m.mn
	items := mn.items()
	var b strings.Builder
	b.WriteString(styleMuted.Render(" " + mn.crumb() + " "))
	for i, it := range items {
		b.WriteString(" ")
		if i == mn.cur {
			b.WriteString(styleSelected.Render(" " + it.Name + " "))
			continue
		}
		b.WriteString(" " + menuWord(it) + " ")
	}
	second := ""
	if here := mn.here(); len(here.Items) > 0 {
		var names []string
		for _, it := range here.Items {
			names = append(names, it.Name)
		}
		second = strings.Join(names, "  ")
	} else {
		second = mn.here().Desc
	}
	return clamp(width, b.String()) + "\n" + styleMuted.Render(" "+fit(second, max(0, width-2)))
}

// menuWord is an item's name with its accelerator picked out, so the letter
// to press is visible without a legend.
func menuWord(it menuItem) string {
	name, a := it.Name, it.accel()
	if a == "" {
		return name
	}
	r := []rune(name)
	for i, c := range r {
		if strings.ToLower(string(c)) == a {
			return string(r[:i]) + styleHelpKey.Render(string(c)) + string(r[i+1:])
		}
	}
	return name + styleMuted.Render("("+a+")")
}
