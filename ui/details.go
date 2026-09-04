package ui

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/olgeni/nats-tui/cli"
)

// showDetails opens the details screen for a node; back is where esc goes.
func (m *Model) showDetails(n node, back screen) {
	m.detail, m.rawJSON, m.detailBk = n, false, back
	m.vp.SetContent(m.detailText())
	m.vp.GotoTop()
	m.vp.SetXOffset(0)
	m.scr = scrDetails
}

// showJSON opens the details screen on the raw information.
func (m *Model) showJSON(n node) {
	m.detail, m.rawJSON, m.detailBk = n, true, m.scr
	m.vp.SetContent(m.detailText())
	m.vp.GotoTop()
	m.vp.SetXOffset(0)
	m.scr = scrDetails
}

func (m *Model) detailsView() string {
	n := m.detail
	title := m.nodeTitle(n)
	if m.rawJSON {
		title += " (JSON)"
	}
	var help string
	switch n.kind {
	case kStream:
		help = helpLine("e", "edit", "v", "messages", "t", "subjects", "a", "add consumer", "P", "purge", "s", "subscribe", "J", "json", "esc", "back")
	case kConsumer:
		help = helpLine("e", "edit", "n", "next", "u", "pause/resume", "J", "json", "esc", "back")
	case kKV:
		help = helpLine("e", "edit", "K", "keys", "w", "watch", "J", "json", "esc", "back")
	case kObject:
		help = helpLine("e", "edit", "O", "objects", "w", "watch", "J", "json", "esc", "back")
	case kContext:
		help = helpLine("e", "edit the context", "C", "contexts", "I", "account", "J", "json", "esc", "back")
	default:
		help = helpLine("J", "json/details", "esc", "back", "↑↓ ←→", "scroll")
	}
	return m.frame(title, m.vp.View(), help)
}

func (m *Model) updateDetails(msg tea.Msg) (tea.Model, tea.Cmd) {
	n := m.detail
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc", "q", "enter":
			m.scr = m.detailBk
			return m, nil
		case "J":
			m.rawJSON = !m.rawJSON
			m.vp.SetContent(m.detailText())
			m.vp.GotoTop()
			m.vp.SetXOffset(0)
			return m, nil
		case "e":
			return m, m.editEntity(n)
		case "v":
			if st := n.streamOf(); st != nil {
				return m, m.showMessages(st, 0, "")
			}
		case "t":
			if st := n.streamOf(); st != nil {
				return m, m.showSubjects(st, "")
			}
		case "a":
			return m, m.addChild(n)
		case "P":
			if st := n.streamOf(); st != nil {
				return m, m.purgeStream(st)
			}
		case "s":
			return m, m.subscribe(n)
		case "n":
			if n.kind == kConsumer {
				return m, m.nextMessages(n.cons)
			}
		case "u":
			if n.kind == kConsumer {
				return m, m.pauseResume(n.cons)
			}
		case "K":
			if n.kind == kKV {
				return m, m.showKeys(n.kv)
			}
		case "O":
			if n.kind == kObject {
				return m, m.showObjects(n.obj)
			}
		case "w":
			return m, m.watch(n)
		case "C":
			return m, m.showContexts()
		case "I":
			return m, m.accountInfo()
		case "p":
			return m, m.publish(defaultSubject(n))
		case "R":
			return m, m.request(defaultSubject(n))
		}
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

// refreshDetail re-renders the details screen for the same entity after a
// reload (the pointers are stale then).
func (m *Model) refreshDetail() {
	n := m.detail
	switch n.kind {
	case kStream:
		if st := m.store.Stream(n.stream.Name()); st != nil {
			n.stream = st
		} else {
			m.scr = scrMain
			return
		}
	case kConsumer:
		st := m.store.Stream(n.cons.Stream.Name())
		if st == nil {
			m.scr = scrMain
			return
		}
		found := false
		for _, c := range st.Consumers {
			if c.Name() == n.cons.Name() {
				n.cons, found = c, true
			}
		}
		if !found {
			m.scr = scrMain
			return
		}
	case kKV:
		if b := m.store.KV(n.kv.Name()); b != nil {
			n.kv = b
		} else {
			m.scr = scrMain
			return
		}
	case kObject:
		if b := m.store.Object(n.obj.Name()); b != nil {
			n.obj = b
		} else {
			m.scr = scrMain
			return
		}
	case kService:
		found := false
		for _, s := range m.store.Services {
			if s.Info.ID == n.svc.Info.ID {
				n.svc, found = s, true
			}
		}
		if !found {
			m.scr = scrMain
			return
		}
	}
	m.detail = n
	m.vp.SetContent(m.detailText())
}

// detailText renders the details of the node.
func (m *Model) detailText() string {
	n := m.detail
	if m.rawJSON {
		var v any
		switch n.kind {
		case kContext:
			v = struct {
				Context cli.ContextInfo        `json:"context"`
				Server  cli.ServerInfo         `json:"server"`
				Account *jetstream.AccountInfo `json:"jetstream,omitempty"`
			}{m.store.Context, m.store.Server, m.store.Account}
		case kStream:
			v = n.stream.Info
		case kConsumer:
			v = n.cons.Info
		case kKV:
			v = struct {
				Config jetstream.KeyValueConfig `json:"config"`
				Stream *jetstream.StreamInfo    `json:"stream,omitempty"`
			}{n.kv.Status.Config(), n.kv.Info}
		case kObject:
			v = struct {
				Objects []*jetstream.ObjectInfo `json:"objects"`
				Stream  *jetstream.StreamInfo   `json:"stream,omitempty"`
			}{n.obj.Objects, n.obj.Info}
		case kService:
			v = n.svc.Info
		}
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return styleErr.Render(err.Error())
		}
		return string(b)
	}
	d := &detailWriter{width: m.width}
	switch n.kind {
	case kContext:
		m.contextDetail(d)
	case kStream:
		m.streamDetail(d, n.stream)
	case kConsumer:
		m.consumerDetail(d, n.cons)
	case kKV:
		m.kvDetail(d, n.kv)
	case kObject:
		m.objectDetail(d, n.obj)
	case kService:
		m.serviceDetail(d, n.svc)
	}
	return d.String()
}

// detailWriter builds label/value text with sections.
type detailWriter struct {
	b     strings.Builder
	width int
}

func (d *detailWriter) section(title string) {
	if d.b.Len() > 0 {
		d.b.WriteString("\n")
	}
	d.b.WriteString(styleHeader.Render(title) + "\n")
}

func (d *detailWriter) row(label, value string) {
	if value == "" {
		value = styleMuted.Render("(none)")
	}
	d.b.WriteString(" " + styleLabel.Render(fit(label, 24)) + " " + d.value(value) + "\n")
}

// value keeps a value inside the screen: paths lose their head (the tail
// names the file), everything else its tail.
func (d *detailWriter) value(v string) string {
	avail := d.width - 27
	if avail < 8 || lipgloss.Width(v) <= avail {
		return v
	}
	if strings.ContainsRune(v, '\x1b') { // styled: truncate escape-aware
		return clamp(avail, v)
	}
	if strings.HasPrefix(v, "/") || strings.HasPrefix(v, "~") {
		return fitLeft(v, avail)
	}
	return fit(v, avail)
}

func (d *detailWriter) rows(label string, values []string) {
	if len(values) == 0 {
		d.row(label, "")
		return
	}
	for i, v := range values {
		if i == 0 {
			d.row(label, v)
		} else {
			d.b.WriteString(" " + fit("", 24) + " " + d.value(v) + "\n")
		}
	}
}

func (d *detailWriter) warn(label, value string) {
	d.b.WriteString(" " + styleLabel.Render(fit(label, 24)) + " " + styleWarn.Render(d.value("⚠ "+value)) + "\n")
}

func (d *detailWriter) String() string { return d.b.String() }

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func (m *Model) contextDetail(d *detailWriter) {
	s := m.store
	c := s.Context
	d.section("Context")
	d.row("Name", c.Name)
	d.row("Description", c.Description)
	d.row("File", c.Path)
	d.row("Server URL", c.URL)
	switch {
	case c.Creds != "":
		d.row("Credentials", c.Creds)
	case c.User != "":
		d.row("User", c.User)
	case c.NKey != "":
		d.row("NKey", c.NKey)
	case c.Token:
		d.row("Token", "set")
	case c.NscURL != "":
		d.row("nsc lookup", c.NscURL)
	default:
		d.row("Credentials", "")
	}
	if c.Cert != "" || c.Key != "" || c.CA != "" {
		d.row("TLS certificate", c.Cert)
		d.row("TLS key", c.Key)
		d.row("TLS CA", c.CA)
	}
	if c.JSDomain != "" || c.JSAPIPrefix != "" || c.JSEventPfx != "" || c.InboxPrefix != "" {
		d.row("JetStream domain", c.JSDomain)
		d.row("JetStream API prefix", c.JSAPIPrefix)
		d.row("JetStream event prefix", c.JSEventPfx)
		d.row("Inbox prefix", c.InboxPrefix)
	}
	sv := s.Server
	d.section("Connection")
	d.row("Connected to", sv.URL)
	d.row("Address", sv.Addr)
	d.row("Server name", sv.Name)
	d.row("Server id", sv.ID)
	d.row("Version", sv.Version)
	d.row("Cluster", sv.Cluster)
	d.row("RTT", sv.RTT.String())
	d.row("Max payload", cli.Size(uint64(max(sv.MaxPayload, 0))))
	d.row("Headers", yesNo(sv.Headers))
	d.row("TLS", yesNo(sv.TLS))
	d.row("Auth required", yesNo(sv.AuthNeeded))
	d.rows("Other servers", sv.Discovered)
	d.section("JetStream")
	if a := s.Account; a != nil {
		accountRows(d, a)
	} else {
		d.warn("Status", s.JSError)
	}
}

// accountRows renders a JetStream account.
func accountRows(d *detailWriter, a *jetstream.AccountInfo) {
	d.row("Domain", a.Domain)
	d.row("API level", fmt.Sprint(a.API.Level))
	d.row("API calls", fmt.Sprintf("%d (%d errors, %d in flight)", a.API.Total, a.API.Errors, a.API.Inflight))
	tierRows(d, "", a.Tier)
	for _, name := range sortedKeys(a.Tiers) {
		d.section("Tier " + name)
		tierRows(d, "", a.Tiers[name])
	}
}

func tierRows(d *detailWriter, prefix string, t jetstream.Tier) {
	l := t.Limits
	d.row(prefix+"Memory", cli.Size(t.Memory)+" of "+cli.Bytes(l.MaxMemory))
	d.row(prefix+"Storage", cli.Size(t.Store)+" of "+cli.Bytes(l.MaxStore))
	if t.ReservedMemory > 0 || t.ReservedStore > 0 {
		d.row(prefix+"Reserved", cli.Size(t.ReservedMemory)+" memory, "+cli.Size(t.ReservedStore)+" storage")
	}
	d.row(prefix+"Streams", fmt.Sprintf("%d of %s", t.Streams, cli.Limit(int64(l.MaxStreams))))
	d.row(prefix+"Consumers", fmt.Sprintf("%d of %s", t.Consumers, cli.Limit(int64(l.MaxConsumers))))
	d.row(prefix+"Max ack pending", cli.Limit(int64(l.MaxAckPending)))
	d.row(prefix+"Max memory stream", cli.Bytes(l.MemoryMaxStreamBytes))
	d.row(prefix+"Max storage stream", cli.Bytes(l.StoreMaxStreamBytes))
	d.row(prefix+"Max bytes required", yesNo(l.MaxBytesRequired))
}

func (m *Model) streamDetail(d *detailWriter, s *cli.Stream) {
	i := s.Info
	c := i.Config
	st := i.State
	d.section("Stream")
	d.row("Name", c.Name)
	d.row("Description", c.Description)
	d.rows("Subjects", c.Subjects)
	d.row("Created", cli.Date(i.Created))
	d.row("Storage", fmt.Sprintf("%s, %d replica(s), %s compression", cli.StorageName(c.Storage), c.Replicas, cli.StreamSpecFrom(c).Compression))
	d.row("Retention", c.Retention.String())
	disc := c.Discard.String()
	if c.DiscardNewPerSubject {
		disc += " (per subject)"
	}
	d.row("Discard", disc)
	if c.Placement != nil {
		d.row("Placement", strings.TrimSpace(c.Placement.Cluster+" "+strings.Join(c.Placement.Tags, ",")))
	}
	if c.Sealed {
		d.warn("Sealed", "no more changes are possible")
	}
	d.section("State")
	d.row("Messages", cli.Count(st.Msgs))
	d.row("Bytes", fmt.Sprintf("%s (%d)", cli.Size(st.Bytes), st.Bytes))
	d.row("First sequence", fmt.Sprintf("%d  %s", st.FirstSeq, cli.Date(st.FirstTime)))
	d.row("Last sequence", fmt.Sprintf("%d  %s", st.LastSeq, cli.Date(st.LastTime)))
	d.row("Subjects held", cli.Count(st.NumSubjects))
	d.row("Deleted", fmt.Sprint(st.NumDeleted))
	d.row("Consumers", fmt.Sprint(st.Consumers))
	d.section("Limits")
	d.row("Max messages", cli.Limit(c.MaxMsgs))
	d.row("Max per subject", cli.Limit(c.MaxMsgsPerSubject))
	d.row("Max bytes", cli.Bytes(c.MaxBytes))
	d.row("Max age", cli.HumanDuration(c.MaxAge))
	d.row("Max message size", cli.Bytes(int64(c.MaxMsgSize)))
	d.row("Max consumers", cli.Limit(int64(c.MaxConsumers)))
	d.row("Duplicate window", cli.HumanDuration(c.Duplicates))
	if c.ConsumerLimits.InactiveThreshold > 0 || c.ConsumerLimits.MaxAckPending > 0 {
		d.row("Consumer limits", fmt.Sprintf("inactive threshold %s, max ack pending %d", cli.HumanDuration(c.ConsumerLimits.InactiveThreshold), c.ConsumerLimits.MaxAckPending))
	}
	d.section("Options")
	d.row("Acknowledge publishes", yesNo(!c.NoAck))
	d.row("Allow rollup", yesNo(c.AllowRollup))
	d.row("Deny delete", yesNo(c.DenyDelete))
	d.row("Deny purge", yesNo(c.DenyPurge))
	d.row("Allow direct get", yesNo(c.AllowDirect))
	d.row("Mirror direct get", yesNo(c.MirrorDirect))
	d.row("Per-message TTL", yesNo(c.AllowMsgTTL))
	d.row("Message schedules", yesNo(c.AllowMsgSchedules))
	d.row("Batch publishing", yesNo(c.AllowBatchPublish))
	d.row("Atomic publishing", yesNo(c.AllowAtomicPublish))
	if c.SubjectDeleteMarkerTTL > 0 {
		d.row("Delete marker TTL", cli.HumanDuration(c.SubjectDeleteMarkerTTL))
	}
	if c.RePublish != nil {
		v := c.RePublish.Source + " → " + c.RePublish.Destination
		if c.RePublish.HeadersOnly {
			v += " (headers only)"
		}
		d.row("Republish", v)
	}
	if c.SubjectTransform != nil {
		d.row("Subject transform", c.SubjectTransform.Source+" → "+c.SubjectTransform.Destination)
	}
	if c.Mirror != nil {
		d.section("Mirror")
		sourceRows(d, c.Mirror, i.Mirror)
	}
	for k, src := range c.Sources {
		d.section(fmt.Sprintf("Source %d", k+1))
		var info *jetstream.StreamSourceInfo
		for _, si := range i.Sources {
			if si.Name == src.Name {
				info = si
			}
		}
		sourceRows(d, src, info)
	}
	if md := cli.Metadata(c.Metadata); len(md) > 0 {
		d.section("Metadata")
		d.rows("Entries", md)
	}
	if i.Cluster != nil && (i.Cluster.Leader != "" || len(i.Cluster.Replicas) > 0) {
		d.section("Cluster")
		d.row("Name", i.Cluster.Name)
		d.row("Leader", i.Cluster.Leader)
		for _, p := range i.Cluster.Replicas {
			state := "current"
			if !p.Current {
				state = "behind"
			}
			if p.Offline {
				state = "OFFLINE"
			}
			d.row("Replica "+p.Name, fmt.Sprintf("%s, seen %s ago, lag %d", state, cli.HumanDuration(p.Active), p.Lag))
		}
	}
	d.section(fmt.Sprintf("Consumers (%d)", s.ConsumerCount()))
	for _, cs := range s.Consumers {
		d.row(cs.Name(), consumerLine(cs))
	}
	if s.ConsumersErr != nil {
		d.warn("Error", s.ConsumersErr.Error())
	}
	if !s.Loaded {
		d.row("", "not fetched yet: expand the stream in the tree")
	} else if len(s.Consumers) == 0 && s.ConsumersErr == nil {
		d.row("", "")
	}
}

func sourceRows(d *detailWriter, src *jetstream.StreamSource, info *jetstream.StreamSourceInfo) {
	d.row("Stream", src.Name)
	if src.FilterSubject != "" {
		d.row("Filter", src.FilterSubject)
	}
	if src.OptStartSeq > 0 {
		d.row("Start sequence", fmt.Sprint(src.OptStartSeq))
	}
	if src.OptStartTime != nil {
		d.row("Start time", cli.Date(*src.OptStartTime))
	}
	for _, t := range src.SubjectTransforms {
		d.row("Transform", t.Source+" → "+t.Destination)
	}
	if src.External != nil {
		d.row("External API", src.External.APIPrefix)
		d.row("External deliver", src.External.DeliverPrefix)
	}
	if info != nil {
		d.row("Lag", fmt.Sprintf("%d messages, seen %s ago", info.Lag, cli.HumanDuration(info.Active)))
	}
}

func consumerLine(c *cli.Consumer) string {
	i := c.Info
	parts := []string{"pull"}
	if !c.Pull() {
		parts[0] = "push → " + i.Config.DeliverSubject
	}
	parts = append(parts, "ack "+ackName(i.Config), fmt.Sprintf("%d pending", i.NumPending), fmt.Sprintf("%d ack pending", i.NumAckPending))
	if f := consumerFilter(i.Config); f != "" {
		parts = append(parts, "filter "+f)
	}
	if i.Paused {
		parts = append(parts, "PAUSED")
	}
	return strings.Join(parts, "  ")
}

func (m *Model) consumerDetail(d *detailWriter, cs *cli.Consumer) {
	i := cs.Info
	c := i.Config
	d.section("Consumer")
	d.row("Name", i.Name)
	d.row("Stream", i.Stream)
	d.row("Description", c.Description)
	d.row("Created", cli.Date(i.Created))
	if c.Durable != "" {
		d.row("Durable", c.Durable)
	} else {
		d.row("Durable", styleMuted.Render("no (ephemeral)"))
	}
	if cs.Pull() {
		d.row("Mode", "pull")
	} else {
		d.row("Mode", "push to "+c.DeliverSubject)
		d.row("Deliver group", c.DeliverGroup)
		d.row("Flow control", yesNo(c.FlowControl))
		d.row("Idle heartbeat", cli.HumanDuration(c.IdleHeartbeat))
		d.row("Push bound", yesNo(i.PushBound))
	}
	if i.Paused {
		d.warn("Paused", "for another "+cli.HumanDuration(i.PauseRemaining))
	}
	d.section("Delivery")
	dp := c.DeliverPolicy.String()
	switch c.DeliverPolicy {
	case jetstream.DeliverByStartSequencePolicy:
		dp += fmt.Sprintf(" %d", c.OptStartSeq)
	case jetstream.DeliverByStartTimePolicy:
		if c.OptStartTime != nil {
			dp += " " + cli.Date(*c.OptStartTime)
		}
	}
	d.row("Deliver policy", dp)
	d.row("Filter", consumerFilter(c))
	d.row("Ack policy", ackName(c))
	d.row("Ack wait", cli.HumanDuration(c.AckWait))
	d.row("Max deliver", cli.Limit(int64(c.MaxDeliver)))
	d.row("Max ack pending", cli.Limit(int64(c.MaxAckPending)))
	d.row("Replay", c.ReplayPolicy.String())
	if len(c.BackOff) > 0 {
		var bo []string
		for _, b := range c.BackOff {
			bo = append(bo, cli.HumanDuration(b))
		}
		d.row("Backoff", strings.Join(bo, ", "))
	}
	d.row("Headers only", yesNo(c.HeadersOnly))
	if c.RateLimit > 0 {
		d.row("Rate limit", fmt.Sprintf("%d bps", c.RateLimit))
	}
	d.row("Sample frequency", c.SampleFrequency)
	d.row("Inactive threshold", cli.HumanDuration(c.InactiveThreshold))
	if cs.Pull() {
		d.row("Max waiting pulls", cli.Limit(int64(c.MaxWaiting)))
		d.row("Max pull batch", cli.Limit(int64(c.MaxRequestBatch)))
		d.row("Max pull expires", cli.HumanDuration(c.MaxRequestExpires))
		d.row("Max pull bytes", cli.Limit(int64(c.MaxRequestMaxBytes)))
	}
	if len(c.PriorityGroups) > 0 {
		d.row("Priority groups", strings.Join(c.PriorityGroups, ", "))
	}
	d.section("Storage")
	d.row("Replicas", fmt.Sprintf("%d (0: the stream's)", c.Replicas))
	d.row("Memory storage", yesNo(c.MemoryStorage))
	d.section("State")
	d.row("Delivered", fmt.Sprintf("consumer seq %d, stream seq %d", i.Delivered.Consumer, i.Delivered.Stream))
	if i.Delivered.Last != nil {
		d.row("Last delivery", cli.Date(*i.Delivered.Last))
	}
	d.row("Ack floor", fmt.Sprintf("consumer seq %d, stream seq %d", i.AckFloor.Consumer, i.AckFloor.Stream))
	if i.AckFloor.Last != nil {
		d.row("Last ack", cli.Date(*i.AckFloor.Last))
	}
	d.row("Unprocessed", cli.Count(i.NumPending))
	d.row("Ack pending", fmt.Sprint(i.NumAckPending))
	d.row("Redelivered", fmt.Sprint(i.NumRedelivered))
	d.row("Waiting pulls", fmt.Sprint(i.NumWaiting))
	for _, pg := range i.PriorityGroups {
		d.row("Group "+pg.Group, fmt.Sprintf("pinned %s since %s", pg.PinnedClientID, cli.Date(pg.PinnedTS)))
	}
	if md := cli.Metadata(c.Metadata); len(md) > 0 {
		d.section("Metadata")
		d.rows("Entries", md)
	}
	if i.Cluster != nil && i.Cluster.Leader != "" {
		d.section("Cluster")
		d.row("Name", i.Cluster.Name)
		d.row("Leader", i.Cluster.Leader)
	}
}

func (m *Model) kvDetail(d *detailWriter, b *cli.Bucket) {
	st := b.Status
	c := st.Config()
	d.section("Bucket")
	d.row("Name", st.Bucket())
	d.row("Description", c.Description)
	d.row("Values", cli.Count(st.Values())+" (revisions of every key, history included)")
	d.row("Size", cli.Size(st.Bytes()))
	d.row("History per key", fmt.Sprint(st.History()))
	d.row("TTL", cli.HumanDuration(st.TTL()))
	d.row("Limit marker TTL", cli.HumanDuration(st.LimitMarkerTTL()))
	d.row("Max value size", cli.Bytes(int64(c.MaxValueSize)))
	d.row("Max bucket size", cli.Bytes(c.MaxBytes))
	d.row("Compressed", yesNo(st.IsCompressed()))
	d.row("Backing store", st.BackingStore())
	if b.Info != nil {
		i := b.Info
		d.row("Stream", i.Config.Name)
		d.row("Created", cli.Date(i.Created))
		d.row("Storage", fmt.Sprintf("%s, %d replica(s)", cli.StorageName(i.Config.Storage), i.Config.Replicas))
		d.row("Last change", cli.Date(i.State.LastTime))
		if i.Config.Placement != nil {
			d.row("Placement", strings.TrimSpace(i.Config.Placement.Cluster+" "+strings.Join(i.Config.Placement.Tags, ",")))
		}
		if i.Config.RePublish != nil {
			d.row("Republish", i.Config.RePublish.Source+" → "+i.Config.RePublish.Destination)
		}
		if i.Config.Mirror != nil {
			d.row("Mirror of", strings.TrimPrefix(i.Config.Mirror.Name, "KV_"))
		}
		for _, s := range i.Config.Sources {
			d.row("Source", strings.TrimPrefix(s.Name, "KV_"))
		}
	}
	if md := cli.Metadata(st.Metadata()); len(md) > 0 {
		d.section("Metadata")
		d.rows("Entries", md)
	}
}

func (m *Model) objectDetail(d *detailWriter, b *cli.ObjectBucket) {
	st := b.Status
	d.section("Object store")
	d.row("Name", st.Bucket())
	d.row("Description", st.Description())
	if b.Loaded {
		d.row("Objects", fmt.Sprint(len(b.Objects)))
	}
	d.row("Size", cli.Size(st.Size()))
	d.row("TTL", cli.HumanDuration(st.TTL()))
	d.row("Storage", fmt.Sprintf("%s, %d replica(s)", cli.StorageName(st.Storage()), st.Replicas()))
	d.row("Compressed", yesNo(st.IsCompressed()))
	if st.Sealed() {
		d.warn("Sealed", "no more changes are possible")
	}
	d.row("Backing store", st.BackingStore())
	if b.Info != nil {
		d.row("Stream", b.Info.Config.Name)
		d.row("Created", cli.Date(b.Info.Created))
		d.row("Max bucket size", cli.Bytes(b.Info.Config.MaxBytes))
		d.row("Last change", cli.Date(b.Info.State.LastTime))
	}
	if md := cli.Metadata(st.Metadata()); len(md) > 0 {
		d.section("Metadata")
		d.rows("Entries", md)
	}
	if !b.Loaded {
		d.section("Objects")
		d.row("", "not listed yet: O lists them")
		return
	}
	d.section(fmt.Sprintf("Objects (%d)", len(b.Objects)))
	for _, o := range b.Objects {
		d.row(o.Name, fmt.Sprintf("%s, %d chunk(s), %s", cli.Size(o.Size), o.Chunks, cli.Date(o.ModTime)))
	}
	if b.ListErr != nil {
		d.warn("Error", b.ListErr.Error())
	}
	if len(b.Objects) == 0 && b.ListErr == nil {
		d.row("", "")
	}
}

func (m *Model) serviceDetail(d *detailWriter, s *cli.Service) {
	i := s.Info
	d.section("Service")
	d.row("Name", i.Name)
	d.row("Instance id", i.ID)
	d.row("Version", i.Version)
	d.row("Description", i.Description)
	if md := cli.Metadata(i.Metadata); len(md) > 0 {
		d.rows("Metadata", md)
	}
	d.section(fmt.Sprintf("Endpoints (%d)", len(i.Endpoints)))
	for _, e := range i.Endpoints {
		v := e.Subject
		if e.QueueGroup != "" {
			v += "  queue " + e.QueueGroup
		}
		d.row(e.Name, v)
	}
	d.section("Statistics")
	switch {
	case s.StatsErr != nil:
		d.warn("Error", s.StatsErr.Error())
	case s.Stats == nil:
		d.row("", "asking the instance…")
	default:
		d.row("Started", cli.Date(s.Stats.Started))
		for _, e := range s.Stats.Endpoints {
			d.row(e.Name, fmt.Sprintf("%d requests, %d errors, avg %s", e.NumRequests, e.NumErrors, e.AverageProcessingTime))
			if e.LastError != "" {
				d.warn("  last error", e.LastError)
			}
		}
	}
}
