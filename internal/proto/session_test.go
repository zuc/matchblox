package proto

import (
	"errors"
	"strings"
	"testing"
)

func TestCheckName(t *testing.T) {
	for _, ok := range []string{"api-gw", "ships_flake", "m74", "A1"} {
		if err := CheckName(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "a.b", "a:b", "a b", "-a", "_a", "é", strings.Repeat("a", 41)} {
		if CheckName(bad) == nil {
			t.Errorf("%q passes", bad)
		}
	}
}

func TestCheckFirstNamesWhatItRefuses(t *testing.T) {
	for _, ok := range []string{DefaultHandoff, "fix it - then test"} {
		if err := CheckFirst(ok, "the handoff"); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for text, want := range map[string]string{
		"":                       "the handoff is empty",
		"--yolo":                 "the handoff does not start with -",
		"a\nb":                   "the handoff must be one line, without control keys",
		strings.Repeat("a", 501): "the handoff is longer than 500 characters",
	} {
		if err := CheckFirst(text, "the handoff"); err == nil || err.Error() != want {
			t.Errorf("%q: %v", text, err)
		}
	}
	if err := CheckFirst("-p", "the first message"); !errors.Is(err, ErrDash) {
		t.Errorf("a dash: %v", err)
	}
}

func TestNameOf(t *testing.T) {
	for in, want := range map[string]string{
		"feat/88-x":              "feat-88-x",
		"matchblox-74":           "matchblox-74",
		"my repo.git":            "my-repo-git",
		"--a//b--":               "a-b",
		strings.Repeat("ab", 30): strings.Repeat("ab", 20),
	} {
		if got := NameOf(in); got != want {
			t.Errorf("NameOf(%q) = %q, want %q", in, got, want)
		}
		if got := NameOf(in); CheckName(got) != nil {
			t.Errorf("NameOf(%q) = %q fails CheckName", in, got)
		}
	}
}
