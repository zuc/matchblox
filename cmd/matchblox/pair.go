package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"

	"github.com/blox-eng/matchblox/internal/app"
	"github.com/blox-eng/matchblox/internal/pair"
	"github.com/blox-eng/matchblox/internal/remote"
	"github.com/blox-eng/matchblox/internal/setup"
)

const (
	pairWindow       = 5 * time.Minute
	maxPairRequest   = 4096
	defaultHostKey   = setup.DefaultHostKey
	pairPollInterval = 100 * time.Millisecond
)

// pairEnv is what pairCmd reads from the machine; tests replace it.
type pairEnv struct {
	home, exe   string
	hostKeyPath string
	poll, limit time.Duration
}

// pairReadTimeout is how long gate pair waits for the request line; tests
// shorten it.
var pairReadTimeout = 30 * time.Second

var pairSettings = pairEnv{hostKeyPath: defaultHostKey, poll: pairPollInterval, limit: pairWindow}

var hostLabel = setup.HostLabel

// pairMain is `gate pair <id>`: the one-time key's forced command. It reads
// one bounded request line from the phone, adds the phone's key and removes
// the one-time line. It never echoes the request.
func pairMain(home, exe string, in io.Reader, out io.Writer, id string) error {
	reply := func(r pair.Reply) error {
		b, _ := json.Marshal(r)
		_, err := fmt.Fprintf(out, "%s\n", b)
		return err
	}
	refuse := func(msg string) error { return reply(pair.Reply{Error: msg}) }

	// The console learns that a phone opened the page before it sends anything.
	_ = setup.MarkPairSeen(home, id, "")

	// stdin is a pipe from sshd, which has no read deadline: wait in a
	// goroutine. A phone that holds the session open must not claim the code
	// later, so a slow request is refused before the code is claimed.
	lines := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReaderSize(io.LimitReader(in, maxPairRequest+1), maxPairRequest+1).ReadString('\n')
		lines <- line
	}()
	var line string
	timer := time.NewTimer(pairReadTimeout)
	defer timer.Stop()
	select {
	case line = <-lines:
	case <-timer.C:
		return refuse("too slow")
	}
	var req pair.Request
	if len(line) > maxPairRequest || json.Unmarshal([]byte(line), &req) != nil {
		return refuse("bad request")
	}
	if !setup.ValidDeviceName(req.Name) {
		return refuse("bad name")
	}
	if !remote.ValidDeviceKey(req.Key) {
		return refuse("bad key")
	}
	_ = setup.MarkPairSeen(home, id, req.Name)
	// One write claims the code and adds the phone: it works once.
	_, err := setup.ClaimPairKey(home, exe, id, req.Key, req.Name)
	switch {
	case errors.Is(err, setup.ErrCodeUsed):
		return refuse("code used")
	case errors.Is(err, setup.ErrKeyInUse):
		return refuse("key in use")
	case err != nil:
		fmt.Fprintln(os.Stderr, "matchblox gate pair:", err)
		return refuse("write failed")
	}
	return reply(pair.Reply{OK: true, Host: hostLabel()})
}

// pairCmd is `matchblox pair`: it shows a one-time QR code and waits until a
// phone uses it or the code expires.
func pairCmd(ctx context.Context, args []string, out io.Writer, now func() time.Time) error {
	env := pairSettings
	fl := flag.NewFlagSet("pair", flag.ContinueOnError)
	address := fl.String("address", "", "host name the phone connects to (default: the Tailscale name, else the host name)")
	port := fl.Int("port", 22, "ssh port on this host")
	name := fl.String("name", setup.HostLabel(), "label of this host on the phone")
	noMotion := fl.Bool("no-motion", false, "show the last frame of each motion")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if fl.NArg() != 0 || *port < 1 || *port > 65535 {
		return errors.New("usage: matchblox pair [--address host] [--port 22] [--name label] [--no-motion]")
	}
	if *address == "" {
		*address = setup.PairAddress()
	}
	if env.home == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		env.home = home
	}
	if env.exe == "" {
		if env.exe = invokedPath(os.Args[0]); env.exe == "" {
			return errors.New("cannot find this matchblox on the PATH")
		}
	}
	if isTerminal(out) {
		host := pairHost(env.home, env.exe, env.hostKeyPath, func() string { return *address }, *port, *name, false)
		line, err := app.RunPairPage(app.PairOptions{
			Host:     host,
			NoMotion: *noMotion || os.Getenv("NO_MOTION") == "1",
			Now:      now,
			Paired:   func(d app.PairDevice) string { return pairedText(d.Name, d.Fingerprint) },
		})
		if line != "" {
			_, _ = fmt.Fprintln(out, line)
		}
		return err
	}
	until := now().Add(env.limit)
	id, url, err := setup.NewPairOffer(env.home, env.exe, *address, *port, *name, env.hostKeyPath, until)
	if err != nil {
		return err
	}
	qr, err := pair.QR(url)
	if err != nil {
		_, _ = setup.RemovePairKey(env.home, id)
		return err
	}
	_, _ = fmt.Fprintln(out, strings.Join(qr, "\n"))
	_, _ = fmt.Fprintln(out, url)
	_, _ = fmt.Fprintf(out, "Scan with the matchblox app, or paste the link. The code works once, until %s.\n", until.Local().Format("15:04"))

	tick := time.NewTicker(env.poll)
	defer tick.Stop()
	deadline := time.NewTimer(env.limit)
	defer deadline.Stop()
	for {
		select {
		case <-ctx.Done():
		case <-deadline.C:
		case <-tick.C:
			present, err := setup.PairKeyPresent(env.home, id)
			if err != nil {
				_, _ = setup.RemovePairKey(env.home, id)
				return err
			}
			if present {
				continue
			}
			_, _ = fmt.Fprintln(out, pairedLine(env.home, id))
			return nil
		}
		removed, err := setup.RemovePairKey(env.home, id)
		if err != nil {
			return err
		}
		if !removed { // the phone took it in the last moment
			_, _ = fmt.Fprintln(out, pairedLine(env.home, id))
			return nil
		}
		if ctx.Err() != nil {
			_, _ = fmt.Fprintln(out, "Stopped. The code no longer works.")
			return nil
		}
		_, _ = fmt.Fprintln(out, "The code expired. Run matchblox pair again.")
		return nil
	}
}

// isTerminal tells if out and stdin are a terminal: only then the pair page
// can run.
func isTerminal(out io.Writer) bool {
	f, ok := out.(*os.File)
	return ok && term.IsTerminal(f.Fd()) && term.IsTerminal(os.Stdin.Fd())
}

// consolePair is the console's pair page on this machine: the key line in
// home's authorized_keys, and the notes of gate pair.
func consolePair(home, exe, hostKeyPath string, address func() string) *app.PairHost {
	return pairHost(home, exe, hostKeyPath, address, 22, setup.HostLabel(), true)
}

// pairHost is the pair page's side of this machine. In the console, an
// offer error says to press Enter; in matchblox pair it ends the command.
func pairHost(home, exe, hostKeyPath string, address func() string, port int, name string, console bool) *app.PairHost {
	return &app.PairHost{
		Offer: func(until time.Time) (string, string, error) {
			switch {
			case exe == "":
				return "", "", errors.New("cannot find this matchblox on the PATH")
			case home == "":
				return "", "", errors.New("cannot find your home directory")
			}
			id, link, err := setup.NewPairOffer(home, exe, address(), port, name, hostKeyPath, until)
			if err != nil && console { // the page retries on Enter, not on a new command
				err = errors.New(strings.Replace(err.Error(), "then matchblox pair again", "then press Enter", 1))
			}
			return id, link, err
		},
		Seen:    func(id string) (bool, string) { return setup.PairSeen(home, id) },
		Present: func(id string) (bool, error) { return setup.PairKeyPresent(home, id) },
		Remove: func(id string) (bool, error) {
			setup.RemovePairSeen(home, id)
			return setup.RemovePairKey(home, id)
		},
		Claim: func(id string) (app.PairDevice, bool) {
			d, ok := setup.TakeClaim(home, id)
			return app.PairDevice(d), ok
		},
		Devices: func() ([]app.PairDevice, error) {
			ds, err := setup.Devices(home)
			out := make([]app.PairDevice, len(ds))
			for i, d := range ds {
				out[i] = app.PairDevice(d)
			}
			return out, err
		},
		RemoveDevice: func(name string) (string, error) { return setup.RemoveDevice(home, name) },
		DeviceTTYs:   func() map[string]string { return setup.DeviceTTYs(home) },
		DeviceIn:     func(id string) (time.Time, string, bool) { return setup.DeviceIn(home, id) },
	}
}

// pairedLine names the phone that used the one-time key id, so a phone
// that is not yours is seen at once. With no claim, the line went some
// other way.
func pairedLine(home, id string) string {
	if d, ok := setup.TakeClaim(home, id); ok {
		return pairedText(d.Name, d.Fingerprint)
	}
	return "The code is gone, but no phone used it. Run matchblox pair again."
}

func pairedText(name, fingerprint string) string {
	return fmt.Sprintf("Paired %s (%s). Not your phone? Run matchblox devices rm %s.", name, fingerprint, fingerprint)
}
