// Package doors is the setup a builder walks through in the console: one
// row for each step, each with one action. The service finds the doors of
// its machine; the console draws them and runs only the commands this
// package allows.
package doors

import (
	"slices"
	"strings"
)

// The doors, in the order the console shows them.
const (
	Tmux      = "tmux"
	Hooks     = "hooks"
	Limits    = "limits"
	SafetyNet = "safetynet"
	WayBack   = "wayback"
	Guide     = "guide"
	Night     = "night"
	Ways      = "ways"
	// KeepRunning writes a user unit, then runs Term: the service writes
	// the file and runs the command, so the terminal never runs it.
	KeepRunning = "keeprunning"
)

type Door struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Why   string `json:"why"`
	// Preview is the exact diff or command, shown before anything changes.
	Preview string `json:"preview,omitempty"`
	// Path is the file the door changes.
	Path string `json:"path,omitempty"`
	// Sum names the file the preview was made from. The act sends it back,
	// and the service refuses when the file changed after the preview.
	Sum string `json:"sum,omitempty"`
	// Term runs in the builder's terminal (a password prompt, an agent).
	Term []string `json:"term,omitempty"`
	// Also is a choice the preview offers, off until the person turns it on.
	Also *Also `json:"also,omitempty"`
	// Problem is why the door cannot open now, and the fix.
	Problem string `json:"problem,omitempty"`
	Done    bool   `json:"done,omitempty"`
	// Closed: the builder closed it; `matchblox setup` shows it again.
	Closed bool `json:"closed,omitempty"`
}

// Also is one more change a door can make on request: its label and the
// whole diff with it. It opens with the same Sum.
type Also struct {
	Label   string `json:"label"`
	Preview string `json:"preview"`
}

// RunsTerm: the builder's terminal runs the door's Term. KeepRunning shows
// its command but runs it itself, after it writes the unit.
func (d Door) RunsTerm() bool { return d.Term != nil && d.ID != KeepRunning }

// Open is a door the console shows: not done and not closed.
func (d Door) Open() bool { return !d.Done && !d.Closed }

// managers maps an os-release ID to the command that installs tmux.
// A fresh Debian image has no package lists, so apt updates them first.
var managers = []struct {
	ids  []string
	bin  string
	argv []string
}{
	{[]string{"debian", "ubuntu"}, "apt-get", []string{"sh", "-c", "apt-get update && apt-get install -y tmux"}},
	{[]string{"fedora", "rhel", "centos"}, "dnf", []string{"dnf", "install", "-y", "tmux"}},
	{[]string{"arch"}, "pacman", []string{"pacman", "-S", "--noconfirm", "tmux"}},
	{[]string{"alpine"}, "apk", []string{"apk", "add", "tmux"}},
}

var brew = []string{"brew", "install", "tmux"}

// TmuxInstall is the command that installs tmux on this OS, or nil when no
// known package manager is there. Linux names its family in os-release
// (ID, then ID_LIKE). Without sudo (a root shell) the command runs as is.
func TmuxInstall(goos, osRelease string, look func(string) (string, error)) []string {
	found := func(name string) bool { _, err := look(name); return err == nil }
	switch goos {
	case "darwin":
		if found("brew") {
			return slices.Clone(brew)
		}
		return nil
	case "linux":
	default:
		return nil
	}
	for _, id := range releaseIDs(osRelease) {
		for _, m := range managers {
			if !slices.Contains(m.ids, id) || !found(m.bin) {
				continue
			}
			if found("sudo") {
				return append([]string{"sudo"}, m.argv...)
			}
			return slices.Clone(m.argv)
		}
	}
	return nil
}

// releaseIDs is ID, then each ID_LIKE entry, of an os-release file.
func releaseIDs(osRelease string) []string {
	var id, like []string
	for line := range strings.SplitSeq(osRelease, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"'`)
		switch k {
		case "ID":
			id = []string{v}
		case "ID_LIKE":
			like = strings.Fields(v)
		}
	}
	return append(id, like...)
}

// GuidePrompt is the first prompt of the guide session.
const GuidePrompt = "Help me set up matchblox. Read https://docs.matchblox.com/start and walk me through it."

// GuideArgv starts the guide: in its own tmux window, or, outside tmux, in
// this terminal until the agent exits.
func GuideArgv(inTmux bool) []string {
	if inTmux {
		return []string{"tmux", "new-window", "-n", "guide", "claude", GuidePrompt}
	}
	return []string{"claude", GuidePrompt}
}

// TermAllowed is what a door may run in the builder's terminal: the console
// runs it on a typed y, so a service can never make it run anything but
// these exact commands.
func TermAllowed(argv []string) bool {
	if slices.Equal(argv, GuideArgv(false)) || slices.Equal(argv, GuideArgv(true)) || slices.Equal(argv, brew) {
		return true
	}
	cmd := argv
	if len(cmd) > 0 && cmd[0] == "sudo" {
		cmd = cmd[1:]
	}
	for _, m := range managers {
		if slices.Equal(cmd, m.argv) {
			return true
		}
	}
	return false
}
