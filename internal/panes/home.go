package panes

// Home is the tmux session the console lives in, so a phone that connects
// lands in it and every pane has one way back to it.
const Home = "matchblox"

// HomeArgv is the command that runs the console in its own tmux session:
// -A attaches when the session is already there, so a second connection
// sees the same console. Inside tmux, or without tmux, it is nil and the
// console runs in place. -u keeps the client UTF-8: an ssh session with no
// locale, such as the phone's, would else see every glyph of the console
// as "_".
func HomeArgv(inTmux, haveTmux bool, self []string) []string {
	if inTmux || !haveTmux || len(self) == 0 {
		return nil
	}
	return append([]string{"tmux", "-u", "new-session", "-A", "-s", Home, "-e", HomeEnv + "=1"}, self...)
}

// SizeArgv makes the home window take the size of the device used last, so a
// phone that rotates, or a laptop that attaches, resizes the shared console.
func SizeArgv() []string {
	return []string{"tmux", "set-option", "-w", "-t", "=" + Home + ":", "window-size", "latest"}
}

// HomeKey is what the app sends to go back to the console: an APC string,
// which no keyboard types, so the key takes nothing from the apps in the
// panes and works whatever the prefix is.
const HomeKey = "\x1b_matchblox\x1b\\"

// homeUserKey is the user-keys slot of HomeKey: far from 0, where the
// person's own user keys start.
const homeUserKey = "900"

// HomeKeyArgv makes HomeKey a key of the tmux server that takes the client
// to the console's session. A client attached already gets it at once.
func HomeKeyArgv() []string {
	return []string{"tmux", "set-option", "-s", "user-keys[" + homeUserKey + "]", HomeKey,
		";", "bind-key", "-n", "User" + homeUserKey, "switch-client", "-t", "=" + Home}
}

// HomeEnv is set in the home session: the console there is the session's
// only command, so on an error it waits for the person to read it.
const HomeEnv = "MATCHBLOX_HOME"

// WayBack is the tmux config of the way back to the console: prefix m, and
// a tap on "◂ matchblox" in the status line. "=matchblox" is the session of
// that exact name, never one that only starts with it. The label is added
// once, also when the config is sourced again. The default
// status-left-length (10) would cut the tap target off, so a shorter length
// grows to 40.
const WayBack = `# matchblox: the way back to the console
bind m switch-client -t =matchblox
if -F "#{m:*range=user|matchblox*,#{status-left}}" "" "set -ga status-left '#[range=user|matchblox] ◂ matchblox #[norange]'"
if -F "#{<:#{status-left-length},40}" "set -g status-left-length 40"
bind -n MouseDown1Status if -F "#{==:#{mouse_status_range},matchblox}" "switch-client -t =matchblox" "select-window -t ="
`

// Mouse turns the tmux mouse on, so a tap on "◂ matchblox" works from a
// phone. It changes how every pane selects and scrolls, so the way back
// adds it only when the person asks.
const Mouse = `# matchblox: a tap on ◂ matchblox needs the mouse
set -g mouse on
`
