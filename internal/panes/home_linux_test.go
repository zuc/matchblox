package panes

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/blox-eng/matchblox/internal/tmuxtest"
)

// TestHomeKeyTakesTheClientHome types the home key into a real client that
// is in another session, after the key was set: tmux moves it to the
// console's session.
func TestHomeKeyTakesTheClientHome(t *testing.T) {
	sock := tmuxtest.Server(t)
	tmux := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("tmux", append([]string{"-L", sock, "-f", os.DevNull}, args...)...).CombinedOutput() //nolint:gosec // test argv
		if err != nil {
			t.Fatalf("tmux %q: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	session := func() string { return tmux("list-clients", "-F", "#{client_session}") }
	tmux("new-session", "-d", "-s", Home)
	tmux("new-session", "-d", "-s", "work")

	term := attach(t, sock, "work")
	waitFor(t, func() bool { return session() == "work" })
	tmux(HomeKeyArgv()[1:]...)
	if _, err := term.WriteString(HomeKey); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return session() == Home })
}

// attach runs `tmux attach -t target` on a new pseudo-terminal and returns
// the terminal's side, where the test types.
func attach(t *testing.T, sock, target string) *os.File {
	t.Helper()
	ptmx, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo-terminals: %v", err)
	}
	t.Cleanup(func() { _ = ptmx.Close() })
	if err := unix.IoctlSetPointerInt(int(ptmx.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	n, err := unix.IoctlGetInt(int(ptmx.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	pts, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pts.Close() }()
	cmd := exec.Command("tmux", "-L", sock, "attach", "-t", target) //nolint:gosec // test argv
	cmd.Stdin, cmd.Stdout, cmd.Stderr = pts, pts, pts
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	cmd.SysProcAttr = &unix.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	go drain(ptmx)
	return ptmx
}

// drain reads what tmux draws, so a full terminal never blocks it.
func drain(f *os.File) {
	b := make([]byte, 4096)
	for {
		if _, err := f.Read(b); err != nil {
			return
		}
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal("timed out")
}
