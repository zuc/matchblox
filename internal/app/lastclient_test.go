package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/setup"
)

func TestLastClientPicksTheLatest(t *testing.T) {
	out := "1700000100 1699990000 /dev/ttys003\n1700000900 1699990007 /dev/ttys007\n1700000500 1699990001 /dev/ttys001\n"
	if tty, created := lastClientOf(out); tty != "/dev/ttys007" || !created.Equal(time.Unix(1699990007, 0)) {
		t.Fatalf("got %q, %v", tty, created)
	}
	for _, none := range []string{"", "\n", "x y z\nnot-a-number 1 /dev/ttys1\n1 not-a-number /dev/ttys1\n1700000100 /dev/ttys003\n"} {
		if tty, _ := lastClientOf(none); tty != "" {
			t.Errorf("%q gave %q", none, tty)
		}
	}
}

// phoneIn puts a device in on tty and makes it the last client: its note
// came a moment before its client.
func phoneIn(t *testing.T, r *pairRig, m *Model, tty *string) {
	t.Helper()
	at := time.Date(2026, 10, 21, 14, 0, 0, 0, time.UTC)
	m.opt.Pair.DeviceTTYs = func() map[string]string { return map[string]string{"/dev/ttys009": "phone-id"} }
	m.opt.Pair.DeviceIn = func(id string) (time.Time, string, bool) { return at, "/dev/ttys009", id == "phone-id" }
	m.opt.LastClient = func() (string, time.Time, error) { return *tty, at.Add(time.Second), nil }
	_ = r
}

// The system gives a tty again: a client made on it after the device's
// note is the builder's, and the builder pairs.
func TestANewClientOnTheTTYOfANoteIsTheBuilders(t *testing.T) {
	r, m := pairDoorConsole(t)
	const id, tty = "0123456789ab", "/dev/ttys005"
	line := `restrict,pty,command="'` + pairExe + `' gate phone ` + id + `" ` + pairPhoneKey + " matchblox:phone:iPad\n"
	if err := os.WriteFile(filepath.Join(r.home, ".ssh", "authorized_keys"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	if err := setup.MarkDeviceIn(r.home, id, tty, at); err != nil {
		t.Fatal(err)
	}
	created := at.Truncate(time.Second).Add(2 * time.Second) // the device's own client
	m.opt.LastClient = func() (string, time.Time, error) { return tty, created, nil }
	if !m.fromDevice() {
		t.Fatal("the device's own client is not a device")
	}
	created = at.Add(24 * time.Hour) // the builder, a day later, on the same tty
	if m.fromDevice() {
		t.Fatal("a later client on the tty of a note counted as the device")
	}
	next, cmd := key(m, "enter")
	if got := next.(Model); got.pair == nil || cmd == nil {
		t.Fatalf("the builder got no page: flash %q", got.flash)
	}
	created = time.Time{} // tmux gave no creation time
	if m.fromDevice() {
		t.Fatal("a client of no known age counted as the device")
	}
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
	m.opt.LastClient = func() (string, time.Time, error) { return "/dev/ttys009", time.Time{}, errors.New("no server") }
	if m.fromDevice() {
		t.Fatal("a tmux error counted as a device")
	}
	m.opt.LastClient = func() (string, time.Time, error) { return "", time.Time{}, nil }
	if m.fromDevice() {
		t.Fatal("no client counted as a device")
	}
}
