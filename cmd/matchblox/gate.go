package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/remote"
	"github.com/blox-eng/matchblox/internal/service"
	"github.com/blox-eng/matchblox/internal/setup"
	"github.com/blox-eng/matchblox/internal/transport"
)

// gateMain is the forced command of the matchblox key on a host
// (design/0005-remote-mode.md §3.3). A laptop key (`gate`) starts the service
// stream; a phone key (`gate phone` or `gate phone <id>`) also starts the
// console. A phone key with an id leaves a note that the device is in, which
// the console reads. Anything else exits 126.
func gateMain(args []string) error {
	if len(args) == 2 && args[0] == "pair" {
		return gatePair(args[1])
	}
	mode, id := "", ""
	switch {
	case len(args) == 0:
	case len(args) == 1 && args[0] == phoneMode:
		mode = phoneMode
	case len(args) == 2 && args[0] == phoneMode && setup.ValidPairID(args[1]):
		mode, id = phoneMode, args[1]
	default:
		fmt.Fprintln(os.Stderr, "matchblox gate: refused: this key line has an unknown gate mode; run matchblox connect or matchblox pair again")
		os.Exit(126)
	}
	orig := os.Getenv("SSH_ORIGINAL_COMMAND")
	argv, err := gateDecision(mode, id, orig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "matchblox gate: refused %q: %v\n", orig, err)
		os.Exit(126)
	}
	n, open := openTarget(orig)
	if id != "" && (orig == remote.ConsoleCommand || open) {
		// Only the console marks the device in; a note that fails never blocks it.
		if home, err := os.UserHomeDir(); err == nil {
			_ = setup.MarkDeviceIn(home, id, os.Getenv("SSH_TTY"), time.Now())
		}
	}
	// ssh's own PATH (a non-login shell, or sshd's default on macOS) may
	// not have tmux: the service it starts lists the panes with it.
	_ = os.Setenv("PATH", os.ExpandEnv(remote.HostPath)+string(os.PathListSeparator)+os.Getenv("PATH"))
	_ = os.Unsetenv("SSH_ORIGINAL_COMMAND")
	// The console re-runs itself in the home session with these arguments:
	// without "gate" it is the builder's own `matchblox`.
	os.Args = os.Args[:1]
	if open {
		// The console's home session ignores arguments: the service tells
		// it which session to open. No service: the console just opens.
		_ = gateFocus(n)
	}
	return gateRun(argv)
}

// gateRun runs the console or the service; tests replace it.
var gateRun func([]string) error

func init() { gateRun = run }

const phoneMode = "phone"

// gateDecision maps the requested command to what runs. The requested
// command is never a tmux command: only a phone key reaches tmux, through
// the console and goHome's fixed argv. A laptop key (mode "") gets the
// service stream alone; the phone mode also gets the console, which attaches
// the shared home session, where goHome already decides tmux or in place. An
// unknown mode gets nothing. A phone also registers for pushes, by the id of
// its key line (id), never by anything in the request; a phone line from
// before ids is refused until the service gives it one. A phone that opens
// one session (`matchblox console --open <n>`, from the island or a buzz)
// gets the console too; a laptop key never does.
func gateDecision(mode, id, orig string) (argv []string, err error) {
	if mode != "" && mode != phoneMode {
		return nil, errors.New("this key line has an unknown gate mode; run matchblox connect or matchblox pair again")
	}
	switch {
	case orig == remote.PushRegisterCommand && mode == phoneMode && setup.ValidPairID(id):
		return []string{"push", "register", id}, nil
	case orig == remote.PushRegisterCommand && mode == phoneMode:
		return nil, errors.New("this phone's key line is old; open matchblox serve once, then try again")
	case orig == remote.ServeCommand:
		return []string{"serve", "--stdio"}, nil
	case orig == remote.ConsoleCommand && mode == phoneMode:
		return []string{}, nil
	case mode == phoneMode && isOpen(orig):
		return []string{}, nil // the console; gateMain tells the service the session
	case orig == remote.ConsoleCommand:
		return nil, errors.New("this key starts only the matchblox stream; run matchblox pair to add a phone")
	case mode == phoneMode:
		return nil, errors.New("this key starts only the matchblox stream and console; use your own ssh login for a shell")
	}
	return nil, errors.New("this key starts only the matchblox stream; use your own ssh login for a shell")
}

// gatePair runs the forced command of a one-time key. The mode comes from the
// key line alone; SSH_ORIGINAL_COMMAND is not read.
func gatePair(id string) error {
	if !setup.ValidPairID(id) {
		fmt.Fprintln(os.Stderr, "matchblox gate: refused: this key line has a bad pairing id; run matchblox pair again")
		os.Exit(126)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	exe := invokedPath(os.Args[0])
	if exe == "" {
		return errors.New("cannot find this matchblox on the PATH")
	}
	return pairMain(home, exe, os.Stdin, os.Stdout, id)
}

// openCommand is what a phone asks the gate to run when a tap on the island
// or a buzz opens a session: the console, with the session's island number.
const openCommand = remote.ConsoleCommand + " --open "

// openTarget reads the number of `matchblox console --open <n>`: n a
// positive decimal without a leading zero, and nothing after it.
func openTarget(orig string) (n int, ok bool) {
	v, ok := strings.CutPrefix(orig, openCommand)
	if !ok || v == "" || len(v) > 9 || v[0] < '1' || v[0] > '9' {
		return 0, false
	}
	for _, c := range v {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(v)
	return n, err == nil
}

func isOpen(orig string) bool { _, ok := openTarget(orig); return ok }

// gateFocus asks the running service to focus the session of island number
// n; tests replace it.
var gateFocus = func(n int) error {
	res, err := doAct(transport.SocketPath(), proto.Act{RecID: service.FocusAct(n)})
	if err == nil && res.Err != "" {
		err = errors.New(res.Err)
	}
	return err
}
