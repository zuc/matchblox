# Config

matchblox works without a config. The first run writes
`~/.config/matchblox/config.toml` with goals for this machine: `load1_over`
is the number of cores, and the agents it finds. Every key is optional.

The service reads the config when it starts. To apply a change, stop the
service and run `matchblox` again. The lock file next to the service socket
holds its pid. With `XDG_RUNTIME_DIR` set (most Linux systems):

```sh
kill "$(cat "$XDG_RUNTIME_DIR/matchblox/matchblox.sock.lock")"
matchblox
```

Without it (macOS), the lock is in the temp directory:

```sh
kill "$(cat "${TMPDIR:-/tmp}/matchblox-$(id -u)/matchblox.sock.lock")"
matchblox
```

To use another file, give `matchblox --config <file>`.

## Every key

```toml
--8<-- "config.example.toml"
```

## The stoker's agent

`[stoker] agent` is the command of the stoker agent. matchblox starts it
itself, on a tmux server of its own (`tmux -L matchblox`, session
`stoker`), the first time you press `f` or `n`. It is `claude` by default.
`codex`, `opencode` and `agent` (Cursor) work too. The command must be on the service's
`PATH`.

`[stoker] pane` is gone: you no longer start the stoker in a pane of your
own. `matchblox status` warns while the config still names it, and nothing
reads it. Remove the line. See [the stoker](stoker.md#the-stoker-agent).
