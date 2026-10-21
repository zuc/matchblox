package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/blox-eng/matchblox/internal/actions"
	"github.com/blox-eng/matchblox/internal/doors"
	"github.com/blox-eng/matchblox/internal/setup"
	"github.com/blox-eng/matchblox/internal/state"
)

// liveSetup is this machine, for the doors; compactAt is the stoker's line.
func liveSetup(compactAt float64) *setup.Env {
	home, _ := os.UserHomeDir()
	release, _ := os.ReadFile("/etc/os-release")
	return &setup.Env{
		GOOS: runtime.GOOS, OSRelease: string(release), Home: home,
		Exe:       invokedPath(os.Args[0]),
		StateDir:  filepath.Dir(state.Path()),
		Root:      os.Geteuid() == 0,
		UID:       os.Getuid(),
		CompactAt: compactAt,
		Getenv:    os.Getenv, LookPath: exec.LookPath, Source: tmuxSource,
	}
}

// tmuxSource loads a config into the default tmux server, the one the
// service watches.
func tmuxSource(conf string) error {
	c := exec.Command("tmux", "source-file", conf) //nolint:gosec // our own config path
	c.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "TMUX=") })
	var stderr bytes.Buffer
	c.Stderr = &stderr
	err := c.Run()
	switch msg := stderr.String(); {
	case err == nil:
		return nil
	case errors.Is(err, exec.ErrNotFound), strings.Contains(msg, "no server running"), strings.Contains(msg, "error connecting"):
		return setup.ErrNoServer
	case msg != "":
		return errors.New(strings.TrimSpace(msg))
	}
	return err
}

// firstRun writes the config with this machine's thresholds when there is
// none; a config that cannot be written still leaves the defaults.
func firstRun(path string, cores int, memTotal uint64) {
	if wrote, err := setup.WriteConfig(path, cores, memTotal, setup.FindAgents(agentLookPath(home()))); err != nil {
		fmt.Fprintln(os.Stderr, "matchblox: write", path+":", err)
	} else if wrote {
		fmt.Fprintln(os.Stderr, "matchblox: wrote", path, "with the goals of this machine")
	}
}

// agentDirs are where agents install themselves outside a usual PATH: the
// service can be the first to run, with the small PATH of its manager.
var agentDirs = []string{"~/.local/bin", "~/.claude/local", "~/.opencode/bin", "~/.bun/bin", "~/.npm-global/bin",
	"/home/linuxbrew/.linuxbrew/bin", "/opt/homebrew/bin", "/usr/local/bin"}

// agentLookPath finds a command on PATH, then in agentDirs.
func agentLookPath(home string) func(string) (string, error) {
	return func(name string) (string, error) {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
		for _, d := range agentDirs {
			if rest, ok := strings.CutPrefix(d, "~/"); ok {
				d = filepath.Join(home, rest)
			}
			p := filepath.Join(d, name)
			if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() && st.Mode()&0o111 != 0 {
				return p, nil
			}
		}
		return "", exec.ErrNotFound
	}
}

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

func machineSize() (int, uint64) {
	mem, _ := actions.NewHost().Meminfo()
	return runtime.NumCPU(), mem.Total
}

// setupDoors is `matchblox setup`: every door at once, done ones too. For
// each open door it shows the diff or the command, and opens it on y.
func setupDoors(e setup.Env, in io.Reader, out io.Writer, run func(argv []string) error) error {
	var werr error // the first failed write to out: the person sees nothing more
	say := func(format string, a ...any) {
		if werr == nil {
			_, werr = fmt.Fprintf(out, format, a...)
		}
	}
	answers := bufio.NewReader(in)
	for _, d := range setup.Doors(e) {
		if d.Done {
			say("✓ %s\n", d.Title)
			continue
		}
		say("\n%s\n  %s\n", d.Title, d.Why)
		if d.Problem != "" {
			say("  %s\n", d.Problem)
			continue
		}
		if d.ID == doors.Night {
			say("  press n in the console to start one\n")
			continue
		}
		if d.Path != "" {
			say("\n  %s\n", d.Path)
		}
		for _, l := range strings.Split(d.Preview, "\n") {
			say("  %s\n", l)
		}
		say("\ny opens it, Enter skips: ")
		line, err := answers.ReadString('\n')
		if strings.TrimSpace(line) != "y" {
			say("skipped\n")
			if err != nil {
				break // no more answers: the rest stay as they are
			}
			continue
		}
		if d.RunsTerm() {
			if err := run(d.Term); err != nil {
				say("failed: %v\n", err)
			}
			continue
		}
		also := false
		if d.Also != nil {
			say("\n  %s\n", d.Also.Label)
			for _, l := range alsoLines(d) {
				say("  %s\n", l)
			}
			say("\ny adds it, Enter leaves it off: ")
			line, _ := answers.ReadString('\n')
			also = strings.TrimSpace(line) == "y"
		}
		switch backup, err := setup.Open(e, d.ID, d.Sum, also); {
		case err != nil:
			say("not written: %v\n", err)
		case backup != "":
			say("wrote %s; backup %s\n", d.Path, backup)
		default:
			say("wrote %s\n", d.Path)
		}
	}
	return werr
}

// alsoLines are the lines a door's choice adds to its diff.
func alsoLines(d doors.Door) []string {
	plain := strings.Split(d.Preview, "\n")
	var out []string
	for _, l := range strings.Split(d.Also.Preview, "\n") {
		if strings.HasPrefix(l, "+") && !slices.Contains(plain, l) {
			out = append(out, l)
		}
	}
	return out
}

// runInTerminal gives a door's command this terminal.
func runInTerminal(argv []string) error {
	c := exec.Command(argv[0], argv[1:]...) //nolint:gosec // a door's command, from setup.Doors
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}
