package setup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/blox-eng/matchblox/internal/doors"
)

func limitsDoor(e Env) doors.Door {
	path := settingsPath(e)
	d := doors.Door{ID: doors.Limits, Title: "Show your limits", Path: path,
		Why: "See how much of this 5-hour window and this week each account has used. Your status line stays as it is."}
	_, old, _, err := read(path)
	if err != nil {
		d.Problem = err.Error()
		return d
	}
	next, err := withStatusLine(old, func(wrapped string) string { return statusLineCommand(e.Exe, wrapped) })
	if err != nil {
		d.Problem = tilde(e.Home, path) + " is not valid JSON (" + err.Error() + "): fix it, then this door opens"
		return d
	}
	if bytes.Equal(next, old) {
		d.Done = true
		return d
	}
	d.Sum, d.Preview = Sum(old), diff(string(old), string(next))
	return d
}

// TapStatusLine makes Claude Code's status line run `matchblox hook
// statusline`, which keeps the limits it is given. A status line that is
// there keeps running inside it and prints what it printed.
func TapStatusLine(settingsPath, exe, seen string) (backup string, err error) {
	return edit(settingsPath, seen, func(old []byte) ([]byte, error) {
		return withStatusLine(old, func(wrapped string) string { return statusLineCommand(exe, wrapped) })
	})
}

func statusLineCommand(exe, wrapped string) string {
	if exe == "" {
		exe = "matchblox"
	}
	cmd := shellQuote(exe) + " hook statusline"
	if wrapped != "" {
		cmd += " -- sh -c " + shellQuote(wrapped)
	}
	return cmd
}

// withStatusLine is the settings with the tap (command, given the status
// line it wraps) in the status line, on one line so the diff stays short;
// the other keys keep their bytes. Claude Code and Cursor keep a status line
// alike.
func withStatusLine(old []byte, command func(wrapped string) string) ([]byte, error) {
	top, err := members(old)
	if err != nil {
		return nil, err
	}
	i := lastIndex(top, "statusLine")
	var cur []member
	if i >= 0 && string(bytes.TrimSpace(top[i].val)) != "null" {
		if cur, err = members(top[i].val); err != nil {
			return nil, fmt.Errorf("statusLine: %w", err)
		}
	}
	wrapped := ""
	if j := lastIndex(cur, "command"); j >= 0 {
		if err := json.Unmarshal(cur[j].val, &wrapped); err != nil {
			return nil, fmt.Errorf("statusLine.command: %w", err)
		}
		// Any build of matchblox (matchblox-dev too) already taps it.
		if f := shellWords(wrapped); len(f) >= 3 && strings.HasPrefix(filepath.Base(f[0]), "matchblox") && f[1] == "hook" && f[2] == "statusline" {
			return old, nil
		}
	}
	parts := []string{`"type": "command"`, `"command": ` + quote(command(strings.TrimSpace(wrapped)))}
	for _, m := range cur {
		if m.key != "type" && m.key != "command" {
			parts = append(parts, quote(m.key)+": "+string(compact(m.val)))
		}
	}
	val := json.RawMessage("{" + strings.Join(parts, ", ") + "}")
	if i >= 0 {
		top[i].val = val
	} else {
		top = append(top, member{key: "statusLine", val: val})
	}
	return append(object(top, 0), '\n'), nil
}

func compact(v json.RawMessage) []byte {
	var b bytes.Buffer
	if json.Compact(&b, v) != nil {
		return v
	}
	return b.Bytes()
}
