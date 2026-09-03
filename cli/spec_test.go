package cli

import (
	"strings"
	"testing"
	"time"
)

func has(args []string, want ...string) bool {
	s := " " + strings.Join(args, " ") + " "
	for _, w := range want {
		if !strings.Contains(s, " "+w+" ") {
			return false
		}
	}
	return true
}

func TestAddStreamFlags(t *testing.T) {
	s := NewStreamSpec()
	s.Name, s.Subjects, s.MaxAge, s.MaxMsgs, s.Storage = "ORDERS", []string{"orders.>", "shipments.*"}, "1h", 1000, "memory"
	s.Metadata = []string{"team=ops"}
	p := AddStream(s)
	if len(p.Cmds) != 1 {
		t.Fatalf("commands: %v", p.Cmds)
	}
	a := p.Cmds[0].Args
	for _, w := range []string{"stream add ORDERS", "--subjects=orders.>,shipments.*", "--storage=memory", "--max-age=1h", "--max-msgs=1000", "--metadata=team=ops", "--defaults"} {
		if !strings.Contains(strings.Join(a, " "), w) {
			t.Errorf("add lacks %q: %v", w, a)
		}
	}
	// defaults are not restated
	for _, w := range []string{"--retention", "--discard", "--compression", "--no-allow-rollup", "--allow-direct", "--ack"} {
		if strings.Contains(strings.Join(a, " "), w) {
			t.Errorf("add restates a default %q: %v", w, a)
		}
	}
}

func TestEditStreamOnlyChanges(t *testing.T) {
	old := NewStreamSpec()
	old.Name, old.Subjects, old.MaxAge = "X", []string{"x.>"}, "1h"
	cur := old
	if p := EditStream(old, cur); !p.Empty() {
		t.Errorf("no change should be empty: %v", p.Cmds)
	}
	cur.MaxMsgs, cur.DenyDelete, cur.Description, cur.MaxAge = 50, true, "hello", ""
	cur.RepubDest = "repub.>"
	p := EditStream(old, cur)
	if len(p.Cmds) != 1 {
		t.Fatalf("commands: %v", p.Cmds)
	}
	a := strings.Join(p.Cmds[0].Args, " ")
	for _, w := range []string{"stream edit X", "--max-msgs=50", "--deny-delete", "--description=hello", "--max-age=0", "--republish-source=>", "--republish-destination=repub.>", "-f"} {
		if !strings.Contains(a, w) {
			t.Errorf("edit lacks %q: %s", w, a)
		}
	}
	if strings.Contains(a, "--subjects") || strings.Contains(a, "--storage") {
		t.Errorf("edit restates unchanged settings: %s", a)
	}
	// clearing the republish
	old2 := cur
	cur2 := old2
	cur2.RepubDest = ""
	if a := strings.Join(EditStream(old2, cur2).Cmds[0].Args, " "); !strings.Contains(a, "--no-republish") {
		t.Errorf("clearing the republish: %s", a)
	}
	// storage cannot change: a note, no flag
	cur3 := old
	cur3.Storage = "memory"
	p = EditStream(old, cur3)
	if !p.Empty() || len(p.Notes) == 0 {
		t.Errorf("storage change: %v %v", p.Cmds, p.Notes)
	}
}

func TestConsumerPlans(t *testing.T) {
	s := NewConsumerSpec("ORDERS")
	s.Name, s.Filter, s.Deliver = "worker", []string{"orders.new", "orders.paid"}, "new"
	a := strings.Join(AddConsumer(s).Cmds[0].Args, " ")
	for _, w := range []string{"consumer add ORDERS worker", "--pull", "--deliver=new", "--filter=orders.new", "--filter=orders.paid", "--ack=explicit", "--wait=30s", "--max-deliver=-1", "--max-pending=1000", "--replay=instant", "--no-headers-only", "--defaults"} {
		if !strings.Contains(a, w) {
			t.Errorf("add lacks %q: %s", w, a)
		}
	}
	push := s
	push.Pull, push.Target, push.Name = false, "deliver.here", "pusher"
	a = strings.Join(AddConsumer(push).Cmds[0].Args, " ")
	if !strings.Contains(a, "--target=deliver.here") || strings.Contains(a, "--pull") {
		t.Errorf("push add: %s", a)
	}
	eph := s
	eph.Ephemeral, eph.Name = true, ""
	if a := strings.Join(AddConsumer(eph).Cmds[0].Args, " "); !strings.HasPrefix(a, "consumer add ORDERS --pull") || !strings.Contains(a, "--ephemeral") {
		t.Errorf("ephemeral add: %s", a)
	}
	cur := s
	cur.MaxDeliver, cur.Description, cur.Pull = 5, "retry five times", false
	p := EditConsumer(s, cur)
	a = strings.Join(p.Cmds[0].Args, " ")
	if !has(p.Cmds[0].Args, "consumer", "edit", "ORDERS", "worker", "--max-deliver=5", "-f") || !strings.Contains(a, "--description=retry five times") {
		t.Errorf("edit: %s", a)
	}
	if len(p.Notes) == 0 {
		t.Error("switching pull/push should leave a note")
	}
	if a := strings.Join(NextMessages("ORDERS", "worker", 3, "nak").Cmds[0].Args, " "); a != "consumer next ORDERS worker --count=3 --nak" {
		t.Errorf("next: %s", a)
	}
	until := time.Date(2030, 1, 2, 3, 4, 5, 0, time.Local)
	if a := strings.Join(PauseConsumer("ORDERS", "worker", until).Cmds[0].Args, " "); a != "consumer pause ORDERS worker 2030-01-02 03:04:05 -f" {
		t.Errorf("pause: %s", a)
	}
}

func TestBucketPlans(t *testing.T) {
	s := NewKVSpec()
	s.Name, s.History, s.TTL = "CONFIG", 5, "1h"
	a := strings.Join(AddKV(s).Cmds[0].Args, " ")
	for _, w := range []string{"kv add CONFIG", "--history=5", "--ttl=1h", "--storage=file"} {
		if !strings.Contains(a, w) {
			t.Errorf("kv add lacks %q: %s", w, a)
		}
	}
	cur := s
	cur.TTL, cur.Description = "", "settings"
	a = strings.Join(EditKV(s, cur).Cmds[0].Args, " ")
	if a != "kv edit CONFIG --description=settings --ttl=0" {
		t.Errorf("kv edit: %s", a)
	}
	if p := EditKV(s, s); !p.Empty() {
		t.Errorf("kv no change: %v", p.Cmds)
	}
	put := PutKey("CONFIG", "a.b", "line1\nline2")
	if put.Cmds[0].Stdin != "line1\nline2" || strings.Join(put.Cmds[0].Args, " ") != "kv put CONFIG a.b" {
		t.Errorf("put: %v", put.Cmds[0])
	}
	o := NewObjectSpec()
	o.Name, o.TTL, o.Compress = "FILES", "7d", true
	a = strings.Join(AddObjectStore(o).Cmds[0].Args, " ")
	if a != "object add FILES --ttl=7d --storage=file --compress" {
		t.Errorf("object add: %s", a)
	}
	if a := strings.Join(PutObject("FILES", "/tmp/x.txt", "x", "").Cmds[0].Args, " "); a != "object put FILES /tmp/x.txt -f --no-progress --name=x" {
		t.Errorf("object put: %s", a)
	}
}

func TestMessagingPlans(t *testing.T) {
	p := Publish(PublishSpec{Subject: "orders.new", Body: "hi", Headers: []string{"X-Id:1"}, Count: 3, Sleep: "10ms", JetStream: true})
	if a := strings.Join(p.Cmds[0].Args, " "); a != "pub orders.new hi --header=X-Id:1 --count=3 --sleep=10ms --jetstream" {
		t.Errorf("pub: %s", a)
	}
	p = Publish(PublishSpec{Subject: "x", Body: "a\nb", Count: 1})
	if a := strings.Join(p.Cmds[0].Args, " "); a != "pub x --force-stdin" || p.Cmds[0].Stdin != "a\nb" {
		t.Errorf("multi-line pub: %s %q", a, p.Cmds[0].Stdin)
	}
	// schedules: every/cron/at pass through, dest/source/ttl follow, no --jetstream (implied)
	p = Publish(PublishSpec{Subject: "sched.in", Body: "tick", Count: 1, JetStream: true, Schedule: "every", ScheduleValue: "1m", ScheduleDest: "sched.out", ScheduleSource: "sched.src", ScheduleTTL: "1h"})
	if a := strings.Join(p.Cmds[0].Args, " "); a != "pub sched.in tick --schedule-every=1m --schedule-dest=sched.out --schedule-source=sched.src --schedule-ttl=1h" || len(p.Notes) != 1 {
		t.Errorf("scheduled pub: %s %v", a, p.Notes)
	}
	p = Publish(PublishSpec{Subject: "s", Body: "b", Count: 1, Schedule: "cron", ScheduleValue: "0 */5 * * * *", ScheduleDest: "d"})
	if a := strings.Join(p.Cmds[0].Args, " "); a != "pub s b --schedule-cron=0 */5 * * * * --schedule-dest=d" {
		t.Errorf("cron pub: %s", a)
	}
	// after becomes an absolute --schedule-at, worked out here (nats 0.4.0 sends it wrong)
	p = Publish(PublishSpec{Subject: "s", Body: "b", Count: 1, Schedule: "after", ScheduleValue: "1h", ScheduleDest: "d"})
	a := strings.Join(p.Cmds[0].Args, " ")
	at := p.Cmds[0].Args[3]
	if !strings.HasPrefix(at, "--schedule-at=") || strings.Contains(a, "schedule-after") || len(p.Notes) != 2 {
		t.Errorf("after pub: %s %v", a, p.Notes)
	}
	if when, err := time.Parse(time.RFC3339, strings.TrimPrefix(at, "--schedule-at=")); err != nil || time.Until(when) < 59*time.Minute || time.Until(when) > 61*time.Minute {
		t.Errorf("after time: %s %v", at, err)
	}
	if p := Publish(PublishSpec{Subject: "s", Body: "b", Count: 1, Schedule: "none", JetStream: true}); !strings.Contains(strings.Join(p.Cmds[0].Args, " "), "--jetstream") {
		t.Errorf("none is no schedule: %v", p.Cmds[0].Args)
	}
	p = Request(RequestSpec{Subject: "svc", Body: "ping", Replies: 0, Timeout: "2s", Count: 1})
	if a := strings.Join(p.Cmds[0].Args, " "); a != "request svc ping --timeout=2s --replies=0" {
		t.Errorf("request: %s", a)
	}
}

func TestContextPlans(t *testing.T) {
	s := ContextSpec{Name: "prod", Server: "tls://nats.example.com:4222", Creds: "/x/user.creds", Description: "production", Select: true}
	a := strings.Join(SaveContext(s, false).Cmds[0].Args, " ")
	if a != "--server=tls://nats.example.com:4222 --creds=/x/user.creds context add prod --description=production --select" {
		t.Errorf("context add: %s", a)
	}
	if a := strings.Join(DeleteContext("prod").Cmds[0].Args, " "); a != "context rm prod -f" {
		t.Errorf("context rm: %s", a)
	}
}

func TestHelpers(t *testing.T) {
	for in, want := range map[string]int64{"": -1, "-1": -1, "10": 10, "1k": 1000, "2m": 2000000, "1kib": 1024, "1.5g": 1500000000, "100b": 100} {
		if got, err := ParseBytes(in); err != nil || got != want {
			t.Errorf("ParseBytes(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	if _, err := ParseBytes("lots"); err == nil {
		t.Error("ParseBytes accepted garbage")
	}
	for in, want := range map[string]time.Duration{"": 0, "30s": 30 * time.Second, "1h30m": 90 * time.Minute, "2d": 48 * time.Hour, "1w": 7 * 24 * time.Hour, "1d12h": 36 * time.Hour} {
		if got, err := ParseDuration(in); err != nil || got != want {
			t.Errorf("ParseDuration(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := ParseDuration("soon"); err == nil {
		t.Error("ParseDuration accepted garbage")
	}
	for in, want := range map[time.Duration]string{0: "", time.Hour: "1h", 90 * time.Minute: "1h30m", 2 * time.Minute: "2m", 48 * time.Hour: "2d", 500 * time.Millisecond: "500ms"} {
		if got := DurationText(in); got != want {
			t.Errorf("DurationText(%v) = %q; want %q", in, got, want)
		}
	}
	if Size(1536) != "1.5 KiB" || Size(10) != "10 B" || Bytes(-1) != "unlimited" {
		t.Error("Size/Bytes")
	}
	if Count(1234567) != "1,234,567" || Count(999) != "999" {
		t.Error("Count")
	}
	if ShellQuote("a b") != "'a b'" || ShellQuote("orders.>") != "'orders.>'" || ShellQuote("plain-1") != "plain-1" {
		t.Error("ShellQuote")
	}
	if got := (Settings{Context: "c", Server: "nats://x"}).CommandLine([]string{"stream", "ls"}); got != "nats --context c -s nats://x stream ls" {
		t.Errorf("CommandLine: %s", got)
	}
	md, err := ParseMetadata([]string{"a=1", "b = two"})
	if err != nil || md["a"] != "1" || md["b"] != "two" {
		t.Errorf("ParseMetadata: %v %v", md, err)
	}
	if _, err := ParseMetadata([]string{"novalue"}); err == nil {
		t.Error("ParseMetadata accepted a bare key")
	}
}
