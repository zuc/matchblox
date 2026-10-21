package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/pair"
	"github.com/blox-eng/matchblox/internal/setup"
)

type pairFixture struct {
	HostKeyPub  string            `json:"hostkey_pub"`
	HostKeyFP   string            `json:"hostkey_fp"`
	RequestLine string            `json:"request_line"`
	Replies     map[string]string `json:"replies"`
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

const testExe = "/usr/local/bin/matchblox"

func homeWithPairKey(t *testing.T) (home, id string) {
	t.Helper()
	_, authorized, id, err := pair.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	home = t.TempDir()
	if err := setup.AddPairKey(home, testExe, authorized, id, time.Now().Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	return home, id
}

func useHostLabel(t *testing.T, name string) {
	t.Helper()
	old := hostLabel
	hostLabel = func() string { return name }
	t.Cleanup(func() { hostLabel = old })
}

func TestPairMainAuthorizesAndRemovesTheCode(t *testing.T) {
	f := loadPairFixture(t)
	useHostLabel(t, "ws-1")
	home, id := homeWithPairKey(t)
	var out bytes.Buffer
	if err := pairMain(home, testExe, strings.NewReader(f.RequestLine), &out, id); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != f.Replies["ok"] {
		t.Fatalf("reply = %s, want %s", got, f.Replies["ok"])
	}
	devs, err := setup.Devices(home)
	if err != nil || len(devs) != 1 || devs[0].Name != "Jordan’s iPhone" {
		t.Fatalf("Devices = %+v, %v", devs, err)
	}
	if present, _ := setup.PairKeyPresent(home, id); present {
		t.Fatal("the one-time line is still there")
	}
}

func TestPairMainRefuses(t *testing.T) {
	f := loadPairFixture(t)
	useHostLabel(t, "ws-1")
	good := f.RequestLine
	for name, c := range map[string]struct{ in, reply string }{
		"bad name":     {strings.Replace(good, "Jordan’s iPhone", `a\"b`, 1), f.Replies["bad name"]},
		"bad key":      {`{"name":"iPhone","key":"ssh-ed25519 AAAA"}` + "\n", f.Replies["bad key"]},
		"not json":     {"hello\n", `{"ok":false,"error":"bad request"}`},
		"empty":        {"", `{"ok":false,"error":"bad request"}`},
		"too long":     {strings.Repeat("x", 10000) + "\n", `{"ok":false,"error":"bad request"}`},
		"unterminated": {strings.Repeat("{", 5000), `{"ok":false,"error":"bad request"}`},
	} {
		t.Run(name, func(t *testing.T) {
			home, id := homeWithPairKey(t)
			before, _ := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
			var out bytes.Buffer
			if err := pairMain(home, testExe, strings.NewReader(c.in), &out, id); err != nil {
				t.Fatal(err)
			}
			if got := strings.TrimSpace(out.String()); got != c.reply {
				t.Fatalf("reply = %s, want %s", got, c.reply)
			}
			after, _ := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
			if !bytes.Equal(before, after) {
				t.Fatalf("authorized_keys changed:\n%s", after)
			}
		})
	}
}

func TestPairMainRefusesASlowPhoneAndKeepsTheCode(t *testing.T) {
	f := loadPairFixture(t)
	old := pairReadTimeout
	pairReadTimeout = 50 * time.Millisecond
	t.Cleanup(func() { pairReadTimeout = old })
	home, id := homeWithPairKey(t)
	before, _ := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))

	r, w := io.Pipe() // like the stdin of sshd: open, and silent
	var out bytes.Buffer
	if err := pairMain(home, testExe, r, &out, id); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != f.Replies["too slow"] {
		t.Fatalf("reply = %s, want %s", got, f.Replies["too slow"])
	}
	_, _ = w.Write([]byte(f.RequestLine)) // a request after the refusal claims nothing
	_ = w.Close()
	time.Sleep(20 * time.Millisecond)
	after, _ := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
	if !bytes.Equal(before, after) {
		t.Fatalf("authorized_keys changed:\n%s", after)
	}
}

func TestPairMainNeverEchoesInput(t *testing.T) {
	useHostLabel(t, "ws-1")
	home, id := homeWithPairKey(t)
	var out bytes.Buffer
	_ = pairMain(home, testExe, strings.NewReader("SECRET-TOKEN\n"), &out, id)
	if strings.Contains(out.String(), "SECRET") {
		t.Fatalf("reply echoes input: %s", out.String())
	}
}

func TestPairIDIsTwelveLowercaseHex(t *testing.T) {
	for id, ok := range map[string]bool{
		"0123456789ab": true, "0123456789AB": false, "0123456789a": false,
		"0123456789abc": false, "../../etc/pa": false, "": false,
	} {
		if setup.ValidPairID(id) != ok {
			t.Errorf("ValidPairID(%q) != %v", id, ok)
		}
	}
}

func TestConsolePairWritesAndRemovesTheKey(t *testing.T) {
	f := loadPairFixture(t)
	home := t.TempDir()
	hostKey := filepath.Join(t.TempDir(), "host.pub")
	if err := os.WriteFile(hostKey, []byte(f.HostKeyPub+" root@ws-1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := consolePair(home, testExe, hostKey, func() string { return "ws-1.tail1234.ts.net" })
	id, link, err := h.Offer(time.Now().Add(time.Minute))
	if err != nil || !strings.HasPrefix(link, "matchblox://pair?p=") {
		t.Fatalf("offer: %q %v", link, err)
	}
	if err := setup.MarkPairSeen(home, id, "iPad"); err != nil {
		t.Fatal(err)
	}
	if seen, name := h.Seen(id); !seen || name != "iPad" {
		t.Fatalf("seen = %v %q", seen, name)
	}
	if removed, err := h.Remove(id); !removed || err != nil {
		t.Fatalf("remove = %v %v", removed, err)
	}
	if present, _ := h.Present(id); present {
		t.Fatal("the key line stayed")
	}
	if seen, _ := h.Seen(id); seen {
		t.Fatal("the seen note stayed")
	}
	_, _, err = consolePair(home, "", hostKey, func() string { return "ws-1" }).Offer(time.Now().Add(time.Minute))
	if err == nil || err.Error() != "cannot find this matchblox on the PATH" {
		t.Fatalf("no binary: %v", err)
	}
}

func TestPairCmdStopsOnCtrlC(t *testing.T) {
	f := loadPairFixture(t)
	home := t.TempDir()
	hostKey := filepath.Join(t.TempDir(), "host.pub")
	if err := os.WriteFile(hostKey, []byte(f.HostKeyPub+" root@ws-1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := pairSettings
	pairSettings = pairEnv{home: home, exe: testExe, hostKeyPath: hostKey, poll: time.Millisecond, limit: 5 * time.Minute}
	t.Cleanup(func() { pairSettings = old })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	now := time.Date(2026, 10, 14, 14, 32, 0, 0, time.Local)
	var out bytes.Buffer
	if err := pairCmd(ctx, []string{"--address", "ws-1.tail1234.ts.net", "--name", "ws-1"}, &out, func() time.Time { return now }); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"matchblox://pair?p=", "Scan with the iPhone camera. The code works once, until 14:37."} {
		if !strings.Contains(s, want) {
			t.Errorf("output lacks %q:\n%s", want, s)
		}
	}
	if !strings.HasSuffix(s, "Stopped. The code no longer works.\n") {
		t.Errorf("output does not end with the stopped line:\n%s", s)
	}
	b, _ := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
	if strings.Contains(string(b), "gate pair") {
		t.Fatalf("the pair line is still there:\n%s", b)
	}
}

func TestPairCmdExpiresAtTheDeadline(t *testing.T) {
	f := loadPairFixture(t)
	home := t.TempDir()
	hostKey := filepath.Join(t.TempDir(), "host.pub")
	if err := os.WriteFile(hostKey, []byte(f.HostKeyPub+" root@ws-1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := pairSettings
	pairSettings = pairEnv{home: home, exe: testExe, hostKeyPath: hostKey, poll: time.Hour, limit: time.Millisecond}
	t.Cleanup(func() { pairSettings = old })

	var out bytes.Buffer
	if err := pairCmd(context.Background(), []string{"--address", "ws-1"}, &out, time.Now); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(out.String(), "The code expired. Run matchblox pair again.\n") {
		t.Errorf("output does not end with the expired line:\n%s", out.String())
	}
	b, _ := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
	if strings.Contains(string(b), "gate pair") {
		t.Fatalf("the pair line is still there:\n%s", b)
	}
}

func TestPairCmdSaysWhenTheHostKeyIsMissing(t *testing.T) {
	old := pairSettings
	pairSettings = pairEnv{home: t.TempDir(), exe: testExe, hostKeyPath: filepath.Join(t.TempDir(), "none.pub"), poll: time.Millisecond, limit: 5 * time.Minute}
	t.Cleanup(func() { pairSettings = old })
	err := pairCmd(context.Background(), []string{"--address", "ws-1"}, &bytes.Buffer{}, time.Now)
	if err == nil || !strings.Contains(err.Error(), "sudo ssh-keygen -A") {
		t.Fatalf("err = %v", err)
	}
}

// runPair runs pairCmd while take does what a phone's gate pair does to the
// one-time line, and returns what pair printed.
func runPair(t *testing.T, take func(home, id string)) string {
	t.Helper()
	f := loadPairFixture(t)
	home := t.TempDir()
	hostKey := filepath.Join(t.TempDir(), "host.pub")
	if err := os.WriteFile(hostKey, []byte(f.HostKeyPub+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := pairSettings
	pairSettings = pairEnv{home: home, exe: testExe, hostKeyPath: hostKey, poll: time.Millisecond, limit: 5 * time.Minute}
	t.Cleanup(func() { pairSettings = old })
	deadline := time.Now().Add(10 * time.Second)
	go func() {
		for time.Now().Before(deadline) {
			b, _ := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
			if strings.Contains(string(b), "gate pair") {
				take(home, string(b[strings.Index(string(b), "gate pair ")+len("gate pair "):][:12]))
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	var out bytes.Buffer
	if err := pairCmd(context.Background(), []string{"--address", "ws-1"}, &out, time.Now); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestPairCmdNamesThePhoneThatPaired(t *testing.T) {
	var req pair.Request
	if err := json.Unmarshal([]byte(loadPairFixture(t).RequestLine), &req); err != nil {
		t.Fatal(err)
	}
	fp, _ := setup.Fingerprint(req.Key)
	out := runPair(t, func(home, id string) {
		if _, err := setup.ClaimPairKey(home, testExe, id, req.Key, req.Name); err != nil {
			t.Error(err)
		}
	})
	want := "Paired " + req.Name + " (" + fp + "). Not your phone? Run matchblox devices rm " + fp + ".\n"
	if !strings.HasSuffix(out, want) {
		t.Fatalf("output:\n%s\nwant suffix %q", out, want)
	}
}

func TestPairCmdNamesAPhoneThatPairsAgain(t *testing.T) {
	var req pair.Request
	if err := json.Unmarshal([]byte(loadPairFixture(t).RequestLine), &req); err != nil {
		t.Fatal(err)
	}
	out := runPair(t, func(home, id string) {
		// The same phone, with the same key and name, is there already.
		if _, err := setup.AuthorizeDevice(home, testExe, req.Key, req.Name); err != nil {
			t.Error(err)
		}
		if _, err := setup.ClaimPairKey(home, testExe, id, req.Key, req.Name); err != nil {
			t.Error(err)
		}
	})
	if !strings.Contains(out, "Paired "+req.Name+" (") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestPairCmdSaysWhenTheLineWentWithoutAPhone(t *testing.T) {
	out := runPair(t, func(home, id string) { _, _ = setup.RemovePairKey(home, id) })
	if !strings.HasSuffix(out, "The code is gone, but no phone used it. Run matchblox pair again.\n") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestPairCmdRefusesBadArguments(t *testing.T) {
	home := t.TempDir()
	old := pairSettings
	pairSettings = pairEnv{home: home, exe: testExe, hostKeyPath: "/nonexistent", poll: time.Millisecond, limit: time.Minute}
	t.Cleanup(func() { pairSettings = old })
	for _, args := range [][]string{{"--port", "0"}, {"--port", "70000"}, {"extra"}, {"--bogus"}} {
		if err := pairCmd(context.Background(), args, io.Discard, time.Now); err == nil {
			t.Errorf("pairCmd(%q) = nil, want an error", args)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "authorized_keys")); !os.IsNotExist(err) {
		t.Fatalf("bad arguments wrote authorized_keys: %v", err)
	}
}

func TestPairMainRefusesAKeyThatOpensALogin(t *testing.T) {
	f := loadPairFixture(t)
	var req pair.Request
	if err := json.Unmarshal([]byte(f.RequestLine), &req); err != nil {
		t.Fatal(err)
	}
	home, id := homeWithPairKey(t)
	path := filepath.Join(home, ".ssh", "authorized_keys")
	b, _ := os.ReadFile(path)
	login := req.Key + " me@laptop\n"
	if err := os.WriteFile(path, append([]byte(login), b...), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := pairMain(home, testExe, strings.NewReader(f.RequestLine), &out, id); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != f.Replies["key in use"] {
		t.Fatalf("reply = %s, want %s", got, f.Replies["key in use"])
	}
	after, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(after), login) {
		t.Fatalf("the login line changed:\n%s", after)
	}
	if present, _ := setup.PairKeyPresent(home, id); !present {
		t.Fatal("a refused phone used the code")
	}
}

func TestPairMainWorksOnce(t *testing.T) {
	f := loadPairFixture(t)
	useHostLabel(t, "ws-1")
	home, id := homeWithPairKey(t)
	for i, want := range []string{f.Replies["ok"], f.Replies["code used"]} {
		var out bytes.Buffer
		if err := pairMain(home, testExe, strings.NewReader(f.RequestLine), &out, id); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(out.String()); got != want {
			t.Fatalf("call %d: reply = %s, want %s", i, got, want)
		}
	}
	if devs, _ := setup.Devices(home); len(devs) != 1 {
		t.Fatalf("Devices = %+v", devs)
	}
}

func TestPairMainWithoutALineAddsNothing(t *testing.T) {
	f := loadPairFixture(t)
	home := t.TempDir()
	var out bytes.Buffer
	if err := pairMain(home, testExe, strings.NewReader(f.RequestLine), &out, "0123456789ab"); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != f.Replies["code used"] {
		t.Fatalf("reply = %s", got)
	}
	if devs, _ := setup.Devices(home); len(devs) != 0 {
		t.Fatalf("Devices = %+v", devs)
	}
}

func TestPairMainMarksSeenBeforeItReads(t *testing.T) {
	old := pairReadTimeout
	pairReadTimeout = 50 * time.Millisecond
	t.Cleanup(func() { pairReadTimeout = old })
	home, id := homeWithPairKey(t)
	r, w := io.Pipe() // open, and silent
	t.Cleanup(func() { _ = w.Close() })
	done := make(chan error, 1)
	go func() { done <- pairMain(home, testExe, r, io.Discard, id) }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if seen, _ := setup.PairSeen(home, id); seen {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if seen, name := setup.PairSeen(home, id); !seen || name != "" {
		t.Fatalf("PairSeen = %v, %q; want true and no name", seen, name)
	}
	<-done
}

func TestPairMainNamesTheSeenNote(t *testing.T) {
	f := loadPairFixture(t)
	useHostLabel(t, "ws-1")
	home, id := homeWithPairKey(t)
	if err := pairMain(home, testExe, strings.NewReader(f.RequestLine), io.Discard, id); err != nil {
		t.Fatal(err)
	}
	if seen, name := setup.PairSeen(home, id); !seen || name != "Jordan’s iPhone" {
		t.Fatalf("PairSeen = %v, %q", seen, name)
	}
}

func TestConsolePairOfferErrorSaysPressEnter(t *testing.T) {
	_, _, err := consolePair(t.TempDir(), testExe, filepath.Join(t.TempDir(), "none.pub"), func() string { return "ws-1" }).Offer(time.Now().Add(time.Minute))
	if err == nil || !strings.HasSuffix(err.Error(), "sudo ssh-keygen -A, then press Enter") {
		t.Fatalf("err = %v", err)
	}
}
