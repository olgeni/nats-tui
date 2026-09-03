#!/bin/sh
# Regenerates the README screenshots: starts a throwaway nats-server with
# demo data, drives nats-tui in tmux and snaps the screens with freeze
# (https://github.com/charmbracelet/freeze). Needs nats-server, nats, tmux,
# jq and freeze. Run from the repository root: sh doc/screenshots.sh

set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/nats-tui-shots.XXXXXX")
export XDG_CONFIG_HOME="$tmp/config"
session=nats-tui-shots

cleanup() {
	tmux kill-session -t "$session" 2>/dev/null || true
	[ -n "${server:-}" ] && kill "$server" 2>/dev/null || true
	rm -rf "$tmp"
}
trap cleanup EXIT INT TERM

# ---------------------------------------------------------------- server
mkdir -p "$tmp/store" "$tmp/ports"
nats-server -js -p -1 -a 127.0.0.1 -sd "$tmp/store" --ports_file_dir "$tmp/ports" >"$tmp/server.log" 2>&1 &
server=$!
for _ in $(seq 1 50); do
	[ -f "$tmp/ports/nats-server_$server.ports" ] && break
	sleep 0.1
done
url=$(jq -r '.nats[0]' "$tmp/ports/nats-server_$server.ports")
nats context add acme --server "$url" --description "Acme production" --select >/dev/null

# ---------------------------------------------------------------- demo data
q() { "$@" >/dev/null 2>&1; }

q nats stream add ORDERS --subjects 'orders.>' --description 'Orders taken by the shop' --max-age 30d --dupe-window 2m --defaults
q nats consumer add ORDERS fulfilment --pull --ack explicit --deliver all --max-deliver 5 --wait 30s --defaults
q nats consumer add ORDERS billing --pull --ack explicit --deliver all --filter orders.paid --defaults
q nats consumer add ORDERS audit --target audit.orders --ack none --deliver all --defaults
q nats stream add SHIPMENTS --subjects 'shipments.>' --description 'Parcels handed to the carriers' --defaults
q nats consumer add SHIPMENTS tracking --pull --ack explicit --deliver all --defaults
q nats stream add EVENTS --subjects 'events.>' --description 'Application events, kept for an hour' --storage memory --max-age 1h --max-msgs 10000 --defaults
q nats consumer add EVENTS metrics --pull --ack all --deliver new --defaults

q nats pub orders.new --count 12 -H Content-Type:application/json '{"id": {{Count}}, "customer": "cust-{{Count}}", "total": {{Count}}9.90, "currency": "EUR"}'
q nats pub orders.paid --count 7 -H Content-Type:application/json '{"id": {{Count}}, "method": "card", "captured": true}'
q nats pub shipments.dispatched --count 5 '{"order": {{Count}}, "carrier": "DHL", "tracking": "{{ID}}"}'
q nats pub events.login --count 40 '{"user": "u{{Count}}", "at": "{{TimeStamp}}"}'
q nats consumer next ORDERS fulfilment --count 3 --ack

q nats kv add CONFIG --history 5 --description 'Runtime settings of the services'
q nats kv put CONFIG service.orders.replicas 3
q nats kv put CONFIG service.orders.timeout 30s
q nats kv put CONFIG feature.new_checkout true
q nats kv put CONFIG region eu-west-1
q nats kv put CONFIG service.orders.replicas 4
q nats kv put CONFIG service.orders.replicas 5

q nats object add ASSETS --description 'Static files served by the shop'
printf '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><circle cx="32" cy="32" r="30" fill="#f2a900"/></svg>\n' >"$tmp/logo.svg"
seq 1 400 | sed 's/^/Line & of the terms of service./' >"$tmp/terms.txt"
printf '# Release 2.4\n\n- Faster checkout\n- New carriers\n' >"$tmp/release-notes.md"
q nats object put ASSETS "$tmp/logo.svg" --name logo.svg -f
q nats object put ASSETS "$tmp/terms.txt" --name terms.txt --description 'Terms of service' -f
q nats object put ASSETS "$tmp/release-notes.md" --name release-notes.md -f

# ---------------------------------------------------------------- the tui
(cd "$root" && go build -o "$tmp/nats-tui" .)
tmux kill-session -t "$session" 2>/dev/null || true
tmux new-session -d -s "$session" -x 110 -y 28 -e "XDG_CONFIG_HOME=$XDG_CONFIG_HOME" "$tmp/nats-tui"
tmux set -t "$session" escape-time 0
sleep 2

# key KEY... sends keys one at a time: an Escape followed too closely by
# another key reaches the program as a single Alt chord.
key() {
	for k in "$@"; do
		tmux send-keys -t "$session" "$k"
		case $k in
		Escape) sleep 0.4 ;;
		*) sleep 0.15 ;;
		esac
	done
}

# shot NAME KEY... sends the keys, waits, and snaps the pane
shot() {
	name=$1
	shift
	[ $# -gt 0 ] && key "$@"
	sleep 0.8
	tmux capture-pane -t "$session" -p -e >"$tmp/$name.ansi"
	printf '%s: %s\n' "$name" "$(tmux capture-pane -t "$session" -p | sed -n '1p')"
	freeze --language ansi --window --font.family Menlo -o "$root/doc/$name.png" "$tmp/$name.ansi" >/dev/null </dev/null
	# a screenshot holds about a thousand colors, so quantizing it to a
	# palette cuts the file to a quarter with nothing visible lost
	if command -v pngquant >/dev/null 2>&1; then
		if pngquant --quality 60-90 --speed 1 --strip --skip-if-larger \
			--force --output "$tmp/$name.png" "$root/doc/$name.png" 2>/dev/null; then
			mv "$tmp/$name.png" "$root/doc/$name.png"
		fi
	fi
	echo "doc/$name.png"
}

# The rows of the tree: the context, the Streams section, then the streams
# in name order (EVENTS, ORDERS, SHIPMENTS). Escape on the main screen
# quits, so it is only ever sent to leave another screen.

# main screen: ORDERS expanded, its consumers listed
shot main Down Down Down Right
# details of the stream
shot details Enter
# its editor
shot editor Escape e
# a plan: add a consumer to ORDERS, then preview the nats command
key Escape a Enter
tmux send-keys -t "$session" -l reports
shot plan Enter C-s
# the messages of the stream
shot messages Escape v
# the keys of a bucket: K on a stream asks which bucket, there is one
shot keys Escape K Enter
# a live subscription: s offers the stream's subjects, ctrl+s accepts them
key Escape s C-s
sleep 1
q nats pub orders.new --count 6 --sleep 50ms -H Content-Type:application/json '{"id": {{Count}}, "customer": "cust-{{Count}}", "total": {{Count}}9.90, "currency": "EUR"}'
shot live
