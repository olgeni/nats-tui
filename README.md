# nats-tui — a terminal front-end to nats

`nats-tui` is a Go + [bubbletea](https://github.com/charmbracelet/bubbletea) /
[huh](https://github.com/charmbracelet/huh) front-end to
[`nats`](https://github.com/nats-io/natscli), the NATS command-line tool. It
shows the server a `nats` context points at as a tree — the JetStream
streams with their consumers, the key-value buckets, the object stores, the
micro services answering discovery — opens everything in dialog-style
editors, and drives `nats` for every change: each action becomes a plan of
`nats` commands that is previewed, confirmed, run and followed by a re-read
of the server. Messages are published, requested and watched live from the
same screens. Nothing is ever changed except through `nats` itself, so the
preview is exactly what you could type in a shell.

It is the sibling of [nsc-tui](https://github.com/olgeni/nsc-tui), which does
the same for `nsc`, and shares its look and keys.

```
go install github.com/olgeni/nats-tui@latest     # or: go build -o nats-tui .
nats-tui                                         # nats's selected context
nats-tui -context prod                           # a named context (nats --context)
nats-tui -s nats://localhost:4222                # a server, no context
nats-tui -mouse                                  # with mouse support (m toggles it at runtime)
nats-tui -json | jq '.streams[].config.name'     # non-interactive dump; -tree for a text listing
go test ./...                                    # unit tests + server-backed tests (skipped without nats-server)
man ./nats-tui.1                                 # manual page
```

## Main screen

The tree has the context on top, then the streams (a stream expands into
its consumers), the key-value buckets, the object stores and the services.
The header shows the server, its version and cluster, the round-trip time,
the JetStream usage of the account and the credentials in use.

| Key                                | Action                                                                                                                                                                                                           |
| ---------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `↑/↓` `j/k` `pgup/pgdn` `home/end` | move                                                                                                                                                                                                             |
| `→` / `←` / `space` / `*`          | expand / collapse a stream or a section (`*`: all)                                                                                                                                                               |
| `/`                                | filter by name, subject or description (`esc` clears)                                                                                                                                                            |
| `enter`                            | details of the entity; `J` toggles its raw JSON                                                                                                                                                                  |
| `e`                                | edit the stream, consumer, bucket, object store or context                                                                                                                                                       |
| `a`                                | add what the row holds: a consumer (on a stream), a key (on a bucket), a file (on an object store), a bucket or store (on their section), a stream elsewhere                                                     |
| `A`                                | add a stream                                                                                                                                                                                                     |
| `d` / `del`                        | delete the stream, consumer, bucket or object store                                                                                                                                                              |
| `p`                                | purge a stream: everything, one subject, up to a sequence, or all but the last few                                                                                                                               |
| `v`                                | messages of a stream, one page at a time (`[` `]` page, `g` jumps to a sequence, `f` keeps a subject, `d` deletes a message); on a bucket its keys, on an object store its objects                               |
| `t`                                | subjects held in a stream with their message counts                                                                                                                                                              |
| `n`                                | next messages of a pull consumer (`nats consumer next`), acknowledged, nak'ed, terminated or left alone                                                                                                          |
| `u`                                | pause / resume a consumer                                                                                                                                                                                        |
| `K` / `O`                          | keys of a bucket (put, edit, history, revert, delete, purge, watch) / objects of an object store (put, get, delete, watch)                                                                                       |
| `s`                                | subscribe live to a stream's subjects, or to what you type                                                                                                                                                       |
| `P` / `R`                          | publish a message (`nats pub`) / send a request and show the reply (`nats request`)                                                                                                                              |
| `w`                                | watch a bucket or an object store live                                                                                                                                                                           |
| `E`                                | events: JetStream advisories and metrics, server events                                                                                                                                                          |
| `I`                                | account information                                                                                                                                                                                              |
| `M`                                | monitoring: server list, info, reports, ping, checks, rtt (the server commands need a system account context)                                                                                                    |
| `T`                                | reports: stream report, consumer report, account statistics, `stream find`                                                                                                                                       |
| `C`                                | contexts: list, use, select as default, add, edit, copy, delete, validate                                                                                                                                        |
| `S`                                | use another context (every command then carries `--context`)                                                                                                                                                     |
| `b` / `B`                          | backup a stream to a directory / restore a backup                                                                                                                                                                |
| `y`                                | copy a stream's configuration to a new stream                                                                                                                                                                    |
| `x`                                | seal a stream or an object store (irreversible)                                                                                                                                                                  |
| `m`                                | toggle the mouse (or start with `-mouse`): click a row to select it, click it again to open it, wheel scrolls; in editors a click focuses a field, a second click edits or toggles it; hold shift to select text |
| `r`                                | re-read the server                                                                                                                                                                                               |
| `h` / `?`                          | key map / help, `q` quit                                                                                                                                                                                         |

Sealed streams and paused consumers are shown in yellow; consumers are
muted under their stream.

## Editors

Every entity is edited in the same kind of dialog: labelled fields moved
with `↑/↓`/`tab`, `enter` to edit a text field in place (`enter` keeps,
`esc` reverts), `space` for checkboxes, `←/→` for choices, `ctrl+s` for OK.
Lists (subjects, sources, tags, metadata) are comma-separated; metadata is
written `key=value`. Limits are `-1` for unlimited; sizes take `k`/`m`/`g`/`t`
(decimal) or `kib`/`mib`/`gib`; durations are `30s`, `5m`, `2h`, `1d`,
`1w`, `1y`, blank for none.

Only what changed is passed to `nats`, and the preview shows the commands:

```
[1/1] apply the changes (nats shows the difference it applied)
      nats --context prod stream edit ORDERS --max-msgs=500 --deny-purge -f
```

`nats stream edit` and `nats consumer edit` print the difference they
applied, which the result screen shows. Some settings are fixed at creation
— a stream's storage, a consumer's pull/push mode and its deliver, ack and
replay policies — and the plan says so in a note rather than sending a flag
the server would refuse. A few `nats` details the plans know:

- A new stream or consumer is created with `--defaults`, so `nats` never
  prompts; only what differs from its defaults is passed, plus the storage
  and the subjects.
- An unlimited duration is `0` on edit (`--max-age=0`); `-1` is refused
  there although `stream add` takes it.
- `--republish-destination` needs a `--republish-source`, so a blank source
  is sent as `>`.
- A consumer with `--ack=none` cannot carry an ack wait or a pending limit.
- Deny delete and deny purge cannot be lifted once set, and neither the
  storage nor the retention of an existing stream can change: the editor
  accepts the change, the plan drops it and says so in a note.
- A value or a message body that spans several lines is piped to
  `nats kv put` / `nats pub --force-stdin` rather than passed as an argument.

## Messages and live screens

`v` on a stream reads a page of its messages (an ordered consumer, no state
on the server); `enter` shows one with its headers, JSON indented and
binary hex-dumped. `s` subscribes to subjects and shows what arrives, `w`
watches a bucket (every change with its revision, current values first) or
an object store, `E` follows the JetStream advisories, metrics and server
events. New entries are followed unless you scroll back; `end` follows
again, `/` filters by subject or body, `c` clears, `enter` opens an entry,
`P` publishes to its subject, `esc` stops the subscription. The last 5000
entries are kept.

Subscriptions run in the client library over the same connection; what
`nats sub` would print is what the screen shows, without parsing it.

## Contexts

A context is a file under `~/.config/nats/context` holding a server URL
and credentials; `nats` uses the selected one unless `--context` is given.
`nats-tui` does the same: `-context` or `S` uses one, and every command
then carries `--context` so what runs matches what is shown. `C` lists
them; `enter` uses one, `S` makes it the default, `e` edits it (`nats
context add` on an existing name keeps what is not given), `V` validates it
by connecting. Without any connection — a context that does not answer —
the main screen says why and still offers `C`, `S` and `r`.

## Non-interactive use

`nats-tui -json` prints one JSON document with the context, the known
contexts, the connection facts (server id, name, version, cluster, max
payload, RTT), the JetStream account information, every stream with its
configuration, state and consumers, the buckets with their configuration,
the object stores with their objects, and the services. `-tree` prints the
same as an indented text listing. Both exit 1 only when the server cannot
be reached; a server without JetStream is reported, not an error.

## Tests

```sh
go test ./...            # everything, a few seconds
go test -v ./cli         # the server-backed tests, verbose (SKIP without nats-server)
go vet ./...
```

The server-backed tests start a throwaway `nats-server -js` on a free port
under `t.TempDir()`, point a temporary `nats` context at it (through
`XDG_CONFIG_HOME`), run the plans the editors would produce through the real
`nats`, read the result back with the client library and compare — the same
round trip the TUI makes. They skip themselves when `nats-server` or `nats`
is not installed.
