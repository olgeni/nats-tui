package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

// walkMenu checks one level and everything under it: the accelerators must
// be unique, since menuKeys takes the first match and a duplicate would make
// an item unreachable, and every leaf must have something to run and a
// description for the second row.
func walkMenu(t *testing.T, where string, items []menuItem) {
	t.Helper()
	seen := map[string]string{}
	for _, it := range items {
		a := it.accel()
		if a == "" {
			t.Errorf("%s: %q has no accelerator", where, it.Name)
			continue
		}
		if other, dup := seen[a]; dup {
			t.Errorf("%s: %q and %q both answer to %q", where, other, it.Name, a)
		}
		seen[a] = it.Name
		if !strings.Contains(strings.ToLower(it.Name), a) {
			t.Errorf("%s: %q does not contain its accelerator %q, so the letter cannot be shown", where, it.Name, a)
		}
		if len(it.Items) > 0 {
			if it.Run != nil {
				t.Errorf("%s/%s: a submenu must not also run", where, it.Name)
			}
			walkMenu(t, where+"/"+it.Name, it.Items)
			continue
		}
		if it.Run == nil {
			t.Errorf("%s/%s: a leaf with nothing to run", where, it.Name)
		}
		if it.Desc == "" {
			t.Errorf("%s/%s: a leaf with no description", where, it.Name)
		}
	}
}

// TestMenuAccelerators walks every menu, for every kind of row it can be
// opened on, since the entity commands at the top depend on the selection.
func TestMenuAccelerators(t *testing.T) {
	m, _ := testModel(t)
	press(m, "*") // expand everything, so a consumer row exists
	kinds := map[kind]node{}
	for _, n := range m.rows {
		if _, have := kinds[n.kind]; !have {
			kinds[n.kind] = n
		}
	}
	if len(kinds) < 4 {
		t.Fatalf("the test store gave only %d kinds of row", len(kinds))
	}
	for kind, n := range kinds {
		for _, mm := range []struct {
			name string
			open func(node) tea.Cmd
		}{
			{"Monitor", m.monitor},
			{"Reports", m.reports},
			{"Cluster", m.cluster},
		} {
			m.mn = nil
			mm.open(n)
			if m.mn == nil {
				continue // nothing for this row
			}
			walkMenu(t, mm.name+" on "+n.typeName(), m.mn.root)
			_ = kind
		}
	}
	m.mn = nil
}

// TestMenuNavigation drives the control panel the way a user does.
func TestMenuNavigation(t *testing.T) {
	m, _ := testModel(t)
	press(m, "M")
	if m.mn == nil {
		t.Fatal("M did not open the menu")
	}
	v := m.View()
	for _, want := range []string{"Monitor", "Report", "Check", "Server", "Account", "Rtt"} {
		if !strings.Contains(v, want) {
			t.Errorf("the menu line lacks %q:\n%s", want, v)
		}
	}
	// the second row shows what the highlighted item leads to, before it is
	// entered
	if !strings.Contains(v, "Connections") {
		t.Errorf("the second row does not show Report's items:\n%s", v)
	}

	// → moves along the level, enter descends
	press(m, "right")
	if got := m.mn.here().Name; got != "Check" {
		t.Fatalf("→ landed on %q, want Check", got)
	}
	press(m, "enter")
	if len(m.mn.path) != 1 {
		t.Fatalf("enter did not descend: path %v", m.mn.path)
	}
	if got := m.mn.crumb(); got != "Monitor/Check" {
		t.Errorf("crumb %q, want Monitor/Check", got)
	}
	// esc comes back up one level, not all the way out
	press(m, "esc")
	if m.mn == nil || len(m.mn.path) != 0 {
		t.Fatal("esc left more than one level")
	}
	if got := m.mn.here().Name; got != "Check" {
		t.Errorf("esc came back to %q, want the item it entered", got)
	}
	// esc at the top closes it
	press(m, "esc")
	if m.mn != nil {
		t.Error("esc at the top did not close the menu")
	}

	// a letter picks its item outright: M then S is the Server submenu
	press(m, "M", "s")
	if m.mn == nil || m.mn.crumb() != "Monitor/Server" {
		t.Fatalf("M s did not open Monitor/Server: %v", m.mn)
	}
	// and the opening key closes it from any level
	press(m, "M")
	if m.mn != nil {
		t.Error("M did not close the menu it opened")
	}
}

// TestMenuSwallowsKeys is the reason menuKeys never falls through: a letter
// that accelerates nothing must not reach the tree, where d deletes.
func TestMenuSwallowsKeys(t *testing.T) {
	m, _ := testModel(t)
	before := len(m.rows)
	press(m, "M")
	press(m, "z", "d", "x", "q")
	if m.mn == nil {
		t.Fatal("an unbound letter closed the menu")
	}
	if m.scr != scrMain {
		t.Errorf("a key reached the tree behind the menu: screen %v", m.scr)
	}
	if len(m.rows) != before {
		t.Errorf("the rows changed under the menu: %d then %d", before, len(m.rows))
	}
	press(m, "esc")
}
