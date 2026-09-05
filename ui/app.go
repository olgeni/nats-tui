package ui

import (
	"fmt"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nats.go/micro"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/olgeni/nats-tui/cli"
)

type screen int

const (
	scrMain screen = iota
	scrDetails
	scrEditor
	scrForm
	scrPicker
	scrPlan
	scrBusy
	scrResult
	scrHelp
	scrKeys
	scrTable
	scrText
	scrLive
)

type (
	loadedMsg struct {
		client *cli.Client
		store  *cli.Store
		err    error
	}
	ranMsg struct {
		plan    *cli.Plan
		results []cli.Result
		after   func(m *Model) tea.Cmd // what to do after a successful plan (default: reload)
	}
	textMsg struct {
		title string
		text  string
		help  string
		err   error
	}
	// lazyMsg brings the consumers of a stream, the objects of a store or
	// the statistics of a service instance, fetched on demand; then runs
	// once they are in the store.
	lazyMsg struct {
		stream string
		bucket string
		svc    string // instance id
		cons   []*cli.Consumer
		objs   []*jetstream.ObjectInfo
		stats  *micro.Stats
		err    error
		then   func(m *Model) tea.Cmd
	}
	refreshMsg struct{ gen int }
	tableMsg   struct {
		tbl *table
		err error
	}
)

// Model is the root bubbletea model.
type Model struct {
	settings cli.Settings
	runner   cli.Runner
	client   *cli.Client
	store    *cli.Store
	now      time.Time
	connErr  string // why there is no connection (the main screen says so)
	loading  bool   // a reload is under way

	refresh        time.Duration // re-read the server this often (0: off)
	refreshDefault time.Duration // what ctrl+r turns on (-refresh, else 5s)
	refreshGen     int           // ticks of an earlier setting are ignored

	width, height int
	mouse         bool // mouse reporting on (m toggles, -mouse starts with it)
	scr, prevScr  screen
	status        string
	errMsg        string
	quitting      bool
	fatal         string

	// main tree
	rows     []node
	cursor   int
	offset   int
	expanded map[string]bool
	filter   editLine
	filterOn bool
	tableTop int

	// generic screens
	vp       viewport.Model
	vpTitle  string
	vpHelp   string
	textBack screen

	editor  *editor
	onEdit  func(m *Model, ed *editor) tea.Cmd // OK handler: returns a plan-running cmd or nil
	editBak screen

	form     *form
	onForm   func(m *Model) tea.Cmd
	onAbort  func(m *Model)
	formVals struct {
		str string
		num int
		yes bool
	}

	pk      *picker
	pkDone  func(m *Model, value string) tea.Cmd
	pkAbort func(m *Model)

	plan       *cli.Plan
	planAfter  func(m *Model) tea.Cmd
	planBack   screen // the screen the plan was started from, returned to after the reload
	results    []cli.Result
	busyMsg    string
	busyDetail string

	tbl *table // sub-table screen (contexts, messages, keys, objects, …)
	// tref carries the cursor of a table being reloaded, so r and the
	// automatic reload come back where the user was instead of at the top.
	// A fetched table (keys, messages, objects…) is rebuilt from the server,
	// so nothing else would remember the position. quiet keeps the busy
	// screen out of the way when the reload was not asked for by hand.
	tref struct {
		on     bool
		quiet  bool
		cursor int
		offset int
	}

	lv     *live // live screen (subscribe, watch, events)
	lvBack screen

	mn *menu // the open Lotus-style menu above the tree, nil when none

	// details screen
	detail   node
	rawJSON  bool
	detailBk screen
}

// New returns the root model.
func New(s cli.Settings) *Model {
	m := &Model{settings: s, runner: cli.Exec{Settings: s}, width: 80, height: 24, expanded: map[string]bool{}, now: time.Now()}
	m.filter = newEditLine("")
	m.filter.Focus()
	// ←/→ scroll the text screens sideways: a report or a message body can
	// be wider than the terminal
	m.vp.SetHorizontalStep(horizontalStep)
	return m
}

// horizontalStep is how many columns ←/→ move a text screen.
const horizontalStep = 20

func (m *Model) Init() tea.Cmd { return tea.Batch(m.load(), m.scheduleRefresh()) }

// scheduleRefresh arms the next automatic reload.
func (m *Model) scheduleRefresh() tea.Cmd {
	if m.refresh <= 0 {
		return nil
	}
	gen := m.refreshGen
	return tea.Tick(m.refresh, func(time.Time) tea.Msg { return refreshMsg{gen: gen} })
}

// toggleRefresh turns the automatic reload on or off.
func (m *Model) toggleRefresh() tea.Cmd {
	m.refreshGen++
	if m.refresh > 0 {
		m.refresh = 0
		m.setStatus("Auto-refresh off")
		return nil
	}
	m.refresh = m.refreshDefault
	if m.refresh <= 0 {
		m.refresh = 5 * time.Second
	}
	m.setStatus("Auto-refresh every " + cli.HumanDuration(m.refresh) + " (ctrl+r turns it off)")
	return m.scheduleRefresh()
}

// ensureConsumers runs then once the consumers of a stream are known,
// fetching them first when they are not.
func (m *Model) ensureConsumers(st *cli.Stream, then func(m *Model) tea.Cmd) tea.Cmd {
	if st.Loaded || m.client == nil {
		if then != nil {
			return then(m)
		}
		return nil
	}
	c, name := m.client, st.Name()
	m.setStatus("Loading the consumers of " + name + "…")
	return func() tea.Msg {
		cons, err := c.Consumers(name)
		return lazyMsg{stream: name, cons: cons, err: err, then: then}
	}
}

// ensureObjects runs then once the objects of a store are listed.
func (m *Model) ensureObjects(b *cli.ObjectBucket, then func(m *Model) tea.Cmd) tea.Cmd {
	if b.Loaded || m.client == nil {
		if then != nil {
			return then(m)
		}
		return nil
	}
	c, name := m.client, b.Name()
	m.setStatus("Listing the objects of " + name + "…")
	return func() tea.Msg {
		objs, err := c.Objects(name)
		return lazyMsg{bucket: name, objs: objs, err: err, then: then}
	}
}

// serviceStats asks a service instance for its statistics, once per
// discovery: they land in the store, and on the details screen when it is
// open on the instance.
func (m *Model) serviceStats(s *cli.Service) tea.Cmd {
	if s.Stats != nil || s.StatsErr != nil || m.client == nil {
		return nil
	}
	c, name, id := m.client, s.Info.Name, s.Info.ID
	return func() tea.Msg {
		st, err := c.ServiceStats(name, id)
		return lazyMsg{svc: id, stats: st, err: err}
	}
}

// load connects (or reconnects when the settings changed) and reads the
// server.
func (m *Model) load() tea.Cmd {
	c, s := m.client, m.settings
	// what was fetched on demand stays fetched across the reload
	var opts cli.LoadOpts
	if m.store != nil {
		for _, st := range m.store.Streams {
			if st.Loaded {
				opts.Consumers = append(opts.Consumers, st.Name())
			}
		}
		for _, b := range m.store.Objects {
			if b.Loaded {
				opts.Objects = append(opts.Objects, b.Name())
			}
		}
	}
	m.loading = true
	return func() tea.Msg {
		if c == nil || c.Settings != s || c.NC.IsClosed() {
			if c != nil {
				c.Close()
			}
			var err error
			c, err = cli.Connect(s)
			if err != nil {
				return loadedMsg{err: err}
			}
		}
		st, err := c.Load(opts)
		return loadedMsg{client: c, store: st, err: err}
	}
}

func (m *Model) setStatus(s string) { m.status, m.errMsg = s, "" }
func (m *Model) setError(s string)  { m.errMsg, m.status = s, "" }

// label names the connection in title bars.
func (m *Model) label() string {
	if m.store == nil {
		if m.settings.Context != "" {
			return "context " + m.settings.Context
		}
		return "no connection"
	}
	if m.store.ContextName != "" {
		return "context " + m.store.ContextName
	}
	return m.store.Context.URL
}

// ---------------------------------------------------------------- plumbing

// openForm switches to a huh form; done runs on completion, abort on esc.
func (m *Model) openForm(f *form, done func(m *Model) tea.Cmd, abort func(m *Model)) tea.Cmd {
	m.form = f
	m.form.resize(min(m.width, 110))
	m.onForm, m.onAbort = done, abort
	m.prevScr, m.scr = m.scr, scrForm
	return m.form.Init()
}

// openPicker shows a filterable list; done gets the chosen value.
func (m *Model) openPicker(title, desc string, items []pickItem, initial string, done func(m *Model, value string) tea.Cmd, abort func(m *Model)) tea.Cmd {
	return m.openPickerHelp(title, desc, "", items, initial, done, abort)
}

// openPickerHelp is openPicker with its own footer.
func (m *Model) openPickerHelp(title, desc, help string, items []pickItem, initial string, done func(m *Model, value string) tea.Cmd, abort func(m *Model)) tea.Cmd {
	m.pk = newPicker(title, desc, items, initial, m.width, m.height)
	m.pk.help = help
	m.pkDone, m.pkAbort = done, abort
	m.prevScr, m.scr = m.scr, scrPicker
	return nil
}

// openEditor shows an editor; onOK gets the edited fields.
func (m *Model) openEditor(ed *editor, onOK func(m *Model, ed *editor) tea.Cmd) tea.Cmd {
	ed.setSize(m.width, m.height)
	m.editor, m.onEdit = ed, onOK
	m.editBak = m.scr
	m.scr = scrEditor
	return nil
}

// expandTabs turns tabs into spaces before a text reaches the viewport:
// the viewport cuts long lines at the width counting a tab as no cell,
// lipgloss then renders it as four spaces, and the line that is now too
// wide wraps onto a second row that pushes the last lines of the text
// past the bottom, where they are dropped. A generated server
// configuration indents its JWT lines with tabs.
func expandTabs(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		switch r {
		case '\t':
			n := 8 - col%8
			b.WriteString(strings.Repeat(" ", n))
			col += n
		case '\n':
			b.WriteRune(r)
			col = 0
		default:
			b.WriteRune(r)
			col++
		}
	}
	return b.String()
}

// showText shows a scrollable text screen; esc returns to where it was
// opened from.
func (m *Model) showText(title, text, help string) {
	m.vpTitle, m.vpHelp = title, help
	if help == "" {
		m.vpHelp = helpLine("esc/q", "back", "↑↓ ←→", "scroll")
	}
	m.vp.SetContent(expandTabs(text))
	m.vp.GotoTop()
	m.vp.SetXOffset(0)
	if m.scr != scrText {
		m.textBack = m.scr
	}
	m.scr = scrText
}

// runPlan previews a plan; enter executes it. after runs on success (nil:
// reload and go back to where the plan started from — a sub-table stays
// open, so adding a key or a consumer lands back in its list).
func (m *Model) runPlan(p *cli.Plan, after func(m *Model) tea.Cmd) tea.Cmd {
	// every modal restores the screen underneath before its handler runs,
	// so this is the base screen the action was started from
	m.planBack = scrMain
	if (m.scr == scrTable && m.tbl != nil) || m.scr == scrDetails || (m.scr == scrLive && m.lv != nil) {
		m.planBack = m.scr
	}
	if p.Empty() {
		m.scr = m.planBack
		if len(p.Notes) > 0 {
			m.setStatus(strings.Join(p.Notes, "; "))
		} else {
			m.setStatus("Nothing to change")
		}
		return nil
	}
	m.plan, m.planAfter = p, after
	m.vp.SetContent(m.planText())
	m.vp.GotoTop()
	m.vp.SetXOffset(0)
	m.scr = scrPlan
	return nil
}

// busy switches to the busy screen and runs work.
func (m *Model) busy(msg string, work func() tea.Msg) tea.Cmd {
	if m.tref.quiet {
		return work // an automatic table refresh must not flash the busy screen
	}
	m.scr = scrBusy
	m.busyMsg, m.busyDetail = msg, ""
	return work
}

func (m *Model) execute() tea.Cmd {
	plan, after, r := m.plan, m.planAfter, m.runner
	n := len(plan.Cmds)
	return m.busy(fmt.Sprintf("Running %d nats command(s)…", n), func() tea.Msg {
		return ranMsg{plan, plan.Execute(r, nil), after}
	})
}

// ---------------------------------------------------------------- update

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// every viewport screen is a title bar, the body and a help line
		m.vp.Width, m.vp.Height = m.width, max(3, m.height-2)
		if m.editor != nil {
			m.editor.setSize(m.width, m.height)
		}
		if m.pk != nil {
			m.pk.setSize(m.width, m.height)
		}
		if m.form != nil {
			m.form.resize(min(m.width, 110))
		}
		if m.tbl != nil {
			m.tbl.setSize(m.width, m.height)
		}
		if m.lv != nil {
			m.lv.setSize(m.width, m.height)
		}
		m.clampCursor()
		return m, nil
	case refreshMsg:
		if msg.gen != m.refreshGen || m.refresh <= 0 {
			return m, nil
		}
		next := m.scheduleRefresh()
		if m.loading || m.client == nil {
			return m, next
		}
		switch m.scr {
		case scrMain, scrDetails, scrTable:
			return m, tea.Batch(next, m.load())
		}
		return m, next
	case lazyMsg:
		if m.store != nil {
			if st := m.store.Stream(msg.stream); msg.stream != "" && st != nil {
				for _, c := range msg.cons {
					c.Stream = st
				}
				st.Consumers, st.ConsumersErr, st.Loaded = msg.cons, msg.err, true
			}
			if b := m.store.Object(msg.bucket); msg.bucket != "" && b != nil {
				b.Objects, b.ListErr, b.Loaded = msg.objs, msg.err, true
			}
			for _, s := range m.store.Services {
				if msg.svc != "" && s.Info.ID == msg.svc {
					// the details screen shows the error in its place
					s.Stats, s.StatsErr = msg.stats, msg.err
					msg.err = nil
				}
			}
			m.rebuildRows()
			if m.scr == scrDetails {
				m.refreshDetail()
			}
		}
		if strings.HasPrefix(m.status, "Loading the consumers") || strings.HasPrefix(m.status, "Listing the objects") {
			m.status = ""
		}
		if msg.err != nil {
			m.setError(msg.err.Error())
		}
		if msg.then != nil {
			return m, msg.then(m)
		}
		return m, nil
	case loadedMsg:
		m.now = time.Now()
		m.loading = false
		if msg.client != nil {
			m.client = msg.client
		}
		if msg.err != nil && msg.store == nil {
			// no connection at all: the main screen explains and offers
			// the contexts; nothing is fatal, the server may come back
			m.store = nil
			m.connErr = msg.err.Error()
			m.rebuildRows()
			m.scr = scrMain
			if m.planBack == scrTable && m.tbl != nil && m.tbl.kind == tkContexts {
				m.scr = scrTable
				m.refreshTable(false)
			}
			m.planBack = scrMain
			return m, nil
		}
		m.store = msg.store
		m.connErr = ""
		m.rebuildRows()
		if m.scr == scrBusy || m.scr == scrResult {
			m.scr = scrMain
			if (m.planBack == scrTable && m.tbl != nil) || m.planBack == scrDetails || (m.planBack == scrLive && m.lv != nil) {
				m.scr = m.planBack
			}
			m.planBack = scrMain
		}
		if msg.err != nil {
			m.setError(msg.err.Error())
		}
		for _, w := range m.store.Warnings {
			m.setError(w)
		}
		if m.scr == scrDetails {
			m.refreshDetail()
		}
		if m.tbl != nil && m.scr == scrTable {
			return m, m.refreshTable(true)
		}
		return m, nil
	case tableMsg:
		tr := m.tref
		m.tref.on, m.tref.quiet = false, false
		if msg.err != nil {
			if tr.quiet {
				m.setError(msg.err.Error()) // stay where we are; the next tick tries again
				return m, nil
			}
			m.scr = m.planBack
			if m.scr == scrBusy {
				m.scr = scrMain
			}
			m.setError(msg.err.Error())
			return m, nil
		}
		if tr.on {
			msg.tbl.setSize(m.width, m.height)
			msg.tbl.cursor, msg.tbl.offset = tr.cursor, tr.offset
			msg.tbl.clamp()
			m.tbl = msg.tbl
			m.scr = scrTable
			return m, nil
		}
		m.scr = scrMain
		return m, m.openTable(msg.tbl)
	case ranMsg:
		m.results = msg.results
		m.vp.SetContent(expandTabs(m.resultText()))
		m.vp.GotoTop()
		m.vp.SetXOffset(0)
		m.scr = scrResult
		if !cli.Failed(msg.results) && msg.after != nil {
			m.planAfter = msg.after
		} else {
			m.planAfter = nil
		}
		return m, nil
	case textMsg:
		if msg.err != nil {
			m.scr = scrMain
			if m.textBack == scrTable && m.tbl != nil {
				m.scr = scrTable
			}
			m.setError(msg.err.Error())
			return m, nil
		}
		m.scr = m.textBack
		m.showText(msg.title, msg.text, msg.help)
		return m, nil
	case liveMsg:
		return m, m.updateLiveEvents(msg)
	case liveOpenMsg:
		if msg.err != nil {
			m.scr = m.lvBack
			m.setError(msg.err.Error())
			return m, nil
		}
		m.lv = msg.lv
		m.lv.setSize(m.width, m.height)
		m.scr = scrLive
		return m, m.lv.wait()
	case tea.ResumeMsg:
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			m.stopLive()
			return m, tea.Quit
		case "ctrl+z":
			return m, tea.Suspend
		}
	}

	switch m.scr {
	case scrMain:
		return m.updateMain(msg)
	case scrEditor:
		return m.updateEditor(msg)
	case scrForm:
		return m.updateForm(msg)
	case scrPicker:
		var act pickAction
		if mm, ok := msg.(tea.MouseMsg); ok {
			act = m.pk.Mouse(mm)
		} else {
			act = m.pk.Update(msg)
		}
		switch act {
		case pickDone:
			m.scr = m.prevScr
			return m, m.pkDone(m, m.pk.Value())
		case pickCancel:
			m.scr = m.prevScr
			if m.pkAbort != nil {
				m.pkAbort(m)
			}
		}
		return m, nil
	case scrPlan:
		if k, ok := msg.(tea.KeyMsg); ok {
			switch k.String() {
			case "esc", "n", "q":
				m.scr = m.planBack
				if m.scr == scrTable && m.tbl == nil {
					m.scr = scrMain
				}
				return m, nil
			case "enter", "y":
				return m, m.execute()
			}
		}
	case scrResult:
		if k, ok := msg.(tea.KeyMsg); ok {
			switch k.String() {
			case "enter", "esc", "q", " ":
				after := m.planAfter
				m.planAfter = nil
				if after != nil {
					return m, tea.Batch(m.busyReload(), after(m))
				}
				return m, m.busyReload()
			}
		}
	case scrHelp, scrKeys:
		if k, ok := msg.(tea.KeyMsg); ok {
			switch k.String() {
			case "esc", "q", "?", "h", "enter":
				m.scr = m.prevScr
				return m, nil
			}
		}
	case scrText:
		if k, ok := msg.(tea.KeyMsg); ok {
			switch k.String() {
			case "esc", "q", "enter":
				m.scr = m.textBack
				return m, nil
			}
		}
	case scrDetails:
		return m.updateDetails(msg)
	case scrTable:
		return m.updateTable(msg)
	case scrLive:
		return m.updateLive(msg)
	case scrBusy:
		return m, nil
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

// busyReload re-reads the server behind the busy screen.
func (m *Model) busyReload() tea.Cmd {
	m.scr = scrBusy
	m.busyMsg = "Reloading…"
	return m.load()
}

func (m *Model) updateEditor(msg tea.Msg) (tea.Model, tea.Cmd) {
	var act editorAction
	if mm, ok := msg.(tea.MouseMsg); ok {
		act = m.editor.Mouse(mm)
	} else {
		act = m.editor.Update(msg)
	}
	switch act {
	case actCancel:
		m.scr = m.editBak
		m.setStatus("Cancelled")
	case actPick:
		f := m.editor.row()
		if f.pick != nil {
			return m, f.pick(m, f)
		}
	case actOK:
		ed, done := m.editor, m.onEdit
		m.scr = m.editBak
		if done != nil {
			m.errMsg = ""
			cmd := done(m, ed)
			if m.errMsg != "" && m.scr == m.editBak {
				// the handler refused the input: stay in the editor with
				// what was typed, the error shown underneath
				ed.errMsg = m.errMsg
				m.scr = scrEditor
			}
			return m, cmd
		}
	}
	return m, nil
}

// removeField drops a value from a comma-separated field.
func removeField(f *field, value string) {
	var out []string
	for _, x := range cli.SplitList(f.text) {
		if x != value {
			out = append(out, x)
		}
	}
	f.text = strings.Join(out, ", ")
}

// appendField adds a value to a comma-separated field, without duplicates.
func appendField(f *field, value string) {
	for _, x := range cli.SplitList(f.text) {
		if x == value {
			return
		}
	}
	if strings.TrimSpace(f.text) == "" {
		f.text = value
		return
	}
	f.text += ", " + value
}

func (m *Model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	fm, cmd := m.form.Update(msg)
	if f, ok := fm.(*huh.Form); ok {
		m.form.Form = f
	}
	switch m.form.State {
	case huh.StateCompleted:
		done := m.onForm
		m.form, m.onForm = nil, nil
		m.scr = m.prevScr
		if done != nil {
			return m, tea.Batch(cmd, done(m))
		}
	case huh.StateAborted:
		abort := m.onAbort
		m.form, m.onForm = nil, nil
		m.scr = m.prevScr
		if abort != nil {
			abort(m)
		} else if m.scr == scrForm {
			m.scr = scrMain
		}
	}
	return m, cmd
}

// ---------------------------------------------------------------- plan / result

func (m *Model) planText() string {
	p := m.plan
	var b strings.Builder
	b.WriteString(styleHeader.Render(fit(p.Title, m.width-1)) + "\n")
	if p.Dangerous() {
		b.WriteString(styleWarn.Render("⚠ This deletes data or cannot be undone. Check the commands.") + "\n")
	}
	b.WriteString("\n")
	for i, c := range p.Cmds {
		desc := styleMuted.Render(c.Desc)
		if c.Danger {
			desc = styleWarn.Render(c.Desc)
		}
		fmt.Fprintf(&b, "%s %s\n", styleFocus.Render(fmt.Sprintf("[%d/%d]", i+1, len(p.Cmds))), desc)
		b.WriteString("      " + wrapCommand(m.commandLine(c), m.width-8, "      ") + "\n")
		if c.Stdin != "" {
			b.WriteString("      " + styleMuted.Render("stdin:") + "\n")
			for _, line := range strings.Split(strings.TrimRight(c.Stdin, "\n"), "\n") {
				b.WriteString("        " + fit(line, max(1, m.width-10)) + "\n")
			}
		}
		b.WriteString("\n")
	}
	for _, n := range p.Notes {
		b.WriteString(styleWarn.Render(fit("Note: "+n, m.width-1)) + "\n")
	}
	b.WriteString("\n" + styleMuted.Render("The server is re-read after the commands ran. Commands stop at the first failure.") + "\n")
	return b.String()
}

// commandLine is what the preview shows for a step: the nats invocation.
func (m *Model) commandLine(c cli.Command) string {
	return m.settings.CommandLine(c.Args)
}

// wrapCommand breaks a long shell line at argument boundaries with
// backslash continuations so it stays pasteable.
func wrapCommand(line string, width int, indent string) string {
	if lipgloss.Width(line) <= width || width < 20 {
		return line
	}
	var b strings.Builder
	cur := 0
	for _, word := range strings.Split(line, " ") {
		if cur > 0 && cur+1+len(word) > width-2 {
			b.WriteString(" \\\n" + indent + "  ")
			cur = 2
		} else if cur > 0 {
			b.WriteString(" ")
			cur++
		}
		b.WriteString(word)
		cur += len(word)
	}
	return b.String()
}

func (m *Model) resultText() string {
	var b strings.Builder
	failed := cli.Failed(m.results)
	if failed {
		b.WriteString(styleErr.Render(fmt.Sprintf("✗ Command %d of %d failed; the rest was not run.", len(m.results), len(m.plan.Cmds))) + "\n\n")
	} else {
		b.WriteString(styleOK.Render(fmt.Sprintf("✓ %d command(s) ran.", len(m.results))) + "\n\n")
	}
	for i, r := range m.results {
		mark := styleOK.Render("✓")
		if !r.OK() {
			mark = styleErr.Render("✗")
		}
		line := m.settings.CommandLine(r.Args)
		fmt.Fprintf(&b, "%s %s\n", mark, styleFocus.Render(wrapCommand(line, m.width-4, "  ")))
		out := r.Output()
		if out == "" {
			out = styleMuted.Render("(no output)")
		}
		for _, line := range strings.Split(out, "\n") {
			b.WriteString("    " + line + "\n")
		}
		if i < len(m.results)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// ---------------------------------------------------------------- views

func (m *Model) View() string {
	if m.quitting {
		if m.fatal != "" {
			return styleErr.Render("nats-tui: "+m.fatal) + "\n"
		}
		return ""
	}
	switch m.scr {
	case scrEditor:
		return m.editor.View()
	case scrForm:
		return clamp(m.width, titleBar(m.width, "nats-tui — "+fitLeft(m.label(), m.width-12))+"\n\n"+m.form.View())
	case scrPicker:
		return m.pk.View()
	case scrPlan:
		help := helpLine("enter/y", "run", "esc/n", "back", "↑↓ ←→", "scroll")
		return m.frame("Preview — "+m.plan.Title, m.vp.View(), help)
	case scrResult:
		return m.frame("Result — "+m.plan.Title, m.vp.View(), helpLine("enter/esc", "back (re-reads the server)", "↑↓ ←→", "scroll"))
	case scrHelp:
		return m.frame("Help", m.vp.View(), helpLine("esc/q", "back", "↑↓ ←→", "scroll"))
	case scrKeys:
		return m.frame("Keys", m.vp.View(), helpLine("esc/h", "back", "?", "full help"))
	case scrText:
		return m.frame(m.vpTitle, m.vp.View(), m.vpHelp)
	case scrDetails:
		return m.detailsView()
	case scrTable:
		return m.tbl.View()
	case scrLive:
		return m.lv.View()
	case scrBusy:
		body := "\n  " + m.busyMsg + "\n"
		if m.busyDetail != "" {
			body += "  " + styleMuted.Render(fit(m.busyDetail, m.width-4)) + "\n"
		}
		return m.frame("Working", body, "")
	}
	return m.mainView()
}

func (m *Model) frame(title, body, help string) string {
	head := "nats-tui — " + title
	// the context is context, so it is dropped before the title is
	if avail := m.width - lipgloss.Width(head) - 3; avail >= 12 {
		head += " — " + fitLeft(m.label(), avail)
	}
	return clamp(m.width, titleBar(m.width, head)+"\n"+body+"\n"+help)
}

// Run starts the TUI; mouse enables mouse reporting from the start (m
// toggles it at runtime).
func Run(s cli.Settings, mouse bool, refresh time.Duration) error {
	m := New(s)
	m.refresh, m.refreshDefault = refresh, refresh
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if mouse {
		m.mouse = true
		opts = append(opts, tea.WithMouseCellMotion())
	}
	p := tea.NewProgram(m, opts...)
	final, err := p.Run()
	if m.client != nil {
		m.client.Close()
	}
	if err != nil {
		return err
	}
	if fm, ok := final.(*Model); ok && fm.fatal != "" {
		fmt.Fprintln(os.Stderr, "nats-tui:", fm.fatal)
		os.Exit(1)
	}
	return nil
}
