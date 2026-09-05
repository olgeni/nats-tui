package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/olgeni/nats-tui/internal/testnats"
)

// TestKeysTableReload covers what a bucket with a TTL does to the keys
// table: the rows are what the server answered when the table was opened,
// so a key that expires afterwards stays on the screen until the table is
// read again. r does that, keeps the cursor where it was, and says so in
// the help line.
func TestKeysTableReload(t *testing.T) {
	m, x := testModel(t)
	testnats.Must(t, x, "kv", "add", "SESSIONS", "--ttl=1s")
	for _, k := range []string{"alice", "bob", "carol"} {
		testnats.Must(t, x, "kv", "put", "SESSIONS", k, "here")
	}
	runCmd(t, m, press(m, "r")) // the new bucket into the store
	for i := 0; i < 20 && m.selected().name() != "SESSIONS"; i++ {
		press(m, "down")
	}
	if m.selected().name() != "SESSIONS" {
		t.Fatal("the bucket is not in the tree")
	}
	runCmd(t, m, press(m, "K"))
	if m.scr != scrTable || m.tbl.kind != tkKeys {
		t.Fatalf("keys table: %v %s", m.scr, m.errMsg)
	}
	if len(m.tbl.rows) != 3 {
		t.Fatalf("want 3 keys, got %d", len(m.tbl.rows))
	}
	if !strings.Contains(m.tbl.help, "reload") {
		t.Errorf("the help line does not offer the reload: %q", m.tbl.help)
	}

	// the cursor survives a reload: the table is fetched again, so nothing
	// else would remember where the user was
	press(m, "down", "down")
	want := m.tbl.cursor
	runCmd(t, m, press(m, "r"))
	if m.scr != scrTable {
		t.Fatalf("r left the table: %v", m.scr)
	}
	if m.tbl.cursor != want {
		t.Errorf("r moved the cursor from %d to %d", want, m.tbl.cursor)
	}

	// the keys expire, and the table only finds out when it is read again
	time.Sleep(2 * time.Second)
	if len(m.tbl.rows) != 3 {
		t.Errorf("the rows changed with no reload: %d", len(m.tbl.rows))
	}
	runCmd(t, m, press(m, "r"))
	if len(m.tbl.rows) != 0 {
		t.Errorf("the expired keys are still listed: %d rows", len(m.tbl.rows))
	}
}
