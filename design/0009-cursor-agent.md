# Cursor CLI support: design

Date: 2026-10-21. Status: built (see "Revised after the live run" at the end).

## Intent

matchblox supports Claude Code, Codex, and OpenCode. The user also runs
Cursor's CLI (`agent`, also installed as `cursor-agent`) in tmux and wants
those sessions treated as first-class: shown in Sessions, placed in the
queue when they wait, and started by `matchblox new` and the stoker.

Cursor is closer to Claude than to Codex. It has a hooks system
(`~/.cursor/hooks.json`) and writes a JSONL transcript that marks the end
of each turn. **Agreed with the user:** Cursor gets Claude-style hooks for its
state, and setup writes matchblox's hook into `~/.cursor/hooks.json`. It is
not limited to Codex-style pane guessing.

## Goals (first pass)

1. **Detection.** A tmux pane that runs Cursor's CLI appears as a session
   whose agent is `cursor-agent`, and so does the stoker when it runs Cursor.
   Cursor's background `worker-server` process does not appear.
2. **State from hooks.** Cursor's hook events reach `matchblox hook`, which
   turns them into matchblox's existing events. Then the queue shows asks,
   waits, done, and new prompts as it does for Claude.
3. **Fallback state.** Without hooks, the pane decides busy or idle, as it
   does for Codex today. The asks and permits patterns in `said` cover
   Cursor's prompts.
4. **Setup door.** When Cursor is installed, a "Cursor hooks" door adds the
   matchblox entries to `~/.cursor/hooks.json`. Like the Claude hooks door,
   it shows a preview diff, makes a backup, edits idempotently, and leaves
   other hooks alone.
5. **Starting it.** `matchblox new --agent cursor-agent` (alias `cursor`) starts Cursor, and
   `[stoker] agent = "agent"` (or `cursor-agent`) runs it as the stoker.
   The stoker brief lists cursor-agent among the `--agent` values.
6. **Docs and tests.** `docs/agents.md`, `docs/limits.md`, `docs/config.md`,
   `docs/stoker.md`, the README's "Works with…" line, the CHANGELOG
   `[Unreleased]` section, and tests for each piece above. A Cursor pane is
   added to the demo fixture only if the goldens stay manageable (the
   builder's call).

## Non-goals (later passes)

- **Context %, model, and burn.** Possible sources: the token counts in the
  `stop` hook payload (`input_tokens`, `cache_read_tokens`, …), Cursor's
  status-line payload (`session_id`, `transcript_path`, `model`), and the
  per-chat `store.db` (SQLite). The first pass shows Cursor as "not
  measured", as Codex is on macOS.
- **Account.** The login is not in a file (most likely the Keychain).
  `agent status` prints it, but the sampler must not shell out on every
  tick. Left blank for now.
- **Merge guard.** Cursor's `preToolUse` can return `permission: "deny"`, so
  the guard could cover Cursor later. In the first pass, Cursor is listed
  with Codex and OpenCode as unguarded (`stoker.Unguarded`).
- **Compact and `/rename`.** These stay Claude-only. Cursor is renamed in
  tmux only.
- **Question-tool menus (`panes.Pick`).** Not in this pass.

## Anti-goals

- Counting the `worker-server` node process, or any non-interactive
  `agent -p` run, as a waiting session.
- Writing to `hooks.json` without the door's preview and backup, or
  breaking a user's existing Cursor hooks.
- A Stop hook that prints output Cursor acts on. A `followup_message` makes
  Cursor start another turn, so the Cursor path of `matchblox hook` must
  print nothing, or `{}`.
- Matching the bare word "agent" as a generic process name. It is too
  common.

## What the author's machine showed (2026-10-21, cursor-agent 2026.10.19-6bbf2c4)

- **The command.** `~/.local/bin/agent` and `~/.local/bin/cursor-agent` are
  symlinks to `~/.local/share/cursor-agent/versions/<ver>/cursor-agent`.
  That is a bash script that execs
  `~/.local/share/cursor-agent/versions/<ver>/node …/index.js [args]`.
  - The live process's comm is `node` (macOS shows the full path).
  - argv[1] is `…/cursor-agent/versions/<ver>/index.js`.
  - The background worker runs as `node …/index.js worker-server` and
    outlives `agent -p`. Its pid is listed in `…/versions/<ver>/.running/`.
- **Chats.** `~/.cursor/chats/<md5(cwd)>/<chatId>/meta.json` holds
  `{"schemaVersion":1,"createdAtMs","updatedAtMs","hasConversation","cwd"}`,
  next to `store.db` (SQLite, tables `blobs` and `meta`).
- **Transcript.** `~/.cursor/projects/<cwd with / turned into ->/agent-transcripts/<chatId>/<chatId>.jsonl`
  holds lines `{"role":"user"|"assistant","message":{"content":[{"type":"text","text":…}]}}`.
  A finished turn ends with `{"type":"turn_ended","status":"success"}`.
  There are no token counts.
- **Hooks.** The bundle reads user hooks from `~/.cursor/hooks.json`, and
  also from project `.cursor/hooks.json`, enterprise
  `/Library/Application Support/Cursor/hooks.json`, and team-managed paths.
  - Events: `sessionStart`, `sessionEnd`, `beforeSubmitPrompt`, `stop`,
    `afterAgentResponse`, `afterAgentThought`, `preToolUse`,
    `postToolUse`, `beforeShellExecution`, `afterShellExecution`,
    `beforeMCPExecution`, `afterMCPExecution`, `beforeReadFile`,
    `afterFileEdit`, `subagentStart`, `subagentStop`, `preCompact`,
    `notification`.
  - Every payload has `conversation_id`, `generation_id`,
    `hook_event_name`, `cursor_version`, `workspace_roots`, and
    `user_email`. Most also have `transcript_path`, and some have
    `session_id`.
  - `stop` adds `status` (`completed` | `aborted`), `loop_count`, and token
    counts.
  - `afterAgentResponse` adds `text` (the reply).
  - `preToolUse` can answer `permission`, `user_message`, and
    `agent_message`.
  - The file format is Cursor's documented one:
    `{"version":1,"hooks":{"<event>":[{"command":"…"}]}}`. **The builder
    must confirm this against the bundle or Cursor's docs before writing
    the door.**
- **Login.** `agent status` prints "✓ Logged in as …" and the team. No auth
  file was found under `~/.cursor`.

## Live run (2026-10-21, interactive `agent --trust` in a tmux pane, project `.cursor/hooks.json`)

- **Process.** The interactive session's argv is
  `/Users/<u>/.local/bin/agent --use-system-ca …/cursor-agent/versions/<ver>/index.js --trust`.
  argv[0] is the launcher path (`agent` or `cursor-agent`) and the comm is
  `node`. Its child is `…/versions/<ver>/node …/index.js worker-server`.
  The child also runs `npm exec typescript-language-server`. `underAgent`
  already drops nested agents, but the worker must not match on its own
  either: after `agent -p` exits it outlives its parent.
- **Hooks fire with `TMUX_PANE` and `TMUX` set**, from the pane's server.
- **Order of one turn with an approval:**
  1. `beforeSubmitPrompt`
  2. `preToolUse` (`tool_name: "Shell"`, `tool_input.command`,
     `tool_use_id`)
  3. `beforeShellExecution`
  4. the menu draws and waits; no hook fires
  5. `y`
  6. `postToolUse`
  7. `stop` (`status: "completed"`, token counts)
  8. `afterAgentResponse` (`text`), which comes **after** `stop`
- **Interrupting and quitting.** Esc mid-turn gives `stop` with
  `status: "aborted"`. Ctrl-C twice gives `stop` with `status: "error"`,
  then `sessionEnd` with `reason`, `final_status`, and `duration_ms`.
- **Ids.** Every payload has `session_id` equal to `conversation_id`. It
  is the chat id, the same as the `agent-transcripts/<id>/` directory.
  `transcript_path` is null on `sessionStart` and `beforeSubmitPrompt`,
  and set from the first tool call on.
- **An invalid key disables everything.** A `hooks.json` that listed
  `notification` ran no hook at all, and nothing was logged. Without
  `notification`, every listed hook fired.
- **No Claude-format hooks by default.** The bundle has paths for
  `~/.claude/settings.json` and project `.claude/settings.json`, but a
  project `.claude/settings.json` hook did not fire with default
  settings. matchblox's Claude hooks therefore don't double up with
  Cursor's today.
  - **Risk to handle:** if a user turns on Cursor's Claude-hooks
    compatibility, Cursor would also call `matchblox hook <ClaudeEvent>`
    with Cursor payloads.
  - The Claude parser should drop a payload that has `cursor_version`, so
    Cursor's own hook is the one source.
- **Approval menu in the pane** (the `said` patterns come from this):
  ```
  $ touch second.txt && date Waiting for approval...
   $  touch second.txt && date in .
   Run this command?
   Not in allowlist: date, touch
    → Run (once) (y)
      Add Shell(touch), Shell(date) to allowlist? (tab)
      Run Everything (shift+tab)
      Skip & tell the agent what to do instead (esc or n)
  ```
  - At rest the prompt line reads `→ Add a follow-up` (`→ Plan, search,
    build anything` before the first prompt).
  - While working, the line shows `ctrl+c to stop` on the right.
- **Context in the status bar.** The status bar shows `Auto · 5.7%`,
  which is the model plus context use. That is a cheap source for the
  later context pass.

## How it fits matchblox today

- No agent interface exists. Agents are names in `setup.KnownAgents`,
  `service.Startable`, and `state.Agents`, plus the switch in
  `internal/sample/sampler.go` (around line 589).
- `Sampler.agentName` matches the comm, then argv[0]'s basename. For an
  interpreter (`node` …) it takes the first non-flag argument, which for
  Cursor is `index.js`. Cursor needs a specific rule here.
- `queue` joins a hook event to a session by `SessionID`. When the session
  has none (non-Claude), it joins by `ev.Pane` (`$TMUX_PANE`), at
  `internal/queue/queue.go:160-180`. Cursor events can therefore carry
  `conversation_id` as their SessionID and join by pane.
- `matchblox hook <event>` (`cmd/matchblox/main.go:375`,
  `internal/hooks/hook.go`) parses Claude's payload.

## Decisions taken in this design (Claude's calls, open to change)

- **The agent's name is `cursor-agent`.** `setup.KnownAgents` has listed
  `cursor-agent` since #34, so existing configs already have
  `agents = [..., "cursor-agent"]`. Keeping that name means there is
  nothing to migrate.
  - The name is used in config `agents`, `--agent cursor-agent`, the UI,
    and stoker briefs.
  - `--agent cursor` is accepted as an alias.
  - Detection maps a node process to `cursor-agent` when its argv has
    `/cursor-agent/versions/<ver>/index.js` and no `worker-server`
    argument, and only while `cursor-agent` is listed in `agents`, as for
    every agent.
- **The hook command is `matchblox hook --cursor <event>`.** Its parser
  reads Cursor's payload (`conversation_id` becomes the event's
  SessionID) and maps the events:
  - `beforeSubmitPrompt` → UserPromptSubmit
  - `stop` with `status: "completed"` → Stop. The message and asks come
    from the last assistant text in `transcript_path`'s tail, or else
    nothing.
  - `stop` with `status: "aborted"` → the same as UserPromptSubmit. It
    clears any wait and does not queue the session as done: the person
    interrupted it on purpose.
  - `sessionStart` → SessionStart
  - `sessionEnd` → SessionEnd
  - `stop` with `status: "error"` → treated like aborted. It fires when
    Ctrl-C quits an idle session, just before `sessionEnd`.
  - `postToolUse` → PostToolUse, which clears a permission wait.

  `notification` is **never written to `hooks.json`**. Cursor accepts the
  name in its bundle, but a file that lists it is silently ignored as a
  whole (verified live). The door writes only the five events above:
  `sessionStart`, `sessionEnd`, `beforeSubmitPrompt`, `stop`, and
  `postToolUse`. A test pins that list, because one invalid key turns
  every hook off.

  `preToolUse` is **not installed** in this pass. The Cursor path never
  runs the merge guard and never prints Claude's `hookSpecificOutput`. It
  prints nothing on stdout, so a `stop` never returns a
  `followup_message`.
- **Hooks from other runs are kept out by the service, not the hook.**
  The hook cannot tell an `agent -p` run from an interactive one, and the
  payload has no flag for it. So when the service receives a Cursor
  event, it marks the event as Cursor's (a field on `hooks.Event`, e.g.
  `Agent: "cursor-agent"`). The queue joins that event only to a session
  whose agent is `cursor-agent`, either by pane or by the chat id once
  the sampler learns it.
  - A stray event from an `agent -p` inside a Codex pane matches no
    Cursor session and is dropped.
  - A `-p` run inside a pane that already runs interactive Cursor is
    still possible, but rare. That is accepted for now.
- **Permits on hooked Cursor sessions come from the pane.** The live run
  showed that no hook fires when Cursor starts waiting for approval
  (`preToolUse` and `beforeShellExecution` fire just before the menu
  draws, and for allowlisted commands too). So the pane is the only
  source.
  `Queue.Merge` takes a hooked session's state from hooks only. For
  sessions whose agent is `cursor-agent`, it also takes `s.Permits` /
  `s.Asks` from the pane while the last hook event is not a Stop. Claude's
  path does not change.
- **SessionStart's progress prompt.** For Claude, the SessionStart hook
  injects the progress prompt. It is added for Cursor only if Cursor's
  `sessionStart` supports `additional_context`. Otherwise it is skipped.
- **Launch commands.**
  - `matchblox new --agent cursor-agent` runs `cursor-agent [first]`, or
    `agent` when only that is on PATH.
  - The stoker command is whatever `[stoker] agent` says. When it is
    `agent` or `cursor-agent`, the stoker's agent is `cursor-agent`.
- **Door placement.** The door is gated on Cursor being on PATH or
  `~/.cursor` existing, the way `usesClaude` gates Claude's doors. It sits
  next to the Claude hooks door.

## Left to the builder

- The exact `said` regexes. Build them from the captured screens below.
- Whether `sessionStart` can return `additional_context` for the progress
  prompt. Test it live, and drop the prompt for Cursor if it can't.
- Test structure, fixture names, and how much of the demo or golden
  screens to touch.

## Revised after the live run (2026-10-21)

An end-to-end run of the built branch (an isolated service, live tmux, a
project `.cursor/hooks.json`) changed three decisions above:

- **Six events, not five.** Cursor writes its transcript after its `stop`
  hook runs, so `transcript_path` at `stop` held no reply, or the previous
  turn's. The reply comes from `afterAgentResponse` (its `text`), mapped to
  a `Reply` event that fills the line of its turn's finished wait and makes
  it a question when it asks. The transcript is not read.
- **Stop and Reply in either order.** Cursor runs both hooks at the same
  moment, as separate processes. Events carry `Turn` (`generation_id`), and
  the queue pairs a Reply with its own turn's Stop whichever comes first.
- **Print runs and other `agent`s.** A Cursor process with `-p`/`--print`
  is no session. `matchblox new` runs `agent` only when it resolves into
  `…/cursor-agent/versions/`. When two hooked chats share a pane, the one
  that spoke last is the pane's.

Lines under Cursor's `▀` box edge are its footer, never what it said.
