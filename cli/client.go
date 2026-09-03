package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/jsm.go/natscontext"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nats.go/micro"
)

// Timeout bounds every request made while loading.
const Timeout = 5 * time.Second

// Client is the connection nats-tui reads through: the same context (or
// server flags) the nats commands of the plans use.
type Client struct {
	Settings Settings
	Ctx      *natscontext.Context
	NC       *nats.Conn
	JS       jetstream.JetStream // nil when the connection has no JetStream
	errs     *asyncErrors
}

// asyncErrors collects what the client library reports asynchronously.
type asyncErrors struct {
	mu   sync.Mutex
	list []string
	subs map[*nats.Subscription][]string
}

func (a *asyncErrors) add(sub *nats.Subscription, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	msg := err.Error()
	if sub != nil {
		msg = sub.Subject + ": " + msg
		if a.subs == nil {
			a.subs = map[*nats.Subscription][]string{}
		}
		a.subs[sub] = append(a.subs[sub], msg)
	}
	a.list = append(a.list, msg)
}

// take returns and clears the errors reported for the subscriptions.
func (a *asyncErrors) take(subs []*nats.Subscription) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, s := range subs {
		out = append(out, a.subs[s]...)
		delete(a.subs, s)
	}
	return out
}

// Errors returns and clears every asynchronous error seen so far.
func (c *Client) Errors() []string {
	c.errs.mu.Lock()
	defer c.errs.mu.Unlock()
	out := c.errs.list
	c.errs.list = nil
	return out
}

// Connect opens the connection described by the settings: the named
// context, the selected one, or nats's defaults when there is none.
func Connect(s Settings) (*Client, error) {
	var opts []natscontext.Option
	if s.Server != "" {
		opts = append(opts, natscontext.WithServerURL(s.Server))
	}
	if s.Creds != "" {
		opts = append(opts, natscontext.WithCreds(s.Creds))
	}
	ctx, err := natscontext.New(s.Context, true, opts...)
	if err != nil {
		return nil, err
	}
	timeout := Timeout
	if s.Timeout != "" {
		if d, err := time.ParseDuration(s.Timeout); err == nil && d > 0 {
			timeout = d
		}
	}
	// the client library logs asynchronous errors (a subscription the
	// account may not make, a slow consumer) to stderr, which would land on
	// the screen: they are collected instead and shown by the live screens
	errs := &asyncErrors{}
	nc, err := ctx.Connect(nats.Name("nats-tui"), nats.Timeout(timeout), nats.MaxReconnects(-1), nats.ReconnectWait(time.Second),
		nats.ErrorHandler(func(_ *nats.Conn, sub *nats.Subscription, err error) { errs.add(sub, err) }))
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", ctx.ServerURL(), err)
	}
	c := &Client{Settings: s, Ctx: ctx, NC: nc, errs: errs}
	var jsOpts []jetstream.JetStreamOpt
	if p := ctx.JSAPIPrefix(); p != "" {
		c.JS, err = jetstream.NewWithAPIPrefix(nc, p, jsOpts...)
	} else if d := ctx.JSDomain(); d != "" {
		c.JS, err = jetstream.NewWithDomain(nc, d, jsOpts...)
	} else {
		c.JS, err = jetstream.New(nc, jsOpts...)
	}
	if err != nil {
		c.JS = nil
	}
	return c, nil
}

// Close drains the connection.
func (c *Client) Close() {
	if c != nil && c.NC != nil {
		c.NC.Close()
	}
}

// Name is the context name in use ("" when connecting without one).
func (c *Client) Name() string {
	if c.Ctx == nil {
		return ""
	}
	return c.Ctx.Name
}

// ---------------------------------------------------------------- store

// Store is everything the main screen shows: the context and connection,
// the JetStream account, and the streams, consumers, buckets and services
// of the account.
type Store struct {
	ContextName string   // the context in use ("" when none)
	Selected    string   // nats's selected context
	Contexts    []string // every known context
	ConfigDir   string   // where the contexts live
	Context     ContextInfo
	Server      ServerInfo
	Account     *jetstream.AccountInfo // nil without JetStream
	JSError     string                 // why there is no JetStream
	Streams     []*Stream
	KVs         []*Bucket
	Objects     []*ObjectBucket
	Services    []*Service
	Warnings    []string
	Loaded      time.Time
}

// ContextInfo is the connection configuration, as the context holds it.
type ContextInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url"`
	Creds       string `json:"creds,omitempty"`
	NKey        string `json:"nkey,omitempty"`
	User        string `json:"user,omitempty"`
	Token       bool   `json:"token,omitempty"`
	Cert        string `json:"cert,omitempty"`
	Key         string `json:"key,omitempty"`
	CA          string `json:"ca,omitempty"`
	NscURL      string `json:"nsc,omitempty"`
	JSDomain    string `json:"js_domain,omitempty"`
	JSAPIPrefix string `json:"js_api_prefix,omitempty"`
	JSEventPfx  string `json:"js_event_prefix,omitempty"`
	InboxPrefix string `json:"inbox_prefix,omitempty"`
	Path        string `json:"path,omitempty"`
}

// ServerInfo is what the connection tells about the server.
type ServerInfo struct {
	URL         string        `json:"url"`
	Addr        string        `json:"addr"`
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Version     string        `json:"version"`
	Cluster     string        `json:"cluster,omitempty"`
	MaxPayload  int64         `json:"max_payload"`
	Headers     bool          `json:"headers"`
	TLS         bool          `json:"tls"`
	AuthNeeded  bool          `json:"auth_required"`
	RTT         time.Duration `json:"rtt"`
	Discovered  []string      `json:"discovered,omitempty"`
	ClientCount int           `json:"-"`
}

// Stream is a JetStream stream with its consumers.
type Stream struct {
	Info         *jetstream.StreamInfo
	Consumers    []*Consumer
	ConsumersErr error
}

// Name is the stream name.
func (s *Stream) Name() string { return s.Info.Config.Name }

// Consumer is a consumer of a stream.
type Consumer struct {
	Info   *jetstream.ConsumerInfo
	Stream *Stream
}

// Name is the consumer name.
func (c *Consumer) Name() string { return c.Info.Name }

// Pull reports whether the consumer is pull-based.
func (c *Consumer) Pull() bool { return c.Info.Config.DeliverSubject == "" }

// Bucket is a key-value bucket.
type Bucket struct {
	Status jetstream.KeyValueStatus
	Info   *jetstream.StreamInfo // the backing stream
}

// Name is the bucket name.
func (b *Bucket) Name() string { return b.Status.Bucket() }

// ObjectBucket is an object store bucket with its objects.
type ObjectBucket struct {
	Status  jetstream.ObjectStoreStatus
	Info    *jetstream.StreamInfo
	Objects []*jetstream.ObjectInfo
	ListErr error
}

// Name is the bucket name.
func (b *ObjectBucket) Name() string { return b.Status.Bucket() }

// Service is one instance of a micro service found by discovery.
type Service struct {
	Info  micro.Info
	Stats *micro.Stats
}

// Stream finds a stream by name.
func (s *Store) Stream(name string) *Stream {
	for _, st := range s.Streams {
		if st.Name() == name {
			return st
		}
	}
	return nil
}

// KV finds a bucket by name.
func (s *Store) KV(name string) *Bucket {
	for _, b := range s.KVs {
		if b.Name() == name {
			return b
		}
	}
	return nil
}

// Object finds an object bucket by name.
func (s *Store) Object(name string) *ObjectBucket {
	for _, b := range s.Objects {
		if b.Name() == name {
			return b
		}
	}
	return nil
}

// ConsumerCount counts the consumers of every stream.
func (s *Store) ConsumerCount() int {
	n := 0
	for _, st := range s.Streams {
		n += len(st.Consumers)
	}
	return n
}

// HasJetStream reports whether the account has JetStream.
func (s *Store) HasJetStream() bool { return s.Account != nil }

// Load reads the state of the server: streams with their consumers,
// buckets, object stores and services. It never fails for a missing
// JetStream: the store then says so and holds the connection facts only.
func (c *Client) Load() (*Store, error) {
	s := &Store{Loaded: time.Now(), ContextName: c.Name()}
	s.Selected = natscontext.SelectedContext()
	s.Contexts = natscontext.KnownContexts()
	sort.Strings(s.Contexts)
	s.ConfigDir = ConfigDir()
	s.Context = contextInfo(c.Ctx)
	if !c.NC.IsConnected() {
		return s, fmt.Errorf("not connected to %s (%s)", s.Context.URL, c.NC.Status())
	}
	s.Server = c.serverInfo()
	if c.JS == nil {
		s.JSError = "JetStream is not available on this connection"
		return s, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	ai, err := c.JS.AccountInfo(ctx)
	if err != nil {
		s.JSError = jsErrorText(err)
		return s, nil
	}
	s.Account = ai
	// every stream, including the ones backing buckets and object stores
	infos := map[string]*jetstream.StreamInfo{}
	lister := c.JS.ListStreams(ctx)
	for si := range lister.Info() {
		infos[si.Config.Name] = si
	}
	if err := lister.Err(); err != nil {
		s.Warnings = append(s.Warnings, "list streams: "+err.Error())
	}
	names := make([]string, 0, len(infos))
	for n := range infos {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		si := infos[n]
		switch {
		case strings.HasPrefix(n, "KV_"):
			continue
		case strings.HasPrefix(n, "OBJ_"):
			continue
		}
		s.Streams = append(s.Streams, &Stream{Info: si})
	}
	// consumers, a few streams at a time
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, st := range s.Streams {
		wg.Add(1)
		go func(st *Stream) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			st.Consumers, st.ConsumersErr = c.consumers(st)
		}(st)
	}
	wg.Wait()
	// buckets
	kvl := c.JS.KeyValueStores(ctx)
	for st := range kvl.Status() {
		s.KVs = append(s.KVs, &Bucket{Status: st, Info: infos["KV_"+st.Bucket()]})
	}
	if err := kvl.Error(); err != nil {
		s.Warnings = append(s.Warnings, "list buckets: "+err.Error())
	}
	sort.Slice(s.KVs, func(i, j int) bool { return s.KVs[i].Name() < s.KVs[j].Name() })
	// object stores, with their objects
	ol := c.JS.ObjectStores(ctx)
	for st := range ol.Status() {
		s.Objects = append(s.Objects, &ObjectBucket{Status: st, Info: infos["OBJ_"+st.Bucket()]})
	}
	if err := ol.Error(); err != nil {
		s.Warnings = append(s.Warnings, "list object stores: "+err.Error())
	}
	sort.Slice(s.Objects, func(i, j int) bool { return s.Objects[i].Name() < s.Objects[j].Name() })
	for _, ob := range s.Objects {
		wg.Add(1)
		go func(ob *ObjectBucket) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ob.Objects, ob.ListErr = c.Objects(ob.Name())
		}(ob)
	}
	wg.Wait()
	s.Services = c.Services(300 * time.Millisecond)
	return s, nil
}

func (c *Client) consumers(st *Stream) ([]*Consumer, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	stream, err := c.JS.Stream(ctx, st.Name())
	if err != nil {
		return nil, err
	}
	var out []*Consumer
	l := stream.ListConsumers(ctx)
	for ci := range l.Info() {
		out = append(out, &Consumer{Info: ci, Stream: st})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, l.Err()
}

// serverInfo collects the connection facts.
func (c *Client) serverInfo() ServerInfo {
	nc := c.NC
	si := ServerInfo{
		URL: nc.ConnectedUrlRedacted(), Addr: nc.ConnectedAddr(), ID: nc.ConnectedServerId(), Name: nc.ConnectedServerName(),
		Version: nc.ConnectedServerVersion(), Cluster: nc.ConnectedClusterName(), MaxPayload: nc.MaxPayload(),
		Headers: nc.HeadersSupported(), TLS: nc.TLSRequired(), AuthNeeded: nc.AuthRequired(), Discovered: nc.DiscoveredServers(),
	}
	if rtt, err := nc.RTT(); err == nil {
		si.RTT = rtt
	}
	return si
}

func contextInfo(ctx *natscontext.Context) ContextInfo {
	if ctx == nil {
		return ContextInfo{URL: nats.DefaultURL}
	}
	return ContextInfo{
		Name: ctx.Name, Description: ctx.Description(), URL: ctx.ServerURL(), Creds: ctx.Creds(), NKey: ctx.NKey(),
		User: ctx.User(), Token: ctx.Token() != "", Cert: ctx.Certificate(), Key: ctx.Key(), CA: ctx.CA(), NscURL: ctx.NscURL(),
		JSDomain: ctx.JSDomain(), JSAPIPrefix: ctx.JSAPIPrefix(), JSEventPfx: ctx.JSEventPrefix(), InboxPrefix: ctx.InboxPrefix(),
		Path: ctx.Path(),
	}
}

// ConfigDir is where nats keeps its contexts.
func ConfigDir() string {
	if p := os.Getenv("XDG_CONFIG_HOME"); p != "" {
		return filepath.Join(p, "nats")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "nats")
}

// ReadContext loads one context by name without connecting.
func ReadContext(name string) (ContextInfo, error) {
	ctx, err := natscontext.New(name, true)
	if err != nil {
		return ContextInfo{}, err
	}
	return contextInfo(ctx), nil
}

func jsErrorText(err error) string {
	switch {
	case errors.Is(err, jetstream.ErrJetStreamNotEnabled):
		return "the server has no JetStream"
	case errors.Is(err, jetstream.ErrJetStreamNotEnabledForAccount):
		return "JetStream is not enabled for this account"
	case errors.Is(err, nats.ErrNoResponders), errors.Is(err, context.DeadlineExceeded):
		return "no JetStream (no response from the API)"
	}
	return err.Error()
}

// ---------------------------------------------------------------- reads

// Message is one stored or received message.
type Message struct {
	Subject string      `json:"subject"`
	Reply   string      `json:"reply,omitempty"`
	Seq     uint64      `json:"seq,omitempty"`
	Time    time.Time   `json:"time"`
	Header  nats.Header `json:"header,omitempty"`
	Data    []byte      `json:"data"`
}

// Messages reads up to n messages of a stream starting at seq (1: the
// first), optionally only those matching filter.
func (c *Client) Messages(stream string, seq uint64, n int, filter string) ([]Message, error) {
	if c.JS == nil {
		return nil, errors.New("no JetStream")
	}
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	st, err := c.JS.Stream(ctx, stream)
	if err != nil {
		return nil, err
	}
	cfg := jetstream.OrderedConsumerConfig{DeliverPolicy: jetstream.DeliverByStartSequencePolicy, OptStartSeq: max(seq, 1)}
	if filter != "" {
		cfg.FilterSubjects = []string{filter}
	}
	cons, err := st.OrderedConsumer(ctx, cfg)
	if err != nil {
		return nil, err
	}
	batch, err := cons.FetchNoWait(n)
	if err != nil {
		return nil, err
	}
	var out []Message
	for m := range batch.Messages() {
		msg := Message{Subject: m.Subject(), Header: m.Headers(), Data: m.Data()}
		if md, err := m.Metadata(); err == nil {
			msg.Seq, msg.Time = md.Sequence.Stream, md.Timestamp
		}
		out = append(out, msg)
	}
	return out, batch.Error()
}

// GetMsg reads one message of a stream by sequence.
func (c *Client) GetMsg(stream string, seq uint64) (*Message, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	st, err := c.JS.Stream(ctx, stream)
	if err != nil {
		return nil, err
	}
	raw, err := st.GetMsg(ctx, seq)
	if err != nil {
		return nil, err
	}
	return &Message{Subject: raw.Subject, Seq: raw.Sequence, Time: raw.Time, Header: raw.Header, Data: raw.Data}, nil
}

// Subjects lists the subjects held in a stream with their message counts.
func (c *Client) Subjects(stream, filter string) (map[string]uint64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	st, err := c.JS.Stream(ctx, stream)
	if err != nil {
		return nil, err
	}
	if filter == "" {
		filter = ">"
	}
	info, err := st.Info(ctx, jetstream.WithSubjectFilter(filter))
	if err != nil {
		return nil, err
	}
	return info.State.Subjects, nil
}

// StreamInfo re-reads one stream.
func (c *Client) StreamInfo(name string) (*jetstream.StreamInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	st, err := c.JS.Stream(ctx, name)
	if err != nil {
		return nil, err
	}
	return st.Info(ctx)
}

// KVEntry is one key of a bucket.
type KVEntry struct {
	Key      string    `json:"key"`
	Revision uint64    `json:"revision"`
	Created  time.Time `json:"created"`
	Op       string    `json:"op"`
	Delta    uint64    `json:"delta,omitempty"`
	Value    []byte    `json:"value"`
}

func kvEntry(e jetstream.KeyValueEntry) KVEntry {
	return KVEntry{Key: e.Key(), Revision: e.Revision(), Created: e.Created(), Op: OpName(e.Operation()), Delta: e.Delta(), Value: e.Value()}
}

// OpName is a bucket operation as nats kv history prints it.
func OpName(op jetstream.KeyValueOp) string {
	switch op {
	case jetstream.KeyValueDelete:
		return "DELETE"
	case jetstream.KeyValuePurge:
		return "PURGE"
	}
	return "PUT"
}

// Keys reads the keys of a bucket with their latest values.
func (c *Client) Keys(bucket string) ([]KVEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	kv, err := c.JS.KeyValue(ctx, bucket)
	if err != nil {
		return nil, err
	}
	w, err := kv.WatchAll(ctx, jetstream.IgnoreDeletes())
	if err != nil {
		return nil, err
	}
	defer w.Stop()
	var out []KVEntry
	for e := range w.Updates() {
		if e == nil {
			break
		}
		out = append(out, kvEntry(e))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// History reads every revision of a key.
func (c *Client) History(bucket, key string) ([]KVEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	kv, err := c.JS.KeyValue(ctx, bucket)
	if err != nil {
		return nil, err
	}
	hist, err := kv.History(ctx, key)
	if err != nil {
		return nil, err
	}
	var out []KVEntry
	for _, e := range hist {
		out = append(out, kvEntry(e))
	}
	return out, nil
}

// Objects lists the objects of a bucket.
func (c *Client) Objects(bucket string) ([]*jetstream.ObjectInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	os, err := c.JS.ObjectStore(ctx, bucket)
	if err != nil {
		return nil, err
	}
	objs, err := os.List(ctx)
	if errors.Is(err, jetstream.ErrNoObjectsFound) {
		return nil, nil
	}
	sort.Slice(objs, func(i, j int) bool { return objs[i].Name < objs[j].Name })
	return objs, err
}

// Services discovers the micro services of the account: every instance
// answering a PING within wait.
func (c *Client) Services(wait time.Duration) []*Service {
	subj, err := micro.ControlSubject(micro.InfoVerb, "", "")
	if err != nil {
		return nil
	}
	inbox := c.NC.NewInbox()
	sub, err := c.NC.SubscribeSync(inbox)
	if err != nil {
		return nil
	}
	defer sub.Unsubscribe()
	if err := c.NC.PublishRequest(subj, inbox, nil); err != nil {
		return nil
	}
	var out []*Service
	deadline := time.Now().Add(wait)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			break
		}
		m, err := sub.NextMsg(left)
		if err != nil {
			break
		}
		var info micro.Info
		if json.Unmarshal(m.Data, &info) == nil && info.Name != "" {
			out = append(out, &Service{Info: info})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Info.Name != out[j].Info.Name {
			return out[i].Info.Name < out[j].Info.Name
		}
		return out[i].Info.ID < out[j].Info.ID
	})
	return out
}

// ServiceStats asks one service instance for its statistics.
func (c *Client) ServiceStats(name, id string) (*micro.Stats, error) {
	subj, err := micro.ControlSubject(micro.StatsVerb, name, id)
	if err != nil {
		return nil, err
	}
	m, err := c.NC.Request(subj, nil, Timeout)
	if err != nil {
		return nil, err
	}
	var st micro.Stats
	if err := json.Unmarshal(m.Data, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

// ---------------------------------------------------------------- live

// Event is one item of a live screen: a received message, a bucket change
// or an object change.
type Event struct {
	At      time.Time
	Kind    string // "msg", "put", "delete", "purge", "object", "error", "info"
	Subject string
	Reply   string
	Header  nats.Header
	Data    []byte
	Seq     uint64
	Text    string // error or info text
}

// Live is a running subscription feeding events to a channel; Stop ends
// it and closes the channel.
type Live struct {
	Events <-chan Event
	stop   func()
	once   sync.Once
}

// Stop ends the subscription.
func (l *Live) Stop() { l.once.Do(l.stop) }

const liveBuffer = 4096

// Subscribe delivers the messages of the subjects (core NATS).
func (c *Client) Subscribe(subjects []string, queue string) (*Live, error) {
	ch := make(chan Event, liveBuffer)
	var subs []*nats.Subscription
	push := func(m *nats.Msg) {
		ev := Event{At: time.Now(), Kind: "msg", Subject: m.Subject, Reply: m.Reply, Header: m.Header, Data: m.Data}
		select {
		case ch <- ev:
		default: // full: the screen is behind; drop rather than block the client
		}
	}
	for _, s := range subjects {
		var sub *nats.Subscription
		var err error
		if queue != "" {
			sub, err = c.NC.QueueSubscribe(s, queue, push)
		} else {
			sub, err = c.NC.Subscribe(s, push)
		}
		if err != nil {
			for _, x := range subs {
				x.Unsubscribe()
			}
			return nil, err
		}
		subs = append(subs, sub)
	}
	c.NC.Flush()
	// a permission violation arrives asynchronously, right after the flush
	go func() {
		time.Sleep(200 * time.Millisecond)
		for _, e := range c.errs.take(subs) {
			select {
			case ch <- Event{At: time.Now(), Kind: "error", Text: e}:
			default:
			}
		}
	}()
	stop := func() {
		for _, x := range subs {
			x.Unsubscribe()
		}
		// let the error reporter above finish before the channel closes
		time.Sleep(250 * time.Millisecond)
		close(ch)
	}
	return &Live{Events: ch, stop: stop}, nil
}

// WatchKV delivers the changes of a bucket (all keys, or one pattern).
func (c *Client) WatchKV(bucket, keys string, history bool) (*Live, error) {
	ctx, cancel := context.WithCancel(context.Background())
	kv, err := c.JS.KeyValue(ctx, bucket)
	if err != nil {
		cancel()
		return nil, err
	}
	var opts []jetstream.WatchOpt
	if history {
		opts = append(opts, jetstream.IncludeHistory())
	}
	if keys == "" {
		keys = ">"
	}
	w, err := kv.Watch(ctx, keys, opts...)
	if err != nil {
		cancel()
		return nil, err
	}
	ch := make(chan Event, liveBuffer)
	go func() {
		defer close(ch)
		for e := range w.Updates() {
			if e == nil {
				ch <- Event{At: time.Now(), Kind: "info", Text: "— current values above; changes follow —"}
				continue
			}
			ev := Event{At: e.Created(), Kind: strings.ToLower(OpName(e.Operation())), Subject: e.Key(), Data: e.Value(), Seq: e.Revision()}
			select {
			case ch <- ev:
			default:
			}
		}
	}()
	stop := func() {
		w.Stop()
		cancel()
	}
	return &Live{Events: ch, stop: stop}, nil
}

// WatchObjects delivers the changes of an object bucket.
func (c *Client) WatchObjects(bucket string) (*Live, error) {
	ctx, cancel := context.WithCancel(context.Background())
	os, err := c.JS.ObjectStore(ctx, bucket)
	if err != nil {
		cancel()
		return nil, err
	}
	w, err := os.Watch(ctx)
	if err != nil {
		cancel()
		return nil, err
	}
	ch := make(chan Event, liveBuffer)
	go func() {
		defer close(ch)
		for oi := range w.Updates() {
			if oi == nil {
				ch <- Event{At: time.Now(), Kind: "info", Text: "— current objects above; changes follow —"}
				continue
			}
			kind := "object"
			if oi.Deleted {
				kind = "delete"
			}
			ev := Event{At: oi.ModTime, Kind: kind, Subject: oi.Name, Text: fmt.Sprintf("%s, %d chunk(s), %s", Size(oi.Size), oi.Chunks, oi.Digest)}
			select {
			case ch <- ev:
			default:
			}
		}
	}()
	stop := func() {
		w.Stop()
		cancel()
	}
	return &Live{Events: ch, stop: stop}, nil
}

// EventSubjects are what E subscribes to: JetStream advisories and
// metrics, and the server events of the system account when the user
// has it.
func EventSubjects(eventPrefix string) []string {
	p := "$JS.EVENT"
	if eventPrefix != "" {
		p = eventPrefix
	}
	return []string{p + ".ADVISORY.>", p + ".METRIC.>", "$SYS.SERVER.>", "$SYS.ACCOUNT.>"}
}

// Next pulls up to n messages from a pull consumer without acknowledging
// them (the CLI's consumer next does that, with the ack of choice).
func (c *Client) Peek(stream, consumer string, n int) ([]Message, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	cons, err := c.JS.Consumer(ctx, stream, consumer)
	if err != nil {
		return nil, err
	}
	batch, err := cons.FetchNoWait(n)
	if err != nil {
		return nil, err
	}
	var out []Message
	for m := range batch.Messages() {
		msg := Message{Subject: m.Subject(), Header: m.Headers(), Data: m.Data()}
		if md, err := m.Metadata(); err == nil {
			msg.Seq, msg.Time = md.Sequence.Stream, md.Timestamp
		}
		m.Nak()
		out = append(out, msg)
	}
	return out, batch.Error()
}
