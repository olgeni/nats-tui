package cli

import "strings"

// Command is one nats invocation of a plan.
type Command struct {
	Args   []string // arguments after the global flags
	Desc   string   // what it does, for the preview
	Danger bool     // destructive or irreversible: the preview highlights it
	Stdin  string   // fed to the command (a message body, a value)
}

// Plan is the list of nats commands an action needs, shown to the user
// before anything runs. Commands run in order; the first failure stops the
// plan (later commands usually depend on the earlier ones).
type Plan struct {
	Title string
	Cmds  []Command
	Notes []string // remarks shown under the commands
}

// Add appends a command.
func (p *Plan) Add(desc string, args ...string) *Plan {
	p.Cmds = append(p.Cmds, Command{Args: args, Desc: desc})
	return p
}

// AddDanger appends a command flagged as dangerous.
func (p *Plan) AddDanger(desc string, args ...string) *Plan {
	p.Cmds = append(p.Cmds, Command{Args: args, Desc: desc, Danger: true})
	return p
}

// AddStdin appends a command that reads its input from stdin (the preview
// shows the text piped in).
func (p *Plan) AddStdin(desc, stdin string, args ...string) *Plan {
	p.Cmds = append(p.Cmds, Command{Args: args, Desc: desc, Stdin: stdin})
	return p
}

// Note appends a remark.
func (p *Plan) Note(n string) *Plan {
	p.Notes = append(p.Notes, n)
	return p
}

// Empty reports whether there is nothing to run.
func (p *Plan) Empty() bool { return p == nil || len(p.Cmds) == 0 }

// Dangerous reports whether any command is flagged.
func (p *Plan) Dangerous() bool {
	for _, c := range p.Cmds {
		if c.Danger {
			return true
		}
	}
	return false
}

// Execute runs the plan; progress (optional) is called after each command.
// It returns every result produced; the last one failed when the plan did.
func (p *Plan) Execute(r Runner, progress func(done, total int, res Result)) []Result {
	var out []Result
	for i, c := range p.Cmds {
		var res Result
		if c.Stdin != "" {
			if e, ok := r.(Exec); ok {
				e.Stdin = c.Stdin
				res = e.Run(c.Args...)
			} else if sr, ok := r.(StdinRunner); ok {
				res = sr.RunStdin(c.Stdin, c.Args...)
			} else {
				res = r.Run(c.Args...)
			}
		} else {
			res = r.Run(c.Args...)
		}
		out = append(out, res)
		if progress != nil {
			progress(i+1, len(p.Cmds), res)
		}
		if !res.OK() {
			break
		}
	}
	return out
}

// StdinRunner is a Runner that can feed a command's stdin (fakes in tests).
type StdinRunner interface {
	RunStdin(stdin string, args ...string) Result
}

// Failed reports whether an executed plan stopped on an error.
func Failed(results []Result) bool {
	return len(results) > 0 && !results[len(results)-1].OK()
}

// flagSet builds an argument list from name/value pairs, skipping empty
// values.
type flagSet struct {
	args []string
}

func (f *flagSet) str(name, v string) {
	if v != "" {
		f.args = append(f.args, name+"="+v)
	}
}

// strAlways passes a value even when empty (to clear a setting).
func (f *flagSet) strAlways(name, v string) {
	f.args = append(f.args, name+"="+v)
}

// list passes one comma-joined flag (the CLI splits it).
func (f *flagSet) list(name string, v []string) {
	if len(v) > 0 {
		f.args = append(f.args, name+"="+strings.Join(v, ","))
	}
}

// each passes the flag once per value.
func (f *flagSet) each(name string, v []string) {
	for _, x := range v {
		f.args = append(f.args, name+"="+x)
	}
}

func (f *flagSet) bool(name string, on bool) {
	if on {
		f.args = append(f.args, name)
	}
}

// noBool passes --name or --no-name (for the CLI's --[no-]name flags).
func (f *flagSet) noBool(name string, on bool) {
	if on {
		f.args = append(f.args, name)
	} else {
		f.args = append(f.args, strings.Replace(name, "--", "--no-", 1))
	}
}

// sameList reports whether two lists hold the same items in order.
func sameList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
