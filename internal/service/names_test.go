package service

import (
	"context"
	"maps"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/limits"
	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/push"
	"github.com/blox-eng/matchblox/internal/sample"
)

// The names act tells a phone that registers what each number is called
// now: every current session and account, a number given there if it had
// none. Without the numbers it names nothing, and it never touches the stoker.
func TestTheNamesActNamesTheNumbers(t *testing.T) {
	s := newTest(t, time.Hour)
	res := s.act(context.Background(), proto.Act{RecID: NamesAct})
	if res.Err != "" || res.Names == nil || len(res.Names.Sessions) != 0 || len(res.Names.Accounts) != 0 {
		t.Fatalf("without numbers: %+v", res)
	}

	n, err := push.OpenNumbers(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.PushNumbers = n
	sn, an := n.Session("sess-a"), n.Account("claude", "work")
	s.mu.Lock()
	s.cur.Sessions = []sample.Session{{Pane: "%1", Name: "api", SessionID: "sess-a"}, {Pane: "%2", Name: "new", SessionID: "sess-b"}}
	s.cur.Limits = []limits.Account{{Provider: "claude", Account: "work"}}
	s.mu.Unlock()
	res = s.act(context.Background(), proto.Act{RecID: NamesAct})
	if res.Err != "" || res.Names == nil ||
		!maps.Equal(res.Names.Sessions, map[int]string{sn: "api", n.Session("sess-b"): "new"}) || n.Session("sess-b") != 2 ||
		!maps.Equal(res.Names.Accounts, map[int]string{an: "claude work"}) {
		t.Fatalf("names = %+v", res.Names)
	}
}
