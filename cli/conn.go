// Package cli drives the nats command-line tool and reads the server it
// points at. Everything that changes state goes through nats itself, as a
// plan of commands that the UI previews and then executes; reading is done
// with the NATS client library over the same context the CLI would use, so
// what the screen shows and what the commands touch is the same server.
package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// Settings is how nats finds its server: the same global flags the CLI
// takes (--context, -s, --creds, --timeout) plus the binary to run. Empty
// values are not passed, so the CLI's own defaults, the selected context
// and the environment ($NATS_URL, $NATS_CONTEXT…) apply.
type Settings struct {
	Binary  string // nats binary (default "nats", found in PATH)
	Context string // --context
	Server  string // -s
	Creds   string // --creds
	Timeout string // --timeout
}

// Args returns the global flags for these settings.
func (s Settings) Args() []string {
	var a []string
	if s.Context != "" {
		a = append(a, "--context", s.Context)
	}
	if s.Server != "" {
		a = append(a, "-s", s.Server)
	}
	if s.Creds != "" {
		a = append(a, "--creds", s.Creds)
	}
	if s.Timeout != "" {
		a = append(a, "--timeout", s.Timeout)
	}
	return a
}

func (s Settings) binary() string {
	if s.Binary == "" {
		return "nats"
	}
	return s.Binary
}

// Result is the outcome of one nats invocation.
type Result struct {
	Args     []string // the nats arguments (without the global flags)
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error // exec failure or non-zero exit
}

// OK reports whether the command ran and exited 0.
func (r Result) OK() bool { return r.Err == nil }

// Output is stdout and stderr together, ANSI escapes removed, trimmed.
func (r Result) Output() string {
	s := strings.TrimSpace(StripANSI(r.Stdout))
	e := strings.TrimSpace(StripANSI(r.Stderr))
	switch {
	case s == "":
		return e
	case e == "":
		return s
	}
	return s + "\n" + e
}

// Message is a one-line summary of an error result: the "nats: error: …"
// line the CLI printed, or the exec error.
func (r Result) Message() string {
	for _, line := range strings.Split(r.Output(), "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "nats: error:") || strings.HasPrefix(l, "error:") || strings.HasPrefix(l, "Error:") {
			return l
		}
	}
	if r.Err != nil {
		if out := r.Output(); out != "" {
			return strings.SplitN(out, "\n", 2)[0]
		}
		return r.Err.Error()
	}
	return ""
}

// Runner runs nats. Exec is the real one; tests substitute a fake.
type Runner interface {
	Run(args ...string) Result
}

// Exec runs the real nats binary with the configured settings.
type Exec struct {
	Settings Settings
	Stdin    string // fed to nats's stdin (empty: /dev/null)
}

// Run executes nats with the global flags followed by args.
func (e Exec) Run(args ...string) Result {
	full := append(e.Settings.Args(), args...)
	cmd := exec.Command(e.Settings.binary(), full...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if e.Stdin != "" {
		cmd.Stdin = strings.NewReader(e.Stdin)
	}
	// nats must never prompt: no terminal, and NO_COLOR keeps its output
	// plain (the tables it draws are still boxes, but without escapes)
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	err := cmd.Run()
	res := Result{Args: args, Stdout: out.String(), Stderr: errb.String(), Err: err}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		res.ExitCode = ee.ExitCode()
	} else if err != nil {
		res.ExitCode = -1
	}
	return res
}

// Available reports whether the nats binary can be found.
func (s Settings) Available() (string, error) {
	return exec.LookPath(s.binary())
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// StripANSI removes terminal escape sequences.
func StripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// ShellQuote quotes s for a POSIX shell when needed.
func ShellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./:@=+,%", r)) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// CommandLine renders "nats args…" as a shell-pasteable line. The global
// flags are included only when set, so what the user sees is what they
// could type.
func (s Settings) CommandLine(args []string) string {
	parts := []string{s.binary()}
	for _, a := range append(s.Args(), args...) {
		parts = append(parts, ShellQuote(a))
	}
	return strings.Join(parts, " ")
}
