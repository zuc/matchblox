package push

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// A relay that redirects does not get the event posted again elsewhere: the
// device token and grant stay with the relay that was configured.
func TestTheRelayDoesNotFollowARedirect(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		elsewhere.Add(1)
	}))
	defer other.Close()
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/push/send", http.StatusTemporaryRedirect)
	}))
	defer relay.Close()
	err := NewHTTPRelay(relay.URL, time.Second).Send(context.Background(), Event{Target: "ab", Kind: "device", Grant: "g", Payload: []byte("{}")})
	var re *RelayError
	if !errors.As(err, &re) || re.Code != http.StatusTemporaryRedirect {
		t.Fatalf("send = %v, want relay 307", err)
	}
	if n := elsewhere.Load(); n != 0 {
		t.Fatalf("the redirect target got %d posts", n)
	}
}
