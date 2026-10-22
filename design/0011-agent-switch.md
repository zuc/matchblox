# Switch agent on a session: design

Date: 2026-10-21. Status: built. Issue: #143.
Depends on: Cursor as a first-class agent (`design/0009-cursor-agent.md`, #142).

## Intent

A builder mid-work wants the *same* place in matchblox — name, pane, cwd,
ship, park — to keep going under a different agent (claude ↔ codex ↔
opencode ↔ cursor-agent). That urge hits when the turn feels wrong, or
when the current agent has run out of usage / sessions. matchblox should
swap the agent carefully, offer a handoff that summarises progress, jump
tmux into the destination, and never pretend another product's transcript
is native memory.

This pass also tightens how matchblox *starts* Cursor and how Sessions
treats agents outside tmux, because switch only works for seats matchblox
owns.

## Goals

1. **Switch on a seat.** From Sessions or the CLI, switch the agent on an
   existing tmux-backed session to another startable agent. Idle and busy
   both work. Confirm shows the exact plan; a destructive step needs a
   typed `y` (same gravity as `kill --anyway` when the session works or
   waits).
2. **The place survives.** Matchblox name, cwd, and the tmux pane stay.
   Queue waits for the *old* turn die with the process. Holds and ships
   stay attached to the place: after the new agent appears, a park/hold is
   rebound to its new SessionID; ships already prefer pane.
3. **Careful handoff.** The new agent starts with a first prompt that asks
   it to read the working tree, summarise progress, and continue — not a
   full foreign transcript import. The builder may edit or clear that
   line before confirm. After the swap, matchblox jumps the tmux client to
   that pane.
4. **Canonical switch window.** Console and CLI show the same plan shape:
   who → which agent → end current (idle | mid-turn) → start new in cwd
   with handoff → keep name/pane → jump. One form / one confirm path, not
   an ad-hoc scrap per surface.
5. **Cursor launch defaults.** Every Cursor session matchblox starts
   (`new` and `switch`) runs with `--trust`. Sessions shows Cursor account
   usage for the builder's login when it can be read without shelling out
   on every sample tick.
6. **Outside tmux is not a seat.** Sessions still lists agents with no
   pane, but marks them clearly as outside tmux. Switch, rename, kill of
   the matchblox kind, park, and jump refuse them with the existing
   "outside tmux" gravity. Full lifecycle for non-tmux waits on #86.

## Non-goals

- Importing Claude / Codex / Cursor / OpenCode transcripts as each
  other's native memory.
- Switching a session that runs outside tmux (#86).
- Changing Claude compact, `/rename`, merge guard, or Cursor hook set
  beyond what launch and usage need.
- Starting the destination in a *new* tmux session while leaving the old
  pane alive (that would mint a second place).

## Anti-goals

- `kill` + `new` by hand as the only path (loses hold, queue habit, ship
  attachment to the seat).
- Closing the pane (`kill-pane`) on switch and opening a different pane —
  the seat's tmux target must stay when it still makes sense.
- Pretending the handoff line *is* the old chat.
- Shelling out to `agent status` on every sample tick to fill usage.
- Letting the stoker's own session be switched or killed via switch.

## Decisions (author's calls; open to change on review)

### Identity of the place

The place is `(name, cwd, tmux pane id)`. The agent process and its product
SessionID die with the swap. After the sampler sees the new agent in that
pane:

- **Holds:** today "a new agent in the pane is free" (`holds.Hold`). An
  *explicit* switch is the exception. On switch, record a **pending rebind
  by pane** (old SessionID, By/Why/At). Until the sampler sees a new
  SessionID in that pane, `holdAt` / messageable treat the pane as still
  held (so the seat does not look briefly free). When the new SessionID
  appears, write the hold under that id and clear the pending entry. If
  the new agent never gets an id before `GoneFor`, the pending rebind
  expires like any orphaned hold. If no hold existed, nothing to rebind.
- **Ships:** already match by pane first (`service.shipOf`). No schema
  change; the next sample refreshes Name/ID on the ref as ships already do.
- **Queue:** drop waits bound to the old SessionID / that pane's old turn.
  The new agent starts with an empty wait state until its own hooks/pane
  speak.
- **Driven (stoker):** a builder-initiated switch **clears** driven state
  for that pane. The builder took the seat back; the stoker must not keep
  typing into the new harness under the old order. (Switch of the stoker's
  own session remains refused.)
- **Name:** keep the matchblox / tmux window (and tmux session when it is
  that session's one window) name. If the destination is Claude, send
  `/rename` at rest so Claude's own name matches (same as today's rename
  path). Codex / OpenCode / Cursor stay tmux-named only.

### Pane: reuse, do not recreate

Switch replaces the pane's running command in one step with
`tmux respawn-pane -k -c <cwd> -- <argv…>` (or the nearest equivalent the
installed tmux supports), guarded by the old agent's pid/start so a
replaced process is never signalled wrongly. It does **not** call
`KillSteps` (those close the pane) and does **not** call `new-session`.

**Partial failure:** if `respawn-pane` fails, the act fails: no success, no
jump, no queue drop, no hold rebind. The pane may be empty; the confirm
flash / CLI error says so. Do not invent a second automatic retry in this
pass. Prefer one atomic respawn so "old dead, new never started" is not a
planned two-step window.

Refuse switch when:

- `Pane == ""` (outside tmux),
- the row is the stoker,
- the target agent is not in `Startable` or not installed,
- the target agent is already the session's agent (no-op error).

When the session works or waits, require the same second confirm as kill
(`KillAnyway` / `--anyway` / second typed `y`).

Reuse `internal/service/lifecycle.go` patterns (`agentBin`, `launchArgv`,
guards by pid/start) rather than a parallel path. Add `switch` beside
`New` / `Kill` / `Rename`.

### Handoff line

Default first message (editable in the console form; `--handoff` on CLI,
empty string allowed):

```text
Read the working tree and any local notes. Summarise progress so far,
then continue the work from here.
```

That string is passed the same way `new`'s first message is (`launchArgv`).
It is not read from another product's transcript. Builder-supplied text
replaces the default entirely when set.

### Jump after switch

When the switch action succeeds, the console's result includes a nav jump
to the pane (`tmux switch-client -t <pane>`), same as Enter on a Sessions
row. CLI prints that the agent switched; if `TMUX` is set, the CLI may
also switch-client to the pane (builder's call in the plan whether CLI
jumps or only the console does — default: **both** when inside tmux).

### Canonical switch UI

**Console (Sessions):** one key (proposed `S`, free of the existing map in
`docs/keys.md`) opens a **Switch agent** form in the same lifeform family
as New / Rename: fields `agent` (←→ through installed Startable, excluding
current) and `handoff` (default text above). Enter shows the plan as the
pending action (exact argv steps), typed `y` runs it; mid-turn needs the
second `y` / anyway path. Esc cancels.

**CLI:**

```text
matchblox switch <who> --agent <name> [--handoff '...'] [--anyway]
```

`<who>` resolves like `kill` / `rename`. Confirm is implicit for CLI the
way `kill` already sends `Confirm: "y"`; `--anyway` sets Text to
`KillAnyway` when busy/waiting. Print the plan on refusal when the service
asks for anyway.

### Cursor: `--trust` on every matchblox launch

`launchArgv` for `cursor-agent` always includes `--trust` (before the
optional first message), for both `new` and `switch`. Stoker-configured
Cursor commands that already pass flags are left alone unless they go
through `launchArgv`.

### Cursor: account usage on Sessions

0009 left account and context unmeasured. This pass adds **account usage**
for Cursor when a cheap source exists:

- Prefer a cached parse of whatever `agent status` (or Cursor's status
  files) can provide, refreshed on a slow cadence (e.g. with limits /
  doors), **not** per sample tick.
- Show it in the same Sessions account column shape other agents use
  (`limits.Account` / provider `cursor` or `cursor-agent`).
- Context % may stay "not measured" in this pass if usage is the only
  cheap signal; do not block switch on context.

If no durable source is found without per-tick shell-out, document that
in the plan as deferred and still ship `--trust` + switch; do not invent
fake usage.

### Outside tmux in Sessions

Sessions continues to list agents with `Pane == ""` (detection already
finds them), but the row **marks** them — e.g. target column `outside
tmux` (panels already say `no pane` in places). Group/sort may keep them;
they are never switch targets. Do **not** hide them entirely in this pass
(hiding would surprise builders who still run agents in a raw terminal);
marking is the ruling.

## UX sketch

Confirm plan (console pending / CLI refusal hint), one shape:

```text
switch work: claude → cursor-agent
  end agent in %12 (works: needs anyway)
  respawn-pane -c /work/app — cursor-agent --trust '<handoff>'
  keep name work, pane %12
  jump to %12
```

## Left to the builder

- Exact console key if `S` conflicts after a keys audit.
- Whether CLI jumps tmux or only prints (default above: jump when `TMUX` set).
- Exact Cursor usage source and cache TTL after a short live spike.
- Test layout and fixtures (`you@example.com`, `/work/app`, `/home/u`).
- CHANGELOG / `docs/agents.md` / `docs/keys.md` / README touch list in the
  implementation plan.

## Out of scope reminders

- Full transcript import across harnesses.
- Non-tmux switch (#86).
- Changing squash-only merge policy or release process.

## Revised while building

The plan (`design/0012-agent-switch-plan.md`) ruled on these points:

- **Key `A`, not `S`.** `S` already reverses the sort (`internal/app/model.go:764`). `A` is free and stands for "agent".
- **Claude's own name** comes from `--name <name>` in `launchArgv` at respawn. No `/rename` step is needed.
- **Cursor account usage: #148 shipped the account, #151 the plan usage.** Since #148, Sessions and USAGE show Cursor's account (email and team, from `~/.cursor/cli-config.json`, marked `⬢`), and its context comes from Cursor's status line. A spike on 2026-10-21 (CLI 2026.10.19-6bbf2c4) found no plan usage in `agent status`, `agent about` or `~/.cursor/*.json`, so this plan deferred it. #151 then shipped it on main by another source: with `cursor_usage = true` under `[limits]`, USAGE shows the month's included usage from Cursor's API every 5 minutes. This branch adds nothing for usage.
- **The stoker never switches.** An act from the stoker's server, or from a pane it drives, is refused. The spec only says the stoker's own session cannot be switched.
- **The CLI jumps** with `tmux switch-client -t <pane>` when `TMUX` is set (the spec's default).
- **The CLI refusal keeps kill's shape** (`work works: y again switches it mid-turn`; docs name `--anyway`). It does not print the plan.

Revised during the build:

- **`queue.Forget` takes ids and the caller's clock:** `Forget(ids, now)`. The switch takes `HookedIn(pane)` before the respawn and forgets exactly those ids after it, so a new agent whose first hook lands before the switch returns is not forgotten with the old one.
- **One first-message check.** `proto.CheckFirst` / `proto.ErrDash` serve `new`, the switch service and the console form alike.
- **The Sessions key line shows `A switch`.** It is the first key a short line drops, before `↑↓` and `q`, so the line keeps what it showed before at every width that had no room for it.
- **A ship with no driver** says "its driver no longer runs", not "outside tmux".
- **Cursor always starts with `--trust`** (the spec's call). #151 put it in `launchArgv` on main for `new`, and switch starts Cursor through the same `launchArgv`, so this branch adds only its docs line and `TestLaunchArgvOfEachAgent`. That trusts the session's directory without asking; a builder may narrow it later if the trade-off bites.
- **The seat's name for an agent that keeps none.** The sampler names Codex, OpenCode and Cursor as their tmux window (#151), and `new` and `r` name the window as the seat, so a switch keeps it. When tmux names the window after its command, switch first renames the window to the seat's name; a window someone named otherwise keeps its name, and so does a window the seat shares with another pane. A guard that stops the respawn after the rename ran is reported as a switch that did not happen.
- **The console plan reads who → what → where,** not argv: `switch <name>: <from> → <to> in <pane>, <cwd>, then jump there`. That line reads clearer than the respawn argv, and neither the console nor the CLI asks the builder to read argv to decide; the service builds and guards the steps.
