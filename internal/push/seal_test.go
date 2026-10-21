package push

import (
	"crypto/ecdh"
	"crypto/hpke"
	"encoding/base64"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blox-eng/matchblox/internal/queue"
)

var update = flag.Bool("update", false, "TestTheVectorOpens: write testdata/push/seal-vector.json")

const vectorPath = "../../testdata/push/seal-vector.json"

// vector is the file the Swift tests read. Hex is not used: every binary field
// is standard base64.
type vector struct {
	Note       string `json:"note"`
	Suite      string `json:"suite"`
	Info       string `json:"info"`
	AAD        string `json:"aad"`
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
	Plain      string `json:"plain"`
	Sealed     string `json:"sealed"`
}

func openWith(t *testing.T, priv, sealed []byte) ([]byte, error) {
	t.Helper()
	k, err := ecdh.X25519().NewPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	pk, err := hpke.NewDHKEMPrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return hpke.Open(pk, hpke.HKDFSHA256(), hpke.ChaCha20Poly1305(), []byte(buzzInfo), sealed)
}

func TestASealedBuzzOpensWithTheKey(t *testing.T) {
	k, err := ecdh.X25519().GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte(`work-1 · api waits: "Run the migration?"`)
	sealed, err := Seal(k.PublicKey().Bytes(), plain)
	if err != nil {
		t.Fatal(err)
	}
	if len(sealed) != 32+len(plain)+16 {
		t.Fatalf("sealed is %d bytes, want enc 32 + text + tag 16", len(sealed))
	}
	got, err := openWith(t, k.Bytes(), sealed)
	if err != nil || string(got) != string(plain) {
		t.Fatalf("open = %q, %v", got, err)
	}
	other, _ := ecdh.X25519().GenerateKey(nil)
	if _, err := openWith(t, other.Bytes(), sealed); err == nil {
		t.Fatal("another key opened the buzz")
	}
	if _, err := Seal([]byte("short"), plain); err == nil {
		t.Fatal("a bad key sealed")
	}
}

func TestTheVectorOpens(t *testing.T) {
	priv := make([]byte, 32)
	for i := range priv {
		priv[i] = byte(0xA0 + i)
	}
	if *update {
		k, err := ecdh.X25519().NewPrivateKey(priv)
		if err != nil {
			t.Fatal(err)
		}
		plain := `work-1 · api waits: "Run the migration?"`
		sealed, err := Seal(k.PublicKey().Bytes(), []byte(plain))
		if err != nil {
			t.Fatal(err)
		}
		b64 := base64.StdEncoding.EncodeToString
		v := vector{
			Note:       "HPKE base mode (RFC 9180), no PSK, no auth, empty AAD. sealed = enc (32 bytes) || ciphertext. CryptoKit: HPKE.Recipient(privateKey:ciphersuite:info:encapsulatedKey:).open(ciphertext, authenticating: Data()). All binary fields are standard base64; info and plain are UTF-8 text.",
			Suite:      "DHKEM(X25519, HKDF-SHA256) / HKDF-SHA256 / ChaCha20Poly1305",
			Info:       buzzInfo,
			AAD:        "",
			PrivateKey: b64(priv),
			PublicKey:  b64(k.PublicKey().Bytes()),
			Plain:      plain,
			Sealed:     b64(sealed),
		}
		out, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(vectorPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(vectorPath, append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(vectorPath)
	if err != nil {
		t.Fatal(err)
	}
	var v vector
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	dec := func(s string) []byte {
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if v.Info != "matchblox buzz v1" || v.AAD != "" {
		t.Fatalf("info %q aad %q", v.Info, v.AAD)
	}
	k, err := ecdh.X25519().NewPrivateKey(dec(v.PrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	if string(k.PublicKey().Bytes()) != string(dec(v.PublicKey)) {
		t.Fatal("public_key is not the public half of private_key")
	}
	got, err := openWith(t, dec(v.PrivateKey), dec(v.Sealed))
	if err != nil || string(got) != v.Plain {
		t.Fatalf("open = %q, %v", got, err)
	}
	if v.Plain != `work-1 · api waits: "Run the migration?"` {
		t.Fatalf("plain = %q", v.Plain)
	}
}

func TestTheBuzzLineSaysWhoWaitsAndWhy(t *testing.T) {
	cases := []struct {
		item queue.Item
		want string
	}{
		{queue.Item{Name: "api", State: queue.StateQuestion, LastLine: "Run the migration?"}, `work-1 · api waits: "Run the migration?"`},
		{queue.Item{Name: "api", State: queue.StatePermission, LastLine: "Run rm?"}, `work-1 · api asks: "Run rm?"`},
		{queue.Item{Name: "api", State: queue.StateQuestion}, `work-1 · api waits`},
		{queue.Item{Name: "api", State: queue.StateQuestion, LastLine: strings.Repeat("é", 130)}, `work-1 · api waits: "` + strings.Repeat("é", 120) + `"`},
	}
	for _, c := range cases {
		if got := BuzzLine("work-1", c.item); got != c.want {
			t.Errorf("BuzzLine = %q, want %q", got, c.want)
		}
	}
}
