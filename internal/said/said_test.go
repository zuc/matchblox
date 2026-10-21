package said

import (
	"strings"
	"testing"
)

// The screens are the agents' own words, read from their binaries and live
// panes: Codex and OpenCode ask for approval in these words.
func TestScreen(t *testing.T) {
	for _, c := range []struct {
		name, screen, line string
		kind               Kind
	}{
		{"codex approval", `
› Run the tests
• I'll run the test suite.

  Would you like to run the following command?

  $ go test ./...

› 1. Yes, proceed (y)
  2. Yes, and don't ask again for this command (a)
  3. No, and tell Codex what to do differently (esc)

  Press enter to confirm or esc to cancel
`, "Would you like to run the following command?", Permits},
		// Cursor's CLI, captured on a live machine.
		{"cursor approval", `
  Run the shell command: touch second.txt && date
  Running that command now.
  $ touch second.txt && date Waiting for approval...
────────────────────────────────────────────────────────────
 $  touch second.txt && date in .
 Run this command?
 Not in allowlist: date, touch
  → Run (once) (y)
    Add Shell(touch), Shell(date) to allowlist? (tab)
    Run Everything (shift+tab)
    Skip & tell the agent what to do instead (esc or n)
`, "Run this command?", Permits},
		{"cursor trust prompt", `
  ╭──────────────────────────────────────────────────────────────────────────╮
  │                                                                          │
  │  Do you trust the contents of this directory?                            │
  │                                                                          │
  │    /work/app                                                             │
  │                                                                          │
  │  ▶ [a] Trust this workspace                                              │
  │    [q] Quit                                                              │
  │                                                                          │
  │  Use arrow keys to navigate, Enter to select, or press the key shown     │
  │                                                                          │
  ╰──────────────────────────────────────────────────────────────────────────╯
`, "Do you trust the contents of this directory?", Permits},
		{"cursor at rest", `
  Done. second.txt was created.
 ▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄
  → Add a follow-up
 ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
  Auto · 5.7%
  /work/app · main
`, "Done. second.txt was created.", None},
		{"cursor before its first turn", `
  Cursor Agent
  v2026.10.19-6bbf2c4
  Tip: Use /plan to plan execution and reach the right outcome faster.
 ▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄
  → Plan, search, build anything
 ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
  Auto
  /work/app · main
`, "Tip: Use /plan to plan execution and reach the right outcome faster.", None},
		{"opencode permission", `
┃  Permission required
┃  $ rm -rf build
┃
┃  Allow once   Allow always   Reject
   ctrl+f fullscreen  ⇆ select  enter confirm
`, "Permission required", Permits},
		{"claude permission", `
 Bash command
   rm -rf build
 Do you want to proceed?
 ❯ 1. Yes
   2. No, and tell Claude what to do differently (esc)
`, "Do you want to proceed?", Permits},
		{"aider confirm", `
Add main.go to the chat? (Y)es/(N)o/(D)on't ask again [Yes]:
`, "Add main.go to the chat? (Y)es/(N)o/(D)on't ask again [Yes]:", Permits},
		{"the agent asks in its reply", `
⏺ I changed the parser and the tests pass.
  Should I also update the docs?

╭──────────────────────────────────────────╮
│ >                                        │
╰──────────────────────────────────────────╯
  ? for shortcuts
`, "Should I also update the docs?", Asks},
		{"finished, nothing asked", `
  Done. All 42 tests pass.
                                           ┃
                                           ┃  Ask anything... "Fix broken tests"
                                           ╹▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
                                     ctrl+t variants  tab agents  ctrl+p commands
  /home/u/app:main                                                          1.1.48
`, "Done. All 42 tests pass.", None},
		{"an old question further up", `
  Want me to continue?
  Step one: read the parser.
  Step two: split the tokens.
  Step three: test the edges.
  Step four: fix the edges.
  Step five: run the suite.
  Step six: read the output.
  Step seven: fix the lint.
  Step eight: write the notes.
  Finished the refactor.
`, "Finished the refactor.", None},
		{"only key hints", "\n  ? for shortcuts\n", "", None},
		{"the person's own question, echoed by Codex", `
› Why does the search test fail?

• The fixture uses the old index path. I fixed it.
`, "The fixture uses the old index path. I fixed it.", None},
		{"the person's question in OpenCode", `
┃  Why is the build slow?

  The cache was cold. It is warm now.
`, "The cache was cold. It is warm now.", None},
		{"a draft in the input box", `
  Done. The tests pass.
╭──────────────────────────────────────────╮
│ > why is it slow?                        │
╰──────────────────────────────────────────╯
`, "Done. The tests pass.", None},
	} {
		line, kind := Screen(c.screen)
		if line != c.line || kind != c.kind {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", c.name, line, kind, c.line, c.kind)
		}
	}
}

// TestReply: Claude Code ends each reply with the progress line, so the
// question sits above it. A reply that asks waits for the person.
func TestReply(t *testing.T) {
	for _, c := range []struct {
		name, text, line string
		asks             bool
	}{
		{"asks above the progress line", "I fixed the parser.\n\n**Shall I open the PR?**\n\nProgress [██████░░] review", "Shall I open the PR?", true},
		{"asks mid-line", "Both are ready. Want me to merge? I can also tag it.\n\nProgress [████████] done", "Both are ready. Want me to merge? I can also tag it.", true},
		{"a numbered choice", "Two ways:\n1. Raise it to 40\n2. Keep 28\n\nWhich one do you want?", "Which one do you want?", true},
		{"done, nothing asked", "All 42 tests pass and the PR is merged.\n\nProgress [████████] done", "All 42 tests pass and the PR is merged.", false},
		{"a heading is not a question", "## What changed?\n- the parser\n- the tests\nAll green.\n\nProgress [████████] done", "All green.", false},
		{"a question long answered", "Why did it fail?\n" + strings.Repeat("It read the wrong file.\n", 9) + "Fixed now.", "Fixed now.", false},
	} {
		line, asks := Reply(c.text)
		if line != c.line || asks != c.asks {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", c.name, line, asks, c.line, c.asks)
		}
	}
}

// TestControlKeysNeverReachTheConsole: a reply or a screen is not trusted;
// an escape in it would drive the person's terminal.
func TestControlKeysNeverReachTheConsole(t *testing.T) {
	screen := func(s string) (string, bool) { l, k := Screen(s); return l, k != None }
	for _, read := range []func(string) (string, bool){Reply, screen} {
		line, _ := read("Shall I \x1b]52;c;eHg=\x07open\tit?")
		if strings.ContainsFunc(line, func(r rune) bool { return r < 0x20 || r == 0x7f }) || line != "Shall I open it?" {
			t.Fatalf("line %q", line)
		}
	}
}

func TestDraft(t *testing.T) {
	for _, c := range []struct{ screen, want string }{
		{"⏺ Done.\n────\n> \n────\n  ? for shortcuts\n", ""},
		{"⏺ Done.\n╭────╮\n│ >  │\n╰────╯\n", ""},
		{"⏺ Done.\n────\n> half a prompt\n────\n  ? for shortcuts\n", "half a prompt"},
		{"╭────────╮\n│ > fix the test │\n╰────────╯\n", "fix the test"},
		{"> an old turn\n⏺ Done.\n❯ go on\n", "go on"},
		{"────\n> Try \"refactor the parser\"\n────\n", ""},
		{"no prompt here\n", ""},
	} {
		if got := Draft(c.screen); got != c.want {
			t.Errorf("Draft(%q) = %q, want %q", c.screen, got, c.want)
		}
	}
}

func TestPlain(t *testing.T) {
	in := "## Done\n\nThe **refresh** endpoint and `rotate()` are in, see __notes__.\n\n" +
		"| Step | State |\n|---|:--:|\n| tests | pass |\n\n- one\n  * two\n```go\nx := 1\n```\nA 2*3*4 sum stays."
	want := "Done\n\nThe refresh endpoint and rotate() are in, see notes.\n\n" +
		"Step   State\ntests   pass\n\n• one\n  • two\nx := 1\nA 2*3*4 sum stays."
	if got := Plain(in); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

// TestDraftLeavesOutClaudeCodesSuggestion: #82, Claude Code draws its
// prompt suggestion in grey in an empty field. It is no draft: a compact or
// a send may replace it. What a person typed is drawn in the default
// colour, and a grey completion after it is not part of it.
func TestDraftLeavesOutClaudeCodesSuggestion(t *testing.T) {
	frame := "\x1b[2m────────────────────\x1b[22m\n"
	for in, want := range map[string]string{
		frame + "\x1b[39m❯ \x1b[2madd the ci label to 4611\x1b[22m\n" + frame:                             "",
		frame + "\x1b[38;5;246m❯ \x1b[38;5;244mcheck why #4602 prod deploy was skipped\x1b[39m\n" + frame: "",
		frame + "\x1b[39m❯ \x1b[90mrun the tests\x1b[39m\n" + frame:                                       "",
		frame + "\x1b[39m❯ \x1b[38;2;128;128;128mrun the tests\x1b[39m\n" + frame:                         "",
		frame + "\x1b[39m❯ add the ci label\x1b[0m\n" + frame:                                             "add the ci label",
		frame + "\x1b[39m❯ add the\x1b[2m ci label to 4611\x1b[22m\n" + frame:                             "add the",
		frame + "\x1b[39m❯ \x1b[1mbold is typed\x1b[0m\n" + frame:                                         "bold is typed",
	} {
		if got := Draft(in); got != want {
			t.Errorf("Draft(%q) = %q, want %q", in, got, want)
		}
	}
}
