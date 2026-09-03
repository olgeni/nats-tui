package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/olgeni/nats-tui/cli"
)

// The editor is the dialog every entity is edited in: a column of labelled
// fields (text, number, size, list, checkbox, choice), section headers,
// read-only lines and OK/Cancel buttons — the same shape for operators,
// accounts, users, exports and imports, driven by a field list.

type fieldKind int

const (
	fText    fieldKind = iota // free text
	fList                     // comma-separated list
	fInt                      // count, -1 = unlimited
	fBytes                    // size with k/m/g units, -1 = unlimited
	fBool                     // checkbox
	fChoice                   // ◂ value ▸
	fInfo                     // read-only line
	fSection                  // header
	fButtons                  // OK / Cancel
)

// field is one row of the editor.
type field struct {
	key         string
	kind        fieldKind
	label       string
	desc        string // muted note shown when the row is focused
	text        string // fText/fList/fInt/fBytes (and fInfo) value
	on          bool   // fBool
	choice      int    // fChoice
	choices     []string
	placeholder string
	validate    func(string) error
	// pick, when set, is what enter opens instead of editing the text: a
	// list of the keys or names that make sense here. It appends to the
	// field, so free-form text stays possible (the picker offers it too).
	pick func(m *Model, f *field) tea.Cmd
	// annotate renders the muted line under a focused field from its
	// current value — for key fields it resolves the keys to names.
	annotate func(text string) string
}

type editorAction int

const (
	actNone editorAction = iota
	actOK
	actCancel
	actPick // the focused field asks for its picker
)

// editor is the dialog state.
type editor struct {
	title   string
	fields  []*field
	cursor  int
	col     int // buttons: 0 OK, 1 Cancel
	editing bool
	input   textinput.Model
	width   int
	height  int
	offset  int
	errMsg  string
	help    string

	// screen geometry recorded by View for the mouse and the text input
	labelW             int   // width of the label column
	rowLine            []int // screen line of each field (-1 when not shown)
	okX0, okX1         int
	cancelX0, cancelX1 int
}

func newEditor(title string, fields []*field, width, height int) *editor {
	fields = append(fields, &field{kind: fButtons})
	ed := &editor{title: title, fields: fields, width: width, height: height}
	ed.input = textinput.New()
	ed.input.Prompt = ""
	ed.input.CharLimit = 4096
	for i, f := range fields {
		if f.focusable() {
			ed.cursor = i
			break
		}
	}
	return ed
}

func (f *field) focusable() bool { return f.kind != fInfo && f.kind != fSection }

func (ed *editor) setSize(w, h int) { ed.width, ed.height = w, h }

// text/list/int/bytes/bool/choice accessors by key, for the caller.
func (ed *editor) get(key string) *field {
	for _, f := range ed.fields {
		if f.key == key {
			return f
		}
	}
	return &field{}
}
func (ed *editor) str(key string) string    { return strings.TrimSpace(ed.get(key).text) }
func (ed *editor) list(key string) []string { return cli.SplitList(ed.get(key).text) }
func (ed *editor) on(key string) bool       { return ed.get(key).on }
func (ed *editor) choice(key string) string { f := ed.get(key); return f.choices[f.choice] }
func (ed *editor) int64(key string) int64   { n, _ := cli.ParseLimit(ed.get(key).text); return n }
func (ed *editor) bytes(key string) int64   { n, _ := cli.ParseBytes(ed.get(key).text); return n }
func (ed *editor) setErr(err error)         { ed.errMsg = err.Error() }
func (ed *editor) row() *field              { return ed.fields[ed.cursor] }

// Update handles a key.
func (ed *editor) Update(msg tea.Msg) editorAction {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return actNone
	}
	if ed.editing {
		return ed.updateEditing(km)
	}
	ed.errMsg = ""
	switch km.String() {
	case "esc", "q":
		return actCancel
	case "ctrl+s", "f10":
		return ed.confirm()
	case "up", "k", "shift+tab":
		ed.move(-1)
	case "down", "j", "tab":
		ed.move(1)
	case "pgup":
		for i := 0; i < 8; i++ {
			ed.move(-1)
		}
	case "pgdown":
		for i := 0; i < 8; i++ {
			ed.move(1)
		}
	case "home", "g":
		ed.cursor = 0
		ed.move(1)
		ed.move(-1)
	case "end", "G":
		ed.cursor = len(ed.fields) - 1
	case "left", "h":
		ed.horizontal(-1)
	case "right", "l":
		ed.horizontal(1)
	case " ":
		return ed.activate(false)
	case "enter":
		return ed.activate(true)
	case "e":
		if ed.row().pick != nil {
			ed.startEdit()
		}
	}
	ed.scroll()
	return actNone
}

func (ed *editor) updateEditing(km tea.KeyMsg) editorAction {
	switch km.String() {
	case "esc":
		ed.editing = false
		ed.errMsg = ""
		return actNone
	case "enter", "tab", "down", "up", "shift+tab":
		if err := ed.commitEdit(); err != nil {
			ed.errMsg = err.Error()
			return actNone
		}
		switch km.String() {
		case "tab", "down":
			ed.move(1)
		case "up", "shift+tab":
			ed.move(-1)
		}
		ed.scroll()
		return actNone
	case "ctrl+s", "f10":
		if err := ed.commitEdit(); err != nil {
			ed.errMsg = err.Error()
			return actNone
		}
		return ed.confirm()
	}
	var cmd tea.Cmd
	ed.input, cmd = ed.input.Update(km)
	_ = cmd
	return actNone
}

func (ed *editor) commitEdit() error {
	f := ed.row()
	v := strings.TrimSpace(ed.input.Value())
	if err := f.check(v); err != nil {
		return err
	}
	f.text = v
	ed.editing = false
	ed.errMsg = ""
	return nil
}

// check validates a value for the field's kind and validator.
func (f *field) check(v string) error {
	switch f.kind {
	case fInt:
		if _, err := cli.ParseLimit(v); err != nil {
			return err
		}
	case fBytes:
		if _, err := cli.ParseBytes(v); err != nil {
			return err
		}
	}
	if f.validate != nil {
		return f.validate(v)
	}
	return nil
}

func (ed *editor) move(d int) {
	for i := ed.cursor + d; i >= 0 && i < len(ed.fields); i += d {
		if ed.fields[i].focusable() {
			ed.cursor = i
			return
		}
	}
}

func (ed *editor) horizontal(d int) {
	switch f := ed.row(); f.kind {
	case fChoice:
		n := len(f.choices)
		f.choice = ((f.choice+d)%n + n) % n
	case fBool:
		f.on = !f.on
	case fButtons:
		if d < 0 {
			ed.col = 0
		} else {
			ed.col = 1
		}
	default:
		if d > 0 {
			ed.startEdit()
		}
	}
}

func (ed *editor) startEdit() {
	f := ed.row()
	ed.editing = true
	ed.input.SetValue(f.text)
	ed.input.Placeholder = f.placeholder
	// the input starts after "▸ " and the label column, and a value longer
	// than that scrolls inside it, so it must never be wider than the rest
	// of the line or it would run off the screen
	ed.input.Width = max(12, ed.width-(2+ed.labelWidth()+1)-2)
	ed.input.CursorEnd()
	ed.input.Focus()
}

// labelWidth is the width of the label column (recorded by View; the same
// computation is used before the first View).
func (ed *editor) labelWidth() int {
	if ed.labelW > 0 {
		return ed.labelW
	}
	lw := 22
	for _, f := range ed.fields {
		if f.kind != fSection && f.kind != fButtons && len([]rune(f.label))+1 > lw {
			lw = len([]rune(f.label)) + 1
		}
	}
	return min(lw, 34)
}

func (ed *editor) activate(enter bool) editorAction {
	if f := ed.row(); f.pick != nil && enter {
		return actPick
	}
	switch f := ed.row(); f.kind {
	case fBool:
		f.on = !f.on
	case fChoice:
		ed.horizontal(1)
	case fButtons:
		if ed.col == 0 {
			return ed.confirm()
		}
		return actCancel
	default:
		ed.startEdit()
	}
	return actNone
}

func (ed *editor) confirm() editorAction {
	for i, f := range ed.fields {
		if err := f.check(f.text); err != nil {
			ed.cursor = i
			ed.errMsg = f.label + ": " + err.Error()
			ed.scroll()
			return actNone
		}
	}
	return actOK
}

// listHeight is how many field rows fit between the title and the footer.
func (ed *editor) listHeight() int { return max(4, ed.height-5) }

func (ed *editor) scroll() {
	h := ed.listHeight()
	// keep a section header above the cursor visible when possible
	top := ed.cursor
	if top > 0 && ed.fields[top-1].kind == fSection {
		top--
	}
	if top < ed.offset {
		ed.offset = top
	}
	if ed.cursor >= ed.offset+h {
		ed.offset = ed.cursor - h + 1
	}
	if ed.offset < 0 {
		ed.offset = 0
	}
}

func (ed *editor) View() string {
	var b strings.Builder
	b.WriteString(titleBar(max(ed.width, 20), ed.title) + "\n")
	lw := ed.labelWidth()
	ed.labelW = lw
	h := ed.listHeight()
	shown := 0
	ed.rowLine = make([]int, len(ed.fields))
	for i := range ed.rowLine {
		ed.rowLine[i] = -1
	}
	for i := ed.offset; i < len(ed.fields) && shown < h; i++ {
		f := ed.fields[i]
		focus := i == ed.cursor
		ed.rowLine[i] = 1 + shown // the title is line 0
		mark := "  "
		if focus {
			mark = styleFocus.Render("▸ ")
		}
		label := styleLabel.Render(fit(f.label+":", lw))
		var line string
		switch f.kind {
		case fSection:
			line = "  " + styleHeader.Render(f.label)
		case fInfo:
			if f.label == "" { // a note, not a labelled value
				line = "  " + styleMuted.Render(fit(f.text, max(1, ed.width-4)))
			} else {
				line = "  " + label + " " + styleMuted.Render(fit(f.text, max(1, ed.width-lw-4)))
			}
		case fBool:
			box := checkbox(f.on) + " " + f.label
			if focus {
				box = styleSelected.Render(box)
			}
			line = mark + box
			if f.desc != "" && focus {
				line += styleMuted.Render("  " + f.desc)
			}
		case fChoice:
			v := "◂ " + styleValue.Render(f.choices[f.choice]) + " ▸"
			line = mark + label + " " + v
			if focus && f.desc != "" {
				line += styleMuted.Render("  " + f.desc)
			}
		case fButtons:
			okB, cancelB := styleButton.Render("OK"), styleButton.Render("Cancel")
			if focus {
				if ed.col == 0 {
					okB = styleButtonOn.Render("OK")
				} else {
					cancelB = styleButtonOn.Render("Cancel")
				}
			}
			line = "\n" + mark + fit("", lw) + " " + okB + "  " + cancelB
			ed.rowLine[i]++ // the blank line above the buttons
			ed.okX0 = 2 + lw + 1
			ed.okX1 = ed.okX0 + lipgloss.Width(okB)
			ed.cancelX0 = ed.okX1 + 2
			ed.cancelX1 = ed.cancelX0 + lipgloss.Width(cancelB)
		default:
			if focus && ed.editing {
				line = mark + label + " " + ed.input.View()
			} else {
				v := f.text
				st := styleValue
				if v == "" {
					v = f.placeholder
					st = styleMuted
				}
				w := max(1, ed.width-lw-4)
				hint := ""
				if focus && f.pick != nil {
					hint = styleMuted.Render("   (enter: add/remove, e: type)")
					w = max(1, w-lipgloss.Width(hint))
				}
				line = mark + label + " " + st.Render(fitLeft(v, w)) + hint
			}
		}
		b.WriteString(line + "\n")
		shown++
		if f.kind == fButtons {
			shown++
		}
		if note := f.note(); focus && note != "" && (f.kind == fText || f.kind == fList || f.kind == fInt || f.kind == fBytes) && shown < h {
			b.WriteString("  " + fit("", lw) + " " + styleMuted.Render(fit(note, max(1, ed.width-lw-4))) + "\n")
			shown++
		}
	}
	for ; shown < h; shown++ {
		b.WriteString("\n")
	}
	if ed.errMsg != "" {
		b.WriteString(styleErr.Render(" " + fit(ed.errMsg, ed.width-2)))
	}
	b.WriteString("\n")
	if ed.editing {
		b.WriteString(helpLine("enter/tab", "keep", "esc", "revert", "ctrl+s", "OK"))
	} else {
		b.WriteString(helpLine("↑↓/tab", "move", "enter", "edit / toggle", "←→", "choice", "ctrl+s", "OK", "esc", "cancel"))
	}
	return lipgloss.NewStyle().MaxWidth(ed.width).Render(b.String())
}

// Mouse maps a mouse event onto the editor: the wheel moves, a click
// focuses a field, a click on the focused field activates it (edit,
// toggle, next choice), OK and Cancel are buttons. View must have run.
func (ed *editor) Mouse(mm tea.MouseMsg) editorAction {
	switch {
	case mm.Button == tea.MouseButtonWheelUp:
		if !ed.editing {
			ed.move(-1)
			ed.scroll()
		}
		return actNone
	case mm.Button == tea.MouseButtonWheelDown:
		if !ed.editing {
			ed.move(1)
			ed.scroll()
		}
		return actNone
	case mm.Button != tea.MouseButtonLeft || mm.Action != tea.MouseActionPress:
		return actNone
	}
	row := -1
	for i, ln := range ed.rowLine {
		if ln == mm.Y && ed.fields[i].focusable() {
			row = i
		}
	}
	if row < 0 {
		return actNone
	}
	if ed.editing {
		if row == ed.cursor {
			return actNone
		}
		if err := ed.commitEdit(); err != nil {
			ed.errMsg = err.Error()
			return actNone
		}
	}
	ed.errMsg = ""
	f := ed.fields[row]
	switch f.kind {
	case fButtons:
		ed.cursor = row
		switch {
		case mm.X >= ed.okX0 && mm.X < ed.okX1:
			ed.col = 0
			return ed.confirm()
		case mm.X >= ed.cancelX0 && mm.X < ed.cancelX1:
			ed.col = 1
			return actCancel
		}
		return actNone
	case fBool, fChoice:
		ed.cursor = row
		return ed.activate(true)
	}
	if row == ed.cursor {
		return ed.activate(true)
	}
	ed.cursor = row
	ed.scroll()
	return actNone
}

// note is the muted line shown under a focused field: the annotation of
// the current value when there is one, the static description otherwise.
func (f *field) note() string {
	if f.annotate != nil {
		if s := f.annotate(f.text); s != "" {
			return s
		}
	}
	return f.desc
}

func checkbox(on bool) string {
	if on {
		return "[x]"
	}
	return "[ ]"
}

// helpers to build fields

func textField(key, label, value, desc, placeholder string, validate func(string) error) *field {
	return &field{key: key, kind: fText, label: label, text: value, desc: desc, placeholder: placeholder, validate: validate}
}

func listField(key, label string, value []string, desc string) *field {
	return &field{key: key, kind: fList, label: label, text: cli.JoinList(value), desc: desc, placeholder: "(none)"}
}

func intField(key, label string, value int64, desc string) *field {
	return &field{key: key, kind: fInt, label: label, text: limitText(value), desc: desc, placeholder: "-1"}
}

func bytesField(key, label string, value int64, desc string) *field {
	return &field{key: key, kind: fBytes, label: label, text: limitText(value), desc: desc, placeholder: "-1"}
}

func limitText(v int64) string {
	if v < 0 {
		return "-1"
	}
	return fmt.Sprint(v)
}

func boolField(key, label string, on bool, desc string) *field {
	return &field{key: key, kind: fBool, label: label, on: on, desc: desc}
}

func choiceField(key, label string, choices []string, current, desc string) *field {
	f := &field{key: key, kind: fChoice, label: label, choices: choices, desc: desc}
	for i, c := range choices {
		if strings.EqualFold(c, current) {
			f.choice = i
		}
	}
	return f
}

func infoField(label, value string) *field { return &field{kind: fInfo, label: label, text: value} }
func section(title string) *field          { return &field{kind: fSection, label: title} }
