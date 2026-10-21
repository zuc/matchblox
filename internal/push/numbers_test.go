package push

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/blox-eng/matchblox/internal/limits"
	"github.com/blox-eng/matchblox/internal/sample"
)

func openNumbers(t *testing.T, dir string) *Numbers {
	t.Helper()
	n, err := OpenNumbers(dir)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestASessionKeepsItsNumber(t *testing.T) {
	dir := t.TempDir()
	n := openNumbers(t, dir)
	a, b := n.Session("a"), n.Session("b")
	if a != 1 || b != 2 {
		t.Fatalf("numbers = %d, %d; want 1, 2", a, b)
	}
	if n.Session("a") != a {
		t.Fatal("same id, new number")
	}
	n2 := openNumbers(t, dir)
	if got := n2.Session("a"); got != a {
		t.Fatalf("after reopen a = %d, want %d", got, a)
	}
	if got := n2.Session("c"); got != 3 {
		t.Fatalf("after reopen c = %d, want 3", got)
	}
	if id, ok := n2.SessionOf(2); !ok || id != "b" {
		t.Fatalf("SessionOf(2) = %q, %v", id, ok)
	}
	if _, ok := n2.SessionOf(99); ok {
		t.Fatal("SessionOf(99) found")
	}
}

func TestANumberIsNeverUsedAgain(t *testing.T) {
	dir := t.TempDir()
	n := openNumbers(t, dir)
	if n.Session("a") != 1 {
		t.Fatal("a is not 1")
	}
	if n.Session("b") != 2 {
		t.Fatal("b is not 2")
	}
	if n.Session("a") != 1 {
		t.Fatal("a changed")
	}
	// After a restart, a session that ends and a new one in its pane: the
	// new one gets a new number, and the old one keeps its own.
	n = openNumbers(t, dir)
	if got := n.Session("c"); got != 3 {
		t.Fatalf("after reopen c = %d, want 3", got)
	}
	if n.Session("a") != 1 || n.Session("b") != 2 {
		t.Fatal("a number moved on reopen")
	}
}

// A number that cannot be written down is not handed out: after a restart
// it would go to another session.
func TestANumberThatCannotBeSavedIsNotGiven(t *testing.T) {
	dir := t.TempDir()
	n := openNumbers(t, dir)
	if n.Session("a") != 1 {
		t.Fatal("a is not 1")
	}
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	n.dir = filepath.Join(blocked, "sub") // MkdirAll fails under a file
	if got := n.Session("b"); got != 0 {
		t.Fatalf("unsaved b = %d, want 0", got)
	}
	if got := n.Account("anthropic", "x"); got != 0 {
		t.Fatalf("unsaved account = %d, want 0", got)
	}
	if n.Err() == nil {
		t.Fatal("no error after a failed save")
	}
	if n.Session("a") != 1 {
		t.Fatal("a known number is still given")
	}
	ss, _ := n.Names([]sample.Session{{SessionID: "a", Name: "api"}, {SessionID: "b", Name: "web"}}, nil)
	if len(ss) != 1 || ss[1] != "api" {
		t.Fatalf("names with a failed save = %v", ss)
	}
	n.dir = dir
	if got := n.Session("b"); got != 2 {
		t.Fatalf("b after the disk mends = %d, want 2", got)
	}
	if n.Err() != nil {
		t.Fatalf("error after a good save: %v", n.Err())
	}
	if got := openNumbers(t, dir).Session("b"); got != 2 {
		t.Fatalf("b after reopen = %d, want 2", got)
	}
}

func TestAccountsAreNumberedOnTheirOwn(t *testing.T) {
	dir := t.TempDir()
	n := openNumbers(t, dir)
	s := n.Session("a")
	x := n.Account("anthropic", "a@b.c · Max")
	y := n.Account("openai", "")
	if s != 1 || x != 1 || y != 2 {
		t.Fatalf("session %d, accounts %d, %d", s, x, y)
	}
	if n.Account("anthropic", "a@b.c · Max") != 1 {
		t.Fatal("account changed")
	}
	if openNumbers(t, dir).Account("openai", "") != 2 {
		t.Fatal("account lost on reopen")
	}
}

// Every current session and account has a number and a name, given or not
// before: the phone learns them all at one register.
func TestNamesNumberEveryCurrentSessionAndAccount(t *testing.T) {
	n := openNumbers(t, t.TempDir())
	n.Session("gone")
	live := n.Session("live")
	acc := n.Account("anthropic", "a@b.c · Max")
	n.Account("openai", "x")
	ss, as := n.Names(
		[]sample.Session{{SessionID: "live", Name: "api"}, {SessionID: "new", Name: "web"}, {SessionID: ""}},
		[]limits.Account{{Provider: "anthropic", Account: "a@b.c · Max"}, {Provider: "openai", Account: "y"}},
	)
	if len(ss) != 2 || ss[live] != "api" || ss[n.Session("new")] != "web" || n.Session("new") != 3 {
		t.Fatalf("sessions = %v", ss)
	}
	if len(as) != 2 || as[acc] != "anthropic a@b.c · Max" || as[n.Account("openai", "y")] != "openai y" {
		t.Fatalf("accounts = %v", as)
	}
}

// A numbers file that does not parse keeps push off: starting again at 1
// would hand old numbers to new sessions.
func TestANumbersFileThatDoesNotParseIsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := writeFile(dir, "{nope"); err != nil {
		t.Fatal(err)
	}
	if n, err := OpenNumbers(dir); err == nil || n != nil {
		t.Fatalf("OpenNumbers = %v, %v; want an error", n, err)
	}
}

func writeFile(dir, s string) error {
	return os.WriteFile(filepath.Join(dir, "numbers.json"), []byte(s), 0o600)
}
