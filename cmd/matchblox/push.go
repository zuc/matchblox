package main

import (
	"time"

	"github.com/blox-eng/matchblox/internal/config"
	"github.com/blox-eng/matchblox/internal/push"
	"github.com/blox-eng/matchblox/internal/setup"
)

// homePhones are the registrations kept in home, read again on each List:
// a phone that registers or is removed counts from the next doc on. It is a
// few small files once per tick.
type homePhones string

func (h homePhones) List() []push.Registration {
	regs, _ := setup.Registrations(string(h)) // unreadable: no phone this tick
	return regs
}

// pushSender is the sender of the buzz and the island, for the phones that
// registered in home. Nil and no error with [push] off: nothing ever
// connects to the relay. The host's tag is the fingerprint of the host key
// the pair link pins, so it is the one the phone learned at pairing.
func pushSender(cfg config.Push, home, hostKeyPath string, n *push.Numbers) (*push.Sender, error) {
	if cfg.Off {
		return nil, nil
	}
	fp, err := setup.HostKeyFingerprint(hostKeyPath)
	if err != nil {
		return nil, err
	}
	s := push.NewSender(push.NewHTTPRelay(cfg.Relay, push.DefaultTimeout), homePhones(home), n, fp, time.Now)
	s.HostLabel = setup.HostLabel()
	return s, nil
}
