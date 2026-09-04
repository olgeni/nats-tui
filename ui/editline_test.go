package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func elKey(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }
func elRunes(s string) tea.KeyMsg    { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// TestEditLine: what is typed replaces the pre-filled value, every other
// key edits it where it is.
func TestEditLine(t *testing.T) {
	f := newEditLine("tank/ROOT/default")
	f.Focus()
	if !f.Pristine() || f.Value() != "tank/ROOT/default" || f.Empty() {
		t.Fatalf("start: %q pristine %v", f.Value(), f.Pristine())
	}
	// backspace keeps the value and deletes its last character
	f.Update(elKey(tea.KeyBackspace))
	if f.Pristine() || f.Value() != "tank/ROOT/defaul" {
		t.Fatalf("backspace: %q pristine %v", f.Value(), f.Pristine())
	}
	// the cursor moves inside the text
	f.Update(elKey(tea.KeyLeft))
	f.Update(elKey(tea.KeyLeft))
	f.Update(elRunes("X"))
	if f.Value() != "tank/ROOT/defaXul" {
		t.Fatalf("left left X: %q", f.Value())
	}
	f.Update(elKey(tea.KeyHome))
	f.Update(elKey(tea.KeyDelete))
	if f.Value() != "ank/ROOT/defaXul" {
		t.Fatalf("home delete: %q", f.Value())
	}
	// typing over a value that has not been touched replaces it
	f = newEditLine("500G")
	f.Focus()
	if !f.Update(elRunes("1")) || f.Value() != "1" {
		t.Fatalf("typing over the pre-filled value: %q", f.Value())
	}
	// Update reports whether the value changed
	if f.Update(elKey(tea.KeyLeft)) {
		t.Error("moving the cursor changed the value")
	}
	// a value that was typed is not pre-filled: it is edited, not replaced
	f.Type("10G")
	if f.Pristine() {
		t.Error("Type left it pre-filled")
	}
	f.Update(elRunes("B"))
	if f.Value() != "10GB" {
		t.Fatalf("typed value: %q", f.Value())
	}
	f.Set("")
	if !f.Empty() || f.Pristine() {
		t.Fatalf("cleared: %q pristine %v", f.Value(), f.Pristine())
	}
	// a blurred field ignores keys
	f.Blur()
	f.Update(elRunes("z"))
	if !f.Empty() {
		t.Fatalf("blurred: %q", f.Value())
	}
	f.Focus()
	f.SetWidth(4)
	f.Update(elRunes("abcdefgh"))
	if v := f.View(); v == "" {
		t.Error("empty view")
	}
}
