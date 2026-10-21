package push

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/limits"
	"github.com/blox-eng/matchblox/internal/queue"
	"github.com/blox-eng/matchblox/internal/sample"
	"github.com/blox-eng/matchblox/internal/state"
	"github.com/blox-eng/matchblox/internal/stoker"
)

var t0 = time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC)

const fp = "SHA256:8Q94Er9GLB5qGPeqlE+5hCoHnmgKwAvlRvrZkV8Mnb0"

func doc(ss []sample.Session, q []queue.Item, as ...limits.Account) state.Doc {
	var d state.Doc
	d.Sessions, d.Queue, d.Limits = ss, q, as
	return d
}

func sess(id string, busy bool, started time.Time) sample.Session {
	return sample.Session{SessionID: id, Busy: busy, Started: started}
}

func TestAWaitComesBeforeABurn(t *testing.T) {
	n := openNumbers(t, t.TempDir())
	d := doc([]sample.Session{sess("burn", true, t0), sess("wait", false, t0.Add(-time.Hour))},
		[]queue.Item{{SessionID: "wait", State: queue.StateQuestion, Since: t0}})
	got := Build(d, fp, n, t0)
	if got.Top == nil || got.Top.State != "waits" || got.Top.N != n.Session("wait") {
		t.Fatalf("top = %+v", got.Top)
	}
	if got.Burn != 1 || got.Wait != 1 {
		t.Fatalf("burn %d wait %d", got.Burn, got.Wait)
	}
}

func TestTheOldestWaitComesFirst(t *testing.T) {
	n := openNumbers(t, t.TempDir())
	d := doc([]sample.Session{sess("new", false, t0), sess("old", false, t0)},
		[]queue.Item{
			{SessionID: "new", State: queue.StatePermission, Since: t0.Add(-time.Minute)},
			{SessionID: "old", State: queue.StatePermission, Since: t0.Add(-time.Hour)},
		})
	got := Build(d, fp, n, t0)
	if got.Top.N != n.Session("old") || got.Wait != 2 {
		t.Fatalf("top = %+v wait %d", got.Top, got.Wait)
	}
}

func TestTheLastPromptedBurnComesFirst(t *testing.T) {
	n := openNumbers(t, t.TempDir())
	d := doc([]sample.Session{sess("a", true, t0.Add(-time.Hour)), sess("b", true, t0)}, nil)
	got := Build(d, fp, n, t0)
	if got.Top.State != "burns" || got.Top.N != n.Session("b") || got.Burn != 2 {
		t.Fatalf("top = %+v", got.Top)
	}
}

func TestDoneAndOutHaveTheirStates(t *testing.T) {
	n := openNumbers(t, t.TempDir())
	d := doc([]sample.Session{sess("a", false, t0)},
		[]queue.Item{{SessionID: "a", State: queue.StateFinished, Since: t0}})
	if got := Build(d, fp, n, t0); got.Top.State != "done" || got.Wait != 0 {
		t.Fatalf("done: %+v wait %d", got.Top, got.Wait)
	}
	out := t0.Add(2 * time.Hour)
	s := sess("b", false, t0)
	s.Out = out
	got := Build(doc([]sample.Session{s}, nil), fp, n, t0)
	if got.Top.State != "out" || !got.Top.Out.Equal(out) {
		t.Fatalf("out: %+v", got.Top)
	}
}

func TestProgressGoesInStepsOf5(t *testing.T) {
	for in, want := range map[int]int{0: 0, 4: 0, 5: 5, 47: 45, 99: 95, 100: 100, 140: 100, -3: 0} {
		n := openNumbers(t, t.TempDir())
		s := sess("a", true, t0)
		s.Progress = &sample.Progress{Pct: in}
		got := Build(doc([]sample.Session{s}, nil), fp, n, t0)
		if got.Top.Pct != want {
			t.Errorf("pct %d -> %d, want %d", in, got.Top.Pct, want)
		}
	}
}

func TestQuietHasNoTop(t *testing.T) {
	n := openNumbers(t, t.TempDir())
	got := Build(doc([]sample.Session{sess("a", false, t0)}, nil), fp, n, t0)
	if got.Top != nil || got.Burn != 0 || got.Wait != 0 {
		t.Fatalf("got %+v", got)
	}
	if got.Accounts == nil {
		t.Fatal("accounts must be [] not null")
	}
}

func acct(name string, five, week float64, now time.Time, f *limits.Outlook) limits.Account {
	return limits.Account{Provider: "anthropic", Account: name, State: limits.Measured,
		Windows: []limits.Window{
			{UsedPct: week, Minutes: limits.WeekMinutes, ResetsAt: now.Add(5 * 24 * time.Hour)},
			{UsedPct: five, Minutes: limits.BoxMinutes, ResetsAt: now.Add(2 * time.Hour)},
		}, Forecast: f}
}

func TestUsageHasTheWindowsAndTheForecastOfEachAccount(t *testing.T) {
	n := openNumbers(t, t.TempDir())
	out := t0.Add(time.Hour)
	d := doc(nil, nil,
		acct("a", 62.4, 31, t0, &limits.Outlook{Lasts: true}),
		acct("b", 93, 71.6, t0, &limits.Outlook{Out: out}))
	got := Build(d, fp, n, t0)
	if len(got.Accounts) != 2 {
		t.Fatalf("accounts %+v", got.Accounts)
	}
	a, b := got.Accounts[0], got.Accounts[1]
	if a.N != n.Account("anthropic", "a") || len(a.Windows) != 2 || a.Windows[0].Name != "5h" ||
		a.Windows[0].Pct != 62 || a.Windows[1].Name != "wk" || a.Windows[1].Pct != 31 || !a.Lasts {
		t.Fatalf("a = %+v", a)
	}
	if b.Lasts || !b.RunsOut.Equal(out) || b.Windows[1].Pct != 72 {
		t.Fatalf("b = %+v", b)
	}
	if !a.Windows[0].Resets.Equal(t0.Add(2 * time.Hour)) {
		t.Fatalf("resets %v", a.Windows[0].Resets)
	}
}

func TestUsageSkipsWhatItCannotName(t *testing.T) {
	n := openNumbers(t, t.TempDir())
	a := limits.Account{Provider: "p", Account: "a", State: limits.Measured, Windows: []limits.Window{
		{UsedPct: 10, Minutes: 3 * 24 * 60}, {UsedPct: 20, Minutes: 30 * 24 * 60}}}
	un := limits.Account{Provider: "p", Account: "none", State: limits.Unmeasured}
	got := Build(doc(nil, nil, un, a), fp, n, t0)
	if len(got.Accounts) != 1 || len(got.Accounts[0].Windows) != 1 || got.Accounts[0].Windows[0].Name != "mo" {
		t.Fatalf("got %+v", got.Accounts)
	}
}

func TestStokerOnIsInTheState(t *testing.T) {
	n := openNumbers(t, t.TempDir())
	d := doc(nil, nil)
	if Build(d, fp, n, t0).Stoker {
		t.Fatal("no stoker is on")
	}
	d.Stoker = &stoker.View{Mode: stoker.Mode{On: true}}
	if !Build(d, fp, n, t0).Stoker {
		t.Fatal("stoker on is lost")
	}
}

func TestTheContentStateStaysUnder2KB(t *testing.T) {
	n := openNumbers(t, t.TempDir())
	var as []limits.Account
	for i := range 5 {
		a := acct(fmt.Sprintf("acct-%d", i), 99, 99, t0, &limits.Outlook{Out: t0.Add(time.Hour)})
		a.Windows = append(a.Windows, limits.Window{UsedPct: 88, Minutes: 30 * 24 * 60, ResetsAt: t0.Add(20 * 24 * time.Hour)})
		as = append(as, a)
	}
	s := sess("a", false, t0)
	s.Out = t0.Add(time.Hour)
	s.Progress = &sample.Progress{Pct: 95}
	d := doc([]sample.Session{s}, []queue.Item{{SessionID: "a", State: queue.StatePermission, Since: t0}}, as...)
	got := Build(d, fp, n, t0)
	if len(got.Accounts) != 4 {
		t.Fatalf("accounts = %d, want 4", len(got.Accounts))
	}
	for i := 1; i < len(got.Accounts); i++ {
		if got.Accounts[i-1].N >= got.Accounts[i].N {
			t.Fatal("accounts not by number")
		}
	}
	b, _ := json.Marshal(got)
	if len(b) >= 2048 {
		t.Fatalf("%d bytes", len(b))
	}
}

func TestBuildMatchesTheFixture(t *testing.T) {
	want, err := os.ReadFile("../../testdata/push/content-state.json")
	if err != nil {
		t.Fatal(err)
	}
	n := openNumbers(t, t.TempDir())
	for _, id := range []string{"s1", "s2", "s3"} {
		n.Session(id)
	}
	n.Account("anthropic", "a")
	n.Account("anthropic", "b")
	now := time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC)
	s3 := sess("s3", false, now)
	s3.Progress = &sample.Progress{Pct: 47}
	s3.Out = time.Date(2026, 10, 21, 16, 40, 0, 0, time.UTC)
	wk := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	h5 := time.Date(2026, 10, 21, 17, 0, 0, 0, time.UTC)
	mk := func(name string, f, w float64, o *limits.Outlook) limits.Account {
		return limits.Account{Provider: "anthropic", Account: name, State: limits.Measured, Forecast: o,
			Windows: []limits.Window{{UsedPct: f, Minutes: limits.BoxMinutes, ResetsAt: h5}, {UsedPct: w, Minutes: limits.WeekMinutes, ResetsAt: wk}}}
	}
	d := doc([]sample.Session{sess("s1", true, now), sess("s2", true, now), s3},
		[]queue.Item{{SessionID: "s3", State: queue.StatePermission, Since: now}},
		mk("a", 62, 31, &limits.Outlook{Lasts: true}),
		mk("b", 93, 71, &limits.Outlook{Out: time.Date(2026, 10, 21, 16, 15, 0, 0, time.UTC)}))
	d.Stoker = &stoker.View{Mode: stoker.Mode{On: true}}
	got, _ := json.Marshal(Build(d, "SHA256:"+"8Q94EmFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWE", n, now))
	var a, b bytes.Buffer
	if err := json.Compact(&a, want); err != nil {
		t.Fatal(err)
	}
	b.Write(got)
	if a.String() != b.String() {
		t.Fatalf("\n got %s\nwant %s", b.String(), a.String())
	}
}

func TestWaitAtIsTheNewestWait(t *testing.T) {
	n := openNumbers(t, t.TempDir())
	d := doc([]sample.Session{sess("a", true, t0), sess("b", true, t0)},
		[]queue.Item{
			{SessionID: "a", State: queue.StatePermission, Since: t0.Add(-time.Hour)},
			{SessionID: "b", State: queue.StateQuestion, Since: t0.Add(-time.Minute)},
			{SessionID: "a", State: queue.StateFinished, Since: t0},
		})
	got := Build(d, fp, n, t0)
	if !got.WaitAt.Equal(t0.Add(-time.Minute)) {
		t.Fatalf("WaitAt = %v", got.WaitAt)
	}
	if got := Build(doc([]sample.Session{sess("a", true, t0)}, nil), fp, n, t0); !got.WaitAt.IsZero() {
		t.Fatalf("no wait, WaitAt = %v", got.WaitAt)
	}
}
