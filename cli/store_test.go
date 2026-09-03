package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/olgeni/nats-tui/cli"
	"github.com/olgeni/nats-tui/internal/testnats"
)

// The server-backed tests start a throwaway nats-server, run the plans the
// editors would produce through the real nats CLI, and read the result
// back with the client library — the same round trip the TUI makes.

func connect(t *testing.T, s cli.Settings) *cli.Client {
	t.Helper()
	c, err := cli.Connect(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func run(t *testing.T, x cli.Exec, p *cli.Plan) {
	t.Helper()
	res := p.Execute(x, nil)
	if cli.Failed(res) {
		t.Fatalf("%s: %s\n%s", p.Title, strings.Join(res[len(res)-1].Args, " "), res[len(res)-1].Output())
	}
}

func TestLoadEmpty(t *testing.T) {
	s, _ := testnats.Setup(t)
	c := connect(t, s)
	st, err := c.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if st.ContextName != "test" || st.Selected != "test" || st.Context.Description != "throwaway test server" {
		t.Errorf("context: %+v selected %q", st.Context, st.Selected)
	}
	if !st.HasJetStream() || len(st.Streams) != 0 || len(st.KVs) != 0 || len(st.Objects) != 0 {
		t.Errorf("empty server: js=%v streams=%d kv=%d obj=%d (%s)", st.HasJetStream(), len(st.Streams), len(st.KVs), len(st.Objects), st.JSError)
	}
	if st.Server.Version == "" || st.Server.MaxPayload <= 0 {
		t.Errorf("server info: %+v", st.Server)
	}
	d := cli.BuildDump(st)
	if tree := d.Tree(time.Now()); !strings.Contains(tree, "streams (0)") {
		t.Errorf("tree:\n%s", tree)
	}
}

func TestStreamRoundTrip(t *testing.T) {
	s, x := testnats.Setup(t)
	c := connect(t, s)
	spec := cli.NewStreamSpec()
	spec.Name, spec.Subjects, spec.MaxAge, spec.MaxMsgs, spec.Description = "ORDERS", []string{"orders.>", "shipments.*"}, "2h", 500, "orders and shipments"
	spec.DenyPurge, spec.Metadata, spec.Compression = true, []string{"team=ops"}, "s2"
	run(t, x, cli.AddStream(spec))
	st, err := c.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	stream := st.Stream("ORDERS")
	if stream == nil {
		t.Fatal("stream not found after add")
	}
	got := cli.StreamSpecFrom(stream.Info.Config)
	for _, chk := range []struct{ name, want, got string }{
		{"subjects", "orders.>, shipments.*", cli.JoinList(got.Subjects)},
		{"max age", "2h", got.MaxAge},
		{"max msgs", "500", cli.Limit(got.MaxMsgs)},
		{"description", "orders and shipments", got.Description},
		{"deny purge", "true", boolText(got.DenyPurge)},
		{"metadata", "team=ops", cli.JoinList(got.Metadata)},
		{"compression", "s2", got.Compression},
		{"storage", "file", got.Storage},
		{"dupe window", "2m", got.DupeWindow},
	} {
		if chk.got != chk.want {
			t.Errorf("%s: got %q, want %q", chk.name, chk.got, chk.want)
		}
	}
	// edit: only the changes are passed, and nats applies them
	cur := got
	cur.MaxMsgs, cur.Subjects, cur.DenyPurge, cur.MaxAge = -1, []string{"orders.>"}, false, ""
	cur.RepubDest = "repub.>"
	p := cli.EditStream(got, cur)
	line := strings.Join(p.Cmds[0].Args, " ")
	if !strings.Contains(line, "--max-msgs=-1") || !strings.Contains(line, "--subjects=orders.>") || !strings.Contains(line, "--max-age=0") || strings.Contains(line, "--description") {
		t.Errorf("edit flags: %s", line)
	}
	// deny purge cannot be lifted: the flag is dropped and a note says so
	if strings.Contains(line, "deny-purge") || len(p.Notes) != 1 {
		t.Errorf("deny purge: %s %v", line, p.Notes)
	}
	run(t, x, p)
	st, _ = c.LoadAll()
	got2 := cli.StreamSpecFrom(st.Stream("ORDERS").Info.Config)
	if got2.MaxMsgs != -1 || cli.JoinList(got2.Subjects) != "orders.>" || !got2.DenyPurge || got2.MaxAge != "" || got2.RepubDest != "repub.>" || got2.Description != "orders and shipments" {
		t.Errorf("after edit: %+v", got2)
	}
	if p := cli.EditStream(got2, got2); !p.Empty() {
		t.Errorf("re-reading yields a change: %v", p.Cmds)
	}
	// messages: publish, read back, purge, delete
	run(t, x, cli.Publish(cli.PublishSpec{Subject: "orders.new", Body: "order {{Count}}", Count: 5, Headers: []string{"X-Test:yes"}}))
	run(t, x, cli.Publish(cli.PublishSpec{Subject: "orders.multi", Body: "line one\nline two", Count: 1}))
	msgs, err := c.Messages("ORDERS", 1, 100, "")
	if err != nil || len(msgs) != 6 {
		t.Fatalf("messages: %d %v", len(msgs), err)
	}
	if msgs[0].Subject != "orders.new" || string(msgs[0].Data) != "order 1" || msgs[0].Header.Get("X-Test") != "yes" || msgs[0].Seq != 1 {
		t.Errorf("first message: %+v", msgs[0])
	}
	if string(msgs[5].Data) != "line one\nline two" {
		t.Errorf("stdin body: %q", msgs[5].Data)
	}
	only, err := c.Messages("ORDERS", 1, 100, "orders.multi")
	if err != nil || len(only) != 1 {
		t.Errorf("filtered messages: %d %v", len(only), err)
	}
	subs, err := c.Subjects("ORDERS", "")
	if err != nil || subs["orders.new"] != 5 || subs["orders.multi"] != 1 {
		t.Errorf("subjects: %v %v", subs, err)
	}
	// schedules: the stream must allow them (a flag that never comes off), then
	// the stored message carries the schedule headers
	sched := cli.NewStreamSpec()
	sched.Name, sched.Subjects, sched.AllowSchedules = "SCHED", []string{"sched.>"}, true
	if line := strings.Join(cli.AddStream(sched).Cmds[0].Args, " "); !strings.Contains(line, "--allow-schedules") {
		t.Errorf("add flags: %s", line)
	}
	run(t, x, cli.AddStream(sched))
	run(t, x, cli.Publish(cli.PublishSpec{Subject: "sched.in", Body: "tick", Count: 1, Schedule: "at", ScheduleValue: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), ScheduleDest: "sched.out"}))
	run(t, x, cli.Publish(cli.PublishSpec{Subject: "sched.later", Body: "tock", Count: 1, Schedule: "after", ScheduleValue: "2h", ScheduleDest: "sched.out"}))
	if m, err := c.GetMsg("SCHED", 1); err != nil || !strings.HasPrefix(m.Header.Get("Nats-Schedule"), "@at ") || m.Header.Get("Nats-Schedule-Target") != "sched.out" {
		t.Errorf("scheduled message: %+v %v", m, err)
	}
	if m, err := c.GetMsg("SCHED", 2); err != nil || !strings.HasPrefix(m.Header.Get("Nats-Schedule"), "@at ") || string(m.Data) != "tock" {
		t.Errorf("after message: %+v %v", m, err)
	}
	st, _ = c.LoadAll()
	got3 := cli.StreamSpecFrom(st.Stream("SCHED").Info.Config)
	if !got3.AllowSchedules {
		t.Errorf("allow schedules not read back: %+v", got3)
	}
	if p := cli.EditStream(got3, got3); !p.Empty() {
		t.Errorf("re-reading SCHED yields a change: %v", p.Cmds)
	}
	if m, err := c.GetMsg("ORDERS", 3); err != nil || string(m.Data) != "order 3" {
		t.Errorf("get msg: %+v %v", m, err)
	}
	run(t, x, cli.DeleteMessage("ORDERS", 2))
	st, _ = c.LoadAll()
	if n := st.Stream("ORDERS").Info.State.Msgs; n != 5 {
		t.Errorf("after deleting one message: %d messages", n)
	}
	// deny purge is on: nats refuses, the plan fails and says so
	if res := cli.PurgeStream("ORDERS", "orders.new", 0, 1).Execute(x, nil); !cli.Failed(res) || !strings.Contains(res[0].Output(), "purge") {
		t.Errorf("purge on a deny-purge stream: %v", res)
	}
	run(t, x, cli.CopyStream("ORDERS", "ORDERS2", []string{"orders2.>"}))
	run(t, x, cli.DeleteStream("ORDERS"))
	st, _ = c.LoadAll()
	if st.Stream("ORDERS") != nil || st.Stream("ORDERS2") == nil {
		t.Errorf("after copy and delete: %d streams", len(st.Streams))
	}
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestConsumerRoundTrip(t *testing.T) {
	s, x := testnats.Setup(t)
	c := connect(t, s)
	spec := cli.NewStreamSpec()
	spec.Name, spec.Subjects = "EVENTS", []string{"ev.>"}
	run(t, x, cli.AddStream(spec))
	run(t, x, cli.Publish(cli.PublishSpec{Subject: "ev.a", Body: "e{{Count}}", Count: 3}))
	cs := cli.NewConsumerSpec("EVENTS")
	cs.Name, cs.Filter, cs.MaxDeliver, cs.AckWait, cs.Description = "worker", []string{"ev.a"}, 3, "10s", "the worker"
	run(t, x, cli.AddConsumer(cs))
	push := cli.NewConsumerSpec("EVENTS")
	push.Name, push.Pull, push.Target, push.Ack = "pusher", false, "deliver.pusher", "none"
	run(t, x, cli.AddConsumer(push))
	st, err := c.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	stream := st.Stream("EVENTS")
	if stream == nil || len(stream.Consumers) != 2 {
		t.Fatalf("consumers: %+v", stream)
	}
	var worker, pusher *cli.Consumer
	for _, cn := range stream.Consumers {
		switch cn.Name() {
		case "worker":
			worker = cn
		case "pusher":
			pusher = cn
		}
	}
	if worker == nil || pusher == nil {
		t.Fatal("consumers not found")
	}
	got := cli.ConsumerSpecFrom("EVENTS", worker.Info.Config, worker.Info)
	if !got.Pull || cli.JoinList(got.Filter) != "ev.a" || got.MaxDeliver != 3 || got.AckWait != "10s" || got.Description != "the worker" || got.Ack != "explicit" || got.Deliver != "all" {
		t.Errorf("worker spec: %+v", got)
	}
	if worker.Info.NumPending != 3 {
		t.Errorf("pending: %d", worker.Info.NumPending)
	}
	if p := cli.ConsumerSpecFrom("EVENTS", pusher.Info.Config, pusher.Info); p.Pull || p.Target != "deliver.pusher" || p.Ack != "none" {
		t.Errorf("pusher spec: %+v", p)
	}
	cur := got
	cur.MaxDeliver, cur.Description = 7, "edited"
	p := cli.EditConsumer(got, cur)
	if len(p.Cmds) != 1 || !strings.Contains(strings.Join(p.Cmds[0].Args, " "), "--max-deliver=7") {
		t.Errorf("edit plan: %v", p.Cmds)
	}
	run(t, x, p)
	// next: fetch and ack one
	run(t, x, cli.NextMessages("EVENTS", "worker", 1, "ack"))
	st, _ = c.LoadAll()
	for _, cn := range st.Stream("EVENTS").Consumers {
		if cn.Name() == "worker" {
			if cn.Info.Config.MaxDeliver != 7 || cn.Info.Config.Description != "edited" {
				t.Errorf("after edit: %+v", cn.Info.Config)
			}
			if cn.Info.NumPending != 2 || cn.Info.AckFloor.Stream != 1 {
				t.Errorf("after next: pending %d ack floor %d", cn.Info.NumPending, cn.Info.AckFloor.Stream)
			}
		}
	}
	// pause and resume
	run(t, x, cli.PauseConsumer("EVENTS", "worker", time.Now().Add(time.Hour)))
	st, _ = c.LoadAll()
	paused := false
	for _, cn := range st.Stream("EVENTS").Consumers {
		if cn.Name() == "worker" {
			paused = cn.Info.Paused
		}
	}
	if !paused {
		t.Error("consumer not paused")
	}
	run(t, x, cli.ResumeConsumer("EVENTS", "worker"))
	run(t, x, cli.DeleteConsumer("EVENTS", "pusher"))
	st, _ = c.LoadAll()
	if len(st.Stream("EVENTS").Consumers) != 1 {
		t.Errorf("after delete: %d consumers", len(st.Stream("EVENTS").Consumers))
	}
}

func TestBucketsRoundTrip(t *testing.T) {
	s, x := testnats.Setup(t)
	c := connect(t, s)
	kv := cli.NewKVSpec()
	kv.Name, kv.History, kv.TTL, kv.Description = "CONFIG", 3, "1h", "settings"
	run(t, x, cli.AddKV(kv))
	run(t, x, cli.PutKey("CONFIG", "app.name", "demo"))
	run(t, x, cli.PutKey("CONFIG", "app.name", "demo v2"))
	run(t, x, cli.PutKey("CONFIG", "app.multi", "a\nb"))
	run(t, x, cli.CreateKey("CONFIG", "only.once", "1", ""))
	if res := cli.CreateKey("CONFIG", "only.once", "2", "").Execute(x, nil); !cli.Failed(res) {
		t.Error("create on an existing key should fail")
	}
	st, err := c.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	b := st.KV("CONFIG")
	if b == nil {
		t.Fatal("bucket not found")
	}
	got := cli.KVSpecFrom(b.Status, b.Info)
	if got.History != 3 || got.TTL != "1h" || got.Description != "settings" || got.Storage != "file" {
		t.Errorf("kv spec: %+v", got)
	}
	keys, err := c.Keys("CONFIG")
	if err != nil || len(keys) != 3 {
		t.Fatalf("keys: %v %v", keys, err)
	}
	if keys[1].Key != "app.name" || string(keys[1].Value) != "demo v2" || keys[1].Revision != 2 {
		t.Errorf("key: %+v", keys[1])
	}
	if string(keys[0].Value) != "a\nb" {
		t.Errorf("multi-line value: %q", keys[0].Value)
	}
	hist, err := c.History("CONFIG", "app.name")
	if err != nil || len(hist) != 2 || string(hist[0].Value) != "demo" {
		t.Errorf("history: %v %v", hist, err)
	}
	cur := got
	cur.History, cur.TTL = 5, ""
	p := cli.EditKV(got, cur)
	if strings.Join(p.Cmds[0].Args, " ") != "kv edit CONFIG --history=5 --ttl=0" {
		t.Errorf("kv edit: %v", p.Cmds[0].Args)
	}
	run(t, x, p)
	run(t, x, cli.RevertKey("CONFIG", "app.name", 1))
	run(t, x, cli.DeleteKey("CONFIG", "only.once"))
	st, _ = c.LoadAll()
	b = st.KV("CONFIG")
	if b.Status.History() != 5 || b.Status.TTL() != 0 {
		t.Errorf("after edit: history %d ttl %v", b.Status.History(), b.Status.TTL())
	}
	keys, _ = c.Keys("CONFIG")
	if len(keys) != 2 || string(keys[1].Value) != "demo" {
		t.Errorf("after revert and delete: %+v", keys)
	}
	// object store
	obj := cli.NewObjectSpec()
	obj.Name, obj.Description = "FILES", "uploads"
	run(t, x, cli.AddObjectStore(obj))
	dir := t.TempDir()
	file := filepath.Join(dir, "hello.txt")
	os.WriteFile(file, []byte("hello object"), 0o644)
	run(t, x, cli.PutObject("FILES", file, "hello.txt", "a greeting"))
	st, _ = c.LoadAll()
	ob := st.Object("FILES")
	if ob == nil || len(ob.Objects) != 1 || ob.Objects[0].Name != "hello.txt" || ob.Objects[0].Size != 12 || ob.Objects[0].Description != "a greeting" {
		t.Fatalf("objects: %+v", ob)
	}
	out := filepath.Join(dir, "out.txt")
	run(t, x, cli.GetObject("FILES", "hello.txt", out))
	if b, _ := os.ReadFile(out); string(b) != "hello object" {
		t.Errorf("get object: %q", b)
	}
	run(t, x, cli.EditObjectStore(cli.ObjectSpecFrom(ob.Status, ob.Info), cli.ObjectSpec{Name: "FILES", Description: "changed", Storage: "file", Replicas: 1, MaxBytes: -1}))
	run(t, x, cli.DeleteObject("FILES", "hello.txt"))
	st, _ = c.LoadAll()
	ob = st.Object("FILES")
	if ob.Status.Description() != "changed" || len(ob.Objects) != 0 {
		t.Errorf("after edit and delete: %q %d", ob.Status.Description(), len(ob.Objects))
	}
	run(t, x, cli.DeleteObjectStore("FILES"))
	run(t, x, cli.DeleteKV("CONFIG"))
	st, _ = c.LoadAll()
	if len(st.KVs) != 0 || len(st.Objects) != 0 {
		t.Errorf("after deleting the buckets: %d kv %d obj", len(st.KVs), len(st.Objects))
	}
}

func TestLiveSubscribeAndWatch(t *testing.T) {
	s, x := testnats.Setup(t)
	c := connect(t, s)
	live, err := c.Subscribe([]string{"chat.>"}, "")
	if err != nil {
		t.Fatal(err)
	}
	run(t, x, cli.Publish(cli.PublishSpec{Subject: "chat.room", Body: "hello", Headers: []string{"From:test"}, Count: 1}))
	select {
	case ev := <-live.Events:
		if ev.Kind != "msg" || ev.Subject != "chat.room" || string(ev.Data) != "hello" || ev.Header.Get("From") != "test" {
			t.Errorf("event: %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no message received")
	}
	live.Stop()
	if _, ok := <-live.Events; ok {
		t.Error("channel not closed after Stop")
	}
	kv := cli.NewKVSpec()
	kv.Name = "WATCHED"
	run(t, x, cli.AddKV(kv))
	run(t, x, cli.PutKey("WATCHED", "k1", "v1"))
	w, err := c.WatchKV("WATCHED", "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	got := map[string]string{}
	deadline := time.After(5 * time.Second)
	run(t, x, cli.PutKey("WATCHED", "k2", "v2"))
	run(t, x, cli.DeleteKey("WATCHED", "k1"))
	for len(got) < 3 {
		select {
		case ev := <-w.Events:
			if ev.Kind == "info" {
				continue
			}
			got[ev.Subject+"/"+ev.Kind] = string(ev.Data)
		case <-deadline:
			t.Fatalf("watch events: %v", got)
		}
	}
	if got["k1/put"] != "v1" || got["k2/put"] != "v2" {
		t.Errorf("watch: %v", got)
	}
	if _, ok := got["k1/delete"]; !ok {
		t.Errorf("watch missed the delete: %v", got)
	}
}

func TestRequestAndContexts(t *testing.T) {
	s, x := testnats.Setup(t)
	c := connect(t, s)
	// a responder through the library, a request through the CLI
	sub, err := c.NC.Subscribe("svc.echo", func(m *nats.Msg) { m.Respond(append([]byte("echo: "), m.Data...)) })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe()
	c.NC.Flush()
	res := cli.Request(cli.RequestSpec{Subject: "svc.echo", Body: "ping", Replies: 1, Timeout: "3s", Count: 1}).Execute(x, nil)
	if cli.Failed(res) || !strings.Contains(res[0].Output(), "echo: ping") {
		t.Errorf("request: %s", res[0].Output())
	}
	run(t, x, cli.SaveContext(cli.ContextSpec{Name: "other", Server: c.Ctx.ServerURL(), Description: "second"}, false))
	run(t, x, cli.CopyContext("other", "third"))
	run(t, x, cli.DeleteContext("third"))
	st, err := c.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if !contains(st.Contexts, "other") || contains(st.Contexts, "third") || st.Selected != "test" {
		t.Errorf("contexts: %v selected %s", st.Contexts, st.Selected)
	}
	info, err := cli.ReadContext("other")
	if err != nil || info.Description != "second" {
		t.Errorf("read context: %+v %v", info, err)
	}
	run(t, x, cli.SelectContext("other"))
	st, _ = c.LoadAll()
	if st.Selected != "other" {
		t.Errorf("selected: %s", st.Selected)
	}
	// the settings name the context explicitly, so the plans still go to "test"
	if line := s.CommandLine([]string{"stream", "ls"}); line != "nats --context test stream ls" {
		t.Errorf("command line: %s", line)
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}
