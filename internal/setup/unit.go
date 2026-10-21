package setup

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/blox-eng/matchblox/internal/doors"
)

const (
	plistName   = "com.blox.matchblox.plist"
	serviceName = "matchblox.service"
)

// unit is what keeps `matchblox serve` running on this machine: the file,
// its text, and the command that starts it.
type unit struct {
	path, text string
	argv       []string
	advice     string // a line for the preview, "" for none
	// before runs first and its error is ignored (an agent not loaded yet).
	before []string
}

// unitFor is the user unit for e.GOOS: a launchd agent on macOS, a systemd
// user unit on Linux. ok is false on any other system.
func unitFor(e Env) (u unit, ok bool) {
	exe := e.Exe
	if exe == "" {
		exe = "matchblox"
		if e.LookPath != nil {
			if p, err := e.LookPath(exe); err == nil {
				exe = p
			}
		}
	}
	switch e.GOOS {
	case "darwin":
		path := filepath.Join(e.Home, "Library", "LaunchAgents", plistName)
		return unit{path: path, text: plist(exe, filepath.Join(e.StateDir, "serve.log"), e.Getenv("PATH")),
			before: []string{"launchctl", "bootout", "gui/" + strconv.Itoa(e.UID) + "/com.blox.matchblox"},
			argv:   []string{"launchctl", "bootstrap", "gui/" + strconv.Itoa(e.UID), path}}, true
	case "linux":
		dir := e.Getenv("XDG_CONFIG_HOME")
		if dir == "" {
			dir = filepath.Join(e.Home, ".config")
		}
		return unit{path: filepath.Join(dir, "systemd", "user", serviceName), text: service(exe, e.Getenv("PATH")),
			argv:   []string{"systemctl", "--user", "enable", "--now", serviceName},
			advice: "# to keep it running after you log out: loginctl enable-linger"}, true
	}
	return unit{}, false
}

// The unit sets PATH to the one of the process that opens the door: launchd
// and systemd start with a minimal one, and serve runs tmux and claude.
// Both start serve again only after a nonzero exit (the binary was
// replaced, or the socket removed): exit 0 means the service already runs.
func plist(exe, log, path string) string {
	esc := func(s string) string {
		var b bytes.Buffer
		_ = xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	env := ""
	if path != "" {
		env = "\t<key>EnvironmentVariables</key>\n\t<dict>\n\t\t<key>PATH</key>\n\t\t<string>" + esc(path) + "</string>\n\t</dict>\n"
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.blox.matchblox</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + esc(exe) + `</string>
		<string>serve</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
` + env + `	<key>StandardErrorPath</key>
	<string>` + esc(log) + `</string>
</dict>
</plist>
`
}

func service(exe, path string) string {
	// systemd splits ExecStart on spaces: quote a path that has one.
	if strings.ContainsAny(exe, " \t") {
		exe = `"` + strings.ReplaceAll(exe, `"`, `\"`) + `"`
	}
	exe = strings.ReplaceAll(exe, "%", "%%") // systemd specifiers
	env := ""
	if path != "" {
		env = "Environment=\"PATH=" + strings.NewReplacer("%", "%%", `"`, `\"`).Replace(path) + "\"\n"
	}
	return `[Unit]
Description=matchblox serve: the phone buzzes with no console open

[Service]
ExecStart=` + exe + ` serve
Restart=on-failure
RestartSec=5
` + env + `
[Install]
WantedBy=default.target
`
}

// keepRunningDoor writes the user unit and starts it, so the phone buzzes
// with no console open. It is done when the file holds this very text. The
// zero Door (no ID): this system has no user units.
func keepRunningDoor(e Env) doors.Door {
	u, ok := unitFor(e)
	if !ok {
		return doors.Door{}
	}
	d := doors.Door{ID: doors.KeepRunning, Title: "Keep matchblox running", Path: u.path, Term: u.argv,
		Why: "the phone buzzes with no console open"}
	_, old, _, err := read(u.path)
	if err != nil {
		d.Problem = err.Error()
		return d
	}
	if string(old) == u.text {
		d.Done = true
		return d
	}
	d.Sum = Sum(old)
	d.Preview = diff(string(old), u.text) + "\n\nthen runs: " + shellJoin(u.argv)
	if u.advice != "" {
		d.Preview += "\n" + u.advice
	}
	return d
}

// openKeepRunning writes the unit when the file is still what the person
// saw, then runs the command that starts it. A refused write runs nothing.
func openKeepRunning(e Env, seen string) error {
	u, ok := unitFor(e)
	if !ok {
		return fmt.Errorf("no user unit on %s", e.GOOS)
	}
	target, old, _, err := read(u.path)
	if err != nil {
		return err
	}
	if _, err := edit(u.path, seen, func([]byte) ([]byte, error) { return []byte(u.text), nil }); err != nil {
		return err
	}
	run := e.Run
	if run == nil {
		run = func(argv []string) error {
			out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput() //nolint:gosec // the unit's own command, from unitFor
			if err != nil {
				return fmt.Errorf("%s: %w: %s", shellJoin(argv), err, strings.TrimSpace(string(out)))
			}
			return nil
		}
	}
	if u.before != nil {
		_ = run(u.before) // a stale agent leaves, so the new file is the one that loads
	}
	if err := run(u.argv); err != nil {
		// The service did not start: put the file back, so the door stays open.
		if old == nil {
			_ = os.Remove(target)
		} else {
			_ = replace(target, old, 0o600)
		}
		return err
	}
	return nil
}
