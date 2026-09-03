package ui

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// form is a one-group huh form that remembers its group so resize can fix
// the group's height: huh sizes the group's viewport once, when the group is
// built at huh's default width, so a description that wraps at the real
// width would otherwise push the field below the bottom of the viewport.
type form struct {
	*huh.Form
	group *huh.Group
}

func newForm(fields ...huh.Field) *form {
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "cancel"))
	g := huh.NewGroup(fields...)
	return &form{Form: huh.NewForm(g).WithTheme(huhTheme()).WithKeyMap(km).WithShowHelp(true), group: g}
}

// resize sets the form width and recomputes the height for it.
func (f *form) resize(width int) {
	f.Form.WithWidth(width)
	h := lipgloss.Height(f.group.Content())
	for _, s := range []string{f.group.Header(), f.group.Footer()} {
		if s != "" {
			h += lipgloss.Height(s)
		}
	}
	f.Form.WithHeight(h)
}

// confirmForm is a yes/no question.
func confirmForm(title, desc string, value *bool) *form {
	c := huh.NewConfirm().Title(title).Description(desc).Affirmative("Yes").Negative("No").Value(value)
	return newForm(c)
}

// inputForm asks for one string, optionally validated.
func inputForm(title, desc, placeholder string, value *string, validate func(string) error) *form {
	in := huh.NewInput().Title(title).Description(desc).Placeholder(placeholder).Value(value)
	if validate != nil {
		in = in.Validate(validate)
	}
	return newForm(in)
}

// selectForm asks to pick one of a few options (short lists; long ones use
// the picker).
func selectForm(title, desc string, opts []huh.Option[int], value *int) *form {
	sel := huh.NewSelect[int]().Title(title).Description(desc).Options(opts...).Value(value)
	return newForm(sel)
}
