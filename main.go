// Command nats-tui is a terminal user interface for nats, the NATS
// command-line tool: it shows the streams, consumers, buckets and services
// of a server as a tree, edits them in dialogs, and drives nats for every
// change, previewing the commands before running them. Messages are
// published, requested and watched live from the same screens.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/olgeni/nats-tui/cli"
	"github.com/olgeni/nats-tui/ui"
)

const version = "1.0.0"

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `usage: nats-tui [-mouse] [connection flags]
       nats-tui -json | -tree [connection flags]     (non-interactive: dump the server)

Terminal front-end to nats. The connection flags are nats's own and are
passed to every nats command; without them nats's selected context and
environment apply.

`)
		flag.PrintDefaults()
	}
	var s cli.Settings
	showVersion := flag.Bool("version", false, "print version and exit")
	mouse := flag.Bool("mouse", false, "enable mouse support in the TUI (click/wheel; hold shift to select text); m toggles it at runtime")
	asJSON := flag.Bool("json", false, "non-interactive: print the server (context, connection, account, streams, consumers, buckets, services) as JSON and exit")
	asTree := flag.Bool("tree", false, "non-interactive: print the streams, consumers and buckets as a text tree and exit")
	flag.StringVar(&s.Context, "context", "", "nats configuration context to use (default: the selected one, or $NATS_CONTEXT)")
	flag.StringVar(&s.Server, "s", "", "NATS server URL(s), overriding the context (nats -s)")
	flag.StringVar(&s.Creds, "creds", "", "user credentials file, overriding the context (nats --creds)")
	flag.StringVar(&s.Timeout, "timeout", "", "time to wait on responses from NATS (nats --timeout, default 5s)")
	flag.StringVar(&s.Binary, "nats", "nats", "the nats binary to run")
	flag.Parse()
	if *showVersion {
		fmt.Println("nats-tui", version)
		return
	}
	if _, err := s.Available(); err != nil {
		fmt.Fprintf(os.Stderr, "nats-tui: %v (install nats: https://github.com/nats-io/natscli)\n", err)
		os.Exit(1)
	}
	if *asJSON || *asTree {
		os.Exit(dump(s, *asJSON))
	}
	if err := ui.Run(s, *mouse); err != nil {
		fmt.Fprintln(os.Stderr, "nats-tui:", err)
		os.Exit(1)
	}
}

// dump prints the server as JSON or as a tree; exit status 0, or 1 when it
// could not be reached at all.
func dump(s cli.Settings, asJSON bool) int {
	c, err := cli.Connect(s)
	if err != nil {
		fmt.Fprintln(os.Stderr, "nats-tui:", err)
		return 1
	}
	defer c.Close()
	st, err := c.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "nats-tui:", err)
		return 1
	}
	d := cli.BuildDump(st)
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(d); err != nil {
			fmt.Fprintln(os.Stderr, "nats-tui:", err)
			return 1
		}
		return 0
	}
	fmt.Print(d.Tree(time.Now()))
	return 0
}
