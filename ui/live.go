package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/olgeni/nats-tui/cli"
)

// The live screen shows what arrives while it is open: the messages of a
// subscription, the changes of a bucket, the advisories of the server. The
// subscription runs in the client library and feeds a channel; the screen
// drains it in batches, keeps the last entries, and follows the newest one
// unless the user scrolled back.

const maxLive = 5000

type live struct {
	title    string
	desc     string // what is subscribed, for the second line
	command  string // the nats command this stands for
	src      *cli.Live
	entries  []cli.Event
	total    int
	dropped  int
	ended    bool
	cursor   int
	offset   int
	follow   bool
	filter   editLine
	filterOn bool
	width    int
	height   int
	vis      []int // indexes of the entries matching the filter
}

type (
	liveMsg struct {
		lv   *live
		evs  []cli.Event
		done bool
	}
	liveOpenMsg struct {
		lv  *live
		err error
	}
)

func newLive(title, desc, command string, src *cli.Live, width, height int) *live {
	l := &live{title: title, desc: desc, command: command, src: src, follow: true, width: width, height: height}
	l.filter = newEditLine("")
	l.filter.Focus()
	return l
}

func (l *live) setSize(w, h int) { l.width, l.height = w, h; l.clamp() }
func (l *live) listHeight() int  { return max(3, l.height-5) }

// wait is the command that delivers the next batch of events.
func (l *live) wait() tea.Cmd {
	src := l.src
	return func() tea.Msg {
		ev, ok := <-src.Events
		if !ok {
			return liveMsg{lv: l, done: true}
		}
		evs := []cli.Event{ev}
		for len(evs) < 512 {
			select {
			case ev, ok := <-src.Events:
				if !ok {
					return liveMsg{lv: l, evs: evs, done: true}
				}
				evs = append(evs, ev)
			default:
				return liveMsg{lv: l, evs: evs}
			}
		}
		return liveMsg{lv: l, evs: evs}
	}
}

func (l *live) add(evs []cli.Event) {
	l.total += len(evs)
	l.entries = append(l.entries, evs...)
	if over := len(l.entries) - maxLive; over > 0 {
		l.entries = l.entries[over:]
		l.dropped += over
		l.cursor -= over
	}
	l.refilter()
}

func (l *live) refilter() {
	f := strings.ToLower(strings.TrimSpace(l.filter.Value()))
	l.vis = l.vis[:0]
	for i, e := range l.entries {
		if f == "" || strings.Contains(strings.ToLower(e.Subject), f) || strings.Contains(strings.ToLower(string(e.Data)), f) || strings.Contains(strings.ToLower(e.Text), f) {
			l.vis = append(l.vis, i)
		}
	}
	if l.follow {
		l.cursor = len(l.vis) - 1
	}
	l.clamp()
}

func (l *live) clamp() {
	if l.cursor >= len(l.vis) {
		l.cursor = len(l.vis) - 1
	}
	if l.cursor < 0 {
		l.cursor = 0
	}
	h := l.listHeight()
	if l.cursor < l.offset {
		l.offset = l.cursor
	}
	if l.cursor >= l.offset+h {
		l.offset = l.cursor - h + 1
	}
	if l.offset < 0 {
		l.offset = 0
	}
}

// selected is the entry under the cursor.
func (l *live) selected() *cli.Event {
	if l.cursor < len(l.vis) {
		return &l.entries[l.vis[l.cursor]]
	}
	return nil
}

func (l *live) View() string {
	var b strings.Builder
	b.WriteString(titleBar(l.width, l.title) + "\n")
	state := fmt.Sprintf("%d received", l.total)
	if l.dropped > 0 {
		state += fmt.Sprintf(", first %d discarded", l.dropped)
	}
	if l.ended {
		state += " · ended"
	} else if l.follow {
		state += " · following"
	} else {
		state += " · paused (end resumes)"
	}
	if !l.filter.Empty() || l.filterOn {
		state += "   Filter: " + l.filter.View()
		if !l.filterOn {
			state += fmt.Sprintf(" (%d shown)", len(l.vis))
		}
	}
	b.WriteString(" " + styleMuted.Render(fit(l.desc, max(1, l.width-2))) + "\n")
	b.WriteString(" " + fit(state, max(1, l.width-2)) + "\n")
	wTime, wKind, wSubj, wSize := 12, 6, min(36, max(14, l.width/4)), 8
	wBody := max(8, l.width-wTime-wKind-wSubj-wSize-6)
	b.WriteString(styleHeader.Render(" "+fit("Time", wTime)+" "+fit("Kind", wKind)+" "+fit("Subject", wSubj)+" "+fit("Size", wSize)+" "+fit("Body", wBody)) + "\n")
	h := l.listHeight()
	for i := l.offset; i < len(l.vis) && i < l.offset+h; i++ {
		e := l.entries[l.vis[i]]
		kind := e.Kind
		switch e.Kind {
		case "msg":
			kind = ""
			if len(e.Header) > 0 {
				kind = "hdr"
			}
			if e.Reply != "" {
				kind = strings.TrimSpace(kind + " req")
			}
		case "put", "delete", "purge", "object":
			kind = strings.ToUpper(e.Kind[:min(3, len(e.Kind))])
		}
		subj := e.Subject
		if e.Seq > 0 && e.Kind != "msg" {
			subj = fmt.Sprintf("%s @%d", e.Subject, e.Seq)
		}
		body := preview(e.Data)
		size := ""
		if e.Kind == "msg" || e.Kind == "put" {
			size = cli.Size(uint64(len(e.Data)))
		}
		if e.Text != "" {
			body = e.Text
		}
		line := " " + fit(e.At.Local().Format("15:04:05.000"), wTime) + " " + fit(kind, wKind) + " " + fit(subj, wSubj) + " " + fit(size, wSize) + " " + fit(body, wBody)
		switch {
		case i == l.cursor:
			line = styleSelected.Render(line)
		case e.Kind == "error":
			line = styleErr.Render(line)
		case e.Kind == "info":
			line = styleMuted.Render(line)
		case e.Kind == "delete" || e.Kind == "purge":
			line = styleWarn.Render(line)
		}
		b.WriteString(line + "\n")
	}
	if len(l.vis) == 0 {
		if l.ended {
			b.WriteString(styleMuted.Render("   (nothing received)") + "\n")
		} else {
			b.WriteString(styleMuted.Render("   (waiting…)") + "\n")
		}
	}
	for i := max(len(l.vis)-l.offset, 1); i < h; i++ {
		b.WriteString("\n")
	}
	if l.filterOn {
		b.WriteString(helpLine("type", "filter subject/body", "enter", "keep", "esc", "clear"))
	} else {
		b.WriteString(helpLine("enter", "message", "↑↓", "browse (stops following)", "end", "follow", "p", "publish", "/", "filter", "c", "clear", "esc", "stop"))
	}
	return lipgloss.NewStyle().MaxWidth(l.width).Render(b.String())
}

// preview is the first line of a body, or a note about binary data.
func preview(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return fmt.Sprintf("(binary, %d bytes)", len(data))
	}
	s := string(data)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i] + " ⏎"
	}
	return s
}

// updateLive handles keys on the live screen.
func (m *Model) updateLive(msg tea.Msg) (tea.Model, tea.Cmd) {
	l := m.lv
	if mm, ok := msg.(tea.MouseMsg); ok {
		switch {
		case mm.Button == tea.MouseButtonWheelUp:
			l.follow = false
			l.cursor -= 3
		case mm.Button == tea.MouseButtonWheelDown:
			l.cursor += 3
		case mm.Button == tea.MouseButtonLeft && mm.Action == tea.MouseActionPress:
			idx := l.offset + mm.Y - 4
			if mm.Y < 4 || idx < 0 || idx >= len(l.vis) || idx >= l.offset+l.listHeight() {
				return m, nil
			}
			if idx == l.cursor {
				return m.updateLive(tea.KeyMsg{Type: tea.KeyEnter})
			}
			l.follow = false
			l.cursor = idx
		}
		l.clamp()
		return m, nil
	}
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if l.filterOn {
		switch k.String() {
		case "esc":
			l.filter.Set("")
			l.filterOn = false
			l.refilter()
		case "enter":
			l.filterOn = false
		default:
			// every other key edits the filter, inside it as well as
			// at its end
			if l.filter.Update(k) {
				l.refilter()
			}
		}
		return m, nil
	}
	switch k.String() {
	case "esc", "q":
		m.stopLive()
		m.scr = m.lvBack
		return m, nil
	case "up", "k":
		l.follow = false
		l.cursor--
	case "down", "j":
		l.cursor++
		if l.cursor >= len(l.vis)-1 {
			l.follow = true
		}
	case "pgup":
		l.follow = false
		l.cursor -= l.listHeight()
	case "pgdown":
		l.cursor += l.listHeight()
		if l.cursor >= len(l.vis)-1 {
			l.follow = true
		}
	case "home", "g":
		l.follow = false
		l.cursor = 0
	case "end", "G", " ":
		l.follow = !l.follow || k.String() != " "
		if l.follow {
			l.cursor = len(l.vis) - 1
		}
	case "/":
		l.filterOn = true
	case "c":
		l.entries, l.vis = nil, nil
		l.cursor, l.offset = 0, 0
		l.follow = true
	case "?", "f1":
		m.vp.SetContent(helpText)
		m.vp.GotoTop()
		m.vp.SetXOffset(0)
		m.prevScr, m.scr = scrLive, scrHelp
	case "enter":
		if e := l.selected(); e != nil {
			m.showText(eventTitle(*e), eventText(*e, m.width), helpLine("esc", "back to the live view", "↑↓ ←→", "scroll"))
		}
	case "p":
		subject := ""
		if e := l.selected(); e != nil && e.Kind == "msg" {
			subject = e.Subject
		}
		return m, m.publish(subject)
	case "m":
		return m, m.toggleMouse()
	}
	l.clamp()
	return m, nil
}

// updateLiveEvents appends a batch to the live screen and waits for the
// next one; batches for a screen that was closed are dropped.
func (m *Model) updateLiveEvents(msg liveMsg) tea.Cmd {
	if m.lv == nil || msg.lv != m.lv {
		return nil
	}
	m.lv.add(msg.evs)
	if msg.done {
		m.lv.ended = true
		return nil
	}
	return m.lv.wait()
}

// stopLive ends the subscription of the live screen, if any.
func (m *Model) stopLive() {
	if m.lv != nil {
		m.lv.src.Stop()
		m.lv.ended = true
	}
}

// openLive starts a subscription and shows the live screen for it.
func (m *Model) openLive(title, desc, command string, start func() (*cli.Live, error)) tea.Cmd {
	m.stopLive()
	m.lvBack = m.scr
	if m.lvBack == scrLive {
		m.lvBack = scrMain
	}
	w, h := m.width, m.height
	return m.busy("Subscribing…", func() tea.Msg {
		src, err := start()
		if err != nil {
			return liveOpenMsg{err: err}
		}
		return liveOpenMsg{lv: newLive(title, desc, command, src, w, h)}
	})
}

func eventTitle(e cli.Event) string {
	switch e.Kind {
	case "msg":
		return "Message on " + e.Subject
	case "put", "delete", "purge":
		return fmt.Sprintf("%s %s (revision %d)", strings.ToUpper(e.Kind), e.Subject, e.Seq)
	case "object":
		return "Object " + e.Subject
	}
	return e.Subject
}

// eventText renders one event in full: time, subject, reply, headers and
// the body (pretty-printed when it is JSON, hex-dumped when binary).
func eventText(e cli.Event, width int) string {
	return eventTextWhen(e, width, "Received")
}

func eventTextWhen(e cli.Event, width int, when string) string {
	d := &detailWriter{width: width}
	d.section("Message")
	d.row(when, e.At.Local().Format("2006-01-02 15:04:05.000000"))
	d.row("Subject", e.Subject)
	if e.Reply != "" {
		d.row("Reply to", e.Reply)
	}
	if e.Seq > 0 {
		d.row("Sequence", fmt.Sprint(e.Seq))
	}
	d.row("Size", fmt.Sprintf("%d bytes", len(e.Data)))
	if e.Text != "" {
		d.row("Note", e.Text)
	}
	if len(e.Header) > 0 {
		d.section("Headers")
		for _, k := range sortedKeys(e.Header) {
			d.rows(k, e.Header[k])
		}
	}
	d.section("Body")
	return d.String() + bodyText(e.Data)
}

// bodyText renders a payload: JSON indented, text as is, binary as hex.
func bodyText(data []byte) string {
	if len(data) == 0 {
		return styleMuted.Render("(empty)") + "\n"
	}
	if json.Valid(data) && (data[0] == '{' || data[0] == '[') {
		var out bytes.Buffer
		if json.Indent(&out, data, "", "  ") == nil {
			return out.String() + "\n"
		}
	}
	if utf8.Valid(data) && bytes.IndexByte(data, 0) < 0 {
		return string(data) + "\n"
	}
	var b strings.Builder
	for i := 0; i < len(data); i += 16 {
		end := min(i+16, len(data))
		fmt.Fprintf(&b, "%08x  ", i)
		for j := i; j < i+16; j++ {
			if j < end {
				fmt.Fprintf(&b, "%02x ", data[j])
			} else {
				b.WriteString("   ")
			}
			if j == i+7 {
				b.WriteString(" ")
			}
		}
		b.WriteString(" |")
		for j := i; j < end; j++ {
			c := data[j]
			if c < 32 || c > 126 {
				c = '.'
			}
			b.WriteByte(c)
		}
		b.WriteString("|\n")
	}
	return b.String()
}

// messageText renders a stored message the same way.
func messageText(msg cli.Message, width int) string {
	return eventTextWhen(cli.Event{At: msg.Time, Kind: "msg", Subject: msg.Subject, Reply: msg.Reply, Header: msg.Header, Data: msg.Data, Seq: msg.Seq}, width, "Stored")
}

var _ = time.Now
