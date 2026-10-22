package main

import (
	"flag"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/blox-eng/matchblox/internal/docscheck"
	"github.com/blox-eng/matchblox/internal/remote"
)

// internalCommands run from a hook, an ssh forced command or a connect;
// the docs may name them and need not.
var internalCommands = []string{"hook", "authorize", "gate"}

// docHosts are the hosts public text may name (DESIGN.md §7): the demo
// host, or a placeholder. Any other word is a typo of a command.
var docHosts = []string{"ws-1", "<host>"}

// commandFlags are the flags each command reads; a flag the binary parses
// and the command ignores is a doc that misleads.
var commandFlags = map[string][]string{
	"console":  {"config", "interval", "fixtures", "no-motion"},
	"<host>":   {"config", "interval", "no-motion"},
	"devices":  {},
	"pair":     {"address", "port", "name"},
	"serve":    {"config", "interval", "fixtures", "stdio"},
	"status":   {"config", "interval", "fixtures", "text"},
	"sessions": {"json"},
	"ships":    {"json"},
	"usage":    {"json"},
	"send":     {"pick"},
	"compact":  {},
	"hold":     {"why"},
	"release":  {},
	"stoker":   {"state", "needs", "rule", "ship", "stage", "rest", "why", "drop", "list"},
	"peek":     {"lines", "json"},
	"new":      {"where", "agent", "branch", "name"},
	"rename":   {},
	"kill":     {"anyway"},
	"switch":   {"agent", "handoff", "anyway"},
}

// checkCommand returns the command a docs line shows ("console", "<host>"
// or a command name) and what is wrong with it.
func checkCommand(line string) (cmd string, problems []string) {
	args := strings.Fields(line)[1:]
	cmd = "console"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch {
		case slices.Contains(commands, args[0]) || slices.Contains(internalCommands, args[0]):
			cmd = args[0]
		case slices.Contains(docHosts, args[0]) && (args[0] == "<host>" || remote.ValidHost(args[0])):
			cmd = "<host>"
		default:
			return "", []string{fmt.Sprintf("%q is not a matchblox command", args[0])}
		}
		args = args[1:]
	}
	if slices.Contains(internalCommands, cmd) {
		return cmd, nil
	}
	fl, _ := newFlags()
	if cmd == "pair" { // pair parses its own flags
		fl = flag.NewFlagSet("pair", flag.ContinueOnError)
		for _, n := range commandFlags["pair"] {
			fl.String(n, "", "")
		}
	}
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			continue
		}
		name, _, _ := strings.Cut(strings.TrimLeft(a, "-"), "=")
		switch {
		case fl.Lookup(name) == nil:
			problems = append(problems, "matchblox has no flag "+a)
		case !slices.Contains(commandFlags[cmd], name):
			problems = append(problems, fmt.Sprintf("matchblox %s does not read %s", cmd, a))
		}
	}
	return cmd, problems
}

func TestCheckCommandRefusesAFlagItsCommandIgnores(t *testing.T) {
	for line, bad := range map[string]bool{
		"matchblox setup --stdio":          true,
		"matchblox connect ws-1 --text":    true,
		"matchblox status -bogus":          true,
		"matchblox stat_us":                true,
		"matchblox status --text":          false,
		"matchblox --config <file>":        false,
		"matchblox ws-1":                   false,
		"matchblox serve -stdio":           false,
		"matchblox hook PreToolUse --any":  false,
		"matchblox pair --name x --port 2": false,
		"matchblox pair --text":            true,
		"matchblox devices --name x":       true,
		"matchblox devices rm iPad":        false,
	} {
		if _, p := checkCommand(line); (len(p) > 0) != bad {
			t.Errorf("%q: problems %v, want bad=%v", line, p, bad)
		}
	}
}

// TestTheDocsShowOnlyRealCommands holds every `matchblox …` line of the
// README, CONTRIBUTING and the docs to the binary: the command is one it
// has (or the demo host), each flag is one that command reads, and every
// command a builder types is shown somewhere.
func TestTheDocsShowOnlyRealCommands(t *testing.T) {
	files, err := docscheck.Files("../..")
	if err != nil {
		t.Fatal(err)
	}
	refs, err := docscheck.Commands(files)
	if err != nil {
		t.Fatal(err)
	}
	shown := map[string]bool{}
	for _, r := range refs {
		cmd, problems := checkCommand(r.Text)
		for _, p := range problems {
			t.Errorf("%s:%d: %q: %s", r.File, r.Line, r.Text, p)
		}
		shown[cmd] = true
	}
	for _, c := range append([]string{"console", "<host>"}, commands...) {
		if !shown[c] {
			t.Errorf("the docs never show `matchblox %s`", strings.TrimPrefix(c, "console"))
		}
	}
}
