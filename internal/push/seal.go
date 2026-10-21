package push

import (
	"crypto/ecdh"
	"crypto/hpke"
	"fmt"

	"github.com/blox-eng/matchblox/internal/queue"
)

// buzzInfo is the HPKE info string; the phone opens with the same one.
const buzzInfo = "matchblox buzz v1"

// lineMax is how many characters of an item's last line a buzz carries.
const lineMax = 120

// Seal seals plain to the phone's X25519 key (32 raw bytes) with HPKE base
// mode: DHKEM(X25519, HKDF-SHA256), HKDF-SHA256, ChaCha20Poly1305, info
// "matchblox buzz v1", empty AAD. The result is enc (32 bytes) followed by the
// ciphertext; CryptoKit's Curve25519_SHA256_ChachaPoly suite opens it.
func Seal(notifyKey, plain []byte) ([]byte, error) {
	pub, err := ecdh.X25519().NewPublicKey(notifyKey)
	if err != nil {
		return nil, fmt.Errorf("notify key: %w", err)
	}
	pk, err := hpke.NewDHKEMPublicKey(pub)
	if err != nil {
		return nil, err
	}
	return hpke.Seal(pk, hpke.HKDFSHA256(), hpke.ChaCha20Poly1305(), []byte(buzzInfo), plain)
}

// BuzzLine is the text of a buzz: `work-1 · api waits: "Run the migration?"`.
// A buzz goes only for a wait: the word is asks for a permission, else
// waits. The last line is cut to 120 characters and left out when the item
// has none.
func BuzzLine(hostLabel string, item queue.Item) string {
	word := "waits"
	if item.State == queue.StatePermission {
		word = "asks"
	}
	line := fmt.Sprintf("%s · %s %s", hostLabel, item.Name, word)
	if item.LastLine == "" {
		return line
	}
	r := []rune(item.LastLine)
	if len(r) > lineMax {
		r = r[:lineMax]
	}
	return line + ": \"" + string(r) + "\""
}
