package panes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/blox-eng/matchblox/internal/tmuxtest"
)

func TestHomeArgvOutsideTmux(t *testing.T) {
	got := HomeArgv(false, true, []string{"/usr/local/bin/matchblox", "--no-motion"})
	want := []string{"tmux", "-u", "new-session", "-A", "-s", "matchblox", "-e", "MATCHBLOX_HOME=1", "/usr/local/bin/matchblox", "--no-motion"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestHomeArgvInsideTmuxRunsInPlace(t *testing.T) {
	if got := HomeArgv(true, true, []string{"matchblox"}); got != nil {
		t.Fatalf("inside tmux the console runs in place, got %q", got)
	}
}

func TestHomeArgvWithoutTmuxRunsInPlace(t *testing.T) {
	if got := HomeArgv(false, false, []string{"matchblox"}); got != nil {
		t.Fatalf("without tmux the console runs in place, got %q", got)
	}
}

// tmux32 tells if tmux is installed at 3.2 or later: the way back uses
// status ranges, which 3.2 added.
func tmux32(t *testing.T) {
	t.Helper()
	out, err := exec.Command("tmux", "-V").Output()
	if err != nil {
		t.Skip("tmux is not installed")
	}
	var major, minor int
	if _, err := fmt.Sscanf(strings.TrimPrefix(strings.TrimSpace(string(out)), "tmux "), "%d.%d", &major, &minor); err != nil {
		t.Skipf("unknown tmux version %q", out)
	}
	if major < 3 || major == 3 && minor < 2 {
		t.Skipf("tmux %d.%d is older than 3.2", major, minor)
	}
}

// TestWayBackParses loads WayBack into a private tmux server. source-file
// prints a bad line on stderr and still exits 0, so the test reads stderr
// and the bindings the lines made.
func TestWayBackParses(t *testing.T) {
	tmux32(t)
	conf := filepath.Join(t.TempDir(), "wayback.conf")
	if err := os.WriteFile(conf, []byte(WayBack), 0o600); err != nil {
		t.Fatal(err)
	}
	sock := tmuxtest.Server(t)
	cmd := exec.Command("tmux", "-L", sock, "-f", os.DevNull, "new-session", "-d", "-s", "work", //nolint:gosec // test argv
		";", "source-file", conf,
		";", "source-file", conf, // people source their conf again after an edit
		";", "list-keys", "-T", "prefix", "m",
		";", "list-keys", "-T", "root", "MouseDown1Status",
		";", "show-options", "-g", "status-left",
		";", "show-options", "-g", "status-left-length",
		";", "kill-server")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil || stderr.Len() > 0 {
		t.Fatalf("tmux: %v\n%s", err, stderr.String())
	}
	// tmux 3.8 pads list-keys with a column of its own: "bind-key  -T".
	out = regexp.MustCompile(` {2,}`).ReplaceAll(out, []byte(" "))
	for _, want := range []string{
		"bind-key -T prefix m switch-client -t =matchblox",
		`"switch-client -t =matchblox"`,
		"#[range=user|matchblox] ◂ matchblox #[norange]",
		"status-left-length 40",
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("tmux lacks %q:\n%s", want, out)
		}
	}
	if n := strings.Count(string(out), "◂ matchblox"); n != 1 {
		t.Fatalf("sourced twice, the status line has %d labels:\n%s", n, out)
	}
}

func TestSizeArgv(t *testing.T) {
	want := []string{"tmux", "set-option", "-w", "-t", "=matchblox:", "window-size", "latest"}
	if got := SizeArgv(); !slices.Equal(got, want) {
		t.Errorf("SizeArgv() = %q, want %q", got, want)
	}
}

// TestHomeKeyMatchesTheApp reads what the phone sends and taps from the
// fixture the Swift tests read too.
func TestHomeKeyMatchesTheApp(t *testing.T) {
	b, err := os.ReadFile("../../testdata/home/key.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx struct{ Sequence, Label string }
	if err := json.Unmarshal(b, &fx); err != nil {
		t.Fatal(err)
	}
	if HomeKey != fx.Sequence {
		t.Errorf("HomeKey = %q, the app sends %q", HomeKey, fx.Sequence)
	}
	if !strings.Contains(WayBack, fx.Label) {
		t.Errorf("WayBack lacks the label %q the app taps", fx.Label)
	}
}
