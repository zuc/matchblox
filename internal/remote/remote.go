// Package remote reaches the service of another host over SSH: the console
// there speaks the same wire as a local one, on the stdin and stdout of
// `matchblox serve --stdio`. It uses its own key, which the host lets start
// only `matchblox gate` (design/0005-remote-mode.md §3).
package remote

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/transport"
)

// Why a host cannot be reached; each one has its own fix.
var (
	ErrNotConnected   = errors.New("not connected")
	ErrNotInstalled   = errors.New("matchblox is not installed there")
	ErrHostKeyUnknown = errors.New("the host key is not known")
	ErrHostKeyChanged = errors.New("the host key changed")
)

// ServeCommand is what the console asks the host's gate to run.
const ServeCommand = "matchblox serve --stdio"

// ConsoleCommand is what a phone asks the host's gate to run.
const ConsoleCommand = "matchblox console"

// PushRegisterCommand is what a phone asks the host's gate to run to be
// pushed to: one request line in, one reply line out.
const PushRegisterCommand = "matchblox push register"

// install is the one install command, with no data in it. MATCHBLOX_NO_START
// keeps the script from starting a console inside this one.
const install = "curl -fsSL https://matchblox.sh | MATCHBLOX_NO_START=1 sh"

// KeyName is the matchblox key in ~/.ssh.
const KeyName = "matchblox_ed25519"

// KeyPath is the matchblox key of a home directory.
func KeyPath(home string) string { return filepath.Join(home, ".ssh", KeyName) }

var hostRE = regexp.MustCompile(`^[A-Za-z0-9_.@:%\[\]-]+$`)

// ValidHost is a name ssh reads as a host and never as an option.
func ValidHost(h string) bool { return hostRE.MatchString(h) && !strings.HasPrefix(h, "-") }

// Target is a host and the key that reaches its gate.
type Target struct{ Host, Key string }

// ssh is the locked-down ssh of the console: its own key and no other, no
// password prompt, a known host key only, nothing forwarded. ServerAlive: a
// dropped network ends the connection in 15 s instead of never.
func (t Target) ssh(tty string, cmd string) []string {
	return []string{"ssh", tty, "-i", t.Key,
		"-o", "IdentitiesOnly=yes", "-o", "IdentityAgent=none", "-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=yes", "-o", "ForwardAgent=no", "-o", "ClearAllForwardings=yes",
		"-o", "ConnectTimeout=10", "-o", "ServerAliveInterval=5", "-o", "ServerAliveCountMax=3",
		// Never share a connection with the builder's own ssh: a master
		// opened with their key would bypass the gate, and one opened with
		// this key would refuse their shell.
		"-o", "ControlMaster=no", "-o", "ControlPath=none",
		"--", t.Host, cmd}
}

// Argv joins this console to the host's service.
func (t Target) Argv() []string { return t.ssh("-T", ServeCommand) }

// HostPath is where a host keeps tmux, claude and matchblox when ssh's
// own PATH does not have them (a non-login shell; Homebrew on macOS).
const HostPath = "$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin"

// onHost is the shell line that runs argv on the host as argv: one
// /bin/sh, which every login shell can start (fish and csh too), with
// HostPath in front.
func onHost(argv ...string) string {
	return Quote(append([]string{"/bin/sh", "-c", `PATH="` + HostPath + `:$PATH"; export PATH; exec "$@"`, "sh"}, argv...))
}

// TermArgv runs a command on the host in this terminal with the builder's
// own ssh login: a jump or a door's command is interactive, and the
// matchblox key never gets a terminal there.
func TermArgv(host string, argv []string) []string {
	return []string{"ssh", "-t", "-o", "ForwardAgent=no", "-o", "ClearAllForwardings=yes", "--", host, onHost(argv...)}
}

// NavArgv turns the console's tmux moves into one command that attaches to
// the host's tmux: there is no tmux client of the host to switch.
func NavArgv(host string, steps [][]string) []string {
	cmd := []string{"tmux"}
	attach := []string{"attach-session"}
	for _, s := range steps {
		if len(s) < 2 || s[0] != "tmux" {
			continue
		}
		switch args := s[1:]; args[0] {
		case "switch-client":
			cmd = append(cmd, "select-window", "-t", args[2], ";", "select-pane", "-t", args[2], ";")
			attach = []string{"attach-session", "-t", args[2]}
		case "attach-session":
			attach = args
		default:
			cmd = append(append(cmd, args...), ";")
		}
	}
	return TermArgv(host, append(cmd, attach...))
}

// NavAllowed is what a tmux move may be: the console runs it on one key and
// the gate runs it for the matchblox key, so neither runs anything but the
// moves advice builds, in exactly that shape. tmux runs a trailing argument
// of new-window as a shell command, splits commands on ";", and
// format-expands a start directory (#() runs a shell command). So: four
// arguments, a pane id as the target, and an absolute directory without "#"
// or ";".
func NavAllowed(argv []string) bool {
	if len(argv) != 4 || argv[0] != "tmux" {
		return false
	}
	target := argv[3]
	switch argv[1] {
	case "switch-client", "select-window", "select-pane", "attach-session":
		return argv[2] == "-t" && PaneID(target)
	case "new-window":
		return argv[2] == "-c" && filepath.IsAbs(target) && !strings.ContainsAny(target, "#;")
	}
	return false
}

// StokerPopup opens the stoker's own tmux session (on the server server,
// tmux -L) in a popup over the console: the builder answers the stoker's
// prompts there, and detaching (prefix d) closes it.
func StokerPopup(server string) []string {
	return []string{"tmux", "display-popup", "-E", "-w", "90%", "-h", "90%", "tmux -L " + server + " attach -t stoker"}
}

var serverRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// PopupAllowed is what the console's popup may be: exactly StokerPopup of
// a server name a shell reads as one word. display-popup runs its last
// argument through a shell.
func PopupAllowed(argv []string) bool {
	if len(argv) != 8 {
		return false
	}
	rest, ok := strings.CutPrefix(argv[7], "tmux -L ")
	server, ok2 := strings.CutSuffix(rest, " attach -t stoker")
	return ok && ok2 && serverRE.MatchString(server) && slices.Equal(argv, StokerPopup(server))
}

// PaneID is a tmux pane id such as %12.
func PaneID(s string) bool {
	if len(s) < 2 || s[0] != '%' {
		return false
	}
	for _, r := range s[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

var pubKeyRE = regexp.MustCompile(`^ssh-ed25519 [A-Za-z0-9+/]+={0,2}( [A-Za-z0-9@._-]+)?$`)

// ValidPubKey is one ed25519 public key line, and nothing a shell reads.
func ValidPubKey(k string) bool { return pubKeyRE.MatchString(k) }

var deviceKeyRE = regexp.MustCompile(`^ecdsa-sha2-nistp256 ([A-Za-z0-9+/]+={0,2})( [A-Za-z0-9@._-]+)?$`)

// ValidDeviceKey is one P-256 public key line of a phone (its Secure
// Enclave key), and nothing a shell reads.
func ValidDeviceKey(k string) bool {
	m := deviceKeyRE.FindStringSubmatch(k)
	if m == nil {
		return false
	}
	blob, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil {
		return false
	}
	var fields [3][]byte
	for i := range fields {
		if len(blob) < 4 {
			return false
		}
		n := int(binary.BigEndian.Uint32(blob))
		if n > len(blob)-4 {
			return false
		}
		fields[i], blob = blob[4:4+n], blob[4+n:]
	}
	return len(blob) == 0 && string(fields[0]) == "ecdsa-sha2-nistp256" &&
		string(fields[1]) == "nistp256" && len(fields[2]) == 65 && fields[2][0] == 0x04
}

// Bootstrap is the command that connects a host, run with the builder's own
// ssh login: the verified install (always: an older matchblox does not know
// authorize, and would start its console instead), then authorize, which
// lets the matchblox key start the gate. The key is an argument, never part
// of the script.
func Bootstrap(pubKey string) (string, error) {
	if !ValidPubKey(pubKey) {
		return "", errors.New("not an ed25519 public key")
	}
	script := `PATH="` + HostPath + `:$PATH"; export PATH; ` + install + ` || exit 1; matchblox authorize "$1"`
	return Quote([]string{"/bin/sh", "-c", script, "sh", pubKey}), nil
}

// Classify names why ssh ended, from its exit and its stderr; nil when no
// known cause fits.
func Classify(stderr string, exit int) error {
	switch {
	case strings.Contains(stderr, "REMOTE HOST IDENTIFICATION HAS CHANGED"):
		return ErrHostKeyChanged
	case strings.Contains(stderr, "Host key verification failed"):
		return ErrHostKeyUnknown
	case strings.Contains(stderr, "Permission denied"):
		return ErrNotConnected
	case exit == 127 || strings.Contains(stderr, "matchblox: command not found") || strings.Contains(stderr, "matchblox: not found"):
		return ErrNotInstalled
	}
	return nil
}

// Quote is argv as a shell reads it: a word with a space, a quote or a
// shell character gets single quotes.
func Quote(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = a
		if a == "" || strings.ContainsAny(a, unsafe+"'\\") {
			q[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(q, " ")
}

// Line is argv as the person would type it, and can paste: the command an
// ssh argv runs on the host stays one word, in double quotes when nothing
// in it would expand there.
func Line(argv []string) string {
	n := len(argv) - 1
	if n < 1 || argv[0] != "ssh" {
		return Quote(argv)
	}
	last := argv[n]
	head := Quote(argv[:n])
	if len(argv) > 5 && argv[2] == "-i" && slices.Contains(argv, "IdentitiesOnly=yes") {
		// The console's own ssh: its key and fixed options (Target.ssh)
		// fold to "…".
		return "ssh … " + Quote([]string{argv[n-1]}) + " " + last
	}
	if w, err := Split(last); err == nil && len(w) > 4 && last == onHost(w[4:]...) {
		// A command on the host (TermArgv): the options and the /bin/sh
		// around it fold to "…".
		return "ssh … " + Quote([]string{argv[n-1]}) + " " + Quote(w[4:])
	}
	if Quote([]string{last}) != last && !strings.ContainsAny(last, "\"$`\\!") {
		last = `"` + last + `"`
	} else {
		last = Quote([]string{last})
	}
	return head + " " + last
}

// Split reads back a line that Quote wrote: plain words and single-quoted
// words, one space between them. Anything a shell would expand or join is
// an error, never a guess: the gate runs only what it can read exactly.
func Split(line string) ([]string, error) {
	var out []string
	for i := 0; i < len(line); {
		var w strings.Builder
		quoted := false
		for i < len(line) && line[i] != ' ' {
			switch c := line[i]; {
			case c == '\'':
				j := strings.IndexByte(line[i+1:], '\'')
				if j < 0 {
					return nil, errors.New("an unclosed quote")
				}
				w.WriteString(line[i+1 : i+1+j])
				i, quoted = i+j+2, true
			case c == '\\':
				if strings.HasPrefix(line[i:], `\''`) {
					w.WriteByte('\'')
					i += 2
					continue
				}
				return nil, errors.New("a backslash outside quotes")
			case strings.IndexByte(unsafe, c) >= 0:
				return nil, fmt.Errorf("%q outside quotes", c)
			default:
				w.WriteByte(c)
				i++
			}
		}
		if w.Len() == 0 && !quoted {
			return nil, errors.New("two spaces")
		}
		out = append(out, w.String())
		if i < len(line) {
			i++ // the space
			if i == len(line) {
				return nil, errors.New("a trailing space")
			}
		}
	}
	return out, nil
}

// unsafe are the bytes Quote never leaves outside quotes.
const unsafe = " \t\n\"$`;&|<>()*?#~{}[]!="

// Connect starts ssh and speaks on its stdin and stdout. It returns once ssh
// runs; a host that cannot be reached shows on the first Recv, with the
// cause ssh gave. Without the matchblox key the host is not connected.
func Connect(ctx context.Context, t Target) (transport.Conn, error) {
	if !ValidHost(t.Host) {
		return nil, fmt.Errorf("%q is not a host name", t.Host)
	}
	if _, err := os.Stat(t.Key); err != nil {
		return nil, ErrNotConnected
	}
	argv := t.Argv()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // argv, the host checked by ValidHost
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	c := &conn{cmd: cmd}
	cmd.Stderr = &c.stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c.Conn = transport.Stdio(out, in)
	return c, nil
}

type conn struct {
	transport.Conn
	cmd    *exec.Cmd
	stderr lockedBuffer
	once   sync.Once
	err    error
	spoke  atomic.Bool // the far side sent a message
}

// Recv names why ssh ended instead of a bare EOF.
func (c *conn) Recv() (proto.Envelope, error) {
	env, err := c.Conn.Recv()
	if err != nil {
		return env, c.exit(err)
	}
	c.spoke.Store(true)
	return env, nil
}

func (c *conn) Close() error {
	_ = c.Conn.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	_ = c.exit(nil)
	return nil
}

// exit waits for ssh once and keeps why it ended.
func (c *conn) exit(readErr error) error {
	c.once.Do(func() {
		werr := c.cmd.Wait()
		code := 0
		var ee *exec.ExitError
		if errors.As(werr, &ee) {
			code = ee.ExitCode()
		}
		stderr := strings.TrimSpace(c.stderr.String())
		if c.spoke.Load() && code != 255 {
			// Once the far side spoke, what it wrote first (a first run's
			// note) is not why it ended; ssh's own failures exit 255.
			stderr = ""
		}
		switch err := Classify(stderr, code); {
		case err != nil:
			c.err = err
		case stderr != "":
			c.err = errors.New(lastLine(stderr))
		case code > 0:
			c.err = fmt.Errorf("ssh exited %d", code)
		case werr != nil:
			c.err = werr
		case readErr != nil:
			c.err = readErr
		default:
			c.err = errors.New("ssh ended")
		}
	})
	return c.err
}

func lastLine(s string) string {
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[i+1:])
	}
	return s
}

// lockedBuffer is ssh's stderr: exec writes it while Recv may read it.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.b.Len() > 64<<10 {
		l.b.Reset() // only the last lines matter
	}
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

var ttyRE = regexp.MustCompile(`^/dev/[A-Za-z0-9_/-]{1,32}$`)

// ClientTTY tells if tty is the name of a terminal device, as SSH_TTY and
// tmux's client_tty give it: the client a focus moves.
func ClientTTY(tty string) bool { return ttyRE.MatchString(tty) }

// FocusArgv moves the tmux client on tty to pane: a tap on the island or a
// buzz opens the session's pane on the phone, and on no other terminal.
func FocusArgv(tty, pane string) []string {
	return []string{"tmux", "switch-client", "-c", tty, "-t", pane}
}

// FocusAllowed is exactly FocusArgv of a terminal device and a pane id. It
// is not a Nav step: the gate never runs it.
func FocusAllowed(argv []string) bool {
	return len(argv) == 6 && argv[0] == "tmux" && argv[1] == "switch-client" &&
		argv[2] == "-c" && ClientTTY(argv[3]) && argv[4] == "-t" && PaneID(argv[5])
}
