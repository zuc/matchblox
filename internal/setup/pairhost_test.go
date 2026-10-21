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

const pairPub2 = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIIVa1qDAsYiACLeJanjm6m41vDw6mWU9nkPWxmXYmMJV"

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

// A console that crashed leaves its pair line, which sshd refuses after its
// expiry-time. The next offer takes such lines out, and their seen notes.
func TestNewPairOfferSweepsExpiredPairLines(t *testing.T) {
	home := t.TempDir()
	hostKey := filepath.Join(t.TempDir(), "host.pub")
	if err := os.WriteFile(hostKey, []byte(testHostKey), 0o644); err != nil {
		t.Fatal(err)
	}
	const dead, deadZ, live = "0123456789ab", "abcdef012345", "ba9876543210"
	phone := `restrict,pty,command="'/usr/local/bin/matchblox' gate phone" ` + phoneKey + " " + phoneComment + "Desk"
	writeFile(t, filepath.Join(home, ".ssh", "authorized_keys"),
		`restrict,expiry-time="202001010000",command="'/usr/local/bin/matchblox' gate pair `+dead+`" `+pairPub+" matchblox:pair\n"+
			phone+"\n"+
			`restrict,expiry-time="202001010000Z",command="'/usr/local/bin/matchblox' gate pair `+deadZ+`" ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIM4bfwfUcmyCX2tnMZ4mHkeEjSRJnkf0ehLEuB8iWLHU matchblox:pair`+"\n"+
			`restrict,expiry-time="`+expiryTime(time.Now().Add(time.Minute), expiryZone())+`",command="'/usr/local/bin/matchblox' gate pair `+live+`" `+pairPub2+" matchblox:pair\n")
	for _, id := range []string{dead, deadZ, live} {
		if err := MarkPairSeen(home, id, ""); err != nil {
			t.Fatal(err)
		}
	}
	id, _, err := NewPairOffer(home, "/usr/local/bin/matchblox", "ws-1", 22, "ws-1", hostKey, time.Now().Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{dead, deadZ} {
		if present, _ := PairKeyPresent(home, gone); present {
			t.Errorf("the expired line %s stayed", gone)
		}
		if seen, _ := PairSeen(home, gone); seen {
			t.Errorf("the seen note of %s stayed", gone)
		}
	}
	for _, kept := range []string{live, id} {
		if present, _ := PairKeyPresent(home, kept); !present {
			t.Errorf("the live line %s went", kept)
		}
	}
	if seen, _ := PairSeen(home, live); !seen {
		t.Error("the seen note of a live code went")
	}
	if !strings.Contains(authKeys(home), phone+"\n") {
		t.Errorf("the phone line went:\n%s", authKeys(home))
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
