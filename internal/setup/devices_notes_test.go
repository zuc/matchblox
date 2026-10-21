package setup

import (
	"path/filepath"
	"testing"
	"time"
)

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
