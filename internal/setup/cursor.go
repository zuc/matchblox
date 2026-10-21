package setup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/blox-eng/matchblox/internal/doors"
	"github.com/blox-eng/matchblox/internal/hooks"
)

// cursorHooksPath is Cursor's user hooks, the ones every Cursor session
// runs.
func cursorHooksPath(e Env) string {
	return filepath.Join(e.Home, ".cursor", "hooks.json")
}

func usesCursor(e Env) bool {
	if _, err := e.LookPath("cursor-agent"); err == nil {
		return true
	}
	_, err := os.Stat(filepath.Dir(cursorHooksPath(e)))
	return err == nil
}

func cursorHooksDoor(e Env) doors.Door {
	path := cursorHooksPath(e)
	d := doors.Door{ID: doors.CursorHooks, Title: "Add the Cursor hooks", Path: path,
		Why: "Cursor tells matchblox when a session starts, ends and finishes its turn. Without the hooks its queue is read from its pane."}
	_, old, _, err := read(path)
	if err != nil {
		d.Problem = err.Error()
		return d
	}
	next, err := withCursorHooks(old, e.Exe)
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

// AddCursorHooks adds `matchblox hook --cursor` to each of hooks.CursorEvents
// in Cursor's hooks.json. Other hooks and keys stay as they are, and nothing
// is added twice.
func AddCursorHooks(path, exe, seen string) (backup string, err error) {
	return edit(path, seen, func(old []byte) ([]byte, error) { return withCursorHooks(old, exe) })
}

// withCursorHooks is Cursor's hooks.json with matchblox in every event of
// hooks.CursorEvents, and the version Cursor needs. As withHooks does, it
// writes again only the arrays it adds to.
func withCursorHooks(old []byte, exe string) ([]byte, error) {
	top, err := members(old)
	if err != nil {
		return nil, err
	}
	changed := false
	if lastIndex(top, "version") < 0 {
		top = slices.Insert(top, 0, member{key: "version", val: json.RawMessage("1")})
		changed = true
	}
	i := lastIndex(top, "hooks")
	if i >= 0 && string(bytes.TrimSpace(top[i].val)) == "null" {
		top[i].val = json.RawMessage("{}")
	}
	if i < 0 {
		top = append(top, member{key: "hooks", val: json.RawMessage("{}")})
		i = len(top) - 1
	}
	events, err := members(top[i].val)
	if err != nil {
		return nil, fmt.Errorf("hooks: %w", err)
	}
	added := false
	for _, ev := range hooks.CursorEvents {
		j := lastIndex(events, ev)
		var entries []json.RawMessage
		if j >= 0 {
			if err := json.Unmarshal(events[j].val, &entries); err != nil {
				return nil, fmt.Errorf("hooks.%s: %w", ev, err)
			}
		}
		if runsCursorHook(entries) {
			continue
		}
		val := array(append(entries, json.RawMessage(`{"command": `+quote(cursorHookCommand(exe))+`}`)), 2)
		if j >= 0 {
			events[j].val = val
		} else {
			events = append(events, member{key: ev, val: val})
		}
		added = true
	}
	if added {
		top[i].val = object(events, 1)
	}
	if !changed && !added {
		return old, nil
	}
	return append(object(top, 0), '\n'), nil
}

// cursorHookCommand is one command for every Cursor event: the event is in
// the payload.
func cursorHookCommand(exe string) string {
	if exe == "" {
		exe = "matchblox"
	}
	return shellQuote(exe) + " hook --cursor"
}

// runsCursorHook: an entry already runs `matchblox hook --cursor`, from any
// path.
func runsCursorHook(entries []json.RawMessage) bool {
	for _, raw := range entries {
		var h struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(raw, &h) != nil {
			continue
		}
		f := shellWords(h.Command)
		if len(f) >= 3 && filepath.Base(f[0]) == "matchblox" && f[1] == "hook" && f[2] == "--cursor" {
			return true
		}
	}
	return false
}
