package setup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	phoneKey  = "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBH7iriHk4uoQ5QFqWYraWlOwFNM2wikZzsWVilC6boso/FQAq1zahWnKxRjYJ2Z/EOtOHrgqLqjFZsrrjAmjJQA="
	phoneKey2 = "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBMvaEjNCnphfEsogXCHt0gNKw4Ek7XjbU/1H71OWJvVrSjYFsg1HSGRIpqX+VP2kHH5WQFlMn6djJG65WRbo/ME="
)

type pairFixture struct {
	Names struct {
		Good, Bad []string
	} `json:"names"`
	HostkeyPub string `json:"hostkey_pub"`
	HostkeyFp  string `json:"hostkey_fp"`
}

func loadPairFixture(t *testing.T) pairFixture {
	t.Helper()
	b, err := os.ReadFile("../../testdata/pair/v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var f pairFixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func authKeys(home string) string {
	b, _ := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
	return string(b)
}

func TestValidDeviceName(t *testing.T) {
	f := loadPairFixture(t)
	for _, s := range f.Names.Good {
		if !ValidDeviceName(s) {
			t.Errorf("%q refused, want accepted", s)
		}
	}
	for _, s := range f.Names.Bad {
		if ValidDeviceName(s) {
			t.Errorf("%q accepted, want refused", s)
		}
	}
	if ValidDeviceName("trail ") {
		t.Error("trailing space accepted")
	}
}

func TestAuthorizeDeviceWritesTheGateLine(t *testing.T) {
	home := t.TempDir()
	if _, err := AuthorizeDevice(home, "/usr/local/bin/matchblox", phoneKey, "Jordan’s iPhone"); err != nil {
		t.Fatal(err)
	}
	want := `restrict,pty,command="'/usr/local/bin/matchblox' gate phone" ` + phoneKey + " matchblox:phone:Jordan’s iPhone\n"
	if got := authKeys(home); got != want {
		t.Fatalf("authorized_keys:\n%s\nwant:\n%s", got, want)
	}
}

func TestAuthorizeDeviceRefuses(t *testing.T) {
	home := t.TempDir()
	cases := map[string][3]string{
		"ed25519 key": {"/opt/mb/matchblox", pub, "iPhone"},
		"bad name":    {"/opt/mb/matchblox", phoneKey, "a\"b"},
		"bad path":    {"/opt/mb/it's", phoneKey, "iPhone"},
	}
	for name, c := range cases {
		if _, err := AuthorizeDevice(home, c[0], c[1], c[2]); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if got := authKeys(home); got != "" {
		t.Fatalf("authorized_keys written: %q", got)
	}
}

func TestAuthorizeDeviceKeepsOtherLines(t *testing.T) {
	home := t.TempDir()
	laptop := `restrict,pty,command="'/opt/mb/matchblox' gate" ` + pub
	old := "ssh-rsa AAAAother other@box\n" + laptop + "\nssh-rsa AAAAlast last@box"
	writeFile(t, filepath.Join(home, ".ssh", "authorized_keys"), old)
	backup, err := AuthorizeDevice(home, "/opt/mb/matchblox", phoneKey, "iPhone")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(backup); string(b) != old {
		t.Fatalf("backup %q: %q", backup, b)
	}
	want := old + "\n" + `restrict,pty,command="'/opt/mb/matchblox' gate phone" ` + phoneKey + " matchblox:phone:iPhone\n"
	if got := authKeys(home); got != want {
		t.Fatalf("authorized_keys:\n%s\nwant:\n%s", got, want)
	}
}

func TestAuthorizeDeviceSameKeyReplacesItsLine(t *testing.T) {
	home := t.TempDir()
	for _, c := range [][2]string{{"/old/matchblox", "iPhone"}, {"/new/matchblox", "Desk-2"}} {
		if _, err := AuthorizeDevice(home, c[0], phoneKey, c[1]); err != nil {
			t.Fatal(err)
		}
	}
	want := `restrict,pty,command="'/new/matchblox' gate phone" ` + phoneKey + " matchblox:phone:Desk-2\n"
	if got := authKeys(home); got != want {
		t.Fatalf("authorized_keys:\n%s\nwant:\n%s", got, want)
	}
	if backup, err := AuthorizeDevice(home, "/new/matchblox", phoneKey, "Desk-2"); err != nil || backup != "" {
		t.Fatalf("unchanged file: backup %q, err %v", backup, err)
	}
}

func TestAuthorizeDeviceThroughASymlink(t *testing.T) {
	home := t.TempDir()
	real := filepath.Join(home, "dotfiles", "authorized_keys")
	writeFile(t, real, "ssh-rsa AAAAother other@box\n")
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, ".ssh", "authorized_keys")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := AuthorizeDevice(home, "/opt/mb/matchblox", phoneKey, "iPhone"); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("link replaced: %v %v", fi, err)
	}
	if b, _ := os.ReadFile(real); !strings.HasPrefix(string(b), "ssh-rsa AAAAother other@box\n") || !strings.Contains(string(b), "matchblox:phone:iPhone") {
		t.Fatalf("target: %q", b)
	}
}

func TestDevicesAndRemoveDevice(t *testing.T) {
	home := t.TempDir()
	laptopKey := loadPairFixture(t).HostkeyPub
	laptop := `restrict,pty,command="'/opt/mb/matchblox' gate" ` + laptopKey + " matchblox"
	other := "ssh-rsa AAAAother other@box"
	pair := `restrict,expiry-time="202610141200Z",command="'/opt/mb/matchblox' gate pair abc" ` + pub + " matchblox:pair"
	writeFile(t, filepath.Join(home, ".ssh", "authorized_keys"), other+"\n"+laptop+"\n"+pair+"\n")
	for name, key := range map[string]string{"Jordan’s iPhone": phoneKey, "Desk-2": phoneKey2} {
		if _, err := AuthorizeDevice(home, "/opt/mb/matchblox", key, name); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Devices(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Kind != "laptop" || got[1].Kind != "phone" || got[2].Kind != "phone" {
		t.Fatalf("devices: %+v", got)
	}
	names := []string{got[1].Name, got[2].Name}
	if !slices.Contains(names, "Jordan’s iPhone") || !slices.Contains(names, "Desk-2") {
		t.Fatalf("phone names: %v", names)
	}
	fp, err := Fingerprint(laptopKey)
	if err != nil || got[0].Fingerprint != fp {
		t.Fatalf("laptop fingerprint %q, want %q (%v)", got[0].Fingerprint, fp, err)
	}

	backup, err := RemoveDevice(home, "Jordan’s iPhone")
	if err != nil || backup == "" {
		t.Fatalf("remove: backup %q, err %v", backup, err)
	}
	if left := authKeys(home); strings.Contains(left, phoneKey) || !strings.Contains(left, other) || !strings.Contains(left, pair) || !strings.Contains(left, phoneKey2) {
		t.Fatalf("after remove:\n%s", left)
	}
	if got, _ = Devices(home); len(got) != 2 {
		t.Fatalf("devices after remove: %+v", got)
	}
	if _, err := RemoveDevice(home, "Nobody"); err == nil || err.Error() != "no device named Nobody; run matchblox devices and give a name or a fingerprint from that list" {
		t.Fatalf("unknown name: %v", err)
	}
}

func TestRemoveDeviceByLaptopName(t *testing.T) {
	home := t.TempDir()
	f := loadPairFixture(t)
	if _, err := Authorize(home, "/opt/mb/matchblox", f.HostkeyPub+" matchblox"); err != nil {
		t.Fatal(err)
	}
	if _, err := AuthorizeDevice(home, "/opt/mb/matchblox", phoneKey, "iPhone"); err != nil {
		t.Fatal(err)
	}
	if got, _ := Devices(home); len(got) != 2 || got[0].Name != "matchblox" || got[0].Kind != "laptop" {
		t.Fatalf("devices: %+v", got)
	}
	if _, err := RemoveDevice(home, "matchblox"); err != nil {
		t.Fatal(err)
	}
	if got := authKeys(home); strings.Contains(got, f.HostkeyPub) || !strings.Contains(got, phoneKey) {
		t.Fatalf("authorized_keys:\n%s", got)
	}
}

func TestRemoveDeviceLaptopsWithTheSameNameNeedTheFingerprint(t *testing.T) {
	home := t.TempDir()
	if _, err := Authorize(home, "/opt/mb/matchblox", loadPairFixture(t).HostkeyPub+" matchblox"); err != nil {
		t.Fatal(err)
	}
	if _, err := Authorize(home, "/opt/mb/matchblox", pairPub+" matchblox"); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveDevice(home, "matchblox"); err == nil || err.Error() != "2 devices named matchblox; give the fingerprint" {
		t.Fatalf("repeated laptop name: %v", err)
	}
}

func TestRemoveDeviceByFingerprintWhenNamesRepeat(t *testing.T) {
	home := t.TempDir()
	for _, key := range []string{phoneKey, phoneKey2} {
		if _, err := AuthorizeDevice(home, "/opt/mb/matchblox", key, "iPhone"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := RemoveDevice(home, "iPhone"); err == nil || err.Error() != "2 devices named iPhone; give the fingerprint" {
		t.Fatalf("repeated name: %v", err)
	}
	fp, err := Fingerprint(phoneKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveDevice(home, fp); err != nil {
		t.Fatal(err)
	}
	if got := authKeys(home); strings.Contains(got, phoneKey) || !strings.Contains(got, phoneKey2) {
		t.Fatalf("authorized_keys:\n%s", got)
	}
}

func TestFingerprintMatchesSSHKeygen(t *testing.T) {
	f := loadPairFixture(t)
	got, err := Fingerprint(f.HostkeyPub)
	if err != nil || got != f.HostkeyFp {
		t.Fatalf("Fingerprint = %q, %v; want %q", got, err, f.HostkeyFp)
	}
	for _, bad := range []string{"", "x", "ssh-ed25519 !!!"} {
		if _, err := Fingerprint(bad); err == nil {
			t.Errorf("Fingerprint(%q): no error", bad)
		}
	}
}

const pairPub = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJuq8ePD+piatVSqgsicCgoOmr96M/08H8oC9fvzMWs9"

func useZone(t *testing.T, loc *time.Location) {
	t.Helper()
	old := expiryZone
	expiryZone = func() *time.Location { return loc }
	t.Cleanup(func() { expiryZone = old })
}

func TestExpiryTimeRoundsUpToTheMinute(t *testing.T) {
	sofia, err := time.LoadLocation("Europe/Sofia")
	if err != nil {
		t.Skip("no tzdata:", err)
	}
	for _, c := range []struct {
		until time.Time
		want  string
	}{
		{time.Date(2026, 10, 14, 13, 37, 0, 0, time.UTC), "202610141637"},
		{time.Date(2026, 10, 14, 13, 37, 1, 0, time.UTC), "202610141638"},
		{time.Date(2026, 10, 14, 13, 37, 59, 999, time.UTC), "202610141638"},
		{time.Date(2026, 10, 14, 23, 59, 30, 0, time.UTC), "202610150300"},
	} {
		if got := expiryTime(c.until, sofia); got != c.want {
			t.Errorf("expiryTime(%v) = %s, want %s", c.until, got, c.want)
		}
	}
}

func TestSystemZoneReadsTheZoneFile(t *testing.T) {
	src := "/usr/share/zoneinfo/Europe/Sofia"
	data, err := os.ReadFile(src)
	if err != nil {
		t.Skip("no tzdata:", err)
	}
	old := zoneFile
	t.Cleanup(func() { zoneFile = old })
	t.Setenv("TZ", "America/New_York")

	zoneFile = filepath.Join(t.TempDir(), "localtime")
	writeFile(t, zoneFile, string(data))
	if got := expiryTime(time.Date(2026, 1, 14, 12, 0, 0, 0, time.UTC), systemZone()); got != "202601141400" {
		t.Errorf("winter in the file zone: %s", got)
	}
	zoneFile = filepath.Join(t.TempDir(), "missing")
	if got := expiryTime(time.Date(2026, 1, 14, 12, 0, 0, 0, time.UTC), systemZone()); got != "202601141200" {
		t.Errorf("no file, want UTC: %s", got)
	}
	zoneFile = filepath.Join(t.TempDir(), "junk")
	writeFile(t, zoneFile, "not a zone")
	if got := systemZone(); got != time.UTC {
		t.Errorf("bad file, want UTC: %v", got)
	}
}

func TestAddPairKeyLine(t *testing.T) {
	home := t.TempDir()
	useZone(t, time.FixedZone("EEST", 3*3600))
	until := time.Date(2026, 10, 14, 16, 37, 20, 0, time.FixedZone("EEST", 3*3600))
	if err := AddPairKey(home, "/usr/local/bin/matchblox", pairPub, "0123456789ab", until); err != nil {
		t.Fatal(err)
	}
	want := `restrict,expiry-time="202610141638",command="'/usr/local/bin/matchblox' gate pair 0123456789ab" ` + pairPub + " matchblox:pair\n"
	if got := authKeys(home); got != want {
		t.Fatalf("line:\n%s\nwant:\n%s", got, want)
	}
	if ok, err := PairKeyPresent(home, "0123456789ab"); err != nil || !ok {
		t.Fatalf("present = %v, %v", ok, err)
	}
	if ok, _ := PairKeyPresent(home, "ba9876543210"); ok {
		t.Fatal("another id present")
	}
	if got, _ := Devices(home); len(got) != 0 {
		t.Fatalf("pair line listed as device: %+v", got)
	}
}

func TestAddPairKeyRefuses(t *testing.T) {
	home := t.TempDir()
	until := time.Now()
	for name, c := range map[string]struct{ exe, key, id string }{
		"short id":    {"/opt/mb/matchblox", pairPub, "0123"},
		"upper id":    {"/opt/mb/matchblox", pairPub, "0123456789AB"},
		"injected id": {"/opt/mb/matchblox", pairPub, `0123456789a" `},
		"quote exe":   {`/opt/mb/m"x`, pairPub, "0123456789ab"},
		"quote exe 2": {`/opt/mb/m'x`, pairPub, "0123456789ab"},
		"relative":    {"matchblox", pairPub, "0123456789ab"},
		"p256 key":    {"/opt/mb/matchblox", phoneKey, "0123456789ab"},
	} {
		if err := AddPairKey(home, c.exe, c.key, c.id, until); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if got := authKeys(home); got != "" {
		t.Fatalf("file written:\n%s", got)
	}
}

func TestRemovePairKey(t *testing.T) {
	home := t.TempDir()
	until := time.Date(2026, 10, 14, 14, 37, 0, 0, time.UTC)
	other := "ssh-rsa AAAAB3NzaC1yc2E other@host"
	keep2 := `restrict,expiry-time="209910141437Z",command="'/opt/mb/matchblox' gate pair ba9876543210" ` + "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOtherOtherOtherOtherOtherOtherOtherOtherOth matchblox:pair"
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "authorized_keys"), []byte(other+"\n"+keep2), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AddPairKey(home, "/usr/local/bin/matchblox", pairPub, "0123456789ab", until); err != nil {
		t.Fatal(err)
	}
	removed, err := RemovePairKey(home, "0123456789ab")
	if err != nil || !removed {
		t.Fatalf("remove = %v, %v", removed, err)
	}
	if got := authKeys(home); got != other+"\n"+keep2+"\n" {
		t.Fatalf("after remove:\n%q", got)
	}
	if removed, err = RemovePairKey(home, "0123456789ab"); err != nil || removed {
		t.Fatalf("second remove = %v, %v", removed, err)
	}
	if got := authKeys(home); got != other+"\n"+keep2+"\n" {
		t.Fatalf("after second remove:\n%q", got)
	}
}

func TestRemovePairKeyKeepsBytesWithoutFinalNewline(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	pair := `restrict,expiry-time="202610141437Z",command="'/opt/mb/matchblox' gate pair 0123456789ab" ` + pairPub + " matchblox:pair"
	in := "ssh-rsa AAAA a\n" + pair + "\nssh-rsa BBBB b"
	if err := os.WriteFile(filepath.Join(dir, "authorized_keys"), []byte(in), 0o600); err != nil {
		t.Fatal(err)
	}
	if removed, err := RemovePairKey(home, "0123456789ab"); err != nil || !removed {
		t.Fatalf("remove = %v, %v", removed, err)
	}
	if got := authKeys(home); got != "ssh-rsa AAAA a\nssh-rsa BBBB b" {
		t.Fatalf("got %q", got)
	}
	if _, err := RemovePairKey(home, "nothex"); err == nil {
		t.Fatal("bad id: no error")
	}
}

func TestDevicesReadsCRLFLines(t *testing.T) {
	home := t.TempDir()
	if _, err := AuthorizeDevice(home, "/opt/mb/matchblox", phoneKey, "iPhone"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".ssh", "authorized_keys")
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(authKeys(home), "\n", "\r\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := Devices(home); err != nil || len(got) != 1 || got[0].Name != "iPhone" {
		t.Fatalf("devices: %+v, %v", got, err)
	}
	if _, err := RemoveDevice(home, "iPhone"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(authKeys(home), phoneKey) {
		t.Fatalf("the phone is still there:\n%s", authKeys(home))
	}
}

func TestRemoveDeviceRefusesAnEmptyName(t *testing.T) {
	home := t.TempDir()
	f := loadPairFixture(t)
	if _, err := Authorize(home, "/opt/mb/matchblox", f.HostkeyPub); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveDevice(home, ""); err == nil {
		t.Fatal("an empty name removed a device")
	}
	if !strings.Contains(authKeys(home), f.HostkeyPub) {
		t.Fatalf("the laptop is gone:\n%s", authKeys(home))
	}
}

func TestClaimPairKeyWorksOnceUnderRace(t *testing.T) {
	home := t.TempDir()
	if err := AddPairKey(home, "/opt/mb/matchblox", pairPub, "0123456789ab", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for _, key := range []string{phoneKey, phoneKey2, phoneKey, phoneKey2, phoneKey, phoneKey2, phoneKey, phoneKey2} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := ClaimPairKey(home, "/opt/mb/matchblox", "0123456789ab", key, "iPhone")
			switch {
			case err == nil:
				wins.Add(1)
			case !errors.Is(err, ErrCodeUsed):
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("winners = %d, want 1", wins.Load())
	}
	if got, _ := Devices(home); len(got) != 1 || strings.Contains(authKeys(home), "gate pair") {
		t.Fatalf("devices %+v, file:\n%s", got, authKeys(home))
	}
}

func TestClaimPairKeyRefusesAKeyThatOpensALoginAndKeepsTheCode(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".ssh", "authorized_keys")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(phoneKey+" me@laptop\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AddPairKey(home, "/opt/mb/matchblox", pairPub, "0123456789ab", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimPairKey(home, "/opt/mb/matchblox", "0123456789ab", phoneKey, "iPhone"); !errors.Is(err, ErrKeyInUse) {
		t.Fatalf("err = %v, want ErrKeyInUse", err)
	}
	if _, err := AuthorizeDevice(home, "/opt/mb/matchblox", phoneKey, "iPhone"); !errors.Is(err, ErrKeyInUse) {
		t.Fatalf("AuthorizeDevice err = %v, want ErrKeyInUse", err)
	}
	if got := authKeys(home); !strings.HasPrefix(got, phoneKey+" me@laptop\n") || !strings.Contains(got, "gate pair") {
		t.Fatalf("file:\n%s", got)
	}
}

func TestPairingKeepsOneBackup(t *testing.T) {
	home := t.TempDir()
	if _, err := AuthorizeDevice(home, "/opt/mb/matchblox", phoneKey2, "iPad"); err != nil {
		t.Fatal(err)
	}
	baks := func() int {
		m, _ := filepath.Glob(filepath.Join(home, ".ssh", "authorized_keys.matchblox-*.bak"))
		return len(m)
	}
	n := baks()
	if err := AddPairKey(home, "/opt/mb/matchblox", pairPub, "0123456789ab", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimPairKey(home, "/opt/mb/matchblox", "0123456789ab", phoneKey, "iPhone"); err != nil {
		t.Fatal(err)
	}
	if got := baks() - n; got != 1 {
		t.Fatalf("a pairing made %d backups, want 1", got)
	}
}

func TestRemoveDeviceNeverTakesAPairLine(t *testing.T) {
	home := t.TempDir()
	if err := AddPairKey(home, "/opt/mb/matchblox", pairPub, "0123456789ab", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	fp, _ := Fingerprint(pairPub)
	for _, name := range []string{"matchblox:pair", "pair", fp} {
		if _, err := RemoveDevice(home, name); err == nil {
			t.Errorf("RemoveDevice(%q) took the pair line", name)
		}
	}
	if !strings.Contains(authKeys(home), "gate pair") {
		t.Fatal("the pair line is gone")
	}
}

func TestRewriteKeysKeepsALineAnotherProgramAdds(t *testing.T) {
	home := t.TempDir()
	if _, err := AuthorizeDevice(home, "/opt/mb/matchblox", phoneKey2, "iPad"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".ssh", "authorized_keys")
	const other = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIM4bfwfUcmyCX2tnMZ4mHkeEjSRJnkf0ehLEuB8iWLHU copied@id\n"
	once := sync.Once{}
	old := beforeReplace
	beforeReplace = func() { // ssh-copy-id appends while matchblox writes
		once.Do(func() {
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				t.Error(err)
				return
			}
			_, _ = f.WriteString(other)
			_ = f.Close()
		})
	}
	t.Cleanup(func() { beforeReplace = old })
	if _, err := AuthorizeDevice(home, "/opt/mb/matchblox", phoneKey, "iPhone"); err != nil {
		t.Fatal(err)
	}
	got := authKeys(home)
	if !strings.Contains(got, other) || !strings.Contains(got, phoneKey) || !strings.Contains(got, phoneKey2) {
		t.Fatalf("a line went:\n%s", got)
	}
}

func TestPhoneKeyInUse(t *testing.T) {
	body := strings.Fields(phoneKey)[1]
	for name, c := range map[string]struct {
		line  string
		inUse bool
	}{
		"login":            {phoneKey + " me@laptop\n", true},
		"login, tab":       {phoneKey + "\tme@laptop\n", true},
		"login, CRLF":      {phoneKey + " me\r\n", true},
		"login, no note":   {phoneKey + "\n", true},
		"login, options":   {`from="10.0.0.1" ` + phoneKey + " me\n", true},
		"laptop gate line": {`restrict,pty,command="'/opt/mb/matchblox' gate" ` + phoneKey + " matchblox\n", true},
		"commented out":    {"# " + phoneKey + " old\n", false},
		"same phone":       {`restrict,pty,command="'/opt/mb/matchblox' gate phone" ` + phoneKey + " matchblox:phone:iPhone\n", false},
		"other key":        {phoneKey2 + " me\n", false},
	} {
		t.Run(name, func(t *testing.T) {
			next, err := withPhone([]byte(c.line), "/opt/mb/matchblox", phoneKey, "iPhone", "")
			if c.inUse {
				if !errors.Is(err, ErrKeyInUse) {
					t.Fatalf("err = %v, want ErrKeyInUse", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if n := strings.Count(string(next), body); n != 1 {
				t.Fatalf("the key is in %d lines:\n%s", n, next)
			}
		})
	}
}

func TestRewriteKeysGivesUpOnAFileThatKeepsChanging(t *testing.T) {
	home := t.TempDir()
	if _, err := AuthorizeDevice(home, "/opt/mb/matchblox", phoneKey2, "iPad"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".ssh", "authorized_keys")
	old := beforeReplace
	beforeReplace = func() {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = f.WriteString("# another program\n")
			_ = f.Close()
		}
	}
	t.Cleanup(func() { beforeReplace = old })
	if _, err := AuthorizeDevice(home, "/opt/mb/matchblox", phoneKey, "iPhone"); err == nil {
		t.Fatal("no error for a file that keeps changing")
	}
	if got := authKeys(home); strings.Contains(got, phoneKey) || strings.Count(got, "# another program") != 3 {
		t.Fatalf("file:\n%s", got)
	}
}

func TestTakeClaimNamesThePhoneOfTheIDOnce(t *testing.T) {
	home := t.TempDir()
	for id, key := range map[string]string{"0123456789ab": pairPub, "ba9876543210": loadPairFixture(t).HostkeyPub} {
		if err := AddPairKey(home, "/opt/mb/matchblox", key, id, time.Now().Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ClaimPairKey(home, "/opt/mb/matchblox", "0123456789ab", phoneKey, "iPhone"); err != nil {
		t.Fatal(err)
	}
	if _, ok := TakeClaim(home, "ba9876543210"); ok {
		t.Fatal("a claim for an id that no phone used")
	}
	fp, _ := Fingerprint(phoneKey)
	if d, ok := TakeClaim(home, "0123456789ab"); !ok || d != (Device{Kind: "phone", Name: "iPhone", Fingerprint: fp, ID: "0123456789ab"}) {
		t.Fatalf("claim = %+v, %v", d, ok)
	}
	if _, ok := TakeClaim(home, "0123456789ab"); ok {
		t.Fatal("the claim note is still there")
	}
}

func TestAFailedClaimKeepsTheNoteOfTheClaimThatWorked(t *testing.T) {
	home := t.TempDir()
	if err := AddPairKey(home, "/opt/mb/matchblox", pairPub, "0123456789ab", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimPairKey(home, "/opt/mb/matchblox", "0123456789ab", phoneKey, "Stranger"); err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimPairKey(home, "/opt/mb/matchblox", "0123456789ab", phoneKey2, "iPhone"); !errors.Is(err, ErrCodeUsed) {
		t.Fatalf("second claim: %v, want ErrCodeUsed", err)
	}
	if d, ok := TakeClaim(home, "0123456789ab"); !ok || d.Name != "Stranger" {
		t.Fatalf("claim = %+v, %v; want the phone that paired", d, ok)
	}
}

func TestClaimPairKeyRefusesABadID(t *testing.T) {
	if _, err := ClaimPairKey(t.TempDir(), "/opt/mb/matchblox", "x/../../a", phoneKey, "iPhone"); err == nil {
		t.Fatal("a bad id was accepted")
	}
}

func claimThree(t *testing.T, home string) []Device {
	t.Helper()
	phoneKey3 := "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBGMt7fGXt2BCQ35mlNWthRvVka0x8TAjocrNXcf2JcvjHlmAI0vnJaVT793tsGTpVtb7V9XvMpzByQwfg6/H1Yo="
	keys := []string{phoneKey, phoneKey2, phoneKey3}
	ids := []string{"0123456789ab", "ba9876543210", "aaaaaaaaaaaa"}
	var out []Device
	for i, id := range ids {
		if err := AddPairKey(home, "/opt/mb/matchblox", pairPub, id, time.Now().Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		d, err := ClaimPairKey(home, "/opt/mb/matchblox", id, keys[i], "iPad")
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, d)
	}
	return out
}

func TestClaimWritesTheDeviceID(t *testing.T) {
	home := t.TempDir()
	if err := AddPairKey(home, "/opt/mb/matchblox", pairPub, "0123456789ab", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	d, err := ClaimPairKey(home, "/opt/mb/matchblox", "0123456789ab", phoneKey, "iPhone")
	if err != nil || d.ID != "0123456789ab" {
		t.Fatalf("claim = %+v, %v", d, err)
	}
	if !strings.Contains(authKeys(home), `gate phone 0123456789ab"`) {
		t.Fatalf("no id in the line:\n%s", authKeys(home))
	}
	got, err := Devices(home)
	if err != nil || len(got) != 1 || got[0].ID != "0123456789ab" || got[0].Kind != "phone" {
		t.Fatalf("devices = %+v, %v", got, err)
	}
}

func TestClaimMakesNamesUnique(t *testing.T) {
	home := t.TempDir()
	var names []string
	for _, d := range claimThree(t, home) {
		names = append(names, d.Name)
	}
	if !slices.Equal(names, []string{"iPad", "iPad 2", "iPad 3"}) {
		t.Fatalf("names = %v", names)
	}
	if d, ok := TakeClaim(home, "ba9876543210"); !ok || d.Name != "iPad 2" {
		t.Fatalf("claim note = %+v, %v; want the final name", d, ok)
	}
	if _, err := RemoveDevice(home, "iPad 2"); err != nil {
		t.Fatal(err)
	}
	got, _ := Devices(home)
	if len(got) != 2 || got[0].Name != "iPad" || got[1].Name != "iPad 3" {
		t.Fatalf("devices = %+v", got)
	}
}

func TestClaimKeepsALongNameWithinForty(t *testing.T) {
	home := t.TempDir()
	long := strings.Repeat("é", 40)
	for i, id := range []string{"0123456789ab", "ba9876543210"} {
		if err := AddPairKey(home, "/opt/mb/matchblox", pairPub, id, time.Now().Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		d, err := ClaimPairKey(home, "/opt/mb/matchblox", id, []string{phoneKey, phoneKey2}[i], long)
		if err != nil || !ValidDeviceName(d.Name) {
			t.Fatalf("claim %d = %+v, %v", i, d, err)
		}
		if i == 1 && d.Name != strings.Repeat("é", 38)+" 2" {
			t.Fatalf("name = %q", d.Name)
		}
	}
}

func TestDevicesReadsEveryLineKind(t *testing.T) {
	home := t.TempDir()
	laptopKey := loadPairFixture(t).HostkeyPub
	pre := `restrict,pty,command="'/opt/mb/matchblox' gate phone" ` + phoneKey + " " + phoneComment + "Old"
	nw := `restrict,pty,command="'/opt/mb/matchblox' gate phone 0123456789ab" ` + phoneKey2 + " " + phoneComment + "New"
	laptop := `restrict,pty,command="'/opt/mb/matchblox' gate" ` + laptopKey + " matchblox"
	pair := `restrict,expiry-time="202610141200",command="'/opt/mb/matchblox' gate pair abcdef012345" ` + pairPub + " matchblox:pair"
	writeFile(t, filepath.Join(home, ".ssh", "authorized_keys"), pre+"\n"+nw+"\n"+laptop+"\n"+pair+"\nssh-rsa AAAAother other@box\n")
	got, err := Devices(home)
	if err != nil || len(got) != 3 {
		t.Fatalf("devices = %+v, %v", got, err)
	}
	if got[0].Kind != "phone" || got[0].Name != "Old" || got[0].ID != "" ||
		got[1].Kind != "phone" || got[1].Name != "New" || got[1].ID != "0123456789ab" ||
		got[2].Kind != "laptop" || got[2].ID != "" {
		t.Fatalf("devices = %+v", got)
	}
}

func TestSeenAndDeviceNotes(t *testing.T) {
	home := t.TempDir()
	id := "0123456789ab"
	if seen, _ := PairSeen(home, id); seen {
		t.Fatal("seen before it was marked")
	}
	if err := MarkPairSeen(home, id, "iPad"); err != nil {
		t.Fatal(err)
	}
	if seen, name := PairSeen(home, id); !seen || name != "iPad" {
		t.Fatalf("seen = %v, %q", seen, name)
	}
	if fi, err := os.Stat(filepath.Join(home, ".ssh", ".matchblox-pair-"+id+".seen")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("seen note: %v, %v", fi, err)
	}
	RemovePairSeen(home, id)
	if seen, _ := PairSeen(home, id); seen {
		t.Fatal("seen after remove")
	}
	if err := MarkPairSeen(home, id, ""); err != nil {
		t.Fatal(err)
	}
	if seen, name := PairSeen(home, id); !seen || name != "" {
		t.Fatalf("seen = %v, %q", seen, name)
	}

	at := time.Date(2026, 10, 21, 9, 30, 0, 0, time.UTC)
	phoneLines(t, home, id, "ba9876543210")
	if _, _, ok := DeviceIn(home, id); ok {
		t.Fatal("in before it was marked")
	}
	if err := MarkDeviceIn(home, id, "/dev/ttys004", at); err != nil {
		t.Fatal(err)
	}
	if err := MarkDeviceIn(home, "ba9876543210", "/dev/ttys009", at); err != nil {
		t.Fatal(err)
	}
	gotAt, tty, ok := DeviceIn(home, id)
	if !ok || !gotAt.Equal(at) || tty != "/dev/ttys004" {
		t.Fatalf("in = %v, %q, %v", gotAt, tty, ok)
	}
	if m := DeviceTTYs(home); len(m) != 2 || m["/dev/ttys004"] != id || m["/dev/ttys009"] != "ba9876543210" {
		t.Fatalf("ttys = %v", m)
	}
	if fi, err := os.Stat(filepath.Join(home, ".ssh", ".matchblox-device-"+id)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("device note: %v, %v", fi, err)
	}
}
