package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/pair"
)

const testHostKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIM4bfwfUcmyCX2tnMZ4mHkeEjSRJnkf0ehLEuB8iWLHU root@ws-1\n"

func TestNewPairOfferWritesTheKeyAndTheLink(t *testing.T) {
	home := t.TempDir()
	hostKey := filepath.Join(t.TempDir(), "host.pub")
	if err := os.WriteFile(hostKey, []byte(testHostKey), 0o644); err != nil {
		t.Fatal(err)
	}
	id, link, err := NewPairOffer(home, "/usr/local/bin/matchblox", "ws-1.tail1234.ts.net", 22, "ws-1", hostKey, time.Now().Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if present, err := PairKeyPresent(home, id); err != nil || !present {
		t.Fatalf("the pair line is not there: %v %v", present, err)
	}
	p, err := pair.Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	if p.V != 2 || p.Host != "ws-1.tail1234.ts.net" || p.Port != 22 || p.Name != "ws-1" || p.HostKey != "SHA256:HTAHrx6XYe6mj7kCVbwPfI6FVfygp52CqBMyPnXA0OU" {
		t.Fatalf("payload = %+v", p)
	}
}

func TestNewPairOfferWritesNoKeyWithoutAHostKey(t *testing.T) {
	home := t.TempDir()
	_, _, err := NewPairOffer(home, "/usr/local/bin/matchblox", "ws-1", 22, "ws-1", filepath.Join(home, "none.pub"), time.Now().Add(time.Minute))
	if err == nil || !strings.Contains(err.Error(), "sudo ssh-keygen -A") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "authorized_keys")); err == nil {
		t.Fatal("a key was written")
	}
}

func TestNewPairOfferRemovesTheKeyOnABadLink(t *testing.T) {
	home := t.TempDir()
	hostKey := filepath.Join(t.TempDir(), "host.pub")
	if err := os.WriteFile(hostKey, []byte(testHostKey), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewPairOffer(home, "/usr/local/bin/matchblox", "ws-1", 0, "ws-1", hostKey, time.Now().Add(time.Minute)); err == nil {
		t.Fatal("port 0 made a link")
	}
	b, _ := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
	if strings.Contains(string(b), "gate pair") {
		t.Fatalf("the pair line stayed:\n%s", b)
	}
}
