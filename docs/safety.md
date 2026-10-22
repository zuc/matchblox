# Safety

matchblox runs commands on your machine for you. These rules hold for every
command.

- It reads, and does nothing to your agents, processes or checkouts by
  itself. Every action starts from a key. Two run on their own once you
  start them: [the stoker](stoker.md)'s compacts and the messages of a
  stoker agent you started (`n`, `f`), each logged with its command, and
  `[hooks] alert`, which runs the command you name on each alert.
- Every action shows its exact command before it runs, in a box that asks
  for its key: on a phone above the key line, wider in the middle. A tap
  cancels it; only a typed key runs it.
- `Enter` never runs a destructive action. That needs `x`, then a typed
  `y`. The stoker's compact and the messages of its agent are the steps that
  run without it: `n` or `f`, and the order you send with `Enter`, are your
  yes. A merge, a release or a deploy in a session the stoker messaged
  needs its chip on the launch sheet, or your `y` on the STOKER tab.
- Each step checks its facts again just before it runs: the same process
  and start time, a worktree still clean and unused, a session still idle,
  a pane with no prompt, question or draft.
  If a fact changed, the step does not run.
- Two consoles that confirm the same action run it once.
- Each step is a list of arguments, never a shell string built from data.
- matchblox runs as you, at the lowest CPU priority. It opens no network
  port. The service socket is in a directory that only you can open.
- It sends no pane text to a model.
- It reads no agent's login token, with one exception you turn on:
  `[limits] cursor_usage = true` reads the token Cursor's CLI keeps in the
  macOS Keychain (`cursor-access-token`), every 5 minutes while a Cursor
  session runs, and sends it to Cursor's API only, for the plan usage
  Cursor's own `/usage` shows. It keeps the token for that one call, and
  never writes or shows it.
- A setup step that edits a file (`~/.claude/settings.json`,
  `~/.tmux.conf`) shows the exact change first and keeps a backup.
- A remote host's stream uses its own key. That key can start only the
  stream, with a strict host key check and nothing forwarded.
- A phone key can go to every agent pane, so it has your power. The Secure
  Enclave and Face ID protect it. Remove a lost phone with
  `matchblox devices rm`.

To report a security problem, see
[SECURITY.md](https://github.com/blox-eng/matchblox/blob/main/SECURITY.md).
