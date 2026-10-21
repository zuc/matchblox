package limits

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// cursorUsage is GetCurrentPeriodUsage in the shape Cursor's API answers it,
// with synthetic figures.
const cursorUsage = `{"billingCycleStart":"1790000000000","billingCycleEnd":"1792678400000",` +
	`"planUsage":{"totalSpend":500,"includedSpend":500,"remaining":1500,"limit":2000,"autoPercentUsed":12.5,"apiPercentUsed":2,"totalPercentUsed":25},` +
	`"spendLimitUsage":{"totalSpend":1000,"pooledLimit":"100000","pooledUsed":1000,"limitType":"team"},"enabled":true}`

// TestCursorUsageIsItsMonth: Cursor's GetCurrentPeriodUsage, in the shape
// Cursor's API answers it (synthetic figures), is the month of the plan: the
// share of the included usage used, until the billing cycle ends.
func TestCursorUsageIsItsMonth(t *testing.T) {
	now := time.UnixMilli(1791000000000)
	r, err := ParseCursorUsage([]byte(cursorUsage), now)
	if err != nil {
		t.Fatal(err)
	}
	if !r.At.Equal(now) || len(r.Windows) != 1 {
		t.Fatalf("reading %+v", r)
	}
	w := r.Windows[0]
	start, end := time.UnixMilli(1790000000000), time.UnixMilli(1792678400000)
	if w.UsedPct != 25 || !w.ResetsAt.Equal(end) || w.Minutes != int(end.Sub(start).Minutes()) {
		t.Errorf("window %+v, want 25%% until %v", w, end)
	}
	a := Build("cursor", "you@example.com · Acme", SourceCursor, r, now, Quiet{})
	if u := a.Uses(now); a.State != Measured || len(u) != 1 || u[0].Name != "mo" || u[0].Pct != 25 {
		t.Errorf("account %+v uses %+v, want one measured mo of 25%%", a, u)
	}
}

func TestCursorUsageWithoutAPlanIsNoReading(t *testing.T) {
	if _, err := ParseCursorUsage([]byte(`{"enabled":false}`), time.Now()); err == nil {
		t.Error("no planUsage parsed, want an error")
	}
}

// TestCursorUsageAsksCursorsAPI: the call is the one Cursor's CLI makes, its
// token in the Authorization header only.
func TestCursorUsageAsksCursorsAPI(t *testing.T) {
	var got *http.Request
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		if status != http.StatusOK {
			http.Error(w, `{"code":"unauthenticated"}`, status)
			return
		}
		_, _ = w.Write([]byte(cursorUsage))
	}))
	defer srv.Close()
	b, err := FetchCursorUsage(context.Background(), srv.Client(), srv.URL, "tok")
	if err != nil || len(b) == 0 {
		t.Fatalf("fetch: %v %q", err, b)
	}
	if got.Method != http.MethodPost || got.URL.Path != "/aiserver.v1.DashboardService/GetCurrentPeriodUsage" ||
		got.Header.Get("Authorization") != "Bearer tok" || got.Header.Get("Connect-Protocol-Version") != "1" {
		t.Errorf("request %s %s %v", got.Method, got.URL.Path, got.Header)
	}
	status = http.StatusUnauthorized
	if _, err := FetchCursorUsage(context.Background(), srv.Client(), srv.URL, "tok"); err == nil {
		t.Error("401 fetched, want an error")
	}
}

// TestCursorUsageAsksAtMostEveryInterval: the sampler asks every 2s; the API
// is called once an interval, out of the sample, and a failure keeps the last
// reading.
func TestCursorUsageAsksAtMostEveryInterval(t *testing.T) {
	calls := make(chan struct{}, 10)
	fail := false
	u := &CursorUsage{
		Every: time.Minute,
		Token: func(context.Context) (string, error) { return "tok", nil },
		Fetch: func(_ context.Context, token string) ([]byte, error) {
			calls <- struct{}{}
			if fail {
				return nil, context.DeadlineExceeded
			}
			return []byte(cursorUsage), nil
		},
	}
	now := time.UnixMilli(1791000000000)
	if r := u.Reading(now); r != nil {
		t.Fatalf("the first sample waits for no call: %+v", r)
	}
	<-calls
	u.Wait()
	r := u.Reading(now.Add(time.Second))
	if r == nil || r.Windows[0].UsedPct != 25 {
		t.Fatalf("reading %+v, want the month's 25%%", r)
	}
	if len(calls) != 0 {
		t.Errorf("%d calls within the interval, want none", len(calls))
	}
	fail = true
	u.Reading(now.Add(2 * time.Minute))
	<-calls
	u.Wait()
	if r := u.Reading(now.Add(2*time.Minute + time.Second)); r == nil || r.Windows[0].UsedPct != 25 {
		t.Errorf("after a failed call: %+v, want the last reading", r)
	}
}

func TestCursorUsageWithoutATokenAsksNothing(t *testing.T) {
	u := &CursorUsage{
		Every: time.Minute,
		Token: func(context.Context) (string, error) { return "", context.Canceled },
		Fetch: func(context.Context, string) ([]byte, error) {
			t.Error("called the API with no token")
			return nil, nil
		},
	}
	u.Reading(time.Now())
	u.Wait()
	if r := u.Reading(time.Now()); r != nil {
		t.Errorf("reading %+v, want none", r)
	}
}
