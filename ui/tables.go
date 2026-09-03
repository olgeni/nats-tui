package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/olgeni/nats-tui/cli"
)

// The sub-tables: one screen type for the contexts, the messages and
// subjects of a stream, the keys of a bucket and the objects of a store,
// each with its own columns, rows and key actions.

type tableKind int

const (
	tkContexts tableKind = iota
	tkMessages
	tkSubjects
	tkKeys
	tkHistory
	tkObjects
)

type tcol struct {
	name string
	w    int // 0: takes the remaining width
}

type trow struct {
	cells []string
	style *lipgloss.Style
	ref   any
}

type table struct {
	kind   tableKind
	title  string
	desc   string
	cols   []tcol
	rows   []trow
	cursor int
	offset int
	width  int
	height int
	help   string
	empty  string
	// what the table is about
	stream string // messages, subjects
	bucket string // keys, history, objects
	key    string // history
	filter string // messages, subjects
	seq    uint64 // messages: first sequence of the page
	last   bool   // messages: the page holds the newest message
}

func (t *table) setSize(w, h int) { t.width, t.height = w, h; t.clamp() }
func (t *table) listHeight() int  { return max(3, t.height-6) }

func (t *table) clamp() {
	if t.cursor >= len(t.rows) {
		t.cursor = len(t.rows) - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
	h := t.listHeight()
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+h {
		t.offset = t.cursor - h + 1
	}
	if t.offset < 0 {
		t.offset = 0
	}
}

func (t *table) selected() *trow {
	if t.cursor < len(t.rows) {
		return &t.rows[t.cursor]
	}
	return nil
}

func (t *table) View() string {
	var b strings.Builder
	b.WriteString(titleBar(t.width, t.title) + "\n")
	b.WriteString(" " + styleMuted.Render(fit(t.desc, t.width-2)) + "\n")
	// column widths
	fixed := 0
	flex := 0
	for _, c := range t.cols {
		if c.w > 0 {
			fixed += c.w + 1
		} else {
			flex++
		}
	}
	rest := t.width - 1 - fixed
	fw := 10
	if flex > 0 {
		fw = max(8, rest/flex-1)
	}
	widths := make([]int, len(t.cols))
	for i, c := range t.cols {
		widths[i] = c.w
		if c.w == 0 {
			widths[i] = fw
		}
	}
	hdr := " "
	for i, c := range t.cols {
		hdr += fit(c.name, widths[i]) + " "
	}
	b.WriteString(styleHeader.Render(fit(hdr, t.width)) + "\n")
	h := t.listHeight()
	for i := t.offset; i < len(t.rows) && i < t.offset+h; i++ {
		r := t.rows[i]
		line := " "
		for j := range t.cols {
			cell := ""
			if j < len(r.cells) {
				cell = r.cells[j]
			}
			if strings.HasPrefix(cell, "/") || strings.HasPrefix(cell, "~") {
				line += fit(fitLeft(cell, widths[j]), widths[j]) + " " // paths: keep the tail
			} else {
				line += fit(cell, widths[j]) + " "
			}
		}
		line = fit(line, t.width)
		switch {
		case i == t.cursor:
			line = styleSelected.Render(line)
		case r.style != nil:
			line = r.style.Render(line)
		}
		b.WriteString(line + "\n")
	}
	if len(t.rows) == 0 {
		b.WriteString(styleMuted.Render("   "+t.empty) + "\n")
	}
	for i := max(len(t.rows)-t.offset, 1); i < h; i++ {
		b.WriteString("\n")
	}
	if len(t.rows) > h {
		b.WriteString(styleMuted.Render(fmt.Sprintf(" (%d–%d of %d)", t.offset+1, min(t.offset+h, len(t.rows)), len(t.rows))))
	}
	b.WriteString("\n" + t.help)
	return lipgloss.NewStyle().MaxWidth(t.width).Render(b.String())
}

func (m *Model) openTable(t *table) tea.Cmd {
	t.setSize(m.width, m.height)
	if m.tbl != nil && m.tbl.kind == t.kind && m.scr == scrTable {
		t.cursor, t.offset = m.tbl.cursor, m.tbl.offset
		t.clamp()
	}
	m.tbl = t
	m.scr = scrTable
	return nil
}

// refreshTable rebuilds the current table after a reload; tables that
// read the server do so behind the busy screen.
func (m *Model) refreshTable() tea.Cmd {
	t := m.tbl
	if t == nil {
		return nil
	}
	cur, off := t.cursor, t.offset
	keep := func(nt *table) tea.Cmd {
		nt.setSize(m.width, m.height)
		nt.cursor, nt.offset = cur, off
		nt.clamp()
		m.tbl = nt
		return nil
	}
	switch t.kind {
	case tkContexts:
		return keep(m.contextsTable())
	case tkMessages:
		if m.store == nil || m.store.Stream(t.stream) == nil {
			m.scr = scrMain
			return nil
		}
		return m.loadMessages(t.stream, t.seq, t.filter, t.last)
	case tkSubjects:
		if m.store == nil || m.store.Stream(t.stream) == nil {
			m.scr = scrMain
			return nil
		}
		return m.loadSubjects(t.stream, t.filter)
	case tkKeys:
		if m.store == nil || m.store.KV(t.bucket) == nil {
			m.scr = scrMain
			return nil
		}
		return m.loadKeys(t.bucket)
	case tkHistory:
		if m.store == nil || m.store.KV(t.bucket) == nil {
			m.scr = scrMain
			return nil
		}
		return m.loadHistory(t.bucket, t.key)
	case tkObjects:
		b := m.store.Object(t.bucket)
		if b == nil {
			m.scr = scrMain
			return nil
		}
		return keep(m.objectsTable(b))
	}
	return nil
}

// tableTop is the screen line of the first row (title, description, header).
const tableTop = 3

func (m *Model) updateTable(msg tea.Msg) (tea.Model, tea.Cmd) {
	t := m.tbl
	if mm, ok := msg.(tea.MouseMsg); ok {
		switch {
		case mm.Button == tea.MouseButtonWheelUp:
			t.cursor -= 3
		case mm.Button == tea.MouseButtonWheelDown:
			t.cursor += 3
		case mm.Button == tea.MouseButtonLeft && mm.Action == tea.MouseActionPress:
			idx := t.offset + mm.Y - tableTop
			if mm.Y < tableTop || idx < 0 || idx >= len(t.rows) || idx >= t.offset+t.listHeight() {
				return m, nil
			}
			if idx == t.cursor {
				return m.updateTable(tea.KeyMsg{Type: tea.KeyEnter})
			}
			t.cursor = idx
		}
		t.clamp()
		return m, nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch k.String() {
	case "esc", "q":
		m.scr = scrMain
		if t.kind == tkHistory {
			return m, m.showKeys(m.store.KV(t.bucket))
		}
		return m, nil
	case "up", "k":
		t.cursor--
	case "down", "j":
		t.cursor++
	case "pgup":
		t.cursor -= t.listHeight()
	case "pgdown":
		t.cursor += t.listHeight()
	case "home", "g":
		t.cursor = 0
	case "end", "G":
		t.cursor = len(t.rows) - 1
	case "?", "f1":
		m.vp.SetContent(helpText)
		m.vp.GotoTop()
		m.prevScr, m.scr = scrTable, scrHelp
	case "r":
		return m, m.refreshTable()
	case "m":
		return m, m.toggleMouse()
	default:
		switch t.kind {
		case tkContexts:
			return m, m.contextKeys(k.String())
		case tkMessages:
			return m, m.messageKeys(k.String())
		case tkSubjects:
			return m, m.subjectKeys(k.String())
		case tkKeys:
			return m, m.kvKeys(k.String())
		case tkHistory:
			return m, m.historyKeys(k.String())
		case tkObjects:
			return m, m.objectKeys(k.String())
		}
	}
	t.clamp()
	return m, nil
}

// ---------------------------------------------------------------- contexts

func (m *Model) showContexts() tea.Cmd { return m.openTable(m.contextsTable()) }

// contextRow is what a row of the contexts table refers to.
type contextRow struct {
	name string
	info cli.ContextInfo
	err  error
}

func (m *Model) contextsTable() *table {
	dir := cli.ConfigDir()
	t := &table{kind: tkContexts, title: "Contexts in " + dir,
		desc:  "The connection settings nats keeps under " + dir + "; the selected one is what nats and nats-tui use without --context.",
		cols:  []tcol{{"Name", 22}, {"", 3}, {"Server", 0}, {"Credentials", 0}, {"JetStream", 14}, {"Description", 0}},
		help:  helpLine("enter/u", "use", "S", "select as default", "a", "add", "e", "edit", "y", "copy", "d", "delete", "V", "validate", "esc", "back"),
		empty: "no contexts (a adds one; without any, nats connects to localhost:4222)"}
	names := cli.SortedContextNames(knownContexts())
	selected := selectedContext()
	using := m.settings.Context
	if using == "" && m.store != nil {
		using = m.store.ContextName
	}
	for _, name := range names {
		info, err := cli.ReadContext(name)
		mark := ""
		if name == selected {
			mark += "*"
		}
		if name == using {
			mark += "●"
		}
		auth := ""
		switch {
		case err != nil:
			auth = "⚠ " + err.Error()
		case info.Creds != "":
			auth = info.Creds
		case info.User != "":
			auth = "user " + info.User
		case info.NKey != "":
			auth = "nkey " + info.NKey
		case info.Token:
			auth = "token"
		case info.NscURL != "":
			auth = info.NscURL
		}
		js := info.JSDomain
		if info.JSAPIPrefix != "" {
			js = "api " + info.JSAPIPrefix
		}
		var st *lipgloss.Style
		if err != nil {
			st = &styleWarn
		}
		t.rows = append(t.rows, trow{cells: []string{name, mark, info.URL, auth, js, info.Description}, style: st, ref: contextRow{name, info, err}})
	}
	return t
}

func knownContexts() []string {
	entries, err := os.ReadDir(filepath.Join(cli.ConfigDir(), "context"))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if n := e.Name(); strings.HasSuffix(n, ".json") && !e.IsDir() {
			out = append(out, strings.TrimSuffix(n, ".json"))
		}
	}
	return out
}

func selectedContext() string {
	b, err := os.ReadFile(filepath.Join(cli.ConfigDir(), "context.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func (m *Model) contextKeys(key string) tea.Cmd {
	t := m.tbl
	var sel *contextRow
	if r := t.selected(); r != nil {
		x := r.ref.(contextRow)
		sel = &x
	}
	switch key {
	case "enter", "u":
		if sel == nil {
			return nil
		}
		return m.useContext(sel.name)
	case "S":
		if sel == nil {
			return nil
		}
		return m.runPlan(cli.SelectContext(sel.name), nil)
	case "a":
		return m.editContext(nil)
	case "e":
		if sel == nil {
			return nil
		}
		if sel.err != nil {
			m.setError("The context does not load: " + sel.err.Error())
			return nil
		}
		return m.editContext(sel)
	case "y":
		if sel == nil {
			return nil
		}
		m.formVals.str = sel.name + "-copy"
		return m.openForm(inputForm("Copy context "+sel.name+" to", "The name of the new context.", "", &m.formVals.str, validName), func(m *Model) tea.Cmd {
			return m.runPlan(cli.CopyContext(sel.name, strings.TrimSpace(m.formVals.str)), nil)
		}, nil)
	case "d", "delete", "x":
		if sel == nil {
			return nil
		}
		m.formVals.yes = false
		return m.openForm(confirmForm("Delete context "+sel.name+"?", "The file "+sel.info.Path+" is removed. nats refuses to delete the selected context while others exist.", &m.formVals.yes), func(m *Model) tea.Cmd {
			if !m.formVals.yes {
				return nil
			}
			return m.runPlan(cli.DeleteContext(sel.name), nil)
		}, nil)
	case "V":
		if sel == nil {
			return m.runText("Validate every context", "context", "validate", "--connect")
		}
		return m.runText("Validate context "+sel.name, "context", "validate", sel.name, "--connect")
	}
	return nil
}

// useContext connects with another context without making it nats's
// default: every command then carries --context.
func (m *Model) useContext(name string) tea.Cmd {
	m.settings.Context = name
	m.runner = cli.Exec{Settings: m.settings}
	m.tbl = nil
	m.expanded = map[string]bool{}
	m.cursor, m.offset = 0, 0
	m.setStatus("Using context " + name + " (commands carry --context " + name + "; S makes it the default)")
	return m.busyReload()
}

// switchContext picks a context to use.
func (m *Model) switchContext() tea.Cmd {
	names := cli.SortedContextNames(knownContexts())
	selected := selectedContext()
	using := m.settings.Context
	if using == "" && m.store != nil {
		using = m.store.ContextName
	}
	var items []pickItem
	for _, n := range names {
		label := fmt.Sprintf("%-24s", n)
		if info, err := cli.ReadContext(n); err == nil {
			label += " " + info.URL
		}
		if n == selected {
			label += "   (default)"
		}
		if n == using {
			label += "   (in use)"
		}
		items = append(items, pickItem{label, n})
	}
	items = append(items, pickItem{"» add a context…", addValue})
	return m.openPicker("Use which context?", "The connection and every nats command switch to it; S in the contexts table makes one the default.", items, using, func(m *Model, v string) tea.Cmd {
		if v == addValue {
			return m.editContext(nil)
		}
		if v == using {
			return nil
		}
		return m.useContext(v)
	}, nil)
}

// ---------------------------------------------------------------- messages

const messagePage = 100

// showMessages opens the messages of a stream: the newest page, or the
// page starting at seq.
func (m *Model) showMessages(st *cli.Stream, seq uint64, filter string) tea.Cmd {
	return m.loadMessages(st.Name(), seq, filter, seq == 0)
}

// loadMessages reads a page of messages behind the busy screen; last asks
// for the page that ends at the newest message.
func (m *Model) loadMessages(stream string, seq uint64, filter string, last bool) tea.Cmd {
	c := m.client
	w, h := m.width, m.height
	st := m.store.Stream(stream)
	if st == nil {
		return nil
	}
	first, lastSeq := st.Info.State.FirstSeq, st.Info.State.LastSeq
	if last || seq == 0 {
		seq = 1
		if lastSeq >= messagePage {
			seq = lastSeq - messagePage + 1
		}
		if seq < first {
			seq = first
		}
	}
	if m.scr != scrBusy {
		m.planBack = m.scr
	}
	return m.busy("Reading messages of "+stream+"…", func() tea.Msg {
		msgs, err := c.Messages(stream, seq, messagePage, filter)
		if err != nil {
			return tableMsg{err: fmt.Errorf("read messages: %w", err)}
		}
		t := messagesTable(stream, seq, filter, msgs, first, lastSeq)
		t.setSize(w, h)
		t.last = last || len(msgs) > 0 && msgs[len(msgs)-1].Seq >= lastSeq
		if t.last {
			t.cursor = len(t.rows) - 1
			t.clamp()
		}
		return tableMsg{tbl: t}
	})
}

func messagesTable(stream string, seq uint64, filter string, msgs []cli.Message, first, last uint64) *table {
	desc := fmt.Sprintf("Sequences %d to %d are stored; a page holds %d messages.", first, last, messagePage)
	if filter != "" {
		desc += " Only subject " + filter + "."
	}
	t := &table{kind: tkMessages, stream: stream, seq: seq, filter: filter, title: "Messages of stream " + stream, desc: desc,
		cols:  []tcol{{"Seq", 9}, {"Time", 19}, {"Subject", 0}, {"Size", 9}, {"Hdr", 4}, {"Body", 0}},
		help:  helpLine("enter", "message", "[ ]", "older / newer page", "g", "go to seq", "f", "filter subject", "d", "delete", "s", "subscribe", "esc", "back"),
		empty: "no messages here (the stream may be empty, or purged past this point)"}
	for _, msg := range msgs {
		hdr := ""
		if len(msg.Header) > 0 {
			hdr = fmt.Sprint(len(msg.Header))
		}
		t.rows = append(t.rows, trow{cells: []string{fmt.Sprint(msg.Seq), cli.Date(msg.Time), msg.Subject, cli.Size(uint64(len(msg.Data))), hdr, preview(msg.Data)}, ref: msg})
	}
	return t
}

func (m *Model) messageKeys(key string) tea.Cmd {
	t := m.tbl
	var msg *cli.Message
	if r := t.selected(); r != nil {
		x := r.ref.(cli.Message)
		msg = &x
	}
	st := m.store.Stream(t.stream)
	switch key {
	case "enter":
		if msg != nil {
			m.showText(fmt.Sprintf("Message %d of %s", msg.Seq, t.stream), messageText(*msg, m.width), helpLine("esc", "back", "↑↓", "scroll"))
		}
	case "[", "pgup":
		if key == "pgup" && t.cursor > 0 {
			t.cursor -= t.listHeight()
			t.clamp()
			return nil
		}
		if t.seq <= 1 {
			return nil
		}
		seq := uint64(1)
		if t.seq > messagePage {
			seq = t.seq - messagePage
		}
		return m.loadMessages(t.stream, seq, t.filter, false)
	case "]", "pgdown":
		if key == "pgdown" && t.cursor < len(t.rows)-1 {
			t.cursor += t.listHeight()
			t.clamp()
			return nil
		}
		if t.last || len(t.rows) == 0 {
			return nil
		}
		lastRow := t.rows[len(t.rows)-1].ref.(cli.Message)
		return m.loadMessages(t.stream, lastRow.Seq+1, t.filter, false)
	case "end", "G":
		if t.last {
			t.cursor = len(t.rows) - 1
			t.clamp()
			return nil
		}
		return m.loadMessages(t.stream, 0, t.filter, true)
	case "g":
		m.formVals.str = ""
		return m.openForm(inputForm("Go to sequence", "The page starts at this message sequence.", fmt.Sprint(t.seq), &m.formVals.str, func(s string) error {
			if _, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64); err != nil {
				return fmt.Errorf("a sequence number")
			}
			return nil
		}), func(m *Model) tea.Cmd {
			seq, _ := strconv.ParseUint(strings.TrimSpace(m.formVals.str), 10, 64)
			return m.loadMessages(t.stream, seq, t.filter, false)
		}, nil)
	case "f":
		m.formVals.str = t.filter
		return m.openForm(inputForm("Filter subject", "Only the messages of this subject (wildcards allowed); blank for all.", ">", &m.formVals.str, nil), func(m *Model) tea.Cmd {
			return m.loadMessages(t.stream, 0, strings.TrimSpace(m.formVals.str), true)
		}, nil)
	case "d", "delete", "x":
		if msg != nil {
			return m.runPlan(cli.DeleteMessage(t.stream, msg.Seq), nil)
		}
	case "s":
		if st != nil {
			return m.subscribe(node{kind: kStream, stream: st})
		}
	case "P":
		subj := ""
		if msg != nil {
			subj = msg.Subject
		}
		return m.publish(subj)
	}
	return nil
}

// ---------------------------------------------------------------- subjects

func (m *Model) showSubjects(st *cli.Stream, filter string) tea.Cmd {
	return m.loadSubjects(st.Name(), filter)
}

func (m *Model) loadSubjects(stream, filter string) tea.Cmd {
	c := m.client
	w, h := m.width, m.height
	if m.scr != scrBusy {
		m.planBack = m.scr
	}
	return m.busy("Reading the subjects of "+stream+"…", func() tea.Msg {
		subs, err := c.Subjects(stream, filter)
		if err != nil {
			return tableMsg{err: fmt.Errorf("read subjects: %w", err)}
		}
		t := subjectsTable(stream, filter, subs)
		t.setSize(w, h)
		return tableMsg{tbl: t}
	})
}

type subjectRow struct {
	subject string
	count   uint64
}

func subjectsTable(stream, filter string, subs map[string]uint64) *table {
	desc := "Every subject with messages in the stream and how many it holds."
	if filter != "" {
		desc = "Subjects matching " + filter + " and how many messages each holds."
	}
	t := &table{kind: tkSubjects, stream: stream, filter: filter, title: "Subjects of stream " + stream, desc: desc,
		cols:  []tcol{{"Subject", 0}, {"Messages", 12}},
		help:  helpLine("enter", "messages of the subject", "f", "filter", "p", "purge the subject", "s", "subscribe", "esc", "back"),
		empty: "no subjects (the stream holds no messages)"}
	var rows []subjectRow
	for s, n := range subs {
		rows = append(rows, subjectRow{s, n})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].count != rows[j].count {
			return rows[i].count > rows[j].count
		}
		return rows[i].subject < rows[j].subject
	})
	for _, r := range rows {
		t.rows = append(t.rows, trow{cells: []string{r.subject, cli.Count(r.count)}, ref: r})
	}
	return t
}

func (m *Model) subjectKeys(key string) tea.Cmd {
	t := m.tbl
	var sel *subjectRow
	if r := t.selected(); r != nil {
		x := r.ref.(subjectRow)
		sel = &x
	}
	switch key {
	case "enter":
		if sel != nil {
			return m.loadMessages(t.stream, 0, sel.subject, true)
		}
	case "f":
		m.formVals.str = t.filter
		return m.openForm(inputForm("Filter", "Subjects matching this pattern (wildcards allowed); blank for all.", ">", &m.formVals.str, nil), func(m *Model) tea.Cmd {
			return m.loadSubjects(t.stream, strings.TrimSpace(m.formVals.str))
		}, nil)
	case "p":
		if sel != nil {
			return m.runPlan(cli.PurgeStream(t.stream, sel.subject, 0, 0), nil)
		}
	case "s":
		if sel != nil {
			return m.subscribeTo([]string{sel.subject}, "")
		}
	case "P":
		if sel != nil {
			return m.publish(sel.subject)
		}
	}
	return nil
}

// ---------------------------------------------------------------- keys

func (m *Model) showKeys(b *cli.Bucket) tea.Cmd {
	if b == nil {
		return nil
	}
	return m.loadKeys(b.Name())
}

func (m *Model) loadKeys(bucket string) tea.Cmd {
	c := m.client
	w, h := m.width, m.height
	if m.scr != scrBusy {
		m.planBack = m.scr
	}
	return m.busy("Reading the keys of "+bucket+"…", func() tea.Msg {
		keys, err := c.Keys(bucket)
		if err != nil {
			return tableMsg{err: fmt.Errorf("read keys: %w", err)}
		}
		t := keysTable(bucket, keys)
		t.setSize(w, h)
		return tableMsg{tbl: t}
	})
}

func keysTable(bucket string, keys []cli.KVEntry) *table {
	t := &table{kind: tkKeys, bucket: bucket, title: "Keys of bucket " + bucket,
		desc:  "Every key with its latest value; deleted keys are not listed (their history is, under h).",
		cols:  []tcol{{"Key", 0}, {"Revision", 9}, {"Created", 19}, {"Size", 9}, {"Value", 0}},
		help:  helpLine("enter", "value", "a", "put", "e", "edit value", "h", "history", "d", "delete", "D", "purge", "w", "watch", "esc", "back"),
		empty: "no keys (a puts one)"}
	for _, e := range keys {
		t.rows = append(t.rows, trow{cells: []string{e.Key, fmt.Sprint(e.Revision), cli.Date(e.Created), cli.Size(uint64(len(e.Value))), preview(e.Value)}, ref: e})
	}
	return t
}

func (m *Model) kvKeys(key string) tea.Cmd {
	t := m.tbl
	var e *cli.KVEntry
	if r := t.selected(); r != nil {
		x := r.ref.(cli.KVEntry)
		e = &x
	}
	switch key {
	case "enter":
		if e != nil {
			m.showText(fmt.Sprintf("%s > %s (revision %d)", t.bucket, e.Key, e.Revision), kvText(*e, m.width), helpLine("esc", "back", "↑↓", "scroll"))
		}
	case "a":
		return m.putKey(t.bucket, "", "")
	case "e":
		if e != nil {
			return m.putKey(t.bucket, e.Key, string(e.Value))
		}
	case "h":
		if e != nil {
			return m.loadHistory(t.bucket, e.Key)
		}
	case "d", "delete", "x":
		if e != nil {
			return m.runPlan(cli.DeleteKey(t.bucket, e.Key), nil)
		}
	case "D":
		if e != nil {
			return m.runPlan(cli.PurgeKey(t.bucket, e.Key), nil)
		}
	case "w":
		return m.watchKV(t.bucket, "")
	case "c":
		return m.runPlan(cli.CompactKV(t.bucket), nil)
	}
	return nil
}

func kvText(e cli.KVEntry, width int) string {
	d := &detailWriter{width: width}
	d.section("Key")
	d.row("Key", e.Key)
	d.row("Revision", fmt.Sprint(e.Revision))
	d.row("Operation", e.Op)
	d.row("Created", cli.Date(e.Created))
	d.row("Size", fmt.Sprintf("%d bytes", len(e.Value)))
	d.section("Value")
	return d.String() + bodyText(e.Value)
}

// putKey asks for a key and a value and writes them.
func (m *Model) putKey(bucket, key, value string) tea.Cmd {
	title := "Put a key in " + bucket
	fields := []*field{section("Key-value")}
	if key != "" {
		title = "Edit " + key + " in " + bucket
		fields = append(fields, infoField("Key", key))
	} else {
		fields = append(fields, textField("key", "Key", "", "letters, digits, - _ = . and / (dots separate levels)", "required", validKey))
	}
	fields = append(fields,
		textField("value", "Value", value, "any text; it is piped to nats kv put, so nothing is interpreted", "(empty)", nil),
		boolField("create", "Only if the key does not exist yet (nats kv create)", false, ""),
		textField("ttl", "TTL", "", "with create only: how long the key lives (needs per-key TTLs on the bucket)", "(none)", cli.ValidDuration),
	)
	return m.openEditor(newEditor(title, fields, m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
		k := key
		if k == "" {
			k = ed.str("key")
		}
		v := ed.get("value").text
		if ed.on("create") {
			return m.runPlan(cli.CreateKey(bucket, k, v, ed.str("ttl")), nil)
		}
		return m.runPlan(cli.PutKey(bucket, k, v), nil)
	})
}

func validKey(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("a key is required")
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_=./", r)) {
			return fmt.Errorf("keys hold letters, digits and - _ = . /")
		}
	}
	if strings.HasPrefix(s, ".") || strings.HasSuffix(s, ".") {
		return fmt.Errorf("a key cannot start or end with a dot")
	}
	return nil
}

// ---------------------------------------------------------------- history

func (m *Model) loadHistory(bucket, key string) tea.Cmd {
	c := m.client
	w, h := m.width, m.height
	if m.scr != scrBusy {
		m.planBack = m.scr
	}
	return m.busy("Reading the history of "+key+"…", func() tea.Msg {
		hist, err := c.History(bucket, key)
		if err != nil {
			return tableMsg{err: fmt.Errorf("read history: %w", err)}
		}
		t := historyTable(bucket, key, hist)
		t.setSize(w, h)
		t.cursor = len(t.rows) - 1
		t.clamp()
		return tableMsg{tbl: t}
	})
}

func historyTable(bucket, key string, hist []cli.KVEntry) *table {
	t := &table{kind: tkHistory, bucket: bucket, key: key, title: fmt.Sprintf("History of %s > %s", bucket, key),
		desc:  "Every revision the bucket still keeps (its history setting says how many), oldest first.",
		cols:  []tcol{{"Revision", 9}, {"Op", 7}, {"Created", 19}, {"Size", 9}, {"Value", 0}},
		help:  helpLine("enter", "value", "R", "revert to this revision", "esc", "back to the keys"),
		empty: "no history"}
	for _, e := range hist {
		var st *lipgloss.Style
		if e.Op != "PUT" {
			st = &styleWarn
		}
		t.rows = append(t.rows, trow{cells: []string{fmt.Sprint(e.Revision), e.Op, cli.Date(e.Created), cli.Size(uint64(len(e.Value))), preview(e.Value)}, style: st, ref: e})
	}
	return t
}

func (m *Model) historyKeys(key string) tea.Cmd {
	t := m.tbl
	var e *cli.KVEntry
	if r := t.selected(); r != nil {
		x := r.ref.(cli.KVEntry)
		e = &x
	}
	switch key {
	case "enter":
		if e != nil {
			m.showText(fmt.Sprintf("%s > %s (revision %d)", t.bucket, e.Key, e.Revision), kvText(*e, m.width), helpLine("esc", "back", "↑↓", "scroll"))
		}
	case "R":
		if e != nil {
			return m.runPlan(cli.RevertKey(t.bucket, e.Key, e.Revision), nil)
		}
	}
	return nil
}

// ---------------------------------------------------------------- objects

func (m *Model) showObjects(b *cli.ObjectBucket) tea.Cmd {
	if b == nil {
		return nil
	}
	return m.openTable(m.objectsTable(b))
}

func (m *Model) objectsTable(b *cli.ObjectBucket) *table {
	t := &table{kind: tkObjects, bucket: b.Name(), title: "Objects in " + b.Name(),
		desc:  "The files stored in the bucket (nats object ls); an object is read back with g.",
		cols:  []tcol{{"Name", 0}, {"Size", 10}, {"Modified", 19}, {"Chunks", 7}, {"Description", 0}, {"Digest", 24}},
		help:  helpLine("enter", "info", "a", "put a file", "g", "get to a file", "d", "delete", "w", "watch", "esc", "back"),
		empty: "no objects (a puts a file)"}
	if b.ListErr != nil {
		t.empty = "⚠ " + b.ListErr.Error()
	}
	for _, o := range b.Objects {
		var st *lipgloss.Style
		if o.Deleted {
			st = &styleWarn
		}
		t.rows = append(t.rows, trow{cells: []string{o.Name, cli.Size(o.Size), cli.Date(o.ModTime), fmt.Sprint(o.Chunks), o.Description, strings.TrimPrefix(o.Digest, "SHA-256=")}, style: st, ref: o})
	}
	return t
}

func (m *Model) objectKeys(key string) tea.Cmd {
	t := m.tbl
	var o *jetstream.ObjectInfo
	if r := t.selected(); r != nil {
		o = r.ref.(*jetstream.ObjectInfo)
	}
	switch key {
	case "enter":
		if o != nil {
			m.showText("Object "+o.Name+" in "+t.bucket, objectText(o, m.width), helpLine("esc", "back", "↑↓", "scroll"))
		}
	case "a":
		return m.putObject(t.bucket)
	case "g":
		if o != nil {
			m.formVals.str = filepath.Base(o.Name)
			return m.openForm(inputForm("Write "+o.Name+" to", "Path of the file to write (it is overwritten).", "", &m.formVals.str, nonEmpty("a path")), func(m *Model) tea.Cmd {
				return m.runPlan(cli.GetObject(t.bucket, o.Name, expandPath(m.formVals.str)), nil)
			}, nil)
		}
	case "d", "delete", "x":
		if o != nil {
			return m.runPlan(cli.DeleteObject(t.bucket, o.Name), nil)
		}
	case "w":
		return m.watchObjects(t.bucket)
	}
	return nil
}

func objectText(o *jetstream.ObjectInfo, width int) string {
	d := &detailWriter{width: width}
	d.section("Object")
	d.row("Name", o.Name)
	d.row("Bucket", o.Bucket)
	d.row("Description", o.Description)
	d.row("Size", fmt.Sprintf("%s (%d bytes)", cli.Size(o.Size), o.Size))
	d.row("Modified", cli.Date(o.ModTime))
	d.row("Chunks", fmt.Sprint(o.Chunks))
	d.row("Digest", o.Digest)
	d.row("NUID", o.NUID)
	d.row("Deleted", yesNo(o.Deleted))
	if o.Opts != nil && o.Opts.ChunkSize > 0 {
		d.row("Chunk size", cli.Size(uint64(o.Opts.ChunkSize)))
	}
	if len(o.Headers) > 0 {
		d.section("Headers")
		for _, k := range sortedKeys(o.Headers) {
			d.rows(k, o.Headers[k])
		}
	}
	if len(o.Metadata) > 0 {
		d.section("Metadata")
		for _, k := range sortedKeys(o.Metadata) {
			d.row(k, o.Metadata[k])
		}
	}
	return d.String()
}

// putObject asks for a file and stores it.
func (m *Model) putObject(bucket string) tea.Cmd {
	fields := []*field{
		section("Put a file into " + bucket),
		textField("file", "File", "", "path of the file to store", "required", existingFile),
		textField("name", "Object name", "", "blank: the file path as given", "(the path)", nil),
		textField("description", "Description", "", "", "(none)", nil),
	}
	return m.openEditor(newEditor("Put a file into "+bucket, fields, m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
		return m.runPlan(cli.PutObject(bucket, expandPath(ed.str("file")), ed.str("name"), ed.str("description")), nil)
	})
}
