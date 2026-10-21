package setup

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/blox-eng/matchblox/internal/doors"
)

// unitEnv is env for goos with a fake runner: nothing here calls launchctl
// or systemctl, and every file is under a temp dir.
func unitEnv(t *testing.T, goos string) (Env, *[][]string) {
	t.Helper()
	e := env(t)
	e.GOOS, e.UID = goos, 501
	ran := new([][]string)
	e.Run = func(argv []string) error { *ran = append(*ran, argv); return nil }
	return e, ran
}

func TestTheKeepRunningDoorWritesAPlistOnMacOS(t *testing.T) {
	e, ran := unitEnv(t, "darwin")
	d := door(t, e, doors.KeepRunning)
	plist := filepath.Join(e.Home, "Library", "LaunchAgents", "com.blox.matchblox.plist")
	if !d.Open() || d.Title != "Keep matchblox running" || d.Why != "the phone buzzes with no console open" || d.Path != plist {
		t.Fatalf("door = %+v", d)
	}
	if _, err := Open(e, doors.KeepRunning, d.Sum, false); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(plist)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<string>com.blox.matchblox</string>",
		"<key>ProgramArguments</key>\n\t<array>\n\t\t<string>" + exe + "</string>\n\t\t<string>serve</string>\n\t</array>",
		"<key>RunAtLoad</key>\n\t<true/>",
		"<key>KeepAlive</key>\n\t<true/>",
		"<key>StandardErrorPath</key>\n\t<string>" + filepath.Join(e.StateDir, "serve.log") + "</string>",
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("plist lacks %q:\n%s", want, b)
		}
	}
	if want := [][]string{{"launchctl", "bootstrap", "gui/501", plist}}; !slices.EqualFunc(*ran, want, slices.Equal) {
		t.Fatalf("ran = %q, want %q", *ran, want)
	}
}

func TestTheKeepRunningDoorWritesAUnitOnLinux(t *testing.T) {
	e, ran := unitEnv(t, "linux")
	d := door(t, e, doors.KeepRunning)
	unit := filepath.Join(e.Home, ".config", "systemd", "user", "matchblox.service")
	if !d.Open() || d.Path != unit {
		t.Fatalf("door = %+v", d)
	}
	if !strings.Contains(d.Preview, "loginctl enable-linger") {
		t.Fatalf("no linger advice in the preview:\n%s", d.Preview)
	}
	if _, err := Open(e, doors.KeepRunning, d.Sum, false); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(unit)
	for _, want := range []string{"ExecStart=" + exe + " serve\n", "Restart=on-failure\n", "WantedBy=default.target\n"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("unit lacks %q:\n%s", want, b)
		}
	}
	if want := [][]string{{"systemctl", "--user", "enable", "--now", "matchblox.service"}}; !slices.EqualFunc(*ran, want, slices.Equal) {
		t.Fatalf("ran = %q, want %q", *ran, want)
	}
}

func TestTheDoorIsDoneWhenTheUnitIsThere(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		e, _ := unitEnv(t, goos)
		d := door(t, e, doors.KeepRunning)
		if _, err := Open(e, doors.KeepRunning, d.Sum, false); err != nil {
			t.Fatal(err)
		}
		if d := door(t, e, doors.KeepRunning); !d.Done {
			t.Fatalf("%s: door after = %+v", goos, d)
		}
		// A unit that holds other content is not done, and the preview shows the change.
		if err := os.WriteFile(door(t, e, doors.KeepRunning).Path, []byte("other\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if d := door(t, e, doors.KeepRunning); d.Done || !strings.Contains(d.Preview, "-other") {
			t.Fatalf("%s: door with other content = %+v", goos, d)
		}
	}
}

func TestTheDoorRefusesAFileThatChangedAfterThePreview(t *testing.T) {
	e, ran := unitEnv(t, "linux")
	d := door(t, e, doors.KeepRunning)
	if err := os.MkdirAll(filepath.Dir(d.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.Path, []byte("mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(e, doors.KeepRunning, d.Sum, false); !errors.Is(err, ErrChanged) || len(*ran) != 0 {
		t.Fatalf("err = %v, ran = %q", err, *ran)
	}
}

func TestTheDoorShowsItsCommand(t *testing.T) {
	e, _ := unitEnv(t, "darwin")
	d := door(t, e, doors.KeepRunning)
	plist := d.Path
	if want := []string{"launchctl", "bootstrap", "gui/501", plist}; !slices.Equal(d.Term, want) || !strings.Contains(d.Preview, "launchctl bootstrap gui/501 "+plist) {
		t.Fatalf("darwin Term = %q, Preview:\n%s", d.Term, d.Preview)
	}
	e, _ = unitEnv(t, "linux")
	d = door(t, e, doors.KeepRunning)
	if want := []string{"systemctl", "--user", "enable", "--now", "matchblox.service"}; !slices.Equal(d.Term, want) || !strings.Contains(d.Preview, "systemctl --user enable --now matchblox.service") {
		t.Fatalf("linux Term = %q, Preview:\n%s", d.Term, d.Preview)
	}
}

func TestNoKeepRunningDoorOnOtherSystems(t *testing.T) {
	e, _ := unitEnv(t, "windows")
	for _, d := range Doors(e) {
		if d.ID == doors.KeepRunning {
			t.Fatalf("door on windows: %+v", d)
		}
	}
}
