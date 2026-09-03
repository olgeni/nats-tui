// Package testnats starts a throwaway nats-server for the tests and points
// a temporary nats context at it, so the server-backed tests never touch a
// real server or the user's contexts.
package testnats

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olgeni/nats-tui/cli"
)

// Start runs nats-server with JetStream on a free port under a temporary
// directory and returns the client URL; the server is stopped when the
// test ends. The test is skipped when nats-server or nats is missing.
func Start(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("nats-server"); err != nil {
		t.Skip("nats-server not installed")
	}
	if _, err := exec.LookPath("nats"); err != nil {
		t.Skip("nats not installed")
	}
	dir := t.TempDir()
	cmd := exec.Command("nats-server", "-js", "-p", "-1", "-a", "127.0.0.1", "-sd", filepath.Join(dir, "store"), "--ports_file_dir", dir)
	logf, err := os.Create(filepath.Join(dir, "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
		logf.Close()
	})
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		matches, _ := filepath.Glob(filepath.Join(dir, "nats-server_*.ports"))
		if len(matches) > 0 {
			b, err := os.ReadFile(matches[0])
			if err == nil && len(b) > 0 {
				var ports struct {
					NATS []string `json:"nats"`
				}
				if json.Unmarshal(b, &ports) == nil {
					for _, u := range ports.NATS {
						if strings.Contains(u, "127.0.0.1") {
							return u
						}
					}
					if len(ports.NATS) > 0 {
						return ports.NATS[0]
					}
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("nats-server did not start")
	return ""
}

// Setup starts a server and makes it the selected context of a temporary
// nats configuration directory (XDG_CONFIG_HOME), returning the settings
// that name the context and the runner for it.
func Setup(t *testing.T) (cli.Settings, cli.Exec) {
	t.Helper()
	url := Start(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := cli.Settings{Context: "test"}
	x := cli.Exec{Settings: cli.Settings{}}
	if r := x.Run("context", "add", "test", "--server", url, "--select", "--description", "throwaway test server"); !r.OK() {
		t.Fatalf("nats context add: %s", r.Output())
	}
	return s, cli.Exec{Settings: s}
}

// Must runs a nats command and fails the test when it does.
func Must(t *testing.T, x cli.Exec, args ...string) cli.Result {
	t.Helper()
	r := x.Run(args...)
	if !r.OK() {
		t.Fatalf("nats %s: %s", strings.Join(args, " "), r.Output())
	}
	return r
}
