package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/config"
	"github.com/blox-eng/matchblox/internal/push"
	"github.com/blox-eng/matchblox/internal/service"
	"github.com/blox-eng/matchblox/internal/setup"
	"github.com/blox-eng/matchblox/internal/state"
)

// fakeRelay is a relay on this machine that counts the sends and answers
// code.
func fakeRelay(t *testing.T, code int) (string, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.WriteHeader(code)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &n
}

// hostKey is a host public key in a temp dir, as sshd keeps it.
func hostKey(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ssh_host_ed25519_key.pub")
	if err := os.WriteFile(p, []byte(phoneKey1+" root@host\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// registered is a home whose one phone follows the island.
func registered(t *testing.T) string {
	t.Helper()
	home := homeWithPhoneID(t)
	r := push.Registration{Device: regID, At: time.Now(), RegisterRequest: push.RegisterRequest{
		DeviceToken: "dev-a",
		Island:      &push.IslandReg{StartToken: "start-a", Channel: "chan-a"},
		Grants:      map[string]string{"device:dev-a": "g", "start:start-a": "g", "channel:chan-a": "g"},
	}}
	if err := setup.Register(home, r); err != nil {
		t.Fatal(err)
	}
	return home
}

func numbers(t *testing.T) *push.Numbers {
	t.Helper()
	n, err := push.OpenNumbers(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// waitHits waits up to a second for the relay to see want sends.
func waitHits(n *atomic.Int32, want int32) int32 {
	deadline := time.Now().Add(time.Second)
	for n.Load() < want && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	return n.Load()
}

// [push] off: no sender, and the relay never hears from this host.
func TestPushOffSendsNothing(t *testing.T) {
	url, hits := fakeRelay(t, http.StatusOK)
	s, err := pushSender(config.Push{Relay: url, Off: true}, registered(t), hostKey(t), numbers(t))
	if s != nil || err != nil {
		t.Fatalf("sender %v, %v with push off", s, err)
	}
	time.Sleep(50 * time.Millisecond)
	if n := hits.Load(); n != 0 {
		t.Fatalf("the relay got %d sends with push off", n)
	}
}

// The sender sends to the relay in the config, for the phones that
// registered, and reports a failing relay as push: <why>.
func TestTheSenderUsesTheConfiguredRelay(t *testing.T) {
	url, hits := fakeRelay(t, http.StatusServiceUnavailable)
	s, err := pushSender(config.Push{Relay: url}, registered(t), hostKey(t), numbers(t))
	if s == nil || err != nil {
		t.Fatalf("sender %v, %v", s, err)
	}
	defer s.Close()
	s.OnDoc(state.Doc{})
	if n := waitHits(hits, 1); n == 0 {
		t.Fatal("the relay got no send")
	}
	deadline := time.Now().Add(time.Second)
	for s.Status() == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if st := s.Status(); st != "push: relay 503" {
		t.Fatalf("status %q", st)
	}
	if s.HostLabel == "" {
		t.Fatal("no host label")
	}
}

// No phone registered: nothing is sent.
func TestNoPhoneNoSend(t *testing.T) {
	url, hits := fakeRelay(t, http.StatusOK)
	s, err := pushSender(config.Push{Relay: url}, homeWithPhoneID(t), hostKey(t), numbers(t))
	if s == nil || err != nil {
		t.Fatalf("sender %v, %v", s, err)
	}
	defer s.Close()
	s.OnDoc(state.Doc{})
	time.Sleep(50 * time.Millisecond)
	if n := hits.Load(); n != 0 || s.Status() != "" {
		t.Fatalf("%d sends, status %q without a phone", n, s.Status())
	}
}

// A host without its key sends nothing: no phone could pin it.
func TestNoHostKeyNoSender(t *testing.T) {
	s, err := pushSender(config.Push{Relay: "http://127.0.0.1:9"}, registered(t), filepath.Join(t.TempDir(), "missing.pub"), numbers(t))
	if s != nil || err == nil {
		t.Fatalf("sender %v, %v without a host key", s, err)
	}
}

// A numbers.json that does not parse keeps push off, and the Machine tab
// says why; a good one starts the sender.
func TestUnreadableNumbersKeepPushOff(t *testing.T) {
	url, hits := fakeRelay(t, http.StatusOK)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "numbers.json"), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &service.Service{}
	wirePush(s, config.Push{Relay: url}, registered(t), dir, hostKey(t))
	if s.Push != nil || s.PushNumbers != nil {
		t.Fatalf("push on with a bad numbers.json: %v, %v", s.Push, s.PushNumbers)
	}
	if !strings.HasPrefix(s.PushOff, "push: numbers: ") {
		t.Fatalf("push line %q", s.PushOff)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("the relay got %d sends", n)
	}

	good := &service.Service{}
	wirePush(good, config.Push{Relay: url}, registered(t), t.TempDir(), hostKey(t))
	if good.Push == nil || good.PushNumbers == nil || good.PushOff != "" {
		t.Fatalf("push off with a good dir: %q", good.PushOff)
	}
	good.Push.Close()
}
