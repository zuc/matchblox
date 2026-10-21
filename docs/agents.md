# Agents

matchblox finds every agent that runs in a tmux pane. Start each agent in
a tmux session. Each command gives the console row under it.

**Claude Code**

```sh
tmux new -s app claude
```
```
✦ busy         app        app:1.1   ██████░░░░  62%
```

**Codex**

```sh
tmux new -s api codex
```
```
╿ asks   1m    api        api:1.1   █████░░░░░  55%   ❋ you@example.com · Plus
```

**OpenCode**

```sh
tmux new -s web opencode
```
```
✦ busy         web        web:1.1   ███░░░░░░░  32%   ▣ API key
```

**Cursor**

```sh
tmux new -s app agent
```
```
╿ asks   1m    app        app:1.1   █░░░░░░░░░   6%   ⬢ you@example.com · Acme
```

Cursor's CLI counts whether you start it as `agent` or `cursor-agent`. The
first run lists it in `agents` as `cursor-agent`.

**Any other agent**, for example aider

```sh
tmux new -s docs aider
```
```
╿ done   4m    docs       docs:1.1        —
```

The first run writes the agents it finds to `agents` in the
[config](config.md). Add any other agent there by its command.

An agent that keeps no name of its own (all but Claude Code) is named as
its tmux window, which `r` renames, or as its tmux session while tmux names
the window after the command.

## Context use

Claude Code, Codex and OpenCode show their context use, read from the
agent's own files: the transcript, the rollout, the session storage.
Cursor gives it to its status line: open the door "Show Cursor's context"
in Sessions, or run `matchblox setup`, and each Cursor session shows its
context, model and window from its first turn. A status line of your own
keeps running inside it; without one, the line shows what Cursor's bar
showed (`Auto · 5.7%`). Before the first turn a session is "fresh".

The context use is a share of the session's window. matchblox uses the
first window it finds, in this order:

1. The window the agent reports for the session. Codex and OpenCode report
   it in their files. Claude Code gives it to its status line: open the
   door "Show your limits" in Sessions, or run `matchblox setup`, and each
   session reports its window from its first turn.
2. `[sessions.windows]` in the [config](config.md), by model-name prefix.
3. The window of a Claude model that matchblox knows, such as 1M for
   `claude-opus-5-5`. A model setting that ends in `[1m]` gives 1M.

When none of them gives a window, matchblox does not guess one. The row
shows `—` and the detail says "not measured", with what to do. The stoker
then compacts the session at `compact_tokens` only, never at a percentage.

## The account

On a wide screen, Sessions shows the account of each session: the
provider's mark, then the email and plan, or "API key". Only those two
fields are read from the agent's auth files; no token reaches the console.
Cursor's is the email and team its CLI keeps in `~/.cursor/cli-config.json`
(`⬢ you@example.com · Acme`), and USAGE lists the account. Its plan usage,
the share of the month's included usage as Cursor's `/usage` shows it,
needs `cursor_usage = true` under `[limits]`: matchblox then asks Cursor's
API with the login token Cursor's CLI keeps in the macOS Keychain (see
[Safety](safety.md)).

## How an agent tells that it waits

Claude Code tells its waits through hooks: the setup step "Add the queue
hooks" adds them. One of them, `PreToolUse` for the question tool
(`AskUserQuestion`), hands the console the question and its options before
the menu draws: in auto mode Claude Code writes the question to its
transcript only once it is answered, so the hook is how the answer peek
shows the options. A second, `PreToolUse` for `Bash`, is the stoker's merge
guard: in a session the stoker drove, it stops `gh pr merge`, `gh release`
and `gh workflow run` unless the chip on the launch sheet gave the go, or
you gave it with `y` on the STOKER tab. The same step adds permission rules
(`Bash(matchblox sessions:*)`, `ships`, `usage`, `send`, `compact`, `hold`,
`release`, `stoker` and `peek`), so the stoker's hands run without a
prompt. The rules are in your user settings, so every Claude Code session
may call these commands without a prompt. The service refuses a `send`,
`compact`, `hold` or `release` typed in a pane that hosts an agent and is
neither the stoker's nor one the stoker drives (`only the stoker and the
sessions it drives act from an agent's pane: this one is neither`). Your
own shell runs no agent, so the commands work there. `peek` and the
other reads work from any pane. An install from before these hooks or
rules shows the step again, with the entries it adds. Every other agent is read from its pane: busy
while the pane changes, waiting when it is idle and its last lines ask.

Cursor tells its turns through its own hooks: the setup step "Add the
Cursor hooks" adds `matchblox hook --cursor` to `~/.cursor/hooks.json`, for
`sessionStart`, `sessionEnd`, `beforeSubmitPrompt`, `stop`,
`afterAgentResponse` and `postToolUse`, and for nothing else: Cursor
ignores the whole file, without a word, when one event in it is one it
does not take. A turn that ends waits for you as `done`, or `asks` when its
reply (from `afterAgentResponse`) ends on a question; one you stop with Esc
does not. No Cursor hook fires when its approval menu
(`Run this command?`) draws, so that wait is read from its pane. The hooks
are global, so an `agent -p` you run counts only in a pane where Cursor
runs.

An approval menu (Codex, OpenCode, Cursor) shows as `asks`. Answer it in its pane,
where `Enter` takes the selected choice.

See [Limits](limits.md) for what is not measured yet.
