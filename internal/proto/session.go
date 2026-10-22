package proto

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/blox-eng/matchblox/internal/panes"
	"github.com/blox-eng/matchblox/internal/sample"
)

// The names and the steps of a session's life (#74): what the console
// checks before it sends new:session or rename:<pane>, and shows before
// kill:<pane>, and what the service holds to.

// MaxName bounds a session's name: it heads a row at 46 columns.
const MaxName = 40

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// CheckName tells if name can be both a tmux session's and an agent's: a
// word of letters, digits, "-" and "_". tmux takes no "." or ":" in a
// session name, and a space would need quotes in every command shown.
func CheckName(name string) error {
	switch {
	case name == "":
		return errors.New("the session needs a name")
	case len(name) > MaxName:
		return errors.New("a name is 40 characters at most")
	case !nameRe.MatchString(name):
		return errors.New("a name is letters, digits, - and _, and starts with a letter or a digit")
	}
	return nil
}

// ErrDash is CheckFirst's refusal of a message that starts with a dash.
var ErrDash = errors.New("does not start with -")

// CheckFirst checks the first message an agent starts with, what in the
// refusal ("the first message", "the handoff"): one line it takes as typed.
// The console checks before it sends, the service again.
func CheckFirst(text, what string) error {
	if err := panes.CheckAnswer(text); err != nil {
		return errors.New(strings.Replace(err.Error(), "the answer", what, 1))
	}
	// It goes to the agent as an argument: one that starts with a dash
	// would be read as a flag (--dangerously-skip-permissions).
	if strings.HasPrefix(strings.TrimSpace(text), "-") {
		return fmt.Errorf("%s %w", what, ErrDash)
	}
	return nil
}

// NameOf makes a name from a path's last part or a branch: what CheckName
// takes, with every other character as "-".
func NameOf(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-'):
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	n := strings.Trim(b.String(), "-_")
	for strings.Contains(n, "--") {
		n = strings.ReplaceAll(n, "--", "-")
	}
	if len(n) > MaxName {
		n = strings.TrimRight(n[:MaxName], "-_")
	}
	return n
}

// KillSteps end the agent pid in pane: kill-pane hangs the pane up, and a
// TERM follows for an agent that outlived it. No pid: the pane alone.
func KillSteps(pane string, pid int) [][]string {
	tmux, id := sample.Tmux(pane)
	steps := [][]string{slices.Concat(tmux, []string{"kill-pane", "-t", id})}
	if pid > 0 {
		steps = append(steps, []string{"kill", "-TERM", strconv.Itoa(pid)})
	}
	return steps
}
