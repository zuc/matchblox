# Agent Switch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A builder switches the agent on an existing tmux seat (claude ↔ codex ↔ opencode ↔ cursor-agent) from Sessions or the CLI, and the seat keeps its name, pane, cwd, hold and ship.

**Architecture:**
- Service: a `switch:<pane>` act beside `kill:` and `rename:` in `internal/service/lifecycle.go`. It runs one guarded step, `tmux respawn-pane -k`, with the argv `launchArgv` builds. On success it lets go of the stoker's drive on the pane, drops the pane's old queue state, and the hold follows the pane until the new agent has a session id.
- Holds: a hold may be marked `Rebind`. Such a hold belongs to its pane, not its session id, until the sampler sees a new session id there.
- Surfaces: `matchblox switch <who> --agent <name> [--handoff '…'] [--anyway]`, and an **Switch agent** form on `A` in Sessions. Both send the same act. Both jump tmux to the pane after success: the console always, the CLI when `TMUX` is set.

**Tech Stack:** Go 1.26, the standard library, tmux ≥ 3.0 (`respawn-pane` with a multi-word command execs it directly). No new dependencies.

**Spec:** `design/0011-agent-switch.md`. Read it first. This plan settles its "Left to the builder" items and makes the deviations listed below. Task 8 writes them back into the design.

## Rulings and deviations from the spec

- **Key `A`, not `S`.** `S` already reverses the sort (`internal/app/model.go:764`). `A` is free and stands for "agent".
- **Claude's own name** comes from `--name <name>` in `launchArgv` at respawn. No `/rename` step is needed.
- **Cursor account usage: #148 shipped the account, #151 the plan usage.** Since #148, Sessions and USAGE show Cursor's account (email and team, from `~/.cursor/cli-config.json`, marked `⬢`), and its context comes from Cursor's status line. A spike on 2026-10-21 (CLI 2026.10.19-6bbf2c4) found no plan usage in `agent status`, `agent about` or `~/.cursor/*.json`, so this plan deferred it. #151 then shipped it on main by another source: with `cursor_usage = true` under `[limits]`, USAGE shows the month's included usage from Cursor's API every 5 minutes. This branch adds nothing for usage.
- **The stoker never switches.** An act from the stoker's server, or from a pane it drives, is refused. The spec only says the stoker's own session cannot be switched.
- **The CLI jumps** with `tmux switch-client -t <pane>` when `TMUX` is set (the spec's default).
- **The CLI refusal keeps kill's shape** (`work works: y again switches it mid-turn`; docs name `--anyway`). It does not print the plan.
- **`--trust` came from main.** #151 added it to `launchArgv` on main (CHANGELOG 0.14.0), so after the rebase Task 1 leaves only `TestLaunchArgvOfEachAgent` and the `docs/agents.md` line; its CHANGELOG line was dropped as a duplicate.

## Global Constraints

- Agent names: `claude`, `codex`, `opencode`, `cursor-agent`. `cursor` is accepted as an alias where a person types it (`--agent`, `proto.SwitchAgent.Agent`), as in `newSession`.
- Default handoff, verbatim, one line: `Read the working tree and any local notes. Summarise progress so far, then continue the work from here.`
- Handoff text passes the same checks as `new`'s first message: `panes.CheckAnswer`, and it may not start with `-`. Empty means no first message.
- Every matchblox launch of `cursor-agent` (`new` and `switch`) includes `--trust` right after the binary, before any first message.
- Switch never calls `KillSteps`, `kill-pane` or `new-session`.
- Fixtures stay anonymous: `you@example.com`, `/work/app`, `/home/u`.
- Match the surrounding code: sentence-style comments, `Test<Sentence>` names, the existing helpers `lifeTest`, `lifeSnap`, `consoleAct`, `loadedWith`, `key`, `typeText`, `cliDoc`.
- Each user-facing change adds a line under `## [Unreleased]` in `CHANGELOG.md`, in its own task.
- Run `go test ./...` and `golangci-lint run` before each commit. Commits are conventional (`feat:`, `fix:`, `docs:`), and each ends with the session's attribution line.

## Review Focus

1. **A hook from the old agent arrives after the respawn** (its `Stop`, or `SessionEnd` from the dying process). It must not make the new agent in that pane look like it waits. Test in Task 2.
2. **The new agent never gets a session id** (Codex or OpenCode with no hooks, or a crash at start). A parked seat stays parked, and messages to it are refused, until `holds.GoneFor`. Test in Task 4.
3. **The guard stops the respawn** because the pane's pid or start time changed between confirm and run. Nothing runs, and the hold is not left in `Rebind`. Test in Task 4.
4. **A handoff that starts with `-`, or holds a control character.** It is refused with the first-message wording, adapted to "the handoff", before anything runs. Test in Task 4.
5. **The cwd is gone since the session started** (a removed worktree). Switch refuses with `no directory <cwd>` and leaves the pane alone. Test in Task 4.

---

### Task 1: Cursor starts with `--trust`

**Files:**
- Modify: `internal/service/lifecycle.go` (`launchArgv`, its doc comment)
- Test: `internal/service/lifecycle_test.go`
- Modify: `docs/agents.md` (Cursor section: matchblox starts it with `--trust`), `CHANGELOG.md`

**Interfaces:**
- Produces: `launchArgv(bin, agent, name, first string) []string` (same signature). For `sample.CursorAgent` it returns `[bin, "--trust", first?]`.

- [ ] **Step 1: Write the failing tests.**
  - `TestLaunchArgvOfEachAgent` is a table:
    - `("claude","claude","work","go")` → `[claude --name work go]`
    - `("codex","codex","work","")` → `[codex]`
    - `("opencode","opencode","work","go")` → `[opencode --prompt go]`
    - `("agent","cursor-agent","work","go")` → `[agent --trust go]`
    - `("cursor-agent","cursor-agent","work","")` → `[cursor-agent --trust]`
  - Update `TestNewStartsCursor`: the suffix becomes `-c <dir> cursor-agent --trust fix the build`.
- [ ] **Step 2: Run** `go test ./internal/service -run 'TestLaunchArgv|TestNewStartsCursor' -v`. Expected: FAIL, `--trust` is missing.
- [ ] **Step 3: Implement.** Add `case sample.CursorAgent: argv = append(argv, "--trust")` to `launchArgv`'s switch. Extend its doc comment: Cursor trusts the workspace (`--trust`), or its first turn waits on a trust prompt.
- [ ] **Step 4: Run** `go test ./internal/service/...`. Expected: PASS.
- [ ] **Step 5: Docs and commit.** Add one line to `docs/agents.md`'s Cursor section, and to the CHANGELOG: "A Cursor session matchblox starts trusts its directory (`--trust`): its first turn no longer waits on the trust prompt." Then commit `feat: a Cursor session matchblox starts trusts its directory`.

### Task 2: The queue forgets a pane

**Files:**
- Modify: `internal/queue/queue.go`
- Test: `internal/queue/queue_test.go`

**Interfaces:**
- Produces: `func (q *Queue) Forget(pane string)`. It deletes every hooked session whose last event names `pane`, from `hooked`, `waiting`, `seen`, `asks`, `replies` and `stopped`. It also records each id in a new `forgot map[string]time.Time`. `Apply` drops events of a forgotten id. `Merge` lets go of `forgot` entries older than `UnseenGrace`.

- [ ] **Step 1: Write the failing test** `TestAForgottenPaneShowsNoneOfItsOldAgentsWaits`:
  - Apply a `Stop` for session `old` in pane `%1`, then `Merge` with a session `{SessionID: "old", Pane: "%1", Status: "idle"}`. One item, for `old`.
  - `Forget("%1")`. Then `Merge` with the new agent `{SessionID: "", Pane: "%1", Agent: "codex", Status: "busy", Busy: true}`. No item.
  - Apply a late `Stop` for `old` in `%1`, at a later time, and `Merge` with the same new agent. Still no item (Review Focus 1).
  - A `Stop` from a new id `new` in `%1` after that is an item. The pane is not deaf.
- [ ] **Step 2: Run** `go test ./internal/queue -run TestAForgottenPane -v`. Expected: FAIL, `Forget` is undefined.
- [ ] **Step 3: Implement** `Forget` and the `forgot` map as above. Initialize the map in `New`. The doc comment: a switch's old agent is gone from the pane, and its late hooks must not speak for the new one.
- [ ] **Step 4: Run** `go test ./internal/queue/...`. Expected: PASS.
- [ ] **Step 5: Commit** `feat: the queue forgets a pane whose agent was switched`

### Task 3: A hold follows its pane through a switch

**Files:**
- Modify: `internal/holds/holds.go`
- Test: `internal/holds/holds_test.go`

**Interfaces:**
- Produces:
  - `Hold.Rebind bool` with json tag `rebind,omitempty`. True: the seat's agent was switched, and the hold is the pane's until a new session id shows there.
  - `func (s *Store) Pending(pane string) (Hold, bool)`: the `Rebind` hold whose `Pane == pane`.
  - `func (s *Store) Rebind(pane, sessionID string) (bool, error)`: when a `Rebind` hold for `pane` exists and `sessionID` is non-empty and differs from it, it re-keys the hold to `sessionID` with `Rebind` false, sets `seen[sessionID] = now`, and saves. It returns whether it moved. A save failure restores the old entry.
  - Update the `Hold` type comment: it follows the session, but an explicit switch keeps it on the pane.

- [ ] **Step 1: Write the failing test** `TestAHoldMarkedRebindMovesToTheNextSessionInItsPane`, on a store in `t.TempDir()`:
  - `Set(Hold{SessionID: "old", Pane: "%1", By: ByYou, Rebind: true})`. `Pending("%1")` finds it, and `Pending("%2")` does not.
  - `Rebind("%1", "old")` → false, and nothing changes. `Rebind("%1", "")` → false.
  - `Rebind("%1", "new")` → true. `Of("new")` has `By == ByYou`, `Pane == "%1"`, `Rebind == false`. `Of("old")` is gone. `Pending("%1")` is gone.
  - Reopen the store from the same dir: `Of("new")` survives. `Rebind` round-trips through JSON, and a plain hold's JSON has no `rebind` key.
- [ ] **Step 2: Run** `go test ./internal/holds -v`. Expected: FAIL to compile.
- [ ] **Step 3: Implement** the field and the two methods.
- [ ] **Step 4: Run** `go test ./internal/holds/...`. Expected: PASS.
- [ ] **Step 5: Commit** `feat: a hold can follow its pane to the next agent there`

### Task 4: The service switches a seat's agent

**Files:**
- Modify: `internal/proto/proto.go` (beside `NewSession`)
- Modify: `internal/service/lifecycle.go` (`switchAgent`, `SwitchAction`, `checkFirst`; `newSteps` uses `checkFirst`)
- Modify: `internal/service/actions.go` (`dispatch`: `switch:`; `messageable` uses `heldIn`)
- Modify: `internal/service/hands.go` (`paneActs` gains `"switch:"`)
- Modify: `internal/service/holds.go` (`heldIn`; `holdAt` and `heldSessions` honour pending holds)
- Modify: `internal/service/service.go` (the sample's holds block calls `Holds.Rebind` before `Prune`)
- Test: `internal/service/lifecycle_test.go`

**Interfaces:**
- Consumes: `queue.Forget(pane)` (Task 2), `holds.Hold.Rebind`, `Store.Pending`, `Store.Rebind` (Task 3), `launchArgv` (Task 1).
- Produces:
  - `proto.SwitchAgent struct { Agent string \`json:"agent"\`; Handoff string \`json:"handoff,omitempty"\`; Anyway bool \`json:"anyway,omitempty"\` }`. This is the Text of a `switch:<pane>` act. Clients fill `Handoff`. The service never substitutes the default.
  - `const proto.DefaultHandoff = "Read the working tree and any local notes. Summarise progress so far, then continue the work from here."`
  - `func SwitchAction(pane string, x sample.Session, argv []string) advice.Action`. Label `"switch"`, `Destructive: true`, one step: `TmuxOn(server) + ["respawn-pane", "-k", "-t", bare, "-c", x.Cwd, "env", "-u", "GIT_TERMINAL_PROMPT", "-u", "GIT_SSH_COMMAND", "-u", panes.HomeEnv] + argv`. It has one guard, `advice.Guard{PID: x.PID, StartTicks: x.Start}`.
  - `func checkFirst(text, what string) error`. This is `newSteps`' first-message check moved out. `what` is `"the first message"` or `"the handoff"`, and the dash error reads `<what> does not start with -`. `newSteps` keeps its exact messages.
  - `func (s *Service) heldIn(pane string, x sample.Session) (holds.Hold, bool)`: `heldOf(x.SessionID)`, else `Holds.Pending(pane)`.

- [ ] **Step 1: Write the failing tests** in `lifecycle_test.go`. Use `lifeTest`, set `Cwd: "/work/app"` on `%1` and `%2` in `lifeSnap`, and set `s.IsDir` to accept `/work/app`. Add the helper `switchAct(t, pane string, n proto.SwitchAgent) proto.Act`, which marshals `n` into `consoleAct("switch:"+pane, …)`.
  - `TestSwitchRespawnsTheAgentInItsPane`: switching `%1` to codex with `Handoff: proto.DefaultHandoff` runs exactly `[tmux respawn-pane -k -t %1 -c /work/app env -u GIT_TERMINAL_PROMPT -u GIT_SSH_COMMAND -u <panes.HomeEnv> codex <DefaultHandoff>]`. No `kill-pane` or `new-session` appears in `*ran`.
  - `TestSwitchToClaudeKeepsTheName`: switching `%3` (codex, name `cx`; give it `Cwd: "/work/app"`) to claude with no handoff ends with `claude --name cx`.
  - `TestSwitchToCursorTrusts`: switching to `cursor` (the alias) ends with `cursor-agent --trust <handoff>`.
  - `TestSwitchAsksAgainWhileItWorks`: `%2` → `busy works: y again switches it mid-turn`, and nothing runs. With `Anyway: true` it runs one step.
  - `TestSwitchRefuses` is a table, and nothing runs in any row:
    - `Confirm` empty → `needs confirm`
    - `%1` to claude → `work runs claude already`
    - to `gpt` → `gpt is not an agent matchblox starts: claude, codex, opencode or cursor-agent`
    - to opencode (not installed in `lifeTest`) → `opencode is not installed here`
    - `%77` → `no agent session in %77 any more`
    - `%9@mbtest` (add the stoker session as in `TestKillNeverTheStokersSession`) → `the stoker's session keeps its agent`
    - an act with `From` set to a pane the stoker drives (`s.drive("%4")`, with a stoker set the way `TestStokerKillWaitsForTheBuildersGo` sets it) → `the stoker does not switch agents: the builder does`
    - Handoff `--yolo` → `the handoff does not start with -` (Review Focus 4)
    - Handoff `"a\x1bb"` → the `panes.CheckAnswer` error, with "the answer" replaced by "the handoff"
    - `IsDir` false for `/work/app` → `no directory /work/app` (Review Focus 5)
  - `TestSwitchTakesTheSeatFromTheStoker`: `s.drive("%1")`, then a builder switch of `%1` succeeds, and `agentsPane(proto.Act{From: "%1"})` is false afterwards.
  - `TestSwitchKeepsTheParkOnTheSeat`:
    - Park `%1` (`park:%1`, so the hold is on `s1`), then switch it. `holdAt("%1")` still holds, `By == ByYou`.
    - Publish a snapshot where `%1` runs `{Agent: "codex", PID: 201, SessionID: ""}`. `messageable("%1", 201)` errors with `… is parked: z lets it go` (Review Focus 2).
    - Publish one with `SessionID: "s9"`. `Holds.Of("s9")` holds, and `Holds.Of("s1")` does not.
  - `TestASwitchTheGuardStopsLeavesTheHoldAsItWas`: park `%1`, then set `s.Actions.Check = func(advice.Guard) error { return errors.New("another process has pid 101 now") }` (`internal/actions/runner.go:26`). The switch returns without `Ran`. `Holds.Pending("%1")` is false, and `Holds.Of("s1")` is still a hold (Review Focus 3).
  - `TestASwitchForgetsTheOldTurn`: a hook `Stop` for `s1` in `%1` makes it wait. After the switch, and a publish with the new codex agent (no id) in `%1`, `s.queued("%1")` is false.
- [ ] **Step 2: Run** `go test ./internal/service -run 'TestSwitch|TestASwitch' -v`. Expected: FAIL to compile.
- [ ] **Step 3: Implement `switchAgent(ctx context.Context, pane string, a proto.Act) proto.Result`.** The checks run in the order of the refusals table above, and every message is copied verbatim from it. Then:
  1. The word `works` / `waits for you`, as `kill` computes it. With no `Anyway`: `<name> <word>: y again switches it mid-turn`.
  2. When `heldIn(pane, x)` is a hold, `Set` it again with `Rebind: true`.
  3. `release := s.holdPane(pane)`, then `res := s.Actions.Do(ctx, SwitchAction(pane, x, launchArgv(s.agentBin(n.Agent), n.Agent, x.Name, n.Handoff)), "y")`, then `release()`.
  4. When `res.Err != ""` or `len(res.Ran) == 0`: put the hold back as it was (`Rebind: false`). When the step ran and failed, append `: the pane may have no agent now` to `res.Err`. Return `res`.
  5. On success: `s.undrive(pane)`, `s.renames.drop(pane)`, `s.queue.Forget(pane)` under `s.mu`, then a non-blocking `s.sampleKick` like `newSession`'s. Return `res`.

  Wire `switch:` into `dispatch` after `rename:`, and add it to `paneActs`. Use `heldIn` in `messageable`, `holdAt` (when the pane has a session) and `kill`'s parked check. In `heldSessions`, also add the session id now in a pending hold's pane, when it is non-empty. In the sample's holds block, call `s.Holds.Rebind(x.Pane, x.SessionID)` for each sampled session with a pane, before `Prune`, and append an error to `s.cur.Errors` the way `Prune`'s is.

  The doc comment of `switchAgent` names the place `(name, cwd, pane)`, the one respawn step, the hold rebind and the drive it clears.
- [ ] **Step 4: Run** `go test ./internal/... && golangci-lint run`. Expected: PASS. The existing kill, rename, park and hold tests are unchanged.
- [ ] **Step 5: Commit** `feat: the service switches a seat's agent and keeps its place (#143)`

### Task 5: `matchblox switch`

**Files:**
- Modify: `cmd/matchblox/life.go` (`switchOpts`, `switchAct`, `cliSwitch`)
- Modify: `cmd/matchblox/main.go`: a `"switch"` case beside `"kill", "rename"`; the `--handoff` flag; the `--agent` and `--anyway` flag help names switch too; one usage line after `kill`.
- Test: `cmd/matchblox/life_test.go`
- Modify: `docs/console.md` (the CLI table: a `matchblox switch` row after `kill`), `CHANGELOG.md`

**Interfaces:**
- Consumes: `proto.SwitchAgent`, `proto.DefaultHandoff` (Task 4).
- Produces:
  - `type switchOpts struct { agent string; handoff *string; anyway bool }`. `agent` is `""` when `--agent` was not given. `handoff` is nil when `--handoff` was not given. `main.go` detects both with `fl.Visit`.
  - `func switchAct(doc state.Doc, words []string, o switchOpts, from string) (proto.Act, error)`. It returns `RecID: "switch:"+pane`, `Confirm: "y"`, `From: from`, and Text as JSON `proto.SwitchAgent`, with `Handoff` defaulting to `proto.DefaultHandoff` when `o.handoff == nil`.
  - `func cliSwitch(words []string, o switchOpts, w io.Writer, path string, jump func(argv []string) error) error`. `main.go` passes a jump that runs the argv when `os.Getenv("TMUX") != ""`, else nil.

- [ ] **Step 1: Write the failing tests:**
  - `TestSwitchNamesThePaneAndTheAgent`: `switchAct(cliDoc(), ["api"], {agent: "codex"}, "%9")` returns `RecID "switch:%3"`, `Confirm "y"`, `From "%9"`, and Text decoding to `{Agent: "codex", Handoff: proto.DefaultHandoff}`. With `handoff: &""` the Handoff is empty. With `anyway: true`, `Anyway` is true.
  - `TestSwitchRefusals` is a table:
    - no words → `switch needs a session: matchblox switch <who> --agent <name>`
    - no agent → `switch needs an agent: --agent claude, codex, opencode or cursor-agent`
    - two words → `switch takes one session: matchblox switch <who> --agent <name>`
    - an unknown session → `no session named nope`
    - a session with no pane (add one to `cliDoc()` if none) → `<who> is outside tmux: nothing to switch`
  - `TestSwitchJumpsOnlyAfterItRan`: drive `cliSwitch` against the fake service the CLI tests use (the `doAct` path in `cli_test.go`). With a result that has `Ran` set, it prints `switched api to codex`, and `jump` gets `[tmux switch-client -t %3]`. With `Err`, `jump` is not called and the error reads `switch api: refused: …`. With `jump == nil` it still prints.
- [ ] **Step 2: Run** `go test ./cmd/matchblox -run 'TestSwitch' -v`. Expected: FAIL to compile.
- [ ] **Step 3: Implement.** `cliSwitch` reads the doc and builds the act. It calls `doAct`, and maps errors through `handOutcome(w, "switch", who, res, err)` when anything went wrong. On success it prints `switched <who> to <agent>`, then calls `jump(slices.Concat(sample.TmuxOn(server), []string{"switch-client", "-t", bare}))` when `jump != nil`. A jump error is printed as `switched, but not jumped: <err>` and does not fail the command. Usage line: `matchblox switch <who> --agent <name>  run another agent in the session's pane (--handoff, --anyway)`.
- [ ] **Step 4: Run** `go test ./cmd/matchblox/...` (including `docs_test.go`, which checks the usage against the docs). Expected: PASS.
- [ ] **Step 5: Docs and commit.** Add the `docs/console.md` row: what it ends, what it starts and where, the default handoff text, `--handoff ''` for none, `--anyway` while it works or waits, and the jump when inside tmux. Add the CHANGELOG line: "`matchblox switch <who> --agent <name>` runs another agent in a session's pane: same name, directory, park and ship, with a handoff line to start from." Commit `feat: matchblox switch runs another agent in a session's pane (#143)`.

### Task 6: Switch agent on `A` in Sessions

**Files:**
- Modify: `internal/app/lifeform.go` (the form's switch mode, `openSwitch`, `switchAction`)
- Modify: `internal/app/model.go`: `case "A", "shift+a"` on Sessions; `action.againText`; the second-y branch uses it; `Model.jumpAfter`; the `resultMsg` jump.
- Test: `internal/app/lifeform_test.go`
- Modify: `docs/keys.md` (an `A` row; "New session, Rename and Switch agent" section title), `CHANGELOG.md` (fold into Task 5's line: "…from the CLI, or `A` in Sessions.")

**Interfaces:**
- Consumes: `proto.SwitchAgent`, `proto.DefaultHandoff`.
- Produces:
  - `form.swap bool` and `form.from string` (the session's agent now). In switch mode `fields()` is `[fieldAgent, fieldFirst]`, `fieldFirst` is labelled `handoff`, and `←`/`→` on agent walks `m.agents` without `f.from`.
  - `func (m Model) openSwitch() (tea.Model, tea.Cmd)`
  - `func (m Model) switchAction(f form) *action`: `label "switch"`, `destructive`, `rec "switch:"+pane`, and `text` set to the JSON of `SwitchAgent{Agent, Handoff}`. `say` is `switch <name>: <from> → <to> in <pane>, <cwd>, then jump there`. `again` is set as `killAction` sets it, with "switches" for "kills".
  - `action.againText string`: the Text the second y sends (`""`: `killAnyway`). For a switch it is the same JSON with `Anyway: true`.
  - `Model.jumpAfter struct{ act, pane string }`: set when a confirmed `switch:` act is sent.

- [ ] **Step 1: Write the failing tests:**
  - `TestAOpensSwitchAgentAndJumpsAfter`, with `lifeState()` (agents claude and codex; row 0 is claude in `%1`):
    - `A` opens a form with `swap`, `agent == "codex"`, and `first == proto.DefaultHandoff`. The screen shows `Switch agent` and `handoff`.
    - `left` keeps codex (the only choice).
    - `enter` closes the form. `m.pending` has rec `switch:%1`, and `say` contains `claude → codex`. No act is sent yet.
    - `y` sends one act `{RecID: "switch:%1", Confirm: "y"}` whose Text decodes to `{Agent: "codex", Handoff: DefaultHandoff}`.
    - A `resultMsg` for that act with `Ran` set returns a cmd that runs `[tmux switch-client -t %1]` through `opt.Run`. A result with `Err` runs none, and the flash shows the error.
  - `TestSwitchOfABusySessionTakesASecondY`: make row 0 busy. `A`, `enter`, `y` shows `alpha works: y again switches it mid-turn` and sends nothing. The second `y` sends Text with `"anyway":true`.
  - `TestSwitchRefusesOutsideTmuxAndOneAgent`: on a row with `Pane == ""`, the flash is `<name> is outside tmux: nothing to switch`. With `Agents = ["claude"]` on a claude row, the flash is `no other agent to switch to here`.
  - `TestSwitchEscSendsNothing`.
- [ ] **Step 2: Run** `go test ./internal/app -run 'TestAOpensSwitch|TestSwitch' -v`. Expected: FAIL.
- [ ] **Step 3: Implement.** In switch mode, `formEnter` sets `m.form = nil` and `m.pending = m.switchAction(f)`. It refuses an empty agent in the form, and refuses the handoff with the same checks as Task 4 (`panes.CheckAnswer`, no leading `-`), so the refusal stays in the form. `confirm` sets `m.jumpAfter` after `send` when `a.rec` starts with `switch:`. The `resultMsg` case checks `jumpAfter` before `tally`/`describe`. On success it flashes `switched <name> to <agent>` and runs `m.jump(pane)` as a nav cmd, the way `confirm` runs `a.nav`.
- [ ] **Step 4: Run** `go test ./internal/app/...`. Expected: PASS, with the existing kill and rename tests unchanged.
- [ ] **Step 5: Docs and commit.** Add the `docs/keys.md` row: "`A` | Switches the agent of the session (Sessions): another installed agent (`←` `→`) starts in its pane and directory with a handoff line you may edit or clear. The plan shows, `y` runs it, and a session that works or waits asks for a second `y`. The console jumps to the pane after." Commit `feat: A in Sessions switches a session's agent (#143)`.

### Task 7: Sessions marks an agent outside tmux

**Files:**
- Modify: `internal/app/view.go` (`place`)
- Modify: `cmd/matchblox/cli.go` (the `sessions` text line; JSON unchanged)
- Test: `internal/app/sessions_test.go`, `cmd/matchblox/cli_test.go`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Produces: `place("", "") == "outside tmux"`.

- [ ] **Step 1: Write the failing tests:**
  - `TestASessionOutsideTmuxSaysSo`: a state with a session `{Name: "loose", Pane: "", Target: ""}`. Its Sessions row contains `outside tmux`. `A`, `r`, `z` and `x` on it change nothing and send nothing (the existing refusals, plus Task 6's).
  - `TestSessionsLineMarksOutsideTmux`: the CLI text line of a session with no pane shows `outside tmux` in the target column, and its JSON keeps `"target":""`.
- [ ] **Step 2: Run** them. Expected: FAIL.
- [ ] **Step 3: Implement.** `place` returns `"outside tmux"` when both arguments are empty. The CLI line prints `outside tmux` for `r.Target` when `r.Pane == ""`.
- [ ] **Step 4: Run** `go test ./...`. Expected: PASS. Fix snapshot or golden tests that relied on an empty place, by updating their expectations only.
- [ ] **Step 5: Commit** with the CHANGELOG line "Sessions says `outside tmux` for an agent with no pane." Message: `feat: Sessions says when an agent runs outside tmux`.

### Task 8: Close the loop

**Files:**
- Modify: `design/0011-agent-switch.md`: Status becomes `built`. Add a "Revised while building" section that copies this plan's "Rulings and deviations" list.
- Delete: `docs/handover-agent-switch.md` (the kickoff has served).
- Modify: `README.md` only if it lists the CLI verbs (`rg -n 'matchblox kill' README.md`); then add `switch` beside them.

- [ ] **Step 1: Live check** on a scratch tmux session in `/tmp` (not a real work pane):
  - `matchblox new --agent claude --where /tmp/sw`, then `matchblox switch sw --agent codex`. The pane id stays the same (`tmux display -p -t sw '#{pane_id}'` before and after), codex shows the handoff, Sessions shows codex on the row, and the client jumps.
  - In the console, repeat with `A` back to claude. Park it first with `z`. After the switch, the row is still parked once the new id appears.
  - Write the result (what you saw, or why you could not) into the PR body.
- [ ] **Step 2: Run** `go test ./... && golangci-lint run && .github/scripts/changelog-guard.sh`. Expected: PASS.
- [ ] **Step 3: Commit** `docs: agent switch design matches what was built (#143)`. Push `agentswitcher`. Edit PR #147: the title `feat: a builder switches a session from one agent to another (#143)`, a body that closes #143 with the test plan ticked, and a draft → ready flip after the live check.
