package push

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "push", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestHost8MatchesItsFixture: the tag is the first 8 hex characters of the
// SHA-256 in the fingerprint; the Swift tests read the same file.
func TestHost8MatchesItsFixture(t *testing.T) {
	var rows []struct{ Fingerprint, Host8 string }
	if err := json.Unmarshal(fixture(t, "host8.json"), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("host8.json has %d rows, want 3", len(rows))
	}
	for _, r := range rows {
		if got := Host8(r.Fingerprint); got != r.Host8 {
			t.Errorf("Host8(%q) = %q, want %q", r.Fingerprint, got, r.Host8)
		}
	}
}

// TestThePayloadsRoundTripTheirFixtures: each fixture reads into its type and
// writes back the same bytes, so Go and Swift agree on the wire.
func TestThePayloadsRoundTripTheirFixtures(t *testing.T) {
	for name, v := range map[string]any{
		"content-state.json":    new(ContentState),
		"buzz.json":             new(Buzz),
		"register-request.json": new(RegisterRequest),
		"register-reply.json":   new(RegisterReply),
	} {
		raw := fixture(t, name)
		if err := json.Unmarshal(raw, v); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var want bytes.Buffer
		if err := json.Compact(&want, raw); err != nil {
			t.Fatal(err)
		}
		if string(got) != want.String() {
			t.Errorf("%s:\n got %s\nwant %s", name, got, want.String())
		}
	}
}

// TestARegistrationHoldsTheRequestInline: the request's fields sit beside the
// device id and the time, not under a key.
func TestARegistrationHoldsTheRequestInline(t *testing.T) {
	var r Registration
	in := `{"device":"0123456789ab","device_token":"tok","grants":{},"notify_key":"AQ==","buzz":true,"at":"2026-10-21T10:00:00Z"}`
	if err := json.Unmarshal([]byte(in), &r); err != nil {
		t.Fatal(err)
	}
	if r.DeviceToken != "tok" || !r.Buzz || r.Device != "0123456789ab" {
		t.Fatalf("got %+v", r)
	}
	out, _ := json.Marshal(r)
	if string(out) != in {
		t.Errorf("got %s", out)
	}
}

// TestTheContentStateFixtureHasNoName: every string in a content state is a
// tag, a state, a window short name or a time; no name can leave the host.
func TestTheContentStateFixtureHasNoName(t *testing.T) {
	ok := regexp.MustCompile(`^([0-9a-f]{8}|burns|waits|done|out|quiet|5h|wk|mo|\d{4}-.*Z)$`)
	var doc any
	if err := json.Unmarshal(fixture(t, "content-state.json"), &doc); err != nil {
		t.Fatal(err)
	}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			if !ok.MatchString(x) {
				t.Errorf("string %q may carry a name", x)
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(doc)
}
