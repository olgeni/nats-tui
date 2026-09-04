package ui

import "strings"

const helpText = `nats-tui — a terminal front-end to nats (streams, consumers, buckets, messages)

The main screen is the tree of the server the current context points at:
its streams with their consumers, its key-value buckets, its object stores
and the services answering discovery. Every action that changes something
builds a plan of nats commands, shows them, and runs them only after you
confirm; the server is re-read afterwards. Nothing is ever changed except
through nats itself, so what you see in the preview is exactly what you
could type in a shell. Reading goes through the NATS client library over
the same context, and the live screens (subscribe, watch, events) too.

Main screen
  ↑/↓ j/k        move                 enter          details of the entity
  →/←            expand / collapse    * / space      toggle the row (space) or all (*)
  /              filter by name, subject or description (esc clears)
  e              edit (stream, consumer, bucket, object store, or the context)
  a              add what the row holds: a consumer (on a stream or consumer),
                 a bucket or object store (on their section), a key (on a
                 bucket), a file (on an object store), a stream (elsewhere)
  A              add a stream        d / del        delete the entity
  P              purge a stream (all, a subject, up to a sequence, keep n)
  v              messages of a stream, paged; on a bucket its keys, on an
                 object store its objects
  t              subjects held in a stream, with their message counts
  n              next: pull messages from a pull consumer (nats consumer next)
  u              pause / resume a consumer
  K / O          keys of a bucket / objects of an object store (editing a
                 key runs nats kv update with the revision it was read at)
  s              subscribe live: to a stream's subjects, or to what you type
  p              publish a message (nats pub)
  R              send a request and show the reply (nats request)
  w              watch a bucket or an object store live
  E              events: JetStream advisories and metrics, server events
  I              account information (JetStream usage and limits)
  M              monitoring: server list, info, ping, the server reports,
                 the checks (of the selected stream, consumer or bucket,
                 and of the cluster, a server, a request, a credential),
                 subject mappings, rtt, account info and tls (the server
                 commands need a system account context)
  T              reports: stream report, consumer report, account report,
                 stream find, consumer find, stream gaps
  L              cluster: leader step-down of a stream, a consumer or the
                 meta group, peer removal, leader balancing, consumer reset
                 and unpin, config reload, kick a client, purge an account
  C              contexts: list, use, select, add, edit, copy, delete, validate
  S              use another context (commands then carry --context)
  b / B          backup a stream (or, elsewhere, every stream of the
                 account) to a directory / restore a stream or account backup
  y              copy a stream's configuration to a new stream; on a
                 consumer, copy the consumer
  x              seal a stream or an object store (irreversible)
  J              raw JSON of the selected entity
  m              toggle the mouse (also: nats-tui -mouse): click a row to select
                 it, click it again to open it, wheel scrolls; in editors a
                 click focuses a field, a second click edits or toggles it, OK
                 and Cancel are buttons; hold shift to select text meanwhile
  r              re-read the server   h              key map
  ctrl+r         auto-refresh on/off (also: nats-tui -refresh 5s)
  ?              this help            q / esc / ^C   quit
  ctrl+z         suspend (fg to come back)

Loading
  The consumers of a stream and the objects of a store are fetched when the
  stream is expanded or the store opened, one request each, and kept across
  reloads: a server with hundreds of streams opens fast. A service is asked
  for its request statistics when its details open. ctrl+r re-reads
  the server every 5 seconds (nats-tui -refresh sets the interval) on the
  main screen, the details and the tables.

Editors
  Fields are edited in place: enter starts editing a text field (enter keeps
  the value, esc reverts it), space toggles a checkbox, ←/→ picks a choice,
  ctrl+s is OK. Lists (subjects, sources, metadata…) are comma-separated;
  metadata is written key=value. Limits are -1 for unlimited; sizes take
  k/m/g/t (decimal) or kib/mib/gib; durations are 30s, 5m, 2h, 1d, 1w, 1y
  and blank for none / unlimited. Only what changed is passed to nats, as the
  preview shows; nats stream edit and consumer edit print the difference
  they applied. Some settings are fixed at creation (a stream's storage, a
  consumer's mode, deliver, ack and replay policies): the plan says so in a
  note and leaves them alone.

Messages
  v on a stream shows a page of its messages: [ and ] page through the
  sequences, g jumps to one, f keeps one subject, enter shows a message with
  its headers (JSON is indented, binary is hex-dumped), d deletes it (nats
  stream rmm). t lists the subjects held with their counts; enter on one
  shows its messages, P purges it. a adds a consumer
  filtered on the shown subject (the subjects table offers it too).

Live screens
  s subscribes to subjects (core NATS: what arrives from now on), w watches
  a bucket (every change with its revision) or an object store, E follows
  the JetStream advisories, metrics and server events. New entries are
  followed unless you scroll back; end follows again, / filters by subject
  or body, c clears, enter opens an entry, p publishes to its subject, esc
  stops the subscription. The screen keeps the last 5000 entries.

Publishing and requests
  p asks for a subject, a body, headers (K:V), a count and whether to
  publish through JetStream; a multi-line body is piped to nats pub. The
  message can be scheduled instead of sent: once at an RFC3339 time, once
  after a delay, every interval, or on a six-field cron line (seconds
  first), to a destination subject; the stream holding the publish subject
  must allow message schedules (a stream option). One schedule lives on a
  subject and a new one replaces it. R
  sends a request and shows the reply (or several, with a reply count), as
  nats request prints it. n on a pull consumer fetches its next messages
  with nats consumer next, acknowledged or not as you choose, and shows
  them in the result.

Contexts
  A context is a file under ~/.config/nats/context holding the server URL
  and the credentials; nats uses the selected one unless --context is given.
  nats-tui does the same: -context or S uses one, and every command then
  carries --context so what runs matches what is shown. C lists them; enter
  uses one, S makes it the default, e edits it (nats context add on an
  existing name keeps what is not given), V validates one by connecting.

Monitoring
  M runs the server commands (nats server list, info, ping, the reports:
  connections, jetstream, accounts, health, cpu, mem, routes, gateways,
  leafnodes, downgrade) and shows their output. They need the system
  account: a context whose credentials belong to it. The menu opens with
  nats server check of the selected stream, consumer or bucket, and offers
  the other checks (jetstream, connection, meta, server, request,
  credential) behind a small editor for their thresholds; a check that
  warns or fails exits non-zero, and its report is shown all the same.
  rtt, account info and account tls work with any user. T runs the stream,
  consumer and account reports, stream find, consumer find and stream gaps.

Cluster
  L holds the commands that change the cluster rather than an entity, the
  ones for the selected stream or consumer first: stream and consumer
  cluster step-down and peer-remove, consumer reset (delivery starts again
  from a sequence, or the outstanding messages are delivered again) and
  unpin, stream and consumer cluster balance, server cluster step-down and
  peer-remove, server config reload, server request kick and server
  account purge. Each one is a plan, previewed and confirmed like the
  others; the destructive ones are flagged.

Connection flags
  nats-tui takes nats's own -context, -s, --creds and --timeout and passes
  them to every nats command; without them the selected context and the
  environment ($NATS_URL, $NATS_CONTEXT…) apply, as nats context info shows.
`

// keymapSections is the compact key reference shown by h.
type keyDesc struct{ key, desc string }

var keymapSections = []struct {
	title string
	keys  []keyDesc
}{
	{"Main screen", []keyDesc{
		{"↑/↓ j/k", "move"},
		{"pgup/pgdn", "page"},
		{"home/end", "first / last"},
		{"→/← space *", "expand / collapse"},
		{"/", "filter (arrows edit it)"},
		{"enter", "details"},
		{"e", "edit"},
		{"a / A", "add child / stream"},
		{"d / del", "delete"},
		{"P", "purge stream"},
		{"v", "messages / keys / objects"},
		{"t", "subjects of a stream"},
		{"n", "next messages (consumer)"},
		{"u", "pause / resume consumer"},
		{"K / O", "keys / objects"},
		{"s", "subscribe live"},
		{"p / R", "publish / request"},
		{"w", "watch bucket live"},
		{"E", "events live"},
		{"I", "account info"},
		{"M", "monitoring / checks"},
		{"T", "reports"},
		{"L", "cluster"},
		{"C / S", "contexts / switch"},
		{"b / B", "backup / restore"},
		{"y", "copy stream / consumer"},
		{"x", "seal"},
		{"J", "raw JSON"},
		{"m", "toggle mouse"},
		{"r", "reload"},
		{"ctrl+r", "auto-refresh"},
		{"h / ?", "keys / help"},
		{"ctrl+z", "suspend"},
		{"q / esc / ctrl+c", "quit"},
	}},
	{"Editors", []keyDesc{
		{"↑/↓ tab", "move"},
		{"enter", "edit text / toggle"},
		{"space", "toggle"},
		{"←/→", "choice"},
		{"ctrl+s / f10", "OK"},
		{"esc / q", "cancel"},
	}},
	{"Messages", []keyDesc{
		{"enter", "show the message"},
		{"[ / ]", "older / newer page"},
		{"g", "go to sequence"},
		{"f", "filter subject"},
		{"a", "consumer on the subject"},
		{"d", "delete message"},
	}},
	{"Live screens", []keyDesc{
		{"↑/↓", "browse (stops following)"},
		{"end / space", "follow again"},
		{"/", "filter (arrows edit it)"},
		{"c", "clear"},
		{"enter", "show the entry"},
		{"p", "publish"},
		{"esc", "stop"},
	}},
	{"Tables and text", []keyDesc{
		{"↑/↓", "move / scroll"},
		{"←/→", "scroll wide text"},
		{"enter", "open / use"},
		{"a e d", "add / edit / delete"},
		{"r", "reload"},
		{"esc / q", "back"},
	}},
}

// keymapView renders the key map in aligned columns: two side by side when
// width allows, one otherwise.
func keymapView(width int) string {
	const kw, dw = 17, 28
	col := kw + 1 + dw
	cols := 1
	if width >= 2*col+6 {
		cols = 2
	}
	var b strings.Builder
	for si, sec := range keymapSections {
		if si > 0 {
			b.WriteString("\n")
		}
		b.WriteString(styleHeader.Render(sec.title) + "\n")
		rows := (len(sec.keys) + cols - 1) / cols
		for r := 0; r < rows; r++ {
			line := " "
			for c := 0; c < cols; c++ {
				i := c*rows + r
				if i >= len(sec.keys) {
					break
				}
				k := sec.keys[i]
				line += " " + styleHelpKey.Render(fit(k.key, kw)) + " " + fit(k.desc, dw)
				if c < cols-1 {
					line += "  "
				}
			}
			b.WriteString(strings.TrimRight(line, " ") + "\n")
		}
	}
	return b.String()
}
