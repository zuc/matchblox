package remote

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/proto"
)

// fakeSSH puts an ssh on PATH that drops its options and the host and runs
// the command here with sh, as the far side would. matchblox is the far
// side's binary; "" means it has none.
func fakeSSH(t *testing.T, matchblox string) {
	t.Helper()
	dir := t.TempDir()
	ssh := `#!/bin/sh
while [ $# -gt 0 ]; do
  case "$1" in
    -o|-i) shift 2 ;;
    --) shift; break ;;
    -*) shift ;;
    *) break ;;
  esac
done
echo "$1" > "` + dir + `/host"
shift
exec sh -c "$*"
`
	write(t, filepath.Join(dir, "ssh"), ssh)
	far := filepath.Join(dir, "far")
	if err := os.Mkdir(far, 0o700); err != nil {
		t.Fatal(err)
	}
	if matchblox != "" {
		write(t, filepath.Join(far, "matchblox"), matchblox)
	}
	// No matchblox of this machine leaks in.
	t.Setenv("PATH", dir+":"+far+":/usr/bin:/bin")
	t.Setenv("HOME", t.TempDir())
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil { //nolint:gosec // a test script
		t.Fatal(err)
	}
}

// echoHello is a far-side matchblox that says hello as ws-1, then echoes.
const echoHello = `#!/bin/sh
[ "$1 $2" = "serve --stdio" ] || { echo "wrong args: $*" >&2; exit 2; }
printf '%s\n' '{"kind":"hello","body":{"version":1,"binary":"v0.1.0","host":"ws-1"}}'
cat
`

func TestRemoteHelloOverFakeSSH(t *testing.T) {
	fakeSSH(t, echoHello)
	c, err := Connect(context.Background(), target(t, "ws-1"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	env, err := c.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if env.Kind != proto.KindHello || !strings.Contains(string(env.Body), `"host":"ws-1"`) {
		t.Fatalf("got %s %s", env.Kind, env.Body)
	}
	if err := c.Send(proto.KindHello, "", proto.Hello{Version: proto.Version}); err != nil {
		t.Fatal(err)
	}
	if env, err := c.Recv(); err != nil || env.Kind != proto.KindHello {
		t.Fatalf("the echo: %v %v", env, err)
	}
}

// target is a host reached with a key that exists.
func target(t *testing.T, host string) Target {
	t.Helper()
	key := filepath.Join(t.TempDir(), "matchblox_ed25519")
	write(t, key, "key")
	return Target{Host: host, Key: key}
}

func TestMissingBinaryIsNotInstalled(t *testing.T) {
	fakeSSH(t, "")
	c, err := Connect(context.Background(), target(t, "ws-1"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, err = c.Recv()
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("err = %v, want ErrNotInstalled", err)
	}
}

func TestSSHErrorNamesTheCause(t *testing.T) {
	fakeSSH(t, "#!/bin/sh\necho 'ssh: Could not resolve hostname ws-9: Name or service not known' >&2\nexit 255\n")
	c, err := Connect(context.Background(), target(t, "ws-9"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, err = c.Recv()
	if err == nil || errors.Is(err, ErrNotInstalled) || !strings.Contains(err.Error(), "Could not resolve hostname") {
		t.Fatalf("err = %v, want the last line of ssh's stderr", err)
	}
}

func TestCloseEndsSSH(t *testing.T) {
	fakeSSH(t, echoHello)
	c, err := Connect(context.Background(), target(t, "ws-1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Recv(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := c.Recv(); done <- err }()
	_ = c.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Recv after Close: no error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Recv still blocks after Close")
	}
}

func TestNoKeyIsNotConnected(t *testing.T) {
	fakeSSH(t, echoHello)
	_, err := Connect(context.Background(), Target{Host: "ws-1", Key: filepath.Join(t.TempDir(), "none")})
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("err = %v, want ErrNotConnected", err)
	}
}

func TestClassify(t *testing.T) {
	for _, c := range []struct {
		stderr string
		exit   int
		want   error
	}{
		{"", 127, ErrNotInstalled},
		{"sh: 1: exec: matchblox: not found", 127, ErrNotInstalled},
		{"bash: line 1: /home/b/.local/bin/matchblox: No such file or directory", 127, ErrNotInstalled},
		{"builder@ws-1: Permission denied (publickey).", 255, ErrNotConnected},
		{"Host key verification failed.", 255, ErrHostKeyUnknown},
		{"@@@ WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED! @@@\nHost key verification failed.", 255, ErrHostKeyChanged},
		{"ssh: connect to host ws-1 port 22: Connection refused", 255, nil},
	} {
		got := Classify(c.stderr, c.exit)
		if c.want == nil {
			if got != nil {
				t.Errorf("Classify(%q) = %v, want nil", c.stderr, got)
			}
			continue
		}
		if !errors.Is(got, c.want) {
			t.Errorf("Classify(%q, %d) = %v, want %v", c.stderr, c.exit, got, c.want)
		}
	}
}

func TestArgvIsLockedDown(t *testing.T) {
	tg := Target{Host: "ws-1", Key: "/h/.ssh/matchblox_ed25519"}
	a := tg.Argv()
	for _, want := range []string{"-T", "-i", "/h/.ssh/matchblox_ed25519", "IdentitiesOnly=yes", "IdentityAgent=none", "BatchMode=yes",
		"StrictHostKeyChecking=yes", "ForwardAgent=no", "ClearAllForwardings=yes", "ConnectTimeout=10",
		"ControlMaster=no", "ControlPath=none"} {
		if !slices.Contains(a, want) {
			t.Errorf("argv %q lacks %q", a, want)
		}
	}
	i := slices.Index(a, "--")
	if i < 0 || a[i+1] != "ws-1" || a[i+2] != ServeCommand || len(a) != i+3 {
		t.Fatalf("argv %q: want -- ws-1 %q at the end", a, ServeCommand)
	}
}

// Jumps and door commands run with the builder's own login: the matchblox
// key never gets a terminal on the host.
func TestTermArgvIsTheBuildersOwnLogin(t *testing.T) {
	term := TermArgv("ws-1", []string{"claude", "it's"})
	if term[1] != "-t" || slices.Contains(term, "-i") || slices.Contains(term, "IdentitiesOnly=yes") ||
		!slices.Contains(term, "ForwardAgent=no") || !slices.Contains(term, "ClearAllForwardings=yes") {
		t.Fatalf("term argv %q", term)
	}
	words, err := Split(term[len(term)-1])
	if err != nil {
		t.Fatal(err)
	}
	// One shell that every login shell can start (fish, csh too), which
	// finds tmux and claude in the usual places, then runs argv as argv.
	if words[0] != "/bin/sh" || words[1] != "-c" || !strings.Contains(words[2], "/opt/homebrew/bin") ||
		!strings.Contains(words[2], `exec "$@"`) || !slices.Equal(words[3:], []string{"sh", "claude", "it's"}) {
		t.Fatalf("remote words %q", words)
	}
}

func TestValidHost(t *testing.T) {
	for h, want := range map[string]bool{
		"ws-1": true, "build.example.com": true, "me@ws-1": true, "10.0.0.2": true,
		"": false, "-oProxyCommand=x": false, "ws 1": false, "ws;1": false, "ws-1/x": false,
	} {
		if got := ValidHost(h); got != want {
			t.Errorf("ValidHost(%q) = %v", h, got)
		}
	}
}

func TestNavArgvAttachesOnTheHost(t *testing.T) {
	jump := NavArgv("ws-1", [][]string{{"tmux", "switch-client", "-t", "%12"}})
	if got, want := Line(jump), `ssh … ws-1 tmux select-window -t %12 ';' select-pane -t %12 ';' attach-session -t %12`; got != want {
		t.Fatalf("jump %s\nwant %s", got, want)
	}
	shell := NavArgv("ws-1", [][]string{{"tmux", "new-window", "-c", "/w/a"}})
	if got, want := Line(shell), `ssh … ws-1 tmux new-window -c /w/a ';' attach-session`; got != want {
		t.Fatalf("shell %s\nwant %s", got, want)
	}
}

// The gate reads back only what Quote writes: plain words and single
// quotes. Anything a shell would expand is refused, not guessed.
func TestSplitIsTheInverseOfQuote(t *testing.T) {
	for _, argv := range [][]string{
		{"tmux", "select-window", "-t", "%12", ";", "attach-session", "-t", "%12"},
		{"claude", "Help me; read it's docs"},
		{"sudo", "sh", "-c", "apt-get update && apt-get install -y tmux"},
		{"a", ""},
	} {
		got, err := Split(Quote(argv))
		if err != nil || !slices.Equal(got, argv) {
			t.Errorf("Split(Quote(%q)) = %q, %v", argv, got, err)
		}
	}
	for _, bad := range []string{`echo "x"`, "echo $HOME", "a\\ b", "x `id`", "a 'b", "a; b", "a | b", "a\nb", "$(id)", "a > b"} {
		if got, err := Split(bad); err == nil {
			t.Errorf("Split(%q) = %q, want an error", bad, got)
		}
	}
}

// TestPopupAllowedIsExact: the console opens one popup, on the stoker's
// session, and its shell line holds no word but a server name.
func TestPopupAllowedIsExact(t *testing.T) {
	if got := StokerPopup("matchblox"); !slices.Equal(got, []string{"tmux", "display-popup", "-E", "-w", "90%", "-h", "90%", "tmux -L matchblox attach -t stoker"}) {
		t.Fatalf("popup %q", got)
	}
	for _, c := range []struct {
		argv []string
		ok   bool
	}{
		{StokerPopup("matchblox"), true},
		{StokerPopup("matchblox-test-1"), true},
		{StokerPopup("x; rm -rf ~"), false},
		{StokerPopup("$(id)"), false},
		{[]string{"tmux", "display-popup", "-E", "-w", "90%", "-h", "90%", "tmux -L matchblox attach -t work"}, false},
		{[]string{"tmux", "display-popup", "-E", "sh"}, false},
		{[]string{"tmux", "switch-client", "-t", "%1"}, false},
	} {
		if got := PopupAllowed(c.argv); got != c.ok {
			t.Errorf("%q: %v, want %v", c.argv, got, c.ok)
		}
	}
}

func TestNavAllowedIsExact(t *testing.T) {
	for _, c := range []struct {
		argv []string
		ok   bool
	}{
		{[]string{"tmux", "switch-client", "-t", "%1"}, true},
		{[]string{"tmux", "attach-session", "-t", "%1"}, true},
		{[]string{"tmux", "new-window", "-c", "/w/a"}, true},
		{[]string{"tmux", "new-window", "-c", "/w/#(id)"}, false},
		{[]string{"tmux", "new-window", "rm -rf /"}, false},
		{[]string{"tmux", "switch-client", "-t", "%1;x"}, false},
		{[]string{"sh", "-c", "x"}, false},
	} {
		if got := NavAllowed(c.argv); got != c.ok {
			t.Errorf("NavAllowed(%q) = %v", c.argv, got)
		}
	}
}

func TestBootstrap(t *testing.T) {
	const pub = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeFakeFakeFakeFakeFakeFakeFakeFakeFakeFake matchblox"
	s, err := Bootstrap(pub)
	if err != nil {
		t.Fatal(err)
	}
	words, err := Split(s)
	if err != nil {
		t.Fatal(err)
	}
	// Always the verified install: an older matchblox on the host does not
	// know authorize and would start its console instead. The key is an
	// argument, never part of the script.
	if words[0] != "/bin/sh" || words[1] != "-c" || words[3] != "sh" || words[4] != pub || len(words) != 5 {
		t.Fatalf("words %q", words)
	}
	for _, want := range []string{"curl -fsSL https://matchblox.sh | MATCHBLOX_NO_START=1 sh || exit 1", `matchblox authorize "$1"`} {
		if !strings.Contains(words[2], want) {
			t.Errorf("script lacks %q:\n%s", want, words[2])
		}
	}
	if strings.Contains(words[2], "command -v") {
		t.Errorf("the install must not be skipped:\n%s", words[2])
	}
	for _, bad := range []string{"ssh-rsa AAAA x", "ssh-ed25519 AAAA'; rm -rf ~; echo '", "ssh-ed25519 AAAA x\nssh-ed25519 BBBB y", ""} {
		if _, err := Bootstrap(bad); err == nil {
			t.Errorf("Bootstrap(%q) took a key that is not one ed25519 key", bad)
		}
	}
}

func TestLineShowsTheRemoteCommandAsTyped(t *testing.T) {
	for _, c := range []struct {
		argv []string
		want string
	}{
		{[]string{"ssh", "-t", "--", "ws-1", "sudo sh -c 'apt-get update && apt-get install -y tmux'"}, `ssh -t -- ws-1 "sudo sh -c 'apt-get update && apt-get install -y tmux'"`},
		{[]string{"ssh", "-t", "--", "ws-1", "echo 'a\"b'"}, `ssh -t -- ws-1 'echo '\''a"b'\'''`},
		{[]string{"ssh", "-t", "--", "ws-1", "claude"}, `ssh -t -- ws-1 claude`},
	} {
		if got := Line(c.argv); got != c.want {
			t.Errorf("Line = %s\nwant   %s", got, c.want)
		}
	}
}

func TestLineFoldsTheConsoleOptions(t *testing.T) {
	tg := Target{Host: "ws-1", Key: "/h/.ssh/matchblox_ed25519"}
	if got, want := Line(tg.Argv()), "ssh … ws-1 matchblox serve --stdio"; got != want {
		t.Fatalf("Line = %s\nwant   %s", got, want)
	}
}

func TestValidDeviceKey(t *testing.T) {
	const good = "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBH7iriHk4uoQ5QFqWYraWlOwFNM2wikZzsWVilC6boso/FQAq1zahWnKxRjYJ2Z/EOtOHrgqLqjFZsrrjAmjJQA="
	if !ValidDeviceKey(good) || !ValidDeviceKey(good+" iphone@home") {
		t.Fatal("a P-256 key from ssh-keygen was refused")
	}
	for name, k := range map[string]string{
		"ed25519":     "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIM4bfwfUcmyCX2tnMZ4mHkeEjSRJnkf0ehLEuB8iWLHU",
		"nistp384":    "ecdsa-sha2-nistp384 AAAAE2VjZHNhLXNoYTItbmlzdHAzODQAAAAIbmlzdHAzODQAAABhBHm7/4305pCOcNUw2lMsS2Hur/bludwPenHsorMahHh/qJRnbGl5IazFSRCLpPU0so0xljdgFTnkpRt6LbSroPx9T/b+cmcgRzTZ1d2YsjVNJhyBiP9FxgKvpF3QOblRrA==",
		"cut blob":    good[:len(good)-24],
		"two words":   "x y",
		"empty":       "",
		"bad comment": good + ` x"y`,
		"newline":     good + "\nssh-rsa AAAA",
	} {
		if ValidDeviceKey(k) {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestClientTTYIsADeviceName(t *testing.T) {
	for tty, ok := range map[string]bool{
		"/dev/ttys004": true, "/dev/pts/3": true,
		"": false, "ttys004": false, "/dev/": false, "/dev/tty;x": false, "/dev/tty x": false,
		"/dev/#(id)": false, "/dev/../etc/passwd": false, "/dev/" + strings.Repeat("a", 64): false,
	} {
		if got := ClientTTY(tty); got != ok {
			t.Errorf("ClientTTY(%q) = %v", tty, got)
		}
	}
}

// The focus moves one client, named by its tty, to one pane: nothing else.
func TestFocusAllowedIsExact(t *testing.T) {
	for _, c := range []struct {
		argv []string
		ok   bool
	}{
		{FocusArgv("/dev/ttys004", "%1"), true},
		{[]string{"tmux", "switch-client", "-c", "/dev/ttys004", "-t", "%1"}, true},
		{[]string{"tmux", "switch-client", "-t", "%1"}, false},
		{[]string{"tmux", "switch-client", "-c", "/dev/tty;x", "-t", "%1"}, false},
		{[]string{"tmux", "switch-client", "-c", "/dev/ttys004", "-t", "%1;x"}, false},
		{[]string{"tmux", "attach-session", "-c", "/dev/ttys004", "-t", "%1"}, false},
		{[]string{"tmux", "switch-client", "-t", "%1", "-c", "/dev/ttys004"}, false},
	} {
		if got := FocusAllowed(c.argv); got != c.ok {
			t.Errorf("FocusAllowed(%q) = %v", c.argv, got)
		}
	}
	// The gate's and the console's nav allowlist does not grow.
	if NavAllowed(FocusArgv("/dev/ttys004", "%1")) {
		t.Error("NavAllowed takes the focus's -c")
	}
}
