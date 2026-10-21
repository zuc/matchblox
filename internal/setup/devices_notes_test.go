package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// phoneLines writes the gate lines of phones with ids, as ClaimPairKey
// writes them, with the two test keys in turn.
func phoneLines(t *testing.T, home string, ids ...string) {
	t.Helper()
	var b strings.Builder
	for i, id := range ids {
		key := []string{phoneKey, phoneKey2}[i%2]
		b.WriteString(`restrict,pty,command="'/opt/mb/matchblox' gate phone ` + id + `" ` + key + " " + phoneComment + "P" + id + "\n")
	}
	writeFile(t, filepath.Join(home, ".ssh", "authorized_keys"), b.String())
}

func TestWithPhoneRefusesABadID(t *testing.T) {
	for _, id := range []string{"xyz", "0123456789AB", "0123456789abc", "0123456789a", "a b"} {
		if _, err := withPhone(nil, "/opt/mb/matchblox", phoneKey, "iPad", id); err == nil {
			t.Errorf("withPhone accepted the id %q", id)
		}
	}
	if _, err := withPhone(nil, "/opt/mb/matchblox", phoneKey, "iPad", ""); err != nil {
		t.Errorf("withPhone refused an empty id: %v", err)
	}
	if _, err := withPhone(nil, "/opt/mb/matchblox", phoneKey, "iPad", "0123456789ab"); err != nil {
		t.Errorf("withPhone refused a good id: %v", err)
	}
}

func TestDeviceTTYsKeepsTheLaterNoteOfATerminal(t *testing.T) {
	home := t.TempDir()
	phoneLines(t, home, "aaaaaaaaaaaa", "bbbbbbbbbbbb")
	old := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	// Each order of the two ids must give the same answer.
	for _, later := range []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb"} {
		for _, id := range []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb"} {
			at := old
			if id == later {
				at = old.Add(time.Hour)
			}
			if err := MarkDeviceIn(home, id, "/dev/ttys004", at); err != nil {
				t.Fatal(err)
			}
		}
		if m := DeviceTTYs(home); len(m) != 1 || m["/dev/ttys004"] != later {
			t.Fatalf("ttys = %v, want %s: its note is the later one", m, later)
		}
	}
}

func TestRemoveDeviceDeletesItsDeviceNote(t *testing.T) {
	home := t.TempDir()
	const id, other = "0123456789ab", "ba9876543210"
	writeFile(t, filepath.Join(home, ".ssh", "authorized_keys"),
		`restrict,pty,command="'/opt/mb/matchblox' gate phone `+id+`" `+phoneKey+" matchblox:phone:Desk\n"+
			`restrict,pty,command="'/opt/mb/matchblox' gate phone `+other+`" `+phoneKey2+" matchblox:phone:Other\n")
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	for _, i := range []string{id, other} {
		if err := MarkDeviceIn(home, i, "/dev/ttys004", at); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := RemoveDevice(home, "Desk"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := DeviceIn(home, id); ok {
		t.Error("the note of the removed device is still there")
	}
	if _, _, ok := DeviceIn(home, other); !ok {
		t.Error("the note of another device went too")
	}
}

// A note whose id has no line is of a device that is gone: removed by hand,
// or paired again with a new id. Its terminal is no device's.
func TestDeviceTTYsIgnoresTheNoteOfAGoneDevice(t *testing.T) {
	home := t.TempDir()
	const id, gone = "0123456789ab", "ba9876543210"
	phoneLines(t, home, id)
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	if err := MarkDeviceIn(home, id, "/dev/ttys004", at); err != nil {
		t.Fatal(err)
	}
	if err := MarkDeviceIn(home, gone, "/dev/ttys005", at); err != nil {
		t.Fatal(err)
	}
	if m := DeviceTTYs(home); len(m) != 1 || m["/dev/ttys004"] != id {
		t.Fatalf("ttys = %v, want only %s", m, id)
	}
}

// A home whose path is a glob pattern still finds its notes.
func TestDeviceTTYsInAHomeWithAPatternInItsPath(t *testing.T) {
	home := filepath.Join(t.TempDir(), "a[b]*?")
	const id = "0123456789ab"
	phoneLines(t, home, id)
	if err := MarkDeviceIn(home, id, "/dev/ttys004", time.Now()); err != nil {
		t.Fatal(err)
	}
	if m := DeviceTTYs(home); m["/dev/ttys004"] != id {
		t.Fatalf("ttys = %v", m)
	}
}

// A device that pairs again keeps its key and gets a new id: the note of
// the old id goes with the old line.
func TestClaimPairKeyRemovesTheNoteOfTheLineItReplaces(t *testing.T) {
	home := t.TempDir()
	const old, other, id = "0123456789ab", "ba9876543210", "abcdef012345"
	phoneLines(t, home, old, other)
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	for _, i := range []string{old, other} {
		if err := MarkDeviceIn(home, i, "/dev/ttys004", at); err != nil {
			t.Fatal(err)
		}
	}
	if err := AddPairKey(home, "/opt/mb/matchblox", pairPub, id, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimPairKey(home, "/opt/mb/matchblox", id, phoneKey, "iPad"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := DeviceIn(home, old); ok {
		t.Error("the note of the replaced line is still there")
	}
	if _, _, ok := DeviceIn(home, other); !ok {
		t.Error("the note of another device went too")
	}
	if _, err := os.Stat(claimNote(home, id)); err != nil {
		t.Errorf("the claim note is gone: %v", err)
	}
}
