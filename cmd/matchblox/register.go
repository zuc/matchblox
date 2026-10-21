package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/push"
	"github.com/blox-eng/matchblox/internal/service"
	"github.com/blox-eng/matchblox/internal/setup"
	"github.com/blox-eng/matchblox/internal/transport"
)

const maxRegisterRequest = 4096

// registerReadTimeout is how long push register waits for the request line;
// tests shorten it.
var registerReadTimeout = 30 * time.Second

// pushCmd is `matchblox push register <id>`, which the gate runs for a phone
// key with the id of its line.
func pushCmd(args []string) error {
	if len(args) != 2 || args[0] != "register" {
		return errors.New("usage: matchblox push register <device id>; a phone runs it through the gate")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return registerMain(home, os.Stdin, os.Stdout, args[1], func() (map[int]string, map[int]string) {
		return serviceNames(transport.SocketPath())
	})
}

// registerMain reads one bounded request line from the phone whose device
// line has the id id, keeps it as that phone's one registration on this host,
// and replies the names of the numbers. A request without the island takes
// this phone's island off this host. It never echoes the request.
func registerMain(home string, in io.Reader, out io.Writer, id string, names func() (map[int]string, map[int]string)) error {
	reply := func(r any) error {
		b, _ := json.Marshal(r)
		_, err := fmt.Fprintf(out, "%s\n", b)
		return err
	}
	refuse := func(msg string) error {
		return reply(struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}{false, msg})
	}

	// stdin is a pipe from sshd, which has no read deadline: wait in a
	// goroutine, as gate pair does.
	lines := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReaderSize(io.LimitReader(in, maxRegisterRequest+1), maxRegisterRequest+1).ReadString('\n')
		lines <- line
	}()
	var line string
	timer := time.NewTimer(registerReadTimeout)
	defer timer.Stop()
	select {
	case line = <-lines:
	case <-timer.C:
		return refuse("too slow")
	}
	var req push.RegisterRequest
	if len(line) > maxRegisterRequest || json.Unmarshal([]byte(line), &req) != nil {
		return refuse("bad request")
	}
	if req.DeviceToken == "" {
		return refuse("no device token")
	}
	// The notify key is an X25519 public key; none is a phone without buzz.
	if len(req.NotifyKey) != 0 && len(req.NotifyKey) != 32 {
		return refuse("bad notify key")
	}
	err := setup.Register(home, push.Registration{Device: id, RegisterRequest: req, At: time.Now()})
	switch {
	case errors.Is(err, setup.ErrUnknownDevice):
		return refuse("unknown device")
	case err != nil:
		fmt.Fprintln(os.Stderr, "matchblox push register:", err)
		return refuse("write failed")
	}
	ss, as := names()
	if ss == nil {
		ss = map[int]string{}
	}
	if as == nil {
		as = map[int]string{}
	}
	return reply(push.RegisterReply{OK: true, Sessions: ss, Accounts: as})
}

// namesWait is how long push register waits for the service's names: the
// phone waits on the reply, in the foreground.
const namesWait = 5 * time.Second

// serviceNames asks the running service what each number is called now. No
// service, or one that does not answer within namesWait, names nothing.
func serviceNames(path string) (map[int]string, map[int]string) {
	got := make(chan *proto.Names, 1)
	go func() {
		res, err := doAct(path, proto.Act{RecID: service.NamesAct})
		if err != nil || res.Err != "" {
			res.Names = nil
		}
		got <- res.Names
	}()
	timer := time.NewTimer(namesWait)
	defer timer.Stop()
	select {
	case n := <-got:
		if n != nil {
			return n.Sessions, n.Accounts
		}
	case <-timer.C:
	}
	return map[int]string{}, map[int]string{}
}
