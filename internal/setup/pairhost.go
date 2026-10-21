package setup

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"time"

	"github.com/blox-eng/matchblox/internal/pair"
)

// What a pair link names on this host, for `matchblox pair` and the
// console's pair page.

// DefaultHostKey is sshd's ed25519 host key, which the pair link pins.
const DefaultHostKey = "/etc/ssh/ssh_host_ed25519_key.pub"

// PairAddress is the Tailscale name of this host when there is one, else
// its host name.
func PairAddress() string {
	if path, err := exec.LookPath("tailscale"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if b, err := exec.CommandContext(ctx, path, "status", "--json").Output(); err == nil {
			var st struct{ Self struct{ DNSName string } }
			if json.Unmarshal(b, &st) == nil {
				if n := strings.TrimSuffix(st.Self.DNSName, "."); n != "" {
					return n
				}
			}
		}
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "localhost"
}

// HostLabel is the short host name: the label of this host on a device.
func HostLabel() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "host"
	}
	short, _, _ := strings.Cut(h, ".")
	return short
}

// HostKeyFingerprint is the SHA256 fingerprint of the host key at path.
func HostKeyFingerprint(path string) (string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("sshd has no ed25519 host key (%s is missing); run sudo ssh-keygen -A, then matchblox pair again", path)
	}
	if err != nil {
		return "", err
	}
	fp, err := Fingerprint(string(b))
	if err != nil {
		return "", fmt.Errorf("%s is not a public key; run sudo ssh-keygen -A", path)
	}
	return fp, nil
}

// NewPairOffer makes a one-time key, lets it in until until, and returns
// its id and the v2 link that carries it. With an error, no key stays.
func NewPairOffer(home, exe, address string, port int, name, hostKeyPath string, until time.Time) (id, link string, err error) {
	hostKey, err := HostKeyFingerprint(hostKeyPath)
	if err != nil {
		return "", "", err
	}
	u, err := user.Current()
	if err != nil {
		return "", "", err
	}
	seed, authorized, id, err := pair.NewKey()
	if err != nil {
		return "", "", err
	}
	if err := AddPairKey(home, exe, authorized, id, until); err != nil {
		return "", "", err
	}
	p := pair.Payload{V: 2, Host: address, Port: port, User: u.Username, Name: name, HostKey: hostKey, Key: base64.StdEncoding.EncodeToString(seed)}
	if link = p.URL(); link == "" {
		_, _ = RemovePairKey(home, id)
		return "", "", errors.New("cannot make the pair link: check the host key and the port")
	}
	return id, link, nil
}
