package app

import (
	"errors"
	"testing"

	"github.com/blox-eng/matchblox/internal/setup"
)

func TestLastClientPicksTheLatest(t *testing.T) {
	out := "1700000100 /dev/ttys003\n1700000900 /dev/ttys007\n1700000500 /dev/ttys001\n"
	if got := lastClientTTY(out); got != "/dev/ttys007" {
		t.Fatalf("got %q", got)
	}
	for _, none := range []string{"", "\n", "x y z\nnot-a-number /dev/ttys1\n"} {
		if got := lastClientTTY(none); got != "" {
			t.Errorf("%q gave %q", none, got)
		}
	}
}

// phoneIn puts a device in on tty and makes it the last client.
func phoneIn(t *testing.T, r *pairRig, m *Model, tty *string) {
	t.Helper()
	m.opt.Pair.DeviceTTYs = func() map[string]string { return map[string]string{"/dev/ttys009": "phone-id"} }
	m.opt.LastClient = func() (string, error) { return *tty, nil }
	_ = r
}

func TestDeviceGetsTheRefusals(t *testing.T) {
	r, m := pairDoorConsole(t)
	tty := "/dev/ttys009"
	phoneIn(t, r, &m, &tty)
	next, cmd := key(m, "enter")
	got := next.(Model)
	if got.pair != nil || cmd != nil || got.flash != "Pair from the host's own terminal." {
		t.Fatalf("page %v, flash %q", got.pair, got.flash)
	}

	dr, dm := devicesConsole(t, 0)
	phoneIn(t, r, &dm, &tty)
	dm.selectRow(2) // the plus row
	next, _ = key(dm, "enter")
	if got := next.(Model); got.pair != nil || got.flash != "Pair from the host's own terminal." {
		t.Fatalf("plus row: page %v, flash %q", got.pair, got.flash)
	}
	dm.selectRow(0)
	keep, _ := key(dm, "x")
	next, _ = key(keep, "y")
	got = next.(Model)
	if got.flash != "Remove devices from the host's own terminal." || len(got.devices) != 2 {
		t.Fatalf("flash %q, %d devices", got.flash, len(got.devices))
	}
	if ds, _ := setup.Devices(dr.home); len(ds) != 2 {
		t.Fatalf("the file holds %v", ds)
	}
}

func TestDeviceAfterXGetsTheRefusalAtConfirm(t *testing.T) {
	r, m := devicesConsole(t, 0)
	tty := "/dev/ttys001" // the builder at x
	phoneIn(t, r, &m, &tty)
	m.selectRow(0)
	keep, _ := key(m, "x")
	if keep.(Model).pending == nil {
		t.Fatal("the builder's x built nothing")
	}
	tty = "/dev/ttys009" // the phone presses y
	next, _ := key(keep, "y")
	if got := next.(Model); got.flash != "Remove devices from the host's own terminal." || len(got.devices) != 2 {
		t.Fatalf("flash %q, %d devices", got.flash, len(got.devices))
	}
}

func TestDeviceRetryOnAnExpiredPageIsRefused(t *testing.T) {
	r, m := pairDoorConsole(t)
	tty := "/dev/ttys001"
	phoneIn(t, r, &m, &tty)
	m, _ = m.openPairDoor()
	m.pair.failed = true
	tty = "/dev/ttys009"
	next, cmd := key(m, "enter")
	if got := next.(Model); got.pair == nil || cmd != nil || got.flash != "Pair from the host's own terminal." {
		t.Fatalf("page %v, flash %q", got.pair, got.flash)
	}
}

func TestBuilderAfterDevice(t *testing.T) {
	r, m := pairDoorConsole(t)
	tty := "/dev/ttys009"
	phoneIn(t, r, &m, &tty)
	if next, _ := key(m, "enter"); next.(Model).pair != nil {
		t.Fatal("the device opened the page")
	}
	tty = "/dev/ttys001"
	next, cmd := key(m, "enter")
	got := next.(Model)
	if got.pair == nil || cmd == nil {
		t.Fatalf("the builder got no page: flash %q", got.flash)
	}
}

func TestTmuxErrorIsTheBuilders(t *testing.T) {
	r, m := pairDoorConsole(t)
	tty := ""
	phoneIn(t, r, &m, &tty)
	m.opt.LastClient = func() (string, error) { return "/dev/ttys009", errors.New("no server") }
	if m.fromDevice() {
		t.Fatal("a tmux error counted as a device")
	}
	m.opt.LastClient = func() (string, error) { return "", nil }
	if m.fromDevice() {
		t.Fatal("no client counted as a device")
	}
}
