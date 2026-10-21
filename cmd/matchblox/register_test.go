package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/push"
	"github.com/blox-eng/matchblox/internal/service"
	"github.com/blox-eng/matchblox/internal/setup"
	"github.com/blox-eng/matchblox/internal/transport"
)

const regID = "0123456789ab"

// homeWithPhoneID is a home whose one phone has the id regID.
func homeWithPhoneID(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	line := `restrict,pty,command="'` + testExe + `' gate phone ` + regID + `" ` + phoneKey1 + " matchblox:phone:Desk\n"
	if err := os.WriteFile(filepath.Join(home, ".ssh", "authorized_keys"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func noNames() (map[int]string, map[int]string) { return nil, nil }

func registerLine(t *testing.T, r push.RegisterRequest) string {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

func register(t *testing.T, home, id, in string, names func() (map[int]string, map[int]string)) string {
	t.Helper()
	var out bytes.Buffer
	if err := registerMain(home, strings.NewReader(in), &out, id, names); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestRegisterRepliesTheNames(t *testing.T) {
	home := homeWithPhoneID(t)
	names := func() (map[int]string, map[int]string) {
		return map[int]string{1: "api", 3: "docs"}, map[int]string{2: "claude work"}
	}
	before := time.Now()
	req := push.RegisterRequest{DeviceToken: "tok", Grants: map[string]string{"device:tok": "g"}, NotifyKey: []byte{1, 2}, Buzz: true,
		Island: &push.IslandReg{StartToken: "start", Channel: "chan"}}
	got := register(t, home, regID, registerLine(t, req), names)
	if want := `{"ok":true,"sessions":{"1":"api","3":"docs"},"accounts":{"2":"claude work"}}` + "\n"; got != want {
		t.Fatalf("reply = %s, want %s", got, want)
	}
	rs, err := setup.Registrations(home)
	if err != nil || len(rs) != 1 {
		t.Fatalf("registrations = %+v, %v", rs, err)
	}
	r := rs[0]
	if r.Device != regID || r.DeviceToken != "tok" || !r.Buzz || r.Island == nil || r.Island.Channel != "chan" || r.At.Before(before) {
		t.Fatalf("registration = %+v", r)
	}
	// No service: no names, still ok.
	if got := register(t, home, regID, registerLine(t, req), noNames); got != `{"ok":true,"sessions":{},"accounts":{}}`+"\n" {
		t.Fatalf("reply without names = %s", got)
	}
}

func TestRegisterKeepsOneRegistrationPerPhone(t *testing.T) {
	home := homeWithPhoneID(t)
	first := push.RegisterRequest{DeviceToken: "one", Buzz: true, Island: &push.IslandReg{StartToken: "s", Channel: "c"}}
	register(t, home, regID, registerLine(t, first), noNames)
	// A request without the island takes this phone's island off this host.
	register(t, home, regID, registerLine(t, push.RegisterRequest{DeviceToken: "two"}), noNames)
	rs, err := setup.Registrations(home)
	if err != nil || len(rs) != 1 || rs[0].DeviceToken != "two" || rs[0].Island != nil || rs[0].Buzz {
		t.Fatalf("registrations = %+v, %v", rs, err)
	}
}

func TestRegisterRefusesABadRequest(t *testing.T) {
	good := registerLine(t, push.RegisterRequest{DeviceToken: "tok"})
	for name, c := range map[string]struct{ id, in, reply string }{
		"too long":        {regID, `{"device_token":"` + strings.Repeat("x", 5000) + `"}` + "\n", `{"ok":false,"error":"bad request"}`},
		"bad json":        {regID, "hello\n", `{"ok":false,"error":"bad request"}`},
		"empty":           {regID, "", `{"ok":false,"error":"bad request"}`},
		"unterminated":    {regID, strings.Repeat("{", 5000), `{"ok":false,"error":"bad request"}`},
		"no device token": {regID, `{"buzz":true}` + "\n", `{"ok":false,"error":"no device token"}`},
		"unknown device":  {"aaaaaaaaaaaa", good, `{"ok":false,"error":"unknown device"}`},
	} {
		t.Run(name, func(t *testing.T) {
			home := homeWithPhoneID(t)
			if got := strings.TrimSpace(register(t, home, c.id, c.in, noNames)); got != c.reply {
				t.Fatalf("reply = %s, want %s", got, c.reply)
			}
			if rs, _ := setup.Registrations(home); len(rs) != 0 {
				t.Fatalf("registered: %+v", rs)
			}
		})
	}
}

func TestRegisterRefusesASlowPhone(t *testing.T) {
	old := registerReadTimeout
	registerReadTimeout = 50 * time.Millisecond
	t.Cleanup(func() { registerReadTimeout = old })
	home := homeWithPhoneID(t)
	r, w := io.Pipe()
	defer w.Close()
	var out bytes.Buffer
	if err := registerMain(home, r, &out, regID, noNames); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != `{"ok":false,"error":"too slow"}` {
		t.Fatalf("reply = %s", got)
	}
}

// The names come from the running service, by its names act.
func TestServiceNamesAsksTheService(t *testing.T) {
	sock := isolate(t)
	l, err := transport.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		nc, err := l.Accept()
		if err != nil {
			return
		}
		c := transport.NewConn(nc)
		defer c.Close()
		c.Send(proto.KindHello, "", proto.Hello{Version: proto.Version})
		if env, err := c.Recv(); err != nil || env.Kind != proto.KindHello {
			return
		}
		env, err := c.Recv()
		var a proto.Act
		if err != nil || env.Kind != proto.KindAct || json.Unmarshal(env.Body, &a) != nil || a.RecID != service.NamesAct {
			c.Send(proto.KindResult, env.ID, proto.Result{ActID: env.ID, Err: "wrong act"})
			return
		}
		c.Send(proto.KindResult, env.ID, proto.Result{ActID: env.ID, Names: &proto.Names{
			Sessions: map[int]string{4: "api"}, Accounts: map[int]string{1: "claude work"}}})
	}()
	ss, as := serviceNames(sock)
	if ss[4] != "api" || len(ss) != 1 || as[1] != "claude work" || len(as) != 1 {
		t.Fatalf("names = %v, %v", ss, as)
	}
}

func TestServiceNamesWithoutAServiceAreEmpty(t *testing.T) {
	if ss, as := serviceNames(isolate(t)); len(ss) != 0 || len(as) != 0 {
		t.Fatalf("names = %v, %v", ss, as)
	}
}
