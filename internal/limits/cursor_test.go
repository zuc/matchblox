package limits

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func cursorStatus(t *testing.T, file string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "hooks", file))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

const cursorSocket = "/tmp/tmux-501/default"

// TestCursorTapKeepsTheContextOfItsPane: Cursor's status line, as Cursor
// sent it on a live machine, gives a session's context use; the tap keeps it
// by the pane the status line runs in.
func TestCursorTapKeepsTheContextOfItsPane(t *testing.T) {
	dir, now := t.TempDir(), time.Date(2026, 10, 21, 20, 0, 0, 0, time.UTC)
	if err := TapCursor(cursorStatus(t, "cursor-statusline.json"), dir, cursorSocket, "%70", now); err != nil {
		t.Fatal(err)
	}
	r, ok := ReadCursor(dir, cursorSocket, "%70")
	if !ok {
		t.Fatal("no reading")
	}
	want := CursorReading{SessionID: "151e5fee-2111-4149-a65e-53a27734b16a", Model: "Auto", Tokens: 14592, Window: 256000, At: now}
	if r != want {
		t.Errorf("reading %+v, want %+v", r, want)
	}
	if _, ok := ReadCursor(dir, cursorSocket, "%71"); ok {
		t.Error("another pane has a reading")
	}
	if _, ok := ReadCursor(dir, "/tmp/tmux-501/other", "%70"); ok {
		t.Error("the pane of another server has a reading")
	}
}

// TestCursorTapBeforeTheFirstTurnIsFresh: before its first turn Cursor
// sends no context: the reading has the model and no window.
func TestCursorTapBeforeTheFirstTurnIsFresh(t *testing.T) {
	dir, now := t.TempDir(), time.Now()
	if err := TapCursor(cursorStatus(t, "cursor-statusline-fresh.json"), dir, cursorSocket, "%70", now); err != nil {
		t.Fatal(err)
	}
	r, ok := ReadCursor(dir, cursorSocket, "%70")
	if !ok || r.Model != "Auto" || r.Tokens != 0 || r.Window != 0 {
		t.Errorf("reading %+v %v, want the model and no context", r, ok)
	}
}

// TestCursorTapWritesOnlyAChange: Cursor runs its status line every 300ms;
// the same reading is not written again.
func TestCursorTapWritesOnlyAChange(t *testing.T) {
	dir, now := t.TempDir(), time.Now()
	in := cursorStatus(t, "cursor-statusline.json")
	if err := TapCursor(in, dir, cursorSocket, "%70", now); err != nil {
		t.Fatal(err)
	}
	if err := TapCursor(in, dir, cursorSocket, "%70", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if r, _ := ReadCursor(dir, cursorSocket, "%70"); !r.At.Equal(now) {
		t.Errorf("at %v, want the first write's %v", r.At, now)
	}
}

func TestCursorTapWithoutAPaneKeepsNothing(t *testing.T) {
	dir := t.TempDir()
	if err := TapCursor(cursorStatus(t, "cursor-statusline.json"), dir, cursorSocket, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if es, _ := os.ReadDir(filepath.Join(dir, "cursor")); len(es) != 0 {
		t.Errorf("%d files, want none", len(es))
	}
}

// TestCursorLineIsCursorsOwnBar: a status line replaces Cursor's own bar,
// so the tap prints what that bar said.
func TestCursorLineIsCursorsOwnBar(t *testing.T) {
	for file, want := range map[string]string{"cursor-statusline.json": "Auto · 5.7%", "cursor-statusline-fresh.json": "Auto"} {
		if got := CursorLine(cursorStatus(t, file)); got != want {
			t.Errorf("%s: %q, want %q", file, got, want)
		}
	}
}
