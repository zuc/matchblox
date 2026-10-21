package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blox-eng/matchblox/internal/push"
	"github.com/blox-eng/matchblox/internal/setup"
)

const (
	laptopKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIM4bfwfUcmyCX2tnMZ4mHkeEjSRJnkf0ehLEuB8iWLHU"
	phoneKey1 = "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBH7iriHk4uoQ5QFqWYraWlOwFNM2wikZzsWVilC6boso/FQAq1zahWnKxRjYJ2Z/EOtOHrgqLqjFZsrrjAmjJQA="
	phoneKey2 = "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBMvaEjNCnphfEsogXCHt0gNKw4Ek7XjbU/1H71OWJvVrSjYFsg1HSGRIpqX+VP2kHH5WQFlMn6djJG65WRbo/ME="
)

func homeWithDevices(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if _, err := setup.Authorize(home, testExe, laptopKey+" matchblox"); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.AuthorizeDevice(home, testExe, phoneKey1, "Jordan's iPhone"); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.AuthorizeDevice(home, testExe, phoneKey2, "iPad"); err != nil {
		t.Fatal(err)
	}
	return home
}

func fp(t *testing.T, key string) string {
	t.Helper()
	f, err := setup.Fingerprint(key)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestDevicesListAndRemove(t *testing.T) {
	home := homeWithDevices(t)
	var out bytes.Buffer
	if err := devicesCmd(nil, home, &out); err != nil {
		t.Fatal(err)
	}
	want := "phone   Jordan's iPhone  " + fp(t, phoneKey1) + "  buzz off  island no\n" +
		"phone   iPad             " + fp(t, phoneKey2) + "  buzz off  island no\n" +
		"laptop  matchblox        " + fp(t, laptopKey) + "\n"
	if out.String() != want {
		t.Fatalf("list:\n%s\nwant:\n%s", out.String(), want)
	}

	realHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := devicesCmd([]string{"rm", "iPad"}, home, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "Removed iPad. Backup: "+filepath.Join(realHome, ".ssh")) {
		t.Fatalf("rm printed %q", out.String())
	}
	b, _ := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
	if strings.Contains(string(b), "matchblox:phone:iPad") || !strings.Contains(string(b), "Jordan's iPhone") {
		t.Fatalf("authorized_keys after rm:\n%s", b)
	}

	out.Reset()
	if err := devicesCmd([]string{"rm", "matchblox"}, home, &out); err != nil {
		t.Fatalf("rm laptop by name: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys")); strings.Contains(string(b), laptopKey) {
		t.Fatalf("laptop still there:\n%s", b)
	}

	err = devicesCmd([]string{"rm", "nobody"}, home, &out)
	if err == nil || err.Error() != "no device named nobody; run matchblox devices and give a name or a fingerprint from that list" {
		t.Fatalf("rm unknown: %v", err)
	}
}

func TestDevicesRefusesWrongArguments(t *testing.T) {
	home := homeWithDevices(t)
	for _, args := range [][]string{{"rm"}, {"--x"}, {"list"}} {
		if err := devicesCmd(args, home, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "usage: matchblox devices") {
			t.Errorf("%v: %v", args, err)
		}
	}
}

// A name with a space works unquoted: matchblox devices rm iPad 2.
func TestDevicesRemoveANameWithASpace(t *testing.T) {
	home := t.TempDir()
	if _, err := setup.AuthorizeDevice(home, testExe, phoneKey1, "iPad"); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.AuthorizeDevice(home, testExe, phoneKey2, "iPad 2"); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := devicesCmd([]string{"rm", "iPad", "2"}, home, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "Removed iPad 2. Backup: ") {
		t.Fatalf("rm printed %q", out.String())
	}
	if ds, _ := setup.Devices(home); len(ds) != 1 || ds[0].Name != "iPad" {
		t.Fatalf("devices after rm: %+v", ds)
	}
}

func TestDevicesWithoutKeysSaysHowToAddOne(t *testing.T) {
	var out bytes.Buffer
	if err := devicesCmd(nil, t.TempDir(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "matchblox pair") {
		t.Fatalf("empty list: %q", out.String())
	}
}

// Each phone shows whether it buzzes and whether it carries the island; a
// phone that never registered does neither.
func TestDevicesListShowsPushAndIsland(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	const id1, id2 = "0123456789ab", "ba9876543210"
	keys := `restrict,pty,command="'` + testExe + `' gate phone ` + id1 + `" ` + phoneKey1 + " matchblox:phone:Desk\n" +
		`restrict,pty,command="'` + testExe + `' gate phone ` + id2 + `" ` + phoneKey2 + " matchblox:phone:iPad\n"
	if err := os.WriteFile(filepath.Join(home, ".ssh", "authorized_keys"), []byte(keys), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := setup.Authorize(home, testExe, laptopKey+" matchblox"); err != nil {
		t.Fatal(err)
	}
	r := push.Registration{Device: id1, RegisterRequest: push.RegisterRequest{DeviceToken: "tok", Buzz: true,
		Island: &push.IslandReg{StartToken: "s", Channel: "c"}}}
	if err := setup.Register(home, r); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := devicesCmd(nil, home, &out); err != nil {
		t.Fatal(err)
	}
	want := "phone   Desk       " + fp(t, phoneKey1) + "  buzz on   island yes\n" +
		"phone   iPad       " + fp(t, phoneKey2) + "  buzz off  island no\n" +
		"laptop  matchblox  " + fp(t, laptopKey) + "\n"
	if out.String() != want {
		t.Fatalf("list:\n%s\nwant:\n%s", out.String(), want)
	}
}
