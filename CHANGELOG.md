# Changelog

All notable changes to matchblox are in this file. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- **The box opens: pair a device in one gesture.** The door "Pair a
  device" and `DEVICES` on the Machine tab open the pair page in the
  console. It shows the matchbox with the code on its label. When the app
  reaches the host the box knocks; when it claims the code the box opens,
  the match strikes, and the label shows the device's name. Then the page
  shows when the device is in and what it did first. A device sees
  `DEVICES` but cannot pair or remove: it says to use the host's own
  terminal.
- **The app has no pairing form.** The scan is the consent: the box on the
  screen catches the code and opens, with haptics, and the console slides
  in. A device that cannot scan can paste the link. A failure shows the fix
  on the screen.
- **The pair code is half its area.** The v2 link packs the same facts in
  binary, so the code is at most 57 × 29 cells (v1 was 77 × 39) and fits a
  normal terminal.

### Changed

- **Build the app again with this release.** An older app cannot read the
  v2 link and says "This code is damaged…". The new app reads v1 and v2.

## [0.11.0] - 2026-10-21

### Added

- **The app runs on the iPad.** It was an iPhone app, so an iPad showed it
  in a small phone window. It now fills the screen and turns to each side.
- **The app has an icon: the matchblox mark.** It was blank on the home
  screen. `make icon` makes it from `ios/AppIcon.svg`.

## [0.10.3] - 2026-10-21

### Fixed

- **The phone shows the console's glyphs, not `_`.** The app's ssh session
  sends no locale, so tmux took the phone for an ASCII terminal and drew
  each match, bar, `▰`, `●` and `◂ matchblox` as `_`. The console now
  starts tmux with `-u`, so its client is UTF-8 whatever the locale.

## [0.10.2] - 2026-10-21

### Fixed

- **Context use is a share of the session's real window.** A session of a
  1M model, such as Opus 5.5 without `[1m]` in the model setting, showed
  its context against 200k: 99% while it had room for 800k more, then 20%
  past 200k. The stoker could compact it at about 170k. The window now
  comes from the agent first: Claude Code's status line gives it for each
  session. Then from `[sessions.windows]`, then from the Claude models that
  matchblox knows. Without a window the session is "not measured" and the
  row shows `—`; matchblox no longer guesses 200k. `[sessions.windows]`
  no longer wins over a window that the agent reports.
  ([#118](https://github.com/blox-eng/matchblox/issues/118))
- **A new session shows in Sessions within a second.** It took up to 10
  seconds: the processes were read every fifth sample. tmux is now listed
  at every sample, and a new pane, or a pane that starts an agent, has its
  processes read at once. A session from `N` or `matchblox new` shows at
  once. ([#119](https://github.com/blox-eng/matchblox/issues/119))
- **A session working through its subagents no longer waits for you.** A
  turn that dispatches background subagents ends at once, and its Stop hook
  put the session in "waits for you" while the subagents worked, and while
  the turn their results start ran. Subagent-driven development showed as
  waiting most of the time. A turn that ended asking nothing now stays out
  of the queue while the session works, and comes back when the work ends,
  waiting since then, not since the turn that dispatched the subagents. A
  question or an approval still waits.
- **A peek shows the question, not the progress line under it.** `asks` in
  `matchblox peek` and in the stoker's wakes was the reply's last line,
  often the `Progress [...]` line that ends each reply.

## [0.10.1] - 2026-10-15

### Fixed

- **Releases carry their binaries again.** v0.8.0, v0.9.0 and v0.10.0 were
  published with none: the repository's releases are immutable, and a
  published release takes no assets. A release now stays a draft until its
  binaries, checksums and SBOMs are attached, and the install script
  kept installing v0.7.0 meanwhile. This release is the first to
  carry what v0.8.0 to v0.10.0 added.

## [0.10.0] - 2026-10-14

### Added

- **Start, rename and kill a session from matchblox.** `N` opens New
  session: where (a path, or the Git tab's repositories and worktrees), an
  optional branch for a new worktree beside the repository, a name, the
  agent (the installed Claude Code, Codex or OpenCode) and a first message.
  It starts in a tmux session of its own, and the tmux session, its window
  and Claude Code's own name are that one name. `r` renames a session the
  same way, all alike; a busy Claude Code session takes its name when it
  rests. `x` kills any session, not only a stale one: its exact commands
  on the RUN line, a typed `y`, and a second `y` for one that works or
  waits for you. Its worktree and transcript stay. All three are on the
  key line, a tap away on a phone, which drops the spare keys first. The
  same as commands: `matchblox new`, `matchblox rename`, `matchblox kill`.
- **The stoker's hands start, rename and kill sessions.** `matchblox new`
  works only in a run and only on your tmux server, and the guard watches
  the session it starts. `matchblox kill` ends only a session at rest you
  did not park, with the new `kills` go chip or your `y` on the card.

## [0.9.0] - 2026-10-14

### Added

- **The stoker takes orders, in your own words.** `n` and `f` start a
  stoker agent (Claude Code, or `[stoker] agent`) on a tmux server of
  matchblox's own, out of your `tmux ls`; `Enter` on the STOKER tab opens
  it in a popup, `Q` quits it. Then they open a launch sheet: say how to run tonight, turn on
  the gos you give, and `Enter` starts the run at once. The brief carries
  the machine as it is, so the agent needs no lookup to start, and it keeps
  its own plan on the card, rewriting it as it goes: nobody approves it. A
  night is sealed once it runs. By day, `o` sends a new order as one line,
  `New order: …`, and the run goes on with its done steps. matchblox calls
  no model. The stoker's own
  session stays out of Sessions, and the header says when it waits for you.
- **Your ways.** One markdown file, `ways.md`, next to the config, holds
  your standing rules and your `# Recipe: <name>` procedures. The launch
  sheet offers each recipe by name. The setup door **Write your ways**
  writes a starter. See [Your ways](https://docs.matchblox.com/ways/).
- **The stoker acts through your sessions, and matchblox wakes it** when a
  session asks something or its turn ends (with the first line of the
  question, or the last line it said), a pull request appears or moves, a
  usage window passes another 10%, and before the night ends, at most once
  a minute. Its hands are `matchblox peek`, `send`, `compact`, `hold` and
  `release`, under the guards of the console, and `max_steps` (60) bounds
  the messages of a run. The setup step "Add the queue hooks" lets them
  run in Claude Code without a prompt.
- **Triggers the service fires itself.** `matchblox stoker when` sets one
  for the run: at a pull request's stage (`--ship 4573 --stage merged`,
  `red` for CI failed) or at the end of a session's turn (`--rest api`),
  matchblox sends, compacts, releases or holds as the stoker said, once,
  under the same guards, logs it to the card and tells the stoker. The
  stoker sets a trigger where it would otherwise poll.
- **The STOKER tab** shows your words and the gos, the plan, what waits
  for your go, the triggers, the standing rules and every step with its
  outcome and the exact command it ran. `y` gives the go a step waits for,
  and `a` opens the session of a failed step.
- **A merge, a release or a deploy needs its go.** The gos are the chips
  on the launch sheet (`Tab` to them, `Space` turns one on), never read
  from your words. In a session the stoker messaged, a `PreToolUse` hook
  stops `gh pr merge`, `gh release` and `gh workflow run` without one, and
  matchblox puts the command on the card by itself
  (`⏸ waits for you: api wants to run gh pr merge 4556`), where `y` lets
  one command of that kind through, once. The session reads why it was stopped, and a
  here-document a command reads as text (`cat > notes.md <<'EOF'`) needs
  no go. Codex and OpenCode have no such hook, and the card says so.
- **Sessions say why they are idle** (`#4556 waits your merge`, `CI red`,
  `stuck: …`, `back 14:20`). `z` parks a session so nothing types into it,
  and `g` groups the list by need, state, repo or not at all.
- **`matchblox sessions`, `ships` and `usage`** print one line a row, or
  JSON with `--json`, for a script or an agent.
- **The matchblox iPhone app opens your console** (#88). Scan the code of
  `matchblox pair` once, and the app opens the console of the host. Face ID
  or the passcode unlocks the key each time the app opens. The key stays in
  the Secure Enclave, and each host gets its own key. The app trusts only
  the host key from the code, so a host with a new host key does not open
  until you pair again. With two or more hosts, the app lists them, and a
  swipe removes one. When the app can't reach a host, it asks for a new
  address and keeps the pairing. Build the app from `ios/` until it is on
  TestFlight.


## [0.8.0] - 2026-10-14

### Added

- **`matchblox devices` lists the phones and laptops that open this host,
  and removes one.** `matchblox devices rm <name|fingerprint>` removes a
  key and keeps a backup of `authorized_keys`. A laptop shows the comment
  of its key as its name, which is `matchblox` for a key from
  `matchblox connect`.
- **`matchblox pair` shows a one-time QR code for the coming iPhone app**
  (#88). The code holds a key that works once and that sshd refuses after
  5 minutes. Stop the command, or close the terminal, and the key is gone.
  When a phone pairs, the host prints its name and fingerprint, so a phone
  that is not yours is seen at once. A key that already opens a login on
  the host cannot pair, so a pairing never changes your own ssh login.
- **A phone key opens the console.** The new key kind (`gate phone`) starts
  the stream and the console. A laptop key from `matchblox connect` keeps
  the stream alone.

### Changed

- **The home session uses `window-size latest`.** Its size now follows the
  device you used last, so a phone and a laptop on one session do not
  keep the size of the smaller one.

## [0.7.0] - 2026-10-13

### Changed

- **matchblox.com shows the console on an iPhone Duo.** Folded it plays the
  phone's one column; it unfolds sideways into the desk layout partway
  through, and a tap folds it again. It replaces the Phone, Wide and Desk
  switch, and the 107×60 recording that only the switch used.

## [0.6.0] - 2026-10-12

### Added

- **The console fits the screen it is on.** A closed phone shows one line
  a session, grouped by what each needs from you (waits for you, working,
  quiet), and the selected one opens in place with what it said and its
  keys. A screen held upright is read by its shape, so its keyboard does
  not change the layout: under 90 columns it is this one column, from 90
  (a phone with a small font) the detail is a drawer beside the list. On a
  screen held sideways, from 60 columns, the selected session's detail
  sits beside the list. Every column of a list lines up, the name taking
  what is left, and the selected row is washed across the whole pane with
  its name in the accent, the others muted.
- **Answer or message from a peek.** `a`, or a tap on a selected session,
  opens the conversation: the detail pane itself where there is one, the
  list staying beside it, else a panel at the bottom of a phone, with
  what the agent said last and your answer; a session
  that works gets your line as a message, which Claude Code queues. `→`
  and `←` (a swipe on a phone), or a tap on `next →` and `← prev`, move
  the peek to the next or previous session. When Claude
  Code asks with its question tool, its options are there, numbered: a digit
  or a tap picks one, several questions come one at a time, and a question
  of several choices ticks with `Space`. `Enter` sends, and the peek moves
  to the next session that waits.
- **Ships.** Every pull request a session drives, on one track from open to
  live: CI, a conflict, a review, the merge and the deploy, said in words.
  `x` merges a ship that waits for you, after a typed `y`, pinned to the
  commit that was read green. Name the
  workflows that deploy a repository under the new `[ships.deploy]` config
  key; by default every run on the merge commit counts.
  A running stage says how long it has run (`merged · deploying · 2h`,
  `CI · 6 of 9 jobs · 12m`) or waited (`merged · deploy queued · 1h`). A
  failed run on the merge commit counts once a newer run of its workflow
  on the default branch passed.
- **Usage, the machine and the stoker in the rows the list leaves free.**
  Each account's windows as `/usage` says them, the machine (cpu, memory,
  the disk your home is on, the agents that run) and the stoker, whose
  panel takes the dock while a run is in charge; `c` opens its whole card.
  With rows to spare the dock grows: forecasts, the top processes, the
  recent steps.
- **Recommendations you can put away.** They sit under the sessions now.
  `d` dismisses one until its evidence changes, and `h` folds them all to
  one line; the console remembers both.

### Changed

- The tabs are Sessions, Ships, Machine, Git, History and Panes, each
  named, and the digits `1-6` follow that order. The Queue is the top of
  Sessions, the processes are on Machine, and `p` opens Ships.
- Usage speaks in `/usage` percentages and keeps its forecast; the matches
  and sparks of the header are gone.
- An answer goes on `Enter`: the peek shows what goes where, so there is no
  typed `y` for it any more.
- The keys are at the bottom of the screen, each a tap target, where a
  phone app keeps them above its keyboard.
- **The queue hooks include the question tool.** A `PreToolUse` hook for
  `AskUserQuestion` tells the console what Claude Code asks before its menu
  draws, so the answer peek shows the options in auto mode too, where the
  question reaches the transcript only once it is answered. The step "Add
  the queue hooks" asks to be opened again, with the one entry it adds.

## [0.5.1] - 2026-10-12

### Fixed

- **A session working through its subagents reads `agents`, not idle.**
  Its turn ended, but their results start its next turn: the queue no
  longer lists it as waiting for you, and the stoker still compacts it at
  that turn's end when its pace calls for it. A turn that ended with only
  background shells running stays idle.

## [0.5.0] - 2026-10-12

### Added

- **The stoker compacts each session at the moment that suits it.** It
  follows each session's pace (tokens per point of its progress bar): one
  that finishes under the line is left alone, one whose next step would
  cross it is compacted at the end of this turn. The card says what it
  decided for every session, and why.
- **A session that ended its turn is idle, even while its background
  works.** Its detail shows the subagents and shells it left running, and a
  shell that has run for an hour or more can be ended with `x`.
- **A net under every session.** A door turns Claude Code's own
  auto-compact on at the stoker's line, the only thing that compacts inside
  a turn.

### Fixed

- The site's news line names the latest release: each release writes it
  from the first headline of its changelog section, and links to the
  release.

## [0.4.0] - 2026-10-11

### Added

- **See which sessions your plan's limit stopped, and until when.** A
  session whose turn the limit refused, or whose account spent its window,
  says `out` with the time it can work again, in Sessions and in
  `matchblox status`.
- **The stoker's card tells the truth about the limit.** A run the limit
  stopped says so on its first line (`limit out 11:51 → 14:50`), and a
  session that is out is not counted as ready.
- **A 1M window can fill.** `compact_tokens` (350k by default) fills a
  session before `compact_at`'s percentage does, so the stoker and the
  `! compact` mark act on large windows too. To keep the percentage alone,
  set `compact_tokens = 0`.
- **Every stoker card is kept** in `stoker-runs.jsonl`: a new run never
  erases the one before it.

## [0.3.0] - 2026-10-11

### Added

- **See all of matchblox before you install it.** The replay on
  matchblox.com and in the README walks one evening into the morning: an
  answer typed in one line, a compact and a worktree removal you confirm,
  a busy loop killed, then a night the stoker keeps burning and its score
  at 07:00.

### Fixed

- **A session without hooks shows what it asked** on its queue row, so `a`
  answers a question you can read.
- **A removed worktree leaves the Git tab at once**, not at the next scan.
- One merged worktree and one process are named in the singular.

## [0.2.0] - 2026-10-11

### Added

- **You can see the night.** While the stoker or a night runs, the whole
  console goes dark blue and only the matches keep their fire.
- **Take over with one key.** The first time you act on a session during a
  run, the console asks `Take over?`. `y` rests the stoker while you work,
  and it goes on by itself once you leave. `n` keeps it on.
- **Wake up to the night's score.** Every night and every stoker run ends
  with a card in the queue, even when there was nothing to do: what it
  watched and compacted, the tokens it gave back, and `ready 6/7`, how
  many sessions can go on, with what each of the others needs.
  `matchblox status` shows it too.

## [0.1.1] - 2026-10-11

### Fixed

- **docs.matchblox.com shows the docs of the latest release**, with the
  release install. It showed the text from before the first release.

## [0.1.0] - 2026-10-10

The first release: one console for every coding agent you run in tmux, on
the host, over SSH and from a phone.

### Added

- **The queue**: every agent that asks for permission, waits for input or
  finished its turn, oldest first, with its last line. `Enter` goes to its
  pane, and `a` answers in one line after a typed `y`. Claude Code reaches
  the queue through its hooks; Codex, OpenCode, Gemini CLI, aider,
  cursor-agent and any command named in `agents` reach it from their pane.
- **Each session is a match**: `✦` burning while it works, `╿` at rest
  while it waits, `│` spent at the compact limit.
- **Sessions**: how full each context is (Claude Code, OpenCode, and Codex
  on Linux), the queue's word (`asks`, `waits`, `done`, `paused`), the
  account each one runs under, and its open pull request as `#N`.
- **The machine**: load, memory and health, detached busy loops, git
  checkouts and merged worktrees, recommendations with one guarded action
  each, and 24 h of history.
- **The stoker**: press `n` before you sleep, or `f` for a lunch. Each
  Claude Code session that fills its context compacts itself and leaves one
  RESUME line. In the morning, STOKED shows each one, its context before and
  after. A session that waits for you, or holds a line you started to type,
  is left as it is.
- **Your limits**: one line for each account under the metrics, with the
  matches left in this 5-hour window, the sparks left this week, and if the
  week lasts (`out Fri ~20:00`). When it will not, the queue names the
  session that burns the most, one key from a compact. Claude Code's come
  from your status line, Codex reports its own.
- **Any terminal, even a phone**: a tap selects, a second tap acts. The way
  back, `prefix m` or a tap on `◂ matchblox`, returns to the console from
  any pane; `m` in its preview turns the tmux mouse on for the tap.
- **Remote hosts**: `matchblox connect <host>` connects a host once, with a
  key that can start only the console's stream, and `matchblox <host>`
  opens its console.
- **Setup in the queue**: tmux, the queue hooks, your limits, the way back
  and a guide session, each shown as the exact change before a typed `y`.
- **Install in one line**: `curl -fsSL https://matchblox.sh | sh` installs
  the release binary for Linux or macOS, amd64 or arm64, its checksum and
  build provenance checked, and starts it. `go install` builds the same
  version, and `matchblox version` names it.
- **Docs** at [docs.matchblox.com](https://docs.matchblox.com).

[Unreleased]: https://github.com/blox-eng/matchblox/compare/v0.11.0...HEAD
[0.11.0]: https://github.com/blox-eng/matchblox/compare/v0.10.3...v0.11.0
[0.10.3]: https://github.com/blox-eng/matchblox/compare/v0.10.2...v0.10.3
[0.10.2]: https://github.com/blox-eng/matchblox/compare/v0.10.1...v0.10.2
[0.10.1]: https://github.com/blox-eng/matchblox/compare/v0.10.0...v0.10.1
[0.10.0]: https://github.com/blox-eng/matchblox/compare/v0.9.0...v0.10.0
[0.9.0]: https://github.com/blox-eng/matchblox/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/blox-eng/matchblox/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/blox-eng/matchblox/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/blox-eng/matchblox/compare/v0.5.1...v0.6.0
[0.5.1]: https://github.com/blox-eng/matchblox/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/blox-eng/matchblox/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/blox-eng/matchblox/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/blox-eng/matchblox/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/blox-eng/matchblox/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/blox-eng/matchblox/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/blox-eng/matchblox/releases/tag/v0.1.0
