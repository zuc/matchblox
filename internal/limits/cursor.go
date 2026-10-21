package limits

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// CursorReading is what Cursor's status line last said of the session in
// one pane: its chat, model and context use. Window 0: no turn yet.
type CursorReading struct {
	SessionID string    `json:"session_id"`
	Model     string    `json:"model"`
	Tokens    int       `json:"tokens"`
	Window    int       `json:"window"`
	At        time.Time `json:"at"`
}

// cursorInput is Cursor's status line JSON, only the fields we read.
type cursorInput struct {
	SessionID string `json:"session_id"`
	Model     struct {
		ID   string `json:"id"`
		Name string `json:"display_name"`
	} `json:"model"`
	ContextWindow struct {
		Tokens *int     `json:"total_input_tokens"`
		Size   *int     `json:"context_window_size"`
		Used   *float64 `json:"used_percentage"`
	} `json:"context_window"`
}

func (in cursorInput) model() string {
	if in.Model.Name != "" {
		return in.Model.Name
	}
	return in.Model.ID
}

// cursorPath is the file of the session in a pane. Pane ids repeat across
// tmux servers: the server's socket is in the name too.
func cursorPath(stateDir, socket, pane string) string {
	h := sha256.Sum256([]byte(socket + "\x00" + pane))
	return filepath.Join(stateDir, "cursor", hex.EncodeToString(h[:8])+".json")
}

// TapCursor keeps what Cursor's status line says of the session in pane. It
// writes only a change: Cursor runs its status line every 300ms. A status
// line outside tmux has no pane, and nothing to keep. A new pane removes the
// files of panes that have not written for Keep.
func TapCursor(input []byte, stateDir, socket, pane string, now time.Time) error {
	if pane == "" {
		return nil
	}
	var in cursorInput
	if err := json.Unmarshal(input, &in); err != nil {
		return err
	}
	r := CursorReading{SessionID: in.SessionID, Model: in.model(), At: now}
	if cw := in.ContextWindow; cw.Size != nil && *cw.Size > 0 {
		r.Window = *cw.Size
		switch {
		case cw.Tokens != nil:
			r.Tokens = *cw.Tokens
		case cw.Used != nil:
			r.Tokens = int(*cw.Used * float64(r.Window) / 100)
		}
	}
	if old, ok := ReadCursor(stateDir, socket, pane); ok {
		old.At = r.At
		if old == r {
			return nil
		}
	}
	path := cursorPath(stateDir, socket, pane)
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		pruneWindows(filepath.Dir(path), now)
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeFile(path, b)
}

// ReadCursor is the last reading of the session in pane.
func ReadCursor(stateDir, socket, pane string) (CursorReading, bool) {
	if pane == "" {
		return CursorReading{}, false
	}
	b, err := os.ReadFile(cursorPath(stateDir, socket, pane))
	if err != nil {
		return CursorReading{}, false
	}
	var r CursorReading
	if json.Unmarshal(b, &r) != nil {
		return CursorReading{}, false
	}
	return r, true
}

// CursorLine is what Cursor's own bar shows, "Auto · 5.7%": a status line
// takes its place.
func CursorLine(input []byte) string {
	var in cursorInput
	if json.Unmarshal(input, &in) != nil {
		return ""
	}
	line := in.model()
	if u := in.ContextWindow.Used; u != nil {
		pct := strconv.FormatFloat(*u, 'f', 1, 64) + "%"
		if line == "" {
			return pct
		}
		line += " · " + pct
	}
	return line
}
