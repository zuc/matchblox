// Package push holds what the host sends the phone through the relay: the
// content state of the Live Activity, the buzz, and the phone's registration.
// A payload holds only numbers, states, times and percents; no session name,
// account name, provider, path or text. The testdata/push fixtures are read by
// the Swift tests too.
package push

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"time"
)

// ContentState is what the Live Activity shows. Host is the Host8 tag of the
// host, so the widget can show nothing for a host it does not follow.
type ContentState struct {
	Host     string       `json:"host"`
	Burn     int          `json:"burn"`
	Wait     int          `json:"wait"`
	Top      *Top         `json:"top,omitempty"`
	Accounts []AccountUse `json:"accounts"`
	Stoker   bool         `json:"stoker"`
}

// Top is the session the island leads with, by number.
type Top struct {
	N     int       `json:"n"`
	State string    `json:"state"` // burns, waits, done, out or quiet
	Pct   int       `json:"pct"`   // 0..100, in steps of 5
	Out   time.Time `json:"out,omitzero"`
}

// AccountUse is one account's windows, by number.
type AccountUse struct {
	N       int         `json:"n"`
	Windows []WindowUse `json:"windows"`
	Lasts   bool        `json:"lasts"`
	RunsOut time.Time   `json:"runs_out,omitzero"`
}

// WindowUse is one usage window: Name is 5h, wk or mo.
type WindowUse struct {
	Name   string    `json:"name"`
	Pct    int       `json:"pct"`
	Resets time.Time `json:"resets,omitzero"`
}

// Buzz is the event that makes the phone buzz. Sealed is the HPKE enc followed
// by the ciphertext, so the relay cannot read it.
type Buzz struct {
	Host   string `json:"host"`
	N      int    `json:"n"`
	Sealed []byte `json:"sealed"`
}

// RegisterRequest is what the phone sends a host to be pushed to. Grants maps
// a Host8 tag to the grant the relay checks; NotifyKey is the X25519 public
// key the buzz is sealed to.
type RegisterRequest struct {
	DeviceToken string            `json:"device_token"`
	Grants      map[string]string `json:"grants"`
	NotifyKey   []byte            `json:"notify_key"`
	Buzz        bool              `json:"buzz"`
	Island      *IslandReg        `json:"island,omitempty"`
}

// IslandReg is what the host needs to start and update the Live Activity.
type IslandReg struct {
	StartToken string `json:"start_token"`
	Channel    string `json:"channel"`
}

// Registration is a request the host keeps, with the phone's fingerprint and
// when it came. It lives here, not in setup: setup imports push, and push
// never imports setup.
type Registration struct {
	Fingerprint string `json:"fingerprint"`
	RegisterRequest
	At time.Time `json:"at"`
}

// RegisterReply answers a registration: the numbers the host keeps, each with
// the tag the island shows it by.
type RegisterReply struct {
	OK       bool           `json:"ok"`
	Error    string         `json:"error,omitempty"`
	Sessions map[int]string `json:"sessions"`
	Accounts map[int]string `json:"accounts"`
}

// Host8 is the first 8 hex characters of the SHA-256 in a fingerprint
// "SHA256:<base64>", lowercase; "" if it is not one.
func Host8(fingerprint string) string {
	b64, ok := strings.CutPrefix(fingerprint, "SHA256:")
	if !ok {
		return ""
	}
	sum, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(b64, "="))
	if err != nil || len(sum) < 4 {
		return ""
	}
	return hex.EncodeToString(sum[:4])
}
