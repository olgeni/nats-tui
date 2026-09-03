package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/olgeni/nats-tui/cli"
)

// kind is what a row of the main tree stands for.
type kind int

const (
	kContext kind = iota
	kSection
	kStream
	kConsumer
	kKV
	kObject
	kService
)

// group is which section a section row (or a child) belongs to.
type group int

const (
	secStreams group = iota
	secKV
	secObjects
	secServices
)

var sectionNames = map[group]string{secStreams: "Streams", secKV: "Key-value buckets", secObjects: "Object stores", secServices: "Services"}

// node is one row of the main tree.
type node struct {
	kind   kind
	sec    group
	stream *cli.Stream
	cons   *cli.Consumer
	kv     *cli.Bucket
	obj    *cli.ObjectBucket
	svc    *cli.Service
}

func (n node) name() string {
	switch n.kind {
	case kSection:
		return sectionNames[n.sec]
	case kStream:
		return n.stream.Name()
	case kConsumer:
		return n.cons.Name()
	case kKV:
		return n.kv.Name()
	case kObject:
		return n.obj.Name()
	case kService:
		return n.svc.Info.Name
	}
	return "context"
}

// id identifies a row across reloads (cursor restore, expansion state).
func (n node) id() string {
	switch n.kind {
	case kSection:
		return fmt.Sprintf("sec:%d", n.sec)
	case kStream:
		return "stream:" + n.stream.Name()
	case kConsumer:
		return "cons:" + n.cons.Stream.Name() + "/" + n.cons.Name()
	case kKV:
		return "kv:" + n.kv.Name()
	case kObject:
		return "obj:" + n.obj.Name()
	case kService:
		return "svc:" + n.svc.Info.Name + "/" + n.svc.Info.ID
	}
	return "ctx"
}

func (n node) typeName() string {
	switch n.kind {
	case kContext:
		return "context"
	case kStream:
		return "stream"
	case kConsumer:
		return "consumer"
	case kKV:
		return "kv"
	case kObject:
		return "object"
	case kService:
		return "service"
	}
	return ""
}

// streamOf is the stream a row belongs to (the row itself for a stream,
// the parent for a consumer, nil otherwise).
func (n node) streamOf() *cli.Stream {
	switch n.kind {
	case kStream:
		return n.stream
	case kConsumer:
		return n.cons.Stream
	}
	return nil
}

// rebuildRows flattens the store into rows, honouring expansion and the
// filter (a filter shows every matching entity with its parents).
func (m *Model) rebuildRows() {
	sel := ""
	if m.cursor < len(m.rows) {
		sel = m.rows[m.cursor].id()
	}
	m.rows = m.rows[:0]
	if m.store == nil {
		m.cursor = 0
		return
	}
	s := m.store
	f := strings.ToLower(strings.TrimSpace(m.filter))
	match := func(n node) bool {
		if f == "" {
			return true
		}
		if strings.Contains(strings.ToLower(n.name()), f) {
			return true
		}
		for _, t := range m.searchable(n) {
			if strings.Contains(strings.ToLower(t), f) {
				return true
			}
		}
		return false
	}
	open := func(key string, def bool) bool {
		if v, ok := m.expanded[key]; ok {
			return v
		}
		return def
	}
	m.rows = append(m.rows, node{kind: kContext})
	if s.HasJetStream() || len(s.Streams) > 0 {
		sec := node{kind: kSection, sec: secStreams}
		var children []node
		for _, st := range s.Streams {
			sn := node{kind: kStream, sec: secStreams, stream: st}
			var cons []node
			for _, c := range st.Consumers {
				cn := node{kind: kConsumer, sec: secStreams, cons: c}
				if f == "" && open(sn.id(), false) || f != "" && match(cn) {
					cons = append(cons, cn)
				}
			}
			if f == "" || match(sn) || len(cons) > 0 {
				children = append(children, sn)
				children = append(children, cons...)
			}
		}
		if f == "" || len(children) > 0 {
			m.rows = append(m.rows, sec)
			if f != "" || open(sec.id(), true) {
				m.rows = append(m.rows, children...)
			}
		}
		sec = node{kind: kSection, sec: secKV}
		children = nil
		for _, b := range s.KVs {
			bn := node{kind: kKV, sec: secKV, kv: b}
			if f == "" || match(bn) {
				children = append(children, bn)
			}
		}
		if f == "" || len(children) > 0 {
			m.rows = append(m.rows, sec)
			if f != "" || open(sec.id(), true) {
				m.rows = append(m.rows, children...)
			}
		}
		sec = node{kind: kSection, sec: secObjects}
		children = nil
		for _, b := range s.Objects {
			bn := node{kind: kObject, sec: secObjects, obj: b}
			if f == "" || match(bn) {
				children = append(children, bn)
			}
		}
		if f == "" || len(children) > 0 {
			m.rows = append(m.rows, sec)
			if f != "" || open(sec.id(), true) {
				m.rows = append(m.rows, children...)
			}
		}
	}
	if len(s.Services) > 0 {
		sec := node{kind: kSection, sec: secServices}
		var children []node
		for _, sv := range s.Services {
			sn := node{kind: kService, sec: secServices, svc: sv}
			if f == "" || match(sn) {
				children = append(children, sn)
			}
		}
		if f == "" || len(children) > 0 {
			m.rows = append(m.rows, sec)
			if f != "" || open(sec.id(), true) {
				m.rows = append(m.rows, children...)
			}
		}
	}
	m.cursor = 0
	for i, r := range m.rows {
		if r.id() == sel {
			m.cursor = i
		}
	}
	m.clampCursor()
}

// searchable is what the filter matches besides the name: subjects,
// descriptions, filter subjects.
func (m *Model) searchable(n node) []string {
	switch n.kind {
	case kStream:
		c := n.stream.Info.Config
		return append(append([]string{c.Description}, c.Subjects...), cli.Metadata(c.Metadata)...)
	case kConsumer:
		c := n.cons.Info.Config
		return append([]string{c.Description, c.FilterSubject, c.DeliverSubject}, c.FilterSubjects...)
	case kKV:
		return []string{n.kv.Status.Config().Description}
	case kObject:
		return []string{n.obj.Status.Description()}
	case kService:
		return []string{n.svc.Info.Description, n.svc.Info.ID, n.svc.Info.Version}
	case kContext:
		return []string{m.store.Context.URL, m.store.Context.Name}
	}
	return nil
}

func (m *Model) listHeight() int { return max(3, m.height-13) }

func (m *Model) clampCursor() {
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	h := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// selected is the node under the cursor (the context node when the tree
// is empty).
func (m *Model) selected() node {
	if m.cursor < len(m.rows) {
		return m.rows[m.cursor]
	}
	return node{kind: kContext}
}

// summary is the "Details" column: what is notable about the row.
func (m *Model) summary(n node) string {
	var parts []string
	switch n.kind {
	case kContext:
		s := m.store
		parts = append(parts, s.Context.URL)
		if s.Server.Version != "" {
			parts = append(parts, "server "+s.Server.Version)
		}
		if s.Server.Cluster != "" {
			parts = append(parts, "cluster "+s.Server.Cluster)
		}
		if s.Context.Creds != "" {
			parts = append(parts, "creds")
		} else if s.Context.User != "" {
			parts = append(parts, "user "+s.Context.User)
		} else if s.Context.NKey != "" {
			parts = append(parts, "nkey")
		}
		if s.Server.TLS {
			parts = append(parts, "tls")
		}
		if !s.HasJetStream() {
			parts = append(parts, "no JetStream")
		}
	case kSection:
		switch n.sec {
		case secStreams:
			parts = append(parts, fmt.Sprintf("%d streams, %d consumers", len(m.store.Streams), m.store.ConsumerCount()))
		case secKV:
			parts = append(parts, fmt.Sprintf("%d buckets", len(m.store.KVs)))
		case secObjects:
			parts = append(parts, fmt.Sprintf("%d buckets", len(m.store.Objects)))
		case secServices:
			parts = append(parts, fmt.Sprintf("%d instances", len(m.store.Services)))
		}
	case kStream:
		c := n.stream.Info.Config
		st := n.stream.Info.State
		parts = append(parts, fmt.Sprintf("%s R%d", cli.StorageName(c.Storage), c.Replicas))
		if len(c.Subjects) > 0 {
			parts = append(parts, strings.Join(c.Subjects, " "))
		}
		if c.Mirror != nil {
			parts = append(parts, "mirror of "+c.Mirror.Name)
		}
		for _, src := range c.Sources {
			parts = append(parts, "sources "+src.Name)
		}
		if c.Retention != 0 {
			parts = append(parts, c.Retention.String())
		}
		if n := n.stream.ConsumerCount(); n > 0 {
			parts = append(parts, fmt.Sprintf("%d consumers", n))
		}
		if c.MaxAge > 0 {
			parts = append(parts, "max age "+cli.HumanDuration(c.MaxAge))
		}
		if c.MaxMsgs > 0 {
			parts = append(parts, "max "+cli.Count(uint64(c.MaxMsgs))+" msgs")
		}
		if c.MaxBytes > 0 {
			parts = append(parts, "max "+cli.Size(uint64(c.MaxBytes)))
		}
		if st.NumDeleted > 0 {
			parts = append(parts, fmt.Sprintf("%d deleted", st.NumDeleted))
		}
		if c.Sealed {
			parts = append(parts, "SEALED")
		}
		if c.Description != "" {
			parts = append(parts, c.Description)
		}
		if n.stream.ConsumersErr != nil {
			parts = append(parts, "⚠ consumers: "+n.stream.ConsumersErr.Error())
		}
	case kConsumer:
		i := n.cons.Info
		c := i.Config
		if n.cons.Pull() {
			parts = append(parts, "pull")
		} else {
			parts = append(parts, "push → "+c.DeliverSubject)
		}
		parts = append(parts, "ack "+ackName(c))
		if f := consumerFilter(c); f != "" {
			parts = append(parts, "filter "+f)
		}
		if i.NumAckPending > 0 {
			parts = append(parts, fmt.Sprintf("%d ack pending", i.NumAckPending))
		}
		if i.NumRedelivered > 0 {
			parts = append(parts, fmt.Sprintf("%d redelivered", i.NumRedelivered))
		}
		if i.NumWaiting > 0 {
			parts = append(parts, fmt.Sprintf("%d waiting pulls", i.NumWaiting))
		}
		if i.Paused {
			parts = append(parts, "PAUSED "+cli.HumanDuration(i.PauseRemaining))
		}
		if c.Durable == "" && c.Name == "" {
			parts = append(parts, "ephemeral")
		}
		if c.Description != "" {
			parts = append(parts, c.Description)
		}
	case kKV:
		st := n.kv.Status
		parts = append(parts, fmt.Sprintf("history %d", st.History()))
		if st.TTL() > 0 {
			parts = append(parts, "ttl "+cli.HumanDuration(st.TTL()))
		}
		if n.kv.Info != nil {
			c := n.kv.Info.Config
			parts = append(parts, fmt.Sprintf("%s R%d", cli.StorageName(c.Storage), c.Replicas))
		}
		if st.IsCompressed() {
			parts = append(parts, "compressed")
		}
		if d := st.Config().Description; d != "" {
			parts = append(parts, d)
		}
	case kObject:
		st := n.obj.Status
		if st.TTL() > 0 {
			parts = append(parts, "ttl "+cli.HumanDuration(st.TTL()))
		}
		parts = append(parts, fmt.Sprintf("%s R%d", cli.StorageName(st.Storage()), st.Replicas()))
		if st.Sealed() {
			parts = append(parts, "SEALED")
		}
		if st.IsCompressed() {
			parts = append(parts, "compressed")
		}
		if d := st.Description(); d != "" {
			parts = append(parts, d)
		}
		if n.obj.ListErr != nil {
			parts = append(parts, "⚠ "+n.obj.ListErr.Error())
		}
	case kService:
		i := n.svc.Info
		parts = append(parts, "v"+i.Version, "id "+i.ID, fmt.Sprintf("%d endpoints", len(i.Endpoints)))
		if i.Description != "" {
			parts = append(parts, i.Description)
		}
	}
	return strings.Join(parts, ", ")
}

// consumerFilter is the filter subject(s) of a consumer.
func consumerFilter(c jetstream.ConsumerConfig) string {
	if len(c.FilterSubjects) > 0 {
		return strings.Join(c.FilterSubjects, " ")
	}
	return c.FilterSubject
}

// ackName is the ack policy as nats spells it.
func ackName(c jetstream.ConsumerConfig) string {
	switch c.AckPolicy {
	case jetstream.AckNonePolicy:
		return "none"
	case jetstream.AckAllPolicy:
		return "all"
	}
	return "explicit"
}

func (m *Model) updateMain(msg tea.Msg) (tea.Model, tea.Cmd) {
	if mm, ok := msg.(tea.MouseMsg); ok && m.store != nil {
		return m.mouseMain(mm)
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if m.filterOn {
		switch k.String() {
		case "esc":
			m.filter, m.filterOn = "", false
			m.rebuildRows()
		case "enter":
			m.filterOn = false
		case "backspace":
			if r := []rune(m.filter); len(r) > 0 {
				m.filter = string(r[:len(r)-1])
				m.rebuildRows()
			}
		case "up", "down", "pgup", "pgdown":
			m.filterOn = false
			return m.updateMain(msg)
		default:
			if k.Type == tea.KeyRunes || k.Type == tea.KeySpace {
				m.filter += k.String()
				m.rebuildRows()
			}
		}
		return m, nil
	}
	if m.store == nil {
		return m.updateEmpty(k)
	}
	n := m.selected()
	switch k.String() {
	case "q", "esc":
		if m.filter != "" {
			m.filter = ""
			m.rebuildRows()
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit
	case "?", "f1":
		m.vp.SetContent(helpText)
		m.vp.GotoTop()
		m.prevScr, m.scr = scrMain, scrHelp
	case "h":
		m.vp.SetContent(keymapView(m.width))
		m.vp.GotoTop()
		m.prevScr, m.scr = scrMain, scrKeys
	case "up", "k":
		m.cursor--
	case "down", "j":
		m.cursor++
	case "pgup":
		m.cursor -= m.listHeight()
	case "pgdown":
		m.cursor += m.listHeight()
	case "home":
		m.cursor = 0
	case "end":
		m.cursor = len(m.rows) - 1
	case "right", "l":
		switch n.kind {
		case kStream:
			m.expanded[n.id()] = true
			m.rebuildRows()
			return m, m.ensureConsumers(n.stream, nil)
		case kSection:
			m.expanded[n.id()] = true
			m.rebuildRows()
		case kContext:
			return m, m.expandAll(true)
		}
	case "left":
		switch n.kind {
		case kConsumer:
			m.expanded["stream:"+n.cons.Stream.Name()] = false
			m.rebuildRows()
			for i, r := range m.rows {
				if r.kind == kStream && r.stream == n.cons.Stream {
					m.cursor = i
				}
			}
		case kStream, kSection:
			m.expanded[n.id()] = false
			m.rebuildRows()
		case kKV, kObject, kService:
			m.expanded[fmt.Sprintf("sec:%d", n.sec)] = false
			m.rebuildRows()
			for i, r := range m.rows {
				if r.kind == kSection && r.sec == n.sec {
					m.cursor = i
				}
			}
		default:
			return m, m.expandAll(false)
		}
	case " ":
		switch n.kind {
		case kStream, kSection:
			open := !m.isOpen(n)
			m.expanded[n.id()] = open
			m.rebuildRows()
			if open && n.kind == kStream {
				return m, m.ensureConsumers(n.stream, nil)
			}
		default:
			return m, m.expandAll(!m.anyExpanded())
		}
	case "*":
		return m, m.expandAll(!m.anyExpanded())
	case "/":
		m.filterOn = true
	case "r":
		return m, m.busyReload()
	case "ctrl+r":
		return m, m.toggleRefresh()
	case "m":
		return m, m.toggleMouse()
	case "enter":
		switch n.kind {
		case kSection:
			m.expanded[n.id()] = !m.isOpen(n)
			m.rebuildRows()
			return m, nil
		case kStream:
			return m, m.ensureConsumers(n.stream, func(m *Model) tea.Cmd { m.showDetails(n, scrMain); return nil })
		case kObject:
			return m, m.ensureObjects(n.obj, func(m *Model) tea.Cmd { m.showDetails(n, scrMain); return nil })
		case kService:
			m.showDetails(n, scrMain)
			return m, m.serviceStats(n.svc)
		}
		m.showDetails(n, scrMain)
	case "J":
		if n.kind == kObject {
			return m, m.ensureObjects(n.obj, func(m *Model) tea.Cmd { m.showJSON(n); return nil })
		}
		if n.kind != kSection {
			m.showJSON(n)
		}
	case "e":
		return m, m.editEntity(n)
	case "a":
		return m, m.addChild(n)
	case "A":
		return m, m.addStream()
	case "d", "delete":
		return m, m.deleteEntity(n)
	case "P":
		if st := n.streamOf(); st != nil {
			return m, m.purgeStream(st)
		}
	case "v":
		if st := n.streamOf(); st != nil {
			return m, m.showMessages(st, 0, "")
		}
		if n.kind == kKV {
			return m, m.showKeys(n.kv)
		}
		if n.kind == kObject {
			return m, m.showObjects(n.obj)
		}
	case "t":
		if st := n.streamOf(); st != nil {
			return m, m.showSubjects(st, "")
		}
	case "K":
		if n.kind == kKV {
			return m, m.showKeys(n.kv)
		}
		return m, m.pickBucket("Keys of which bucket?", func(m *Model, b *cli.Bucket) tea.Cmd { return m.showKeys(b) })
	case "O":
		if n.kind == kObject {
			return m, m.showObjects(n.obj)
		}
		return m, m.pickObjectStore("Objects of which store?", func(m *Model, b *cli.ObjectBucket) tea.Cmd { return m.showObjects(b) })
	case "n":
		if n.kind == kConsumer {
			return m, m.nextMessages(n.cons)
		}
	case "u":
		if n.kind == kConsumer {
			return m, m.pauseResume(n.cons)
		}
	case "s":
		return m, m.subscribe(n)
	case "p":
		return m, m.publish(defaultSubject(n))
	case "R":
		return m, m.request(defaultSubject(n))
	case "E":
		return m, m.events()
	case "w":
		return m, m.watch(n)
	case "I":
		return m, m.accountInfo()
	case "M":
		return m, m.monitor()
	case "T":
		return m, m.reports(n)
	case "C":
		return m, m.showContexts()
	case "S":
		return m, m.switchContext()
	case "b":
		if st := n.streamOf(); st != nil {
			return m, m.backupStream(st)
		}
	case "B":
		return m, m.restoreStream()
	case "y":
		if st := n.streamOf(); st != nil {
			return m, m.copyStream(st)
		}
	case "x":
		return m, m.seal(n)
	}
	m.clampCursor()
	return m, nil
}

// defaultSubject is the subject a publish/request form starts with for a
// row: a stream's first subject, a consumer's filter.
func defaultSubject(n node) string {
	switch n.kind {
	case kStream:
		if s := n.stream.Info.Config.Subjects; len(s) > 0 {
			return strings.TrimSuffix(strings.TrimSuffix(s[0], ">"), "*")
		}
	case kConsumer:
		if f := consumerFilter(n.cons.Info.Config); f != "" {
			return strings.TrimSuffix(strings.TrimSuffix(strings.Fields(f)[0], ">"), "*")
		}
		if s := n.cons.Stream.Info.Config.Subjects; len(s) > 0 {
			return strings.TrimSuffix(strings.TrimSuffix(s[0], ">"), "*")
		}
	case kService:
		for _, e := range n.svc.Info.Endpoints {
			return e.Subject
		}
	}
	return ""
}

// toggleMouse switches mouse reporting on or off.
func (m *Model) toggleMouse() tea.Cmd {
	m.mouse = !m.mouse
	if m.mouse {
		m.setStatus("Mouse on: click a row to select it, click it again to open it, wheel scrolls (hold shift to select text)")
		return tea.EnableMouseCellMotion
	}
	m.setStatus("Mouse off")
	return tea.DisableMouse
}

// mouseMain handles mouse events on the main screen: the wheel moves the
// selection, a click selects a row, a click on the selected row opens it.
func (m *Model) mouseMain(mm tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch {
	case mm.Button == tea.MouseButtonWheelUp:
		m.cursor -= 3
	case mm.Button == tea.MouseButtonWheelDown:
		m.cursor += 3
	case mm.Button == tea.MouseButtonLeft && mm.Action == tea.MouseActionPress:
		idx := m.offset + mm.Y - m.tableTop
		if mm.Y < m.tableTop || idx < 0 || idx >= len(m.rows) || idx >= m.offset+m.listHeight() {
			return m, nil
		}
		if idx == m.cursor {
			return m.updateMain(tea.KeyMsg{Type: tea.KeyEnter})
		}
		m.cursor = idx
	}
	m.clampCursor()
	return m, nil
}

func (m *Model) isOpen(n node) bool {
	if v, ok := m.expanded[n.id()]; ok {
		return v
	}
	return n.kind == kSection
}

func (m *Model) anyExpanded() bool {
	for _, st := range m.store.Streams {
		if m.expanded["stream:"+st.Name()] {
			return true
		}
	}
	return false
}

// expandAll opens or closes every stream; opening fetches the consumers
// not known yet.
func (m *Model) expandAll(on bool) tea.Cmd {
	var cmds []tea.Cmd
	for _, st := range m.store.Streams {
		m.expanded["stream:"+st.Name()] = on
		if on && !st.Loaded {
			cmds = append(cmds, m.ensureConsumers(st, nil))
		}
	}
	for _, sec := range []group{secStreams, secKV, secObjects, secServices} {
		m.expanded[fmt.Sprintf("sec:%d", sec)] = true
	}
	m.rebuildRows()
	return tea.Batch(cmds...)
}

// updateEmpty handles keys when there is no connection.
func (m *Model) updateEmpty(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q", "esc":
		m.quitting = true
		return m, tea.Quit
	case "?", "f1":
		m.vp.SetContent(helpText)
		m.vp.GotoTop()
		m.prevScr, m.scr = scrMain, scrHelp
	case "r", "enter":
		return m, m.busyReload()
	case "C":
		return m, m.showContexts()
	case "S":
		return m, m.switchContext()
	}
	return m, nil
}

// ---------------------------------------------------------------- main view

func (m *Model) mainView() string {
	if m.store == nil {
		var b strings.Builder
		b.WriteString(titleBar(m.width, "nats-tui — "+m.label()) + "\n\n")
		if m.connErr == "" {
			b.WriteString("  Connecting…\n")
			return b.String()
		}
		b.WriteString(styleErr.Render(" "+fit("Cannot connect: "+m.connErr, m.width-2)) + "\n\n")
		fmt.Fprintf(&b, " %s %s\n", styleLabel.Render("Contexts:"), cli.ConfigDir())
		b.WriteString("\n " + helpLine("r/enter", "retry", "C", "contexts (add, edit, use another)", "S", "switch context", "?", "help", "q", "quit"))
		if m.errMsg != "" {
			b.WriteString("\n\n" + styleErr.Render(" "+fit(m.errMsg, m.width-2)))
		}
		return b.String()
	}
	s := m.store
	var b strings.Builder
	title := "NATS server " + s.Context.URL
	if s.ContextName != "" {
		title = "NATS context " + s.ContextName + " — " + s.Context.URL
	}
	b.WriteString(titleBar(m.width, title) + "\n")
	// header
	ctxName := s.ContextName
	if ctxName == "" {
		ctxName = "(none)"
	}
	rtt := ""
	if s.Server.RTT > 0 {
		rtt = s.Server.RTT.Round(1000).String()
	}
	cluster := s.Server.Cluster
	if cluster == "" {
		cluster = "(none)"
	}
	b.WriteString(packLine(m.width,
		styleLabel.Render(" Context: ")+styleValue.Render(ctxName),
		styleLabel.Render("   Server: ")+styleValue.Render(cli.ServerLabel(s.Server.Name)),
		styleLabel.Render("   Version: ")+styleValue.Render(s.Server.Version),
		styleLabel.Render("   Cluster: ")+styleValue.Render(cluster),
		styleLabel.Render("   RTT: ")+styleValue.Render(rtt)) + "\n")
	js := s.JSError
	if a := s.Account; a != nil {
		js = fmt.Sprintf("%d streams, %d consumers, %s stored, %s in memory", a.Streams, a.Consumers, cli.Size(a.Store), cli.Size(a.Memory))
		if a.Limits.MaxStore > 0 {
			js += " of " + cli.Bytes(a.Limits.MaxStore)
		}
		if a.Domain != "" {
			js += ", domain " + a.Domain
		}
	}
	auth := "(none)"
	switch {
	case s.Context.Creds != "":
		auth = fitLeft(s.Context.Creds, max(10, m.width/4))
	case s.Context.User != "":
		auth = "user " + s.Context.User
	case s.Context.NKey != "":
		auth = "nkey " + fitLeft(s.Context.NKey, max(10, m.width/4))
	case s.Context.Token:
		auth = "token"
	case s.Context.NscURL != "":
		auth = s.Context.NscURL
	}
	b.WriteString(packLine(m.width,
		styleLabel.Render(" JetStream: ")+styleValue.Render(js),
		styleLabel.Render("   Credentials: ")+styleValue.Render(auth)) + "\n")
	line3 := []string{styleLabel.Render(" Config: ") + fitLeft(s.ConfigDir, max(10, m.width/3)),
		styleLabel.Render("   Max payload: ") + styleValue.Render(cli.Size(uint64(max(s.Server.MaxPayload, 0)))),
		styleLabel.Render("   Connected: ") + styleValue.Render(s.Server.Addr)}
	if s.Server.TLS {
		line3 = append(line3, styleLabel.Render("   TLS: ")+styleValue.Render("yes"))
	}
	if m.refresh > 0 {
		line3 = append(line3, styleLabel.Render("   Refresh: ")+styleValue.Render("every "+cli.HumanDuration(m.refresh)))
	}
	b.WriteString(packLine(m.width, line3...) + "\n")

	hdr := fmt.Sprintf(" Entities: %d streams, %d consumers, %d buckets, %d object stores", len(s.Streams), s.ConsumerCount(), len(s.KVs), len(s.Objects))
	if len(s.Services) > 0 {
		hdr += fmt.Sprintf(", %d services", len(s.Services))
	}
	if m.filter != "" || m.filterOn {
		hdr += styleLabel.Render("   Filter: ") + styleFocus.Render(m.filter)
		if m.filterOn {
			hdr += styleFocus.Render("▏")
		} else {
			hdr += styleMuted.Render(fmt.Sprintf("  (%d shown, esc clears)", len(m.rows)))
		}
	}
	b.WriteString(hdr + "\n")

	// columns
	wName, wType, wMsgs, wSize, wLast := 28, 9, 11, 10, 10
	wDetail := m.width - wName - wType - wMsgs - wSize - wLast - 6
	if wDetail < 12 {
		wName = max(14, wName+wDetail-12)
		wDetail = m.width - wName - wType - wMsgs - wSize - wLast - 6
	}
	header := " " + fit("Name", wName) + " " + fit("Type", wType) + " " + fit("Messages", wMsgs) + " " + fit("Size", wSize) + " " + fit("Last", wLast) + " " + fit("Details", wDetail)
	b.WriteString(styleHeader.Render(header) + "\n")
	h := m.listHeight()
	m.tableTop = strings.Count(b.String(), "\n")
	for i := m.offset; i < len(m.rows) && i < m.offset+h; i++ {
		n := m.rows[i]
		name := n.name()
		msgs, size, last := "", "", ""
		var st *lipgloss.Style
		switch n.kind {
		case kContext:
			name = "▾ " + name
			if s.ContextName != "" {
				name = "▾ " + s.ContextName
			}
			last = cli.Ago(s.Loaded, m.now)
		case kSection:
			mark := "▸ "
			if m.isOpen(n) || m.filter != "" {
				mark = "▾ "
			}
			name = "  " + mark + name
		case kStream:
			mark := "▸ "
			if m.isOpen(n) || m.filter != "" {
				mark = "▾ "
			}
			if n.stream.ConsumerCount() == 0 {
				mark = "· "
			}
			name = "      " + mark + name
			ss := n.stream.Info.State
			msgs, size, last = cli.Count(ss.Msgs), cli.Size(ss.Bytes), cli.Ago(ss.LastTime, m.now)
			if n.stream.Info.Config.Sealed {
				st = &styleWarn
			}
		case kConsumer:
			name = "          " + name
			ci := n.cons.Info
			msgs = cli.Count(ci.NumPending) + " pend"
			if ci.NumAckPending > 0 {
				size = fmt.Sprintf("%d ack", ci.NumAckPending)
			}
			if ci.Delivered.Last != nil {
				last = cli.Ago(*ci.Delivered.Last, m.now)
			}
			if ci.Paused {
				st = &styleWarn
			} else {
				st = &styleMuted
			}
		case kKV:
			name = "      · " + name
			msgs, size = cli.Count(n.kv.Status.Values())+" vals", cli.Size(n.kv.Status.Bytes())
			if n.kv.Info != nil {
				last = cli.Ago(n.kv.Info.State.LastTime, m.now)
			}
		case kObject:
			name = "      · " + name
			size = cli.Size(n.obj.Status.Size())
			if n.obj.Loaded {
				msgs = fmt.Sprintf("%d objs", len(n.obj.Objects))
			}
			if n.obj.Info != nil {
				last = cli.Ago(n.obj.Info.State.LastTime, m.now)
			}
			if n.obj.Status.Sealed() {
				st = &styleWarn
			}
		case kService:
			name = "      · " + name
		}
		row := " " + fit(name, wName) + " " + fit(n.typeName(), wType) + " " + fit(msgs, wMsgs) + " " + fit(size, wSize) + " " + fit(last, wLast) + " " + fit(m.summary(n), wDetail)
		switch {
		case i == m.cursor:
			row = styleSelected.Render(row)
		case st != nil:
			row = st.Render(row)
		}
		b.WriteString(row + "\n")
	}
	for i := len(m.rows) - m.offset; i < h; i++ {
		b.WriteString("\n")
	}
	if len(m.rows) > h {
		b.WriteString(styleMuted.Render(fmt.Sprintf(" (%d–%d of %d)", m.offset+1, min(m.offset+h, len(m.rows)), len(m.rows))))
	}
	b.WriteString("\n")
	// selection detail (two lines)
	n := m.selected()
	b.WriteString(styleLabel.Render(" Selected: ") + styleValue.Render(m.nodeTitle(n)) + "\n")
	b.WriteString(styleMuted.Render(" "+fit(m.selectionLine(n), m.width-2)) + "\n")
	switch {
	case m.errMsg != "":
		b.WriteString(styleErr.Render(" "+fit(m.errMsg, m.width-2)) + "\n")
	case m.status != "":
		b.WriteString(styleOK.Render(" "+fit(m.status, m.width-2)) + "\n")
	default:
		b.WriteString("\n")
	}
	if m.width < 120 {
		b.WriteString(" " + helpLine("↑↓", "select", "→←", "expand", "enter", "details", "e", "edit", "a/A", "add", "d", "delete", "/", "filter", "v", "messages") + "\n")
		b.WriteString(" " + helpLine("s", "subscribe", "p", "publish", "R", "request", "w", "watch", "E", "events", "K/O", "keys/objects", "C", "contexts") + "\n")
		b.WriteString(" " + helpLine("h", "all keys", "?", "help", "q", "quit"))
	} else {
		b.WriteString(" " + helpLine("↑↓", "select", "→←", "expand/collapse", "enter", "details", "e", "edit", "a/A", "add consumer/stream", "d", "delete", "P", "purge", "/", "filter", "J", "json") + "\n")
		b.WriteString(" " + helpLine("v", "messages", "t", "subjects", "n", "next msg", "u", "pause/resume", "K", "keys", "O", "objects", "s", "subscribe", "p", "publish", "R", "request", "w", "watch", "E", "events") + "\n")
		b.WriteString(" " + helpLine("I", "account", "M", "monitor", "T", "reports", "C", "contexts", "S", "switch", "b/B", "backup/restore", "y", "copy", "x", "seal", "m", "mouse", "h", "all keys", "r", "reload", "?", "help", "q", "quit"))
	}
	return lipgloss.NewStyle().MaxWidth(m.width).Render(b.String())
}

// nodeTitle names a row with its type: "stream ORDERS", "context localhost".
func (m *Model) nodeTitle(n node) string {
	if n.kind == kContext {
		return m.label()
	}
	return strings.TrimSpace(n.typeName() + " " + n.name())
}

// packLine joins the segments that fit in width, in order, dropping the
// rest (each segment carries its own leading separator).
func packLine(width int, segs ...string) string {
	line := ""
	for _, s := range segs {
		if lipgloss.Width(line)+lipgloss.Width(s) > width {
			break
		}
		line += s
	}
	return line
}

// selectionLine is the second detail line for the selected node.
func (m *Model) selectionLine(n node) string {
	var parts []string
	switch n.kind {
	case kContext:
		if p := m.store.Context.Path; p != "" {
			parts = append(parts, p)
		}
		parts = append(parts, "server id "+cli.ShortID(m.store.Server.ID))
		if len(m.store.Server.Discovered) > 0 {
			parts = append(parts, fmt.Sprintf("%d more servers discovered", len(m.store.Server.Discovered)))
		}
	case kStream:
		i := n.stream.Info
		parts = append(parts, "created "+cli.Date(i.Created))
		parts = append(parts, fmt.Sprintf("seq %d–%d", i.State.FirstSeq, i.State.LastSeq))
		if i.State.NumSubjects > 0 {
			parts = append(parts, fmt.Sprintf("%d subjects", i.State.NumSubjects))
		}
		if !i.State.FirstTime.IsZero() && i.State.Msgs > 0 {
			parts = append(parts, "oldest "+cli.Ago(i.State.FirstTime, m.now))
		}
		if i.Cluster != nil && i.Cluster.Leader != "" {
			parts = append(parts, "leader "+i.Cluster.Leader)
		}
	case kConsumer:
		i := n.cons.Info
		parts = append(parts, "created "+cli.Date(i.Created))
		parts = append(parts, fmt.Sprintf("delivered %d (stream seq %d)", i.Delivered.Consumer, i.Delivered.Stream))
		parts = append(parts, fmt.Sprintf("ack floor %d (stream seq %d)", i.AckFloor.Consumer, i.AckFloor.Stream))
		parts = append(parts, fmt.Sprintf("deliver %s", i.Config.DeliverPolicy.String()))
		if i.Config.AckWait > 0 {
			parts = append(parts, "ack wait "+cli.HumanDuration(i.Config.AckWait))
		}
	case kKV:
		if n.kv.Info != nil {
			parts = append(parts, "created "+cli.Date(n.kv.Info.Created), "stream "+n.kv.Info.Config.Name)
		}
		if n.kv.Status.LimitMarkerTTL() > 0 {
			parts = append(parts, "marker ttl "+cli.HumanDuration(n.kv.Status.LimitMarkerTTL()))
		}
	case kObject:
		if n.obj.Info != nil {
			parts = append(parts, "created "+cli.Date(n.obj.Info.Created), "stream "+n.obj.Info.Config.Name)
		}
	case kService:
		for _, e := range n.svc.Info.Endpoints {
			parts = append(parts, e.Name+" on "+e.Subject)
		}
	case kSection:
		switch n.sec {
		case secStreams:
			parts = append(parts, "A adds a stream; a stream's consumers are listed under it (→ expands)")
		case secKV:
			parts = append(parts, "a adds a bucket; K lists the keys of one")
		case secObjects:
			parts = append(parts, "a adds an object store; O lists the objects of one")
		case secServices:
			parts = append(parts, "services answering the discovery ping ($SRV.INFO)")
		}
	}
	return strings.Join(parts, " · ")
}

// sortedKeys returns the keys of a string map, sorted.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
