package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/blox-eng/matchblox/internal/config"
	"github.com/blox-eng/matchblox/internal/limits"
)

// statusLine is Claude Code's status line command: it keeps the limits the
// status line is given for its config directory, then prints the status
// line it wraps (args after "--"), fed the same input, or else the limits.
// Nothing here fails the status line: a tap that cannot write is skipped.
func statusLine(args []string, stdin io.Reader, out io.Writer, env func(string) string, stateDir string, cfg config.Config, now time.Time) error {
	input, _ := io.ReadAll(stdin)
	dir := env("CLAUDE_CONFIG_DIR")
	if dir == "" {
		dir = filepath.Join(env("HOME"), ".claude")
	}
	_ = limits.Tap(input, dir, stateDir, now)
	if len(args) > 1 && args[0] == "--" {
		cmd := exec.Command(args[1], args[2:]...) //nolint:gosec // the builder's own status line, from their settings
		cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewReader(input), out, os.Stderr
		_ = cmd.Run()
		return nil
	}
	r, ok := limits.ReadTap(stateDir, dir)
	if !ok {
		return nil
	}
	a := limits.Build("anthropic", "", limits.SourceClaude, r, now, cfg.Limits.Quiet)
	_, err := fmt.Fprint(out, limits.Plain(limits.Parts(a, now)))
	return err
}

// cursorStatusLine is Cursor's status line command (`matchblox hook
// statusline --cursor`): it keeps the context of the session in its pane,
// then prints the status line it wraps, or else the bar a status line takes
// the place of. Nothing here fails the status line.
func cursorStatusLine(args []string, stdin io.Reader, out io.Writer, env func(string) string, stateDir string, now time.Time) error {
	input, _ := io.ReadAll(stdin)
	socket, _, _ := strings.Cut(env("TMUX"), ",")
	_ = limits.TapCursor(input, stateDir, socket, env("TMUX_PANE"), now)
	if len(args) > 1 && args[0] == "--" {
		cmd := exec.Command(args[1], args[2:]...) //nolint:gosec // the builder's own status line, from their config
		cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewReader(input), out, os.Stderr
		_ = cmd.Run()
		return nil
	}
	_, err := fmt.Fprint(out, limits.CursorLine(input))
	return err
}
