package setup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/blox-eng/matchblox/internal/pair"
	"github.com/blox-eng/matchblox/internal/push"
)

// ErrUnknownDevice is a registration for an id that no phone line on this
// host has.
var ErrUnknownDevice = errors.New("no phone on this host has that id")

func pushNote(home, id string) string { return filepath.Join(home, ".ssh", ".matchblox-push-"+id) }

// phoneIDs are the ids of the phone lines in keys.
func phoneIDs(keys []byte) map[string]bool {
	ids := map[string]bool{}
	for line := range strings.SplitSeq(string(keys), "\n") {
		if d, ok := parseDevice(line); ok && d.Kind == "phone" && d.ID != "" {
			ids[d.ID] = true
		}
	}
	return ids
}

func phoneIDsOn(home string) (map[string]bool, error) {
	_, b, _, err := read(authorizedKeys(home))
	if err != nil {
		return nil, err
	}
	return phoneIDs(b), nil
}

// Register keeps r as the one registration of the phone whose line has the
// id r.Device, in place of the one it had. It is written under the keys
// lock, so a RemoveDevice of that phone never leaves it behind.
func Register(home string, r push.Registration) error {
	if !ValidPairID(r.Device) {
		return ErrUnknownDevice
	}
	if ids, err := phoneIDsOn(home); err != nil {
		return err
	} else if !ids[r.Device] {
		return ErrUnknownDevice // before the lock, which makes ~/.ssh
	}
	return withKeysLock(home, func() error {
		ids, err := phoneIDsOn(home)
		if err != nil {
			return err
		}
		if !ids[r.Device] {
			return ErrUnknownDevice
		}
		return writeNote(pushNote(home, r.Device), r)
	})
}

// Registrations are the registrations of the phones that still have their
// line: a phone taken out of authorized_keys by hand is pushed to no more.
func Registrations(home string) ([]push.Registration, error) {
	ids, err := phoneIDsOn(home)
	if err != nil {
		return nil, err
	}
	files, _ := filepath.Glob(pushNote(home, "*"))
	var out []push.Registration
	for _, f := range files {
		id := strings.TrimPrefix(filepath.Base(f), ".matchblox-push-")
		var r push.Registration
		if ValidPairID(id) && ids[id] && readNote(f, &r) && r.Device == id {
			out = append(out, r)
		}
	}
	return out, nil
}

// Unregister takes away the registration of the phone id.
func Unregister(home, id string) error {
	if !ValidPairID(id) {
		return errors.New("the device id is 12 lowercase hex characters: " + id)
	}
	return withKeysLock(home, func() error {
		if err := os.Remove(pushNote(home, id)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	})
}

// newDeviceID is a fresh id from the generator of the pairing offer.
var newDeviceID = func() (string, error) {
	_, _, id, err := pair.NewKey()
	return id, err
}

// UpgradePhoneLines gives each phone line from before ids (`gate phone`
// alone) an id of its own, so it can register. Only the command changes: the
// key, the comment, the binary the line names and the order of the lines
// stay. The binary stays so that a service started from another build never
// points a phone at it.
func UpgradePhoneLines(home string) (changed int, err error) {
	_, b, _, err := read(authorizedKeys(home))
	if err != nil || !hasOldPhone(b) {
		return 0, err // nothing to do; the lock would make ~/.ssh
	}
	_, err = rewriteKeys(home, true, func(old []byte) ([]byte, error) {
		changed = 0
		taken := phoneIDs(old)
		lines := strings.Split(string(old), "\n")
		for i, line := range lines {
			if d, ok := parseDevice(line); !ok || d.Kind != "phone" || d.ID != "" {
				continue
			}
			id, err := freshID(taken)
			if err != nil {
				return nil, err
			}
			next, ok := withGateID(line, id)
			if !ok {
				continue
			}
			taken[id] = true
			lines[i] = next
			changed++
		}
		return []byte(strings.Join(lines, "\n")), nil
	}, nil)
	if err != nil {
		return 0, err
	}
	return changed, nil
}

func hasOldPhone(keys []byte) bool {
	for line := range strings.SplitSeq(string(keys), "\n") {
		if d, ok := parseDevice(line); ok && d.Kind == "phone" && d.ID == "" {
			return true
		}
	}
	return false
}

func freshID(taken map[string]bool) (string, error) {
	for range 8 {
		id, err := newDeviceID()
		if err != nil {
			return "", err
		}
		if ValidPairID(id) && !taken[id] {
			return id, nil
		}
	}
	return "", errors.New("cannot make a device id")
}

// withGateID is the phone line with id put after `gate phone`, inside the
// command and nowhere else.
func withGateID(line, id string) (string, bool) {
	const open = `command="'`
	start := strings.Index(line, open)
	if start < 0 {
		return "", false
	}
	end := strings.IndexByte(line[start+len(open):], '"')
	if end < 0 {
		return "", false
	}
	end += start + len(open)
	if !strings.HasSuffix(line[:end], "' gate phone") {
		return "", false
	}
	next := line[:end] + " " + id + line[end:]
	if d, ok := parseDevice(next); !ok || d.ID != id {
		return "", false
	}
	return next, true
}
