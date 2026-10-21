package app

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	pairFromDeviceLine   = "Pair from the host's own terminal."
	removeFromDeviceLine = "Remove devices from the host's own terminal."
)

// deviceAttach is how long a device's console may take from its note to its
// tmux client. A client made later on that tty is not the device's: the
// system gave the tty again.
const deviceAttach = 30 * time.Second

// lastClientOf is the tty and the creation time of the client with the
// latest activity in the output of tmux list-clients -F '#{client_activity}
// #{client_created} #{client_tty}'. "" when no line parses.
func lastClientOf(out string) (tty string, created time.Time) {
	best := int64(-1)
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) != 3 {
			continue
		}
		at, err := strconv.ParseInt(f[0], 10, 64)
		made, err2 := strconv.ParseInt(f[1], 10, 64)
		if err == nil && err2 == nil && at > best {
			best, tty, created = at, f[2], time.Unix(made, 0)
		}
	}
	return tty, created
}

// tmuxLastClient asks tmux which client of the home session was active
// last. A tmux error or no session gives "": the guard is not a wall.
func tmuxLastClient() (string, time.Time, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "list-clients", "-t", "=matchblox", "-F", "#{client_activity} #{client_created} #{client_tty}").Output()
	if err != nil {
		return "", time.Time{}, nil
	}
	tty, created := lastClientOf(string(out))
	return tty, created, nil
}

// lastClient is the tty and the creation time of the client that was
// active last, "" on any doubt.
func (m Model) lastClient() (string, time.Time) {
	last := m.opt.LastClient
	if last == nil {
		last = tmuxLastClient
	}
	tty, created, err := last()
	if err != nil {
		return "", time.Time{}
	}
	return tty, created
}

// fromDevice: the client that was active last is a device. The phone and
// the builder share one console, so this is the way to tell who pressed.
// The client is the device's only when the device's note came just before
// the client: a note from an earlier session names a tty that the system
// can give to the builder now. On any doubt the key is the builder's.
func (m Model) fromDevice() bool {
	h := m.opt.Pair
	if h == nil || h.DeviceTTYs == nil || h.DeviceIn == nil {
		return false
	}
	tty, created := m.lastClient()
	if tty == "" || created.IsZero() {
		return false
	}
	id, ok := h.DeviceTTYs()[tty]
	if !ok {
		return false
	}
	at, _, ok := h.DeviceIn(id)
	// tmux gives whole seconds: the note may be up to 1 s after it.
	return ok && !at.After(created.Add(time.Second)) && created.Sub(at) <= deviceAttach
}
