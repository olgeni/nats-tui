package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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
	st, err := c.Load()
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
	press(m, "down", "down", "P")
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
