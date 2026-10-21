# Get started

You need Linux or macOS, and tmux. The first run can install tmux for you.

## 1. Install

```sh
curl -fsSL https://matchblox.sh | sh
```

Read the script first at [matchblox.sh](https://matchblox.sh/install.sh).
It installs matchblox to `/usr/local/bin`, or to `~/.local/bin`, and starts
it.
You get the release binary for your machine. The script checks its
checksum and, with `gh` logged in, that CI built it from this repository,
and installs nothing that fails a check. With Go, the same version builds
from source:

```sh
go install github.com/blox-eng/matchblox/cmd/matchblox@latest
```

Check that it runs: `matchblox version`.

## 2. Run it

Run matchblox outside tmux:

```sh
matchblox
```

Outside tmux, the console opens in its own tmux session, `matchblox`. Every
terminal that runs `matchblox` joins it, and `prefix m` brings you back to
it from any pane. Inside tmux, it opens in the current pane.

The service starts with the console and keeps running after it closes: the
history and the stoker go on. `matchblox serve` runs it in the foreground,
for a log or a service manager.

## 3. Answer the first run

The first run asks one question: **Where do your agents run?**

- **this machine**: the console of this machine opens.
- **another machine**: pick a host from `~/.ssh/config`, or type one. See
  [Remote hosts](remote.md).

The first run also writes `~/.config/matchblox/config.toml` with goals for
this machine, and the agents it finds on `PATH`.

## 4. Open the setup steps

Sessions shows the setup steps above everything else, under `SET UP`.
The queue hooks and the guide show only when Claude Code is on this machine. Each
step says what it gives you. `Enter` shows the exact change before it runs. A
typed `y` makes the change. `x` closes a step that you do not want.

| Step | What it does |
|---|---|
| Install tmux | Installs tmux with your package manager, when it is not there. |
| Add the queue hooks | Adds the matchblox hooks to `~/.claude/settings.json`, and keeps a backup. Claude Code then tells the queue when it waits, and the answer peek what its question tool asks. It also allows the stoker's `matchblox` commands without a prompt. Your own hooks and rules stay. After an update that adds a hook or a rule, the step shows again with the entry it adds. |
| Add the way back | Adds `prefix m` and a tap target `◂ matchblox` to your tmux config (`~/.tmux.conf`), and keeps a backup. From any pane, one key or one tap brings you back to the console. It takes over `prefix m`, the tmux key that marks a pane. The tap needs the tmux mouse: `m` in the preview turns it on too (off unless you press it). |
| Keep matchblox running | Writes a user unit that runs `matchblox serve`: `~/Library/LaunchAgents/com.blox.matchblox.plist` on macOS (started with `launchctl bootstrap`), `~/.config/systemd/user/matchblox.service` on Linux (started with `systemctl --user enable --now`). The phone buzzes only while `serve` runs, so this keeps it running with no console open. On Linux, `loginctl enable-linger` keeps it after you log out. The step shows the file and the command first. |
| Open a guide session | Starts Claude Code with a prompt that reads this page and walks you through it. It uses your tokens. |

A step that is done folds away. `matchblox setup` shows every step again,
done and closed ones too.

## 5. Start your agents in tmux

In another terminal:

```sh
tmux new -s app claude
```

matchblox finds each agent that runs in a tmux pane. When an agent waits
for you, it comes to the top of Sessions. `Enter` jumps to its pane. The
way back (`prefix m`) returns to the console.

Next: [the console](console.md).
