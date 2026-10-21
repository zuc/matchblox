// Package setup finds the doors of this machine and opens them: it shows
// each change as an exact diff, writes it after a confirm, keeps a backup,
// and refuses when the file changed after the preview.
package setup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/blox-eng/matchblox/internal/doors"
	"github.com/blox-eng/matchblox/internal/stoker"
)

// Env is the machine the doors are for.
type Env struct {
	GOOS, OSRelease, Home string
	// Exe is the matchblox the hooks run. Empty: "matchblox" on the PATH.
	Exe      string
	StateDir string // where closed doors are kept
	// CompactAt is the stoker's line (compact_at), the percentage the
	// safety net sets. 0: the default, 85.
	CompactAt float64
	Root      bool // a root shell installs packages without sudo
	Getenv    func(string) string
	LookPath  func(string) (string, error)
	// Source loads a tmux config into the running server. ErrNoServer: no
	// server runs, the next one reads the file.
	Source func(conf string) error
	// Run runs a command of a door that writes a file (launchctl, systemctl).
	// Nil: run it on this machine.
	Run func(argv []string) error
	UID int // the user the launchd domain gui/<uid> is for
}

var (
	ErrChanged  = errors.New("the file changed after the preview: press ⏎ to see the new diff")
	ErrNoServer = errors.New("no tmux server runs")
)

// Doors is every door of the machine, in order. Doors that are done or
// closed are in the list too; the console shows only the open ones.
func Doors(e Env) []doors.Door {
	closed := closedDoors(e.StateDir)
	_, tmuxErr := e.LookPath("tmux")
	ds := []doors.Door{tmuxDoor(e, tmuxErr == nil)}
	if usesClaude(e) {
		ds = append(ds, hooksDoor(e), limitsDoor(e), safetyNetDoor(e))
	}
	ds = append(ds, wayBackDoor(e), pairDoor(e))
	if d := keepRunningDoor(e); d.ID != "" {
		ds = append(ds, d)
	}
	if usesClaude(e) {
		ds = append(ds, guideDoor(), nightDoor(e))
	}
	ds = append(ds, waysDoor(e))
	for i := range ds {
		ds[i].Closed = slices.Contains(closed, ds[i].ID)
	}
	return ds
}

// Open runs a door that changes a file. seen is the Sum of the preview the
// person confirmed; also is the door's Also, when they turned it on. It
// returns the backup, "" when there was no file.
func Open(e Env, id, seen string, also bool) (backup string, err error) {
	switch id {
	case doors.Hooks:
		return AddHooks(settingsPath(e), e.Exe, seen)
	case doors.Limits:
		return TapStatusLine(settingsPath(e), e.Exe, seen)
	case doors.SafetyNet:
		return TurnOnSafetyNet(settingsPath(e), e.compactAt(), seen)
	case doors.WayBack:
		return AddWayBack(tmuxConf(e), seen, also, e.Source)
	case doors.KeepRunning:
		return "", openKeepRunning(e, seen)
	case doors.Ways:
		return "", WriteWays(waysPath(e), seen)
	case doors.Tmux, doors.Guide:
		return "", fmt.Errorf("the %s door runs in your terminal", id)
	}
	return "", fmt.Errorf("no door %q", id)
}

var known = []string{doors.Tmux, doors.Hooks, doors.Limits, doors.SafetyNet, doors.WayBack, doors.Pair, doors.KeepRunning, doors.Guide, doors.Night, doors.Ways}

// Close hides a door from the console; `matchblox setup` shows it again.
func Close(e Env, id string) error {
	if !slices.Contains(known, id) {
		return fmt.Errorf("no door %q", id)
	}
	closed := closedDoors(e.StateDir)
	if slices.Contains(closed, id) {
		return nil
	}
	b, err := json.Marshal(struct {
		Closed []string `json:"closed"`
	}{append(closed, id)})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(e.StateDir, 0o700); err != nil {
		return err
	}
	return replace(filepath.Join(e.StateDir, "doors.json"), b, 0o600)
}

func closedDoors(dir string) []string {
	var s struct {
		Closed []string `json:"closed"`
	}
	if b, err := os.ReadFile(filepath.Join(dir, "doors.json")); err == nil { //nolint:gosec // our own state dir
		_ = json.Unmarshal(b, &s) // a broken file opens every door again
	}
	return s.Closed
}

// Sum names the contents of a file; a missing file is an empty one.
func Sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}

// read is the file a link points to, and its contents (nil when missing).
func read(path string) (target string, b []byte, mode fs.FileMode, err error) {
	target, mode = path, 0o600
	if t, err := filepath.EvalSymlinks(path); err == nil {
		target = t
	} else if link, lerr := os.Readlink(path); lerr == nil {
		// A dotfiles link to a file not made yet: write where it points.
		if !filepath.IsAbs(link) {
			link = filepath.Join(filepath.Dir(path), link)
		}
		target = link
	}
	b, err = os.ReadFile(target) //nolint:gosec // the person's own config file
	if errors.Is(err, fs.ErrNotExist) {
		return target, nil, mode, nil
	}
	if fi, statErr := os.Stat(target); statErr == nil {
		mode = fi.Mode().Perm()
	}
	return target, b, mode, err
}

// edit writes change(old) to path when the file is still what the person
// saw, after a backup next to it. A link stays a link: the file it points
// to changes.
func edit(path, seen string, change func(old []byte) ([]byte, error)) (backup string, err error) {
	target, old, mode, err := read(path)
	if err != nil {
		return "", err
	}
	if Sum(old) != seen {
		return "", ErrChanged
	}
	next, err := change(old)
	if err != nil || bytes.Equal(next, old) {
		return "", err
	}
	if old != nil {
		if backup, err = keep(target, old, mode); err != nil {
			return "", fmt.Errorf("backup: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return backup, err
	}
	return backup, replace(target, next, mode)
}

// keep writes a backup next to path under a name no other backup has.
func keep(path string, b []byte, mode fs.FileMode) (string, error) {
	stamp := path + ".matchblox-" + time.Now().Format("20060102-150405")
	for n := 0; ; n++ {
		name := stamp + ".bak"
		if n > 0 {
			name = fmt.Sprintf("%s-%d.bak", stamp, n)
		}
		f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode) //nolint:gosec // next to the person's own file
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.Write(b); err != nil {
			_ = f.Close()
			return "", err
		}
		return name, f.Close()
	}
}

// replace writes a file in one rename, so a reader never sees half of it.
func replace(path string, b []byte, mode fs.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) //nolint:errcheck // gone after the rename
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func tmuxDoor(e Env, have bool) doors.Door {
	d := doors.Door{ID: doors.Tmux, Title: "Install tmux", Done: have,
		Why: "The console and your agents live in tmux. You type your password in this terminal."}
	if have {
		return d
	}
	d.Term = doors.TmuxInstall(e.GOOS, e.OSRelease, e.LookPath)
	if d.Term == nil {
		d.Problem = "install tmux with your package manager: https://github.com/tmux/tmux/wiki/Installing"
		return d
	}
	if d.Term[0] != "sudo" && d.Term[0] != "brew" && !e.Root {
		d.Problem = "install tmux as root: " + shellJoin(d.Term)
		d.Term = nil
		return d
	}
	d.Preview = shellJoin(d.Term)
	return d
}

// nightDoor leads to the stoker. It is done once the stoker has run, and
// the console sends the stoker act itself: nothing here writes a file.
func nightDoor(e Env) doors.Door {
	d := doors.Door{ID: doors.Night, Title: "Try a night",
		Why: "Going to sleep? n keeps your sessions lit until morning: each full one compacts and leaves a RESUME line."}
	if m, err := stoker.ReadMode(e.StateDir); err == nil && !m.Since.IsZero() {
		d.Done = true
	}
	return d
}

// pairDoor leads to the pair page. It is done once a phone is paired: the
// console opens the page itself, and nothing here writes a file.
func pairDoor(e Env) doors.Door {
	d := doors.Door{ID: doors.Pair, Title: "Pair a device", Why: "answer your agents from your phone"}
	if ds, err := Devices(e.Home); err == nil {
		for _, x := range ds {
			if x.Kind == "phone" {
				d.Done = true
			}
		}
	}
	return d
}

func guideDoor() doors.Door {
	argv := doors.GuideArgv(false)
	return doors.Door{ID: doors.Guide, Title: "Open a guide session", Term: argv, Preview: shellJoin(argv),
		Why: "Claude Code walks you through matchblox. This starts Claude Code and uses your tokens."}
}
