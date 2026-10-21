package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blox-eng/matchblox/internal/sample"
	"github.com/blox-eng/matchblox/internal/setup"
)

const laptopLine = `restrict,pty,command="'` + pairExe + `' gate" ` + pairHostKeyBlob + " work-laptop\n"

// pairHostKeyBlob is a key to put on a laptop line.
const pairHostKeyBlob = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIM4bfwfUcmyCX2tnMZ4mHkeEjSRJnkf0ehLEuB8iWLHU"

// devicesConsole is the host's console on the Machine tab, with a laptop in
// authorized_keys first and an iPad after it, and two orphans when asked.
func devicesConsole(t *testing.T, orphans int) (*pairRig, Model) {
	t.Helper()
	r, m := newPairRig(t)
	if err := os.WriteFile(filepath.Join(r.home, ".ssh", "authorized_keys"), []byte(laptopLine), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.AuthorizeDevice(r.home, pairExe, pairPhoneKey, "iPad"); err != nil {
		t.Fatal(err)
	}
	st := fixtureState()
	for i := range orphans {
		pid := 4242 + i
		st.Orphans = append(st.Orphans, sample.Orphan{PID: pid, Start: 7, Comm: "bash", Kill: []string{"kill", "4242"}})
	}
	next, _ := m.Update(stateMsg(st))
	m = next.(Model)
	m.setTab(tabMachine)
	return r, m
}

func TestDevicesListsPhonesFirst(t *testing.T) {
	_, m := devicesConsole(t, 0)
	out := screen(m)
	if !strings.Contains(out, "DEVICES · 2") {
		t.Fatalf("no DEVICES · 2:\n%s", out)
	}
	ipad, laptop, plus := strings.Index(out, "phone   iPad"), strings.Index(out, "laptop  work-laptop"), strings.Index(out, "+ pair a device")
	if ipad < 0 || laptop < ipad || plus < laptop {
		t.Fatalf("phone %d, laptop %d, plus %d, want that order:\n%s", ipad, laptop, plus, out)
	}
	if !strings.Contains(out, "SHA256:") {
		t.Fatalf("no fingerprint:\n%s", out)
	}
}

func TestDevicesPlusRowOpensThePage(t *testing.T) {
	_, m := devicesConsole(t, 0)
	m.selectRow(2) // iPad, laptop, plus
	next, cmd := key(m, "enter")
	got := next.(Model)
	if got.pending != nil || got.pair == nil || cmd == nil {
		t.Fatalf("pending %v, page %v", got.pending, got.pair)
	}
	got = offered(t, got, cmd)
	if got.pair.link == "" {
		t.Fatal("the page made no code")
	}
}

func TestDevicesRemoveNeedsATypedY(t *testing.T) {
	r, m := devicesConsole(t, 0)
	m.selectRow(0) // the iPad
	keep, _ := key(m, "x")
	if p := keep.(Model).pending; p == nil || !strings.Contains(p.String(), "iPad") {
		t.Fatalf("x built %v", p)
	}
	next, _ := key(keep, "n")
	if got := next.(Model); len(got.devices) != 2 || !strings.Contains(got.flash, "cancelled") {
		t.Fatalf("n removed a device: %d left, flash %q", len(got.devices), got.flash)
	}
	if ds, _ := setup.Devices(r.home); len(ds) != 2 {
		t.Fatalf("n changed the file: %v", ds)
	}
	next, _ = key(keep, "y")
	got := next.(Model)
	if len(got.devices) != 1 || got.devices[0].Name != "work-laptop" {
		t.Fatalf("devices after y: %v", got.devices)
	}
	if !strings.Contains(got.flash, "Removed iPad") || !strings.Contains(got.flash, "Backup: ") {
		t.Fatalf("the flash does not name the backup: %q", got.flash)
	}
	if ds, _ := setup.Devices(r.home); len(ds) != 1 || ds[0].Name != "work-laptop" {
		t.Fatalf("the file holds %v", ds)
	}
	if r, ok := got.selectedDevice(); !ok || r.d.Name != "work-laptop" {
		t.Fatalf("the cursor is lost:\n%s", screen(got))
	}
}

func TestDevicesRemoteShowsTheCommand(t *testing.T) {
	_, m := devicesConsole(t, 0)
	m.opt.Host = "ws-1"
	m.setTab(tabMachine)
	out := screen(m)
	if !strings.Contains(out, "DEVICES") || !strings.Contains(out, "run matchblox pair on ws-1") || strings.Contains(out, "+ pair a device") {
		t.Fatalf("remote DEVICES:\n%s", out)
	}
	if n := m.rowCount(); n != 0 {
		t.Fatalf("a remote console reaches %d device rows", n)
	}
	m.opt.Host, m.opt.Pair = "", nil // a fixture with no host: nothing to show
	if out := screen(m); strings.Contains(out, "DEVICES") {
		t.Fatalf("DEVICES with no host and no page:\n%s", out)
	}
}

func TestDevicesKeepsOrphanRows(t *testing.T) {
	_, m := devicesConsole(t, 2)
	m.selectRow(1)
	next, _ := key(m, "x")
	p := next.(Model).pending
	if p == nil || p.rec != "orphan:4243" || p.local != nil {
		t.Fatalf("x on orphan 1 built %+v", p)
	}
	if n := m.rowCount(); n != 5 { // two orphans, two devices, the plus
		t.Fatalf("%d rows", n)
	}
	m.selectRow(2)
	if r, ok := m.selectedDevice(); !ok || r.d.Name != "iPad" {
		t.Fatalf("row 2 is %v", r)
	}
	next, _ = key(m, "down")
	if r, ok := next.(Model).selectedDevice(); !ok || r.d.Name != "work-laptop" {
		t.Fatalf("down from the iPad is %v", r)
	}
}

func TestDevicesReadErrorTellsTheFix(t *testing.T) {
	_, m := devicesConsole(t, 0)
	host := *m.opt.Pair
	host.Devices = func() ([]PairDevice, error) { return nil, errors.New("permission denied") }
	m.opt.Pair = &host
	m.setTab(tabMachine)
	out := screen(m)
	if !strings.Contains(out, "cannot read ~/.ssh/authorized_keys: permission denied. Check its owner and mode (600).") ||
		strings.Contains(out, "iPad") || !strings.Contains(out, "+ pair a device") {
		t.Fatalf("read error:\n%s", out)
	}
}
