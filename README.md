<picture>
  <source media="(prefers-color-scheme: dark)" srcset="www/assets/matchblox-mark-dark.svg">
  <img alt="matchblox" src="www/assets/matchblox-mark-light.svg" width="84">
</picture>

# matchblox

**A tool for modern software builders.**

[![CI](https://github.com/blox-eng/matchblox/actions/workflows/ci.yml/badge.svg)](https://github.com/blox-eng/matchblox/actions/workflows/ci.yml)
[![Go 1.26](https://img.shields.io/badge/go-1.26-00ADD8.svg)](https://go.dev/dl/)
[![License: MIT](https://img.shields.io/badge/license-MIT-black.svg)](LICENSE)

<p>
<img src="www/demo/replay-desk.svg" width="74%" alt="The matchblox console on a desk, on a demo machine: who waits for you, an answer picked from the agent's own options, the pull requests your sessions drive, usage and the machine, then a night the stoker keeps burning, its score in the morning, a new session started with its first message and a finished one killed">
<img src="www/demo/replay-phone.svg" width="24%" alt="The same console on a phone: one column, the selected session open in place, the answer peek at the bottom">
</p>

<p>
<img src="www/demo/stoker.svg" width="32%" alt="The stoker takes orders, on a phone on a demo machine: every quiet session says why, n asks how to run tonight with the gos you give, at 03:12 it works its own plan with a release waiting for your y and a trigger fired, the card in the morning, a new order by day, and the ways file">
</p>

One terminal console for every AI coding agent you run in tmux. It shows
which agent waits for you, what loads the machine, the state of every git
checkout, and the one safe action that resolves each thing. It works on the
host, over SSH, and from a phone.

Works with Claude Code, Codex, OpenCode, Cursor, and any agent in tmux.

**Docs: [docs.matchblox.com](https://docs.matchblox.com)**


## Quick start

1. Install:

   ```sh
   curl -fsSL https://matchblox.sh | sh
   ```

   Read the script first at [matchblox.sh](https://matchblox.sh/install.sh).
   You get the release binary for your machine, its checksum checked and,
   with `gh` logged in, proof that CI built it from this repository.

2. Run `matchblox` outside tmux (the script starts it for you). Answer
   "Where do your agents run?", then open the setup steps in Sessions:
   tmux, the queue hooks, the way back.

3. In another terminal, start your agents in tmux, for example
   `tmux new -s app claude`. When an
   agent waits for you, it comes to the top of Sessions. `Enter` jumps to
   it, and `prefix m` comes back.

The full walk is [Get started](https://docs.matchblox.com/start/).

## What it shows

- **Sessions, grouped by need**: who waits for you first (asks, waits,
  done), then who works, then who is quiet. A match for each: `✦` burning
  while it works, `╿` at rest, `│` spent. Context use, the account, and when
  to compact or clear.
- **Answer without leaving**: `a` opens the conversation with the selected
  agent, in the right pane where there is one, else a peek at the bottom of
  a phone. When it asks with its question tool, its options are there and a
  digit answers; an agent at work takes a message.
- **Switch the agent, keep the seat**: `A` runs another agent in the same
  pane (Claude Code, Codex, OpenCode or Cursor) with the session's name,
  directory and pull request. The new agent gets a handoff to read the
  working tree and carry on. `matchblox switch` does it from the shell.
- **Ships**: every pull request your sessions drive, on one track from CI
  to live, said in words. One that waits for you merges after a typed `y`.
- **The dock**: what `/usage` says for each account, in percentages; the
  machine; and the stoker. Press `n` before you sleep: each session that
  fills its context compacts itself and leaves one line on how to continue.
- **Orders for the stoker**: matchblox starts the stoker agent you choose
  out of sight, on a tmux server of its own. Say in your own words how to
  run the night ("merge them on green") and turn on the gos
  you give. It starts at once, keeps its own plan, follows [your ways
  file](https://docs.matchblox.com/ways/) and logs every step on a card.
  A merge, release or deploy you did not give waits on the card for your
  `y`, and what the stoker would wait for, matchblox fires itself.
- **Three layouts, read from the screen's shape**: one column on a phone,
  the list with a detail drawer on a phone held upright with a small font,
  and every column on a desk.
- **Machine health**, **detached busy loops**, **git** worktrees that are
  safe to remove, **recommendations** with their exact command, and 24 hours
  of **history**.

More in [The console](https://docs.matchblox.com/console/).

## Keys

| Key | What it does |
|---|---|
| `↑` or `k`, `↓` or `j` | Selects a row. |
| `Enter` | The safe action: go to the agent's pane. |
| `a` | Answers the selected agent, or sends it a message. |
| `x` | The destructive action. It runs only after a typed `y`. |
| `A` | Switches the selected session to another agent in the same pane. |
| `1-7` | Opens a tab: Sessions, Ships, Machine, Git, History, Panes, Stoker. |
| `u` | Cycles the dock of a phone: usage, the machine, the stoker. |
| `/` | Searches the open tab. |
| `f`, `n` | The stoker on or off; a night that ends in the morning. |
| `q` | Quits. The service keeps running. |

Every key: [Keys](https://docs.matchblox.com/keys/).

## From a phone

Set the startup snippet of your SSH app (Termius, for example) to
`matchblox`, or run `ssh -t ws-1 matchblox`. The console is the first
screen. Tap a row twice to answer it or send it a message, `Enter` to go to
its pane, `A switch` under it to give its seat to another agent; tap `◂ matchblox` in the tmux
status line (with `set -g mouse on`) to come back, or press `prefix m`. See
[The phone](https://docs.matchblox.com/phone/).

## Safety

Read-only by default. Every action shows its exact command before it runs.
A destructive action needs `x` and then a typed `y`, and checks its facts
again just before it runs. See [Safety](https://docs.matchblox.com/safety/)
and [SECURITY.md](SECURITY.md).

## Config

matchblox works without a config. The first run writes
`~/.config/matchblox/config.toml` with goals for this machine and the agents
it finds. Every key is in [config.example.toml](config.example.toml) and on
[Config](https://docs.matchblox.com/config/).

## Built on tmux

matchblox stands on [tmux](https://github.com/tmux/tmux), and takes its
model from it: a server that keeps running, and clients that attach from
any terminal. See [tmux, the engine](https://docs.matchblox.com/tmux/).

## Help

Questions: [Discord](https://discord.gg/tYxBUpGfX3). Bugs and ideas:
[GitHub issues](https://github.com/blox-eng/matchblox/issues).

## Design

[DESIGN.md](DESIGN.md) is how matchblox looks, moves and speaks: the match,
colour, motion, layout and copy. [design/](design/) holds the specs and plans.
[CONTRIBUTING.md](CONTRIBUTING.md) · [SECURITY.md](SECURITY.md) ·
[CHANGELOG.md](CHANGELOG.md)

*Light the match. Keep it burning.*

[matchblox.com](https://matchblox.com) · MIT · From [Blox Engineering](https://bloxng.com)
