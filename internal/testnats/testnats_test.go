//go:build unix

package testnats

import (
	"bufio"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestServerDiesWithTest runs a test binary that starts a server and
// hangs, kills that binary outright (so no cleanup runs) and checks that
// the server goes away on its own.
func TestServerDiesWithTest(t *testing.T) {
	if os.Getenv("TESTNATS_CHILD") != "" {
		t.Skip("this is the child")
	}
	if _, err := exec.LookPath("nats-server"); err != nil {
		t.Skip("nats-server not installed")
	}
	cmd := exec.Command(os.Args[0], "-test.run", "^TestHangWithServer$", "-test.v")
	cmd.Env = append(os.Environ(), "TESTNATS_CHILD=1")
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// the child prints the server pid once the server is up
	var pid int
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		if line := sc.Text(); strings.HasPrefix(line, "server pid ") {
			pid, _ = strconv.Atoi(strings.TrimPrefix(line, "server pid "))
			break
		}
	}
	if pid == 0 {
		cmd.Process.Kill()
		t.Fatal("no server pid from the child")
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("server %d not running: %v", pid, err)
	}
	cmd.Process.Kill() // SIGKILL: no cleanup of any kind runs in the child
	cmd.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return // gone
		}
		time.Sleep(50 * time.Millisecond)
	}
	syscall.Kill(pid, syscall.SIGKILL)
	t.Fatalf("server %d survived its test binary", pid)
}

// TestHangWithServer is the child of TestServerDiesWithTest: it starts a
// server, reports its pid and never returns.
func TestHangWithServer(t *testing.T) {
	if os.Getenv("TESTNATS_CHILD") == "" {
		t.Skip("only as the child of TestServerDiesWithTest")
	}
	Start(t)
	os.Stdout.WriteString("server pid " + strconv.Itoa(lastPID) + "\n")
	os.Stdout.Sync()
	select {}
}
