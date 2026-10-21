package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultRelay is the relay the host sends through; a config value sets
// another (tests use a fake).
const DefaultRelay = "https://matchblox.sh"

// DefaultTimeout is how long one send may take before it is dropped.
const DefaultTimeout = 5 * time.Second

// Event is one push for the relay to send. Kind is device, start or channel,
// Target the device token, push-to-start token or channel id, and Grant the
// relay's grant for "<kind>:<target>". PushType is alert or liveactivity;
// Priority 10 is high, 5 low. Payload is the APNs JSON body.
type Event struct {
	Target   string
	Kind     string
	Grant    string
	PushType string
	Priority int
	Payload  []byte
}

// Relay sends one event; it never retries and never queues.
type Relay interface {
	Send(ctx context.Context, e Event) error
}

// RelayError is a send the relay answered with a status other than 2xx; a
// 410 on a start says the push-to-start token is no longer valid.
type RelayError struct{ Code int }

func (e *RelayError) Error() string { return fmt.Sprintf("relay %d", e.Code) }

// wireEvent is the JSON body of POST /push/send. The payload goes as the
// APNs body itself, not as a string, so the relay forwards it unchanged.
type wireEvent struct {
	Target   string          `json:"target"`
	Kind     string          `json:"kind"`
	Grant    string          `json:"grant"`
	PushType string          `json:"push_type"`
	Priority int             `json:"priority"`
	Payload  json.RawMessage `json:"payload"`
}

type httpRelay struct {
	url string
	c   *http.Client
}

// NewHTTPRelay posts each event to <base>/push/send. An empty base is
// DefaultRelay, a zero timeout DefaultTimeout.
func NewHTTPRelay(base string, timeout time.Duration) Relay {
	if base == "" {
		base = DefaultRelay
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &httpRelay{url: strings.TrimRight(base, "/") + "/push/send", c: &http.Client{Timeout: timeout}}
}

func (r *httpRelay) Send(ctx context.Context, e Event) error {
	body, err := json.Marshal(wireEvent{Target: e.Target, Kind: e.Kind, Grant: e.Grant,
		PushType: e.PushType, Priority: e.Priority, Payload: json.RawMessage(e.Payload)})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &RelayError{Code: resp.StatusCode}
	}
	return nil
}
