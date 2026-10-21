package app

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/setup"
)

// aloneRun runs RunPairPage on a pipe: act sees the home once the key line
// is written, and may write to the terminal.
func aloneRun(t *testing.T, host func(home, hostKey string) *PairHost, act func(home, id string, tty io.Writer)) (string, string, string, error) {
	t.Helper()
	r, _ := newPairRig(t)
	hostKey := filepath.Join(t.TempDir(), "host.pub")
	if err := os.WriteFile(hostKey, []byte(pairHostKey), 0o644); err != nil {
		t.Fatal(err)
	}
	in, w := io.Pipe()
	t.Cleanup(func() { _ = w.Close() })
	go func() {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			b, _ := os.ReadFile(filepath.Join(r.home, ".ssh", "authorized_keys"))
			if i := strings.Index(string(b), "gate pair "); i >= 0 {
				act(r.home, string(b[i+len("gate pair "):][:12]), w)
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	var out bytes.Buffer
	res, err := RunPairPage(PairOptions{Host: host(r.home, hostKey), NoMotion: true, In: in, cols: 100, rows: 40, Out: &out,
		Paired: func(d PairDevice) string { return "Paired " + d.Name }})
	b, _ := os.ReadFile(filepath.Join(r.home, ".ssh", "authorized_keys"))
	return res, string(b), out.String(), err
}

func standardHost(home, hostKey string) *PairHost { return testPairHost(home, pairExe, hostKey) }

func TestRunPairPageStopsOnCtrlC(t *testing.T) {
	res, keys, out, err := aloneRun(t, standardHost, func(_, _ string, tty io.Writer) {
		time.Sleep(300 * time.Millisecond) // the page draws first
		_, _ = tty.Write([]byte{3})
	})
	if err != nil || res != "Stopped. The code no longer works." {
		t.Fatalf("result %q, err %v", res, err)
	}
	if strings.Contains(keys, "gate pair") {
		t.Fatalf("the pair line is still there:\n%s", keys)
	}
	if !strings.Contains(out, "Scan with the matchblox app, or paste the link.") {
		t.Fatalf("the page did not show:\n%q", out)
	}
}

func TestRunPairPageStopsOnEsc(t *testing.T) {
	res, keys, _, err := aloneRun(t, standardHost, func(_, _ string, tty io.Writer) { _, _ = tty.Write([]byte{27}) })
	if err != nil || res != "Stopped. The code no longer works." || strings.Contains(keys, "gate pair") {
		t.Fatalf("result %q, err %v, keys %q", res, err, keys)
	}
}

func TestRunPairPageNamesTheDevice(t *testing.T) {
	start := time.Now()
	res, keys, _, err := aloneRun(t, standardHost, func(home, id string, _ io.Writer) {
		if _, err := setup.ClaimPairKey(home, pairExe, id, pairPhoneKey, "iPad"); err != nil {
			t.Error(err)
		}
	})
	if err != nil || res != "Paired iPad" {
		t.Fatalf("result %q, err %v", res, err)
	}
	if time.Since(start) < pairAloneWait {
		t.Fatalf("the page ended after %v, before the wait", time.Since(start))
	}
	if strings.Contains(keys, "gate pair") {
		t.Fatalf("the pair line is still there:\n%s", keys)
	}
}

func TestRunPairPageReturnsTheOfferError(t *testing.T) {
	_, _, _, err := aloneRun(t, func(string, string) *PairHost {
		h := testPairHost("", "", "")
		h.Offer = func(time.Time) (string, string, error) { return "", "", errors.New("no host key") }
		return h
	}, func(string, string, io.Writer) {})
	if err == nil || err.Error() != "no host key" {
		t.Fatalf("err = %v", err)
	}
}
