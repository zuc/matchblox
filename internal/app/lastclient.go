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

// lastClientTTY is the tty of the client with the latest activity in the
// output of tmux list-clients -F '#{client_activity} #{client_tty}'. ""
// when no line parses.
func lastClientTTY(out string) string {
	best, tty := int64(-1), ""
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) != 2 {
			continue
		}
		at, err := strconv.ParseInt(f[0], 10, 64)
		if err == nil && at > best {
			best, tty = at, f[1]
		}
	}
	return tty
}

// tmuxLastClient asks tmux which client of the home session was active
// last. A tmux error or no session gives "": the guard is not a wall.
func tmuxLastClient() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "list-clients", "-t", "=matchblox", "-F", "#{client_activity} #{client_tty}").Output()
	if err != nil {
		return "", nil
	}
	return lastClientTTY(string(out)), nil
}

// lastClient is the tty of the client that was active last, "" on any doubt.
func (m Model) lastClient() string {
	last := m.opt.LastClient
	if last == nil {
		last = tmuxLastClient
	}
	tty, err := last()
	if err != nil {
		return ""
	}
	return tty
}

// fromDevice: the client that was active last is a device. The phone and
// the builder share one console, so this is the way to tell who pressed.
// On any doubt the key is the builder's.
func (m Model) fromDevice() bool {
	if m.opt.Pair == nil || m.opt.Pair.DeviceTTYs == nil {
		return false
	}
	tty := m.lastClient()
	if tty == "" {
		return false
	}
	_, ok := m.opt.Pair.DeviceTTYs()[tty]
	return ok
}
