#!/bin/sh
#
# Record the whole nats command tree, so a later version can be diffed
# against it to see what was added or removed:
#
#     sh doc/help-dump.sh && git diff doc/nats-help.txt
#
# nats builds its own recursive help, so one call is enough; the index at
# the top is the command list on its own, which is what usually changes.

set -e

cd "$(dirname "$0")"

help=$(nats --help-long)
index=$(printf '%s\n' "$help" | grep -E '^[a-z][a-z0-9-]*( |$)')

{
    echo "nats $(nats --version)"
    echo
    echo "===== commands ====="
    echo
    printf '%s\n' "$index"
    echo
    echo "===== nats --help-long ====="
    echo
    printf '%s\n' "$help"
} >nats-help.txt

echo "doc/nats-help.txt: $(printf '%s\n' "$index" | wc -l | tr -d ' ') commands"
