# go-console

An interactive frontend for go-bash, with a small Go TUI command to demonstrate
terminal handoff. Run from the repository root in a terminal:

```sh
go run ./cmd/go-console
go run ./cmd/go-console -root ./some-directory -readonly -timeout 10m
```

The selected host directory is mounted at `/workspace`; writes persist unless
`-readonly` is set. `/work` and `/tmp` remain writable virtual memory filesystems.
Use `cmd/go-bash` for scripts and piped input.

| Key | Behavior |
| --- | --- |
| Left/Right, Home/End | Edit the command line |
| Up/Down | Browse session history |
| Ctrl-R | Search history |
| Tab | Complete commands, files, directories after `cd`, and `$VARIABLE` names |
| Ctrl-C | Discard unfinished input or cancel the running command |
| Ctrl-D at an empty prompt | Exit, including EXIT-trap cleanup |

The primary prompt is `$ `; incomplete shell constructs continue at `> `.
History stays in memory. Commands receive live stdin/stdout/stderr, so `read`,
`cat`, and Go command handlers can interact with the terminal. Each submitted
script has a five-minute deadline by default, including time spent in a TUI.

Try `pw` followed by Tab, `ls /` followed by Tab, or `echo $PA` followed by Tab.
Completion uses the virtual cwd, PATH, functions, and variables. It escapes
inserted filenames, including spaces and shell metacharacters. It completes
literal prefixes at word ends; it does not evaluate expansions, expand `~`,
offer command-specific flags, or implement Bash programmable completion.

Use `clear` to clear the visible screen and return to a fresh prompt, preserving
terminal scrollback. It writes ANSI sequences through the command's stdout, so
redirection and pipelines behave normally. The example accepts `clear` without
arguments; it does not use host terminfo or implement ncurses `clear` options.

`history` lists numbered submissions, `history N` shows the most recent N, and
`history -c` clears both the list and Up/Down/Ctrl-R recall. The current submission
is included, multiline scripts form one entry, and consecutive identical
submissions share an entry. History keeps at most 500 entries and 4 MiB of text;
numbers increase as older entries are dropped and restart at 1 after clearing.
History is shared by the console session, including commands invoked from
pipelines and subshells; this is a small console helper, not the full Bash history
builtin. Output can be piped or redirected, for example `history 10 | cat`.

`reset` restores the terminal modes captured at console startup, leaves the
alternate screen, restores normal ANSI display modes and cursor visibility, and
clears the visible screen. Use it to recover after an interactive command leaves
the terminal in a bad state. It requires direct console stdin/stdout and accepts
no arguments. It does not read terminfo or implement ncurses `reset` options.

Type `tui` to browse the current virtual directory in an alternate terminal
screen. Use Up/Down or j/k to move and q to return. Ctrl-C cancels the TUI; resizing
the terminal redraws it. The command restores the screen and terminal modes on
return, timeout, and errors. It requires the console's stdin and stdout, so
`tui | cat` and `tui > file` report that a terminal is required.

The example keeps responsibilities separate:

- [main.go](main.go) uses `chzyer/readline` for editing and manages cancellable
  input, history, multiline scripts, and shell lifetime.
- [completion.go](completion.go) adapts `Shell.Complete` to the editor without
  executing shell code to discover candidates.
- [history.go](history.go) owns session history and synchronizes editor recall
  between prompts; [commands.go](commands.go) implements `clear` and `reset`.
- [tui.go](tui.go) registers a trusted Go command, reads entries through
  `Command.FS`, and demonstrates raw input, resize handling, and terminal cleanup.

Go TUI commands run in the same process and may use host terminal APIs. The
example detects terminal descriptors supplied through `Shell.RunIO` and stops
all TUI input before returning to the prompt. It does not launch host programs
such as vim or top, allocate a guest PTY, or emulate a terminal inside WASM.
Other applications can use their preferred Go TUI toolkit with the same live
I/O boundary and explicit terminal ownership.

The tests include a real pseudo-terminal session on macOS and Linux, exercising
editing, history listing and clearing, completion, terminal recovery, interactive
reads, TUI resizing, cancellation, timeouts, redirection rejection, and cleanup at
EOF.
