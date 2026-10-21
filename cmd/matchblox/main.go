// Command matchblox is a terminal console for a machine that runs many AI
// coding agents: sessions by tmux pane, machine load, git, and what to do.
//
//	matchblox                 open the console (starts the service if needed)
//	matchblox <host>          open the console of a host in ~/.ssh/config
//	matchblox connect <host>  let this console reach a host (once; again to update it)
//	matchblox serve           run the service; --stdio speaks on stdin/stdout
//	matchblox status          print what the service knows as JSON (for agents)
//	matchblox status --text   the same, as a short summary
//	matchblox pair            show a code that pairs the iPhone app
//	matchblox devices         list the phones and laptops that open this host; devices rm <name> removes one
//	matchblox setup           every setup door at once: tmux, the hooks, the way back
//	matchblox version         print the version
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"runtime/pprof"
	"slices"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/blox-eng/matchblox/internal/actions"
	"github.com/blox-eng/matchblox/internal/advice"
	"github.com/blox-eng/matchblox/internal/app"
	"github.com/blox-eng/matchblox/internal/config"
	"github.com/blox-eng/matchblox/internal/gitscan"
	"github.com/blox-eng/matchblox/internal/history"
	"github.com/blox-eng/matchblox/internal/holds"
	"github.com/blox-eng/matchblox/internal/hooks"
	"github.com/blox-eng/matchblox/internal/hosts"
	"github.com/blox-eng/matchblox/internal/limits"
	"github.com/blox-eng/matchblox/internal/panes"
	"github.com/blox-eng/matchblox/internal/procfs"
	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/push"
	"github.com/blox-eng/matchblox/internal/remote"
	"github.com/blox-eng/matchblox/internal/sample"
	"github.com/blox-eng/matchblox/internal/service"
	"github.com/blox-eng/matchblox/internal/setup"
	"github.com/blox-eng/matchblox/internal/ships"
	"github.com/blox-eng/matchblox/internal/state"
	"github.com/blox-eng/matchblox/internal/stoker"
	"github.com/blox-eng/matchblox/internal/transport"
)

// version is stamped into a release build (-ldflags -X main.version=vX),
// which -X can do only to a constant. Any other build names the version Go
// gave it: the module version from go install, the commit in a checkout.
var version = "dev"

func init() { version = buildVersion(version, debug.ReadBuildInfo) }

func buildVersion(stamp string, read func() (*debug.BuildInfo, bool)) string {
	if stamp != "dev" {
		return stamp
	}
	if info, ok := read(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return stamp
}

func main() {
	// MATCHBLOX_CPUPROFILE=file profiles this process, to keep its own cost
	// honest.
	if path := os.Getenv("MATCHBLOX_CPUPROFILE"); path != "" {
		if f, err := os.Create(path); err == nil { //nolint:gosec // the person names their own profile file
			_ = pprof.StartCPUProfile(f)
			defer pprof.StopCPUProfile()
		}
	}
	if err := run(os.Args[1:]); err != nil {
		pprof.StopCPUProfile()
		if errors.Is(err, errShown) {
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "matchblox:", err)
		holdOnError(err, os.Getenv, os.Stdin, os.Stderr)
		os.Exit(1)
	}
}

// holdOnError keeps an error on the screen in the home session: the console
// is its only command, so the pane would close before the person reads it.
func holdOnError(err error, env func(string) string, in io.Reader, out io.Writer) {
	if err == nil || env(panes.HomeEnv) != "1" {
		return
	}
	_, _ = fmt.Fprintln(out, "press Enter to close")
	_, _ = bufio.NewReader(in).ReadString('\n')
}

// configArg is the --config the person gave, passed on to a service the
// console starts.
var configArg string

func run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "gate":
			return gateMain(args[1:])
		case "authorize":
			return authorize(args[1:])
		case "pair":
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
			defer stop()
			return pairCmd(ctx, args[1:], os.Stdout, time.Now)
		case "devices":
			return devicesCmd(args[1:], home(), os.Stdout)
		case "push":
			return pushCmd(args[1:])
		case "connect":
			return liveConnect(args[1:])
		}
	}
	if len(args) > 1 && args[0] == "hook" && args[1] == "statusline" {
		cfg, _ := config.Load(config.Path())
		return statusLine(args[2:], os.Stdin, os.Stdout, os.Getenv, filepath.Dir(state.Path()), cfg, time.Now())
	}
	if len(args) > 1 && args[0] == "hook" && args[1] == "--cursor" {
		return hookCursor(os.Stdin, os.Stdout, transport.SocketPath(), spoolPath())
	}
	if len(args) > 0 && args[0] == "hook" {
		return hook(args[1:], os.Stdin, os.Stdout, transport.SocketPath(), spoolPath(), progressPrompt(args[1:]))
	}
	cmd, host, args, err := parseCommand(args)
	if err != nil {
		return err
	}
	fl, o := newFlags()
	if err := fl.Parse(args); errors.Is(err, flag.ErrHelp) {
		return nil // the help is the answer
	} else if err != nil {
		return err
	}
	cfgPath, interval, root, text, stdio, noMotion := o.cfgPath, o.interval, o.root, o.text, o.stdio, o.noMotion
	switch cmd {
	case "version":
		fmt.Println(version)
		return nil
	case "sessions", "ships", "usage":
		return cliRead(cmd, os.Stdout, transport.SocketPath(), *o.json)
	case "peek":
		words, err := positionals(fl, args)
		if err != nil {
			return err
		}
		return cliPeek(words, *o.lines, os.Stdout, transport.SocketPath(), *o.json)
	case "send", "compact", "hold", "release":
		words, err := positionals(fl, args) // all of them: a -- before the first word holds too
		if err != nil {
			return err
		}
		return cliHand(cmd, words, *o.pick, *o.why, os.Stdout, transport.SocketPath())
	case "kill", "rename":
		words, err := positionals(fl, args)
		if err != nil {
			return err
		}
		return cliLife(cmd, words, *o.anyway, os.Stdout, transport.SocketPath())
	case "new":
		words, err := positionals(fl, args)
		if err != nil {
			return err
		}
		return cliNew(words, newOpts{where: *o.where, agent: *o.agent, branch: *o.branch, name: *o.name}, os.Stdout, transport.SocketPath())
	case "stoker":
		words, err := positionals(fl, args) // all of them: a -- before the first word holds too
		if err != nil {
			return err
		}
		if len(words) > 0 && words[0] == "when" {
			return cliWhen(words, whenOpts{ship: *o.ship, stage: *o.stage, rest: *o.rest, why: *o.why, drop: *o.drop, list: *o.list}, os.Stdout, transport.SocketPath())
		}
		return cliStoker(words, *o.state, *o.needs, *o.rule, os.Stdin, os.Stdout, transport.SocketPath())
	case "setup":
		cfg, _ := config.Load(*cfgPath) // a broken config still opens the doors, at the default line
		return setupDoors(*liveSetup(cfg.Sessions.CompactAt), os.Stdin, os.Stdout, runInTerminal)
	}
	fl.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			configArg = f.Value.String()
		}
	})
	if (cmd == "console" || cmd == "serve") && *root == "" {
		cores, mem := machineSize()
		firstRun(*cfgPath, cores, mem)
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return fmt.Errorf("%s: %w", *cfgPath, err)
	}
	if !cfg.AgentsSet && *root == "" {
		// A config from before the first run found the agents.
		cfg.Agents = setup.FindAgents(agentLookPath(home()))
	}
	if *interval > 0 {
		cfg.Interval.Duration = *interval
	}

	if host != "" && *root != "" {
		return errors.New("a host and --fixtures do not go together")
	}
	if cmd == "console" && *root == "" && host == "" {
		// A phone that connects lands in the console's own tmux session, and
		// every pane has a way back to it. Fixtures (demos, the replay) run
		// in place. Before the nice below: a tmux server started here must
		// not run every agent at nice 19.
		if err := goHome(); err != nil {
			return err
		}
		if os.Getenv(panes.HomeEnv) == "1" {
			_ = exec.Command(panes.SizeArgv()[0], panes.SizeArgv()[1:]...).Run()
		}
	}

	// matchblox must not add load: lowest CPU priority for us and every
	// command we start.
	lowestPriority()

	path := transport.SocketPath()
	switch cmd {
	case "serve":
		if *stdio {
			return serveStdio(path, os.Stdin, os.Stdout)
		}
		return serve(path, newService(cfg, *root))
	case "status":
		return status(os.Stdout, path, cfg, *root, *text)
	}
	return console(path, cfg, *root, *noMotion, host)
}

// commands are the commands a builder types. hook, authorize and gate run
// from a hook, an ssh forced command and a connect.
var commands = []string{"connect", "serve", "sessions", "ships", "status", "setup", "pair", "devices", "usage", "version", "send", "compact", "hold", "release", "stoker", "peek", "new", "rename", "kill"}

type flags struct {
	cfgPath, root         *string
	interval              *time.Duration
	text, stdio, noMotion *bool
	json                  *bool
	pick, lines           *int
	why                   *string
	state, needs, rule    *string
	ship                  *int
	stage, rest, drop     *string
	list                  *bool
	anyway                *bool
	where, agent, branch  *string
	name                  *string
}

// usageText heads --help: every command, one line each, then the flags.
const usageText = `Usage: matchblox [command] [flags]

Commands:
  matchblox                        open the console (starts the service if needed)
  matchblox <host>                 open the console of a host in ~/.ssh/config
  matchblox connect <host>         let this console reach a host
  matchblox serve                  run the service
  matchblox status                 what the service knows, as JSON (--text: a summary)
  matchblox setup                  every setup door at once: tmux, the hooks, the way back
  matchblox pair                   a one-time QR code that pairs the iPhone app
  matchblox devices [rm <name>]    the phones and laptops that open this host
  matchblox version                print the version
  matchblox sessions               every session, one line a row (--json)
  matchblox ships                  the pull requests the sessions drive (--json)
  matchblox usage                  each account's plan windows (--json)
  matchblox peek <who>             what a session said last and what it asks (--lines, --json)
  matchblox send <who> "<text>"    type a line and Enter into a session (--pick n answers its question)
  matchblox compact <who>          compact a Claude Code session at rest
  matchblox hold <who> --why "…"   finish this step, then stop and wait
  matchblox release <who>          let a held session carry on
  matchblox new ["<first message>"]  start an agent in a tmux session of its own (--where, --agent, --branch, --name)
  matchblox rename <who> <name>    name a session, its tmux session and its agent alike
  matchblox kill <who>             end a session: its agent and its pane (--anyway while it works)
  matchblox stoker plan|step       the stoker agent's plan and steps, for its card
  matchblox stoker when …          a trigger matchblox fires itself (--ship n --stage s, or --rest who)
  matchblox stoker quit            end the stoker agent and its own tmux session

<who> is a pane (%3), a target (app:1.1) or a name.

Flags:
`

func newFlags() (*flag.FlagSet, flags) {
	fl := flag.NewFlagSet("matchblox", flag.ContinueOnError)
	fl.Usage = func() {
		_, _ = io.WriteString(fl.Output(), usageText)
		fl.PrintDefaults()
	}
	return fl, flags{
		cfgPath:  fl.String("config", config.Path(), "machine goals (TOML); a missing file means defaults"),
		interval: fl.Duration("interval", 0, "sampling interval (default from config, else 2s)"),
		root:     fl.String("fixtures", "", "read proc/, sys/, home/ and tmux-panes.txt from this directory instead of the live machine"),
		text:     fl.Bool("text", false, "status: print a summary instead of JSON"),
		stdio:    fl.Bool("stdio", false, "serve: speak on stdin and stdout, joined to the local service"),
		json:     fl.Bool("json", false, "sessions, ships, usage, peek: print JSON instead of text"),
		pick:     fl.Int("pick", 0, "send: answer the question the session asks with this option (from 1)"),
		lines:    fl.Int("lines", 20, "peek: the last lines of what the session said (0: all)"),
		why:      fl.String("why", "", "hold, stoker when … hold: why the session is held"),
		state:    fl.String("state", "", "stoker step: doing, done, failed or waits"),
		needs:    fl.String("needs", "", "stoker step: the go a waiting step needs from you (merge:#<n>, release, deploy); none for a step that waits on an event"),
		rule:     fl.String("rule", "", "stoker step 0: the standing rule the step is for"),
		ship:     fl.Int("ship", 0, "stoker when: the pull request whose stage fires the trigger"),
		stage:    fl.String("stage", "", "stoker when --ship: ci, green, merged, live or red (CI failed)"),
		rest:     fl.String("rest", "", "stoker when: the session whose turn's end fires the trigger"),
		drop:     fl.String("drop", "", "stoker when: drop the trigger with this id"),
		list:     fl.Bool("list", false, "stoker when: print the run's triggers"),
		anyway:   fl.Bool("anyway", false, "kill: kill a session that works or waits for you"),
		where:    fl.String("where", "", "new: the directory, or the repository of --branch (default: here)"),
		agent:    fl.String("agent", "claude", "new: the agent to start: claude, codex, opencode or cursor-agent"),
		branch:   fl.String("branch", "", "new: first add a worktree for this new branch, beside the repository"),
		name:     fl.String("name", "", "new: the name of the session, its tmux session and its agent (default: the directory's)"),
		noMotion: fl.Bool("no-motion", os.Getenv("NO_MOTION") == "1", "every motion is an instant change (also NO_MOTION=1)"),
	}
}

// parseCommand splits the command from its flags. A first argument that is
// not a command or a flag is a host: `matchblox ws-1` opens its console.
func parseCommand(args []string) (cmd, host string, rest []string, err error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "console", "", args, nil
	}
	if slices.Contains(commands, args[0]) || args[0] == "authorize" || args[0] == "gate" {
		return args[0], "", args[1:], nil
	}
	if !remote.ValidHost(args[0]) {
		return "", "", nil, fmt.Errorf("%q is not a command or a host; the commands are %s", args[0], strings.Join(commands, ", "))
	}
	return "console", args[0], args[1:], nil
}

// goHome replaces this process with `tmux new-session -A -s matchblox
// matchblox …` outside tmux. It returns only when the console runs in place.
func goHome() error {
	tmux, err := exec.LookPath("tmux")
	self := invokedPath(os.Args[0])
	if self == "" {
		return nil
	}
	argv := panes.HomeArgv(os.Getenv("TMUX") != "", err == nil, append([]string{self}, os.Args[1:]...))
	if argv == nil {
		return nil
	}
	return execve(tmux, argv, os.Environ())
}

// hookTimeout bounds the whole delivery, so `matchblox hook` stops in
// less than 50 ms with the service up, down or stuck.
const hookTimeout = 30 * time.Millisecond

// hookDeny is Claude Code's PreToolUse answer that refuses the tool call,
// with why: one of the stoker's Deny reasons.
func hookDeny(reason string) map[string]any {
	return map[string]any{"hookSpecificOutput": map[string]string{
		"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": reason,
	}}
}

// hook delivers one agent hook event to the service, or to the spool when
// no service answers. It returns nil on every path: a hook that fails must
// not disturb the agent.
func hook(args []string, stdin io.Reader, out io.Writer, sock, spool string, progress bool) error {
	name := ""
	if len(args) > 0 {
		name = args[0]
	}
	ev, err := hooks.Parse(stdin, name, os.Getenv)
	if err != nil {
		return nil
	}
	if ev.Name == "PreToolUse" && ev.Ask == nil {
		// The guard reads the line the shell runs: the scrubbed one is for
		// the spool and the service only.
		ev.Guards = stoker.Guards(ev.Raw)
		if len(ev.Guards) == 0 {
			return nil // a tool call the service has no use for
		}
		// The spool lives in the state dir, beside driven.json.
		if why := stoker.Deny(filepath.Dir(spool), ev.Socket, ev.Pane, ev.Guards); why != "" {
			ev.Denied = true
			_ = json.NewEncoder(out).Encode(hookDeny(why))
		}
	}
	if transport.Notify(sock, hookTimeout, proto.KindHook, ev) != nil {
		_ = hooks.Append(spool, ev)
	}
	if progress && ev.Name == "SessionStart" && ev.Pane != "" { // a session in tmux: what the console watches
		// Claude Code adds what a SessionStart hook prints to the session's
		// context; the console reads the bar back from the transcript.
		_, _ = fmt.Fprintln(out, hooks.ProgressPrompt)
	}
	return nil
}

// hookCursor delivers one event of Cursor's hooks (`matchblox hook
// --cursor`; the event is in the payload), as hook does. It prints nothing:
// Cursor reads what a hook prints, and a stop hook's followup_message would
// start a turn.
func hookCursor(stdin io.Reader, _ io.Writer, sock, spool string) error {
	ev, err := hooks.ParseCursor(stdin, os.Getenv)
	if err != nil {
		return nil
	}
	if transport.Notify(sock, hookTimeout, proto.KindHook, ev) != nil {
		_ = hooks.Append(spool, ev)
	}
	return nil
}

// progressPrompt reads the config only for SessionStart, the one event that
// prints: the other hooks stay as fast as they were.
func progressPrompt(args []string) bool {
	if len(args) == 0 || args[0] != "SessionStart" {
		return false
	}
	cfg, err := config.Load(config.Path())
	return err == nil && cfg.Sessions.ProgressPrompt
}

func spoolPath() string { return filepath.Join(filepath.Dir(state.Path()), "spool.jsonl") }

func console(path string, cfg config.Config, root string, noMotion bool, host string) error {
	base := app.Options{Binary: version, CompactAt: cfg.Sessions.CompactAt, CompactTokens: cfg.Sessions.CompactTokens, NoMotion: noMotion,
		OutsideTmux: os.Getenv("TMUX") == "", Self: invokedPath(os.Args[0]), StateDir: filepath.Dir(state.Path())}
	home, _ := os.UserHomeDir()
	base.Pair = consolePair(home, base.Self, defaultHostKey, setup.PairAddress)
	// The screen changes on a state or a key press, so 15 frames a second
	// is still instant to the eye and wakes the process 4x less than 60.
	if root != "" {
		// Fixtures: a service in this process, for demos and tests.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s := newService(cfg, root)
		a, b := net.Pipe()
		go s.Run(ctx)
		go s.Handle(ctx, transport.NewConn(b)) //nolint:errcheck // ends with the console
		base.Conn = transport.NewConn(a)
		defer base.Conn.Close()
		final, err := tea.NewProgram(app.New(base), tea.WithFPS(15)).Run()
		if m, ok := final.(app.Model); ok {
			m.ClosePair() // however the program ended, its pair code goes with it
		}
		return err
	}
	list := hosts.Path()
	asked := filepath.Join(filepath.Dir(state.Path()), "asked-where")
	sh := app.NewShell(app.ShellOptions{
		Base:     base,
		Hosts:    func() []string { return hosts.Read(list) },
		SSHHosts: func() []string { return hosts.SSHHosts(home) },
		Open:     func(h string) app.Options { return openHost(path, home, h, base.OutsideTmux) },
		Remove:   func(h string) error { return hosts.Remove(list, h) },
		Start:    host,
		FirstRun: !fileExists(asked) && !fileExists(list),
		Answered: func() { _ = os.MkdirAll(filepath.Dir(asked), 0o700); _ = os.WriteFile(asked, nil, 0o600) },
	})
	final, err := tea.NewProgram(sh, tea.WithFPS(15)).Run()
	if s, ok := final.(app.Shell); ok {
		s.Close()
	}
	return err
}

// openHost dials a host for the shell: "" is this machine's service,
// started when needed; another host goes over ssh with the matchblox key.
// A host that cannot be reached yet still opens: the console says why and
// offers the fix.
func openHost(path, home, host string, outsideTmux bool) app.Options {
	if host == "" {
		c, err := ensureService(path)
		return app.Options{Conn: c, DialErr: err, Local: true, OutsideTmux: outsideTmux,
			Redial: func() (transport.Conn, error) { return ensureService(path) },
			Owner:  func() int { return transport.Owner(path) }}
	}
	t := remote.Target{Host: host, Key: remote.KeyPath(home)}
	dial := func() (transport.Conn, error) { return remote.Connect(context.Background(), t) }
	c, err := dial()
	return app.Options{Conn: c, DialErr: err, Redial: dial, Host: host}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// newService builds the service for this machine, or for a fixture tree
// (no git, nothing written).
func newService(cfg config.Config, root string) *service.Service {
	var git service.GitSource
	if root == "" {
		git = gitSource(cfg)
	}
	s := service.New(cfg, newSampler(cfg, root), git)
	s.Binary = version
	if root != "" {
		s.Host = fixtureHost(root)
		// The fixture's panes and pids exist on this machine too.
		s.Actions.Run = func([]string) error { return errors.New("fixtures: nothing runs on this machine") }
		return s
	}
	s.Actions.Run = func(argv []string) error { return actions.Exec(context.Background(), argv) }
	s.StokerServer = sample.StokerServer
	s.StatePath = state.Path()
	s.Spool = spoolPath()
	s.AlertHook = cfg.Hooks.Alert
	s.HistorySources = []string{cfg.History.Source}
	switch cfg.History.Log {
	case "":
		s.HistoryLog = history.DefaultLog()
	case "-":
	default:
		s.HistoryLog = cfg.History.Log
	}
	s.Exe = invokedPath(os.Args[0])
	// A phone paired before ids gets one, so it can register for pushes.
	// stderr is serve.log when the console starts the service.
	if n, err := setup.UpgradePhoneLines(home()); err != nil {
		fmt.Fprintln(os.Stderr, "matchblox: phone lines:", err)
	} else if n > 0 {
		fmt.Fprintf(os.Stderr, "matchblox: upgraded %d phone lines\n", n)
	}
	s.Setup = liveSetup(cfg.Sessions.CompactAt)
	s.Ways = config.WaysPath()
	s.LimitsLog = &limits.Store{Path: filepath.Join(filepath.Dir(s.StatePath), "limits.jsonl")}
	if st, err := stoker.Open(filepath.Dir(s.StatePath)); err == nil {
		s.Stoker = st
	} else {
		fmt.Fprintln(os.Stderr, "matchblox: the stoker is off:", err)
	}
	if h, err := holds.Open(filepath.Dir(s.StatePath)); err == nil {
		s.Holds = h
	} else {
		fmt.Fprintln(os.Stderr, "matchblox: parking is off:", err)
	}
	s.Ships = shipsSource(cfg, filepath.Dir(s.StatePath))
	if n, err := push.OpenNumbers(filepath.Dir(s.StatePath)); err == nil {
		s.PushNumbers = n
		if p, err := pushSender(cfg.Push, home(), defaultHostKey, n); err != nil {
			fmt.Fprintln(os.Stderr, "matchblox: push is off:", err)
		} else {
			s.Push = p
		}
	} else {
		fmt.Fprintln(os.Stderr, "matchblox: push is off: numbers:", err)
	}
	return s
}

// fixtureHost is the host name a fixture tree carries, so a demo or a test
// never shows the name of the machine it runs on.
func fixtureHost(root string) string {
	b, err := os.ReadFile(filepath.Join(root, "proc", "sys", "kernel", "hostname"))
	if h := strings.TrimSpace(string(b)); err == nil && h != "" {
		return h
	}
	return "fixtures"
}

// invokedPath is the path matchblox was started by, links kept: Homebrew
// and nix upgrade by repointing a link, and the resolved file never changes.
func invokedPath(arg0 string) string {
	p := arg0
	if !strings.ContainsRune(arg0, filepath.Separator) {
		found, err := exec.LookPath(arg0)
		if err != nil {
			return ""
		}
		p = found
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return ""
	}
	return abs
}

// serve runs the service on the socket until a signal, or until the binary
// on disk is replaced (the next console starts the new one).
func serve(path string, s *service.Service) error {
	if s.Push != nil {
		defer s.Push.Close()
	}
	l, err := transport.Listen(path)
	if errors.Is(err, transport.ErrInUse) {
		fmt.Fprintf(os.Stderr, "matchblox: the service already runs on %s (pid %d)\n", path, transport.Owner(path))
		return nil
	}
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = s.Serve(ctx, l)
	if errors.Is(err, service.ErrReplaced) || errors.Is(err, service.ErrSocketGone) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// serveStdio joins stdin and stdout to the local service, starting it if
// needed. Remote mode runs it over SSH.
func serveStdio(path string, in io.Reader, out io.Writer) error {
	c, err := ensureService(path)
	if err != nil {
		return err
	}
	transport.Pipe(transport.Stdio(in, out), c)
	return nil
}

// ensureService connects to the local service, or starts it detached and
// connects within 2 s.
func ensureService(path string) (transport.Conn, error) {
	if c, err := transport.Dial(path, 200*time.Millisecond); err == nil {
		return c, nil
	}
	if err := spawnService(); err != nil {
		return nil, fmt.Errorf("start the service: %w", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := transport.Dial(path, 200*time.Millisecond); err == nil {
			return c, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, fmt.Errorf("the service did not start on %s; run `matchblox serve` to see why", path)
}

// spawnService starts `matchblox serve` in its own session, so it keeps
// running when the console and its terminal close.
var spawnService = func() error {
	c, closeLog, err := serviceCmd()
	if err != nil {
		return err
	}
	defer closeLog()
	if err := c.Start(); err != nil {
		return err
	}
	return c.Process.Release()
}

// serviceCmd is the detached `matchblox serve`. It runs from / so that it
// never holds a worktree open, and writes to serve.log next to the state
// file.
func serviceCmd() (*exec.Cmd, func(), error) {
	// Start it by the path we were started by, so that it watches the link
	// an upgrade repoints.
	exe := invokedPath(os.Args[0])
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			return nil, nil, err
		}
	}
	args := []string{"serve"}
	if configArg != "" {
		args = append(args, "--config", configArg)
	}
	c := exec.Command(exe, args...) //nolint:gosec // our own binary
	c.Dir = "/"
	// The service outlives this console: it watches the default tmux server,
	// not the one this console happens to run in.
	c.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_PANE=")
	})
	closeLog := func() {}
	logPath := filepath.Join(filepath.Dir(state.Path()), "serve.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err == nil {
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil { //nolint:gosec // our own state dir
			c.Stdout, c.Stderr = f, f
			// The child holds its own descriptor; a failed close of ours
			// loses nothing it writes, but say so.
			closeLog = func() {
				if err := f.Close(); err != nil {
					fmt.Fprintln(os.Stderr, "matchblox: serve.log:", err)
				}
			}
		}
	}
	detach(c)
	return c, closeLog, nil
}

// staleAfter is the age at which status treats the state file as left by
// a service that no longer runs.
const staleAfter = 15 * time.Second

func gitSource(cfg config.Config) service.GitSource {
	scanner := &gitscan.Scanner{Git: gitscan.Git, Merged: gitscan.GHMerged, Open: gitscan.GHOpen, RecheckIdle: 2 * time.Hour}
	return func(sessionCwds []string) gitscan.Report {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		in := gitscan.Input{
			SessionCwds: sessionCwds,
			ProcessCwds: actions.ProcessCwds(),
			Extra:       cfg.Git.Repos,
			Main:        cfg.Git.Main,
			Remote:      cfg.Git.Remote,
		}
		return scanner.Scan(ctx, in)
	}
}
func shipsSource(cfg config.Config, dir string) service.ShipsSource {
	t := &ships.Tracker{Dir: dir, GH: ships.GH, Git: gitscan.Git, Remote: cfg.Git.Remote, Deploy: cfg.Ships.Deploy}
	return func(ctx context.Context, sessions []sample.Session, git *gitscan.Report) ships.Report {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		return t.Scan(ctx, ships.Input{Sessions: sessions, Git: git})
	}
}

func newSampler(cfg config.Config, root string) *sample.Sampler {
	smp := &sample.Sampler{
		Cfg:    cfg,
		Agents: cfg.Agents,
		Rules: sample.Rules{
			CompactAt:     cfg.Sessions.CompactAt,
			CompactTokens: cfg.Sessions.CompactTokens,
			ClearIdle:     cfg.Sessions.ClearIdle.Duration,
			ClearMinPct:   cfg.Sessions.ClearMinPct,
			ClearStale:    cfg.Sessions.ClearStale.Duration,
			Windows:       cfg.Sessions.Windows,
		},
	}
	if root != "" {
		smp.FS = procfs.FS{Root: filepath.Join(root, "proc"), PageSize: procfs.FixturePage}
		smp.Sys = procfs.Sys{Root: filepath.Join(root, "sys")}
		smp.Home = filepath.Join(root, "home")
		smp.HomeAs = "/home/user" // the HOME of every fixture process
		smp.Tmux = func() ([]byte, error) { return os.ReadFile(filepath.Join(root, "tmux-panes.txt")) }
		smp.Capture = sample.CaptureDir(filepath.Join(root, "panes"))
		smp.LimitsDir = filepath.Join(root, "state")
		smp.Disks = sample.DiskFile(filepath.Join(root, "disks.txt"))
		return smp
	}
	smp.FS, smp.Sys = actions.NewHost(), procfs.Sys{Root: "/sys"}
	smp.Stoker = sample.StokerServer
	smp.Home, _ = os.UserHomeDir()
	smp.GPU, smp.Docker = sample.NvidiaSMI, sample.DockerPS
	smp.Disks = sample.Disks(smp.Home, "/")
	smp.LatencyTarget = cfg.LatencyTarget
	smp.ProcEvery = 5 * cfg.Interval.Duration
	smp.OwnEntries = true
	smp.LimitsDir = filepath.Dir(state.Path())
	return smp
}

// stokerPaneGone warns of a config that still names the stoker's pane.
const stokerPaneGone = "config: [stoker] pane is ignored: matchblox starts the stoker agent itself, on its own tmux server ([stoker] agent, default claude). Remove the line."

// status prints the service's state. Without a service it uses a fresh
// state file, and else samples on its own: two samples a second apart so
// rates are real, and one git scan.
func status(w io.Writer, path string, cfg config.Config, root string, text bool) error {
	if cfg.Stoker.PaneSet {
		warn := io.Writer(os.Stderr) // JSON on stdout stays JSON
		if text {
			warn = w
		}
		_, _ = fmt.Fprintln(warn, stokerPaneGone)
	}
	doc, ok := state.Doc{}, false
	maxAge := max(staleAfter, 3*cfg.Interval.Duration)
	if root == "" {
		doc, ok = fromService(path, maxAge)
	}
	if !ok && root == "" {
		if d, err := state.Read(state.Path()); err == nil && time.Since(d.At) <= maxAge {
			doc, ok = d, true
		}
	}
	if !ok {
		smp := newSampler(cfg, root)
		smp.ProcEvery = 0
		smp.Sample()
		time.Sleep(time.Second)
		doc = state.Doc{Snapshot: smp.Sample()}
		if root == "" {
			var cwds []string
			for _, s := range doc.Sessions {
				cwds = append(cwds, s.Cwd)
			}
			r := gitSource(cfg)(cwds)
			doc.Git = &r
		}
		doc.Recommendations = advice.Build(doc.Snapshot, doc.Git)
	}
	if text {
		_, err := fmt.Fprint(w, app.Summary(doc))
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

// fromService reads the first snapshot from a running service. A snapshot
// older than maxAge means its sample loop is stuck: not current.
func fromService(path string, maxAge time.Duration) (state.Doc, bool) {
	c, err := transport.Dial(path, 200*time.Millisecond)
	if err != nil {
		return state.Doc{}, false
	}
	defer c.Close()
	timer := time.AfterFunc(3*time.Second, func() { c.Close() })
	defer timer.Stop()
	if c.Send(proto.KindHello, "", proto.Hello{Version: proto.Version, Binary: version}) != nil {
		return state.Doc{}, false
	}
	for {
		env, err := c.Recv()
		if err != nil {
			return state.Doc{}, false
		}
		switch env.Kind {
		case proto.KindHello:
			var h proto.Hello
			if json.Unmarshal(env.Body, &h) != nil || h.Version != proto.Version {
				return state.Doc{}, false
			}
		case proto.KindSnapshot:
			var st proto.State
			if json.Unmarshal(env.Body, &st) != nil || time.Since(st.At) > maxAge {
				return state.Doc{}, false
			}
			return st.Doc, true
		}
	}
}
