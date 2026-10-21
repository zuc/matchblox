package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/push"
)

const regID, regID2 = "0123456789ab", "ba9876543210"

func homeWithPhones(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".ssh", "authorized_keys"),
		`restrict,pty,command="'/opt/mb/matchblox' gate phone `+regID+`" `+phoneKey+" matchblox:phone:Desk\n"+
			`restrict,pty,command="'/opt/mb/matchblox' gate phone `+regID2+`" `+phoneKey2+" matchblox:phone:Other\n")
	return home
}

func reg(id, token string) push.Registration {
	return push.Registration{Device: id, At: time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC),
		RegisterRequest: push.RegisterRequest{DeviceToken: token, Buzz: true}}
}

// A phone paired before ids has `gate phone` alone. The service gives it an
// id once; only the command part changes, and a second run changes nothing.
func TestOldPhoneLinesGetAnID(t *testing.T) {
	home := t.TempDir()
	laptop := `restrict,command="'/opt/mb/matchblox' gate" ` + pub + " matchblox"
	keys := "# my keys\n" +
		`restrict,pty,command="'/opt/mb/matchblox' gate phone" ` + phoneKey + " matchblox:phone:Desk\n" +
		laptop + "\n" +
		`restrict,pty,command="'/opt/mb/matchblox' gate phone ` + regID2 + `" ` + phoneKey2 + " matchblox:phone:Other\n"
	writeFile(t, filepath.Join(home, ".ssh", "authorized_keys"), keys)
	n, err := UpgradePhoneLines(home)
	if err != nil || n != 1 {
		t.Fatalf("UpgradePhoneLines = %d, %v; want 1", n, err)
	}
	lines := strings.Split(authKeys(home), "\n")
	if len(lines) != 5 || lines[0] != "# my keys" || lines[2] != laptop || lines[4] != "" ||
		lines[3] != `restrict,pty,command="'/opt/mb/matchblox' gate phone `+regID2+`" `+phoneKey2+" matchblox:phone:Other" {
		t.Fatalf("other lines changed:\n%s", authKeys(home))
	}
	pre, post := `restrict,pty,command="'/opt/mb/matchblox' gate phone `, `" `+phoneKey+" matchblox:phone:Desk"
	id, ok := strings.CutPrefix(lines[1], pre)
	if !ok || !strings.HasSuffix(id, post) || !ValidPairID(strings.TrimSuffix(id, post)) {
		t.Fatalf("the old line did not get an id: %s", lines[1])
	}
	ds, err := Devices(home)
	if err != nil || ds[0].ID != strings.TrimSuffix(id, post) || ds[0].Name != "Desk" {
		t.Fatalf("devices = %+v, %v", ds, err)
	}
	before := authKeys(home)
	if n, err := UpgradePhoneLines(home); err != nil || n != 0 || authKeys(home) != before {
		t.Fatalf("a second upgrade = %d, %v and changed:\n%s", n, err, authKeys(home))
	}
	// No file, nothing to do.
	if n, err := UpgradePhoneLines(t.TempDir()); err != nil || n != 0 {
		t.Fatalf("no keys = %d, %v", n, err)
	}
}

func TestRegistrationIsKeyedByTheDeviceID(t *testing.T) {
	home := homeWithPhones(t)
	if err := Register(home, reg(regID, "tok")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".ssh", ".matchblox-push-"+regID)
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("registration file = %v, %v", fi, err)
	}
	rs, err := Registrations(home)
	if err != nil || len(rs) != 1 || rs[0].Device != regID || rs[0].DeviceToken != "tok" {
		t.Fatalf("registrations = %+v, %v", rs, err)
	}
	if err := Unregister(home, regID); err != nil {
		t.Fatal(err)
	}
	if rs, _ := Registrations(home); len(rs) != 0 {
		t.Fatalf("after unregister: %+v", rs)
	}
}

func TestRegisterKeepsOneRegistrationPerPhone(t *testing.T) {
	home := homeWithPhones(t)
	for _, r := range []push.Registration{reg(regID, "one"), reg(regID2, "other"), reg(regID, "two")} {
		if err := Register(home, r); err != nil {
			t.Fatal(err)
		}
	}
	rs, err := Registrations(home)
	if err != nil || len(rs) != 2 {
		t.Fatalf("registrations = %+v, %v", rs, err)
	}
	got := map[string]string{}
	for _, r := range rs {
		got[r.Device] = r.DeviceToken
	}
	if got[regID] != "two" || got[regID2] != "other" {
		t.Fatalf("registrations = %v", got)
	}
}

func TestRegisterRefusesAnIDWithoutADeviceLine(t *testing.T) {
	home := homeWithPhones(t)
	for _, id := range []string{"aaaaaaaaaaaa", "", "../../x", "0123456789AB"} {
		if err := Register(home, reg(id, "tok")); err == nil {
			t.Errorf("Register(%q) took it", id)
		}
	}
	// A pairing line with the id is not a device.
	writeFile(t, filepath.Join(home, ".ssh", "authorized_keys"),
		`restrict,expiry-time="202610211200",command="'/opt/mb/matchblox' gate pair `+regID+`" `+pub+" matchblox:pair\n")
	if err := Register(home, reg(regID, "tok")); err == nil {
		t.Error("a pairing key's id registered")
	}
	if rs, _ := Registrations(home); len(rs) != 0 {
		t.Fatalf("registrations = %+v", rs)
	}
}

func TestRemovingADeviceRemovesItsRegistration(t *testing.T) {
	home := homeWithPhones(t)
	for _, id := range []string{regID, regID2} {
		if err := Register(home, reg(id, "tok")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := RemoveDevice(home, "Desk"); err != nil {
		t.Fatal(err)
	}
	rs, err := Registrations(home)
	if err != nil || len(rs) != 1 || rs[0].Device != regID2 {
		t.Fatalf("registrations = %+v, %v", rs, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", ".matchblox-push-"+regID)); !os.IsNotExist(err) {
		t.Fatalf("the registration file of the removed phone: %v", err)
	}
}

// A phone taken out of authorized_keys by hand is pushed to no more.
func TestRegistrationsSkipAPhoneWithoutItsLine(t *testing.T) {
	home := homeWithPhones(t)
	if err := Register(home, reg(regID, "tok")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, ".ssh", "authorized_keys"),
		`restrict,pty,command="'/opt/mb/matchblox' gate phone `+regID2+`" `+phoneKey2+" matchblox:phone:Other\n")
	if rs, err := Registrations(home); err != nil || len(rs) != 0 {
		t.Fatalf("registrations = %+v, %v", rs, err)
	}
}
