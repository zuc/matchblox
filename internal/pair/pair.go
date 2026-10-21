// Package pair is the contract between `matchblox pair` and the phone: the
// payload in the QR code, the request and reply lines, and the one-time key.
package pair

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

const urlPrefix = "matchblox://pair?p="

type Payload struct {
	V       int    `json:"v"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	User    string `json:"user"`
	Name    string `json:"name"`
	HostKey string `json:"hostkey"`
	Key     string `json:"key"`
}

type Request struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

type Reply struct {
	OK    bool   `json:"ok"`
	Host  string `json:"host,omitempty"`
	Error string `json:"error,omitempty"`
}

// v2 is the compact binary link: 0x02, port (2 bytes), sha256 of the host
// key, the seed, then host 0x00 user 0x00 name.
const v2Head = 1 + 2 + sha256.Size + ed25519.SeedSize

// URL makes the v2 link. It returns "" when the host key, the key or the port
// is not well formed.
func (p Payload) URL() string {
	fp, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(p.HostKey, "SHA256:"))
	if err != nil || !strings.HasPrefix(p.HostKey, "SHA256:") || len(fp) != sha256.Size {
		return ""
	}
	seed, err := base64.StdEncoding.DecodeString(p.Key)
	if err != nil || len(seed) != ed25519.SeedSize || p.Port < 1 || p.Port > 65535 {
		return ""
	}
	b := []byte{2, byte(p.Port >> 8), byte(p.Port)}
	b = append(b, fp...)
	b = append(b, seed...)
	b = append(b, p.Host+"\x00"+p.User+"\x00"+p.Name...)
	return urlPrefix + base64.RawURLEncoding.EncodeToString(b)
}

// Parse reads a v1 (JSON) or v2 (binary) link.
func Parse(raw string) (Payload, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "matchblox" || u.Host != "pair" {
		return Payload{}, errors.New("not a matchblox pairing URL")
	}
	enc := u.Query().Get("p")
	if enc == "" {
		return Payload{}, errors.New("pairing URL has no payload")
	}
	b, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return Payload{}, errors.New("pairing payload is not base64url")
	}
	var p Payload
	switch {
	case len(b) > 0 && b[0] == '{':
		if err := json.Unmarshal(b, &p); err != nil {
			return Payload{}, errors.New("pairing payload is not JSON")
		}
		if p.V != 1 {
			return Payload{}, errors.New("pairing payload version is not 1")
		}
	case len(b) > 0 && b[0] == 2:
		if p, err = parseV2(b); err != nil {
			return Payload{}, err
		}
	default:
		return Payload{}, errors.New("pairing payload version is not 1 or 2")
	}
	if p.Host == "" || p.User == "" || p.Name == "" || p.HostKey == "" || p.Key == "" {
		return Payload{}, errors.New("pairing payload is missing a field")
	}
	if p.Port < 1 || p.Port > 65535 {
		return Payload{}, errors.New("pairing port is outside 1..65535")
	}
	if seed, err := base64.StdEncoding.DecodeString(p.Key); err != nil || len(seed) != ed25519.SeedSize {
		return Payload{}, errors.New("pairing key is not a 32-byte seed")
	}
	return p, nil
}

func parseV2(b []byte) (Payload, error) {
	if len(b) < v2Head {
		return Payload{}, errors.New("pairing payload is too short")
	}
	parts := strings.Split(string(b[v2Head:]), "\x00")
	if len(parts) != 3 {
		return Payload{}, errors.New("pairing payload is missing a field")
	}
	return Payload{
		V:       2,
		Host:    parts[0],
		Port:    int(b[1])<<8 | int(b[2]),
		User:    parts[1],
		Name:    parts[2],
		HostKey: "SHA256:" + base64.RawStdEncoding.EncodeToString(b[3:3+sha256.Size]),
		Key:     base64.StdEncoding.EncodeToString(b[3+sha256.Size : v2Head]),
	}, nil
}

// NewKey makes the one-time ed25519 key: its seed, its authorized_keys
// public part and a 12-hex id for the sshd command.
func NewKey() (seed []byte, authorized, id string, err error) {
	seed = make([]byte, ed25519.SeedSize)
	if _, err = rand.Read(seed); err != nil {
		return nil, "", "", err
	}
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	blob := []byte{0, 0, 0, 11}
	blob = append(blob, "ssh-ed25519"...)
	blob = append(blob, 0, 0, 0, byte(len(pub)))
	blob = append(blob, pub...)
	sum := sha256.Sum256(blob)
	return seed, "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob), hex.EncodeToString(sum[:])[:12], nil
}
