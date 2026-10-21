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
	n := openNumbers(t, t.TempDir())
	if n.Session("a") != 1 {
		t.Fatal("a is not 1")
	}
	if n.Session("b") != 2 {
		t.Fatal("b is not 2")
	}
	if n.Session("a") != 1 {
		t.Fatal("a changed")
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

func TestNamesAreOnlyForNumbersGiven(t *testing.T) {
	n := openNumbers(t, t.TempDir())
	n.Session("gone")
	live := n.Session("live")
	acc := n.Account("anthropic", "a@b.c · Max")
	n.Account("openai", "x")
	ss, as := n.Names(
		[]sample.Session{{SessionID: "live", Name: "api"}, {SessionID: ""}},
		[]limits.Account{{Provider: "anthropic", Account: "a@b.c · Max"}},
	)
	if len(ss) != 1 || ss[live] != "api" {
		t.Fatalf("sessions = %v", ss)
	}
	if len(as) != 1 || as[acc] != "anthropic a@b.c · Max" {
		t.Fatalf("accounts = %v", as)
	}
}

func TestANumbersFileThatDoesNotParseStartsOver(t *testing.T) {
	dir := t.TempDir()
	if err := writeFile(dir, "{nope"); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenNumbers(dir); err != nil {
		t.Fatal(err)
	}
}

func writeFile(dir, s string) error {
	return os.WriteFile(filepath.Join(dir, "numbers.json"), []byte(s), 0o600)
}
