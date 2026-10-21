package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/pair"
)

func TestPairCmdPlainOutputIsV2(t *testing.T) {
	f := loadPairFixture(t)
	hostKey := filepath.Join(t.TempDir(), "host.pub")
	if err := os.WriteFile(hostKey, []byte(f.HostKeyPub+" root@ws-1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := pairSettings
	pairSettings = pairEnv{home: t.TempDir(), exe: testExe, hostKeyPath: hostKey, poll: time.Millisecond, limit: 5 * time.Minute}
	t.Cleanup(func() { pairSettings = old })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	if err := pairCmd(ctx, []string{"--address", "ws-1.tail1234.ts.net", "--name", "ws-1"}, &out, time.Now); err != nil {
		t.Fatal(err)
	}
	var link string
	qr := 0
	for i, l := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(l, "matchblox://pair?p=") {
			link, qr = l, i
		}
	}
	p, err := pair.Parse(link)
	if err != nil || p.V != 2 {
		t.Fatalf("link %q: V %d, err %v", link, p.V, err)
	}
	if qr > 29 {
		t.Fatalf("the QR is %d lines, want at most 29", qr)
	}
}

func TestPairPollIsTenthOfASecond(t *testing.T) {
	if pairPollInterval != 100*time.Millisecond {
		t.Fatalf("pairPollInterval = %v", pairPollInterval)
	}
}

// The standalone page has no Enter to retry: its offer error keeps the
// command's own words.
func TestStandalonePairHostKeepsTheCommandError(t *testing.T) {
	h := pairHost(t.TempDir(), testExe, filepath.Join(t.TempDir(), "none.pub"), func() string { return "ws-1" }, 22, "ws-1", false)
	_, _, err := h.Offer(time.Now().Add(time.Minute))
	if err == nil || strings.Contains(err.Error(), "press Enter") {
		t.Fatalf("err = %v", err)
	}
	h = pairHost(t.TempDir(), testExe, filepath.Join(t.TempDir(), "none.pub"), func() string { return "ws-1" }, 22, "ws-1", true)
	if _, _, err = h.Offer(time.Now().Add(time.Minute)); err == nil || !strings.Contains(err.Error(), "press Enter") {
		t.Fatalf("console err = %v", err)
	}
}
