package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nats-io/nats.go/micro"

	"github.com/olgeni/nats-tui/cli"
	"github.com/olgeni/nats-tui/internal/testnats"
)

func k(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "space", " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// testModel starts a throwaway server with a stream, a consumer, a bucket
// and an object store, and returns a loaded model.
func testModel(t *testing.T) (*Model, cli.Exec) {
	t.Helper()
	s, x := testnats.Setup(t)
	testnats.Must(t, x, "stream", "add", "ORDERS", "--subjects=orders.>", "--description=all the orders", "--max-age=1h", "--defaults")
	testnats.Must(t, x, "consumer", "add", "ORDERS", "worker", "--pull", "--filter=orders.new", "--defaults")
	testnats.Must(t, x, "pub", "orders.new", "first", "--count=3")
	testnats.Must(t, x, "kv", "add", "CONFIG", "--history=3")
	testnats.Must(t, x, "kv", "put", "CONFIG", "app.name", "demo")
	testnats.Must(t, x, "object", "add", "FILES")
	m := New(s)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	c, err := cli.Connect(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	st, err := c.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	m.Update(loadedMsg{client: c, store: st})
	return m, x
}

func press(m *Model, keys ...string) tea.Cmd {
	var cmd tea.Cmd
	for _, key := range keys {
		_, cmd = m.Update(k(key))
	}
	return cmd
}

// runCmd executes a command and feeds its message back, following busy
// screens until something settles.
func runCmd(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	for i := 0; cmd != nil && i < 10; i++ {
		msg := cmd()
		if msg == nil {
			return
		}
		if b, ok := msg.(tea.BatchMsg); ok {
			for _, c := range b {
				runCmd(t, m, c)
			}
			return
		}
		_, cmd = m.Update(msg)
		if m.scr == scrLive {
			return // the next command waits for messages: the test feeds them itself
		}
	}
}

func TestMainScreen(t *testing.T) {
	m, _ := testModel(t)
	v := m.View()
	for _, want := range []string{"NATS context test", "ORDERS", "CONFIG", "FILES", "1 streams, 1 consumers, 1 buckets, 1 object stores", "orders.>", "all the orders", "JetStream:"} {
		if !strings.Contains(v, want) {
			t.Errorf("main view lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "worker") {
		t.Error("consumers shown before expanding")
	}
	// context, Streams, ORDERS
	press(m, "down", "down")
	if n := m.selected(); n.kind != kStream || n.stream.Name() != "ORDERS" {
		t.Fatalf("selected %v %s", n.kind, n.name())
	}
	press(m, "right")
	v = m.View()
	if !strings.Contains(v, "worker") || !strings.Contains(v, "3 pend") {
		t.Errorf("consumer not shown after expand:\n%s", v)
	}
	press(m, "left")
	if strings.Contains(m.View(), "worker") {
		t.Error("collapse failed")
	}
	press(m, "/", "c", "o", "n", "f")
	v = m.View()
	if !strings.Contains(v, "CONFIG") || strings.Contains(v, "ORDERS") {
		t.Errorf("filter:\n%s", v)
	}
	press(m, "esc")
	if m.filter != "" {
		t.Error("esc did not clear the filter")
	}
	press(m, "?")
	if m.scr != scrHelp || !strings.Contains(m.View(), "Main screen") {
		t.Error("help")
	}
	press(m, "esc", "h")
	if m.scr != scrKeys {
		t.Error("keys")
	}
	press(m, "esc")
	if m.scr != scrMain {
		t.Error("back to main")
	}
}

func TestDetailsAndJSON(t *testing.T) {
	m, _ := testModel(t)
	press(m, "down", "down", "enter")
	if m.scr != scrDetails {
		t.Fatal("details")
	}
	if !strings.Contains(m.View(), "stream ORDERS") {
		t.Errorf("details title:\n%s", m.View())
	}
	v := m.detailText()
	for _, want := range []string{"all the orders", "orders.>", "Max age", "1h", "Consumers (1)", "worker"} {
		if !strings.Contains(v, want) {
			t.Errorf("details lack %q:\n%s", want, v)
		}
	}
	press(m, "J")
	if !strings.Contains(m.View(), `"max_age"`) {
		t.Error("json view")
	}
	press(m, "esc")
	if m.scr != scrMain {
		t.Error("back")
	}
	// the context row
	press(m, "up", "up", "enter")
	if v := m.detailText(); !strings.Contains(v, "throwaway test server") || !strings.Contains(v, "Connection") {
		t.Errorf("context details:\n%s", v)
	}
	press(m, "esc")
}

func TestEditStreamBuildsPlanAndRuns(t *testing.T) {
	m, _ := testModel(t)
	press(m, "down", "down", "e")
	if m.scr != scrEditor {
		t.Fatal("editor")
	}
	ed := m.editor
	for ed.row().key != "max_msgs" {
		ed.move(1)
	}
	press(m, "enter", "backspace", "backspace", "5", "0", "0", "enter")
	for ed.row().key != "deny_purge" {
		ed.move(1)
	}
	press(m, " ")
	press(m, "ctrl+s")
	if m.scr != scrPlan {
		t.Fatalf("expected plan, got %v: %s", m.scr, m.errMsg)
	}
	v := m.View()
	for _, want := range []string{"nats --context test stream edit ORDERS", "--max-msgs=500", "--deny-purge", "-f"} {
		if !strings.Contains(v, want) {
			t.Errorf("plan lacks %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "--subjects") {
		t.Errorf("plan restates the subjects:\n%s", v)
	}
	runCmd(t, m, press(m, "enter"))
	if m.scr != scrResult {
		t.Fatalf("expected result, got %v", m.scr)
	}
	if !strings.Contains(m.View(), "✓ 1 command(s) ran") {
		t.Errorf("result:\n%s", m.View())
	}
	runCmd(t, m, press(m, "enter"))
	if m.scr != scrMain {
		t.Fatalf("expected main after the reload, got %v", m.scr)
	}
	st := m.store.Stream("ORDERS")
	if st.Info.Config.MaxMsgs != 500 || !st.Info.Config.DenyPurge {
		t.Errorf("not applied: %+v", st.Info.Config)
	}
	// editing again with no change says so
	press(m, "e", "ctrl+s")
	if m.scr != scrMain || m.status != "Nothing to change" {
		t.Errorf("no change: scr %v status %q err %q", m.scr, m.status, m.errMsg)
	}
}

func TestAddConsumerAndBucket(t *testing.T) {
	m, _ := testModel(t)
	press(m, "down", "down", "a")
	if m.scr != scrEditor {
		t.Fatal("consumer editor")
	}
	ed := m.editor
	press(m, "enter")
	for _, r := range "reader" {
		press(m, string(r))
	}
	press(m, "enter")
	for ed.row().key != "filter" {
		ed.move(1)
	}
	press(m, "enter")
	for _, r := range "orders.paid" {
		press(m, string(r))
	}
	press(m, "enter", "ctrl+s")
	if m.scr != scrPlan {
		t.Fatalf("expected plan, got %v: %s", m.scr, m.errMsg)
	}
	if v := m.View(); !strings.Contains(v, "consumer add ORDERS reader --pull") || !strings.Contains(v, "--filter=orders.paid") {
		t.Errorf("plan:\n%s", v)
	}
	runCmd(t, m, press(m, "enter"))
	runCmd(t, m, press(m, "enter"))
	if len(m.store.Stream("ORDERS").Consumers) != 2 {
		t.Errorf("consumer not added: %d", len(m.store.Stream("ORDERS").Consumers))
	}
	// a bucket from the section row
	for m.selected().kind != kSection || m.selected().sec != secKV {
		press(m, "down")
	}
	press(m, "a")
	if m.scr != scrEditor {
		t.Fatal("bucket editor")
	}
	press(m, "enter")
	for _, r := range "SETTINGS" {
		press(m, string(r))
	}
	press(m, "enter", "ctrl+s")
	if m.scr != scrPlan || !strings.Contains(m.View(), "kv add SETTINGS --storage=file") {
		t.Fatalf("bucket plan: %v\n%s", m.scr, m.View())
	}
	runCmd(t, m, press(m, "enter"))
	runCmd(t, m, press(m, "enter"))
	if m.store.KV("SETTINGS") == nil {
		t.Error("bucket not added")
	}
}

func TestMessagesAndKeysTables(t *testing.T) {
	m, _ := testModel(t)
	press(m, "down", "down")
	runCmd(t, m, press(m, "v"))
	if m.scr != scrTable || m.tbl.kind != tkMessages {
		t.Fatalf("messages table: %v %s", m.scr, m.errMsg)
	}
	if v := m.View(); !strings.Contains(v, "orders.new") || !strings.Contains(v, "first") || len(m.tbl.rows) != 3 {
		t.Errorf("messages:\n%s", v)
	}
	press(m, "enter")
	if m.scr != scrText || !strings.Contains(m.View(), "Stored") {
		t.Errorf("message text: %v\n%s", m.scr, m.View())
	}
	press(m, "esc")
	// delete one through the plan, the table refreshes
	press(m, "d")
	if m.scr != scrPlan || !strings.Contains(m.View(), "stream rmm ORDERS 3 -f") {
		t.Fatalf("delete plan:\n%s", m.View())
	}
	runCmd(t, m, press(m, "enter"))
	runCmd(t, m, press(m, "enter"))
	if m.scr != scrTable || len(m.tbl.rows) != 2 {
		t.Errorf("after delete: %v rows %d", m.scr, len(m.tbl.rows))
	}
	press(m, "esc")
	// subjects
	runCmd(t, m, press(m, "t"))
	if m.scr != scrTable || m.tbl.kind != tkSubjects || !strings.Contains(m.View(), "orders.new") {
		t.Errorf("subjects:\n%s", m.View())
	}
	press(m, "esc")
	// keys of the bucket
	for m.selected().kind != kKV {
		press(m, "down")
	}
	runCmd(t, m, press(m, "K"))
	if m.scr != scrTable || m.tbl.kind != tkKeys || !strings.Contains(m.View(), "app.name") {
		t.Errorf("keys:\n%s", m.View())
	}
	press(m, "a")
	press(m, "enter")
	for _, r := range "app.env" {
		press(m, string(r))
	}
	press(m, "enter", "down", "enter")
	for _, r := range "prod" {
		press(m, string(r))
	}
	press(m, "enter", "ctrl+s")
	if m.scr != scrPlan || !strings.Contains(m.View(), "kv put CONFIG app.env") {
		t.Fatalf("put plan:\n%s", m.View())
	}
	runCmd(t, m, press(m, "enter"))
	runCmd(t, m, press(m, "enter"))
	if m.scr != scrTable || len(m.tbl.rows) != 2 {
		t.Errorf("after put: %v rows %d", m.scr, len(m.tbl.rows))
	}
	// editing an existing key carries the revision it was read at
	press(m, "e")
	if m.scr != scrEditor {
		t.Fatalf("edit key: %v", m.scr)
	}
	press(m, "ctrl+s")
	if m.scr != scrPlan || !strings.Contains(m.View(), "kv update CONFIG app.env prod 2") {
		t.Fatalf("update plan:\n%s", m.View())
	}
	runCmd(t, m, press(m, "enter"))
	runCmd(t, m, press(m, "enter"))
	if m.scr != scrTable || len(m.tbl.rows) != 2 || !strings.Contains(m.View(), " 3 ") {
		t.Errorf("after update: %v rows %d\n%s", m.scr, len(m.tbl.rows), m.View())
	}
	runCmd(t, m, press(m, "h"))
	if m.scr != scrTable || m.tbl.kind != tkHistory {
		t.Errorf("history: %v", m.scr)
	}
}

func TestLiveSubscribe(t *testing.T) {
	m, x := testModel(t)
	press(m, "down", "down", "s")
	if m.scr != scrEditor || m.editor.get("subjects").text != "orders.>" {
		t.Fatalf("subscribe editor: %v %q", m.scr, m.editor.get("subjects").text)
	}
	runCmd(t, m, press(m, "ctrl+s"))
	if m.scr != scrLive {
		t.Fatalf("live screen: %v %s", m.scr, m.errMsg)
	}
	wait := m.lv.wait()
	testnats.Must(t, x, "pub", "orders.live", "ping", "-H", "X-Live:1")
	done := make(chan tea.Msg, 1)
	go func() { done <- wait() }()
	select {
	case msg := <-done:
		m.Update(msg)
	case <-time.After(5 * time.Second):
		t.Fatal("no live message")
	}
	v := m.View()
	if !strings.Contains(v, "orders.live") || !strings.Contains(v, "ping") || !strings.Contains(v, "1 received") {
		t.Errorf("live view:\n%s", v)
	}
	press(m, "enter")
	if m.scr != scrText || !strings.Contains(m.View(), "X-Live") {
		t.Errorf("live entry:\n%s", m.View())
	}
	press(m, "esc")
	press(m, "esc")
	if m.scr != scrMain || !m.lv.ended {
		t.Errorf("after esc: %v ended %v", m.scr, m.lv.ended)
	}
}

func TestContextsTable(t *testing.T) {
	m, _ := testModel(t)
	press(m, "C")
	if m.scr != scrTable || m.tbl.kind != tkContexts {
		t.Fatal("contexts table")
	}
	if v := m.View(); !strings.Contains(v, "test") || !strings.Contains(v, "*●") || !strings.Contains(v, "throwaway test server") {
		t.Errorf("contexts:\n%s", v)
	}
	press(m, "a")
	if m.scr != scrEditor {
		t.Fatal("context editor")
	}
	press(m, "enter")
	for _, r := range "other" {
		press(m, string(r))
	}
	press(m, "enter", "ctrl+s")
	if m.scr != scrPlan || !strings.Contains(m.View(), "context add other") {
		t.Fatalf("context plan:\n%s", m.View())
	}
	runCmd(t, m, press(m, "enter"))
	runCmd(t, m, press(m, "enter"))
	if m.scr != scrTable || len(m.tbl.rows) != 2 {
		t.Errorf("after add: %v rows %d", m.scr, len(m.tbl.rows))
	}
}

func TestPublishAndRequestPlans(t *testing.T) {
	m, _ := testModel(t)
	press(m, "down", "down", "p")
	if m.scr != scrEditor || m.editor.get("subject").text != "orders." {
		t.Fatalf("publish editor: %q", m.editor.get("subject").text)
	}
	press(m, "enter")
	for _, r := range "test" {
		press(m, string(r))
	}
	press(m, "enter", "down", "enter")
	for _, r := range "hello" {
		press(m, string(r))
	}
	press(m, "enter", "ctrl+s")
	if m.scr != scrPlan || !strings.Contains(m.View(), "pub orders.test hello") {
		t.Fatalf("publish plan:\n%s", m.View())
	}
	runCmd(t, m, press(m, "enter"))
	if m.scr != scrResult || !strings.Contains(m.View(), "Published") {
		t.Errorf("publish result:\n%s", m.View())
	}
	runCmd(t, m, press(m, "enter"))
	if n := m.store.Stream("ORDERS").Info.State.Msgs; n != 4 {
		t.Errorf("messages after publish: %d", n)
	}
}

func TestNoConnection(t *testing.T) {
	m := New(cli.Settings{Server: "nats://127.0.0.1:1"})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(loadedMsg{err: errString("connect to nats://127.0.0.1:1: no servers available")})
	if v := m.View(); !strings.Contains(v, "Cannot connect") || !strings.Contains(v, "no servers available") {
		t.Errorf("no connection view:\n%s", v)
	}
	press(m, "q")
	if !m.quitting {
		t.Error("q should quit")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestValidSchedule(t *testing.T) {
	good := map[string]string{"at": "2026-09-03T18:00:00Z", "after": "10m", "every": "1h30m", "cron": "0 */5 * * * *"}
	for k, v := range good {
		if err := validSchedule(k, v); err != nil {
			t.Errorf("%s %q: %v", k, v, err)
		}
	}
	bad := map[string]string{"at": "tomorrow", "after": "soon", "every": "", "cron": "*/5 * * * *"}
	for k, v := range bad {
		if err := validSchedule(k, v); err == nil {
			t.Errorf("%s %q accepted", k, v)
		}
	}
}

func TestCopyStreamNeedsSubjects(t *testing.T) {
	m, _ := testModel(t)
	press(m, "down", "down", "y")
	if m.scr != scrEditor || m.editor.get("name").text != "ORDERS_COPY" {
		t.Fatalf("copy editor: scr %v", m.scr)
	}
	press(m, "ctrl+s")
	if m.scr != scrEditor || !strings.Contains(m.errMsg, "subjects of its own") {
		t.Fatalf("blank subjects accepted: scr %v err %q", m.scr, m.errMsg)
	}
	ed := m.editor
	for ed.row().key != "subjects" {
		ed.move(1)
	}
	press(m, "enter")
	for _, r := range "orders2.>" {
		press(m, string(r))
	}
	press(m, "enter", "ctrl+s")
	if v := m.View(); m.scr != scrPlan || !strings.Contains(v, "stream copy ORDERS ORDERS_COPY") || !strings.Contains(v, "--subjects=orders2.>") {
		t.Fatalf("copy plan:\n%s", m.View())
	}
}

func TestLazyConsumersAndObjects(t *testing.T) {
	s, x := testnats.Setup(t)
	testnats.Must(t, x, "stream", "add", "ORDERS", "--subjects=orders.>", "--defaults")
	testnats.Must(t, x, "consumer", "add", "ORDERS", "worker", "--pull", "--defaults")
	testnats.Must(t, x, "object", "add", "FILES")
	m := New(s)
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	c, err := cli.Connect(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	st, err := c.Load(cli.LoadOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Stream("ORDERS").Loaded || st.Object("FILES").Loaded || st.ConsumerCount() != 1 {
		t.Fatalf("lazy load: loaded=%v/%v consumers=%d", st.Stream("ORDERS").Loaded, st.Object("FILES").Loaded, st.ConsumerCount())
	}
	m.Update(loadedMsg{client: c, store: st})
	if v := m.View(); !strings.Contains(v, "1 consumers") || strings.Contains(v, "worker") {
		t.Fatalf("before expanding:\n%s", v)
	}
	// expanding fetches the consumers
	press(m, "down", "down")
	runCmd(t, m, press(m, "right"))
	if !m.store.Stream("ORDERS").Loaded || !strings.Contains(m.View(), "worker") {
		t.Fatalf("after expanding:\n%s", m.View())
	}
	// a reload keeps them
	runCmd(t, m, m.load())
	if !m.store.Stream("ORDERS").Loaded || !strings.Contains(m.View(), "worker") {
		t.Fatalf("after reload:\n%s", m.View())
	}
	// the objects list when the store is opened
	press(m, "down", "down", "down", "down")
	if m.selected().kind != kObject {
		t.Fatalf("selected %v", m.selected().kind)
	}
	runCmd(t, m, press(m, "O"))
	if m.scr != scrTable || !m.store.Object("FILES").Loaded {
		t.Fatalf("objects table: scr %v loaded %v", m.scr, m.store.Object("FILES").Loaded)
	}
}

// update is Update returning only the command.
func (m *Model) update(msg tea.Msg) tea.Cmd {
	_, cmd := m.Update(msg)
	return cmd
}

func TestAutoRefresh(t *testing.T) {
	m, _ := testModel(t)
	if m.refresh != 0 {
		t.Fatal("refresh on by default")
	}
	press(m, "ctrl+r")
	if m.refresh != 5*time.Second || !strings.Contains(m.View(), "Refresh: every 5s") {
		t.Fatalf("refresh %v:\n%s", m.refresh, m.View())
	}
	// a tick of the current setting starts a reload on the main screen
	// (the command is not run: it carries the next 5s timer)
	m.loading = false
	if cmd := m.update(refreshMsg{gen: m.refreshGen}); cmd == nil || !m.loading {
		t.Error("tick did not start a reload")
	}
	// not while a reload runs, not in an editor
	if cmd := m.update(refreshMsg{gen: m.refreshGen}); cmd == nil {
		t.Error("tick while loading dropped the timer")
	}
	m.loading = false
	press(m, "A")
	if cmd := m.update(refreshMsg{gen: m.refreshGen}); cmd == nil || m.loading {
		t.Errorf("tick in the editor: cmd %v loading %v", cmd != nil, m.loading)
	}
	press(m, "esc")
	// a stale tick is ignored, and off is off
	press(m, "ctrl+r")
	if m.refresh != 0 || strings.Contains(m.View(), "Refresh:") {
		t.Fatalf("refresh off: %v", m.refresh)
	}
	if cmd := m.update(refreshMsg{gen: m.refreshGen - 1}); cmd != nil {
		t.Error("stale tick scheduled something")
	}
}

func TestSubjectClashInEditors(t *testing.T) {
	m, _ := testModel(t)
	// a new stream on a subject ORDERS holds
	press(m, "A")
	ed := m.editor
	for ed.row().key != "name" {
		ed.move(1)
	}
	press(m, "enter")
	for _, r := range "NEW" {
		press(m, string(r))
	}
	press(m, "enter")
	for ed.row().key != "subjects" {
		ed.move(1)
	}
	press(m, "enter")
	for _, r := range "orders.new" {
		press(m, string(r))
	}
	press(m, "enter", "ctrl+s")
	if m.scr != scrEditor || !strings.Contains(m.errMsg, "overlaps orders.> of stream ORDERS") {
		t.Fatalf("clash: scr %v err %q", m.scr, m.errMsg)
	}
	press(m, "esc")
	// editing ORDERS itself keeps its own subjects
	press(m, "down", "down", "e")
	press(m, "ctrl+s")
	if m.scr != scrMain || m.errMsg != "" {
		t.Fatalf("edit unchanged: scr %v err %q", m.scr, m.errMsg)
	}
}

func TestConsumerFromMessages(t *testing.T) {
	m, _ := testModel(t)
	press(m, "down", "down")
	runCmd(t, m, press(m, "v"))
	if m.scr != scrTable {
		t.Fatalf("messages: scr %v", m.scr)
	}
	runCmd(t, m, press(m, "a"))
	if m.scr != scrEditor || m.editor.get("filter").text != "orders.new" {
		t.Fatalf("consumer editor: scr %v filter %q", m.scr, m.editor.get("filter").text)
	}
	press(m, "enter")
	for _, r := range "newonly" {
		press(m, string(r))
	}
	press(m, "enter", "ctrl+s")
	if v := m.View(); m.scr != scrPlan || !strings.Contains(v, "consumer add ORDERS newonly") || !strings.Contains(v, "--filter=orders.new") {
		t.Fatalf("plan:\n%s", v)
	}
}

func TestServiceDetailsShowStats(t *testing.T) {
	m, _ := testModel(t)
	nc := m.client.NC
	svc, err := micro.AddService(nc, micro.Config{
		Name:        "echo",
		Version:     "1.2.3",
		Description: "answers what it is asked",
		Endpoint: &micro.EndpointConfig{
			Subject: "echo.req",
			Handler: micro.HandlerFunc(func(r micro.Request) { r.Respond(r.Data()) }),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()
	if _, err := nc.Request("echo.req", []byte("hi"), 2*time.Second); err != nil {
		t.Fatal(err)
	}
	// discovery runs on load: the service was not there the first time
	st, err := m.client.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	m.Update(loadedMsg{store: st})
	m.cursor = -1
	for i, n := range m.rows {
		if n.kind == kService {
			m.cursor = i
		}
	}
	if m.cursor < 0 {
		t.Fatalf("no service row:\n%s", m.View())
	}
	cmd := press(m, "enter")
	if m.scr != scrDetails || cmd == nil {
		t.Fatalf("details: %v cmd %v", m.scr, cmd != nil)
	}
	if v := m.View(); !strings.Contains(v, "asking the instance") {
		t.Errorf("before the stats arrive:\n%s", v)
	}
	runCmd(t, m, cmd)
	v := m.View()
	for _, want := range []string{"echo", "1.2.3", "echo.req", "Statistics", "Started", "1 requests, 0 errors"} {
		if !strings.Contains(v, want) {
			t.Errorf("details miss %q:\n%s", want, v)
		}
	}
	// the answer is kept: opening the details again asks nothing
	press(m, "esc")
	if cmd := press(m, "enter"); cmd != nil {
		t.Error("stats asked twice")
	}
}

func TestMonitoringChecksAndReports(t *testing.T) {
	m, _ := testModel(t)
	press(m, "down", "down") // the stream
	if m.selected().kind != kStream {
		t.Fatalf("not on the stream: %v", m.selected().kind)
	}
	// the check of the selected stream comes first in the menu
	press(m, "M")
	if m.scr != scrPicker || !strings.Contains(m.View(), "server check stream ORDERS") {
		t.Fatalf("monitor menu:\n%s", m.View())
	}
	runCmd(t, m, press(m, "enter"))
	if m.scr != scrText || !strings.Contains(m.View(), "ORDERS: OK") {
		t.Fatalf("stream check: %v %s\n%s", m.scr, m.errMsg, m.View())
	}
	press(m, "esc")
	// the same on its consumer
	press(m, "right", "down")
	if m.selected().kind != kConsumer {
		t.Fatalf("not on the consumer: %v", m.selected().kind)
	}
	press(m, "M")
	if !strings.Contains(m.View(), "server check consumer worker") {
		t.Fatalf("monitor menu on a consumer:\n%s", m.View())
	}
	runCmd(t, m, press(m, "enter"))
	if m.scr != scrText || !strings.Contains(m.View(), "ORDERS_worker: OK") {
		t.Fatalf("consumer check: %v %s\n%s", m.scr, m.errMsg, m.View())
	}
	press(m, "esc")
	// a mapping is tried through an editor
	press(m, "M")
	for _, r := range "mappings" {
		press(m, string(r))
	}
	press(m, "enter")
	if m.scr != scrEditor {
		t.Fatalf("mapping editor: %v", m.scr)
	}
	runCmd(t, m, press(m, "ctrl+s"))
	if m.scr != scrText || !strings.Contains(m.View(), "new.paris") {
		t.Fatalf("mapping: %v %s\n%s", m.scr, m.errMsg, m.View())
	}
	press(m, "esc")
	// gaps of the stream the consumer belongs to
	press(m, "T")
	for _, r := range "gaps" {
		press(m, string(r))
	}
	runCmd(t, m, press(m, "enter"))
	if m.scr != scrText || !strings.Contains(m.View(), "No deleted messages in ORDERS") {
		t.Fatalf("gaps: %v %s\n%s", m.scr, m.errMsg, m.View())
	}
	press(m, "esc")
	// consumer find with its default flag
	press(m, "T")
	for _, r := range "consumer find" {
		press(m, string(r))
	}
	press(m, "enter")
	if m.scr != scrForm {
		t.Fatalf("consumer find form: %v", m.scr)
	}
	runCmd(t, m, press(m, "enter"))
	if m.scr != scrText || !strings.Contains(m.View(), "worker") {
		t.Fatalf("consumer find: %v %s\n%s", m.scr, m.errMsg, m.View())
	}
}

func TestAccountBackupAndConsumerCopyKeys(t *testing.T) {
	m, _ := testModel(t)
	// b on the context row backs up the account
	press(m, "b")
	if m.scr != scrEditor || !strings.Contains(m.View(), "every stream of the account") {
		t.Fatalf("account backup editor: %v\n%s", m.scr, m.View())
	}
	press(m, "esc")
	// y on a consumer copies it
	press(m, "down", "down", "right", "down")
	if m.selected().kind != kConsumer {
		t.Fatalf("not on the consumer: %v", m.selected().kind)
	}
	press(m, "y")
	if m.scr != scrEditor || m.editor.get("name").text != "worker_COPY" {
		t.Fatalf("consumer copy editor: %v\n%s", m.scr, m.View())
	}
	press(m, "ctrl+s")
	if m.scr != scrPlan || !strings.Contains(m.View(), "consumer copy ORDERS worker worker_COPY") {
		t.Fatalf("copy plan:\n%s", m.View())
	}
	runCmd(t, m, press(m, "enter"))
	runCmd(t, m, press(m, "enter"))
	if m.scr != scrMain || m.store.Stream("ORDERS").ConsumerCount() != 2 {
		t.Errorf("after copy: %v consumers %d", m.scr, m.store.Stream("ORDERS").ConsumerCount())
	}
}

func TestClusterMenuResetsConsumer(t *testing.T) {
	m, _ := testModel(t)
	press(m, "down", "down", "right", "down")
	if m.selected().kind != kConsumer {
		t.Fatalf("not on the consumer: %v", m.selected().kind)
	}
	press(m, "L")
	if m.scr != scrPicker || !strings.Contains(m.View(), "consumer reset worker") {
		t.Fatalf("cluster menu:\n%s", m.View())
	}
	for _, r := range "reset" {
		press(m, string(r))
	}
	press(m, "enter")
	if m.scr != scrForm {
		t.Fatalf("reset form: %v", m.scr)
	}
	press(m, "1")
	runCmd(t, m, press(m, "enter"))
	if m.scr != scrPlan || !strings.Contains(m.View(), "consumer reset ORDERS worker --sequence=1 -f") {
		t.Fatalf("reset plan:\n%s", m.View())
	}
	runCmd(t, m, press(m, "enter"))
	runCmd(t, m, press(m, "enter"))
	if m.scr != scrMain || m.errMsg != "" {
		t.Errorf("after reset: %v %s", m.scr, m.errMsg)
	}
	// the server commands are offered on any row, with the server ID filled in
	press(m, "L")
	for _, r := range "kick" {
		press(m, string(r))
	}
	press(m, "enter")
	if m.scr != scrEditor || m.editor.get("server").text != m.store.Server.ID {
		t.Fatalf("kick editor: %v %q", m.scr, m.editor.get("server").text)
	}
}

func TestTextScrollsSideways(t *testing.T) {
	m := New(cli.Settings{})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	// the marker sits at column 100: off screen until the text scrolls right
	m.showText("wide", "short\n"+strings.Repeat(" ", 100)+"MARK\nshort", "")
	if strings.Contains(m.View(), "MARK") {
		t.Fatalf("the marker should start off screen:\n%s", m.View())
	}
	press(m, "right", "right")
	if v := m.View(); !strings.Contains(v, "MARK") || !strings.Contains(v, "←→") {
		t.Errorf("after right:\n%s", v)
	}
	press(m, "left")
	if strings.Contains(m.View(), "MARK") {
		t.Errorf("after left:\n%s", m.View())
	}
	// a new text starts at the left edge again
	press(m, "right", "right", "right")
	m.showText("other", strings.Repeat(" ", 100)+"MARK", "")
	if strings.Contains(m.View(), "MARK") {
		t.Errorf("new text:\n%s", m.View())
	}
}
