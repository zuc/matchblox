# Cursor CLI Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cursor's CLI (`agent` / `cursor-agent`) becomes a first-class matchblox agent: detected in tmux, queued from its own hooks, started by `matchblox new` and the stoker.

**Architecture:**
- Detection: one rule in `Sampler.agentName`. It maps Cursor's node launcher to the configured name `cursor-agent`.
- State: a Cursor-specific parser in `internal/hooks` turns Cursor payloads into the existing `hooks.Event` names, tagged `Agent: "cursor-agent"`. The queue joins tagged events only to Cursor sessions, and reads permits from the pane for hooked Cursor sessions.
- Setup: a new door writes a pinned five-event `~/.cursor/hooks.json`.
- Launch: `cursor-agent` is added to the existing per-agent lists.

**Tech Stack:** Go 1.26, the standard library only (no new dependencies).

**Spec:** `design/0009-cursor-agent.md`. Built with deviations: see its section "Revised after the live run". Read it first. Its "Live run" section is the source of truth for payloads, process argv, and screen text.

## Global Constraints

- The agent's name is `cursor-agent` everywhere (config `agents`, `--agent`, UI, stoker brief). `cursor` is accepted as an alias only where a person types an agent (`matchblox new --agent`, `proto.NewSession`).
- Cursor's `hooks.json` gets exactly these events: `sessionStart`, `sessionEnd`, `beforeSubmitPrompt`, `stop`, `postToolUse`. Never `notification`, `preToolUse`, or any unlisted name: one invalid key silently disables every hook.
- The Cursor hook path prints nothing on stdout, ever: no guard output, no progress prompt, no `followup_message`.
- The Claude hook path, Claude's queue behaviour, and Codex/OpenCode behaviour do not change, except that `hooks.Parse` now rejects payloads carrying `cursor_version`.
- Match the surrounding code: package doc comments, sentence-style comments, `Test<Sentence>` test names, fixtures under `testdata/`.
- Every user-facing doc change goes to `docs/*.md` and the CHANGELOG `## [Unreleased]` section (Lint checks the changelog: `.github/scripts/changelog-guard.sh`).
- Run `go test ./...` and `golangci-lint run` (config `.golangci.yml`) before each commit.
- The progress prompt (`hooks.ProgressPrompt`) is not given to Cursor in this pass. The spec allowed it only if `sessionStart` takes `additional_context`, which is unverified, and the Cursor path prints nothing.

## Review Focus

1. **Stray `stop` from `agent -p` in a non-Cursor pane.** It must never mark that pane's Codex/Claude session done. Test in Task 3.
2. **The user already has a `~/.cursor/hooks.json` with their own hooks, or with `"version"` missing.** Their entries must survive byte-for-byte, and nothing is added twice on a second open. Test in Task 4.
3. **Cursor's background `worker-server` and `agent -p` processes.** These must never become sessions, including after `-p` exits and the worker is re-parented to launchd/init. Test in Task 1.
4. **A Cursor `stop` with `transcript_path: null`, or a transcript that is missing or huge.** The hook still delivers a Stop within its time budget, and the message may be empty. Test in Task 2.
5. **The Claude-compat path.** A Cursor payload that arrives at `matchblox hook Stop` (Claude's command) is dropped, not parsed as Claude's. Test in Task 2.

---

### Task 1: Detect Cursor's `agent` launcher

argv[0] `…/cursor-agent` is already detected (`internal/sample/agents_test.go:58`). The `agent` launcher is not: its argv is `[/home/u/.local/bin/agent --use-system-ca /home/u/.local/share/cursor-agent/versions/2026.10.19-6bbf2c4/index.js --trust]` with comm `node`. The worker is `[…/versions/<v>/node …/versions/<v>/index.js worker-server]`.

**Files:**
- Modify: `internal/sample/sampler.go` (`agentName`, around 720-745)
- Test: `internal/sample/agents_test.go`

**Interfaces:**
- Produces: `const CursorAgent = "cursor-agent"` in `internal/sample` (exported; Tasks 3, 5 and 6 use `sample.CursorAgent`). Sessions found by the `agent` launcher have `Agent == sample.CursorAgent` and `Name == "cursor-agent"`.

- [ ] **Step 1: Write the failing test** `TestCursorsAgentLauncherIsCursorAndItsWorkerIsNot` in `agents_test.go`, in the style of `TestEveryAgentInAPaneIsASession`, with `smp.Agents = []string{"claude", "cursor-agent"}`:
  - pane `%20`: pid 900, comm `node`, argv `["/home/u/.local/bin/agent", "--use-system-ca", "/home/u/.local/share/cursor-agent/versions/2026.10.19-6bbf2c4/index.js", "--trust"]`. It must become a session with `Agent == "cursor-agent"`.
  - pane `%21`: pid 910, comm `node`, argv `["/home/u/.local/share/cursor-agent/versions/2026.10.19-6bbf2c4/node", "/home/u/.local/share/cursor-agent/versions/2026.10.19-6bbf2c4/index.js", "worker-server"]`, ppid 1. It must not be a session.
  - pane `%22`: pid 920, comm `node`, argv `["/usr/bin/agent", "serve"]` (some other tool called agent). It must not be a session.
  - A second sampler with `Agents = []string{"claude"}` must see none of the three. Listing in config still gates detection.
- [ ] **Step 2: Run** `go test ./internal/sample -run TestCursorsAgentLauncher -v`. Expected: FAIL, `%20` is missing.
- [ ] **Step 3: Implement.** Add `func cursorLauncher(argv []string) bool` in `sampler.go`. It returns true when some argument ends in `/index.js`, contains `/cursor-agent/versions/`, and no argument is `worker-server`. In `agentName`, after the existing argv read leaves `name == ""`, set `name = CursorAgent` when `s.isAgent(CursorAgent) && cursorLauncher(argv)`. Extend the `agentName` doc comment with one sentence on Cursor's launcher.
- [ ] **Step 4: Run** `go test ./internal/sample/...`. Expected: PASS, including the existing `TestEveryAgentInAPaneIsASession`.
- [ ] **Step 5: Commit** `feat: matchblox sees a Cursor session started as agent`

### Task 2: Parse Cursor's hook payloads

**Files:**
- Create: `internal/hooks/cursor.go`
- Create fixtures: `testdata/hooks/cursor-{sessionStart,beforeSubmitPrompt,postToolUse,stop-completed,stop-aborted,stop-error,sessionEnd}.json`. These are the payloads from the spec's live run, with `user_email` set to `you@example.com`, `workspace_roots` set to `["/work/app"]`, and `transcript_path` pointing at the fixture below or `null` as noted.
- Create fixture: `testdata/hooks/cursor-transcript.jsonl`. It holds three lines: a user line, an assistant line whose text ends `Shall I also push it?`, and `{"type":"turn_ended","status":"success"}`.
- Modify: `internal/hooks/hook.go`: add the `Agent` field to `Event`, add `cursor_version` to `input`, and reject Cursor payloads in `Parse`.
- Test: `internal/hooks/cursor_test.go`, `internal/hooks/hooks_test.go`

**Interfaces:**
- Produces:
  - `Event.Agent string` with json tag `agent,omitempty`. It is `""` for Claude and `"cursor-agent"` for Cursor. Use the literal `"cursor-agent"` in a package const `CursorAgent`, because `hooks` must not import `sample`. `internal/arch_test.go` pins the import direction.
  - `func ParseCursor(stdin io.Reader, env func(string) string) (Event, error)`
  - `var ErrCursor = errors.New("hooks: a Cursor payload on Claude Code's hook")`
  - `var CursorEvents = []string{"sessionStart", "sessionEnd", "beforeSubmitPrompt", "stop", "postToolUse"}`. Task 4 writes exactly this list.

- [ ] **Step 1: Write the failing tests** in `cursor_test.go`:
  - `TestCursorEventsAreTheFiveCursorAccepts`: `CursorEvents` equals the five names in order. Its comment says why: one invalid key disables Cursor's whole file.
  - `TestParseCursorMapsEachEvent` is a table over the fixtures. Each row gives the fixture, the expected `Name`, and the expected `Message`/`Asks`:
    - `sessionStart` → `SessionStart`
    - `beforeSubmitPrompt` → `UserPromptSubmit`
    - `postToolUse` → `PostToolUse`, with `ToolUseID == "dd61bbbb-d9e0-40e9-9460-e42bf6eafaa0"`
    - `stop-completed` → `Stop`, with `Message == "Shall I also push it?"` and `Asks == true` (read from the transcript fixture through `said.Reply`)
    - `stop-aborted` → `UserPromptSubmit`
    - `stop-error` → `UserPromptSubmit`
    - `sessionEnd` → `SessionEnd`

    Every row also checks `SessionID == "19bece93-9e50-4441-b026-46d1cbf6e177"`, `Agent == "cursor-agent"`, `Pane == "%4"`, and `Cwd == "/work/app"` (from `workspace_roots[0]`).
  - `TestParseCursorStopWithoutATranscriptStillStops`: `stop-completed` with `transcript_path: null` gives `Name == "Stop"` and `Message == ""`.
  - `TestParseCursorReadsOnlyTheTranscriptsTail`: given a 5 MB transcript (written in the test to `t.TempDir()`) whose last assistant line is `done.`, `Message == "done."`. Reuse whatever tail reader `internal/sample` uses for Claude transcripts if it is importable without a cycle. Otherwise read the last 64 KiB.
  - `TestParseCursorDropsUnknownEvents`: `{"hook_event_name":"afterAgentThought","session_id":"x"}` returns an error.

  In `hooks_test.go`, `TestParseDropsACursorPayload`: `Parse` of `cursor-stop-completed.json` with name `Stop` returns `ErrCursor`.
- [ ] **Step 2: Run** `go test ./internal/hooks -v`. Expected: FAIL, undefined `ParseCursor`.
- [ ] **Step 3: Implement** `ParseCursor` in `cursor.go`.
  - It decodes `session_id` (falling back to `conversation_id`), `hook_event_name`, `status`, `transcript_path`, `workspace_roots`, and `tool_use_id`, under the same 1 MiB limit as `Parse`.
  - It maps names per the spec's decision list, and `ErrNoSession` applies as in `Parse`.
  - For a completed `stop`, it reads the last assistant text from the transcript tail and puts it through `said.Reply` and `cut`, as `Parse` does for Claude's `last_assistant_message`.

  In `Parse`, return `ErrCursor` when `input.CursorVersion != ""`.
- [ ] **Step 4: Run** `go test ./internal/hooks/...`. Expected: PASS.
- [ ] **Step 5: Commit** `feat: matchblox reads Cursor's hook events`

### Task 3: `matchblox hook --cursor`, and a queue that joins Cursor events only to Cursor sessions

**Files:**
- Modify: `cmd/matchblox/main.go`. At line 133, route `hook --cursor` to a new `hookCursor`. `hook` itself stays as it is.
- Modify: `internal/queue/queue.go` (`Merge`, around 164-215, and `WithAsks`, around 238-265, where `byPane` is used)
- Test: `cmd/matchblox/hook_test.go`, `internal/queue/queue_test.go`

**Interfaces:**
- Consumes: `hooks.ParseCursor`, `hooks.Event.Agent`, `sample.CursorAgent`.
- Produces: `func hookCursor(stdin io.Reader, out io.Writer, sock, spool string) error`. It always returns nil and writes nothing to `out`.

- [ ] **Step 1: Write the failing tests.**
  - In `hook_test.go`, `TestCursorHookPrintsNothingAndSpools`: no service is listening. `hookCursor` with `cursor-stop-completed.json` leaves `out` empty and appends one spool line whose event has `"agent":"cursor-agent"` and `"name":"Stop"`.
  - In `queue_test.go`, `TestACursorEventJoinsOnlyACursorSession`: apply a Cursor `Stop` with `Pane: "%6"` and `Agent: "cursor-agent"`. Then `Merge` a Codex session on `%6` that is idle with no `Permits`. Its item is the estimated one from the pane (`Estimated == true`), never `StateFinished` from the hook. Next, `Merge` a `cursor-agent` session on `%6`: the item is `StateFinished`, not estimated.
  - In `queue_test.go`, `TestAHookedCursorSessionThatPermitsWaitsForPermission`: a Cursor `UserPromptSubmit` is applied for `%7`. A `cursor-agent` session on `%7` has `Status: "idle"`, `Permits: true`, `FromPane: true`, and `LastLine: "Run this command?"`. The queue holds one item with `StatePermission` and that `LastLine`.
  - In the same test, a Claude-hooked session (its own `SessionID`, `Agent: "claude"`) with `Permits: true` and no hook wait gets no item, which is today's behaviour.
- [ ] **Step 2: Run** `go test ./cmd/matchblox ./internal/queue -run 'Cursor' -v`. Expected: FAIL.
- [ ] **Step 3: Implement.**
  - **`hookCursor`:** parse with `ParseCursor`. On error, return nil. Otherwise send with `transport.Notify(sock, hookTimeout, proto.KindHook, ev)`, falling back to `hooks.Append(spool, ev)`.
  - **Joining in `Merge` and `WithAsks`:** when an id comes from `byPane`, use it only if `q.hooked[id].Agent == "" || q.hooked[id].Agent == s.Agent`. Put this in one helper, `func (q *Queue) join(s sample.Session, byPane map[string]string) string`, which both call.
  - **Permits in `Merge`:** for a hooked session with `s.Agent == sample.CursorAgent`, no item in `q.waiting`, `s.Status == "idle"`, and `s.Permits`, append an item with `StatePermission`, `Since: now.Add(-s.Idle)`, `LastLine: s.LastLine`, and `FromPane: true`.
- [ ] **Step 4: Run** `go test ./...`. Expected: PASS, with every existing queue test unchanged.
- [ ] **Step 5: Commit** `feat: Cursor's hooks put its sessions in the queue`

### Task 4: The "Add the Cursor hooks" setup door

**Files:**
- Create: `internal/setup/cursor.go`
- Modify: `internal/doors/doors.go`, adding the const `CursorHooks = "cursorhooks"` after `Hooks`.
- Modify: `internal/setup/setup.go` (`Doors`, `Open`, `known`)
- Test: `internal/setup/cursor_test.go`

**Interfaces:**
- Consumes: `hooks.CursorEvents`, and the existing `edit`, `read`, `members`, `object`, `array`, `lastIndex`, `quote`, `shellQuote`, `diff`, `Sum`, and `tilde` helpers in `internal/setup`.
- Produces:
  - `func cursorHooksPath(e Env) string`, which returns `filepath.Join(e.Home, ".cursor", "hooks.json")`
  - `func usesCursor(e Env) bool`, true when `cursor-agent` is on PATH or `~/.cursor` exists
  - `func cursorHooksDoor(e Env) doors.Door`
  - `func AddCursorHooks(path, exe, seen string) (backup string, err error)`

- [ ] **Step 1: Write the failing tests** in `cursor_test.go`. Reuse the `env`/`open` helpers of `setup_test.go`.
  - `TestCursorHooksDoorWritesTheFiveEvents`: no file exists. After opening, the file parses to `{"version":1,"hooks":{<each of CursorEvents>:[{"command":"<exe> hook --cursor"}]}}`, and has no other event key.
  - `TestCursorHooksKeepTheirOwnHooks`: an existing file with `"afterFileEdit":[{"command":".cursor/hooks/format.sh"}]` and `"stop":[{"command":"say done"}]` keeps both, byte for byte (check the original lines are in the new file), and gains matchblox's entry in `stop` after `say done`.
  - `TestCursorHooksAreAddedOnce`: a second open is `Done` with an unchanged file. An entry that runs `matchblox hook --cursor` from another path counts as present.
  - `TestCursorHooksSetTheVersion`: a file with `"hooks":{}` and no `"version"` gets `"version": 1`.
  - `TestCursorHooksDoorOnlyWhereCursorIs`: `Doors` lists `doors.CursorHooks` only when `cursor-agent` is on `LookPath` or `~/.cursor` exists. With neither, it does not, which is today's door list.
  - `TestCursorHooksOnBrokenJSONSayHowToFix`: invalid JSON gives a `Problem` naming `~/.cursor/hooks.json`.
- [ ] **Step 2: Run** `go test ./internal/setup -run Cursor -v`. Expected: FAIL.
- [ ] **Step 3: Implement.**
  - **The door:** title `Add the Cursor hooks`. Why: `Cursor tells matchblox when a session starts, ends and finishes its turn. Without the hooks its queue is read from its pane.`
  - **Hook entries** are `{"command": "<shellQuote(exe)> hook --cursor"}`. The event name comes from the payload, so one command serves all five.
  - **Order in `Doors`:** append the door right after the Claude block's `hooksDoor`, under `if usesCursor(e)`. Add `case doors.CursorHooks:` to `Open`, and add the id to `known`.
- [ ] **Step 4: Run** `go test ./internal/setup/... ./cmd/matchblox/...`. Expected: PASS. Update any golden door list in `cmd/matchblox` tests only where Cursor is present in the fixture env.
- [ ] **Step 5: Commit** `feat: setup adds matchblox to Cursor's hooks`

### Task 5: Start Cursor with `matchblox new` and as the stoker

**Files:**
- Modify: `internal/service/lifecycle.go` (`Startable`, `launchArgv`, `newSteps`, the `startable()` doc)
- Modify: `internal/state/state.go:42-44` (the comment lists the agents)
- Modify: `cmd/matchblox/main.go:323` (the `--agent` help)
- Modify: `internal/service/stokeragent.go:51` (the missing-agent message, adding `cursor-agent`)
- Modify: `internal/stoker/order.go:108-128` (`Unguarded` covers `cursor-agent`)
- Modify: `internal/stoker/brief.go:26,60`
- Test: `internal/service/lifecycle_test.go`, `internal/stoker/order_test.go`, `internal/stoker/brief_test.go` (or wherever the brief text is pinned: `grep -rn "Codex and OpenCode sessions have no guard" internal`)

**Interfaces:**
- Consumes: `sample.CursorAgent`.

- [ ] **Step 1: Write the failing tests.**
  - `TestNewStartsCursor`: `newSteps` with `Agent: "cursor-agent"` and `First: "fix the build"` runs argv `["cursor-agent", "fix the build"]`.
  - `TestNewTakesCursorAsAlias`: `Agent: "cursor"` behaves exactly like `cursor-agent`.
  - `TestNewStartsCursorByAgentWhenOnlyThatIsInstalled`: `LookPath` knows only `agent`, so argv is `["agent", "fix the build"]`.
  - In `order_test.go`, extend the `Unguarded` case so a step whose session runs `cursor-agent` is unguarded.
  - The brief's `--agent` list reads `claude|codex|opencode|cursor-agent`, and the guard sentence reads `Codex, OpenCode and Cursor sessions have no guard`.
- [ ] **Step 2: Run** `go test ./internal/service ./internal/stoker -v -run 'Cursor|Unguard|Brief'`. Expected: FAIL.
- [ ] **Step 3: Implement.**
  - `Startable = []string{"claude", "codex", "opencode", "cursor-agent"}`.
  - Normalise `cursor` to `cursor-agent` at the top of `newSteps`.
  - The `look` check tries `cursor-agent`, then `agent`, and `launchArgv` uses whichever binary was found. Change `launchArgv(agent, name, first string)` to take the binary as its first element: add a `bin string` parameter and update its one caller (`lifecycle.go:372`).
  - Update the validation message to `claude, codex, opencode or cursor-agent`.
  - `startable()`, which fills `state.Agents`, must report `cursor-agent` when either binary is installed.
- [ ] **Step 4: Run** `go test ./...`. Expected: PASS.
- [ ] **Step 5: Commit** `feat: matchblox new and the stoker start Cursor`

### Task 6: Cursor's approval menu in the pane

**Files:**
- Modify: `internal/said/said.go` (`permitsRe` and its comment)
- Screens are inline raw strings in `said_test.go`, as its other tests do:
  - `cursorPermits`: the captured menu from the spec's "Live run" section, verbatim.
  - `cursorRest`: a finished turn ending `Done. second.txt was created.`, then the `→ Add a follow-up` box and the `Auto · 5.7%` status line.
- Test: `internal/said/said_test.go`

- [ ] **Step 1: Write the failing tests.**
  - `TestCursorsApprovalMenuPermits`: `Screen(cursorPermits)` gives `("Run this command?", Permits)`.
  - `TestCursorAtRestSaysItsLastLine`: `Screen(cursorRest)` gives `("Done. second.txt was created.", None)`. The box line `→ Add a follow-up` and the status line are never returned.
- [ ] **Step 2: Run** `go test ./internal/said -run Cursor -v`. Expected: FAIL on the first test.
- [ ] **Step 3: Implement.** Add `\brun \(once\) \(y\)` to `permitsRe`, and name Cursor's `Run (once) (y)` in its comment. If the second test fails because of `→` or the status line, extend `hintRe` or the box detection in `lines()` minimally. Make no other `said` change.
- [ ] **Step 4: Run** `go test ./...`. Expected: PASS, with all said and goldens unchanged.
- [ ] **Step 5: Commit** `feat: a Cursor approval shows as asks`

### Task 7: Docs, changelog, and an end-to-end check on this machine

**Files:**
- Modify: `docs/agents.md`. Add a **Cursor** block (`tmux new -s app agent` with its console row), and a paragraph under "How an agent tells that it waits" on the Cursor hooks door. That paragraph lists the five events, says why there is no `notification`, says that approvals are read from the pane, and says that `agent -p` events only count in a Cursor pane.
- Modify: `docs/limits.md`. Cursor shows no context use or account yet, and has no merge guard.
- Modify: `docs/config.md`. Add `cursor-agent` to the agents list, and `[stoker] agent = "agent"`.
- Modify: `docs/stoker.md` (the lines that list agents: 39, 307, 391)
- Modify: `README.md`. Change "Works with Claude Code, Codex, OpenCode, and any agent in tmux." to "Works with Claude Code, Codex, OpenCode, Cursor, and any agent in tmux.", and make the same change in `docs/index.md:22`.
- Modify: `CHANGELOG.md` `## [Unreleased]`. Add an `### Added` line in the repo's voice, e.g. `- Cursor's CLI is an agent like the others: Sessions shows it, its hooks put it in the queue, and matchblox new and the stoker start it.`
- Test: `go test ./cmd/matchblox -run Docs` and `internal/docscheck`, which check links and commands named in the docs.

- [ ] **Step 1: Write the docs and changelog.**
- [ ] **Step 2: Run** `go test ./... && golangci-lint run && .github/scripts/changelog-guard.sh`. Expected: all pass.
- [ ] **Step 3: Check it end to end on this machine.** Do not edit the user's real `~/.cursor/hooks.json`. Use a scratch project with a project `.cursor/hooks.json` holding the five entries, pointing at the built binary (`go build -o $SCRATCH/matchblox ./cmd/matchblox`). Run the console from the worktree build against the live tmux server. In a tmux session `cursorcheck`, run `agent --trust` in the scratch project and check that:
  - (a) the row shows `cursor-agent`
  - (b) `Run the shell command: touch x && date` puts it in the queue as `asks` with `Run this command?`
  - (c) after `y` and the reply, it shows `done` with the reply's last line
  - (d) Esc mid-turn leaves it out of the queue

  Record what was seen in the PR description. Kill `cursorcheck` afterwards.
- [ ] **Step 4: Commit** `docs: Cursor is one of the agents`
