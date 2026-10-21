package limits

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// CursorAPI is where Cursor's CLI asks for its plan usage.
const CursorAPI = "https://api2.cursor.sh"

// cursorUsagePath is GetCurrentPeriodUsage of Cursor's DashboardService, in
// the Connect protocol: a POST of JSON.
const cursorUsagePath = "/aiserver.v1.DashboardService/GetCurrentPeriodUsage"

// FetchCursorUsage asks Cursor's API for the plan usage of the login token
// is of, as Cursor's CLI does for /usage. The token goes in the
// Authorization header and nowhere else.
func FetchCursorUsage(ctx context.Context, client *http.Client, api, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api+cursorUsagePath, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }() // a body read to its end loses nothing on close
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cursor usage: %s", resp.Status)
	}
	return b, nil
}

// ParseCursorUsage reads GetCurrentPeriodUsage: the month of the plan is one
// window, the share of the included usage used until the billing cycle
// ends, as /usage shows it under Included.
func ParseCursorUsage(b []byte, now time.Time) (*Reading, error) {
	var f struct {
		Start string `json:"billingCycleStart"` // unix milliseconds, as a string
		End   string `json:"billingCycleEnd"`
		Plan  *struct {
			Used float64 `json:"totalPercentUsed"`
		} `json:"planUsage"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	if f.Plan == nil {
		return nil, errors.New("cursor usage: no plan usage")
	}
	start, _ := strconv.ParseInt(f.Start, 10, 64)
	end, _ := strconv.ParseInt(f.End, 10, 64)
	w := Window{UsedPct: f.Plan.Used}
	if end > 0 {
		w.ResetsAt = time.UnixMilli(end)
		if start > 0 && end > start {
			w.Minutes = int(time.Duration(end-start) * time.Millisecond / time.Minute)
		}
	}
	return &Reading{At: now, Windows: []Window{w}}, nil
}

// CursorUsage keeps Cursor's plan usage, asked of its API at most once an
// interval and never in the caller's time: Reading returns the last one and
// asks again in the background when it is due. A failed call keeps the last
// reading.
type CursorUsage struct {
	Every time.Duration
	// Token is Cursor's login token, read when a call is due and dropped
	// after it.
	Token func(context.Context) (string, error)
	Fetch func(ctx context.Context, token string) ([]byte, error)

	mu    sync.Mutex
	last  *Reading
	asked time.Time
	wg    sync.WaitGroup
}

// cursorCallTimeout bounds one call: the token read and the API's answer.
const cursorCallTimeout = 15 * time.Second

func (u *CursorUsage) Reading(now time.Time) *Reading {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.asked.IsZero() || now.Sub(u.asked) >= u.Every {
		u.asked = now
		u.wg.Add(1)
		go u.ask(now)
	}
	return u.last
}

func (u *CursorUsage) ask(now time.Time) {
	defer u.wg.Done()
	ctx, cancel := context.WithTimeout(context.Background(), cursorCallTimeout)
	defer cancel()
	token, err := u.Token(ctx)
	if err != nil || token == "" {
		return
	}
	b, err := u.Fetch(ctx, token)
	if err != nil {
		return
	}
	r, err := ParseCursorUsage(b, now)
	if err != nil {
		return
	}
	u.mu.Lock()
	u.last = r
	u.mu.Unlock()
}

// Wait waits for a call in the background to end.
func (u *CursorUsage) Wait() { u.wg.Wait() }
