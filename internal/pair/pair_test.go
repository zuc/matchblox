package pair

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/blox-eng/matchblox/internal/remote"
)

type fixture struct {
	Payload     Payload           `json:"payload"`
	URL         string            `json:"url"`
	Request     Request           `json:"request"`
	RequestLine string            `json:"request_line"`
	Replies     map[string]string `json:"replies"`
	P256OpenSSH string            `json:"p256_openssh"`
	P256X963Hex string            `json:"p256_x963_hex"`
}

func load(t *testing.T) fixture {
	t.Helper()
	b, err := os.ReadFile("../../testdata/pair/v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

func load2(t *testing.T) fixture {
	t.Helper()
	b, err := os.ReadFile("../../testdata/pair/v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

// v1URL builds the old JSON link, which only Parse still reads.
func v1URL(p Payload) string {
	b, _ := json.Marshal(p)
	return urlPrefix + base64.RawURLEncoding.EncodeToString(b)
}

func TestV2URLMatchesTheFixture(t *testing.T) {
	f := load2(t)
	if got := f.Payload.URL(); got != f.URL {
		t.Fatalf("URL = %s, want %s", got, f.URL)
	}
}

// The fixture URL is decoded here by hand, so it does not only agree with URL.
func TestV2FixtureBytes(t *testing.T) {
	f := load2(t)
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(f.URL, "matchblox://pair?p="))
	if err != nil {
		t.Fatal(err)
	}
	p := f.Payload
	fp, _ := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(p.HostKey, "SHA256:"))
	seed, _ := base64.StdEncoding.DecodeString(p.Key)
	want := append([]byte{2, byte(p.Port >> 8), byte(p.Port)}, fp...)
	want = append(want, seed...)
	want = append(want, p.Host+"\x00"+p.User+"\x00"+p.Name...)
	if string(b) != string(want) {
		t.Fatalf("bytes = %x, want %x", b, want)
	}
	if p.V != 2 {
		t.Fatalf("fixture v = %d", p.V)
	}
}

func TestParseReadsV1AndV2(t *testing.T) {
	f1, f2 := load(t), load2(t)
	a, err := Parse(f1.URL)
	if err != nil || a != f1.Payload {
		t.Fatalf("Parse v1 = %+v, %v", a, err)
	}
	b, err := Parse(f2.URL)
	if err != nil || b != f2.Payload {
		t.Fatalf("Parse v2 = %+v, %v", b, err)
	}
	a.V = 2
	if a != b {
		t.Fatalf("v1 %+v and v2 %+v differ", a, b)
	}
}

func TestParseRefusesV2(t *testing.T) {
	f := load2(t)
	good, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(f.URL, "matchblox://pair?p="))
	link := func(b []byte) string { return urlPrefix + base64.RawURLEncoding.EncodeToString(b) }
	tail := func(host, user, name string) []byte { return []byte(host + "\x00" + user + "\x00" + name) }
	with := func(t []byte) string { return link(append(append([]byte{}, good[:67]...), t...)) }
	port := func(n int) string {
		b := append([]byte{}, good...)
		b[1], b[2] = byte(n>>8), byte(n)
		return link(b)
	}
	p := f.Payload
	cases := map[string]string{
		"short":         link(good[:40]),
		"version 3":     link(append([]byte{3}, good[1:]...)),
		"no separator":  with([]byte(p.Host + " " + p.User + "\x00" + p.Name)),
		"one separator": with([]byte(p.Host + "\x00" + p.User + p.Name)),
		"no host":       with(tail("", p.User, p.Name)),
		"no user":       with(tail(p.Host, "", p.Name)),
		"no name":       with(tail(p.Host, p.User, "")),
		"port 0":        port(0),
		"bad base64":    "matchblox://pair?p=!!!",
	}
	for name, u := range cases {
		if _, err := Parse(u); err == nil {
			t.Errorf("%s: Parse accepted %s", name, u)
		}
	}
}

func TestParseRefuses(t *testing.T) {
	f := load(t)
	with := func(mod func(*Payload)) string {
		p := f.Payload
		mod(&p)
		return v1URL(p)
	}
	cases := map[string]string{
		"v 3":         with(func(p *Payload) { p.V = 3 }),
		"no host":     with(func(p *Payload) { p.Host = "" }),
		"no user":     with(func(p *Payload) { p.User = "" }),
		"no name":     with(func(p *Payload) { p.Name = "" }),
		"no hostkey":  with(func(p *Payload) { p.HostKey = "" }),
		"port 0":      with(func(p *Payload) { p.Port = 0 }),
		"port 65536":  with(func(p *Payload) { p.Port = 65536 }),
		"31-byte key": with(func(p *Payload) { p.Key = base64.StdEncoding.EncodeToString(make([]byte, 31)) }),
		"not base64":  "matchblox://pair?p=!!!",
		"not json":    "matchblox://pair?p=" + base64.RawURLEncoding.EncodeToString([]byte("x")),
		"no p":        "matchblox://pair",
		"scheme":      strings.Replace(f.URL, "matchblox://", "https://", 1),
		"host":        strings.Replace(f.URL, "://pair", "://other", 1),
	}
	for name, u := range cases {
		if _, err := Parse(u); err == nil {
			t.Errorf("%s: Parse accepted %s", name, u)
		}
	}
}

func TestRequestAndReplyLines(t *testing.T) {
	f := load(t)
	b, err := json.Marshal(f.Request)
	if err != nil || string(b)+"\n" != f.RequestLine {
		t.Fatalf("request line = %q, %v", b, err)
	}
	if !remote.ValidDeviceKey(f.Request.Key) || !remote.ValidDeviceKey(f.P256OpenSSH) {
		t.Fatal("fixture key is not a valid device key")
	}
	x, err := hex.DecodeString(f.P256X963Hex)
	if err != nil || len(x) != 65 || x[0] != 4 {
		t.Fatalf("x963 = %s, %v", f.P256X963Hex, err)
	}
	for name, line := range f.Replies {
		var r Reply
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		b, _ := json.Marshal(r)
		if string(b) != line {
			t.Errorf("%s: round trip %s != %s", name, b, line)
		}
	}
}

func TestNewKeyID(t *testing.T) {
	seed, authorized, id, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 12 {
		t.Fatalf("id = %q", id)
	}
	if _, err := hex.DecodeString(id); err != nil {
		t.Fatalf("id not hex: %v", err)
	}
	if !remote.ValidPubKey(authorized) {
		t.Fatalf("authorized = %q", authorized)
	}
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	blob := append([]byte{0, 0, 0, 11}, "ssh-ed25519"...)
	blob = append(blob, 0, 0, 0, 32)
	blob = append(blob, pub...)
	if want := "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob); authorized != want {
		t.Fatalf("authorized = %s, want %s", authorized, want)
	}
	sum := sha256.Sum256(blob)
	if id != hex.EncodeToString(sum[:])[:12] {
		t.Fatalf("id = %s", id)
	}
}

func TestQRSize(t *testing.T) {
	f := load(t)
	lines, err := QR(f.URL)
	if err != nil {
		t.Fatal(err)
	}
	size := len([]rune(lines[0])) - 4
	if size < 21 || len(lines) != (size+4+1)/2 {
		t.Fatalf("%d lines for size %d", len(lines), size)
	}
	for i, l := range lines {
		if n := len([]rune(l)); n != size+4 {
			t.Fatalf("line %d has %d runes, want %d", i, n, size+4)
		}
	}
}

func TestQRSizeV2(t *testing.T) {
	p := load2(t).Payload
	p.Host, p.User, p.Name = "studio-workstation-mbp.tail1a2b3c.ts.net", "buildersmith", "Studio-Workstation-Mbp"
	lines, err := QR(p.URL())
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) > 29 || len([]rune(lines[0])) > 57 {
		t.Fatalf("%d lines of %d runes", len(lines), len([]rune(lines[0])))
	}
}

func TestModulesMatchQR(t *testing.T) {
	f := load2(t)
	size, light, err := Modules(f.URL)
	if err != nil {
		t.Fatal(err)
	}
	lines, _ := QR(f.URL)
	if len([]rune(lines[0])) != size || light(0, 0) != true {
		t.Fatalf("size %d, width %d", size, len([]rune(lines[0])))
	}
}
