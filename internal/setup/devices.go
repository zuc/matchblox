package setup

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/blox-eng/matchblox/internal/remote"
)

// Device is a key that the gate lets in: a phone, or a laptop console.
// ID is the id of the pairing that added a phone; it is "" for a laptop and
// for a phone line from before ids.
type Device struct{ Kind, Name, Fingerprint, ID string }

// ValidDeviceName is a name the line comment can hold and a person can read.
func ValidDeviceName(s string) bool {
	if n := utf8.RuneCountInString(s); n < 1 || n > 40 || s[0] == ' ' || s[len(s)-1] == ' ' {
		return false
	}
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune(" ._-'’", r) {
			return false
		}
	}
	return true
}

// ErrKeyInUse is a phone key that already opens a login of its own: pairing
// must not take that login's power away.
var ErrKeyInUse = errors.New("this key already opens a login on this host")

// ErrCodeUsed is a one-time key whose line is gone: it was used, it expired,
// or matchblox pair stopped.
var ErrCodeUsed = errors.New("the pairing code was used")

// AuthorizeDevice lets a phone key start only `matchblox gate phone`, which then
// runs the stream or the console. Rules as for Authorize; the comment names the phone.
func AuthorizeDevice(home, exe, pubKey, name string) (backup string, err error) {
	if err := checkDevice(exe, pubKey, name); err != nil {
		return "", err
	}
	return rewriteKeys(home, true, func(old []byte) ([]byte, error) { return withPhone(old, exe, pubKey, name, "") }, nil)
}

func checkDevice(exe, pubKey, name string) error {
	if !remote.ValidDeviceKey(pubKey) {
		return errors.New("not one P-256 public key")
	}
	if !ValidDeviceName(name) {
		return errors.New("the name has 1 to 40 letters, digits, spaces and . _ - ' only, and no space at either end: " + name)
	}
	return checkExe(exe)
}

// withPhone puts the gate line of a phone in old. A phone line of the same key
// is replaced; any other line of that key is the person's own login and
// stays, so the phone is refused. A holder of a live code can learn from the
// refusal that a key opens this host; the code stays theirs anyway. A phone
// with an id gets `gate phone <id>`; the gate keeps notes by that id.
func withPhone(old []byte, exe, pubKey, name, id string) ([]byte, error) {
	if id != "" && !ValidPairID(id) {
		return nil, errors.New("the pairing id is 12 lowercase hex characters: " + id)
	}
	f := strings.Fields(pubKey)
	for line := range strings.SplitSeq(string(old), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") || !hasKey(line, f[1]) {
			continue
		}
		if d, ok := parseDevice(line); !ok || d.Kind != "phone" {
			return nil, ErrKeyInUse
		}
	}
	gate := "gate phone"
	if id != "" {
		gate += " " + id
	}
	return withLine(old, f[1], `restrict,pty,command="'`+exe+`' `+gate+`" `+f[0]+" "+f[1]+" "+phoneComment+name), nil
}

// ClaimPairKey takes out the line of the one-time key id and adds the phone
// in one write, so the code works once and a phone is never half added. Then,
// under the same lock, it leaves a note for TakeClaim. A claim that fails
// never touches the note of one that worked. A name that a phone has gets a
// number (iPad 2, iPad 3); the Device and the note carry the name that was
// written.
func ClaimPairKey(home, exe, id, pubKey, name string) (Device, error) {
	if !ValidPairID(id) {
		return Device{}, errors.New("the pairing id is 12 lowercase hex characters: " + id)
	}
	if err := checkDevice(exe, pubKey, name); err != nil {
		return Device{}, err
	}
	fp, err := Fingerprint(pubKey)
	if err != nil {
		return Device{}, err
	}
	d := Device{Kind: "phone", Name: name, Fingerprint: fp, ID: id}
	_, err = rewriteKeys(home, true, func(old []byte) ([]byte, error) {
		kept, n, err := withoutPairLine(old, id)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return nil, ErrCodeUsed
		}
		d.Name = uniquePhoneName(kept, name, strings.Fields(pubKey)[1])
		return withPhone(kept, exe, pubKey, d.Name, id)
	}, func() {
		// matchblox pair waits for the lock, so it reads the note once the line is gone.
		b, _ := json.Marshal(d)
		_ = os.WriteFile(claimNote(home, id), b, 0o600)
	})
	if err != nil {
		return Device{}, err
	}
	return d, nil
}

// uniquePhoneName is name, or name 2, name 3 and so on, whichever no phone in
// keys has. The line of the same key does not count: it is replaced. The base
// is cut so that the result is still a valid name.
func uniquePhoneName(keys []byte, name, blob string) string {
	taken := map[string]bool{}
	for line := range strings.SplitSeq(string(keys), "\n") {
		if d, ok := parseDevice(line); ok && d.Kind == "phone" && !hasKey(line, blob) {
			taken[d.Name] = true
		}
	}
	if !taken[name] {
		return name
	}
	for n := 2; ; n++ {
		suffix := " " + strconv.Itoa(n)
		base := []rune(name)
		if keep := 40 - len(suffix); len(base) > keep {
			base = base[:keep]
		}
		if c := strings.TrimRight(string(base), " ") + suffix; !taken[c] {
			return c
		}
	}
}

func claimNote(home, id string) string { return filepath.Join(home, ".ssh", ".matchblox-pair-"+id) }

// TakeClaim is the phone that used the one-time key id, and removes the note.
// It waits for a ClaimPairKey that holds the lock.
func TakeClaim(home, id string) (d Device, ok bool) {
	if !ValidPairID(id) {
		return Device{}, false
	}
	_ = withKeysLock(home, func() error {
		b, err := os.ReadFile(claimNote(home, id))
		if err != nil {
			return err
		}
		_ = os.Remove(claimNote(home, id))
		ok = json.Unmarshal(b, &d) == nil
		return nil
	})
	return d, ok
}

// Fingerprint is the SHA256 fingerprint of a key line, as ssh-keygen -lf
// prints it.
func Fingerprint(authorizedKey string) (string, error) {
	f := strings.Fields(authorizedKey)
	if len(f) < 2 {
		return "", errors.New("not a public key line")
	}
	blob, err := base64.StdEncoding.DecodeString(f[1])
	if err != nil || len(blob) == 0 {
		return "", errors.New("not a public key line")
	}
	sum := sha256.Sum256(blob)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]), nil
}

var gateLine = regexp.MustCompile(`^[a-z,-]*command="'[^"]*' gate(?: phone(?: ([0-9a-f]{12}))?)?" (\S+ \S+)(?: (.*))?$`)

const phoneComment = "matchblox:phone:"

func parseDevice(line string) (Device, bool) {
	m := gateLine.FindStringSubmatch(strings.TrimSuffix(line, "\r"))
	if m == nil {
		return Device{}, false
	}
	fp, err := Fingerprint(m[2])
	if err != nil {
		return Device{}, false
	}
	if name, ok := strings.CutPrefix(m[3], phoneComment); ok {
		return Device{Kind: "phone", Name: name, Fingerprint: fp, ID: m[1]}, true
	}
	return Device{Kind: "laptop", Name: m[3], Fingerprint: fp}, true
}

// Devices are the keys that the gate lets in, in the order of the file.
func Devices(home string) ([]Device, error) {
	_, b, _, err := read(authorizedKeys(home))
	if err != nil {
		return nil, err
	}
	var out []Device
	for line := range strings.SplitSeq(string(b), "\n") {
		if d, ok := parseDevice(line); ok {
			out = append(out, d)
		}
	}
	return out, nil
}

// RemoveDevice takes out the line of one phone or laptop, by its name (a
// laptop's name is its key comment) or, when names repeat, by its
// fingerprint. A device with an id also loses its note of MarkDeviceIn.
func RemoveDevice(home, name string) (backup string, err error) {
	if name == "" {
		return "", errors.New("give a name or a fingerprint from matchblox devices")
	}
	var gone string
	return rewriteKeys(home, true, func(old []byte) ([]byte, error) {
		lines := strings.Split(string(old), "\n")
		hit, n := -1, 0
		for i, line := range lines {
			if d, ok := parseDevice(line); ok && (d.Fingerprint == name || d.Name == name) {
				hit, gone = i, d.ID
				n++
			}
		}
		switch {
		case n == 0:
			return nil, errors.New("no device named " + name + "; run matchblox devices and give a name or a fingerprint from that list")
		case n > 1:
			return nil, fmt.Errorf("%d devices named %s; give the fingerprint", n, name)
		}
		return []byte(strings.Join(append(lines[:hit:hit], lines[hit+1:]...), "\n")), nil
	}, func() {
		if gone != "" {
			_ = os.Remove(deviceNote(home, gone))
		}
	})
}

func authorizedKeys(home string) string { return filepath.Join(home, ".ssh", "authorized_keys") }

var pairID = regexp.MustCompile(`^[0-9a-f]{12}$`)

// ValidPairID is the id of a one-time key: 12 lowercase hex characters.
func ValidPairID(id string) bool { return pairID.MatchString(id) }

func pairCommand(exe, id string) string { return `command="'` + exe + `' gate pair ` + id + `"` }

// zoneFile is the system time zone, which sshd reads expiry-time in.
var zoneFile = "/etc/localtime"

// expiryZone is the zone of the expiry-time; tests replace it.
var expiryZone = systemZone

// systemZone is the zone of /etc/localtime, not time.Local: the TZ variable of
// this process may differ from the one of sshd. If the file cannot be read,
// UTC is the fallback: it is right on the many hosts whose clock is UTC, and a
// wrong zone only moves the expiry, it never lets the key work after use.
func systemZone() *time.Location {
	b, err := os.ReadFile(zoneFile)
	if err == nil {
		if loc, err := time.LoadLocationFromTZData("", b); err == nil {
			return loc
		}
	}
	return time.UTC
}

// expiryTime is until, rounded up to the next whole minute so that the key
// never expires early, as YYYYMMDDHHMM in zone. The form has no Z suffix: sshd
// reads it in the system zone from OpenSSH 7.7, and a Z needs OpenSSH 9.1,
// where older sshd rejects the whole line.
func expiryTime(until time.Time, zone *time.Location) string {
	t := until.Truncate(time.Minute)
	if t.Before(until) {
		t = t.Add(time.Minute)
	}
	return t.In(zone).Format("200601021504")
}

// AddPairKey lets a one-time key run only `matchblox gate pair <id>`, and
// tells sshd to refuse it after until.
func AddPairKey(home, exe, pubKey, id string, until time.Time) error {
	if !remote.ValidPubKey(pubKey) {
		return errors.New("not one ed25519 public key")
	}
	if !pairID.MatchString(id) {
		return errors.New("the pairing id is 12 lowercase hex characters: " + id)
	}
	if err := checkExe(exe); err != nil {
		return err
	}
	f := strings.Fields(pubKey)
	_, err := putKeyLine(home, f[1], `restrict,expiry-time="`+expiryTime(until, expiryZone())+`",`+pairCommand(exe, id)+" "+f[0]+" "+f[1]+" matchblox:pair", false)
	return err
}

var pairLine = regexp.MustCompile(`^restrict,expiry-time="\d{12}Z?",command="'[^"']*' gate pair ([0-9a-f]{12})" `)

// withoutPairLine is old without the line of the one-time key id; n is how
// many lines went.
func withoutPairLine(old []byte, id string) (kept []byte, n int, err error) {
	if !pairID.MatchString(id) {
		return nil, 0, errors.New("the pairing id is 12 lowercase hex characters: " + id)
	}
	lines := strings.Split(string(old), "\n")
	all := len(lines)
	lines = slices.DeleteFunc(lines, func(line string) bool {
		m := pairLine.FindStringSubmatch(line)
		return m != nil && m[1] == id
	})
	return []byte(strings.Join(lines, "\n")), all - len(lines), nil
}

// PairKeyPresent is whether the line of the one-time key is still there.
func PairKeyPresent(home, id string) (bool, error) {
	_, old, _, err := read(authorizedKeys(home))
	if err != nil {
		return false, err
	}
	_, n, err := withoutPairLine(old, id)
	return n > 0, err
}

// RemovePairKey takes out the line of the one-time key; removed is true for
// the one caller that took it. Every other line keeps its bytes.
func RemovePairKey(home, id string) (removed bool, err error) {
	_, err = rewriteKeys(home, false, func(old []byte) ([]byte, error) {
		kept, n, err := withoutPairLine(old, id)
		removed = n > 0
		return kept, err
	}, nil)
	return removed, err
}

// The notes below are written by the gate and read by the console, two
// processes: a note is written whole to a temporary file and then renamed.
func writeNote(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".matchblox-note-")
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(f.Name(), 0o600)
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		_ = os.Remove(f.Name())
	}
	return err
}

func readNote(path string, v any) bool {
	b, err := os.ReadFile(path)
	return err == nil && json.Unmarshal(b, v) == nil
}

func seenNote(home, id string) string { return claimNote(home, id) + ".seen" }

func deviceNote(home, id string) string {
	return filepath.Join(home, ".ssh", ".matchblox-device-"+id)
}

// MarkPairSeen notes that a phone opened the page of the one-time key id. The
// name is the one that the phone gave, or "" if it gave none yet.
func MarkPairSeen(home, id, name string) error {
	if !ValidPairID(id) {
		return errors.New("the pairing id is 12 lowercase hex characters: " + id)
	}
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		return err
	}
	return writeNote(seenNote(home, id), struct {
		At   time.Time `json:"at"`
		Name string    `json:"name"`
	}{time.Now(), name})
}

// PairSeen is whether a phone opened the page of the one-time key id, and the
// name it gave.
func PairSeen(home, id string) (seen bool, name string) {
	var n struct {
		Name string `json:"name"`
	}
	if !ValidPairID(id) || !readNote(seenNote(home, id), &n) {
		return false, ""
	}
	return true, n.Name
}

// RemovePairSeen takes away the note of MarkPairSeen.
func RemovePairSeen(home, id string) {
	if ValidPairID(id) {
		_ = os.Remove(seenNote(home, id))
	}
}

type deviceIn struct {
	At  time.Time `json:"at"`
	TTY string    `json:"tty"`
}

// MarkDeviceIn notes that the device id is in, on the terminal tty.
func MarkDeviceIn(home, id, tty string, at time.Time) error {
	if !ValidPairID(id) {
		return errors.New("the device id is 12 lowercase hex characters: " + id)
	}
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		return err
	}
	return writeNote(deviceNote(home, id), deviceIn{at, tty})
}

// DeviceIn is when and where the device id came in; ok is false if no note
// is there.
func DeviceIn(home, id string) (at time.Time, tty string, ok bool) {
	var n deviceIn
	if !ValidPairID(id) || !readNote(deviceNote(home, id), &n) {
		return time.Time{}, "", false
	}
	return n.At, n.TTY, true
}

// DeviceTTYs maps each terminal to the id of the device that is in on it. A
// terminal that two notes name goes to the note with the later time.
func DeviceTTYs(home string) map[string]string {
	out := map[string]string{}
	when := map[string]time.Time{}
	files, _ := filepath.Glob(deviceNote(home, "*"))
	for _, f := range files {
		id := strings.TrimPrefix(filepath.Base(f), ".matchblox-device-")
		if at, tty, ok := DeviceIn(home, id); ok && tty != "" && (out[tty] == "" || at.After(when[tty])) {
			out[tty], when[tty] = id, at
		}
	}
	return out
}
