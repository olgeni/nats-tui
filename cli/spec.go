package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// The specs are what the editors edit: the settings of a stream, consumer
// or bucket in the form the nats flags take them. A spec is built from the
// server's configuration, edited, and the difference between old and new
// becomes the flags of one nats command — so the preview shows exactly what
// changes and nothing else.

// ---------------------------------------------------------------- streams

// Storage, retention, discard and compression choices, as nats spells them.
var (
	StorageTypes    = []string{"file", "memory"}
	RetentionTypes  = []string{"limits", "interest", "work"}
	DiscardTypes    = []string{"old", "new"}
	CompressionAlgs = []string{"none", "s2"}
	AckPolicies     = []string{"explicit", "all", "none"}
	ReplayPolicies  = []string{"instant", "original"}
)

// StreamSpec is the configuration of a stream as nats stream add/edit
// takes it. Durations are text ("1h", "2m30s"; "" unlimited), sizes and
// counts are -1 for unlimited.
type StreamSpec struct {
	Name              string
	Description       string
	Subjects          []string
	Storage           string
	Retention         string
	Discard           string
	DiscardPerSubject bool
	Compression       string
	Replicas          int64
	MaxMsgs           int64
	MaxMsgsPerSubject int64
	MaxBytes          int64
	MaxAge            string
	MaxMsgSize        int64
	MaxConsumers      int64
	DupeWindow        string
	Ack               bool // --[no-]ack (NoAck inverted)
	AllowRollup       bool
	DenyDelete        bool
	DenyPurge         bool
	AllowDirect       bool
	MirrorDirect      bool
	AllowMsgTTL       bool
	AllowSchedules    bool
	AllowBatch        bool
	AllowAtomic       bool
	Mirror            string
	Sources           []string
	RepubSource       string
	RepubDest         string
	RepubHeaders      bool
	TransformSource   string
	TransformDest     string
	Metadata          []string
	Cluster           string
	Tags              []string
	FirstSeq          uint64 // add only
	Sealed            bool   // read-only here
}

// NewStreamSpec is what the add editor starts from: nats's own defaults.
func NewStreamSpec() StreamSpec {
	return StreamSpec{Storage: "file", Retention: "limits", Discard: "old", Compression: "none", Replicas: 1,
		MaxMsgs: -1, MaxMsgsPerSubject: -1, MaxBytes: -1, MaxMsgSize: -1, MaxConsumers: -1, DupeWindow: "2m", Ack: true, AllowDirect: true}
}

// StreamSpecFrom reads the spec of an existing stream.
func StreamSpecFrom(c jetstream.StreamConfig) StreamSpec {
	s := StreamSpec{
		Name: c.Name, Description: c.Description, Subjects: append([]string{}, c.Subjects...),
		Storage: storageName(c.Storage), Retention: retentionName(c.Retention), Discard: discardName(c.Discard),
		DiscardPerSubject: c.DiscardNewPerSubject, Compression: compressionName(c.Compression), Replicas: int64(c.Replicas),
		MaxMsgs: c.MaxMsgs, MaxMsgsPerSubject: c.MaxMsgsPerSubject, MaxBytes: c.MaxBytes, MaxAge: DurationText(c.MaxAge),
		MaxMsgSize: int64(c.MaxMsgSize), MaxConsumers: int64(c.MaxConsumers), DupeWindow: DurationText(c.Duplicates),
		Ack: !c.NoAck, AllowRollup: c.AllowRollup, DenyDelete: c.DenyDelete, DenyPurge: c.DenyPurge,
		AllowDirect: c.AllowDirect, MirrorDirect: c.MirrorDirect, AllowMsgTTL: c.AllowMsgTTL, AllowSchedules: c.AllowMsgSchedules, AllowBatch: c.AllowBatchPublish, AllowAtomic: c.AllowAtomicPublish,
		Metadata: Metadata(c.Metadata), FirstSeq: c.FirstSeq, Sealed: c.Sealed,
	}
	if s.Replicas == 0 {
		s.Replicas = 1
	}
	if c.Mirror != nil {
		s.Mirror = c.Mirror.Name
	}
	for _, src := range c.Sources {
		s.Sources = append(s.Sources, src.Name)
	}
	if c.RePublish != nil {
		s.RepubSource, s.RepubDest, s.RepubHeaders = c.RePublish.Source, c.RePublish.Destination, c.RePublish.HeadersOnly
	}
	if c.SubjectTransform != nil {
		s.TransformSource, s.TransformDest = c.SubjectTransform.Source, c.SubjectTransform.Destination
	}
	if c.Placement != nil {
		s.Cluster, s.Tags = c.Placement.Cluster, append([]string{}, c.Placement.Tags...)
	}
	return s
}

func storageName(st jetstream.StorageType) string { return StorageName(st) }

// StorageName is a storage type as nats spells it.
func StorageName(st jetstream.StorageType) string {
	if st == jetstream.MemoryStorage {
		return "memory"
	}
	return "file"
}

func discardName(d jetstream.DiscardPolicy) string {
	if d == jetstream.DiscardNew {
		return "new"
	}
	return "old"
}

func compressionName(c jetstream.StoreCompression) string {
	if c == jetstream.S2Compression {
		return "s2"
	}
	return "none"
}

func retentionName(r jetstream.RetentionPolicy) string {
	switch r {
	case jetstream.InterestPolicy:
		return "interest"
	case jetstream.WorkQueuePolicy:
		return "work"
	}
	return "limits"
}

// streamFlags renders every setting of a spec as flags (used for add), or
// only the ones that differ from old (used for edit).
func streamFlags(old *StreamSpec, s StreamSpec, add bool) []string {
	f := &flagSet{}
	changed := func(a, b any) bool { return old == nil || a != b }
	if changed(oldStr(old, func(o StreamSpec) string { return o.Description }), s.Description) {
		if s.Description != "" || !add {
			f.strAlways("--description", s.Description)
		}
	}
	if add || !sameList(old.Subjects, s.Subjects) {
		f.list("--subjects", s.Subjects)
	}
	if add {
		f.str("--storage", s.Storage)
	}
	if changed(oldStr(old, func(o StreamSpec) string { return o.Retention }), s.Retention) {
		f.str("--retention", s.Retention)
	}
	if changed(oldStr(old, func(o StreamSpec) string { return o.Discard }), s.Discard) {
		f.str("--discard", s.Discard)
	}
	if changed(oldBool(old, func(o StreamSpec) bool { return o.DiscardPerSubject }), s.DiscardPerSubject) {
		if s.DiscardPerSubject || !add {
			f.noBool("--discard-per-subject", s.DiscardPerSubject)
		}
	}
	if changed(oldStr(old, func(o StreamSpec) string { return o.Compression }), s.Compression) {
		if s.Compression != "none" || !add {
			f.str("--compression", s.Compression)
		}
	}
	if changed(oldInt(old, func(o StreamSpec) int64 { return o.Replicas }), s.Replicas) {
		f.str("--replicas", fmt.Sprint(s.Replicas))
	}
	if changed(oldInt(old, func(o StreamSpec) int64 { return o.MaxMsgs }), s.MaxMsgs) {
		f.str("--max-msgs", fmt.Sprint(s.MaxMsgs))
	}
	if changed(oldInt(old, func(o StreamSpec) int64 { return o.MaxMsgsPerSubject }), s.MaxMsgsPerSubject) {
		f.str("--max-msgs-per-subject", fmt.Sprint(s.MaxMsgsPerSubject))
	}
	if changed(oldInt(old, func(o StreamSpec) int64 { return o.MaxBytes }), s.MaxBytes) {
		f.str("--max-bytes", fmt.Sprint(s.MaxBytes))
	}
	if changed(oldStr(old, func(o StreamSpec) string { return o.MaxAge }), s.MaxAge) {
		f.strAlways("--max-age", durationFlag(s.MaxAge))
	}
	if changed(oldInt(old, func(o StreamSpec) int64 { return o.MaxMsgSize }), s.MaxMsgSize) {
		f.str("--max-msg-size", fmt.Sprint(s.MaxMsgSize))
	}
	if changed(oldInt(old, func(o StreamSpec) int64 { return o.MaxConsumers }), s.MaxConsumers) {
		f.str("--max-consumers", fmt.Sprint(s.MaxConsumers))
	}
	if changed(oldStr(old, func(o StreamSpec) string { return o.DupeWindow }), s.DupeWindow) {
		f.strAlways("--dupe-window", durationFlag(s.DupeWindow))
	}
	for _, b := range []struct {
		flag string
		get  func(StreamSpec) bool
		def  bool
	}{
		{"--ack", func(o StreamSpec) bool { return o.Ack }, true},
		{"--allow-rollup", func(o StreamSpec) bool { return o.AllowRollup }, false},
		{"--deny-delete", func(o StreamSpec) bool { return o.DenyDelete }, false},
		{"--deny-purge", func(o StreamSpec) bool { return o.DenyPurge }, false},
		{"--allow-direct", func(o StreamSpec) bool { return o.AllowDirect }, true},
		{"--allow-mirror-direct", func(o StreamSpec) bool { return o.MirrorDirect }, false},
		{"--allow-batch", func(o StreamSpec) bool { return o.AllowBatch }, false},
	} {
		v := b.get(s)
		if old == nil && v != b.def || old != nil && b.get(*old) != v {
			f.noBool(b.flag, v)
		}
	}
	// flags without a --no- form: only ever turned on
	if s.AllowMsgTTL && (old == nil || !old.AllowMsgTTL) {
		f.bool("--allow-msg-ttl", true)
	}
	if s.AllowSchedules && (old == nil || !old.AllowSchedules) {
		f.bool("--allow-schedules", true)
	}
	if changed(oldStr(old, func(o StreamSpec) string { return o.Mirror }), s.Mirror) {
		if s.Mirror != "" {
			f.str("--mirror", s.Mirror)
		} else if !add {
			f.bool("--no-mirror", true)
		}
	}
	if old == nil || !sameList(old.Sources, s.Sources) {
		f.each("--source", s.Sources)
	}
	if changed(oldStr(old, func(o StreamSpec) string { return o.RepubSource + "\x00" + o.RepubDest }), s.RepubSource+"\x00"+s.RepubDest) ||
		changed(oldBool(old, func(o StreamSpec) bool { return o.RepubHeaders }), s.RepubHeaders) {
		if s.RepubDest != "" {
			f.str("--republish-source", orAll(s.RepubSource))
			f.str("--republish-destination", s.RepubDest)
			f.bool("--republish-headers", s.RepubHeaders)
		} else if !add {
			f.bool("--no-republish", true)
		}
	}
	if changed(oldStr(old, func(o StreamSpec) string { return o.TransformSource + "\x00" + o.TransformDest }), s.TransformSource+"\x00"+s.TransformDest) {
		if s.TransformDest != "" {
			f.str("--transform-source", s.TransformSource)
			f.str("--transform-destination", s.TransformDest)
		} else if !add {
			f.bool("--no-transform", true)
		}
	}
	if old == nil || !sameList(old.Metadata, s.Metadata) {
		f.each("--metadata", s.Metadata)
	}
	if changed(oldStr(old, func(o StreamSpec) string { return o.Cluster }), s.Cluster) {
		f.str("--cluster", s.Cluster)
	}
	if old == nil || !sameList(old.Tags, s.Tags) {
		f.each("--tag", s.Tags)
	}
	if add && s.FirstSeq > 0 {
		f.str("--first-sequence", fmt.Sprint(s.FirstSeq))
	}
	return f.args
}

// orAll is a republish source: nats wants one, and ">" is every subject.
func orAll(s string) string {
	if s == "" {
		return ">"
	}
	return s
}

// durationFlag is how an unlimited duration is passed: nats reads 0 as
// "no limit" for --max-age and the like (-1 is refused by stream edit).
func durationFlag(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

func oldStr(old *StreamSpec, get func(StreamSpec) string) string {
	if old == nil {
		return ""
	}
	return get(*old)
}
func oldBool(old *StreamSpec, get func(StreamSpec) bool) bool {
	if old == nil {
		return false
	}
	return get(*old)
}
func oldInt(old *StreamSpec, get func(StreamSpec) int64) int64 {
	if old == nil {
		return 0
	}
	return get(*old)
}

// AddStream creates a stream. Only what differs from nats's own defaults
// is passed, plus the storage and the subjects, so the command stays
// readable.
func AddStream(s StreamSpec) *Plan {
	p := &Plan{Title: "Add stream " + s.Name}
	def := NewStreamSpec()
	def.Name = s.Name
	args := append([]string{"stream", "add", s.Name}, streamFlags(&def, s, true)...)
	args = append(args, "--defaults")
	p.Add("create the stream", args...)
	return p
}

// EditStream changes what differs between old and cur. What the server
// refuses to change is left alone and mentioned in a note.
func EditStream(old, cur StreamSpec) *Plan {
	p := &Plan{Title: "Edit stream " + old.Name}
	if old.Storage != cur.Storage {
		p.Note("the storage type cannot be changed after creation: it stays " + old.Storage)
		cur.Storage = old.Storage
	}
	if old.Retention != cur.Retention {
		p.Note("the server refuses a retention change on an existing stream: it stays " + old.Retention)
		cur.Retention = old.Retention
	}
	if old.DenyDelete && !cur.DenyDelete {
		p.Note("the server refuses to lift deny delete once it is set: it stays on")
		cur.DenyDelete = true
	}
	if old.DenyPurge && !cur.DenyPurge {
		p.Note("the server refuses to lift deny purge once it is set: it stays on")
		cur.DenyPurge = true
	}
	flags := streamFlags(&old, cur, false)
	if len(flags) == 0 {
		return p
	}
	args := append([]string{"stream", "edit", old.Name}, flags...)
	args = append(args, "-f")
	p.Add("apply the changes (nats shows the difference it applied)", args...)
	return p
}

// DeleteStream removes a stream and its consumers.
func DeleteStream(name string) *Plan {
	p := &Plan{Title: "Delete stream " + name}
	p.AddDanger("remove the stream, its messages and its consumers", "stream", "rm", name, "-f")
	return p
}

// PurgeStream removes messages: all, a subject, up to a sequence, or all
// but the last keep.
func PurgeStream(name, subject string, seq, keep int64) *Plan {
	p := &Plan{Title: "Purge stream " + name}
	args := []string{"stream", "purge", name, "-f"}
	desc := "remove every message"
	if subject != "" {
		args = append(args, "--subject="+subject)
		desc = "remove the messages of " + subject
	}
	if seq > 0 {
		args = append(args, "--seq="+fmt.Sprint(seq))
		desc += fmt.Sprintf(" before sequence %d", seq)
	}
	if keep > 0 {
		args = append(args, "--keep="+fmt.Sprint(keep))
		desc += fmt.Sprintf(", keeping the last %d", keep)
	}
	p.AddDanger(desc, args...)
	return p
}

// DeleteMessage removes one message (securely: it is overwritten).
func DeleteMessage(stream string, seq uint64) *Plan {
	p := &Plan{Title: fmt.Sprintf("Delete message %d of %s", seq, stream)}
	p.AddDanger("erase the message", "stream", "rmm", stream, fmt.Sprint(seq), "-f")
	return p
}

// SealStream makes a stream read-only for good.
func SealStream(name string) *Plan {
	p := &Plan{Title: "Seal stream " + name}
	p.AddDanger("seal the stream: no more publishing, no more deletes, and no way back", "stream", "seal", name, "-f")
	return p
}

// BackupStream writes a stream to a directory.
func BackupStream(name, dir string, consumers bool) *Plan {
	p := &Plan{Title: "Backup stream " + name}
	args := []string{"stream", "backup", name, dir, "--no-progress"}
	if consumers {
		args = append(args, "--consumers")
	} else {
		args = append(args, "--no-consumers")
	}
	p.Add("write the backup", args...)
	return p
}

// RestoreStream restores a backup directory.
func RestoreStream(dir string) *Plan {
	p := &Plan{Title: "Restore stream from " + dir}
	p.AddDanger("restore the stream (it must not exist yet)", "stream", "restore", dir, "--no-progress")
	return p
}

// BackupAccount writes every stream of the account to a directory, one
// subdirectory per stream.
func BackupAccount(dir string, consumers, check bool) *Plan {
	p := &Plan{Title: "Backup every stream to " + dir}
	args := []string{"account", "backup", dir, "-f"}
	if consumers {
		args = append(args, "--consumers")
	} else {
		args = append(args, "--no-consumers")
	}
	if check {
		args = append(args, "--check")
	}
	p.Add("write one backup per stream", args...)
	return p
}

// RestoreAccount restores every stream backup found in a directory.
func RestoreAccount(dir string) *Plan {
	p := &Plan{Title: "Restore every stream from " + dir}
	p.AddDanger("restore each stream of the backup (none may exist yet)", "account", "restore", dir)
	return p
}

// BackupKind tells what a directory holds: a stream backup (backup.json
// and the data file), an account backup (one stream backup per
// subdirectory), or neither.
func BackupKind(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "backup.json")); err == nil {
		return "stream"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			if _, err := os.Stat(filepath.Join(dir, e.Name(), "backup.json")); err == nil {
				return "account"
			}
		}
	}
	return ""
}

// CopyConsumer creates a consumer with the configuration of another.
func CopyConsumer(stream, from, to string) *Plan {
	p := &Plan{Title: "Copy consumer " + from + " to " + to}
	p.Add("create the consumer from the configuration of the source", "consumer", "copy", stream, from, to)
	return p
}

// CopyStream creates a stream with the configuration of another. The
// copy needs subjects of its own, as two streams cannot share a
// subject; only a source without subjects (a mirror) is copied as is.
func CopyStream(from, to string, subjects []string) *Plan {
	p := &Plan{Title: "Copy stream " + from + " to " + to}
	args := []string{"stream", "copy", from, to}
	if len(subjects) > 0 {
		args = append(args, "--subjects="+strings.Join(subjects, ","))
	} else {
		p.Note("the copy keeps the subjects of the source, if any: the server refuses it when they overlap")
	}
	p.Add("create the stream from the configuration of the source (no data)", args...)
	return p
}

// ---------------------------------------------------------------- consumers

// ConsumerSpec is the configuration of a consumer as nats consumer
// add/edit takes it.
type ConsumerSpec struct {
	Stream            string
	Name              string
	Description       string
	Pull              bool
	Target            string // push: deliver subject
	DeliverGroup      string
	Deliver           string // all, new, last, subject, a sequence, a time, a duration ago
	Filter            []string
	Ack               string
	AckWait           string
	MaxDeliver        int64
	MaxPending        int64
	MaxWaiting        int64
	Replay            string
	Heartbeat         string
	FlowControl       bool
	HeadersOnly       bool
	InactiveThreshold string
	Replicas          int64
	Memory            bool
	Sample            int64
	BackoffSteps      []string
	MaxPullBatch      int64
	MaxPullExpire     string
	MaxPullBytes      int64
	Metadata          []string
	Ephemeral         bool
	Paused            bool
	PauseUntil        time.Time
}

// NewConsumerSpec is what the add editor starts from.
func NewConsumerSpec(stream string) ConsumerSpec {
	return ConsumerSpec{Stream: stream, Pull: true, Deliver: "all", Ack: "explicit", AckWait: "30s", MaxDeliver: -1, MaxPending: 1000, Replay: "instant", Sample: -1}
}

// ConsumerSpecFrom reads the spec of an existing consumer.
func ConsumerSpecFrom(stream string, c jetstream.ConsumerConfig, info *jetstream.ConsumerInfo) ConsumerSpec {
	s := ConsumerSpec{
		Stream: stream, Name: c.Name, Description: c.Description, Pull: c.DeliverSubject == "", Target: c.DeliverSubject, DeliverGroup: c.DeliverGroup,
		Ack: ackName(c.AckPolicy), AckWait: DurationText(c.AckWait), MaxDeliver: int64(c.MaxDeliver), MaxPending: int64(c.MaxAckPending),
		MaxWaiting: int64(c.MaxWaiting), Replay: replayName(c.ReplayPolicy), Heartbeat: DurationText(c.IdleHeartbeat), FlowControl: c.FlowControl,
		HeadersOnly: c.HeadersOnly, InactiveThreshold: DurationText(c.InactiveThreshold), Replicas: int64(c.Replicas), Memory: c.MemoryStorage,
		Sample: -1, MaxPullBatch: int64(c.MaxRequestBatch), MaxPullExpire: DurationText(c.MaxRequestExpires), MaxPullBytes: int64(c.MaxRequestMaxBytes),
		Metadata: Metadata(c.Metadata), Ephemeral: c.Durable == "" && c.Name == "",
	}
	if s.Name == "" {
		s.Name = c.Durable
	}
	if s.MaxDeliver == 0 {
		s.MaxDeliver = -1
	}
	if c.SampleFrequency != "" {
		n, _ := strconv.ParseInt(strings.TrimSuffix(c.SampleFrequency, "%"), 10, 64)
		s.Sample = n
	}
	if len(c.FilterSubjects) > 0 {
		s.Filter = append([]string{}, c.FilterSubjects...)
	} else if c.FilterSubject != "" {
		s.Filter = []string{c.FilterSubject}
	}
	for _, b := range c.BackOff {
		s.BackoffSteps = append(s.BackoffSteps, DurationText(b))
	}
	switch c.DeliverPolicy {
	case jetstream.DeliverAllPolicy:
		s.Deliver = "all"
	case jetstream.DeliverNewPolicy:
		s.Deliver = "new"
	case jetstream.DeliverLastPolicy:
		s.Deliver = "last"
	case jetstream.DeliverLastPerSubjectPolicy:
		s.Deliver = "subject"
	case jetstream.DeliverByStartSequencePolicy:
		s.Deliver = fmt.Sprint(c.OptStartSeq)
	case jetstream.DeliverByStartTimePolicy:
		if c.OptStartTime != nil {
			s.Deliver = c.OptStartTime.Format(time.RFC3339)
		}
	}
	if info != nil {
		s.Paused = info.Paused
		if c.PauseUntil != nil {
			s.PauseUntil = *c.PauseUntil
		}
	}
	return s
}

func ackName(a jetstream.AckPolicy) string {
	switch a {
	case jetstream.AckNonePolicy:
		return "none"
	case jetstream.AckAllPolicy:
		return "all"
	}
	return "explicit"
}

func replayName(r jetstream.ReplayPolicy) string {
	if r == jetstream.ReplayOriginalPolicy {
		return "original"
	}
	return "instant"
}

// AddConsumer creates a consumer.
func AddConsumer(s ConsumerSpec) *Plan {
	p := &Plan{Title: fmt.Sprintf("Add consumer %s to %s", s.Name, s.Stream)}
	f := &flagSet{}
	if s.Pull {
		f.bool("--pull", true)
	} else {
		f.str("--target", s.Target)
		f.str("--deliver-group", s.DeliverGroup)
		f.bool("--flow-control", s.FlowControl)
		f.str("--heartbeat", s.Heartbeat)
	}
	f.bool("--ephemeral", s.Ephemeral)
	f.str("--description", s.Description)
	f.str("--deliver", s.Deliver)
	f.each("--filter", s.Filter)
	f.str("--ack", s.Ack)
	f.str("--max-deliver", fmt.Sprint(s.MaxDeliver))
	if s.Ack != "none" {
		// the server refuses an ack wait or a pending limit without acks
		f.str("--wait", s.AckWait)
		f.str("--max-pending", fmt.Sprint(s.MaxPending))
	}
	if s.MaxWaiting > 0 {
		f.str("--max-waiting", fmt.Sprint(s.MaxWaiting))
	}
	f.str("--replay", s.Replay)
	f.noBool("--headers-only", s.HeadersOnly)
	f.str("--inactive-threshold", s.InactiveThreshold)
	if s.Replicas > 0 {
		f.str("--replicas", fmt.Sprint(s.Replicas))
	}
	f.bool("--memory", s.Memory)
	if s.Sample >= 0 {
		f.str("--sample", fmt.Sprint(s.Sample))
	}
	if len(s.BackoffSteps) > 0 {
		f.str("--backoff", "linear")
		f.str("--backoff-steps", fmt.Sprint(len(s.BackoffSteps)))
		f.str("--backoff-min", s.BackoffSteps[0])
		f.str("--backoff-max", s.BackoffSteps[len(s.BackoffSteps)-1])
	}
	if s.MaxPullBatch > 0 {
		f.str("--max-pull-batch", fmt.Sprint(s.MaxPullBatch))
	}
	f.str("--max-pull-expire", s.MaxPullExpire)
	if s.MaxPullBytes > 0 {
		f.str("--max-pull-bytes", fmt.Sprint(s.MaxPullBytes))
	}
	f.each("--metadata", s.Metadata)
	args := append([]string{"consumer", "add", s.Stream}, f.args...)
	if !s.Ephemeral {
		args = append([]string{"consumer", "add", s.Stream, s.Name}, f.args...)
	}
	args = append(args, "--defaults")
	p.Add("create the consumer", args...)
	return p
}

// EditConsumer changes what nats consumer edit can change.
func EditConsumer(old, cur ConsumerSpec) *Plan {
	p := &Plan{Title: fmt.Sprintf("Edit consumer %s of %s", old.Name, old.Stream)}
	f := &flagSet{}
	if old.Description != cur.Description {
		f.strAlways("--description", cur.Description)
	}
	if !sameList(old.Filter, cur.Filter) {
		f.each("--filter", cur.Filter)
	}
	if old.HeadersOnly != cur.HeadersOnly {
		f.noBool("--headers-only", cur.HeadersOnly)
	}
	if old.MaxDeliver != cur.MaxDeliver {
		f.str("--max-deliver", fmt.Sprint(cur.MaxDeliver))
	}
	if old.MaxPending != cur.MaxPending {
		f.str("--max-pending", fmt.Sprint(cur.MaxPending))
	}
	if old.AckWait != cur.AckWait {
		f.str("--wait", cur.AckWait)
	}
	if old.Sample != cur.Sample {
		f.str("--sample", fmt.Sprint(cur.Sample))
	}
	if old.Target != cur.Target && !cur.Pull {
		f.str("--target", cur.Target)
	}
	if old.InactiveThreshold != cur.InactiveThreshold {
		f.str("--inactive-threshold", durationFlag(cur.InactiveThreshold))
	}
	if old.Replicas != cur.Replicas {
		f.str("--replicas", fmt.Sprint(cur.Replicas))
	}
	if old.MaxPullBatch != cur.MaxPullBatch {
		f.str("--max-pull-batch", fmt.Sprint(cur.MaxPullBatch))
	}
	if old.MaxPullExpire != cur.MaxPullExpire {
		f.str("--max-pull-expire", durationFlag(cur.MaxPullExpire))
	}
	if old.MaxPullBytes != cur.MaxPullBytes {
		f.str("--max-pull-bytes", fmt.Sprint(cur.MaxPullBytes))
	}
	if !sameList(old.Metadata, cur.Metadata) {
		f.each("--metadata", cur.Metadata)
	}
	if !sameList(old.BackoffSteps, cur.BackoffSteps) && len(cur.BackoffSteps) > 0 {
		f.str("--backoff", "linear")
		f.str("--backoff-steps", fmt.Sprint(len(cur.BackoffSteps)))
		f.str("--backoff-min", cur.BackoffSteps[0])
		f.str("--backoff-max", cur.BackoffSteps[len(cur.BackoffSteps)-1])
	}
	if len(f.args) == 0 {
		return p
	}
	args := append([]string{"consumer", "edit", old.Stream, old.Name}, f.args...)
	args = append(args, "-f")
	p.Add("apply the changes (nats shows the difference it applied)", args...)
	for _, note := range consumerEditNotes(old, cur) {
		p.Note(note)
	}
	return p
}

// consumerEditNotes lists the settings the server will not change on an
// existing consumer.
func consumerEditNotes(old, cur ConsumerSpec) []string {
	var notes []string
	if old.Pull != cur.Pull {
		notes = append(notes, "a consumer cannot switch between pull and push: delete it and add it again")
	}
	if old.Deliver != cur.Deliver {
		notes = append(notes, "the deliver policy is fixed at creation: delete the consumer and add it again to change it")
	}
	if old.Ack != cur.Ack {
		notes = append(notes, "the ack policy is fixed at creation")
	}
	if old.Replay != cur.Replay {
		notes = append(notes, "the replay policy is fixed at creation")
	}
	return notes
}

// DeleteConsumer removes a consumer.
func DeleteConsumer(stream, name string) *Plan {
	p := &Plan{Title: fmt.Sprintf("Delete consumer %s of %s", name, stream)}
	p.AddDanger("remove the consumer and its delivery state", "consumer", "rm", stream, name, "-f")
	return p
}

// PauseConsumer pauses delivery until a time.
func PauseConsumer(stream, name string, until time.Time) *Plan {
	p := &Plan{Title: fmt.Sprintf("Pause consumer %s of %s", name, stream)}
	p.Add("pause delivery until "+Date(until), "consumer", "pause", stream, name, until.Local().Format("2006-01-02 15:04:05"), "-f")
	return p
}

// ResumeConsumer lifts a pause.
func ResumeConsumer(stream, name string) *Plan {
	p := &Plan{Title: fmt.Sprintf("Resume consumer %s of %s", name, stream)}
	p.Add("resume delivery", "consumer", "resume", stream, name, "-f")
	return p
}

// NextMessages pulls messages from a pull consumer through nats consumer
// next, with the acknowledgement of choice.
func NextMessages(stream, name string, count int, ack string) *Plan {
	p := &Plan{Title: fmt.Sprintf("Next %d message(s) of %s", count, name)}
	args := []string{"consumer", "next", stream, name, "--count=" + fmt.Sprint(count)}
	desc := "fetch and acknowledge"
	switch ack {
	case "ack":
		args = append(args, "--ack")
	case "nak":
		args = append(args, "--nak")
		desc = "fetch and negatively acknowledge (redelivered)"
	case "term":
		args = append(args, "--term")
		desc = "fetch and terminate (never redelivered)"
	default:
		args = append(args, "--no-ack")
		desc = "fetch without acknowledging (redelivered after the ack wait)"
	}
	if ack == "term" {
		p.AddDanger(desc, args...)
	} else {
		p.Add(desc, args...)
	}
	return p
}

// ---------------------------------------------------------------- buckets

// KVSpec is a key-value bucket as nats kv add/edit takes it.
type KVSpec struct {
	Name         string
	Description  string
	History      int64
	TTL          string
	MarkerTTL    string
	Storage      string
	Replicas     int64
	MaxValueSize int64
	MaxBytes     int64
	Compress     bool
	RepubSource  string
	RepubDest    string
	RepubHeaders bool
	Mirror       string
	Sources      []string
	Metadata     []string
	Cluster      string
	Tags         []string
}

// NewKVSpec is what the add editor starts from.
func NewKVSpec() KVSpec {
	return KVSpec{History: 1, Storage: "file", Replicas: 1, MaxValueSize: -1, MaxBytes: -1}
}

// KVSpecFrom reads the spec of a bucket.
func KVSpecFrom(st jetstream.KeyValueStatus, info *jetstream.StreamInfo) KVSpec {
	c := st.Config()
	s := KVSpec{Name: st.Bucket(), Description: c.Description, History: int64(st.History()), TTL: DurationText(st.TTL()),
		MarkerTTL: DurationText(st.LimitMarkerTTL()), Storage: storageName(c.Storage), Replicas: int64(c.Replicas),
		MaxValueSize: int64(c.MaxValueSize), MaxBytes: c.MaxBytes, Compress: st.IsCompressed(), Metadata: Metadata(st.Metadata())}
	if s.Replicas == 0 {
		s.Replicas = 1
	}
	if s.MaxValueSize == 0 {
		s.MaxValueSize = -1
	}
	if s.MaxBytes == 0 {
		s.MaxBytes = -1
	}
	if c.RePublish != nil {
		s.RepubSource, s.RepubDest, s.RepubHeaders = c.RePublish.Source, c.RePublish.Destination, c.RePublish.HeadersOnly
	}
	if c.Mirror != nil {
		s.Mirror = strings.TrimPrefix(c.Mirror.Name, "KV_")
	}
	for _, src := range c.Sources {
		s.Sources = append(s.Sources, strings.TrimPrefix(src.Name, "KV_"))
	}
	if c.Placement != nil {
		s.Cluster, s.Tags = c.Placement.Cluster, append([]string{}, c.Placement.Tags...)
	}
	if info != nil {
		if s.Description == "" {
			s.Description = info.Config.Description
		}
		if info.Config.Placement != nil {
			s.Cluster, s.Tags = info.Config.Placement.Cluster, append([]string{}, info.Config.Placement.Tags...)
		}
		if info.Config.RePublish != nil && s.RepubDest == "" {
			s.RepubSource, s.RepubDest, s.RepubHeaders = info.Config.RePublish.Source, info.Config.RePublish.Destination, info.Config.RePublish.HeadersOnly
		}
	}
	return s
}

func kvFlags(old *KVSpec, s KVSpec, add bool) []string {
	f := &flagSet{}
	if old == nil || old.Description != s.Description {
		if s.Description != "" || !add {
			f.strAlways("--description", s.Description)
		}
	}
	if old == nil || old.History != s.History {
		f.str("--history", fmt.Sprint(s.History))
	}
	if old == nil || old.TTL != s.TTL {
		if s.TTL != "" || !add {
			f.strAlways("--ttl", zeroDuration(s.TTL))
		}
	}
	if old == nil || old.MarkerTTL != s.MarkerTTL {
		if s.MarkerTTL != "" || !add {
			f.strAlways("--marker-ttl", zeroDuration(s.MarkerTTL))
		}
	}
	if add {
		f.str("--storage", s.Storage)
	}
	if old == nil || old.Replicas != s.Replicas {
		f.str("--replicas", fmt.Sprint(s.Replicas))
	}
	if old == nil || old.MaxValueSize != s.MaxValueSize {
		f.str("--max-value-size", fmt.Sprint(s.MaxValueSize))
	}
	if old == nil || old.MaxBytes != s.MaxBytes {
		f.str("--max-bucket-size", fmt.Sprint(s.MaxBytes))
	}
	if old == nil && s.Compress || old != nil && old.Compress != s.Compress {
		f.noBool("--compress", s.Compress)
	}
	if old == nil || old.RepubSource != s.RepubSource || old.RepubDest != s.RepubDest || old.RepubHeaders != s.RepubHeaders {
		if s.RepubDest != "" {
			f.str("--republish-source", orAll(s.RepubSource))
			f.str("--republish-destination", s.RepubDest)
			f.bool("--republish-headers", s.RepubHeaders)
		}
	}
	if add {
		f.str("--mirror", s.Mirror)
	} else if old != nil && old.Mirror != "" && s.Mirror == "" {
		f.bool("--no-mirror", true)
	}
	if old == nil || !sameList(old.Sources, s.Sources) {
		f.each("--source", s.Sources)
	}
	if old == nil || !sameList(old.Metadata, s.Metadata) {
		f.each("--metadata", s.Metadata)
	}
	if old == nil || old.Cluster != s.Cluster {
		f.str("--cluster", s.Cluster)
	}
	if old == nil || !sameList(old.Tags, s.Tags) {
		f.list("--tags", s.Tags)
	}
	return f.args
}

// zeroDuration is how "no TTL" is passed: 0.
func zeroDuration(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

// AddKV creates a bucket; only what differs from nats's defaults is
// passed, plus the storage.
func AddKV(s KVSpec) *Plan {
	p := &Plan{Title: "Add bucket " + s.Name}
	def := NewKVSpec()
	p.Add("create the bucket", append([]string{"kv", "add", s.Name}, kvFlags(&def, s, true)...)...)
	return p
}

// EditKV changes a bucket.
func EditKV(old, cur KVSpec) *Plan {
	p := &Plan{Title: "Edit bucket " + old.Name}
	flags := kvFlags(&old, cur, false)
	if len(flags) == 0 {
		return p
	}
	p.Add("apply the changes", append([]string{"kv", "edit", old.Name}, flags...)...)
	if old.Storage != cur.Storage {
		p.Note("the storage type cannot be changed after creation: it stays " + old.Storage)
	}
	if old.Mirror != cur.Mirror && cur.Mirror != "" {
		p.Note("a mirror can only be set when the bucket is created")
	}
	return p
}

// DeleteKV removes a bucket.
func DeleteKV(name string) *Plan {
	p := &Plan{Title: "Delete bucket " + name}
	p.AddDanger("remove the bucket and every key", "kv", "del", name, "-f")
	return p
}

// PutKey writes a value (through stdin, so any text works).
func PutKey(bucket, key, value string) *Plan {
	p := &Plan{Title: fmt.Sprintf("Put %s in %s", key, bucket)}
	p.AddStdin("write the value", value, "kv", "put", bucket, key)
	return p
}

// UpdateKey writes a value only when the key is still at the revision
// that was read (nats kv update): a write that happened in between makes
// the server refuse it instead of being overwritten. The value is an
// argument, as nats kv update does not read stdin.
func UpdateKey(bucket, key, value string, rev uint64) *Plan {
	p := &Plan{Title: fmt.Sprintf("Update %s in %s", key, bucket)}
	p.Add(fmt.Sprintf("write the value if the key is still at revision %d", rev), "kv", "update", bucket, key, value, fmt.Sprint(rev))
	return p
}

// CreateKey writes a value only when the key is new.
func CreateKey(bucket, key, value, ttl string) *Plan {
	p := &Plan{Title: fmt.Sprintf("Create %s in %s", key, bucket)}
	args := []string{"kv", "create", bucket, key}
	if ttl != "" {
		args = append(args, "--ttl="+ttl)
	}
	p.AddStdin("write the value if the key does not exist", value, args...)
	return p
}

// DeleteKey marks a key deleted (its history stays).
func DeleteKey(bucket, key string) *Plan {
	p := &Plan{Title: fmt.Sprintf("Delete %s from %s", key, bucket)}
	p.AddDanger("put a delete marker (the history is kept)", "kv", "del", bucket, key, "-f")
	return p
}

// PurgeKey deletes a key and its history.
func PurgeKey(bucket, key string) *Plan {
	p := &Plan{Title: fmt.Sprintf("Purge %s from %s", key, bucket)}
	p.AddDanger("delete the key and clear its history", "kv", "purge", bucket, key, "-f")
	return p
}

// RevertKey puts an older revision back.
func RevertKey(bucket, key string, rev uint64) *Plan {
	p := &Plan{Title: fmt.Sprintf("Revert %s to revision %d", key, rev)}
	p.Add("put the old value again as a new revision", "kv", "revert", bucket, key, fmt.Sprint(rev), "--force")
	return p
}

// CompactKV reclaims the space of deleted keys.
func CompactKV(bucket string) *Plan {
	p := &Plan{Title: "Compact bucket " + bucket}
	p.AddDanger("remove the delete markers and their history", "kv", "compact", bucket, "-f")
	return p
}

// ObjectSpec is an object store bucket as nats object add/edit takes it.
type ObjectSpec struct {
	Name        string
	Description string
	TTL         string
	Storage     string
	Replicas    int64
	MaxBytes    int64
	Compress    bool
	Metadata    []string
	Cluster     string
	Tags        []string
}

// NewObjectSpec is what the add editor starts from.
func NewObjectSpec() ObjectSpec {
	return ObjectSpec{Storage: "file", Replicas: 1, MaxBytes: -1}
}

// ObjectSpecFrom reads the spec of a bucket.
func ObjectSpecFrom(st jetstream.ObjectStoreStatus, info *jetstream.StreamInfo) ObjectSpec {
	s := ObjectSpec{Name: st.Bucket(), Description: st.Description(), TTL: DurationText(st.TTL()), Storage: storageName(st.Storage()),
		Replicas: int64(st.Replicas()), MaxBytes: -1, Compress: st.IsCompressed(), Metadata: Metadata(st.Metadata())}
	if s.Replicas == 0 {
		s.Replicas = 1
	}
	if info != nil {
		s.MaxBytes = info.Config.MaxBytes
		if s.MaxBytes == 0 {
			s.MaxBytes = -1
		}
		if info.Config.Placement != nil {
			s.Cluster, s.Tags = info.Config.Placement.Cluster, append([]string{}, info.Config.Placement.Tags...)
		}
	}
	return s
}

func objectFlags(old *ObjectSpec, s ObjectSpec, add bool) []string {
	f := &flagSet{}
	if old == nil || old.Description != s.Description {
		if s.Description != "" || !add {
			f.strAlways("--description", s.Description)
		}
	}
	if old == nil || old.TTL != s.TTL {
		if s.TTL != "" || !add {
			f.strAlways("--ttl", zeroDuration(s.TTL))
		}
	}
	if add {
		f.str("--storage", s.Storage)
	}
	if old == nil || old.Replicas != s.Replicas {
		f.str("--replicas", fmt.Sprint(s.Replicas))
	}
	if old == nil || old.MaxBytes != s.MaxBytes {
		f.str("--max-bucket-size", fmt.Sprint(s.MaxBytes))
	}
	if old == nil && s.Compress || old != nil && old.Compress != s.Compress {
		f.noBool("--compress", s.Compress)
	}
	if old == nil || !sameList(old.Metadata, s.Metadata) {
		f.each("--metadata", s.Metadata)
	}
	if old == nil || old.Cluster != s.Cluster {
		f.str("--cluster", s.Cluster)
	}
	if old == nil || !sameList(old.Tags, s.Tags) {
		f.list("--tags", s.Tags)
	}
	return f.args
}

// AddObjectStore creates a bucket; only what differs from nats's defaults
// is passed, plus the storage.
func AddObjectStore(s ObjectSpec) *Plan {
	p := &Plan{Title: "Add object store " + s.Name}
	def := NewObjectSpec()
	p.Add("create the bucket", append([]string{"object", "add", s.Name}, objectFlags(&def, s, true)...)...)
	return p
}

// EditObjectStore changes a bucket.
func EditObjectStore(old, cur ObjectSpec) *Plan {
	p := &Plan{Title: "Edit object store " + old.Name}
	flags := objectFlags(&old, cur, false)
	if len(flags) == 0 {
		return p
	}
	p.Add("apply the changes", append([]string{"object", "edit", old.Name}, flags...)...)
	if old.Storage != cur.Storage {
		p.Note("the storage type cannot be changed after creation: it stays " + old.Storage)
	}
	return p
}

// DeleteObjectStore removes a bucket.
func DeleteObjectStore(name string) *Plan {
	p := &Plan{Title: "Delete object store " + name}
	p.AddDanger("remove the bucket and every object", "object", "del", name, "-f")
	return p
}

// PutObject stores a file.
func PutObject(bucket, file, name, description string) *Plan {
	p := &Plan{Title: "Put " + file + " into " + bucket}
	args := []string{"object", "put", bucket, file, "-f", "--no-progress"}
	if name != "" {
		args = append(args, "--name="+name)
	}
	if description != "" {
		args = append(args, "--description="+description)
	}
	p.Add("store the file (an object of the same name is replaced)", args...)
	return p
}

// GetObject writes an object to a file.
func GetObject(bucket, name, output string) *Plan {
	p := &Plan{Title: "Get " + name + " from " + bucket}
	args := []string{"object", "get", bucket, name, "-f", "--no-progress"}
	if output != "" {
		args = append(args, "--output="+output)
	}
	p.Add("write the object to the file", args...)
	return p
}

// DeleteObject removes an object.
func DeleteObject(bucket, name string) *Plan {
	p := &Plan{Title: "Delete " + name + " from " + bucket}
	p.AddDanger("remove the object", "object", "del", bucket, name, "-f")
	return p
}

// SealObjectStore makes a bucket read-only for good.
func SealObjectStore(name string) *Plan {
	p := &Plan{Title: "Seal object store " + name}
	p.AddDanger("seal the bucket: no more changes, and no way back", "object", "seal", name, "-f")
	return p
}

// ---------------------------------------------------------------- messaging

// PublishSpec is a nats pub invocation.
type PublishSpec struct {
	Subject   string
	Body      string
	Headers   []string // K:V
	Count     int64
	Sleep     string
	Reply     string
	JetStream bool

	// A schedule stores the message in a stream that allows schedules
	// and publishes it to ScheduleDest when the schedule fires. One
	// schedule lives on each publish subject: a new one replaces it.
	Schedule       string // one of ScheduleKinds; "" publishes now
	ScheduleValue  string // RFC3339 time, a duration, or a six-field cron line
	ScheduleDest   string // where the message goes when the schedule fires
	ScheduleSource string // read the body from the last message on this subject
	ScheduleTTL    string // how long the fired messages live (needs per-message TTL)
}

// ScheduleKinds are the ways to schedule a message: at an RFC3339
// time, after a duration, every interval, or on a cron line.
var ScheduleKinds = []string{"none", "at", "after", "every", "cron"}

// Scheduled tells whether the spec asks for a schedule.
func (s PublishSpec) Scheduled() bool { return s.Schedule != "" && s.Schedule != "none" }

// Publish sends messages. The body goes through stdin when it holds
// newlines, so multi-line text survives; the preview says so.
func Publish(s PublishSpec) *Plan {
	p := &Plan{Title: "Publish to " + s.Subject}
	f := &flagSet{}
	f.each("--header", s.Headers)
	if s.Count > 1 {
		f.str("--count", fmt.Sprint(s.Count))
		f.str("--sleep", s.Sleep)
	}
	f.str("--reply", s.Reply)
	desc := "publish the message"
	if s.Count > 1 {
		desc = fmt.Sprintf("publish %d messages", s.Count)
	}
	if s.Scheduled() {
		p.Title = "Schedule a message on " + s.Subject
		desc = "store the scheduled message (published to " + s.ScheduleDest + " when it fires)"
		switch s.Schedule {
		case "at":
			f.str("--schedule-at", s.ScheduleValue)
		case "after":
			// nats 0.4.0 sends --schedule-after as a bare time, which the
			// server refuses, so the time is worked out here instead.
			at := s.ScheduleValue
			if d, err := ParseDuration(s.ScheduleValue); err == nil {
				at = time.Now().Add(d).UTC().Truncate(time.Second).Format(time.RFC3339)
				p.Note("the time was computed when the plan was built: " + s.ScheduleValue + " from then is " + at)
			}
			f.str("--schedule-at", at)
		case "every":
			f.str("--schedule-every", s.ScheduleValue)
		case "cron":
			f.str("--schedule-cron", s.ScheduleValue)
		}
		f.str("--schedule-dest", s.ScheduleDest)
		f.str("--schedule-source", s.ScheduleSource)
		f.str("--schedule-ttl", s.ScheduleTTL)
		p.Note("the stream holding " + s.Subject + " must allow message schedules; one schedule lives on that subject and a new one replaces it")
	} else {
		f.bool("--jetstream", s.JetStream)
	}
	if strings.Contains(s.Body, "\n") {
		args := append([]string{"pub", s.Subject, "--force-stdin"}, f.args...)
		p.AddStdin(desc+" (body from stdin)", s.Body, args...)
		return p
	}
	args := append([]string{"pub", s.Subject, s.Body}, f.args...)
	p.Add(desc, args...)
	return p
}

// RequestSpec is a nats request invocation.
type RequestSpec struct {
	Subject string
	Body    string
	Headers []string
	Replies int64
	Timeout string
	Count   int64
}

// Request sends a request and waits for the replies.
func Request(s RequestSpec) *Plan {
	p := &Plan{Title: "Request " + s.Subject}
	f := &flagSet{}
	f.each("--header", s.Headers)
	if s.Replies != 1 {
		f.str("--replies", fmt.Sprint(s.Replies))
	}
	if s.Count > 1 {
		f.str("--count", fmt.Sprint(s.Count))
	}
	if s.Timeout != "" {
		f.args = append([]string{"--timeout=" + s.Timeout}, f.args...)
	}
	desc := "send the request and show the reply"
	if s.Replies != 1 {
		desc = "send the request and collect the replies"
	}
	if strings.Contains(s.Body, "\n") {
		p.AddStdin(desc+" (body from stdin)", s.Body, append([]string{"request", s.Subject, "--force-stdin"}, f.args...)...)
		return p
	}
	p.Add(desc, append([]string{"request", s.Subject, s.Body}, f.args...)...)
	return p
}

// ---------------------------------------------------------------- contexts

// ContextSpec is a context as nats context add takes it.
type ContextSpec struct {
	Name        string
	Description string
	Server      string
	User        string
	Password    string
	Token       string
	Creds       string
	NKey        string
	JWT         string
	Seed        string
	Cert        string
	Key         string
	CA          string
	TLSFirst    bool
	NscURL      string
	JSDomain    string
	JSAPIPrefix string
	JSEventPfx  string
	InboxPrefix string
	SocksProxy  string
	Timeout     string
	Select      bool
}

// ContextSpecFrom reads a context.
func ContextSpecFrom(c ContextInfo) ContextSpec {
	return ContextSpec{Name: c.Name, Description: c.Description, Server: c.URL, User: c.User, Creds: c.Creds, NKey: c.NKey,
		Cert: c.Cert, Key: c.Key, CA: c.CA, NscURL: c.NscURL, JSDomain: c.JSDomain, JSAPIPrefix: c.JSAPIPrefix, JSEventPfx: c.JSEventPfx, InboxPrefix: c.InboxPrefix}
}

// SaveContext creates or updates a context. nats context add takes the
// connection settings as the global flags, so they come first.
func SaveContext(s ContextSpec, existing bool) *Plan {
	title := "Add context " + s.Name
	if existing {
		title = "Update context " + s.Name
	}
	p := &Plan{Title: title}
	f := &flagSet{}
	f.str("--server", s.Server)
	f.str("--user", s.User)
	f.str("--password", s.Password)
	f.str("--token", s.Token)
	f.str("--creds", s.Creds)
	f.str("--nkey", s.NKey)
	f.str("--jwt", s.JWT)
	f.str("--seed", s.Seed)
	f.str("--tlscert", s.Cert)
	f.str("--tlskey", s.Key)
	f.str("--tlsca", s.CA)
	f.bool("--tlsfirst", s.TLSFirst)
	f.str("--js-domain", s.JSDomain)
	f.str("--js-api-prefix", s.JSAPIPrefix)
	f.str("--js-event-prefix", s.JSEventPfx)
	f.str("--inbox-prefix", s.InboxPrefix)
	f.str("--socks-proxy", s.SocksProxy)
	f.str("--timeout", s.Timeout)
	args := append(f.args, "context", "add", s.Name)
	if s.Description != "" {
		args = append(args, "--description="+s.Description)
	}
	if s.NscURL != "" {
		args = append(args, "--nsc="+s.NscURL)
	}
	if s.Select {
		args = append(args, "--select")
	}
	desc := "write the context file"
	if existing {
		desc = "update the context file (settings not given here are kept)"
	}
	p.Add(desc, args...)
	if s.Password != "" || s.Token != "" || s.Seed != "" {
		p.Note("the password, token or seed is stored in clear in the context file")
	}
	return p
}

// SelectContext makes a context nats's default.
func SelectContext(name string) *Plan {
	p := &Plan{Title: "Select context " + name}
	p.Add("make it the default context", "context", "select", name)
	return p
}

// DeleteContext removes a context file.
func DeleteContext(name string) *Plan {
	p := &Plan{Title: "Delete context " + name}
	p.AddDanger("remove the context file", "context", "rm", name, "-f")
	return p
}

// CopyContext duplicates a context.
func CopyContext(from, to string) *Plan {
	p := &Plan{Title: "Copy context " + from + " to " + to}
	p.Add("write the copy", "context", "copy", from, to)
	return p
}

// SortedContextNames sorts a copy of a name list.
func SortedContextNames(names []string) []string {
	out := append([]string{}, names...)
	sort.Strings(out)
	return out
}
