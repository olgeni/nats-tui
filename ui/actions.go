package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/olgeni/nats-tui/cli"
)

const (
	manualValue = "\x00manual"
	addValue    = "\x00add"
)

// ---------------------------------------------------------------- editors

// editEntity opens the editor for the selected node.
func (m *Model) editEntity(n node) tea.Cmd {
	switch n.kind {
	case kContext:
		if m.store.ContextName == "" {
			m.setStatus("No context is in use: C adds one")
			return nil
		}
		return m.editContext(&contextRow{name: m.store.ContextName, info: m.store.Context})
	case kStream:
		return m.editStream(n.stream)
	case kConsumer:
		return m.editConsumer(n.cons)
	case kKV:
		return m.editKV(n.kv)
	case kObject:
		return m.editObjectStore(n.obj)
	case kService:
		m.setStatus("A service is what its process serves: nothing to edit here")
	case kSection:
		m.setStatus("Select an entity to edit")
	}
	return nil
}

// addChild adds what the row holds.
func (m *Model) addChild(n node) tea.Cmd {
	switch n.kind {
	case kStream:
		return m.addConsumer(n.stream)
	case kConsumer:
		return m.addConsumer(n.cons.Stream)
	case kKV:
		return m.putKey(n.kv.Name(), "", "", 0)
	case kObject:
		return m.putObject(n.obj.Name())
	case kSection:
		switch n.sec {
		case secKV:
			return m.addKV()
		case secObjects:
			return m.addObjectStore()
		case secServices:
			m.setStatus("Services are started by their own processes (nats service serve runs a demo one)")
			return nil
		}
	}
	return m.addStream()
}

func (m *Model) needJetStream() bool {
	if m.store == nil || !m.store.HasJetStream() {
		m.setError("This needs JetStream: " + m.jsError())
		return false
	}
	return true
}

func (m *Model) jsError() string {
	if m.store == nil {
		return "not connected"
	}
	return m.store.JSError
}

// streamFields is the editor of a stream; existing is nil for a new one.
func (m *Model) streamFields(s cli.StreamSpec, existing *cli.Stream) []*field {
	var f []*field
	f = append(f, section("Stream"))
	if existing != nil {
		f = append(f, infoField("Name", s.Name), infoField("Storage", s.Storage+" (fixed at creation)"))
	} else {
		f = append(f, textField("name", "Name", s.Name, "no spaces, dots, * or >", "required", validStreamName),
			choiceField("storage", "Storage", cli.StorageTypes, s.Storage, "file survives restarts; memory is faster and lost with the server"))
	}
	f = append(f,
		textField("description", "Description", s.Description, "", "(none)", nil),
		listField("subjects", "Subjects", s.Subjects, "comma-separated; wildcards * and > allowed (blank for a mirror)"),
		choiceField("retention", "Retention", cli.RetentionTypes, s.Retention, "limits: keep until a limit; interest: until every consumer got it; work: until one consumer got it"),
		choiceField("discard", "Discard", cli.DiscardTypes, s.Discard, "when a limit is hit: drop the oldest messages, or refuse new ones"),
		boolField("discard_per_subject", "Discard new per subject (with discard new and a per-subject limit)", s.DiscardPerSubject, ""),
		choiceField("compression", "Compression", cli.CompressionAlgs, s.Compression, "file storage only"),
		intField("replicas", "Replicas", s.Replicas, "1, 3 or 5 (clustered servers)"),
		section("Limits"),
		intField("max_msgs", "Max messages", s.MaxMsgs, "-1 unlimited"),
		intField("max_msgs_per_subject", "Max messages per subject", s.MaxMsgsPerSubject, "-1 unlimited"),
		bytesField("max_bytes", "Max bytes", s.MaxBytes, "-1 unlimited; k/m/g units"),
		textField("max_age", "Max age", s.MaxAge, "how long a message is kept: 1h, 7d, 1y; blank: forever", "(unlimited)", cli.ValidDuration),
		bytesField("max_msg_size", "Max message size", s.MaxMsgSize, "-1 unlimited"),
		intField("max_consumers", "Max consumers", s.MaxConsumers, "-1 unlimited"),
		textField("dupe_window", "Duplicate window", s.DupeWindow, "how long message ids are tracked for deduplication (2m); blank: none", "(none)", cli.ValidDuration),
		section("Options"),
		boolField("ack", "Acknowledge publishes (JetStream publishers get an ack)", s.Ack, ""),
		boolField("allow_rollup", "Allow rollup headers (a message replaces the stream or a subject)", s.AllowRollup, ""),
		boolField("deny_delete", "Deny message deletes through the API", s.DenyDelete, ""),
		boolField("deny_purge", "Deny purges through the API", s.DenyPurge, ""),
		boolField("allow_direct", "Allow direct get (fast reads of single messages)", s.AllowDirect, ""),
		boolField("mirror_direct", "Allow direct get on the origin of a mirror", s.MirrorDirect, ""),
		boolField("allow_msg_ttl", "Allow per-message TTL headers (cannot be turned off)", s.AllowMsgTTL, ""),
		boolField("allow_schedules", "Allow message schedules (cannot be turned off)", s.AllowSchedules, ""),
		boolField("allow_batch", "Allow fast batch publishing", s.AllowBatch, ""),
		section("Sources"),
		textField("mirror", "Mirror of", s.Mirror, "copy everything from this stream (no subjects of its own then)", "(none)", nil),
		listField("sources", "Sources", s.Sources, "streams to merge messages from, comma-separated"),
		section("Republish and transform"),
		textField("repub_source", "Republish source", s.RepubSource, "subject filter of the messages to republish (blank: all)", "(all)", nil),
		textField("repub_dest", "Republish destination", s.RepubDest, "where stored messages are published again (blank: no republish)", "(none)", nil),
		boolField("repub_headers", "Republish headers only", s.RepubHeaders, ""),
		textField("transform_source", "Transform source", s.TransformSource, "subject pattern to rewrite when storing", "(none)", nil),
		textField("transform_dest", "Transform destination", s.TransformDest, "what it becomes ($1 reuses wildcard tokens)", "(none)", nil),
		section("Placement and metadata"),
		textField("cluster", "Cluster", s.Cluster, "place the stream in this cluster", "(any)", nil),
		listField("tags", "Server tags", s.Tags, "place the stream on servers with these tags"),
		listField("metadata", "Metadata", s.Metadata, "key=value entries, comma-separated"),
	)
	if existing == nil {
		f = append(f, textField("first_seq", "First sequence", "", "start numbering at this sequence (blank: 1)", "(1)", func(v string) error {
			if v == "" {
				return nil
			}
			if _, err := strconv.ParseUint(v, 10, 64); err != nil {
				return fmt.Errorf("a sequence number")
			}
			return nil
		}))
	}
	return f
}

func streamFromEditor(ed *editor, base cli.StreamSpec, existing bool) (cli.StreamSpec, error) {
	s := base
	if !existing {
		s.Name = ed.str("name")
		s.Storage = ed.choice("storage")
		if v := ed.str("first_seq"); v != "" {
			s.FirstSeq, _ = strconv.ParseUint(v, 10, 64)
		}
	}
	s.Description = ed.str("description")
	s.Subjects = ed.list("subjects")
	s.Retention, s.Discard, s.Compression = ed.choice("retention"), ed.choice("discard"), ed.choice("compression")
	s.DiscardPerSubject = ed.on("discard_per_subject")
	s.Replicas = ed.int64("replicas")
	s.MaxMsgs, s.MaxMsgsPerSubject, s.MaxBytes = ed.int64("max_msgs"), ed.int64("max_msgs_per_subject"), ed.bytes("max_bytes")
	s.MaxAge, s.MaxMsgSize, s.MaxConsumers, s.DupeWindow = ed.str("max_age"), ed.bytes("max_msg_size"), ed.int64("max_consumers"), ed.str("dupe_window")
	s.Ack, s.AllowRollup, s.DenyDelete, s.DenyPurge = ed.on("ack"), ed.on("allow_rollup"), ed.on("deny_delete"), ed.on("deny_purge")
	s.AllowDirect, s.MirrorDirect, s.AllowMsgTTL, s.AllowBatch = ed.on("allow_direct"), ed.on("mirror_direct"), ed.on("allow_msg_ttl"), ed.on("allow_batch")
	s.AllowSchedules = ed.on("allow_schedules")
	s.Mirror, s.Sources = ed.str("mirror"), ed.list("sources")
	s.RepubSource, s.RepubDest, s.RepubHeaders = ed.str("repub_source"), ed.str("repub_dest"), ed.on("repub_headers")
	s.TransformSource, s.TransformDest = ed.str("transform_source"), ed.str("transform_dest")
	s.Cluster, s.Tags, s.Metadata = ed.str("cluster"), ed.list("tags"), ed.list("metadata")
	if s.Replicas < 1 {
		return s, fmt.Errorf("replicas: 1 or more")
	}
	if len(s.Subjects) == 0 && s.Mirror == "" && len(s.Sources) == 0 {
		return s, fmt.Errorf("a stream needs subjects, a mirror or sources")
	}
	if s.Mirror != "" && len(s.Subjects) > 0 {
		return s, fmt.Errorf("a mirror cannot have subjects of its own")
	}
	if _, err := cli.ParseMetadata(s.Metadata); err != nil {
		return s, err
	}
	return s, nil
}

func validStreamName(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("a name is required")
	}
	if strings.ContainsAny(s, " \t.*>/\\") {
		return fmt.Errorf("no spaces, dots, wildcards or slashes in a name")
	}
	return nil
}

func validName(s string) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("a name is required")
	}
	if strings.ContainsAny(s, "/\\ \t") {
		return fmt.Errorf("no spaces or slashes")
	}
	return nil
}

func nonEmpty(what string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("%s is required", what)
		}
		return nil
	}
}

func existingFile(s string) error {
	s = expandPath(strings.TrimSpace(s))
	if s == "" {
		return fmt.Errorf("a file is required")
	}
	if st, err := os.Stat(s); err != nil || st.IsDir() {
		return fmt.Errorf("not a file: %s", s)
	}
	return nil
}

func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func (m *Model) addStream() tea.Cmd {
	if !m.needJetStream() {
		return nil
	}
	spec := cli.NewStreamSpec()
	return m.openEditor(newEditor("Add a stream", m.streamFields(spec, nil), m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
		cur, err := streamFromEditor(ed, spec, false)
		if err != nil {
			m.setError(err.Error())
			return nil
		}
		if m.store.Stream(cur.Name) != nil {
			m.setError("stream " + cur.Name + " already exists")
			return nil
		}
		if err := m.subjectClash(cur.Subjects, ""); err != nil {
			m.setError(err.Error())
			return nil
		}
		return m.runPlan(cli.AddStream(cur), nil)
	})
}

func (m *Model) editStream(st *cli.Stream) tea.Cmd {
	old := cli.StreamSpecFrom(st.Info.Config)
	if old.Sealed {
		m.setError("The stream is sealed: nothing can be changed")
		return nil
	}
	return m.openEditor(newEditor("Edit stream "+st.Name(), m.streamFields(old, st), m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
		cur, err := streamFromEditor(ed, old, true)
		if err != nil {
			m.setError(err.Error())
			return nil
		}
		if err := m.subjectClash(cur.Subjects, st.Name()); err != nil {
			m.setError(err.Error())
			return nil
		}
		return m.runPlan(cli.EditStream(old, cur), nil)
	})
}

// subjectClash refuses subjects another stream already listens on.
func (m *Model) subjectClash(subjects []string, except string) error {
	if subj, other, name := m.store.SubjectClash(subjects, except); name != "" {
		if subj == other {
			return fmt.Errorf("subject %s is already held by stream %s: two streams cannot share a subject", subj, name)
		}
		return fmt.Errorf("subject %s overlaps %s of stream %s: two streams cannot share a subject", subj, other, name)
	}
	return nil
}

// consumerFields is the editor of a consumer; existing is nil for a new one.
func (m *Model) consumerFields(s cli.ConsumerSpec, existing *cli.Consumer) []*field {
	var f []*field
	f = append(f, section("Consumer"), infoField("Stream", s.Stream))
	mode := "pull"
	if !s.Pull {
		mode = "push"
	}
	if existing != nil {
		f = append(f, infoField("Name", s.Name), infoField("Mode", mode+" (fixed at creation)"))
	} else {
		f = append(f, textField("name", "Name", s.Name, "no spaces, dots, * or >; blank with the ephemeral box for a server-named one", "required", nil),
			boolField("ephemeral", "Ephemeral (removed when idle; no name needed)", s.Ephemeral, ""),
			choiceField("mode", "Mode", []string{"pull", "push"}, mode, "pull: clients fetch; push: the server delivers to a subject"))
	}
	f = append(f,
		textField("description", "Description", s.Description, "", "(none)", nil),
		textField("target", "Deliver subject (push)", s.Target, "where a push consumer sends the messages", "(pull)", nil),
		textField("deliver_group", "Deliver group (push)", s.DeliverGroup, "queue group of the push subscribers", "(none)", nil),
		boolField("flow_control", "Flow control (push)", s.FlowControl, ""),
		textField("heartbeat", "Idle heartbeat (push)", s.Heartbeat, "5s, 30s…; blank: none", "(none)", cli.ValidDuration),
		section("Delivery"),
		textField("deliver", "Deliver policy", s.Deliver, "all, new, last, subject (last per subject), a sequence number, a time (RFC3339), or an age (2h: since two hours ago)", "all", nonEmpty("a policy")),
		listField("filter", "Filter subjects", s.Filter, "only these subjects of the stream, comma-separated (blank: all)"),
		choiceField("ack", "Ack policy", cli.AckPolicies, s.Ack, "explicit: every message; all: an ack covers the earlier ones; none"),
		textField("ack_wait", "Ack wait", s.AckWait, "redeliver after this without an ack (30s)", "30s", cli.ValidDuration),
		intField("max_deliver", "Max deliveries", s.MaxDeliver, "-1 unlimited"),
		intField("max_pending", "Max ack pending", s.MaxPending, "outstanding unacknowledged messages (1000); -1 unlimited"),
		choiceField("replay", "Replay", cli.ReplayPolicies, s.Replay, "instant, or at the original pace"),
		listField("backoff", "Backoff steps", s.BackoffSteps, "redelivery delays, comma-separated (1m, 5m, 20m); blank: the ack wait"),
		boolField("headers_only", "Headers only (no bodies)", s.HeadersOnly, ""),
		textField("inactive", "Inactive threshold", s.InactiveThreshold, "remove the consumer after this much inactivity; blank: keep it", "(keep)", cli.ValidDuration),
		intField("sample", "Sample %", s.Sample, "share of acks sampled for monitoring (-1: none)"),
		section("Pull settings"),
		intField("max_waiting", "Max waiting pulls", s.MaxWaiting, "outstanding pull requests (512); 0: the server default"),
		intField("max_pull_batch", "Max pull batch", s.MaxPullBatch, "0: the server default"),
		textField("max_pull_expire", "Max pull expire", s.MaxPullExpire, "longest a pull may wait; blank: the server default", "(default)", cli.ValidDuration),
		bytesField("max_pull_bytes", "Max pull bytes", s.MaxPullBytes, "0 or -1: the server default"),
		section("Storage and metadata"),
		intField("replicas", "Replicas", s.Replicas, "0: the stream's"),
		boolField("memory", "Keep the state in memory", s.Memory, ""),
		listField("metadata", "Metadata", s.Metadata, "key=value entries, comma-separated"),
	)
	return f
}

func consumerFromEditor(ed *editor, base cli.ConsumerSpec, existing bool) (cli.ConsumerSpec, error) {
	s := base
	if !existing {
		s.Name = ed.str("name")
		s.Ephemeral = ed.on("ephemeral")
		s.Pull = ed.choice("mode") == "pull"
		if s.Name == "" && !s.Ephemeral {
			return s, fmt.Errorf("a name is required (or tick ephemeral)")
		}
		if err := validStreamName(s.Name); s.Name != "" && err != nil {
			return s, err
		}
	}
	s.Description = ed.str("description")
	s.Target, s.DeliverGroup, s.FlowControl, s.Heartbeat = ed.str("target"), ed.str("deliver_group"), ed.on("flow_control"), ed.str("heartbeat")
	if !existing {
		if !s.Pull && s.Target == "" {
			return s, fmt.Errorf("a push consumer needs a deliver subject")
		}
		if s.Pull && s.Target != "" {
			return s, fmt.Errorf("a pull consumer has no deliver subject (choose push)")
		}
	}
	s.Deliver, s.Filter, s.Ack, s.AckWait = ed.str("deliver"), ed.list("filter"), ed.choice("ack"), ed.str("ack_wait")
	s.MaxDeliver, s.MaxPending, s.Replay = ed.int64("max_deliver"), ed.int64("max_pending"), ed.choice("replay")
	s.BackoffSteps = ed.list("backoff")
	for _, b := range s.BackoffSteps {
		if d, err := cli.ParseDuration(b); err != nil || d <= 0 {
			return s, fmt.Errorf("backoff steps are durations like 1m, 5m")
		}
	}
	s.HeadersOnly, s.InactiveThreshold, s.Sample = ed.on("headers_only"), ed.str("inactive"), ed.int64("sample")
	s.MaxWaiting, s.MaxPullBatch, s.MaxPullExpire, s.MaxPullBytes = ed.int64("max_waiting"), ed.int64("max_pull_batch"), ed.str("max_pull_expire"), ed.bytes("max_pull_bytes")
	if s.MaxWaiting < 0 {
		s.MaxWaiting = 0
	}
	if s.MaxPullBatch < 0 {
		s.MaxPullBatch = 0
	}
	if s.MaxPullBytes < 0 {
		s.MaxPullBytes = 0
	}
	s.Replicas, s.Memory, s.Metadata = ed.int64("replicas"), ed.on("memory"), ed.list("metadata")
	if s.Replicas < 0 {
		s.Replicas = 0
	}
	if _, err := cli.ParseMetadata(s.Metadata); err != nil {
		return s, err
	}
	return s, nil
}

func (m *Model) addConsumer(st *cli.Stream) tea.Cmd { return m.addConsumerWith(st, nil) }

// addConsumerWith opens the consumer editor with a filter already set:
// the messages and subjects tables offer a consumer on what they show.
func (m *Model) addConsumerWith(st *cli.Stream, filter []string) tea.Cmd {
	if st == nil {
		return m.pickStream("Add a consumer to which stream?", func(m *Model, st *cli.Stream) tea.Cmd { return m.addConsumerWith(st, filter) })
	}
	// the names in use are needed to refuse a duplicate
	return m.ensureConsumers(st, func(m *Model) tea.Cmd { return m.addConsumerEditor(st, filter) })
}

func (m *Model) addConsumerEditor(st *cli.Stream, filter []string) tea.Cmd {
	spec := cli.NewConsumerSpec(st.Name())
	title := "Add a consumer to " + st.Name()
	if len(filter) > 0 {
		spec.Filter = filter
		title += " on " + cli.JoinList(filter)
	}
	return m.openEditor(newEditor(title, m.consumerFields(spec, nil), m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
		cur, err := consumerFromEditor(ed, spec, false)
		if err != nil {
			m.setError(err.Error())
			return nil
		}
		for _, c := range st.Consumers {
			if c.Name() == cur.Name {
				m.setError("consumer " + cur.Name + " already exists")
				return nil
			}
		}
		return m.runPlan(cli.AddConsumer(cur), nil)
	})
}

func (m *Model) editConsumer(c *cli.Consumer) tea.Cmd {
	old := cli.ConsumerSpecFrom(c.Stream.Name(), c.Info.Config, c.Info)
	return m.openEditor(newEditor(fmt.Sprintf("Edit consumer %s of %s", c.Name(), c.Stream.Name()), m.consumerFields(old, c), m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
		cur, err := consumerFromEditor(ed, old, true)
		if err != nil {
			m.setError(err.Error())
			return nil
		}
		return m.runPlan(cli.EditConsumer(old, cur), nil)
	})
}

// kvFields is the editor of a bucket.
func (m *Model) kvFields(s cli.KVSpec, existing bool) []*field {
	var f []*field
	f = append(f, section("Key-value bucket"))
	if existing {
		f = append(f, infoField("Name", s.Name), infoField("Storage", s.Storage+" (fixed at creation)"))
	} else {
		f = append(f, textField("name", "Name", s.Name, "no spaces or dots", "required", validStreamName),
			choiceField("storage", "Storage", cli.StorageTypes, s.Storage, ""))
	}
	f = append(f,
		textField("description", "Description", s.Description, "", "(none)", nil),
		intField("history", "History per key", s.History, "how many revisions of a key are kept (1 to 64)"),
		textField("ttl", "TTL", s.TTL, "how long a value is kept: 1h, 7d; blank: forever", "(unlimited)", cli.ValidDuration),
		textField("marker_ttl", "Limit marker TTL", s.MarkerTTL, "enables per-key TTLs and keeps a marker this long when a key expires; blank: off", "(off)", cli.ValidDuration),
		intField("replicas", "Replicas", s.Replicas, "1, 3 or 5"),
		bytesField("max_value_size", "Max value size", s.MaxValueSize, "-1 unlimited"),
		bytesField("max_bytes", "Max bucket size", s.MaxBytes, "-1 unlimited"),
		boolField("compress", "Compress the data on disk", s.Compress, ""),
		section("Republish, mirror and sources"),
		textField("repub_source", "Republish source", s.RepubSource, "subject filter of the changes to republish (blank: all)", "(all)", nil),
		textField("repub_dest", "Republish destination", s.RepubDest, "subject the changes are published to (blank: none)", "(none)", nil),
		boolField("repub_headers", "Republish headers only", s.RepubHeaders, ""),
	)
	if !existing {
		f = append(f, textField("mirror", "Mirror of", s.Mirror, "copy another bucket (creation only)", "(none)", nil))
	} else if s.Mirror != "" {
		f = append(f, textField("mirror", "Mirror of", s.Mirror, "blank removes the mirror", "(none)", nil))
	}
	f = append(f,
		listField("sources", "Sources", s.Sources, "buckets to merge keys from, comma-separated"),
		section("Placement and metadata"),
		textField("cluster", "Cluster", s.Cluster, "", "(any)", nil),
		listField("tags", "Server tags", s.Tags, "comma-separated"),
		listField("metadata", "Metadata", s.Metadata, "key=value entries, comma-separated"),
	)
	return f
}

func kvFromEditor(ed *editor, base cli.KVSpec, existing bool) (cli.KVSpec, error) {
	s := base
	if !existing {
		s.Name, s.Storage = ed.str("name"), ed.choice("storage")
	}
	s.Description, s.History, s.TTL, s.MarkerTTL = ed.str("description"), ed.int64("history"), ed.str("ttl"), ed.str("marker_ttl")
	s.Replicas, s.MaxValueSize, s.MaxBytes, s.Compress = ed.int64("replicas"), ed.bytes("max_value_size"), ed.bytes("max_bytes"), ed.on("compress")
	s.RepubSource, s.RepubDest, s.RepubHeaders = ed.str("repub_source"), ed.str("repub_dest"), ed.on("repub_headers")
	if ed.get("mirror").kind == fText {
		s.Mirror = ed.str("mirror")
	}
	s.Sources, s.Cluster, s.Tags, s.Metadata = ed.list("sources"), ed.str("cluster"), ed.list("tags"), ed.list("metadata")
	if s.History < 1 || s.History > 64 {
		return s, fmt.Errorf("history: 1 to 64")
	}
	if s.Replicas < 1 {
		return s, fmt.Errorf("replicas: 1 or more")
	}
	if _, err := cli.ParseMetadata(s.Metadata); err != nil {
		return s, err
	}
	return s, nil
}

func (m *Model) addKV() tea.Cmd {
	if !m.needJetStream() {
		return nil
	}
	spec := cli.NewKVSpec()
	return m.openEditor(newEditor("Add a key-value bucket", m.kvFields(spec, false), m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
		cur, err := kvFromEditor(ed, spec, false)
		if err != nil {
			m.setError(err.Error())
			return nil
		}
		if m.store.KV(cur.Name) != nil {
			m.setError("bucket " + cur.Name + " already exists")
			return nil
		}
		return m.runPlan(cli.AddKV(cur), nil)
	})
}

func (m *Model) editKV(b *cli.Bucket) tea.Cmd {
	old := cli.KVSpecFrom(b.Status, b.Info)
	return m.openEditor(newEditor("Edit bucket "+b.Name(), m.kvFields(old, true), m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
		cur, err := kvFromEditor(ed, old, true)
		if err != nil {
			m.setError(err.Error())
			return nil
		}
		return m.runPlan(cli.EditKV(old, cur), nil)
	})
}

// objectFields is the editor of an object store.
func (m *Model) objectFields(s cli.ObjectSpec, existing bool) []*field {
	var f []*field
	f = append(f, section("Object store"))
	if existing {
		f = append(f, infoField("Name", s.Name), infoField("Storage", s.Storage+" (fixed at creation)"))
	} else {
		f = append(f, textField("name", "Name", s.Name, "no spaces or dots", "required", validStreamName),
			choiceField("storage", "Storage", cli.StorageTypes, s.Storage, ""))
	}
	f = append(f,
		textField("description", "Description", s.Description, "", "(none)", nil),
		textField("ttl", "TTL", s.TTL, "how long an object is kept; blank: forever", "(unlimited)", cli.ValidDuration),
		intField("replicas", "Replicas", s.Replicas, "1, 3 or 5"),
		bytesField("max_bytes", "Max bucket size", s.MaxBytes, "-1 unlimited"),
		boolField("compress", "Compress the data on disk", s.Compress, ""),
		section("Placement and metadata"),
		textField("cluster", "Cluster", s.Cluster, "", "(any)", nil),
		listField("tags", "Server tags", s.Tags, "comma-separated"),
		listField("metadata", "Metadata", s.Metadata, "key=value entries, comma-separated"),
	)
	return f
}

func objectFromEditor(ed *editor, base cli.ObjectSpec, existing bool) (cli.ObjectSpec, error) {
	s := base
	if !existing {
		s.Name, s.Storage = ed.str("name"), ed.choice("storage")
	}
	s.Description, s.TTL, s.Replicas, s.MaxBytes, s.Compress = ed.str("description"), ed.str("ttl"), ed.int64("replicas"), ed.bytes("max_bytes"), ed.on("compress")
	s.Cluster, s.Tags, s.Metadata = ed.str("cluster"), ed.list("tags"), ed.list("metadata")
	if s.Replicas < 1 {
		return s, fmt.Errorf("replicas: 1 or more")
	}
	if _, err := cli.ParseMetadata(s.Metadata); err != nil {
		return s, err
	}
	return s, nil
}

func (m *Model) addObjectStore() tea.Cmd {
	if !m.needJetStream() {
		return nil
	}
	spec := cli.NewObjectSpec()
	return m.openEditor(newEditor("Add an object store", m.objectFields(spec, false), m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
		cur, err := objectFromEditor(ed, spec, false)
		if err != nil {
			m.setError(err.Error())
			return nil
		}
		if m.store.Object(cur.Name) != nil {
			m.setError("object store " + cur.Name + " already exists")
			return nil
		}
		return m.runPlan(cli.AddObjectStore(cur), nil)
	})
}

func (m *Model) editObjectStore(b *cli.ObjectBucket) tea.Cmd {
	old := cli.ObjectSpecFrom(b.Status, b.Info)
	if b.Status.Sealed() {
		m.setError("The object store is sealed: nothing can be changed")
		return nil
	}
	return m.openEditor(newEditor("Edit object store "+b.Name(), m.objectFields(old, true), m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
		cur, err := objectFromEditor(ed, old, true)
		if err != nil {
			m.setError(err.Error())
			return nil
		}
		return m.runPlan(cli.EditObjectStore(old, cur), nil)
	})
}

// editContext adds a context (nil) or edits one.
func (m *Model) editContext(row *contextRow) tea.Cmd {
	var s cli.ContextSpec
	title := "Add a context"
	if row != nil {
		s = cli.ContextSpecFrom(row.info)
		title = "Edit context " + row.name
	} else {
		s.Server = "nats://localhost:4222"
	}
	var fields []*field
	fields = append(fields, section("Context"))
	if row != nil {
		fields = append(fields, infoField("Name", row.name), infoField("File", row.info.Path))
	} else {
		fields = append(fields, textField("name", "Name", "", "no spaces or slashes", "required", validName))
	}
	fields = append(fields,
		textField("description", "Description", s.Description, "", "(none)", nil),
		textField("server", "Server URL", s.Server, "nats://host:4222, several comma-separated; tls:// for TLS", "nats://localhost:4222", nonEmpty("a server URL")),
		boolField("select", "Make it the default context", s.Select, ""),
		section("Credentials (one kind)"),
		textField("creds", "Credentials file", s.Creds, "a .creds file from nsc", "(none)", nil),
		textField("nkey", "NKey seed file", s.NKey, "", "(none)", nil),
		textField("user", "User", s.User, "with a password, or a token here alone", "(none)", nil),
		textField("password", "Password", "", "stored in clear in the context file; blank keeps the current one", "(keep)", nil),
		textField("token", "Token", "", "stored in clear; blank keeps the current one", "(keep)", nil),
		textField("nsc", "nsc lookup", s.NscURL, "nsc://operator/account/user: the credentials come from nsc", "(none)", nil),
		section("TLS"),
		textField("cert", "Client certificate", s.Cert, "", "(none)", nil),
		textField("key", "Client key", s.Key, "", "(none)", nil),
		textField("ca", "CA certificate", s.CA, "", "(none)", nil),
		boolField("tlsfirst", "TLS handshake first (before the server greeting)", s.TLSFirst, ""),
		section("JetStream and inboxes"),
		textField("js_domain", "JetStream domain", s.JSDomain, "", "(none)", nil),
		textField("js_api", "JetStream API prefix", s.JSAPIPrefix, "for imported JetStream", "(none)", nil),
		textField("js_event", "JetStream event prefix", s.JSEventPfx, "", "(none)", nil),
		textField("inbox", "Inbox prefix", s.InboxPrefix, "", "(_INBOX)", nil),
		textField("socks", "SOCKS5 proxy", s.SocksProxy, "", "(none)", nil),
	)
	return m.openEditor(newEditor(title, fields, m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
		cur := s
		if row == nil {
			cur.Name = ed.str("name")
			if contains(knownContexts(), cur.Name) {
				m.setError("context " + cur.Name + " already exists (edit it from the table)")
				return nil
			}
		}
		cur.Description, cur.Server, cur.Select = ed.str("description"), ed.str("server"), ed.on("select")
		cur.Creds, cur.NKey, cur.User, cur.Password, cur.Token = expandPath(ed.str("creds")), expandPath(ed.str("nkey")), ed.str("user"), ed.str("password"), ed.str("token")
		cur.NscURL = ed.str("nsc")
		cur.Cert, cur.Key, cur.CA, cur.TLSFirst = expandPath(ed.str("cert")), expandPath(ed.str("key")), expandPath(ed.str("ca")), ed.on("tlsfirst")
		cur.JSDomain, cur.JSAPIPrefix, cur.JSEventPfx, cur.InboxPrefix, cur.SocksProxy = ed.str("js_domain"), ed.str("js_api"), ed.str("js_event"), ed.str("inbox"), ed.str("socks")
		p := cli.SaveContext(cur, row != nil)
		return m.runPlan(p, nil)
	})
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- pickers

func (m *Model) pickStream(title string, done func(m *Model, st *cli.Stream) tea.Cmd) tea.Cmd {
	if !m.needJetStream() {
		return nil
	}
	var items []pickItem
	for _, st := range m.store.Streams {
		items = append(items, pickItem{fmt.Sprintf("%-24s %s", st.Name(), strings.Join(st.Info.Config.Subjects, " ")), st.Name()})
	}
	if len(items) == 0 {
		m.setError("There are no streams yet (A adds one)")
		return nil
	}
	return m.openPicker(title, "", items, "", func(m *Model, v string) tea.Cmd {
		if st := m.store.Stream(v); st != nil {
			return done(m, st)
		}
		return nil
	}, nil)
}

func (m *Model) pickBucket(title string, done func(m *Model, b *cli.Bucket) tea.Cmd) tea.Cmd {
	if !m.needJetStream() {
		return nil
	}
	var items []pickItem
	for _, b := range m.store.KVs {
		items = append(items, pickItem{fmt.Sprintf("%-24s %d values", b.Name(), b.Status.Values()), b.Name()})
	}
	if len(items) == 0 {
		m.setError("There are no buckets yet (a on the buckets section adds one)")
		return nil
	}
	return m.openPicker(title, "", items, "", func(m *Model, v string) tea.Cmd {
		if b := m.store.KV(v); b != nil {
			return done(m, b)
		}
		return nil
	}, nil)
}

func (m *Model) pickObjectStore(title string, done func(m *Model, b *cli.ObjectBucket) tea.Cmd) tea.Cmd {
	if !m.needJetStream() {
		return nil
	}
	var items []pickItem
	for _, b := range m.store.Objects {
		what := cli.Size(b.Status.Size())
		if b.Loaded {
			what = fmt.Sprintf("%d objects", len(b.Objects))
		}
		items = append(items, pickItem{fmt.Sprintf("%-24s %s", b.Name(), what), b.Name()})
	}
	if len(items) == 0 {
		m.setError("There are no object stores yet (a on the object stores section adds one)")
		return nil
	}
	return m.openPicker(title, "", items, "", func(m *Model, v string) tea.Cmd {
		if b := m.store.Object(v); b != nil {
			return done(m, b)
		}
		return nil
	}, nil)
}

// ---------------------------------------------------------------- delete, purge, seal

func (m *Model) deleteEntity(n node) tea.Cmd {
	switch n.kind {
	case kStream:
		st := n.stream
		m.formVals.yes = false
		return m.openForm(confirmForm("Delete stream "+st.Name()+"?", fmt.Sprintf("Its %s messages and %d consumer(s) go with it.", cli.Count(st.Info.State.Msgs), st.ConsumerCount()), &m.formVals.yes), func(m *Model) tea.Cmd {
			if !m.formVals.yes {
				return nil
			}
			return m.runPlan(cli.DeleteStream(st.Name()), nil)
		}, nil)
	case kConsumer:
		return m.runPlan(cli.DeleteConsumer(n.cons.Stream.Name(), n.cons.Name()), nil)
	case kKV:
		b := n.kv
		m.formVals.yes = false
		return m.openForm(confirmForm("Delete bucket "+b.Name()+"?", fmt.Sprintf("Its %d value(s) go with it.", b.Status.Values()), &m.formVals.yes), func(m *Model) tea.Cmd {
			if !m.formVals.yes {
				return nil
			}
			return m.runPlan(cli.DeleteKV(b.Name()), nil)
		}, nil)
	case kObject:
		b := n.obj
		m.formVals.yes = false
		what := "Its objects (" + cli.Size(b.Status.Size()) + ") go with it."
		if b.Loaded {
			what = fmt.Sprintf("Its %d object(s) go with it.", len(b.Objects))
		}
		return m.openForm(confirmForm("Delete object store "+b.Name()+"?", what, &m.formVals.yes), func(m *Model) tea.Cmd {
			if !m.formVals.yes {
				return nil
			}
			return m.runPlan(cli.DeleteObjectStore(b.Name()), nil)
		}, nil)
	case kContext:
		m.setStatus("Contexts are deleted from the contexts table (C)")
	default:
		m.setStatus("Select a stream, consumer, bucket or object store to delete")
	}
	return nil
}

func (m *Model) purgeStream(st *cli.Stream) tea.Cmd {
	if st.Info.Config.DenyPurge {
		m.setError("The stream denies purges (e changes that)")
		return nil
	}
	fields := []*field{
		section("Purge stream " + st.Name()),
		infoField("Messages now", fmt.Sprintf("%s (sequences %d to %d)", cli.Count(st.Info.State.Msgs), st.Info.State.FirstSeq, st.Info.State.LastSeq)),
		textField("subject", "Subject", "", "only the messages of this subject (blank: all)", "(all)", nil),
		textField("seq", "Up to sequence", "", "remove everything before this sequence (blank: everything)", "(all)", func(s string) error {
			if s == "" {
				return nil
			}
			if _, err := strconv.ParseUint(s, 10, 64); err != nil {
				return fmt.Errorf("a sequence number")
			}
			return nil
		}),
		textField("keep", "Keep the last", "", "keep this many messages (blank: none)", "(none)", func(s string) error {
			if s == "" {
				return nil
			}
			if _, err := strconv.ParseUint(s, 10, 64); err != nil {
				return fmt.Errorf("a count")
			}
			return nil
		}),
	}
	return m.openEditor(newEditor("Purge stream "+st.Name(), fields, m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
		seq, _ := strconv.ParseInt(ed.str("seq"), 10, 64)
		keep, _ := strconv.ParseInt(ed.str("keep"), 10, 64)
		if seq > 0 && keep > 0 {
			m.setError("Up to a sequence and keep the last exclude each other")
			return nil
		}
		return m.runPlan(cli.PurgeStream(st.Name(), ed.str("subject"), seq, keep), nil)
	})
}

func (m *Model) seal(n node) tea.Cmd {
	switch n.kind {
	case kStream, kConsumer:
		st := n.streamOf()
		if st.Info.Config.Sealed {
			m.setStatus("The stream is already sealed")
			return nil
		}
		m.formVals.yes = false
		return m.openForm(confirmForm("Seal stream "+st.Name()+"?", "A sealed stream takes no more messages and no more deletes, and cannot be unsealed.", &m.formVals.yes), func(m *Model) tea.Cmd {
			if !m.formVals.yes {
				return nil
			}
			return m.runPlan(cli.SealStream(st.Name()), nil)
		}, nil)
	case kObject:
		b := n.obj
		if b.Status.Sealed() {
			m.setStatus("The object store is already sealed")
			return nil
		}
		m.formVals.yes = false
		return m.openForm(confirmForm("Seal object store "+b.Name()+"?", "A sealed bucket takes no more objects and cannot be unsealed.", &m.formVals.yes), func(m *Model) tea.Cmd {
			if !m.formVals.yes {
				return nil
			}
			return m.runPlan(cli.SealObjectStore(b.Name()), nil)
		}, nil)
	}
	m.setStatus("Select a stream or an object store to seal")
	return nil
}

// ---------------------------------------------------------------- consumers

func (m *Model) nextMessages(c *cli.Consumer) tea.Cmd {
	if !c.Pull() {
		m.setError("Only pull consumers are fetched from; a push consumer delivers to " + c.Info.Config.DeliverSubject)
		return nil
	}
	fields := []*field{
		section(fmt.Sprintf("Next messages of %s (%s unprocessed)", c.Name(), cli.Count(c.Info.NumPending))),
		intField("count", "Messages", 1, "how many to fetch"),
		choiceField("ack", "Then", []string{"ack", "no-ack", "nak", "term"}, "ack", "ack: done; no-ack: redelivered after the ack wait; nak: redelivered now; term: never again"),
	}
	return m.openEditor(newEditor("Next messages of "+c.Name(), fields, m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
		n := ed.int64("count")
		if n < 1 {
			n = 1
		}
		return m.runPlan(cli.NextMessages(c.Stream.Name(), c.Name(), int(n), ed.choice("ack")), nil)
	})
}

func (m *Model) pauseResume(c *cli.Consumer) tea.Cmd {
	if c.Info.Paused {
		return m.runPlan(cli.ResumeConsumer(c.Stream.Name(), c.Name()), nil)
	}
	m.formVals.str = "1h"
	return m.openForm(inputForm("Pause consumer "+c.Name()+" for", "A duration (30m, 2h, 1d): delivery stops until then; u resumes it earlier.", "1h", &m.formVals.str, func(s string) error {
		d, err := cli.ParseDuration(s)
		if err != nil || d <= 0 {
			return fmt.Errorf("a duration like 30m, 2h, 1d")
		}
		return nil
	}), func(m *Model) tea.Cmd {
		d, _ := cli.ParseDuration(m.formVals.str)
		return m.runPlan(cli.PauseConsumer(c.Stream.Name(), c.Name(), time.Now().Add(d)), nil)
	}, nil)
}

// ---------------------------------------------------------------- messaging

// subscribe opens a live subscription: to a stream's subjects, or to what
// is typed.
func (m *Model) subscribe(n node) tea.Cmd {
	subjects := ">"
	switch {
	case n.kind == kStream && len(n.stream.Info.Config.Subjects) > 0:
		subjects = cli.JoinList(n.stream.Info.Config.Subjects)
	case n.kind == kConsumer:
		if f := consumerFilter(n.cons.Info.Config); f != "" {
			subjects = strings.ReplaceAll(f, " ", ", ")
		} else if s := n.cons.Stream.Info.Config.Subjects; len(s) > 0 {
			subjects = cli.JoinList(s)
		}
	case n.kind == kService:
		var subs []string
		for _, e := range n.svc.Info.Endpoints {
			subs = append(subs, e.Subject)
		}
		if len(subs) > 0 {
			subjects = cli.JoinList(subs)
		}
	}
	fields := []*field{
		section("Subscribe"),
		listField("subjects", "Subjects", cli.SplitList(subjects), "comma-separated; wildcards * and > allowed"),
		textField("queue", "Queue group", "", "share the messages with other members of the group (blank: none)", "(none)", nil),
	}
	return m.openEditor(newEditor("Subscribe", fields, m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
		subs := ed.list("subjects")
		if len(subs) == 0 {
			m.setError("A subject is required")
			return nil
		}
		return m.subscribeTo(subs, ed.str("queue"))
	})
}

func (m *Model) subscribeTo(subjects []string, queue string) tea.Cmd {
	c := m.client
	args := append([]string{"sub"}, subjects...)
	if queue != "" {
		args = append(args, "--queue="+queue)
	}
	desc := "Messages arriving on " + strings.Join(subjects, ", ")
	if queue != "" {
		desc += " (queue group " + queue + ")"
	}
	desc += " — " + m.settings.CommandLine(args)
	return m.openLive("Subscribed to "+strings.Join(subjects, ", "), desc, m.settings.CommandLine(args), func() (*cli.Live, error) {
		return c.Subscribe(subjects, queue)
	})
}

func (m *Model) events() tea.Cmd {
	c := m.client
	subjects := cli.EventSubjects(m.store.Context.JSEventPfx)
	desc := "JetStream advisories and metrics, and the server events of the system account — " + m.settings.CommandLine([]string{"events", "--all"})
	return m.openLive("Events", desc, "", func() (*cli.Live, error) {
		return c.Subscribe(subjects, "")
	})
}

func (m *Model) watch(n node) tea.Cmd {
	switch n.kind {
	case kKV:
		return m.watchKV(n.kv.Name(), "")
	case kObject:
		return m.watchObjects(n.obj.Name())
	case kStream, kConsumer:
		return m.subscribe(n)
	}
	return m.pickBucket("Watch which bucket?", func(m *Model, b *cli.Bucket) tea.Cmd { return m.watchKV(b.Name(), "") })
}

func (m *Model) watchKV(bucket, keys string) tea.Cmd {
	c := m.client
	args := []string{"kv", "watch", bucket}
	if keys != "" {
		args = append(args, keys)
	}
	return m.openLive("Watching bucket "+bucket, "Every change of the bucket with its revision, the current values first — "+m.settings.CommandLine(args), "", func() (*cli.Live, error) {
		return c.WatchKV(bucket, keys, false)
	})
}

func (m *Model) watchObjects(bucket string) tea.Cmd {
	c := m.client
	return m.openLive("Watching object store "+bucket, "Every object change, the current objects first — "+m.settings.CommandLine([]string{"object", "watch", bucket}), "", func() (*cli.Live, error) {
		return c.WatchObjects(bucket)
	})
}

func (m *Model) publish(subject string) tea.Cmd {
	fields := []*field{
		section("Publish"),
		textField("subject", "Subject", subject, "", "required", validSubject),
		textField("body", "Body", "", "the message; with a count, {{Count}}, {{Time}}, {{ID}} and {{Random 5 10}} are expanded", "(empty)", nil),
		listField("headers", "Headers", nil, "Key:Value pairs, comma-separated"),
		intField("count", "Count", 1, "publish this many messages"),
		textField("sleep", "Sleep between messages", "", "with a count: 100ms, 1s…", "(none)", cli.ValidDuration),
		textField("reply", "Reply subject", "", "ask for replies here (blank: none)", "(none)", nil),
		boolField("jetstream", "Publish through JetStream (wait for the stream's ack)", false, ""),
		section("Schedule (the stream holding the subject must allow message schedules)"),
		choiceField("schedule", "Schedule", cli.ScheduleKinds, "none", "at: once at a time; after: once after a delay; every: on an interval; cron: on a cron line"),
		textField("schedule_value", "When", "", "at: RFC3339 time (2026-09-03T18:00:00Z); after/every: a duration (10m, 2h); cron: six fields, seconds first (0 */5 * * * *)", "(time, delay, interval or cron line)", nil),
		textField("schedule_dest", "Destination", "", "subject the message is published to when the schedule fires", "(required with a schedule)", nil),
		textField("schedule_source", "Source", "", "publish the last message stored on this subject instead of the body (blank: the body)", "(the body)", nil),
		textField("schedule_ttl", "TTL of the fired messages", "", "how long the published messages live (needs per-message TTL on the stream)", "(none)", cli.ValidDuration),
	}
	return m.openEditor(newEditor("Publish a message", fields, m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
		s := cli.PublishSpec{Subject: ed.str("subject"), Body: ed.get("body").text, Headers: ed.list("headers"), Count: ed.int64("count"), Sleep: ed.str("sleep"), Reply: ed.str("reply"), JetStream: ed.on("jetstream")}
		s.Schedule, s.ScheduleValue, s.ScheduleDest = ed.choice("schedule"), ed.str("schedule_value"), ed.str("schedule_dest")
		s.ScheduleSource, s.ScheduleTTL = ed.str("schedule_source"), ed.str("schedule_ttl")
		if s.Count < 1 {
			s.Count = 1
		}
		for _, h := range s.Headers {
			if !strings.Contains(h, ":") {
				m.setError("headers are written Key:Value")
				return nil
			}
		}
		if s.Scheduled() {
			if err := validSchedule(s.Schedule, s.ScheduleValue); err != nil {
				m.setError(err.Error())
				return nil
			}
			if s.ScheduleDest == "" {
				m.setError("a schedule needs a destination subject")
				return nil
			}
		}
		return m.runPlan(cli.Publish(s), nil)
	})
}

// validSchedule checks the "when" of a schedule against its kind.
func validSchedule(kind, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("say when: a schedule needs a time, a duration or a cron line")
	}
	switch kind {
	case "at":
		if _, err := time.Parse(time.RFC3339, value); err != nil {
			return fmt.Errorf("the time is written as RFC3339, like 2026-09-03T18:00:00Z")
		}
	case "after", "every":
		if err := cli.ValidDuration(value); err != nil {
			return err
		}
	case "cron":
		if len(strings.Fields(value)) != 6 {
			return fmt.Errorf("the server wants a six-field cron line, seconds first: 0 */5 * * * *")
		}
	}
	return nil
}

func (m *Model) request(subject string) tea.Cmd {
	fields := []*field{
		section("Request"),
		textField("subject", "Subject", subject, "", "required", validSubject),
		textField("body", "Body", "", "the request", "(empty)", nil),
		listField("headers", "Headers", nil, "Key:Value pairs, comma-separated"),
		intField("replies", "Replies", 1, "how many replies to wait for (0: until the timeout)"),
		textField("timeout", "Timeout", "", "how long to wait (2s, 10s); blank: nats's default", "(default)", cli.ValidDuration),
		intField("count", "Count", 1, "send the request this many times"),
	}
	return m.openEditor(newEditor("Send a request", fields, m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
		s := cli.RequestSpec{Subject: ed.str("subject"), Body: ed.get("body").text, Headers: ed.list("headers"), Replies: ed.int64("replies"), Timeout: ed.str("timeout"), Count: ed.int64("count")}
		if s.Replies < 0 {
			s.Replies = 1
		}
		if s.Count < 1 {
			s.Count = 1
		}
		return m.runPlan(cli.Request(s), nil)
	})
}

func validSubject(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("a subject is required")
	}
	if strings.ContainsAny(s, " \t") {
		return fmt.Errorf("no spaces in a subject")
	}
	return nil
}

// ---------------------------------------------------------------- reports and monitoring

// runText runs a nats command behind the busy screen and shows its output.
func (m *Model) runText(title string, args ...string) tea.Cmd {
	r := m.runner
	if m.scr != scrText {
		m.textBack = m.scr
	}
	return m.busy("Running "+m.settings.CommandLine(args)+"…", func() tea.Msg {
		res := r.Run(args...)
		if !res.OK() {
			return textMsg{err: fmt.Errorf("%s: %s", strings.Join(args, " "), res.Message())}
		}
		out := res.Output()
		if out == "" {
			out = "(no output)"
		}
		return textMsg{title: title, text: styleMuted.Render("$ "+m.settings.CommandLine(args)) + "\n\n" + out}
	})
}

func (m *Model) accountInfo() tea.Cmd {
	return m.runText("Account information (nats account info)", "account", "info")
}

// runCheck runs a nats server check and shows its report: a warning or a
// critical state exits non-zero, which is the result, not a failure.
func (m *Model) runCheck(title string, args ...string) tea.Cmd {
	r := m.runner
	if m.scr != scrText {
		m.textBack = m.scr
	}
	args = append(args, "--format=text")
	return m.busy("Running "+m.settings.CommandLine(args)+"…", func() tea.Msg {
		res := r.Run(args...)
		out := res.Output()
		if !res.OK() && (out == "" || strings.Contains(out, "usage:")) {
			return textMsg{err: fmt.Errorf("%s: %s", strings.Join(args, " "), res.Message())}
		}
		return textMsg{title: title, text: styleMuted.Render("$ "+m.settings.CommandLine(args)) + "\n\n" + out}
	})
}

// monitor is the M menu: a control panel over the read-only commands,
// grouped the way nats groups them. The check of the selected entity is the
// first item of Check, so it is two keystrokes from the tree.
func (m *Model) monitor(n node) tea.Cmd {
	run := func(v string) func(*Model) tea.Cmd {
		return func(m *Model) tea.Cmd { return m.monitorRun(n, v) }
	}
	checks := []menuItem{}
	switch n.kind {
	case kStream:
		checks = append(checks, menuItem{Name: "This", Desc: "server check stream " + n.stream.Name() + " — sources, mirror and cluster peers of the stream", Run: run("checkstream")})
	case kConsumer:
		checks = append(checks, menuItem{Name: "This", Desc: "server check consumer " + n.cons.Name() + " — pending, waiting and redelivered messages", Run: run("checkconsumer")})
	case kKV:
		checks = append(checks, menuItem{Name: "This", Desc: "server check kv " + n.kv.Name() + " — the bucket answers and holds values", Run: run("checkkv")})
	}
	checks = append(checks,
		menuItem{Name: "Jetstream", Desc: "server check jetstream — the JetStream account state against its limits", Run: run("checkjs")},
		menuItem{Name: "Connection", Desc: "server check connection — the round trip, and that the server answers", Run: run("checkconn")},
		menuItem{Name: "Meta", Desc: "server check meta — the JetStream cluster: peers, lag, last seen (system account)", Run: run("checkmeta")},
		menuItem{Name: "Server", Key: "v", Desc: "server check server — one server: CPU, memory, connections, uptime (system account)", Run: run("checkserver")},
		menuItem{Name: "Request", Desc: "server check request — a request-reply service answers, in time and as expected", Run: run("checkrequest")},
		menuItem{Name: "Credential", Key: "e", Desc: "server check credential — a credential file is valid and not about to expire", Run: run("checkcred")},
	)
	items := []menuItem{
		{Name: "Report", Items: []menuItem{
			{Name: "Connections", Desc: "server report connections — connections and their traffic (system account)", Run: run("connz")},
			{Name: "Jetstream", Desc: "server report jetstream — JetStream usage per server (system account)", Run: run("jsz")},
			{Name: "Accounts", Desc: "server report accounts — accounts and their activity (system account)", Run: run("accounts")},
			{Name: "Health", Desc: "server report health — server health (system account)", Run: run("health")},
			{Name: "Cpu", Key: "p", Desc: "server report cpu — CPU usage per server (system account)", Run: run("cpu")},
			{Name: "Mem", Desc: "server report mem — memory usage per server (system account)", Run: run("mem")},
			{Name: "Routes", Desc: "server report routes — cluster routes and their traffic (system account)", Run: run("routes")},
			{Name: "Gateways", Desc: "server report gateways — super-cluster gateways (system account)", Run: run("gateways")},
			{Name: "Leafnodes", Desc: "server report leafnodes — leaf node connections (system account)", Run: run("leafnodes")},
			{Name: "Downgrade", Desc: "server report downgrade — assets a lower API level could not load (system account)", Run: run("downgrade")},
		}},
		{Name: "Check", Items: checks},
		{Name: "Server", Items: []menuItem{
			{Name: "List", Desc: "server list — every server of the cluster (system account)", Run: run("list")},
			{Name: "Info", Desc: "server info — the connected server (system account)", Run: run("info")},
			{Name: "Ping", Desc: "server ping — round trips to every server (system account)", Run: run("ping")},
			{Name: "Mappings", Desc: "server mappings — try a subject mapping on a subject", Run: run("mappings")},
			{Name: "Account", Desc: "server account info — one account as the servers see it (system account)", Run: run("sysaccount")},
		}},
		{Name: "Account", Items: []menuItem{
			{Name: "Info", Desc: "account info — this account's JetStream usage (any user)", Run: run("account")},
			{Name: "Connections", Desc: "account report connections — this account's connections", Run: run("acctconn")},
			{Name: "Tls", Desc: "account tls — the TLS certificate chain of the connection", Run: run("tls")},
		}},
		{Name: "Rtt", Key: "t", Desc: "rtt — round-trip times to the server (any user)", Run: run("rtt")},
	}
	return m.openMenu("M", "Monitor", items)
}

// monitorRun runs one leaf of the M menu.
func (m *Model) monitorRun(n node, v string) tea.Cmd {
	switch v {
	case "checkstream":
		return m.runCheck("Check of stream "+n.stream.Name(), "server", "check", "stream", "--stream="+n.stream.Name())
	case "checkconsumer":
		return m.runCheck("Check of consumer "+n.cons.Name(), "server", "check", "consumer", "--stream="+n.cons.Stream.Name(), "--consumer="+n.cons.Name())
	case "checkkv":
		return m.runCheck("Check of bucket "+n.kv.Name(), "server", "check", "kv", "--bucket="+n.kv.Name())
	case "list":
		return m.runText("Servers (nats server list)", "server", "list")
	case "info":
		return m.runText("Server (nats server info)", "server", "info")
	case "ping":
		return m.runText("Ping (nats server ping)", "server", "ping")
	case "connz":
		return m.runText("Connections (nats server report connections)", "server", "report", "connections")
	case "jsz":
		return m.runText("JetStream (nats server report jetstream)", "server", "report", "jetstream")
	case "accounts":
		return m.runText("Accounts (nats server report accounts)", "server", "report", "accounts")
	case "health":
		return m.runText("Health (nats server report health)", "server", "report", "health")
	case "cpu":
		return m.runText("CPU (nats server report cpu)", "server", "report", "cpu")
	case "mem":
		return m.runText("Memory (nats server report mem)", "server", "report", "mem")
	case "routes":
		return m.runText("Routes (nats server report routes)", "server", "report", "routes")
	case "gateways":
		return m.runText("Gateways (nats server report gateways)", "server", "report", "gateways")
	case "leafnodes":
		return m.runText("Leaf nodes (nats server report leafnodes)", "server", "report", "leafnodes")
	case "downgrade":
		m.formVals.str = "1"
		return m.openForm(inputForm("Target API level", "nats server report downgrade lists the streams and consumers a server of this JetStream API level could not load.", "1", &m.formVals.str, validInt("an API level")), func(m *Model) tea.Cmd {
			return m.runText("Downgrade to API level "+m.formVals.str, "server", "report", "downgrade", m.formVals.str)
		}, nil)
	case "sysaccount":
		m.formVals.str = ""
		return m.openForm(inputForm("Account", "The name of the account nats server account info describes.", "ACME", &m.formVals.str, nonEmpty("an account name")), func(m *Model) tea.Cmd {
			return m.runText("Account "+m.formVals.str+" (nats server account info)", "server", "account", "info", strings.TrimSpace(m.formVals.str))
		}, nil)
	case "checkjs":
		return m.runCheck("JetStream check (nats server check jetstream)", "server", "check", "jetstream")
	case "checkconn":
		return m.runCheck("Connection check (nats server check connection)", "server", "check", "connection")
	case "checkmeta":
		return m.checkMeta()
	case "checkserver":
		return m.checkServer()
	case "checkrequest":
		return m.checkRequest(defaultSubject(n))
	case "checkcred":
		return m.checkCredential()
	case "mappings":
		return m.tryMapping()
	case "rtt":
		return m.runText("Round-trip times (nats rtt)", "rtt")
	case "account":
		return m.accountInfo()
	case "acctconn":
		return m.runText("Connections (nats account report connections)", "account", "report", "connections")
	case "tls":
		return m.runText("TLS (nats account tls)", "account", "tls")
	}
	return nil
}

func validInt(what string) func(string) error {
	return func(s string) error {
		if _, err := strconv.Atoi(strings.TrimSpace(s)); err != nil {
			return fmt.Errorf("%s is a number", what)
		}
		return nil
	}
}

// checkMeta asks for the three thresholds nats server check meta requires.
func (m *Model) checkMeta() tea.Cmd {
	fields := []*field{
		section("JetStream cluster check (nats server check meta)"),
		intField("expect", "Servers expected", 3, "the number of JetStream peers the meta group should have"),
		intField("lag", "Lag critical", 100, "critical when a peer is this many operations behind the leader"),
		textField("seen", "Seen critical", "1m", "critical when a peer was last seen longer ago than this", "required", cli.ValidDuration),
	}
	return m.openEditor(newEditor("Check the JetStream cluster", fields, m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
		return m.runCheck("JetStream cluster check (nats server check meta)", "server", "check", "meta", "--expect="+ed.str("expect"), "--lag-critical="+ed.str("lag"), "--seen-critical="+ed.str("seen"))
	})
}

// checkServer checks one server, the connected one unless another is named;
// the thresholds are optional and passed only when given.
func (m *Model) checkServer() tea.Cmd {
	name := ""
	if m.store != nil {
		name = m.store.Server.Name
	}
	fields := []*field{
		section("Server check (nats server check server)"),
		textField("name", "Server", name, "the server name the check must find", "required", nonEmpty("a server name")),
		textField("cpu", "CPU warn / critical", "", "percentages, for example 80,90", "(none)", nil),
		textField("mem", "Memory warn / critical", "", "sizes, for example 1g,2g", "(none)", nil),
		textField("conn", "Connections warn / critical", "", "counts", "(none)", nil),
		textField("subs", "Subscriptions warn / critical", "", "counts", "(none)", nil),
		textField("uptime", "Uptime warn / critical", "", "durations, for example 10m,1m: less than this is a problem", "(none)", nil),
	}
	return m.openEditor(newEditor("Check a server", fields, m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
		args := []string{"server", "check", "server", "--name=" + ed.str("name")}
		for _, k := range []string{"cpu", "mem", "conn", "subs", "uptime"} {
			pair := cli.SplitList(ed.str(k))
			if len(pair) > 0 && pair[0] != "" {
				args = append(args, "--"+k+"-warn="+pair[0])
			}
			if len(pair) > 1 && pair[1] != "" {
				args = append(args, "--"+k+"-critical="+pair[1])
			}
		}
		return m.runCheck("Check of server "+ed.str("name"), args...)
	})
}

// checkRequest sends a request and checks the answer.
func (m *Model) checkRequest(subject string) tea.Cmd {
	fields := []*field{
		section("Request check (nats server check request)"),
		textField("subject", "Subject", subject, "", "required", validSubject),
		textField("payload", "Payload", "", "the request body", "(empty)", nil),
		textField("match", "Response matches", "", "a regular expression the reply body must match", "(any)", nil),
		textField("warn", "Response time warn", "", "a duration", "(none)", cli.ValidDuration),
		textField("critical", "Response time critical", "", "a duration", "(none)", cli.ValidDuration),
	}
	return m.openEditor(newEditor("Check a request-reply service", fields, m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
		f := []string{"server", "check", "request", "--subject=" + ed.str("subject")}
		if v := ed.get("payload").text; v != "" {
			f = append(f, "--payload="+v)
		}
		if v := ed.str("match"); v != "" {
			f = append(f, "--match-payload="+v)
		}
		if v := ed.str("warn"); v != "" {
			f = append(f, "--response-warn="+v)
		}
		if v := ed.str("critical"); v != "" {
			f = append(f, "--response-critical="+v)
		}
		return m.runCheck("Check of requests to "+ed.str("subject"), f...)
	})
}

// checkCredential checks a credential file, the context's own by default.
func (m *Model) checkCredential() tea.Cmd {
	creds := m.settings.Creds
	if creds == "" && m.store != nil {
		creds = m.store.Context.Creds
	}
	fields := []*field{
		section("Credential check (nats server check credential)"),
		textField("file", "Credential file", creds, "", "required", existingFile),
		textField("warn", "Validity warn", "", "warn when it expires sooner than this", "(none)", cli.ValidDuration),
		textField("critical", "Validity critical", "", "critical when it expires sooner than this", "(none)", cli.ValidDuration),
		boolField("expiry", "Require an expiry", false, "a credential that never expires is a failure"),
	}
	return m.openEditor(newEditor("Check a credential", fields, m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
		f := []string{"server", "check", "credential", "--credential=" + expandPath(ed.str("file"))}
		if v := ed.str("warn"); v != "" {
			f = append(f, "--validity-warn="+v)
		}
		if v := ed.str("critical"); v != "" {
			f = append(f, "--validity-critical="+v)
		}
		if ed.on("expiry") {
			f = append(f, "--require-expiry")
		}
		return m.runCheck("Check of "+ed.str("file"), f...)
	})
}

// tryMapping runs nats server mappings on a source pattern, a destination
// pattern and a subject, and shows the transformed subject.
func (m *Model) tryMapping() tea.Cmd {
	fields := []*field{
		section("Subject mapping (nats server mappings)"),
		textField("source", "Source pattern", "orders.*", "the pattern the subject is matched against", "required", nonEmpty("a source pattern")),
		textField("dest", "Destination pattern", "new.{{wildcard(1)}}", "the pattern the subject becomes; {{wildcard(n)}}, {{partition(n,…)}}, {{split(n,sep)}}…", "required", nonEmpty("a destination pattern")),
		textField("subject", "Subject", "orders.paris", "the subject to transform", "required", nonEmpty("a subject")),
	}
	return m.openEditor(newEditor("Try a subject mapping", fields, m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
		return m.runText("Mapping of "+ed.str("subject"), "server", "mappings", ed.str("source"), ed.str("dest"), ed.str("subject"))
	})
}

func (m *Model) reports(n node) tea.Cmd {
	run := func(v string) func(*Model) tea.Cmd {
		return func(m *Model) tea.Cmd { return m.reportsRun(n, v) }
	}
	items := []menuItem{
		{Name: "Streams", Desc: "stream report — every stream with its storage, messages and replicas", Run: run("streams")},
		{Name: "Consumers", Desc: "consumer report — the consumers of a stream with their state", Run: run("consumers")},
		{Name: "Account", Desc: "account report statistics — server statistics for this account", Run: run("stats")},
		{Name: "Find", Items: []menuItem{
			{Name: "Streams", Desc: "stream find — streams matching criteria (empty, idle, mirrored…)", Run: run("find")},
			{Name: "Consumers", Desc: "consumer find — consumers of a stream matching criteria (pull, idle, pending…)", Run: run("findconsumer")},
		}},
		{Name: "Gaps", Desc: "stream gaps — gaps in a stream's sequence that would show as deleted messages", Run: run("gaps")},
	}
	if n.kind == kService {
		items = append(items, menuItem{Name: "Service", Key: "v", Items: []menuItem{
			{Name: "Info", Desc: "service info " + n.svc.Info.Name + " — endpoints of the service", Run: run("svcinfo")},
			{Name: "Stats", Desc: "service stats " + n.svc.Info.Name + " — request counts and timings", Run: run("svcstats")},
		}})
	}
	return m.openMenu("T", "Reports", items)
}

// reportsRun runs one leaf of the T menu.
func (m *Model) reportsRun(n node, v string) tea.Cmd {
	switch v {
	case "streams":
		return m.runText("Stream report (nats stream report)", "stream", "report", "-a")
	case "consumers":
		if st := n.streamOf(); st != nil {
			return m.runText("Consumer report of "+st.Name(), "consumer", "report", st.Name())
		}
		return m.pickStream("Consumer report of which stream?", func(m *Model, st *cli.Stream) tea.Cmd {
			return m.runText("Consumer report of "+st.Name(), "consumer", "report", st.Name())
		})
	case "stats":
		return m.runText("Statistics (nats account report statistics)", "account", "report", "statistics")
	case "find":
		m.formVals.str = "--empty"
		return m.openForm(inputForm("nats stream find", "Flags of nats stream find: --empty, --idle 1h, --created 7d, --mirrored, --sourced, --subject x.>, --expression '…'", "--empty", &m.formVals.str, nil), func(m *Model) tea.Cmd {
			return m.runText("nats stream find "+m.formVals.str, append([]string{"stream", "find"}, strings.Fields(m.formVals.str)...)...)
		}, nil)
	case "findconsumer":
		find := func(m *Model, st *cli.Stream) tea.Cmd {
			m.formVals.str = "--pull"
			return m.openForm(inputForm("nats consumer find "+st.Name(), "Flags of nats consumer find: --pull, --push, --bound, --idle 1h, --created 7d, --pending 100, --ack-pending 10, --waiting 5, --replicas 1, --pinned, --invert, --expression '…'", "--pull", &m.formVals.str, nil), func(m *Model) tea.Cmd {
				return m.runText("nats consumer find "+st.Name()+" "+m.formVals.str, append([]string{"consumer", "find", st.Name()}, strings.Fields(m.formVals.str)...)...)
			}, nil)
		}
		if st := n.streamOf(); st != nil {
			return find(m, st)
		}
		return m.pickStream("Find consumers of which stream?", find)
	case "gaps":
		gaps := func(m *Model, st *cli.Stream) tea.Cmd {
			return m.runText("Gaps of "+st.Name()+" (nats stream gaps)", "stream", "gaps", st.Name(), "-f", "--no-progress")
		}
		if st := n.streamOf(); st != nil {
			return gaps(m, st)
		}
		return m.pickStream("Gaps of which stream?", gaps)
	case "svcinfo":
		return m.runText("Service "+n.svc.Info.Name, "service", "info", n.svc.Info.Name, n.svc.Info.ID)
	case "svcstats":
		return m.runText("Statistics of "+n.svc.Info.Name, "service", "stats", n.svc.Info.Name, n.svc.Info.ID)
	}
	return nil
}

// ---------------------------------------------------------------- cluster administration

// cluster is the L menu: the RAFT and server administration commands, the
// ones for the selected stream or consumer first. They change the cluster
// rather than an entity, so each one is a plan like any other.
func (m *Model) cluster(n node) tea.Cmd {
	run := func(v string) func(*Model) tea.Cmd {
		return func(m *Model) tea.Cmd { return m.clusterRun(n, v) }
	}
	var items []menuItem
	switch n.kind {
	case kStream:
		items = append(items,
			menuItem{Name: "Stepdown", Desc: "stream cluster step-down " + n.stream.Name() + " — elect a new leader for the stream", Run: run("streamdown")},
			menuItem{Name: "Peer", Desc: "stream cluster peer-remove " + n.stream.Name() + " — move the stream away from one of its servers", Run: run("streampeer")},
		)
	case kConsumer:
		items = append(items,
			menuItem{Name: "Stepdown", Desc: "consumer cluster step-down " + n.cons.Name() + " — elect a new leader for the consumer", Run: run("consumerdown")},
			menuItem{Name: "Reset", Desc: "consumer reset " + n.cons.Name() + " — deliver again from a sequence, or the outstanding messages", Run: run("reset")},
			menuItem{Name: "Unpin", Desc: "consumer unpin " + n.cons.Name() + " — release the client pinned to a priority group", Run: run("unpin")},
		)
	}
	items = append(items,
		menuItem{Name: "Balance", Items: []menuItem{
			{Name: "Streams", Desc: "stream cluster balance — spread the stream leaders over the servers", Run: run("balancestreams")},
			{Name: "Consumers", Desc: "consumer cluster balance — spread the consumer leaders of a stream", Run: run("balanceconsumers")},
		}},
		menuItem{Name: "Meta", Items: []menuItem{
			{Name: "Stepdown", Desc: "server cluster step-down — elect a new JetStream meta leader (system account)", Run: run("metadown")},
			{Name: "Peer", Desc: "server cluster peer-remove — remove a server from the JetStream cluster (system account)", Run: run("serverpeer")},
		}},
		menuItem{Name: "Reload", Key: "l", Desc: "server config reload — make a server re-read its configuration (system account)", Run: run("reload")},
		menuItem{Name: "Kick", Desc: "server request kick — disconnect a client (system account)", Run: run("kick")},
		menuItem{Name: "Purge", Key: "g", Desc: "server account purge — delete every JetStream asset of an account (system account)", Run: run("purge")},
	)
	return m.openMenu("L", "Cluster", items)
}

// clusterRun runs one leaf of the L menu.
func (m *Model) clusterRun(n node, v string) tea.Cmd {
	switch v {
	case "streamdown":
		return m.askPreferred("Step down the leader of "+n.stream.Name(), func(m *Model, host string) tea.Cmd {
			return m.runPlan(cli.StepDownStream(n.stream.Name(), host), nil)
		})
	case "streampeer":
		return m.askString("Peer to remove from "+n.stream.Name(), "The server name of the peer; the stream is placed on another server.", "", nonEmpty("a server name"), func(m *Model, peer string) tea.Cmd {
			return m.runPlan(cli.RemoveStreamPeer(n.stream.Name(), peer), nil)
		})
	case "consumerdown":
		return m.askPreferred("Step down the leader of "+n.cons.Name(), func(m *Model, host string) tea.Cmd {
			return m.runPlan(cli.StepDownConsumer(n.cons.Stream.Name(), n.cons.Name(), host), nil)
		})
	case "reset":
		return m.askString("Reset "+n.cons.Name()+" to stream sequence", fmt.Sprintf("Delivery starts again from this sequence; blank keeps the position and delivers the outstanding messages again. Last delivered: %d, acknowledged up to: %d.", n.cons.Info.Delivered.Stream, n.cons.Info.AckFloor.Stream), "", optional(validInt("a sequence")), func(m *Model, seq string) tea.Cmd {
			var v uint64
			if seq != "" {
				v, _ = strconv.ParseUint(seq, 10, 64)
			}
			return m.runPlan(cli.ResetConsumer(n.cons.Stream.Name(), n.cons.Name(), v), nil)
		})
	case "unpin":
		return m.askString("Priority group to unpin on "+n.cons.Name(), "The pinned client of the group is released and another one takes its place.", "", nonEmpty("a group name"), func(m *Model, group string) tea.Cmd {
			return m.runPlan(cli.UnpinConsumer(n.cons.Stream.Name(), n.cons.Name(), group), nil)
		})
	case "balancestreams":
		return m.askString("nats stream cluster balance", "Flags selecting the streams: --server-name x, --cluster x, --empty, --idle 1h, --created 7d, --consumers 5, --subject x.>, --replicas 3, --sourced, --mirrored, --leader x, --invert, --expression '…'; blank balances every stream.", "", nil, func(m *Model, flags string) tea.Cmd {
			return m.runPlan(cli.BalanceStreams(strings.Fields(flags)), nil)
		})
	case "balanceconsumers":
		balance := func(m *Model, st *cli.Stream) tea.Cmd {
			return m.askString("nats consumer cluster balance "+st.Name(), "Flags selecting the consumers: --pull, --push, --bound, --waiting 5, --ack-pending 10, --pending 100, --idle 1h, --created 7d, --replicas 3, --leader x, --pinned, --invert; blank balances every consumer.", "", nil, func(m *Model, flags string) tea.Cmd {
				return m.runPlan(cli.BalanceConsumers(st.Name(), strings.Fields(flags)), nil)
			})
		}
		if st := n.streamOf(); st != nil {
			return balance(m, st)
		}
		return m.pickStream("Balance the consumers of which stream?", balance)
	case "metadown":
		fields := []*field{
			section("Where the new meta leader should be"),
			textField("cluster", "Cluster", "", "", "(any)", nil),
			textField("host", "Host", "", "a server name", "(any)", nil),
			listField("tags", "Tags", nil, "servers holding these tags"),
		}
		return m.openEditor(newEditor("Step down the JetStream meta leader", fields, m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
			return m.runPlan(cli.StepDownMeta(ed.str("cluster"), ed.str("host"), ed.list("tags")), nil)
		})
	case "serverpeer":
		return m.askString("Server to remove from the JetStream cluster", "Its name or ID; every stream and consumer it holds is moved to the remaining servers.", "", nonEmpty("a server name"), func(m *Model, name string) tea.Cmd {
			return m.runPlan(cli.RemoveServerPeer(name), nil)
		})
	case "reload":
		return m.askString("Server to reload", "The ID of the server that re-reads its configuration file.", m.serverID(), nonEmpty("a server ID"), func(m *Model, id string) tea.Cmd {
			return m.runPlan(cli.ReloadConfig(id), nil)
		})
	case "kick":
		fields := []*field{
			section("Disconnect a client"),
			textField("client", "Client ID", "", "the CID nats server report connections shows", "required", validInt("a client ID")),
			textField("server", "Server ID", m.serverID(), "the server the client is connected to", "required", nonEmpty("a server ID")),
		}
		return m.openEditor(newEditor("Kick a client", fields, m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
			return m.runPlan(cli.KickClient(ed.str("client"), ed.str("server")), nil)
		})
	case "purge":
		return m.askString("Account to purge", "Every stream and consumer of the account is deleted from the cluster.", "", nonEmpty("an account name"), func(m *Model, name string) tea.Cmd {
			return m.runPlan(cli.PurgeAccount(name), nil)
		})
	}
	return nil
}

// serverID is the ID of the connected server, the default for the
// commands that address one.
func (m *Model) serverID() string {
	if m.store == nil {
		return ""
	}
	return m.store.Server.ID
}

// askString asks for one string in a form and passes it, trimmed, on.
func (m *Model) askString(title, desc, initial string, validate func(string) error, done func(m *Model, s string) tea.Cmd) tea.Cmd {
	m.formVals.str = initial
	return m.openForm(inputForm(title, desc, "", &m.formVals.str, validate), func(m *Model) tea.Cmd {
		return done(m, strings.TrimSpace(m.formVals.str))
	}, nil)
}

// askPreferred asks for the host a new leader should be placed on.
func (m *Model) askPreferred(title string, done func(m *Model, host string) tea.Cmd) tea.Cmd {
	return m.askString(title, "The server name the new leader should be placed on; blank lets the cluster choose.", "", nil, done)
}

// optional accepts a blank value and validates the rest.
func optional(validate func(string) error) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return nil
		}
		return validate(s)
	}
}

// ---------------------------------------------------------------- backup, restore, copy

func (m *Model) backupStream(st *cli.Stream) tea.Cmd {
	fields := []*field{
		section("Backup stream " + st.Name()),
		infoField("Messages", fmt.Sprintf("%s, %s", cli.Count(st.Info.State.Msgs), cli.Size(st.Info.State.Bytes))),
		textField("dir", "Directory", "./"+st.Name()+"-backup", "written by nats stream backup (it must not exist)", "required", nonEmpty("a directory")),
		boolField("consumers", "Include the consumers", true, ""),
	}
	return m.openEditor(newEditor("Backup stream "+st.Name(), fields, m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
		return m.runPlan(cli.BackupStream(st.Name(), expandPath(ed.str("dir")), ed.on("consumers")), nil)
	})
}

// backupAccount writes every stream of the account, one directory each.
func (m *Model) backupAccount() tea.Cmd {
	if !m.needJetStream() {
		return nil
	}
	fields := []*field{
		section("Backup every stream of the account"),
		infoField("Streams", fmt.Sprintf("%d, one subdirectory each", len(m.store.Streams))),
		textField("dir", "Directory", "./nats-backup", "written by nats account backup", "required", nonEmpty("a directory")),
		boolField("consumers", "Include the consumers", true, ""),
		boolField("check", "Check each stream first", false, "nats account backup --check: the health check before the backup"),
	}
	return m.openEditor(newEditor("Backup the account", fields, m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
		return m.runPlan(cli.BackupAccount(expandPath(ed.str("dir")), ed.on("consumers"), ed.on("check")), nil)
	})
}

// restoreBackup restores a directory: a stream backup, or an account
// backup holding one stream backup per subdirectory.
func (m *Model) restoreBackup() tea.Cmd {
	if !m.needJetStream() {
		return nil
	}
	m.formVals.str = ""
	return m.openForm(inputForm("Restore from", "The directory a stream backup (b on a stream) or an account backup (b elsewhere) was written to; the streams must not exist.", "./backup", &m.formVals.str, func(s string) error {
		s = expandPath(strings.TrimSpace(s))
		if st, err := os.Stat(s); err != nil || !st.IsDir() {
			return fmt.Errorf("not a directory: %s", s)
		}
		if cli.BackupKind(s) == "" {
			return fmt.Errorf("no backup.json in %s or its subdirectories", s)
		}
		return nil
	}), func(m *Model) tea.Cmd {
		dir := expandPath(strings.TrimSpace(m.formVals.str))
		if cli.BackupKind(dir) == "account" {
			return m.runPlan(cli.RestoreAccount(dir), nil)
		}
		return m.runPlan(cli.RestoreStream(dir), nil)
	}, nil)
}

// copyConsumer creates a consumer with the configuration of another.
func (m *Model) copyConsumer(c *cli.Consumer) tea.Cmd {
	fields := []*field{
		section("Copy the configuration of " + c.Name()),
		infoField("Copied", "the configuration only: the copy starts from its deliver policy, not from where the source is"),
		textField("name", "New consumer", c.Name()+"_COPY", "", "required", func(s string) error {
			if err := validName(s); err != nil {
				return err
			}
			for _, o := range c.Stream.Consumers {
				if o.Name() == strings.TrimSpace(s) {
					return fmt.Errorf("consumer %s already exists", strings.TrimSpace(s))
				}
			}
			return nil
		}),
	}
	return m.openEditor(newEditor("Copy consumer "+c.Name(), fields, m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
		return m.runPlan(cli.CopyConsumer(c.Stream.Name(), c.Name(), ed.str("name")), nil)
	})
}

func (m *Model) copyStream(st *cli.Stream) tea.Cmd {
	fields := []*field{
		section("Copy the configuration of " + st.Name()),
		infoField("Copied", "the configuration only: no messages, no consumers"),
		textField("name", "New stream", st.Name()+"_COPY", "", "required", func(s string) error {
			if err := validStreamName(s); err != nil {
				return err
			}
			if m.store.Stream(strings.TrimSpace(s)) != nil {
				return fmt.Errorf("stream %s already exists", strings.TrimSpace(s))
			}
			return nil
		}),
		listField("subjects", "Subjects", nil, "the copy needs subjects of its own: two streams cannot share a subject, and the source holds "+cli.JoinList(st.Info.Config.Subjects)),
	}
	return m.openEditor(newEditor("Copy stream "+st.Name(), fields, m.width, m.height), func(m *Model, ed *editor) tea.Cmd {
		subjects := ed.list("subjects")
		if len(subjects) == 0 && len(st.Info.Config.Subjects) > 0 {
			m.setError("the copy needs subjects of its own: the server refuses a second stream on " + cli.JoinList(st.Info.Config.Subjects))
			return nil
		}
		if err := m.subjectClash(subjects, ""); err != nil {
			m.setError(err.Error())
			return nil
		}
		return m.runPlan(cli.CopyStream(st.Name(), ed.str("name"), subjects), nil)
	})
}
