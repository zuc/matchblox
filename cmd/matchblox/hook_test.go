package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/config"
	"github.com/blox-eng/matchblox/internal/stoker"
)

func stopJSON(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../../testdata/hooks/stop.json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func spoolLines(t *testing.T, spool string) int {
	t.Helper()
	b, err := os.ReadFile(spool)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(b), "\n")
}

func TestHookWithoutServiceSpools(t *testing.T) {
	sock := isolate(t)
	spool := filepath.Join(t.TempDir(), "spool.jsonl")
	start := time.Now()
	if err := hook([]string{"Stop"}, bytes.NewReader(stopJSON(t)), io.Discard, sock, spool, false); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("hook took %v", d)
	}
	if n := spoolLines(t, spool); n != 1 {
		t.Fatalf("spool has %d lines", n)
	}
}

func TestHookStaleSocket(t *testing.T) {
	sock := isolate(t)
	if err := os.MkdirAll(filepath.Dir(sock), 0o700); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	l.Close() // the file stays, nobody listens
	spool := filepath.Join(t.TempDir(), "spool.jsonl")
	start := time.Now()
	if err := hook([]string{"Stop"}, bytes.NewReader(stopJSON(t)), io.Discard, sock, spool, false); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("hook took %v", d)
	}
	if n := spoolLines(t, spool); n != 1 {
		t.Fatalf("spool has %d lines", n)
	}
}

// A listener that accepts and never says hello must not hold the agent.
func TestHookSilentListener(t *testing.T) {
	sock := isolate(t)
	if err := os.MkdirAll(filepath.Dir(sock), 0o700); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	spool := filepath.Join(t.TempDir(), "spool.jsonl")
	start := time.Now()
	if err := hook([]string{"Stop"}, bytes.NewReader(stopJSON(t)), io.Discard, sock, spool, false); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("hook took %v", d)
	}
	if n := spoolLines(t, spool); n != 1 {
		t.Fatalf("spool has %d lines", n)
	}
}

func TestHookReachesService(t *testing.T) {
	sock := isolate(t)
	s := newService(config.Default(), fixtures(t))
	// The hook runs in this process, which plays the agent in %1.
	s.PeerPane = func(pid int) string {
		if pid == os.Getpid() {
			return "%1"
		}
		return ""
	}
	go serve(sock, s) //nolint:errcheck // ends with the test process
	deadline := time.Now().Add(5 * time.Second)
	for len(s.State().Sessions) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	ev := `{"session_id":"s9","cwd":"/work/app","hook_event_name":"Notification","message":"Claude needs your permission to use Bash","notification_type":"permission_prompt"}`
	t.Setenv("TMUX_PANE", "%1")
	spool := filepath.Join(t.TempDir(), "spool.jsonl")
	if err := hook([]string{"Notification"}, strings.NewReader(ev), io.Discard, sock, spool, false); err != nil {
		t.Fatal(err)
	}
	for time.Now().Before(deadline) {
		for _, it := range s.State().Queue {
			if it.SessionID == "s9" && it.Pane == "%1" && !it.Estimated {
				if _, err := os.Stat(spool); !os.IsNotExist(err) {
					t.Fatal("delivered and spooled")
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("queue %+v", s.State().Queue)
}

// The agent never sees a failing hook: bad input is dropped, exit 0.
func TestHookIgnoresBadInput(t *testing.T) {
	sock := isolate(t)
	if err := hook([]string{"Stop"}, strings.NewReader("not json"), io.Discard, sock, filepath.Join(t.TempDir(), "s"), false); err != nil {
		t.Fatal(err)
	}
}

// statusInput is what Claude Code gives its status line (shortened).
const statusInput = `{"session_id":"s-1","model":{"display_name":"Opus"},"rate_limits":{"five_hour":{"used_percentage":28,"resets_at":1791673800},"seven_day":{"used_percentage":12,"resets_at":1792267200}}}`

// TestTheStatusLineHookTapsTheLimits: the hook keeps the limits for its
// config directory, then prints the line of the status line it wraps, fed
// the same input; with nothing wrapped it prints the limits. A broken input
// or a failing wrapped command never breaks the status line.
func TestTheStatusLineHookTapsTheLimits(t *testing.T) {
	stateDir := t.TempDir()
	cfgDir := filepath.Join(t.TempDir(), "claude")
	env := func(k string) string {
		if k == "CLAUDE_CONFIG_DIR" {
			return cfgDir
		}
		return ""
	}
	now := time.Unix(1791667916, 0)
	var out bytes.Buffer
	if err := statusLine(nil, strings.NewReader(statusInput), &out, env, stateDir, config.Default(), now); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.HasPrefix(got, "5h 28% · wk 12%") {
		t.Fatalf("alone, the status line is %q", got)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "limits")); err != nil {
		t.Fatal("no reading was kept")
	}
	out.Reset()
	if err := statusLine([]string{"--", "sh", "-c", "grep -c rate_limits; echo mine"}, strings.NewReader(statusInput), &out, env, stateDir, config.Default(), now); err != nil {
		t.Fatal(err)
	}
	if out.String() != "1\nmine\n" {
		t.Fatalf("the wrapped status line printed %q", out.String())
	}
	out.Reset()
	if err := statusLine([]string{"--", "sh", "-c", "echo still; exit 3"}, strings.NewReader("not json"), &out, env, stateDir, config.Default(), now); err != nil {
		t.Fatal(err)
	}
	if out.String() != "still\n" {
		t.Fatalf("a broken input: %q", out.String())
	}
}

// bashHook runs the PreToolUse hook for a Bash command in pane %1, with no
// service, and returns what it printed and how long it took.
func bashHook(t *testing.T, dir, command string) (string, time.Duration) {
	t.Helper()
	return bashHookOn(t, dir, "/tmp/tmux-test/default", command)
}

// bashHookOn is bashHook in pane %1 of the tmux server at socket.
func bashHookOn(t *testing.T, dir, socket, command string) (string, time.Duration) {
	t.Helper()
	sock := isolate(t)
	t.Setenv("TMUX_PANE", "%1")
	t.Setenv("TMUX", socket+",4242,0")
	in, err := json.Marshal(map[string]any{"session_id": "s1", "hook_event_name": "PreToolUse", "tool_name": "Bash",
		"tool_use_id": "toolu_1", "tool_input": map[string]string{"command": command}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	start := time.Now()
	if err := hook([]string{"PreToolUse"}, bytes.NewReader(in), &out, sock, filepath.Join(dir, "spool.jsonl"), false); err != nil {
		t.Fatal(err)
	}
	return out.String(), time.Since(start)
}

// guardDir is a state dir with a run on and %1 driven by the stoker with
// grants.
func guardDir(t *testing.T, grants ...string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "stoker.json"), []byte(`{"on":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := stoker.WriteDriven(dir, stoker.Driven{Run: "r", Panes: map[string][]string{"%1": grants}}); err != nil {
		t.Fatal(err)
	}
	return dir
}

const wantDeny = "the stoker's order did not give the go for this. Give it on the stoker's card (y) or stop the stoker."

func denied(t *testing.T, out string) bool {
	t.Helper()
	return deniedWhy(t, out) != ""
}

// deniedWhy is the reason the hook gave for its deny ("": it allowed).
func deniedWhy(t *testing.T, out string) string {
	t.Helper()
	if out == "" {
		return ""
	}
	var got struct {
		HookSpecificOutput struct {
			HookEventName            string `json:"hookEventName"`
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("the hook printed %q: %v", out, err)
	}
	h := got.HookSpecificOutput
	reasons := []string{wantDeny, stoker.DenyStokerPane, stoker.DenyUnread, stoker.DenyLost}
	if h.HookEventName != "PreToolUse" || h.PermissionDecision != "deny" || !slices.Contains(reasons, h.PermissionDecisionReason) {
		t.Fatalf("the deny is %+v", h)
	}
	return h.PermissionDecisionReason
}

// TestHookSaysWhyItDenies: F0b, each cause has its own words.
func TestHookSaysWhyItDenies(t *testing.T) {
	dir := guardDir(t, "merge")
	if why := deniedWhy(t, mustHook(t, dir, `g"h pr merge 1`)); why != stoker.DenyUnread {
		t.Fatalf("an unread line: %q", why)
	}
	// The stoker's %1, on its own server, and the builder's %1 with a go.
	const stokers = "/tmp/tmux-test/matchblox"
	if err := stoker.WriteDriven(dir, stoker.Driven{Run: "r", Panes: map[string][]string{"%1": {"merge"}}, StokerSocket: stokers}); err != nil {
		t.Fatal(err)
	}
	if out, _ := bashHookOn(t, dir, stokers, "gh pr merge 1"); deniedWhy(t, out) != stoker.DenyStokerPane {
		t.Fatalf("the stoker's own pane: %q", out)
	}
	if out := mustHook(t, dir, "gh pr merge 1"); out != "" {
		t.Fatalf("the builder's %%1 lost its go to the stoker's %%1: %q", out)
	}
	if err := os.Remove(filepath.Join(dir, "driven.json")); err != nil {
		t.Fatal(err)
	}
	if why := deniedWhy(t, mustHook(t, dir, "gh pr merge 1")); why != stoker.DenyLost {
		t.Fatalf("a lost mark: %q", why)
	}
	if why := deniedWhy(t, mustHook(t, guardDir(t), "gh pr merge 1")); why != wantDeny {
		t.Fatalf("no go: %q", why)
	}
}

func mustHook(t *testing.T, dir, cmd string) string {
	t.Helper()
	out, _ := bashHook(t, dir, cmd)
	return out
}

func TestHookDeniesUngrantedMerge(t *testing.T) {
	dir := guardDir(t)
	out, _ := bashHook(t, dir, "cd api && gh pr merge 4556 --squash")
	if !denied(t, out) {
		t.Fatal("a merge the order did not give went through")
	}
	// The service still hears of it.
	if n := spoolLines(t, filepath.Join(dir, "spool.jsonl")); n != 1 {
		t.Fatalf("spool has %d lines", n)
	}
	if out, _ := bashHook(t, dir, "gh pr view 4556"); out != "" {
		t.Fatalf("a read was denied: %q", out)
	}
}

func TestHookAllowsGrantedStep(t *testing.T) {
	dir := guardDir(t, "merge:#4556", "release")
	for _, cmd := range []string{"gh pr merge 4556", "gh release create v1"} {
		if out, _ := bashHook(t, dir, cmd); denied(t, out) {
			t.Fatalf("%s: denied", cmd)
		}
	}
	for _, cmd := range []string{"gh pr merge 4557", "gh workflow run Release"} {
		if out, _ := bashHook(t, dir, cmd); !denied(t, out) {
			t.Fatalf("%s: not denied", cmd)
		}
	}
}

func TestHookAllowsUndrivenPane(t *testing.T) {
	dir := guardDir(t)
	if err := stoker.WriteDriven(dir, stoker.Driven{Run: "r", Panes: map[string][]string{"%2": nil}}); err != nil {
		t.Fatal(err)
	}
	if out, _ := bashHook(t, dir, "gh pr merge 1"); out != "" {
		t.Fatalf("a pane the stoker never drove was denied: %q", out)
	}
	// No run at all: nothing to guard.
	if out, _ := bashHook(t, t.TempDir(), "gh pr merge 1"); out != "" {
		t.Fatalf("no run, yet %q", out)
	}
}

func TestHookFailsClosedOnBadDrivenFile(t *testing.T) {
	dir := guardDir(t)
	if err := os.WriteFile(filepath.Join(dir, "driven.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, _ := bashHook(t, dir, "gh pr merge 1"); !denied(t, out) {
		t.Fatal("a broken driven file let a merge through")
	}
	if err := os.Remove(filepath.Join(dir, "driven.json")); err != nil {
		t.Fatal(err)
	}
	if out, _ := bashHook(t, dir, "gh pr merge 1"); !denied(t, out) {
		t.Fatal("a missing driven file let a merge through while a run is on")
	}
}

func TestHookStaysUnder50ms(t *testing.T) {
	dir := guardDir(t, "merge:#2")
	for _, cmd := range []string{"gh pr merge 1", "gh pr merge 2", "go test ./..."} {
		if _, d := bashHook(t, dir, cmd); d > 50*time.Millisecond {
			t.Fatalf("%s: the hook took %v", cmd, d)
		}
	}
}

// TestCursorHookPrintsNothingAndSpools: what Cursor's stop hook prints it
// can take as a follow-up and start a turn on, so the hook prints nothing.
func TestCursorHookPrintsNothingAndSpools(t *testing.T) {
	sock := isolate(t)
	spool := filepath.Join(t.TempDir(), "spool.jsonl")
	b, err := os.ReadFile("../../testdata/hooks/cursor-stop-completed.json")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := hookCursor(bytes.NewReader(b), &out, sock, spool); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Errorf("the hook printed %q", out.String())
	}
	line, err := os.ReadFile(spool)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(line, []byte(`"name":"Stop"`)) || !bytes.Contains(line, []byte(`"agent":"cursor-agent"`)) {
		t.Errorf("spool %s, want a Stop of cursor-agent", line)
	}
}
