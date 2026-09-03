package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nats.go/micro"
)

// Dump is the non-interactive picture of the server (-json / -tree).
type Dump struct {
	Context   ContextInfo            `json:"context"`
	Selected  string                 `json:"selected_context,omitempty"`
	Contexts  []string               `json:"contexts"`
	ConfigDir string                 `json:"config_dir"`
	Server    ServerInfo             `json:"server"`
	JetStream *jetstream.AccountInfo `json:"jetstream,omitempty"`
	JSError   string                 `json:"jetstream_error,omitempty"`
	Streams   []DumpStream           `json:"streams"`
	KV        []DumpKV               `json:"kv_buckets"`
	Objects   []DumpObjectStore      `json:"object_stores"`
	Services  []micro.Info           `json:"services"`
	Warnings  []string               `json:"warnings,omitempty"`
	Loaded    time.Time              `json:"loaded"`
}

// DumpStream is a stream with its consumers.
type DumpStream struct {
	*jetstream.StreamInfo
	Consumers    []*jetstream.ConsumerInfo `json:"consumers"`
	ConsumersErr string                    `json:"consumers_error,omitempty"`
}

// DumpKV is a bucket.
type DumpKV struct {
	Bucket      string                   `json:"bucket"`
	Description string                   `json:"description,omitempty"`
	Values      uint64                   `json:"values"`
	History     int64                    `json:"history"`
	TTL         time.Duration            `json:"ttl"`
	Bytes       uint64                   `json:"bytes"`
	Compressed  bool                     `json:"compressed"`
	Config      jetstream.KeyValueConfig `json:"config"`
	Stream      *jetstream.StreamInfo    `json:"stream,omitempty"`
}

// DumpObjectStore is an object bucket with its objects.
type DumpObjectStore struct {
	Bucket      string                  `json:"bucket"`
	Description string                  `json:"description,omitempty"`
	TTL         time.Duration           `json:"ttl"`
	Storage     string                  `json:"storage"`
	Replicas    int                     `json:"replicas"`
	Sealed      bool                    `json:"sealed"`
	Size        uint64                  `json:"size"`
	Objects     []*jetstream.ObjectInfo `json:"objects"`
	ListErr     string                  `json:"objects_error,omitempty"`
}

// BuildDump converts a store.
func BuildDump(s *Store) Dump {
	d := Dump{Context: s.Context, Selected: s.Selected, Contexts: s.Contexts, ConfigDir: s.ConfigDir, Server: s.Server,
		JetStream: s.Account, JSError: s.JSError, Warnings: s.Warnings, Loaded: s.Loaded,
		Streams: []DumpStream{}, KV: []DumpKV{}, Objects: []DumpObjectStore{}, Services: []micro.Info{}}
	for _, st := range s.Streams {
		ds := DumpStream{StreamInfo: st.Info, Consumers: []*jetstream.ConsumerInfo{}}
		for _, c := range st.Consumers {
			ds.Consumers = append(ds.Consumers, c.Info)
		}
		if st.ConsumersErr != nil {
			ds.ConsumersErr = st.ConsumersErr.Error()
		}
		d.Streams = append(d.Streams, ds)
	}
	for _, b := range s.KVs {
		d.KV = append(d.KV, DumpKV{Bucket: b.Name(), Description: b.Status.Config().Description, Values: b.Status.Values(), History: b.Status.History(),
			TTL: b.Status.TTL(), Bytes: b.Status.Bytes(), Compressed: b.Status.IsCompressed(), Config: b.Status.Config(), Stream: b.Info})
	}
	for _, b := range s.Objects {
		do := DumpObjectStore{Bucket: b.Name(), Description: b.Status.Description(), TTL: b.Status.TTL(), Storage: b.Status.Storage().String(),
			Replicas: b.Status.Replicas(), Sealed: b.Status.Sealed(), Size: b.Status.Size(), Objects: b.Objects}
		if do.Objects == nil {
			do.Objects = []*jetstream.ObjectInfo{}
		}
		if b.ListErr != nil {
			do.ListErr = b.ListErr.Error()
		}
		d.Objects = append(d.Objects, do)
	}
	for _, sv := range s.Services {
		d.Services = append(d.Services, sv.Info)
	}
	return d
}

// Tree renders the dump as an indented text listing.
func (d Dump) Tree(now time.Time) string {
	var b strings.Builder
	name := d.Context.Name
	if name == "" {
		name = "(no context)"
	}
	fmt.Fprintf(&b, "%s  %s  server %s %s", name, d.Context.URL, d.Server.Name, d.Server.Version)
	if d.Server.Cluster != "" {
		fmt.Fprintf(&b, "  cluster %s", d.Server.Cluster)
	}
	b.WriteString("\n")
	if d.JetStream == nil {
		fmt.Fprintf(&b, "  JetStream: %s\n", d.JSError)
	} else {
		fmt.Fprintf(&b, "  JetStream: %d streams, %d consumers, %s stored, %s in memory\n", d.JetStream.Streams, d.JetStream.Consumers, Size(d.JetStream.Store), Size(d.JetStream.Memory))
	}
	fmt.Fprintf(&b, "  streams (%d)\n", len(d.Streams))
	for _, s := range d.Streams {
		c := s.Config
		fmt.Fprintf(&b, "    %s  %s R%d  %s msgs  %s  subjects %s  last %s\n", c.Name, StorageName(c.Storage), c.Replicas, Count(s.State.Msgs), Size(s.State.Bytes), strings.Join(c.Subjects, ","), Ago(s.State.LastTime, now))
		for _, cs := range s.Consumers {
			mode := "pull"
			if cs.Config.DeliverSubject != "" {
				mode = "push " + cs.Config.DeliverSubject
			}
			fmt.Fprintf(&b, "      %s  %s  %s  pending %d  ack pending %d  redelivered %d\n", cs.Name, mode, ackName(cs.Config.AckPolicy), cs.NumPending, cs.NumAckPending, cs.NumRedelivered)
		}
	}
	fmt.Fprintf(&b, "  kv buckets (%d)\n", len(d.KV))
	for _, k := range d.KV {
		fmt.Fprintf(&b, "    %s  %d values  history %d  ttl %s  %s\n", k.Bucket, k.Values, k.History, HumanDuration(k.TTL), Size(k.Bytes))
	}
	fmt.Fprintf(&b, "  object stores (%d)\n", len(d.Objects))
	for _, o := range d.Objects {
		fmt.Fprintf(&b, "    %s  %d objects  %s  ttl %s\n", o.Bucket, len(o.Objects), Size(o.Size), HumanDuration(o.TTL))
	}
	if len(d.Services) > 0 {
		fmt.Fprintf(&b, "  services (%d)\n", len(d.Services))
		for _, s := range d.Services {
			fmt.Fprintf(&b, "    %s  %s  v%s  %d endpoints\n", s.Name, s.ID, s.Version, len(s.Endpoints))
		}
	}
	return b.String()
}
