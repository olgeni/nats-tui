package ui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// editLine is one line of editable text: the value of what is being
// changed, or a filter. It is a bubbles text input holding the value the
// thing already has: what is typed replaces that value until it is edited,
// while an arrow, a backspace or any other editing key keeps it and works
// inside it. The keys the screen around it keeps for itself never reach
// Update.
type editLine struct {
	ti       textinput.Model
	pristine bool // still the pre-filled value: typing replaces it
}

// newEditLine starts on value: empty for a filter, the current value for
// something that is being changed.
func newEditLine(value string) editLine {
	f := editLine{ti: textinput.New()}
	f.ti.Prompt = ""
	f.ti.CharLimit = 8192
	f.Set(value)
	return f
}

// Set replaces the text (cursor at its end): it is pre-filled again, so
// what is typed next replaces it.
func (f *editLine) Set(s string) {
	f.ti.SetValue(s)
	f.ti.CursorEnd()
	f.pristine = s != ""
}

// Type sets the text as if it had been typed: it is not the pre-filled
// value, so the next key edits it instead of replacing it.
func (f *editLine) Type(s string) {
	f.Set(s)
	f.pristine = false
}

func (f *editLine) Value() string  { return f.ti.Value() }
func (f *editLine) Empty() bool    { return f.ti.Value() == "" }
func (f *editLine) Pristine() bool { return f.pristine }
func (f *editLine) Blur()          { f.ti.Blur() }

// Focus lets the field take keys: a blurred one ignores them.
func (f *editLine) Focus() { f.ti.Focus() }

// SetWidth is the room the text has on its line; a longer value scrolls
// inside it. Zero (the default) is the whole value on one line.
func (f *editLine) SetWidth(w int) {
	f.ti.Width = w
	f.ti.SetCursor(f.ti.Position()) // fits the value to the new width
}

// Update edits the text and reports whether it changed.
func (f *editLine) Update(k tea.KeyMsg) bool {
	before := f.ti.Value()
	if f.pristine {
		// what is typed replaces the pre-filled value; every other key
		// keeps it and edits it where it is
		if k.Type == tea.KeyRunes || k.Type == tea.KeySpace {
			f.ti.SetValue("")
		}
		f.pristine = false
	}
	f.ti, _ = f.ti.Update(k)
	return f.ti.Value() != before
}

// View is the text with the cursor on it when the field is focused. A
// value that is still the pre-filled one is shown from its start, which
// reads better than the end an untouched cursor sits on.
func (f *editLine) View() string {
	if f.pristine {
		if f.ti.Width > 0 {
			return styleMuted.Render(fit(f.ti.Value(), f.ti.Width))
		}
		return styleMuted.Render(f.ti.Value())
	}
	f.ti.TextStyle = styleFocus
	return f.ti.View()
}
